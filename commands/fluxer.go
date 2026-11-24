package commands

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	fluxer "github.com/invaliduser231/fluxer-go"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/decide"
	"github.com/zeozeozeo/x3/docs"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
	"github.com/zeozeozeo/x3/videothumb"
	"github.com/zeozeozeo/x3/worldmem"
)

const (
	fluxerCachePrefix = "fluxer:"
	fluxerMaxTextLen  = 2000
	// fluxerCommandResultMarker prefixes bot messages that are command output
	// rather than conversation, so history backfill can skip them (the text
	// equivalent of Discord's interaction-name check).
	fluxerCommandResultMarker = "\u2063\u200b\u2063\u200d\u2063"
	// fluxerHistoryMessages caps the per-channel ring buffer that feeds the
	// continuation decision, mirroring the Discord history cache.
	fluxerHistoryMessages = 512
	// fluxerCatchUpLimit bounds how many messages a catch-up backfill walks
	// before it gives up looking for the cached history's newest message.
	fluxerCatchUpLimit = 100
)

// FluxerRuntime lets main cancel the gateway session and wait for it.
type FluxerRuntime struct {
	Cancel context.CancelFunc
	Done   <-chan struct{}
}

// fluxerMessageSource fetches one page of channel history, newest first, so
// tests can stub out the REST client.
type fluxerMessageSource func(ctx context.Context, channelID, beforeID string, limit int) ([]fluxer.Message, error)

type FluxerBot struct {
	client   *fluxer.Client
	session  *fluxer.Session
	prefix   string
	instance string

	selfMu   sync.RWMutex
	selfID   string
	selfName string

	inProgress sync.Map // channel ID -> context.CancelFunc

	messageSource fluxerMessageSource
}

type fluxerMessage struct {
	ChannelID   string
	MessageID   string
	GuildID     string
	SenderID    string
	Author      string
	Content     string
	Attachments []fluxerAttachment
	Timestamp   time.Time
	ReplyTo     *fluxerMessage
	IsBot       bool
	IsDM        bool
}

type fluxerAttachment struct {
	Filename    string
	ContentType string
	URL         string
	Size        int
	Data        []byte
	DataURI     string
	IsImage     bool
	IsVideo     bool
	IsAudio     bool
	IsVoice     bool
	Duration    time.Duration
}

type fluxerOutFile struct {
	Name        string
	ContentType string
	Data        []byte
}

// fluxerHistoryEntry is one message of the per-channel ring buffer that feeds
// the continuation decision's unanswered turns.
type fluxerHistoryEntry struct {
	ID         string
	AuthorID   string
	AuthorName string
	Bot        bool
	Content    string
}

// StartFluxerBot starts the Fluxer gateway session when X3_FLUXER_ENABLED is
// set. It returns (nil, nil) when the platform is disabled, like
// StartMatrixBot.
func StartFluxerBot(parent context.Context) (*FluxerRuntime, error) {
	if !truthy(os.Getenv("X3_FLUXER_ENABLED")) {
		return nil, nil
	}
	token := strings.TrimSpace(strings.TrimPrefix(os.Getenv("X3_FLUXER_TOKEN"), "Bot "))
	if token == "" {
		return nil, errors.New("fluxer bot enabled, but X3_FLUXER_TOKEN is required")
	}
	prefix := strings.TrimSpace(os.Getenv("X3_FLUXER_COMMAND_PREFIX"))
	if prefix == "" {
		prefix = "!x3"
	}

	router := fluxer.NewRouter()
	client := fluxer.NewClient(token)
	session := &fluxer.Session{Token: token}

	instance := strings.TrimSpace(os.Getenv("X3_FLUXER_INSTANCE"))
	if instance != "" {
		origin, err := fluxer.NormalizeOrigin(instance)
		if err != nil {
			return nil, fmt.Errorf("invalid X3_FLUXER_INSTANCE %q: %w", instance, err)
		}
		discovered, err := fluxer.Discover(parent, origin)
		if err != nil {
			return nil, fmt.Errorf("fluxer instance discovery failed for %s: %w", origin, err)
		}
		client = discovered.NewClient(token)
		session = discovered.NewSession(token)
	}
	session.Handler = router.Handle

	bot := &FluxerBot{
		client:        client,
		session:       session,
		prefix:        prefix,
		instance:      instance,
		messageSource: defaultFluxerMessageSource(client),
	}
	bot.registerEventHandlers(router)

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := session.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("fluxer session stopped", "err", err)
		}
	}()

	slog.Info("fluxer bot started", "prefix", prefix, "instance", instance)
	return &FluxerRuntime{Cancel: cancel, Done: done}, nil
}

func defaultFluxerMessageSource(client *fluxer.Client) fluxerMessageSource {
	return func(ctx context.Context, channelID, beforeID string, limit int) ([]fluxer.Message, error) {
		return client.Messages(ctx, channelID, fluxer.MessageQuery{Before: beforeID, Limit: limit})
	}
}

func (b *FluxerBot) registerEventHandlers(router *fluxer.Router) {
	fluxer.On(router, func(r *fluxer.Ready) {
		b.selfMu.Lock()
		b.selfID = r.User.ID
		b.selfName = r.User.DisplayName()
		b.selfMu.Unlock()
		slog.Info("fluxer gateway ready", "user", r.User.Tag(), "guilds", len(r.Guilds))
	})
	fluxer.On(router, func(m *fluxer.MessageCreate) {
		// handlers run on the gateway read goroutine; dispatch like Matrix
		go b.onMessage(context.Background(), &m.Message, m.ChannelType)
	})
	fluxer.On(router, func(u *fluxer.MessageUpdate) {
		go b.onMessageUpdate(context.Background(), &u.Message)
	})
	fluxer.On(router, func(_ *fluxer.ChannelUpdate) {
		resetFluxerEnvironmentCache()
	})
	fluxer.On(router, func(_ *fluxer.GuildUpdate) {
		resetFluxerEnvironmentCache()
	})
}

func (b *FluxerBot) selfUserID() string {
	b.selfMu.RLock()
	defer b.selfMu.RUnlock()
	if b.selfID != "" {
		return b.selfID
	}
	return b.session.User().ID
}

func (b *FluxerBot) selfDisplayName() string {
	b.selfMu.RLock()
	defer b.selfMu.RUnlock()
	if b.selfName != "" {
		return b.selfName
	}
	return b.session.User().DisplayName()
}

func (b *FluxerBot) channelKey(channelID string) string {
	return fluxerCachePrefix + channelID
}

func (b *FluxerBot) userKey(userID string) string {
	return fluxerCachePrefix + userID
}

func (b *FluxerBot) eventKey(messageID string) string {
	return fluxerCachePrefix + messageID
}

