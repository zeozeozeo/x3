package commands

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"slices"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
)

var lobotomyMessagesRegex = regexp.MustCompile(`(?i)Removed last (\d+) (messages|turns) from the context`)

var imageURLRegexp = regexp.MustCompile(`https?://[^\s<>"']+`)

var imageURLExtensions = map[string]struct{}{
	".png":  {},
	".jpg":  {},
	".jpeg": {},
	".webp": {},
	".gif":  {},
	".avif": {},
}

func getLobotomyAmountFromMessage(msg discord.Message) int {
	matches := lobotomyMessagesRegex.FindStringSubmatch(msg.Content)
	if len(matches) != 3 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	if strings.EqualFold(matches[2], "turns") {
		return n * 2
	}
	return n
}

func isLobotomyMessage(msg discord.Message) bool {
	return msg.Interaction != nil &&
		(msg.Interaction.Name == "lobotomy" || msg.Interaction.Name == "random_dms")
}

func isChatlogMessage(msg discord.Message) bool {
	return msg.Interaction != nil && msg.Interaction.Name == "chatlog"
}

func formatMsg(msg, username string, reference *discord.Message) string {
	trimmedRefContent := ""
	if reference != nil {
		trimmedRefContent = strings.TrimSuffix(reference.Content, "\u200B")
	}

	if reference != nil && trimmedRefContent != "" {
		return fmt.Sprintf(
			"<in reply to %s: \"%s\">\n%s: %s",
			reference.Author.EffectiveName(),
			ellipsisTrim(strings.TrimSpace(trimmedRefContent), 64),
			username,
			msg,
		)
	}
	if username != "" {
		return fmt.Sprintf("%s: %s", username, msg)
	}
	return msg
}

// addImageAttachments adds compact image URLs to history. Fetching happens only
// later if the LLM request decides the image is recent enough to include.
func addImageAttachments(llmer *llm.Llmer, attachments []discord.Attachment) {
	if attachments == nil {
		return
	}
	for _, imageURL := range imageURLsFromAttachments(attachments) {
		llmer.AddImage(imageURL)
	}
}

func addImageLinks(llmer *llm.Llmer, content string) {
	for _, imageURL := range imageURLsFromContent(content) {
		llmer.AddImage(imageURL)
	}
}

func addImageSources(llmer *llm.Llmer, content string, attachments []discord.Attachment, embeds []discord.Embed) {
	for _, imageURL := range messageImageURLs(content, attachments, embeds) {
		llmer.AddImage(imageURL)
	}
}

func appendImageLinks(content string, imageURLs []string) string {
	if len(imageURLs) == 0 {
		return content
	}

	filtered := make([]string, 0, len(imageURLs))
	seen := make(map[string]struct{}, len(imageURLs))
	for _, raw := range imageURLs {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		if strings.Contains(content, raw) {
			continue
		}
		filtered = append(filtered, raw)
	}
	if len(filtered) == 0 {
		return content
	}
	return appendContextLine(content, "image links: "+strings.Join(filtered, ", "))
}

func appendContextLine(content, line string) string {
	if line == "" {
		return content
	}
	if content != "" {
		content += "\n"
	}
	return content + line
}

func messageReactionEmoji(reaction discord.MessageReaction) string {
	if reaction.Emoji.Name == "" {
		return ""
	}
	if reaction.Emoji.ID == 0 {
		return reaction.Emoji.Name
	}
	return reaction.Emoji.Reaction()
}

func messageReactionsContext(reactions []discord.MessageReaction) string {
	if len(reactions) == 0 {
		return ""
	}

	parts := make([]string, 0, len(reactions))
	for _, reaction := range reactions {
		emoji := messageReactionEmoji(reaction)
		if emoji == "" || reaction.Count <= 0 {
			continue
		}
		parts = append(parts, fmt.Sprintf("%s (%d)", emoji, reaction.Count))
	}
	if len(parts) == 0 {
		return ""
	}
	return "reactions: " + strings.Join(parts, ", ")
}