// onMessage mirrors the Matrix/Discord receive flow: record history, skip
// bots, auto-imports, commands, blacklist, then the trigger decision.
func (b *FluxerBot) onMessage(ctx context.Context, m *fluxer.Message, channelType int) {
	if m == nil || m.ID == "" {
		return
	}
	b.recordFluxerHistory(m)
	if m.Author.ID == b.selfUserID() || m.Author.Bot {
		return
	}
	msg := b.buildMessage(m, channelType)
	if msg == nil {
		return
	}

	if handled, err := b.tryAutoImportChatArchive(ctx, msg); handled {
		if err != nil {
			slog.Error("fluxer chatlog auto-import failed", "channel_id", msg.ChannelID, "message_id", msg.MessageID, "err", err)
		}
		return
	}
	if handled, err := b.tryAutoImportWorldmem(ctx, msg); handled {
		if err != nil {
			slog.Error("fluxer worldmem auto-import failed", "channel_id", msg.ChannelID, "message_id", msg.MessageID, "err", err)
		}
		return
	}

	trimmed := strings.TrimSpace(msg.Content)
	isDM := isFluxerPrivateChannel(channelType)
	if rawCommand, ok := b.parseCommand(trimmed, isDM); ok {
		if err := b.handleCommand(ctx, msg, rawCommand, isDM); err != nil {
			slog.Error("fluxer command failed", "channel_id", msg.ChannelID, "message_id", msg.MessageID, "err", err)
			_ = b.sendText(ctx, msg.ChannelID, msg.MessageID, "Error: "+err.Error())
		}
		return
	}
	if db.IsChannelKeyInBlacklist(b.channelKey(msg.ChannelID)) {
		return
	}

	shouldTrigger := isDM || b.mentioned(m) || b.replyToBot(msg) || containsX3Regex.MatchString(trimmed)
	if !shouldTrigger {
		cache := db.GetChannelCacheByKey(b.channelKey(msg.ChannelID))
		if cache.PersonaMeta.RespondAlways {
			shouldTrigger = true
		} else {
			shouldTrigger = shouldTriggerContinuation(cache, candidate{
				ID:          msg.MessageID,
				Content:     msg.Content,
				AuthorName:  b.userName(msg),
				ChannelName: cachedFluxerChannelName(msg.ChannelID),
				PersonaName: cache.PersonaMeta.Name,
				BotNames:    botNamesForDecision(cache),
				turns:       b.unansweredTurnsFunc(msg.ChannelID, msg.MessageID),
			})
		}
	}
	if !shouldTrigger {
		return
	}
	if err := b.handleLlm(ctx, msg, false, ""); err != nil {
		// logged only, like the Discord path: error text must never enter the
		// channel history the LLM sees
		slog.Error("fluxer LLM interaction failed", "channel_id", msg.ChannelID, "message_id", msg.MessageID, "err", err)
	}
}

func (b *FluxerBot) mentioned(m *fluxer.Message) bool {
	selfID := b.selfUserID()
	for _, user := range m.Mentions {
		if user.ID == selfID {
			return true
		}
	}
	return false
}

func (b *FluxerBot) replyToBot(msg *fluxerMessage) bool {
	if msg == nil || msg.ReplyTo == nil || msg.ReplyTo.SenderID != b.selfUserID() {
		return false
	}
	// a reply to a command output is command interaction, not conversation
	return !strings.HasPrefix(msg.ReplyTo.Content, fluxerCommandResultMarker)
}

func (b *FluxerBot) userName(msg *fluxerMessage) string {
	if msg == nil {
		return ""
	}
	if alias := strings.TrimSpace(db.GetUserCacheByKey(b.userKey(msg.SenderID)).Username); alias != "" {
		return alias
	}
	return msg.Author
}

// buildMessage turns a gateway message into the platform-agnostic form the
// LLM pipeline consumes. Attachments are classified from metadata only; bytes
// are downloaded lazily where they are actually needed.
func (b *FluxerBot) buildMessage(m *fluxer.Message, channelType int) *fluxerMessage {
	if m.Type != fluxer.MessageDefault && m.Type != fluxer.MessageReply {
		return nil // skip system message types (joins, pins, calls, ...)
	}
	msg := &fluxerMessage{
		ChannelID: m.ChannelID,
		MessageID: m.ID,
		GuildID:   m.GuildID,
		SenderID:  m.Author.ID,
		Author:    fluxerAuthorName(m),
		Content:   strings.TrimSpace(m.Content),
		Timestamp: m.Timestamp,
		IsBot:     m.Author.Bot || m.Author.ID == b.selfUserID(),
		IsDM:      isFluxerPrivateChannel(channelType),
	}
	if alias := strings.TrimSpace(db.GetUserCacheByKey(b.userKey(m.Author.ID)).Username); alias != "" {
		msg.Author = alias
	}
	if m.MessageReference != nil && m.MessageReference.Type == fluxer.ReferenceDefault && m.ReferencedMessage != nil {
		if ref := b.buildMessage(m.ReferencedMessage, channelType); ref != nil {
			msg.ReplyTo = ref
		}
	}
	for _, a := range m.Attachments {
		msg.Attachments = append(msg.Attachments, fluxerAttachmentFrom(a, m))
	}
	for _, s := range m.Stickers {
		msg.Content = appendContextLine(msg.Content, fmt.Sprintf("[sticker: %s]", s.Name))
	}
	return msg
}

func fluxerAuthorName(m *fluxer.Message) string {
	if m.Member != nil && m.Member.Nick != nil && strings.TrimSpace(*m.Member.Nick) != "" {
		return strings.TrimSpace(*m.Member.Nick)
	}
	return fluxerUserDisplayName(m.Author)
}

func fluxerUserDisplayName(u fluxer.User) string {
	if u.GlobalName != nil && strings.TrimSpace(*u.GlobalName) != "" {
		return strings.TrimSpace(*u.GlobalName)
	}
	return u.Username
}

// fluxerAttachmentFrom classifies an attachment from its metadata, without
// downloading it.
func fluxerAttachmentFrom(a fluxer.Attachment, m *fluxer.Message) fluxerAttachment {
	att := fluxerAttachment{
		Filename:    firstNonEmpty(a.Filename, "attachment"),
		ContentType: a.ContentType,
		URL:         a.URL,
		Size:        int(a.Size),
	}
	contentType := strings.ToLower(a.ContentType)
	if contentType == "" {
		if isVideoFilename(att.Filename) {
			contentType = "video/"
		} else if isAudioFilename(att.Filename) {
			contentType = "audio/"
		}
	}
	switch {
	case strings.HasPrefix(contentType, "image/"):
		att.IsImage = true
	case strings.HasPrefix(contentType, "video/"):
		att.IsVideo = true
	case strings.HasPrefix(contentType, "audio/"):
		att.IsAudio = true
	}
	if a.Duration > 0 {
		att.Duration = time.Duration(a.Duration) * time.Second
	}
	att.IsVoice = (m.Flags&fluxer.FlagVoiceMessage != 0 || a.Waveform != "") && (att.IsAudio || att.IsVideo)
	return att
}

func (b *FluxerBot) downloadFluxerAttachment(ctx context.Context, att *fluxerAttachment) error {
	if len(att.Data) > 0 || att.URL == "" {
		return nil
	}
	limit := videothumb.MaxDownloadBytes()
	if att.Size > 0 && att.Size > limit {
		return fmt.Errorf("attachment %s exceeds the download limit (%d > %d bytes)", att.Filename, att.Size, limit)
	}
	data, err := downloadFluxerURL(ctx, att.URL, limit)
	if err != nil {
		return err
	}
	if att.ContentType == "" {
		att.ContentType = http.DetectContentType(data)
	}
	att.Data = data
	att.Size = len(data)
	if att.IsImage && len(data) <= llm.MaxImageBytes {
		att.DataURI = fluxerDataURI(att.ContentType, data)
	}
	return nil
}

func downloadFluxerURL(ctx context.Context, url string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("download failed with status %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("download exceeds the limit (%d > %d bytes)", len(data), limit)
	}
	return data, nil
}

func fluxerDataURI(contentType string, data []byte) string {
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	return fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(data))
}

// fluxerDocumentBlocks extracts text from non-image document attachments via
// Gotenberg, mirroring the Matrix/Discord document handling.
func (b *FluxerBot) fluxerDocumentBlocks(ctx context.Context, msg *fluxerMessage) string {
	var sb strings.Builder
	count := 0
	for i := range msg.Attachments {
		attachment := &msg.Attachments[i]
		if attachment.IsImage || attachment.IsVideo || attachment.IsAudio {
			continue
		}
		if !docs.Supported(attachment.Filename, attachment.ContentType) {
			continue
		}
		if count >= docs.MaxDocsPerMessage {
			break
		}
		if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
			slog.Warn("fluxer document download failed", "err", err, "filename", attachment.Filename)
			continue
		}
		count++
		if attachment.Size > docs.MaxDownloadBytes {
			slog.Warn("fluxer document exceeds size limit", "filename", attachment.Filename, "size", attachment.Size)
			fmt.Fprintf(&sb, "[attachment %s: file too large, limit is 10MB]\n", attachment.Filename)
			continue
		}
		text, err := docs.ExtractText(ctx, attachment.Filename, attachment.ContentType, attachment.Data)
		if err != nil {
			slog.Warn("fluxer document extraction failed", "err", err, "filename", attachment.Filename)
			fmt.Fprintf(&sb, "[attachment %s: %s]\n", attachment.Filename, err.Error())
			continue
		}
		sb.WriteString(docs.FormatBlock(attachment.Filename, text))
		sb.WriteByte('\n')
	}
	return strings.TrimSpace(sb.String())
}

// appendFluxerMediaNotes keeps media-only messages readable in history.
func appendFluxerMediaNotes(content string, msg *fluxerMessage) string {
	for _, att := range msg.Attachments {
		if att.IsVideo {
			content = appendContextLine(content, fmt.Sprintf("[video attachment: %s]", att.Filename))
		} else if att.IsAudio {
			if att.IsVoice {
				content = appendContextLine(content, "[voice message]")
			} else {
				content = appendContextLine(content, fmt.Sprintf("[audio attachment: %s]", att.Filename))
			}
		}
	}
	return content
}

// appendFluxerAudioTranscriptionLines appends transcript lines for audio and
// video attachments of the triggering message.
func (b *FluxerBot) appendFluxerAudioTranscriptionLines(ctx context.Context, content string, msg *fluxerMessage) string {
	for i := range msg.Attachments {
		attachment := &msg.Attachments[i]
		if !attachment.IsAudio && !attachment.IsVideo {
			continue
		}
		content = appendContextLine(content, transcribeAudioMedia(ctx, b.fluxerAudioMedia(msg.MessageID, attachment)))
	}
	return content
}

// fluxerAudioMedia adapts a Fluxer attachment to the shared transcription
// pipeline; media bytes are downloaded lazily.
func (b *FluxerBot) fluxerAudioMedia(messageID string, attachment *fluxerAttachment) audioMedia {
	kind := audioMediaAudio
	switch {
	case attachment.IsVideo:
		kind = audioMediaVideo
	case attachment.IsVoice:
		kind = audioMediaVoice
	}
	size := attachment.Size
	if size == 0 {
		size = len(attachment.Data)
	}
	media := audioMedia{
		ID:       fmt.Sprintf("%s:%s", messageID, attachment.Filename),
		Filename: attachment.Filename,
		Size:     size,
		Duration: attachment.Duration,
		Kind:     kind,
	}
	if len(attachment.Data) > 0 || attachment.URL != "" {
		media.Download = func(ctx context.Context) ([]byte, error) {
			if len(attachment.Data) > 0 {
				return attachment.Data, nil
			}
			if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
				return nil, err
			}
			if len(attachment.Data) == 0 {
				return nil, errors.New("missing media data")
			}
			return attachment.Data, nil
		}
	}
	return media
}