func imageURLsFromContent(content string) []string {
	matches := detectURLs(content, imageURLRegexp)
	if len(matches) == 0 {
		return nil
	}

	var out []string
	seen := make(map[string]struct{}, len(matches))
	for _, match := range matches {
		if match.angleQuoted {
			continue
		}
		raw := match.raw
		raw = strings.TrimRight(raw, ".,!?;:)]}")
		if !isLikelyImageURL(raw) {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func imageURLsFromAttachments(attachments []discord.Attachment) []string {
	if len(attachments) == 0 {
		return nil
	}

	out := make([]string, 0, len(attachments)*2)
	seen := make(map[string]struct{}, len(attachments)*2)
	for _, attachment := range attachments {
		if !isImageAttachment(attachment) {
			continue
		}
		for _, raw := range []string{attachment.URL, attachment.ProxyURL} {
			raw = strings.TrimSpace(raw)
			if raw == "" {
				continue
			}
			if _, ok := seen[raw]; ok {
				continue
			}
			seen[raw] = struct{}{}
			out = append(out, raw)
		}
	}
	return out
}

func imageURLsFromEmbeds(embeds []discord.Embed) []string {
	if len(embeds) == 0 {
		return nil
	}

	out := make([]string, 0, len(embeds)*2)
	seen := make(map[string]struct{}, len(embeds)*2)
	add := func(raw string) {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			return
		}
		if _, ok := seen[raw]; ok {
			return
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}

	for _, embed := range embeds {
		if embed.Image != nil {
			add(embed.Image.URL)
			add(embed.Image.ProxyURL)
		}
		if embed.Thumbnail != nil {
			add(embed.Thumbnail.URL)
			add(embed.Thumbnail.ProxyURL)
		}
	}

	return out
}

func messageImageURLs(content string, attachments []discord.Attachment, embeds []discord.Embed) []string {
	urls := imageURLsFromAttachments(attachments)
	urls = append(urls, imageURLsFromEmbeds(embeds)...)
	urls = append(urls, imageURLsFromContent(content)...)

	if len(urls) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(urls))
	out := make([]string, 0, len(urls))
	for _, raw := range urls {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if _, ok := seen[raw]; ok {
			continue
		}
		seen[raw] = struct{}{}
		out = append(out, raw)
	}
	return out
}

func isLikelyImageURL(raw string) bool {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(parsed.Path))
	_, ok := imageURLExtensions[ext]
	return ok
}

func setLatestAssistantMessageMetadata(llmer *llm.Llmer, message *discord.Message) {
	if llmer == nil || message == nil {
		return
	}
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleAssistant {
			llmer.Messages[i].Author = message.Author.EffectiveName()
			llmer.Messages[i].Timestamp = message.CreatedAt
			llmer.Messages[i].MessageID = message.ID.String()
			return
		}
	}
}

// writeTxtCache saves downloaded text attachment content to a local cache file
func writeTxtCache(attachmentID snowflake.ID, content []byte) error {
	if err := os.MkdirAll("x3-txt-cache", 0755); err != nil {
		return fmt.Errorf("failed to create txt cache directory: %w", err)
	}
	filePath := fmt.Sprintf("x3-txt-cache/%s.txt", attachmentID)
	return os.WriteFile(filePath, content, 0644)
}

// readTxtCache reads text attachment content from the local cache if available
func readTxtCache(attachmentID snowflake.ID) ([]byte, bool) {
	filePath := fmt.Sprintf("x3-txt-cache/%s.txt", attachmentID)
	content, err := os.ReadFile(filePath)
	return content, err == nil
}

var citeCleanupRegexp = regexp.MustCompile(`\[+(\d+)\]+\(<[^>]+>\)`)

// [[1]](<https://example.com>) -> [1]
func cleanupCites(s string) string {
	return citeCleanupRegexp.ReplaceAllString(s, "[$1]")
}

// getMessageContent extracts the text content from a message, including text attachments
func getMessageContent(message discord.Message) string {
	content := message.Content

	// rebuild message
	var sb strings.Builder
	sb.Grow(len(content))
	i := 0
	for line := range strings.SplitSeq(content, "\n") {
		if i > 0 {
			sb.WriteRune('\n')
		}
		// handleNarrationGenerate will add progress updates to the end of the messages, strip them
		if !strings.HasPrefix(line, "-# `[") && !strings.HasPrefix(line, "-# r-esrgan") {
			sb.WriteString(line)
		}
		i++
	}

	content = sb.String()

	for _, sticker := range message.StickerItems {
		if sticker.Name == "" {
			continue
		}
		content = appendContextLine(content, fmt.Sprintf("sent a sticker: %q", sticker.Name))
	}

	content = appendContextLine(content, messageReactionsContext(message.Reactions))

	// process text attachments
	if !message.Author.Bot {
		for i, attachment := range message.Attachments {
			if attachment.Filename == "reasoning.txt" {
				continue
			}
			maxSize := 16 * 1024
			if attachment.Size > maxSize {
				continue
			}

			if attachment.ContentType == nil || !strings.Contains(*attachment.ContentType, "text/plain") {
				continue
			}

			if i == 0 && content != "" {
				content += "\n"
			}

			var body []byte
			var readErr error
			// try reading from cache first
			if b, ok := readTxtCache(attachment.ID); ok {
				body = b
			} else {
				// download if not cached
				slog.Info("downloading attachment", slog.String("url", attachment.URL))
				resp, err := http.Get(attachment.URL)
				if err != nil {
					slog.Error("failed to fetch attachment", "err", err, slog.String("url", attachment.URL))
					continue
				}
				defer resp.Body.Close()

				limitedReader := io.LimitReader(resp.Body, int64(maxSize)+1)
				body, readErr = io.ReadAll(limitedReader)
				if readErr != nil {
					slog.Error("failed to read attachment body", slog.Any("err", readErr), slog.String("url", attachment.URL))
					continue
				}
				if len(body) > maxSize {
					slog.Warn("attachment body exceeded size limit after download", slog.String("id", attachment.ID.String()), slog.Int("size", len(body)))
					continue
				}
				if !utf8.Valid(body) {
					slog.Warn("attachment body is not valid utf8", slog.String("id", attachment.ID.String()))
					continue
				}

				// write to cache
				if err := writeTxtCache(attachment.ID, body); err != nil {
					slog.Error("failed to write txt cache", "err", err, slog.String("id", attachment.ID.String()))
				}
			}

			content += string(body)
		}
	}

	// replace mentions with readable names
	replacements := make([]string, 0, len(message.Mentions)+len(message.MentionChannels))
	for _, mention := range message.Mentions {
		replacements = append(replacements,
			mention.Mention(), "@"+mention.EffectiveName())
	}
	for _, channel := range message.MentionChannels {
		replacements = append(replacements,
			fmt.Sprintf("<#%d>", channel.ID), "#"+channel.Name)
	}

	replacer := strings.NewReplacer(replacements...)
	return replacer.Replace(content)
}