// handleLlm is the Fluxer counterpart of Matrix's handleLlm and Discord's
// handleLlmInteraction2.
func (b *FluxerBot) handleLlm(ctx context.Context, msg *fluxerMessage, isRegenerate bool, regeneratePrepend string) error {
	key := b.channelKey(msg.ChannelID)
	ctx, cancel := context.WithCancel(ctx)
	if oldCancel, loaded := b.inProgress.Swap(msg.ChannelID, cancel); loaded {
		if c, ok := oldCancel.(context.CancelFunc); ok {
			c()
		}
	}
	defer func() {
		b.inProgress.Delete(msg.ChannelID)
		cancel()
	}()

	stopTyping := b.startTypingLoop(ctx, msg.ChannelID)

	cache := db.GetChannelCacheByKey(key)
	models := cache.PersonaMeta.GetModels()
	if len(models) == 0 {
		stopTyping()
		return llm.ErrNoModelsForCompletion()
	}

	var llmer *llm.Llmer
	if cache.Llmer != nil {
		llmer = cache.Llmer
		llmer.ConversationID = key
	} else {
		llmer = llm.NewLlmerForKey(key)
		if cache.ImportedHistory != nil {
			llmer.Messages = append([]llm.Message(nil), cache.ImportedHistory.Messages...)
		}
	}

	if isRegenerate {
		lastID := lastFluxerAssistantMessageID(llmer)
		if lastID == "" {
			stopTyping()
			return errRegenerateNoMessage
		}
		llmer.LobotomizeUntilMessageID(lastID)
		// refresh the system prompt so persona (e.g. jailbreak) changes
		// apply to regenerations too, mirroring the Discord path
		promptContext, _ := b.fluxerPromptContext(ctx, key, "", cache, msg)
		p := persona.GetPersonaByMetaWithBotName(cache.PersonaMeta, b.userName(msg), b.selfDisplayName(), msg.IsDM, promptContext)
		llmer.SetPersona(p, &cache.PersonaMeta.ExcessiveSplit)
		llmer.WorldBible = ""
	} else {
		// catch the channel up on messages the bot never triggered on; this
		// is the Fluxer equivalent of Discord's addContextMessages probe
		if err := b.addFluxerContextMessages(ctx, llmer, cache, msg); err != nil {
			slog.Warn("fluxer history backfill failed; continuing with cached context",
				"err", err, "channel_id", msg.ChannelID, "message_id", msg.MessageID)
		}
		promptContext, worldFacts := b.fluxerPromptContext(ctx, key,
			composeWorldQuery(msg.Content, recentWorldQueryContext(llmer, worldQueryContextMessages)), cache, msg)
		p := persona.GetPersonaByMetaWithBotName(cache.PersonaMeta, b.userName(msg), b.selfDisplayName(), msg.IsDM, promptContext)
		content := fluxerFormatMsg(augmentContentWithLinkMetadata(msg.Content), b.userName(msg), msg.ReplyTo)
		if docBlocks := b.fluxerDocumentBlocks(ctx, msg); docBlocks != "" {
			content = appendContextLine(content, docBlocks)
		}
		content = b.appendFluxerAudioTranscriptionLines(ctx, content, msg)
		content = appendFluxerMediaNotes(content, msg)
		llmer.AddMessageWithID(llm.RoleUser, content, 0, msg.MessageID)
		var added *llm.Message
		if len(llmer.Messages) > 0 {
			added = &llmer.Messages[len(llmer.Messages)-1]
			added.Author = msg.Author
			added.Timestamp = msg.Timestamp
			added.MessageID = msg.MessageID
		}
		for i := range msg.Attachments {
			attachment := &msg.Attachments[i]
			if attachment.DataURI == "" && attachment.IsImage {
				if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
					slog.Warn("fluxer image download failed", "err", err, "filename", attachment.Filename)
				}
			}
			if attachment.DataURI != "" {
				llmer.AddImage(attachment.DataURI)
			} else if attachment.IsImage && added != nil {
				// downloaded but over llm.MaxImageBytes, so not attached
				note := fmt.Sprintf("[image %s omitted: larger than %dMB]",
					attachment.Filename, llm.MaxImageBytes/(1024*1024))
				added.Content = appendContextLine(added.Content, note)
			}
		}
		if hasFluxerVideoAttachment(msg) {
			b.addFluxerVideoThumbnails(ctx, llmer, msg)
		}
		llmer.SetPersona(p, &cache.PersonaMeta.ExcessiveSplit)
		llmer.WorldBible = worldmem.FormatBible(worldFacts)
	}

	thinkPrefill := cache.PersonaMeta.JailbreakThinkPrefill(regeneratePrepend)
	prepend := cache.PersonaMeta.Prepend
	if regeneratePrepend != "" && thinkPrefill == "" {
		// without think-prefill the regenerate text replaces the
		// response prefill; with think-prefill it went into the
		//  IMD block and the response keeps the persona prefill
		prepend = regeneratePrepend
		if !endsWithWhitespace(prepend) {
			prepend += " "
		}
	}

	ctxLen := cache.EffectiveContextLength(false)
	previousAnchor := cache.CacheAnchorMessageID
	cacheAnchor := llmer.CacheAnchorFor(previousAnchor, ctxLen)
	if cacheAnchor != previousAnchor {
		slog.Debug("moved prompt cache anchor",
			"channel_id", msg.ChannelID,
			"old_anchor", previousAnchor,
			"new_anchor", cacheAnchor,
			"num_messages", llmer.NumMessages(),
		)
		cache.CacheAnchorMessageID = cacheAnchor
	}

	slog.Info("requesting Fluxer LLM completion",
		"channel_id", msg.ChannelID,
		"num_models", len(models),
		"num_messages", llmer.NumMessages(),
		"is_regenerate", isRegenerate,
		"think_prefill", thinkPrefill != "",
	)
	response, usage, err := requestCompletionCacheFriendly(llmer, models, cache.PersonaMeta.Settings, prepend, thinkPrefill, ctxLen, cacheAnchor, ctx)
	if err != nil {
		stopTyping()
		return err
	}
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage
	_ = db.UpdateGlobalStats(usage)

	var thinking string
	thinking, response = extractThinkingForDisplay(response)
	if displayResponse, memories := extractMemoryTags(response); displayResponse != response || len(memories) > 0 {
		response = displayResponse
		setLatestAssistantMessageContent(llmer, response)
		cache.AddMemories(memories)
	}
	response = handleWorldTags(key, response, func(s string) {
		setLatestAssistantMessageContent(llmer, s)
	})

	files := []*discord.File{}
	if thinking != "" && cache.PersonaMeta.ThinkingTraces {
		files = append(files, &discord.File{Name: "reasoning.txt", Reader: strings.NewReader(thinking)})
	}
	response, generatedFiles := consumeGeneratedArtifacts(response, llmer.GeneratedArtifacts)
	files = append(files, generatedFiles...)
	rawResponse := response
	response, files, htmlRendered := prepareHTMLRenderedResponse(ctx, cache.PersonaMeta, response, files)
	response = strings.TrimSpace(response)

	outFiles, err := fluxerFilesFromDiscordFiles(files)
	if err != nil {
		stopTyping()
		return err
	}

	// stop typing before the first message goes out, like the Discord path
	stopTyping()

	var sentID string
	if isRegenerate {
		// delete trailing splits of the previous response, keeping the
		// first which gets edited below
		b.deleteFluxerTrailingSplits(ctx, msg.ChannelID)
		lastID := lastFluxerAssistantMessageID(llmer)
		sentID, err = b.editResponse(ctx, msg.ChannelID, lastID, replaceLlmTagsWithNewlines(response, &cache.PersonaMeta), outFiles)
	} else {
		sentID, err = b.sendLLMResponse(ctx, msg.ChannelID, msg.MessageID, response, outFiles, &cache.PersonaMeta)
	}
	if err != nil {
		return err
	}
	if sentID != "" {
		setFluxerLatestAssistantMessageMetadata(llmer, b.selfDisplayName(), sentID, time.Now())
		if htmlRendered {
			_ = db.WriteMessageRenderedContentKey(b.eventKey(sentID), rawResponse)
		}
	}

	if summariesEnabled() {
		for i := range cache.Summaries {
			cache.Summaries[i].Age++
		}
	}
	persistedLlmer := *llmer
	// data URIs are multi-MB; only the newest image is ever sent, so don't
	// persist the rest
	persistedLlmer.Messages = llm.TrimPersistedMessages(llmer.Messages)
	cache.Llmer = &persistedLlmer
	if cache.ImportedHistory != nil {
		cache.ImportedHistory.Messages = append([]llm.Message(nil), persistedLlmer.Messages...)
	}
	cache.UpdateInteractionTime()
	if !msg.IsBot {
		_ = db.SetInteractionTimeKey(b.userKey(msg.SenderID), time.Now())
	}
	return cache.WriteKey(key)
}

// startTypingLoop keeps a typing indicator alive for the duration of the LLM
// request. The returned stop function waits for the loop to exit.
func (b *FluxerBot) startTypingLoop(ctx context.Context, channelID string) (stop func()) {
	loopCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		defer close(done)
		_ = b.client.Typing(loopCtx, channelID)
		ticker := time.NewTicker(8 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-loopCtx.Done():
				return
			case <-ticker.C:
				if err := b.client.Typing(loopCtx, channelID); err != nil {
					return
				}
			}
		}
	}()
	return func() {
		cancel()
		<-done
	}
}

func hasFluxerVideoAttachment(msg *fluxerMessage) bool {
	for _, att := range msg.Attachments {
		if att.IsVideo {
			return true
		}
	}
	return false
}

// addFluxerVideoThumbnails downloads trigger-message videos and attaches an
// ffmpeg-extracted frame as the vision image, mirroring the Matrix path.
func (b *FluxerBot) addFluxerVideoThumbnails(ctx context.Context, llmer *llm.Llmer, msg *fluxerMessage) {
	if !videothumb.Enabled() || !videothumb.BackendAvailable() {
		return
	}
	for i := len(msg.Attachments) - 1; i >= 0; i-- {
		attachment := &msg.Attachments[i]
		if !attachment.IsVideo {
			continue
		}
		if len(attachment.Data) == 0 {
			if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
				slog.Warn("fluxer video download failed", "err", err, "filename", attachment.Filename)
				continue
			}
		}
		if len(attachment.Data) == 0 {
			continue
		}
		dataURI := videothumb.Thumbnail(ctx, videothumb.Source{
			Data:        attachment.Data,
			Filename:    attachment.Filename,
			ContentType: attachment.ContentType,
		})
		if dataURI != "" {
			llmer.AddImage(dataURI)
			return // only the newest video gets a thumbnail, like the other platforms
		}
	}
}

// addFluxerContextMessages backfills channel history into the llmer before
// the trigger message is appended: a full window for fresh conversations, or
// a catch-up of what arrived since the cached history for existing ones.
func (b *FluxerBot) addFluxerContextMessages(ctx context.Context, llmer *llm.Llmer, cache *db.ChannelCache, msg *fluxerMessage) error {
	if cache.ImportedHistory != nil || llmerHasContext(llmer) {
		return b.catchUpFluxerContext(ctx, llmer, msg)
	}
	return b.fillFluxerContext(ctx, llmer, cache, msg)
}