func isNarrationMessage(message discord.Message) bool {
	return slices.ContainsFunc(message.Attachments, func(a discord.Attachment) bool {
		return strings.HasPrefix(a.Filename, "narration-") || strings.HasPrefix(a.Filename, "SPOILER_narration-")
	})
}

func fetchMessagesBefore(
	client *bot.Client,
	channelID, beforeID snowflake.ID,
	wanted int,
) ([]discord.Message, error) {
	var all []discord.Message
	lastID := beforeID

	for wanted > 0 {
		msgs, err := client.Rest.GetMessages(channelID, 0, lastID, 0, min(wanted, 100))
		if err != nil {
			if len(all) != 0 {
				return all, nil
			}
			return nil, err
		}
		if len(msgs) == 0 {
			break
		}

		all = append(all, msgs...)
		wanted -= len(msgs)
		lastID = msgs[len(msgs)-1].ID
	}

	return all, nil
}

func loadContextMessagesBefore(
	client *bot.Client,
	channelID, beforeID snowflake.ID,
	wanted int,
) ([]discord.Message, error) {
	if wanted <= 0 {
		return nil, nil
	}

	history := getChannelMessageHistory(channelID)
	messages := history.snapshotBefore(beforeID, wanted)
	if len(messages) >= wanted {
		return messages, nil
	}

	cursor := beforeID
	if len(messages) > 0 {
		cursor = messages[len(messages)-1].ID
	}

	for len(messages) < wanted {
		remaining := wanted - len(messages)
		batchLimit := min(remaining, 100)
		fetched, err := fetchMessagesBefore(client, channelID, cursor, batchLimit)
		if err != nil {
			if len(messages) != 0 {
				return messages, nil
			}
			return nil, err
		}
		if len(fetched) == 0 {
			break
		}

		history.appendOlder(fetched)
		messages = append(messages, fetched...)
		cursor = fetched[len(fetched)-1].ID

		if len(fetched) < batchLimit {
			break
		}
	}

	return messages, nil
}

func defaultKnownUsernames() map[string]struct{} {
	return map[string]struct{}{
		"x3":      {},
		"clanker": {},
		"кланкер": {},
		"zeo":     {},
	}
}

func appendMissingMessages(dst *llm.Llmer, src []llm.Message) int {
	if dst == nil || len(src) == 0 {
		return 0
	}
	seen := make(map[string]struct{}, len(dst.Messages))
	for _, msg := range dst.Messages {
		key := msg.MessageID
		if key == "" && msg.ID != 0 {
			key = msg.ID.String()
		}
		if key != "" {
			seen[key] = struct{}{}
		}
	}

	added := 0
	for _, msg := range src {
		if msg.Role == llm.RoleSystem {
			continue
		}
		key := msg.MessageID
		if key == "" && msg.ID != 0 {
			key = msg.ID.String()
		}
		if key != "" {
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
		}
		dst.Messages = append(dst.Messages, msg)
		added++
	}
	return added
}