func llmerHasContext(llmer *llm.Llmer) bool {
	if llmer == nil {
		return false
	}
	for _, m := range llmer.Messages {
		if m.Role != llm.RoleSystem {
			return true
		}
	}
	return false
}

// fillFluxerContext classifies cache.ContextLength messages before the
// trigger into the llmer, oldest first.
func (b *FluxerBot) fillFluxerContext(ctx context.Context, llmer *llm.Llmer, cache *db.ChannelCache, msg *fluxerMessage) error {
	remaining := cache.EffectiveContextLength(false)
	if remaining <= 0 {
		return nil
	}
	before := msg.MessageID
	var batch []fluxer.Message
	for remaining > 0 {
		page := min(remaining, 100)
		messages, err := b.messageSource(ctx, msg.ChannelID, before, page)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			break
		}
		batch = append(batch, messages...)
		remaining -= len(messages)
		before = messages[len(messages)-1].ID
		if len(messages) < page {
			break
		}
	}
	b.appendFluxerContextBatch(llmer, batch)
	return nil
}

// catchUpFluxerContext walks history backwards from the trigger until it
// reaches a message already present in the llmer, then appends only the
// messages the bot never saw, oldest first.
func (b *FluxerBot) catchUpFluxerContext(ctx context.Context, llmer *llm.Llmer, msg *fluxerMessage) error {
	known := make(map[string]struct{})
	var newestKnown time.Time
	for _, m := range llmer.Messages {
		if m.MessageID != "" && m.MessageID != "0" {
			known[m.MessageID] = struct{}{}
			if m.Timestamp.After(newestKnown) {
				newestKnown = m.Timestamp
			}
		}
	}
	if len(known) == 0 {
		return nil
	}
	before := msg.MessageID
	var fresh []fluxer.Message // newest first
	foundKnown := false
	for len(fresh) < fluxerCatchUpLimit {
		messages, err := b.messageSource(ctx, msg.ChannelID, before, 50)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			break
		}
		for _, m := range messages {
			if _, ok := known[m.ID]; ok {
				foundKnown = true
				break
			}
			fresh = append(fresh, m)
		}
		before = messages[len(messages)-1].ID
		if foundKnown || len(messages) < 50 {
			break
		}
	}
	if len(fresh) == 0 {
		return nil
	}
	// guard against walking past the cached history (e.g. after a lobotomy):
	// only keep messages strictly newer than the newest known one
	if !newestKnown.IsZero() {
		kept := fresh[:0]
		for _, m := range fresh {
			if m.Timestamp.After(newestKnown) {
				kept = append(kept, m)
			}
		}
		fresh = kept
	}
	b.appendFluxerContextBatch(llmer, fresh)
	return nil
}

// appendFluxerContextBatch appends a newest-first batch of channel messages
// to the llmer in chronological order.
func (b *FluxerBot) appendFluxerContextBatch(llmer *llm.Llmer, batch []fluxer.Message) {
	lastAssistantID := lastFluxerAssistantMessageID(llmer)
	for i := len(batch) - 1; i >= 0; i-- {
		m := &batch[i]
		if m.Author.ID == b.selfUserID() {
			b.appendFluxerBotContextMessage(llmer, m)
			lastAssistantID = m.ID
			continue
		}
		if m.Author.Bot {
			continue // other bots are not conversation partners
		}
		if b.isFluxerCommandInvocation(m) {
			continue
		}
		content := fluxerHistoryMessageContent(m)
		reference := m.ReferencedMessage
		if reference != nil && reference.ID == lastAssistantID {
			reference = nil // replying to the immediately preceding assistant message
		}
		if reference != nil && m.MessageReference != nil && m.MessageReference.Type != fluxer.ReferenceDefault {
			reference = nil
		}
		username := fluxerAuthorName(m)
		if alias := strings.TrimSpace(db.GetUserCacheByKey(b.userKey(m.Author.ID)).Username); alias != "" {
			username = alias
		}
		content = fluxerFormatMsg(augmentContentWithLinkMetadata(content), username, fluxerReferencedSummary(reference))
		llmer.AddMessageWithID(llm.RoleUser, content, 0, m.ID)
		if len(llmer.Messages) > 0 {
			added := &llmer.Messages[len(llmer.Messages)-1]
			added.Author = username
			added.Timestamp = m.Timestamp
			added.MessageID = m.ID
		}
	}
}

// isFluxerCommandInvocation reports whether a user message was a text
// command, which never belongs in the LLM history.
func (b *FluxerBot) isFluxerCommandInvocation(m *fluxer.Message) bool {
	trimmed := strings.TrimSpace(m.Content)
	if trimmed == "" {
		return false
	}
	if trimmed == b.prefix || strings.HasPrefix(trimmed, b.prefix+" ") {
		return true
	}
	if m.GuildID != "" || !strings.HasPrefix(trimmed, "!") {
		return false
	}
	// bare "!command" in DMs, mirroring parseCommand
	rest := strings.TrimSpace(strings.TrimPrefix(trimmed, "!"))
	if rest == "" {
		return false
	}
	name := strings.ToLower(rest)
	if idx := strings.IndexAny(name, " \t"); idx >= 0 {
		name = name[:idx]
	}
	switch name {
	case "chat", "persona", "context", "lobotomy", "regenerate", "stats",
		"models", "chatlog", "worldmem", "site", "blacklist", "imageblacklist",
		"help":
		return true
	}
	return false
}

// appendFluxerBotContextMessage reconstructs one of the bot's own messages
// from channel history, applying the same skip and restore rules as the
// Discord path.
func (b *FluxerBot) appendFluxerBotContextMessage(llmer *llm.Llmer, m *fluxer.Message) {
	content := m.Content
	if strings.HasPrefix(content, fluxerCommandResultMarker) {
		return // command output, not conversation
	}
	// narration images and code-run output never enter the history
	for _, a := range m.Attachments {
		if isFluxerNarrationAttachment(a) {
			return
		}
	}
	if content == "" && len(m.Attachments) == 0 && len(m.Stickers) == 0 {
		return
	}
	if rendered, err := db.GetMessageRenderedContentKey(b.eventKey(m.ID)); err == nil && rendered != "" {
		content = stripWorldMemFooter(rendered)
	}
	splitContinuation := strings.HasSuffix(content, "\u200B")
	content = strings.ReplaceAll(content, "\u200B", "")
	content = fluxerHistoryMessageContent(&fluxer.Message{
		ID:                m.ID,
		Author:            m.Author,
		Content:           content,
		Timestamp:         m.Timestamp,
		Mentions:          m.Mentions,
		MentionChannels:   m.MentionChannels,
		Stickers:          m.Stickers,
		Attachments:       m.Attachments,
		Reactions:         m.Reactions,
		MessageReference:  m.MessageReference,
		ReferencedMessage: m.ReferencedMessage,
	})
	if splitContinuation {
		content = strings.TrimRight(content, "\n") + "\n\n"
	}
	if strings.TrimSpace(content) == "" {
		return
	}
	llmer.AddMessageWithID(llm.RoleAssistant, content, 0, m.ID)
	if len(llmer.Messages) > 0 {
		added := &llmer.Messages[len(llmer.Messages)-1]
		added.Author = b.selfDisplayName()
		added.Timestamp = m.Timestamp
		added.MessageID = m.ID
	}
}

func isFluxerNarrationAttachment(a fluxer.Attachment) bool {
	name := strings.TrimPrefix(a.Filename, "SPOILER_")
	return strings.HasPrefix(name, "narration-")
}

var (
	fluxerUserMentionRegex    = regexp.MustCompile(`<@!?(\d+)>`)
	fluxerChannelMentionRegex = regexp.MustCompile(`<#(\d+)>`)
)

// fluxerHistoryMessageContent renders a message's content for history:
// mentions become names, stickers and reaction counts become notes.
func fluxerHistoryMessageContent(m *fluxer.Message) string {
	content := m.Content
	byID := make(map[string]string, len(m.Mentions))
	for _, u := range m.Mentions {
		byID[u.ID] = fluxerUserDisplayName(u)
	}
	if len(byID) > 0 {
		content = fluxerUserMentionRegex.ReplaceAllStringFunc(content, func(match string) string {
			id := fluxerUserMentionRegex.FindStringSubmatch(match)[1]
			if name, ok := byID[id]; ok {
				return "@" + name
			}
			return match
		})
	}
	channels := make(map[string]string, len(m.MentionChannels))
	for _, c := range m.MentionChannels {
		channels[c.ID] = c.Name
	}
	if len(channels) > 0 {
		content = fluxerChannelMentionRegex.ReplaceAllStringFunc(content, func(match string) string {
			id := fluxerChannelMentionRegex.FindStringSubmatch(match)[1]
			if name, ok := channels[id]; ok && name != "" {
				return "#" + name
			}
			return match
		})
	}
	for _, s := range m.Stickers {
		content = appendContextLine(content, fmt.Sprintf("[sticker: %s]", s.Name))
	}
	if len(m.Reactions) > 0 {
		var parts []string
		for _, r := range m.Reactions {
			parts = append(parts, fmt.Sprintf("%s (%d)", r.Emoji.String(), r.Count))
		}
		content = appendContextLine(content, "reactions: "+strings.Join(parts, ", "))
	}
	for _, a := range m.Attachments {
		if isFluxerNarrationAttachment(a) {
			continue
		}
		switch {
		case strings.HasPrefix(strings.ToLower(a.ContentType), "image/"):
			content = appendContextLine(content, fmt.Sprintf("[image attachment: %s]", a.Filename))
		case strings.HasPrefix(strings.ToLower(a.ContentType), "video/"):
			content = appendContextLine(content, fmt.Sprintf("[video attachment: %s]", a.Filename))
		case strings.HasPrefix(strings.ToLower(a.ContentType), "audio/"):
			content = appendContextLine(content, fmt.Sprintf("[audio attachment: %s]", a.Filename))
		}
	}
	return content
}

// fluxerReferencedSummary reduces a referenced message to the compact reply
// summary used in history formatting.
func fluxerReferencedSummary(reference *fluxer.Message) *fluxerMessage {
	if reference == nil {
		return nil
	}
	return &fluxerMessage{
		SenderID: reference.Author.ID,
		Author:   fluxerAuthorName(reference),
		Content:  reference.Content,
	}
}

func fluxerFormatMsg(content, username string, reference *fluxerMessage) string {
	if reference != nil && strings.TrimSpace(reference.Content) != "" {
		return fmt.Sprintf("<in reply to %s: \"%s\">\n%s: %s", reference.Author, ellipsisTrim(strings.TrimSpace(reference.Content), 64), username, content)
	}
	if username != "" {
		return fmt.Sprintf("%s: %s", username, content)
	}
	return content
}

func lastFluxerAssistantMessageID(llmer *llm.Llmer) string {
	if llmer == nil {
		return ""
	}
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleAssistant && llmer.Messages[i].MessageID != "" && llmer.Messages[i].MessageID != "0" {
			return llmer.Messages[i].MessageID
		}
	}
	return ""
}

func setFluxerLatestAssistantMessageMetadata(llmer *llm.Llmer, author, messageID string, timestamp time.Time) {
	if llmer == nil {
		return
	}
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleAssistant {
			llmer.Messages[i].Author = author
			llmer.Messages[i].Timestamp = timestamp
			llmer.Messages[i].MessageID = messageID
			return
		}
	}
}

// ---- sending ----

// sendLLMResponse splits the response and sends it, replying to the trigger
// with the first message and attaching files to the last.
func (b *FluxerBot) sendLLMResponse(ctx context.Context, channelID, replyToID, response string, files []fluxerOutFile, meta *persona.PersonaMeta) (string, error) {
	messages := splitLlmTags(response, meta)
	if len(messages) == 0 && len(files) > 0 {
		messages = []string{""}
	}
	var allIDs []string
	for i, content := range messages {
		currentFiles := []fluxerOutFile(nil)
		if i == len(messages)-1 {
			currentFiles = files
		}
		ids, err := b.sendTextParts(ctx, channelID, replyToID, content, currentFiles)
		allIDs = append(allIDs, ids...)
		if err != nil {
			b.rememberFluxerTrailingSplits(channelID, allIDs)
			if len(allIDs) > 0 {
				return allIDs[0], err
			}
			return "", err
		}
		replyToID = ""
	}
	b.rememberFluxerTrailingSplits(channelID, allIDs)
	if len(allIDs) > 0 {
		return allIDs[0], nil
	}
	return "", nil
}

// sendTextParts hard-splits one logical message into Fluxer-sized messages
// and attaches the files to the final one. Every message but the last carries
// the zero-width continuation marker so history backfill can rejoin them.
func (b *FluxerBot) sendTextParts(ctx context.Context, channelID, replyToID, text string, files []fluxerOutFile) ([]string, error) {
	var ids []string
	text = strings.TrimSpace(text)
	if text != "" || len(files) == 0 {
		parts := splitFluxerText(text)
		for i, part := range parts {
			isFinal := i == len(parts)-1 && len(files) == 0
			if !isFinal {
				part += "\u200B"
			}
			sent, err := b.sendFluxerMessage(ctx, channelID, replyToID, part, nil)
			if sent != nil {
				ids = append(ids, sent.ID)
			}
			if err != nil {
				return ids, err
			}
			replyToID = ""
		}
	}
	if len(files) > 0 {
		for _, group := range chunkFluxerFiles(files, 10) {
			sent, err := b.sendFluxerMessage(ctx, channelID, replyToID, "", group)
			if sent != nil {
				ids = append(ids, sent.ID)
			}
			if err != nil {
				return ids, err
			}
			replyToID = ""
		}
	}
	return ids, nil
}

func chunkFluxerFiles(files []fluxerOutFile, size int) [][]fluxerOutFile {
	if len(files) == 0 {
		return nil
	}
	var groups [][]fluxerOutFile
	for len(files) > size {
		groups = append(groups, files[:size])
		files = files[size:]
	}
	return append(groups, files)
}

// suppressFluxerMentions returns an allowed-mentions object that disables all
// mention parsing, so replies never ping users. Parse must be a non-nil empty
// slice: fluxer-go serializes a nil slice as null, which Fluxer rejects.
func suppressFluxerMentions() *fluxer.AllowedMentions {
	return &fluxer.AllowedMentions{Parse: []string{}}
}