// Returns number of messages fetched, map of usernames, last assistant response message, last assistant message ID, last user ID
// (this way of restoring context is pretty hacky since we use \u200B to indicate splits/impersonations, but that way we don't have to
// rely on a db)
func addContextMessages(
	client *bot.Client,
	llmer *llm.Llmer,
	channelID,
	messageID snowflake.ID, // The ID of the message *before* which to fetch context
	contextLen int,
) (int, map[string]struct{}, *discord.Message, snowflake.ID, snowflake.ID) {
	if contextLen <= 0 {
		return 0, make(map[string]struct{}), nil, 0, 0
	}

	messages, err := loadContextMessagesBefore(client, channelID, messageID, contextLen)
	if err != nil {
		slog.Error("failed to get context messages", "err", err, "channel_id", channelID)
		return 0, make(map[string]struct{}), nil, 0, 0
	}

	latestImageIdx := -1
	for i, msg := range messages { // newest to oldest
		if slices.ContainsFunc(msg.Attachments, isImageAttachment) ||
			len(imageURLsFromContent(msg.Content)) > 0 ||
			len(imageURLsFromEmbeds(msg.Embeds)) > 0 {
			latestImageIdx = i
			break // found the newest image
		}
	}

	// see sendTextPart
	usernames := map[string]struct{}{
		"x3":      {},
		"clanker": {},
		"кланкер": {},
		"zeo":     {},
	}
	var lastResponseMessage *discord.Message
	var lastAssistantMessageID snowflake.ID
	var lastUserID snowflake.ID

	// add messages (oldest to newest)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role := llm.RoleUser
		content := ""
		rawContent := ""

		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
			lastAssistantMessageID = msg.ID
			isSplitAnyway := false

			if cachedContent, cacheErr := db.GetMessageRenderedContent(msg.ID); cacheErr == nil && strings.TrimSpace(cachedContent) != "" {
				content = cachedContent
			} else if isLobotomyMessage(msg) {
				// for lobotomy messages, delete the context
				llmer.Lobotomize(getLobotomyAmountFromMessage(msg))
				continue
			} else if isChatlogMessage(msg) {
				continue
			} else if isCardMessage(msg) {
				// for card messages, extract the actual content after the header
				_, cardContent, found := strings.Cut(msg.Content, "\n\n")
				if found {
					content = cardContent
				} else {
					content = msg.Content
				}
				// remove the trigger message
				llmer.Lobotomize(1)
			} else if msg.EditedTimestamp != nil && strings.HasPrefix(msg.Content, "**") && strings.Count(msg.Content, "**") >= 2 {
				// handle regenerated message with prefill
				content = strings.Replace(msg.Content, "**", "", 2)
			} else if isNarrationMessage(msg) {
				continue // skip narration images from handleNarrationGenerate
			} else if strings.HasPrefix(msg.Content, latexAPI) {
				content = fromLatexAPI(msg.Content)
				isSplitAnyway = true
			} else {
				content = cleanupCites(getMessageContent(msg))
			}

			if msg.ReferencedMessage != nil {
				lastResponseMessage = &msg
			}

			splitContinuation := isSplitAnyway || strings.HasSuffix(content, "\u200B")
			if strings.HasPrefix(content, "\u200B") {
				// means impersonate message!
				role = llm.RoleUser
			}
			content = strings.ReplaceAll(content, "\u200B", "")
			if splitContinuation {
				content = strings.TrimRight(content, "\n") + "\n\n"
			}

			// remove appends
			content = strings.TrimSuffix(content, interactionReminder)
			rawContent = content
		} else { // (user message)
			role = llm.RoleUser
			lastUserID = msg.Author.ID

			// check if this user message was an interaction trigger cached previously
			if msg.Interaction != nil {
				interactionPrompt, promptErr := db.GetMessageInteractionPrompt(msg.ID)
				if promptErr == nil && interactionPrompt != "" {
					content = interactionPrompt
				} else {
					content = getMessageContent(msg)
				}
			} else {
				content = getMessageContent(msg)
			}
			rawContent = content
			content = augmentContentWithLinkMetadata(content)
			content = appendImageLinks(content, messageImageURLs(rawContent, msg.Attachments, msg.Embeds))

			reference := msg.ReferencedMessage
			// don't format reply if it refers to the immediately preceding assistant message
			if reference != nil && reference.ID == lastAssistantMessageID {
				reference = nil
			}
			content = formatMsg(content, msg.Author.EffectiveName(), reference)
		}

		if content != "" {
			llmer.AddMessage(role, content, msg.ID)
			added := &llmer.Messages[len(llmer.Messages)-1]
			added.Author = msg.Author.EffectiveName()
			added.Timestamp = msg.CreatedAt
			added.MessageID = msg.ID.String()
			usernames[msg.Author.EffectiveName()] = struct{}{} // track username
		}

		// add image sources if this is the newest message containing one
		if i == latestImageIdx {
			addImageSources(llmer, rawContent, msg.Attachments, msg.Embeds)
		}
	}

	return len(messages), usernames, lastResponseMessage, lastAssistantMessageID, lastUserID
}