func (b *FluxerBot) sendFluxerMessage(ctx context.Context, channelID, replyToID, content string, files []fluxerOutFile) (*fluxer.Message, error) {
	send := fluxer.MessageSend{
		Content:         content,
		AllowedMentions: suppressFluxerMentions(),
	}
	if replyToID != "" {
		send.MessageReference = &fluxer.MessageReference{
			MessageID: replyToID,
			ChannelID: channelID,
			Type:      fluxer.ReferenceDefault,
		}
	}
	if len(files) == 0 {
		sent, err := b.client.SendMessage(ctx, channelID, send)
		if err != nil {
			slog.Warn("fluxer send failed", "err", err, "channel_id", channelID)
			return nil, err
		}
		return sent, nil
	}
	fluxerFiles := make([]fluxer.File, 0, len(files))
	for _, f := range files {
		fluxerFiles = append(fluxerFiles, fluxer.File{Name: f.Name, Data: f.Data})
	}
	sent, err := b.client.SendMessageFiles(ctx, channelID, send, fluxerFiles)
	if err != nil {
		slog.Warn("fluxer send failed", "err", err, "channel_id", channelID)
		return nil, err
	}
	return sent, nil
}

// fluxerTrailingSplits remembers the split message IDs of the last LLM
// response per channel, so regenerate can delete them.
var fluxerTrailingSplits sync.Map // channel ID -> []string

func (b *FluxerBot) rememberFluxerTrailingSplits(channelID string, allIDs []string) {
	if len(allIDs) > 1 {
		trailing := append([]string(nil), allIDs[1:]...)
		fluxerTrailingSplits.Store(channelID, trailing)
	} else {
		fluxerTrailingSplits.Delete(channelID)
	}
}

// deleteFluxerTrailingSplits deletes the trailing splits of the previous
// response, keeping the first which gets edited by regenerate.
func (b *FluxerBot) deleteFluxerTrailingSplits(ctx context.Context, channelID string) {
	raw, ok := fluxerTrailingSplits.Load(channelID)
	if !ok {
		return
	}
	fluxerTrailingSplits.Delete(channelID)
	trailing, ok := raw.([]string)
	if !ok {
		return
	}
	for _, messageID := range trailing {
		if messageID == "" {
			continue
		}
		if err := b.client.DeleteMessage(ctx, channelID, messageID); err != nil {
			slog.Warn("fluxer: failed to delete trailing split", "channel_id", channelID, "message_id", messageID, "err", err)
		}
	}
}

// editResponse rewrites the target message for regenerate. Over-length text
// is truncated with the full response attached as a file, like the Discord
// path.
func (b *FluxerBot) editResponse(ctx context.Context, channelID, targetID, text string, files []fluxerOutFile) (string, error) {
	if targetID == "" {
		return "", errRegenerateNoMessage
	}
	full := firstNonEmpty(strings.TrimSpace(text), "<empty response>")
	content := full
	var uploads []fluxer.File
	if len([]rune(full)) > fluxerMaxTextLen {
		content = ellipsisTrim(full, fluxerMaxTextLen)
		uploads = append(uploads, fluxer.File{
			Name: fmt.Sprintf("response-%s.txt", targetID),
			Data: []byte(full),
		})
	}
	edit := fluxer.MessageEdit{
		Content:         &content,
		AllowedMentions: suppressFluxerMentions(),
	}
	if _, err := b.client.EditMessageUploads(ctx, channelID, targetID, edit, uploads); err != nil {
		return "", err
	}
	if len(files) > 0 {
		if _, err := b.sendTextParts(ctx, channelID, targetID, "", files); err != nil {
			return targetID, err
		}
	}
	return targetID, nil
}

func splitFluxerText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	runes := []rune(text)
	var out []string
	for len(runes) > fluxerMaxTextLen {
		cut := fluxerMaxTextLen
		for cut > fluxerMaxTextLen/2 && runes[cut-1] != '\n' && runes[cut-1] != ' ' {
			cut--
		}
		if cut <= fluxerMaxTextLen/2 {
			cut = fluxerMaxTextLen
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		out = append(out, strings.TrimSpace(string(runes)))
	}
	return out
}

func fluxerFilesFromDiscordFiles(files []*discord.File) ([]fluxerOutFile, error) {
	out := make([]fluxerOutFile, 0, len(files))
	for _, file := range files {
		if file == nil || file.Reader == nil {
			continue
		}
		data, err := io.ReadAll(file.Reader)
		if err != nil {
			return nil, err
		}
		contentType := mime.TypeByExtension(path.Ext(file.Name))
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		out = append(out, fluxerOutFile{
			Name:        file.Name,
			ContentType: contentType,
			Data:        data,
		})
	}
	return out, nil
}

// ---- message-edit re-trigger (DM-only, like Discord) ----

// onMessageUpdate re-runs the last DM interaction when its triggering user
// message is edited, deleting the bot's replies first.
func (b *FluxerBot) onMessageUpdate(ctx context.Context, m *fluxer.Message) {
	if m == nil || m.ID == "" || m.Author.ID == b.selfUserID() || m.Author.Bot {
		return
	}
	if !b.isFluxerDMChannel(ctx, m.ChannelID) {
		return
	}
	key := b.channelKey(m.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	llmer := cache.Llmer
	if llmer == nil {
		return
	}
	// the edited message must be the newest user message in the history
	lastUserIdx := -1
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleUser && llmer.Messages[i].MessageID != "" {
			lastUserIdx = i
			break
		}
	}
	if lastUserIdx < 0 || llmer.Messages[lastUserIdx].MessageID != m.ID {
		return
	}
	// cancel any in-flight interaction for this channel
	if oldCancel, ok := b.inProgress.LoadAndDelete(m.ChannelID); ok {
		if c, ok := oldCancel.(context.CancelFunc); ok {
			c()
		}
	}
	// delete the bot's messages that followed the edited message
	for i := lastUserIdx + 1; i < len(llmer.Messages); i++ {
		if llmer.Messages[i].Role != llm.RoleAssistant {
			continue
		}
		if id := llmer.Messages[i].MessageID; id != "" && id != "0" {
			if err := b.client.DeleteMessage(ctx, m.ChannelID, id); err != nil {
				slog.Warn("fluxer: failed to delete message after edit", "channel_id", m.ChannelID, "message_id", id, "err", err)
			}
		}
	}
	// drop the edited message and everything after it; handleLlm re-adds it
	llmer.LobotomizeUntilMessageID(m.ID)
	cache.Llmer = llmer
	if err := cache.WriteKey(key); err != nil {
		slog.Warn("fluxer: failed to persist edit re-trigger", "err", err)
	}
	msg := b.buildMessage(m, fluxer.ChannelDM)
	if msg == nil {
		return
	}
	if err := b.handleLlm(ctx, msg, false, ""); err != nil {
		slog.Error("fluxer LLM interaction failed after edit", "channel_id", m.ChannelID, "message_id", m.ID, "err", err)
	}
}

func (b *FluxerBot) isFluxerDMChannel(ctx context.Context, channelID string) bool {
	channel, err := b.client.Channel(ctx, channelID)
	if err != nil {
		return false
	}
	return isFluxerPrivateChannel(channel.Type)
}

// isFluxerPrivateChannel reports whether messages in a channel type belong to a
// private conversation: a DM, group DM, or personal notes.
func isFluxerPrivateChannel(channelType int) bool {
	switch channelType {
	case fluxer.ChannelDM, fluxer.ChannelGroupDM, fluxer.ChannelPersonalNotes:
		return true
	default:
		return false
	}
}

// ---- history ring buffer for the continuation decision ----

var fluxerChannelHistories sync.Map // channel ID -> *fluxerChannelHistory

type fluxerChannelHistory struct {
	mu      sync.RWMutex
	entries []fluxerHistoryEntry // newest first
}

func getFluxerChannelHistory(channelID string) *fluxerChannelHistory {
	if cached, ok := fluxerChannelHistories.Load(channelID); ok {
		if history, ok := cached.(*fluxerChannelHistory); ok {
			return history
		}
	}
	history := &fluxerChannelHistory{}
	if cached, loaded := fluxerChannelHistories.LoadOrStore(channelID, history); loaded {
		if existing, ok := cached.(*fluxerChannelHistory); ok {
			return existing
		}
	}
	return history
}

func (h *fluxerChannelHistory) upsert(entry fluxerHistoryEntry) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i, existing := range h.entries {
		if existing.ID == entry.ID {
			h.entries[i] = entry
			return
		}
	}
	h.entries = append([]fluxerHistoryEntry{entry}, h.entries...)
	if len(h.entries) > fluxerHistoryMessages {
		h.entries = h.entries[:fluxerHistoryMessages]
	}
}

func (b *FluxerBot) recordFluxerHistory(m *fluxer.Message) {
	if m.ID == "" {
		return
	}
	getFluxerChannelHistory(m.ChannelID).upsert(fluxerHistoryEntry{
		ID:         m.ID,
		AuthorID:   m.Author.ID,
		AuthorName: fluxerAuthorName(m),
		Bot:        m.Author.Bot || m.Author.ID == b.selfUserID(),
		Content:    m.Content,
	})
}

// unansweredTurnsFunc supplies the continuation decision with the messages
// that arrived after the bot's most recent reply, from the ring buffer.
func (b *FluxerBot) unansweredTurnsFunc(channelID, messageID string) func(wanted int) []decide.Turn {
	return func(wanted int) []decide.Turn {
		return b.fluxerUnansweredSince(channelID, messageID, wanted)
	}
}

func (b *FluxerBot) fluxerUnansweredSince(channelID, messageID string, wanted int) []decide.Turn {
	if wanted <= 0 {
		return nil
	}
	history := getFluxerChannelHistory(channelID)
	history.mu.RLock()
	count := min(wanted+1, len(history.entries))
	newestFirst := make([]fluxerHistoryEntry, count)
	copy(newestFirst, history.entries[:count])
	history.mu.RUnlock()

	botID := b.selfUserID()
	turns := make([]decide.Turn, 0, wanted)
	for _, entry := range newestFirst {
		if entry.ID == messageID {
			continue // the candidate is passed separately
		}
		if entry.AuthorID == botID {
			break // the bot's last reply; everything older is in the cached history
		}
		role := decide.RoleUser
		if entry.Bot {
			role = decide.RoleBot
		}
		turns = append(turns, decide.Turn{
			Role:    role,
			Name:    entry.AuthorName,
			Content: decide.CleanTurn(role, entry.Content),
		})
	}
	// the snapshot is newest first
	for i, j := 0, len(turns)-1; i < j; i, j = i+1, j-1 {
		turns[i], turns[j] = turns[j], turns[i]
	}
	if len(turns) > wanted {
		turns = turns[len(turns)-wanted:]
	}
	return turns
}

// ---- environment snapshot for the persona prompt ----

type cachedFluxerEnvironment struct {
	env       *persona.FluxerEnvironment
	updatedAt time.Time
}

const fluxerEnvironmentCacheTTL = 12 * time.Hour

var fluxerEnvironmentCache sync.Map // channel ID -> cachedFluxerEnvironment

func resetFluxerEnvironmentCache() {
	fluxerEnvironmentCache.Range(func(key, _ any) bool {
		fluxerEnvironmentCache.Delete(key)
		return true
	})
}

func cachedFluxerChannelName(channelID string) string {
	if cached, ok := fluxerEnvironmentCache.Load(channelID); ok {
		if entry, ok := cached.(*cachedFluxerEnvironment); ok && entry.env != nil {
			return entry.env.ChannelName
		}
	}
	return ""
}

func (b *FluxerBot) fluxerEnvironment(ctx context.Context, channelID, guildID string) *persona.FluxerEnvironment {
	if cached, ok := fluxerEnvironmentCache.Load(channelID); ok {
		if entry, ok := cached.(*cachedFluxerEnvironment); ok && time.Since(entry.updatedAt) <= fluxerEnvironmentCacheTTL {
			return entry.env
		}
	}
	env := b.buildFluxerEnvironment(ctx, channelID, guildID)
	fluxerEnvironmentCache.Store(channelID, &cachedFluxerEnvironment{env: env, updatedAt: time.Now()})
	return env
}

func (b *FluxerBot) buildFluxerEnvironment(ctx context.Context, channelID, guildID string) *persona.FluxerEnvironment {
	env := &persona.FluxerEnvironment{}
	channel, err := b.client.Channel(ctx, channelID)
	if err != nil {
		slog.Warn("failed to build fluxer environment for prompt context", "err", err, "channel_id", channelID)
		return env
	}
	env.ChannelID = channel.ID
	env.ChannelName = strings.TrimSpace(channel.Name)
	env.ChannelDescription = strings.TrimSpace(channel.Topic)
	if channel.GuildID != "" {
		guildID = channel.GuildID
	}
	if guildID != "" {
		if guild, err := b.client.Guild(ctx, guildID); err == nil && guild != nil {
			env.GuildName = strings.TrimSpace(guild.Name)
		} else if err != nil {
			slog.Warn("failed to fetch fluxer guild for environment", "err", err, "guild_id", guildID)
		}
		if channels, err := b.client.GuildChannels(ctx, guildID); err == nil {
			env.VisibleChannels = fluxerVisibleChannelRefs(channels, channelID)
		}
	}
	return env
}

func fluxerVisibleChannelRefs(channels []fluxer.Channel, currentChannelID string) []persona.FluxerChannelRef {
	categories := make(map[string]string)
	for _, c := range channels {
		if c.Type == fluxer.ChannelCategory && strings.TrimSpace(c.Name) != "" {
			categories[c.ID] = strings.TrimSpace(c.Name)
		}
	}
	var refs []persona.FluxerChannelRef
	for _, c := range channels {
		if c.ID == currentChannelID || !isFluxerTextChannel(c) {
			continue
		}
		name := strings.TrimSpace(c.Name)
		if name == "" {
			continue
		}
		refs = append(refs, persona.FluxerChannelRef{
			ID:          c.ID,
			Name:        name,
			Category:    categories[c.ParentID],
			Description: strings.TrimSpace(c.Topic),
		})
	}
	return refs
}

func isFluxerTextChannel(c fluxer.Channel) bool {
	return c.Type == fluxer.ChannelText || c.Type == fluxer.ChannelAnnouncement
}

func (b *FluxerBot) fluxerPromptContext(ctx context.Context, key, worldQuery string, cache *db.ChannelCache, msg *fluxerMessage) (persona.PromptContext, []string) {
	promptCtx := persona.PromptContext{
		Memories: append([]string(nil), cache.Memories...),
		Context:  append([]string(nil), cache.Context...),
	}
	var world []string
	if !cache.WorldMemDisabled {
		world = worldEntriesForPrompt(key, worldQuery, false)
	}
	if summariesEnabled() {
		promptCtx.Summaries = append([]persona.Summary(nil), cache.Summaries...)
	}
	if env := b.fluxerEnvironment(ctx, msg.ChannelID, msg.GuildID); env != nil && !env.IsEmpty() {
		promptCtx.Fluxer = env
	}
	return promptCtx, world
}
