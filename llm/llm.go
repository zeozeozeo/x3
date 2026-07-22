package llm

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"hash/fnv"
	"html"
	"log/slog"
	"net/url"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/eliza"
	"github.com/zeozeozeo/x3/markov"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
	"github.com/zeozeozeo/x3/persona"

	"github.com/zeozeozeo/go-aiml"

	_ "embed"
)

const (
	RoleUser                 = openai.ChatMessageRoleUser
	RoleAssistant            = openai.ChatMessageRoleAssistant
	RoleSystem               = openai.ChatMessageRoleSystem
	SplitWarningPrefix       = "*SYSTEM MESSAGE: you've used >=5 splits in your previous message, try staying within 1-3 splits!*\n"
	providerPromptTrimTarget = 8000
	estimateCacheTTL         = 30 * time.Minute
)

var (
	gImageCache    *imageCache = NewImageCache(100*1024*1024, 24*time.Hour)
	gAlice         *aiml.Kernel
	gEstimateCache = newEstimateCache(estimateCacheTTL)
)

type estimateCache struct {
	mu      sync.Mutex
	ttl     time.Duration
	entries map[string]estimateCacheEntry
}

type estimateCacheEntry struct {
	value     int
	expiresAt time.Time
}

func newEstimateCache(ttl time.Duration) *estimateCache {
	return &estimateCache{
		ttl:     ttl,
		entries: make(map[string]estimateCacheEntry),
	}
}

func (c *estimateCache) get(key string) (int, bool) {
	if c == nil || key == "" {
		return 0, false
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	entry, ok := c.entries[key]
	if !ok || now.After(entry.expiresAt) {
		delete(c.entries, key)
		return 0, false
	}
	entry.expiresAt = now.Add(c.ttl)
	c.entries[key] = entry
	return entry.value, true
}

func (c *estimateCache) set(key string, value int) {
	if c == nil || key == "" {
		return
	}
	now := time.Now()
	c.mu.Lock()
	defer c.mu.Unlock()
	c.pruneLocked(now)
	c.entries[key] = estimateCacheEntry{
		value:     value,
		expiresAt: now.Add(c.ttl),
	}
}

func (c *estimateCache) pruneLocked(now time.Time) {
	for key, entry := range c.entries {
		if now.After(entry.expiresAt) {
			delete(c.entries, key)
		}
	}
}

//go:embed alicebot/*.aiml
var aliceFS embed.FS

func init() {
	gAlice = aiml.NewKernel()
	gAlice.SetVerbose(false)
	gAlice.SetBotPredicate("name", "Alice")
	gAlice.SetBotPredicate("gender", "female")
	gAlice.SetBotPredicate("age", "25")
	gAlice.SetBotPredicate("location", "California")
	gAlice.SetBotPredicate("size", "98000")

	entries, err := aliceFS.ReadDir("alicebot")
	if err == nil {
		categoryCount := 0
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".aiml") {
				continue
			}

			filepath := "alicebot/" + entry.Name()
			data, err := aliceFS.ReadFile(filepath)
			if err != nil {
				continue
			}

			// Parse the AIML data
			parser := aiml.NewParser()
			if err := parser.Parse(strings.NewReader(string(data))); err != nil {
				continue
			}

			for key, template := range parser.GetCategories() {
				normalizedPattern := strings.ToUpper(strings.TrimSpace(key.Pattern))
				normalizedThat := strings.ToUpper(strings.TrimSpace(key.That))
				normalizedTopic := strings.ToUpper(strings.TrimSpace(key.Topic))
				gAlice.AddPattern(normalizedPattern, normalizedThat, normalizedTopic, template)
				categoryCount++
			}
		}
		slog.Info("loaded alice brain", "categoryCount", categoryCount, "files", len(entries))
	}
}

// generateImageDescriptionCallback is a callback function for generating image descriptions
var generateImageDescriptionCallback func(imageURL string, ctx context.Context) (string, error) = func(imageURL string, ctx context.Context) (string, error) {
	slog.Warn("image description callback not initialized, skipping image description")
	return "", nil
}

// SetImageDescriptionCallback sets the callback for generating image descriptions
func SetImageDescriptionCallback(callback func(imageURL string, ctx context.Context) (string, error)) {
	generateImageDescriptionCallback = callback
}

const (
	markovChainOrder = 2
	markovMaxLength  = 100
)

var (
	errNoModelsForCompletion = errors.New("no models provided for completion")
)

func ErrNoModelsForCompletion() error {
	return errNoModelsForCompletion
}

func boolPtr(v bool) *bool {
	return &v
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func applyReasoningSettings(req *openai.ChatCompletionRequest, provider string, reasoning bool) {
	if provider == model.ProviderMistral || provider == model.ProviderCerebras || provider == model.ProviderNim {
		return //oh cool yeah.
	}
	thinkingType := "disabled"
	reasoningEffort := "none"
	if reasoning {
		thinkingType = "enabled"
		reasoningEffort = "medium"
	}

	req.ReasoningEffort = reasoningEffort
	req.Reasoning = &openai.ReasoningConfig{
		Enabled: &reasoning,
		Effort:  reasoningEffort,
	}
	req.Reasoning.Exclude = boolPtr(!reasoning)
	req.Thinking = &openai.ThinkingConfig{Type: thinkingType}
	req.ChatTemplateKwargs = map[string]any{"enable_thinking": reasoning}
	req.ProviderOptions = map[string]any{
		"zai": map[string]any{
			"thinking": req.Thinking,
		},
		"openai": map[string]any{
			"reasoningEffort": reasoningEffort,
		},
		"deepseek": map[string]any{
			"thinking": req.Thinking,
		},
	}
}

func imageFilename(imageURL string) string {
	parsed, err := url.Parse(imageURL)
	if err == nil {
		if filename := filepath.Base(parsed.Path); filename != "." && filename != "/" && filename != "" {
			return filename
		}
	}
	if idx := strings.LastIndex(imageURL, "/"); idx != -1 {
		filename := imageURL[idx+1:]
		if qIdx := strings.Index(filename, "?"); qIdx != -1 {
			filename = filename[:qIdx]
		}
		if filename != "" {
			return filename
		}
	}
	return "image.png"
}

type Message struct {
	Role      string       `json:"role"`
	Content   string       `json:"content"`
	Images    []string     `json:"images"` // image URIs or base64
	ID        snowflake.ID `json:"-"`
	Author    string       `json:"author,omitempty"`
	Timestamp time.Time    `json:"timestamp,omitempty"`
	MessageID string       `json:"message_id,omitempty"`
}

type Usage struct {
	PromptTokens   int `json:"prompt_tokens"`
	ResponseTokens int `json:"response_tokens"`
	TotalTokens    int `json:"total_tokens"`
}

func (u Usage) String() string {
	return fmt.Sprintf("Prompt: %d, Response: %d, Total: %d", u.PromptTokens, u.ResponseTokens, u.TotalTokens)
}

func (lhs Usage) Add(rhs Usage) Usage {
	return Usage{
		PromptTokens:   lhs.PromptTokens + rhs.PromptTokens,
		ResponseTokens: lhs.ResponseTokens + rhs.ResponseTokens,
		TotalTokens:    lhs.TotalTokens + rhs.TotalTokens,
	}
}

func (u Usage) WithTotal() Usage {
	u.TotalTokens = u.PromptTokens + u.ResponseTokens
	return u
}

func (u Usage) IsEmpty() bool {
	return u.PromptTokens == 0 && u.ResponseTokens == 0 && u.TotalTokens == 0
}

type Llmer struct {
	Messages              []Message                                                                              `json:"messages"`
	ChannelID             snowflake.ID                                                                           `json:"channel_id"`
	ConversationID        string                                                                                 `json:"conversation_id,omitempty"`
	GuildID               *snowflake.ID                                                                          `json:"guild_id,omitempty"`
	ToolsEnabled          *bool                                                                                  `json:"tools_enabled,omitempty"`
	DiscordSearchCallback func(ctx context.Context, guildID snowflake.ID, query string) (string, map[int]string) `json:"-"`
}

func NewLlmer(channelID snowflake.ID) *Llmer {
	return &Llmer{
		ChannelID:      channelID,
		ConversationID: channelID.String(),
	}
}

func NewLlmerForKey(conversationID string) *Llmer {
	return &Llmer{
		ConversationID: conversationID,
	}
}

func (l *Llmer) SetGuildID(guildID *snowflake.ID) {
	l.GuildID = guildID
}

func (l *Llmer) SetDiscordSearchCallback(callback func(ctx context.Context, guildID snowflake.ID, query string) (string, map[int]string)) {
	l.DiscordSearchCallback = callback
}

func (l *Llmer) NumMessages() int {
	return len(l.Messages)
}

const (
	contextOvershootMin = 64
	contextOvershootMax = 100
	contextMessageMax   = 400
)

func ContextHardMessageLimit(softLimit int) int {
	if softLimit <= 0 {
		return 0
	}
	overshoot := softLimit
	if overshoot < contextOvershootMin {
		overshoot = contextOvershootMin
	}
	if overshoot > contextOvershootMax {
		overshoot = contextOvershootMax
	}
	hardLimit := softLimit + overshoot
	if hardLimit > contextMessageMax {
		hardLimit = contextMessageMax
	}
	return hardLimit
}

func (l *Llmer) NonSystemMessageCount() int {
	count := 0
	for _, msg := range l.Messages {
		if msg.Role != RoleSystem {
			count++
		}
	}
	return count
}

func (l *Llmer) TrimNonSystemMessages(keep int) bool {
	if keep < 0 {
		keep = 0
	}
	if l.NonSystemMessageCount() <= keep {
		return false
	}

	newMessages := make([]Message, 0, min(len(l.Messages), keep+1))
	if len(l.Messages) > 0 && l.Messages[0].Role == RoleSystem {
		newMessages = append(newMessages, l.Messages[0])
	}

	toKeep := make([]Message, 0, keep)
	for i := len(l.Messages) - 1; i >= 0 && len(toKeep) < keep; i-- {
		if l.Messages[i].Role == RoleSystem {
			continue
		}
		toKeep = append(toKeep, l.Messages[i])
	}
	for i := len(toKeep) - 1; i >= 0; i-- {
		newMessages = append(newMessages, toKeep[i])
	}

	l.Messages = newMessages
	return true
}

func (l *Llmer) TrimCacheFriendlyContext(softLimit int) bool {
	hardLimit := ContextHardMessageLimit(softLimit)
	if hardLimit <= 0 || l.NonSystemMessageCount() <= hardLimit {
		return false
	}
	return l.TrimNonSystemMessages(softLimit)
}

func IsContextLengthError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context") && strings.Contains(msg, "length") ||
		strings.Contains(msg, "context_length_exceeded") ||
		strings.Contains(msg, "maximum context") ||
		strings.Contains(msg, "too many tokens") ||
		strings.Contains(msg, "token limit") ||
		strings.Contains(msg, "tokens exceed")
}

// LobotomizeKeepLast removes messages in a way that the last n messages and the system prompt are kept
func (l *Llmer) LobotomizeKeepLast(n int) {
	numMessages := len(l.Messages)
	if numMessages == 0 {
		return
	}
	if n < 0 {
		n = 0
	}
	hasSystemPrompt := l.Messages[0].Role == RoleSystem
	startIndex := 0
	if hasSystemPrompt {
		startIndex = 1
	}
	numNonSystemMessages := numMessages - startIndex
	if n >= numNonSystemMessages {
		return
	}
	keepStartIndex := numMessages - n

	var newMessages []Message
	if hasSystemPrompt {
		newMessages = append(newMessages, l.Messages[0])
	}
	newMessages = append(newMessages, l.Messages[keepStartIndex:]...)

	l.Messages = newMessages
}

func (l *Llmer) Lobotomize(removeN int) {
	if len(l.Messages) == 0 {
		return
	}

	startIdx := 0
	if l.Messages[0].Role == RoleSystem {
		startIdx = 1
	}

	if removeN > 0 {
		endIdx := max(len(l.Messages)-removeN, startIdx)
		if startIdx == 0 {
			l.Messages = l.Messages[:endIdx]
			return
		}
		newMessages := make([]Message, 0, endIdx-startIdx+1)
		newMessages = append(newMessages, l.Messages[0])
		newMessages = append(newMessages, l.Messages[startIdx:endIdx]...)
		l.Messages = newMessages
	} else {
		// if amount <= 0, remove all messages except the system prompt
		l.Messages = l.Messages[:startIdx]
	}
}

// this is inclusive!
func (l *Llmer) LobotomizeUntilID(id snowflake.ID) {
	l.LobotomizeUntilMessageID(id.String())
}

func (l *Llmer) LobotomizeUntilMessageID(messageID string) {
	if len(l.Messages) == 0 {
		return
	}

	for i := len(l.Messages) - 1; i >= 0; i-- {
		if l.Messages[i].MessageID == messageID || l.Messages[i].ID.String() == messageID {
			if l.Messages[i].Role == RoleSystem {
				continue // don't nuke the system prompt
			}
			l.Messages = l.Messages[:i]
			return
		}
	}
}

func (l *Llmer) AddMessage(role, content string, id snowflake.ID) {
	l.AddMessageWithID(role, content, id, id.String())
}

func (l *Llmer) AddMessageWithID(role, content string, id snowflake.ID, messageID string) {
	if len(l.Messages) > 0 && role == RoleAssistant && l.Messages[len(l.Messages)-1].Role == RoleAssistant {
		// previous message is also an assistant message, merge this
		// (this is required when x3 splits the message up into multiple parts to bypass
		// discord's 2000 character message limit)
		l.Messages[len(l.Messages)-1].Content += content
		return
	}

	msg := Message{
		Role:      role,
		Content:   content,
		ID:        id,
		MessageID: messageID,
	}
	l.Messages = append(l.Messages, msg)
}

func (l *Llmer) SetPersona(persona persona.Persona, punishExcessiveSplit *bool) {
	l.ToolsEnabled = persona.Tools

	for i := range l.Messages {
		if l.Messages[i].Role == RoleUser {
			l.Messages[i].Content = strings.TrimPrefix(l.Messages[i].Content, SplitWarningPrefix)
		}
	}

	// remove system prompt if there is one
	if len(l.Messages) > 0 && l.Messages[0].Role == RoleSystem {
		l.Messages = l.Messages[1:]
	}

	if len(persona.System) == 0 {
		return
	}

	// add new system prompt as the first message
	l.Messages = append([]Message{{
		Role:    RoleSystem,
		Content: persona.System,
	}}, l.Messages...)

	/*
		if punishExcessiveSplit != nil && *punishExcessiveSplit {
			*punishExcessiveSplit = false
			if len(l.Messages) > 0 {
				lastMsg := &l.Messages[len(l.Messages)-1]
				if lastMsg.Role == RoleUser {
					lastMsg.Content = SplitWarningPrefix + lastMsg.Content
				}
			}
		}
	*/
}

// Add an image by URL to the latest message.
func (l *Llmer) AddImage(imageURL string) {
	if len(l.Messages) == 0 {
		return
	}
	msg := &l.Messages[len(l.Messages)-1]
	if msg.Role != RoleUser {
		return // some apis crash out when assistants have images
	}
	msg.Images = append(msg.Images, imageURL)
}

func (l Llmer) convertMessages(hasVision, supportsImageURL bool, prepend, searchResults string, ctx context.Context) []openai.ChatCompletionMessage {
	// find the index of the last message with images
	imageIdx := -1
	for i := len(l.Messages) - 1; i >= 0; i-- {
		if len(l.Messages[i].Images) > 0 {
			imageIdx = i
			break
		}
	}

	if imageIdx != -1 && len(l.Messages)-imageIdx >= 3 {
		// older than 3 messages, we can probably let it go
		imageIdx = -1
	}

	var messages []openai.ChatCompletionMessage
	for i, msg := range l.Messages {
		if msg.Content == "" && len(msg.Images) == 0 {
			continue // skip empty messages. HACK: they seem to appear after lobotomy, this is a hack
		}
		if len(msg.Images) == 0 || i != imageIdx || !hasVision {
			var content strings.Builder
			content.WriteString(msg.Content)

			// If this message has images but we don't have vision, generate/use descriptions
			if len(msg.Images) > 0 && !hasVision && i == imageIdx {
				for _, imageURL := range msg.Images {
					description, err := generateImageDescriptionCallback(imageURL, ctx)
					if err != nil {
						slog.Warn("failed to generate image description", "err", err, "url", imageURL)
						continue
					}
					if description != "" {
						fmt.Fprintf(&content, "\n[attached %s: %s]", imageFilename(imageURL), description)
					}
				}
			}

			role := msg.Role
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    role,
				Content: content.String(),
			})
		} else {
			imageURL := msg.Images[0]
			data := gImageCache.MemoizedImageBase64(imageURL)
			if data == "" {
				var content strings.Builder
				content.WriteString(msg.Content)
				fmt.Fprintf(&content, "\n<failed to fetch image `%s`>", imageURL)
				messages = append(messages, openai.ChatCompletionMessage{
					Role:    msg.Role,
					Content: content.String(),
				})
				continue
			}

			// must structure as a multipart message if we have images
			parts := []openai.ChatMessagePart{
				{
					Type: openai.ChatMessagePartTypeText,
					Text: msg.Content,
				},
			}
			/*
				for _, imageURL := range msg.Images {
					parts = append(parts, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL: imageURL,
						},
					})
				}
			*/
			// NB: most apis seem to only support one image sadly
			// we choose the first attachment
			if supportsImageURL {
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{
						URL: imageURL,
					},
				})
			} else { // api needs base64, will fetch image and store in memory cache
				parts = append(parts, openai.ChatMessagePart{
					Type: openai.ChatMessagePartTypeImageURL,
					ImageURL: &openai.ChatMessageImageURL{
						URL: data,
					},
				})
			}

			messages = append(messages, openai.ChatCompletionMessage{
				Role:         msg.Role,
				MultiContent: parts,
			})
		}
	}

	if searchResults != "" && len(messages) > 0 {
		messages[len(messages)-1].Content += searchResults
	}

	if prepend != "" {
		// https://console.groq.com/docs/prefilling
		messages = append(messages, openai.ChatCompletionMessage{
			Role:    RoleAssistant,
			Content: prepend,
		})
	}

	return messages
}

func estimateCacheKey(prefix string, m model.Model, writePayload func(hash.Hash64)) string {
	h := fnv.New64a()
	_, _ = h.Write([]byte(prefix))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(m.Name))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(string(m.Encoding)))
	_, _ = h.Write([]byte{0})
	writePayload(h)
	return prefix + ":" + strconv.FormatUint(h.Sum64(), 16)
}

func tokenCountCacheKey(m model.Model, s string) string {
	return estimateCacheKey("tok", m, func(h hash.Hash64) {
		_, _ = h.Write([]byte(s))
	})
}

func chatMessageTokensCacheKey(m model.Model, msg openai.ChatCompletionMessage) string {
	return estimateCacheKey("msg", m, func(h hash.Hash64) {
		writeChatMessageSignature(h, msg)
	})
}

func chatPromptTokensCacheKey(m model.Model, messages []openai.ChatCompletionMessage) string {
	return estimateCacheKey("prompt", m, func(h hash.Hash64) {
		for i := range messages {
			writeChatMessageSignature(h, messages[i])
			_, _ = h.Write([]byte{0xff})
		}
	})
}

func writeChatMessageSignature(h hash.Hash64, msg openai.ChatCompletionMessage) {
	_, _ = h.Write([]byte(msg.Role))
	_, _ = h.Write([]byte{0})
	_, _ = h.Write([]byte(msg.Content))
	_, _ = h.Write([]byte{0})
	for i := range msg.MultiContent {
		_, _ = h.Write([]byte(msg.MultiContent[i].Type))
		_, _ = h.Write([]byte{0})
		_, _ = h.Write([]byte(msg.MultiContent[i].Text))
		_, _ = h.Write([]byte{0xfe})
	}
}

func tokenCount(m model.Model, s string) int {
	if s == "" {
		return 0
	}
	key := tokenCountCacheKey(m, s)
	if cached, ok := gEstimateCache.get(key); ok {
		return cached
	}
	count, err := m.Tokenizer().Count(s)
	if err == nil {
		gEstimateCache.set(key, count)
		return count
	}
	ids, _, err := m.Tokenizer().Encode(s)
	if err != nil {
		return 0
	}
	count = len(ids)
	gEstimateCache.set(key, count)
	return count
}

func estimateChatMessageTokens(m model.Model, msg openai.ChatCompletionMessage) int {
	key := chatMessageTokensCacheKey(m, msg)
	if cached, ok := gEstimateCache.get(key); ok {
		return cached
	}
	tokens := tokenCount(m, msg.Content)
	for _, part := range msg.MultiContent {
		if part.Type == openai.ChatMessagePartTypeText {
			tokens += tokenCount(m, part.Text)
		}
	}
	gEstimateCache.set(key, tokens)
	return tokens
}

func estimateChatPromptTokens(m model.Model, messages []openai.ChatCompletionMessage) int {
	key := chatPromptTokensCacheKey(m, messages)
	if cached, ok := gEstimateCache.get(key); ok {
		return cached
	}
	tokens := 0
	for _, msg := range messages {
		tokens += estimateChatMessageTokens(m, msg)
	}
	gEstimateCache.set(key, tokens)
	return tokens
}

func trimMessagesForCerebras(m model.Model, messages []openai.ChatCompletionMessage, targetTokens int) []openai.ChatCompletionMessage {
	if len(messages) == 0 || targetTokens <= 0 {
		return messages
	}
	if estimateChatPromptTokens(m, messages) <= targetTokens {
		return messages
	}

	trimmed := cloneChatMessages(messages)

	for len(trimmed) > 1 && estimateChatPromptTokens(m, trimmed) > targetTokens {
		idx := oldestDroppableMessageIndex(trimmed)
		if idx < 0 {
			break
		}
		trimmed = append(trimmed[:idx], trimmed[idx+1:]...)
	}

	for estimateChatPromptTokens(m, trimmed) > targetTokens {
		idx := bestMessageToTruncateIndex(m, trimmed)
		if idx < 0 {
			break
		}
		next, changed := truncateChatMessageToReduceTokens(m, trimmed[idx], estimateChatPromptTokens(m, trimmed)-targetTokens, idx == len(trimmed)-1)
		if !changed {
			break
		}
		trimmed[idx] = next
	}

	return trimmed
}

func cloneChatMessages(messages []openai.ChatCompletionMessage) []openai.ChatCompletionMessage {
	cloned := make([]openai.ChatCompletionMessage, len(messages))
	for i := range messages {
		cloned[i] = messages[i]
		if len(messages[i].MultiContent) > 0 {
			cloned[i].MultiContent = append([]openai.ChatMessagePart(nil), messages[i].MultiContent...)
		}
		if len(messages[i].ToolCalls) > 0 {
			cloned[i].ToolCalls = append([]openai.ToolCall(nil), messages[i].ToolCalls...)
		}
	}
	return cloned
}

func oldestDroppableMessageIndex(messages []openai.ChatCompletionMessage) int {
	lastIdx := len(messages) - 1
	for i := 0; i < len(messages); i++ {
		if i == lastIdx {
			continue
		}
		if messages[i].Role == RoleSystem || messages[i].Role == openai.ChatMessageRoleTool {
			continue
		}
		return i
	}
	return -1
}

func bestMessageToTruncateIndex(m model.Model, messages []openai.ChatCompletionMessage) int {
	bestIdx := -1
	bestScore := 0
	for i := range messages {
		if !messageHasTrimmableText(messages[i]) {
			continue
		}
		score := estimateChatMessageTokens(m, messages[i])
		if score <= 0 {
			continue
		}
		if i == len(messages)-1 {
			score -= 64
		}
		if messages[i].Role == RoleSystem {
			score -= 32
		}
		if bestIdx < 0 || score > bestScore {
			bestIdx = i
			bestScore = score
		}
	}
	return bestIdx
}

func messageHasTrimmableText(msg openai.ChatCompletionMessage) bool {
	if strings.TrimSpace(msg.Content) != "" {
		return true
	}
	for _, part := range msg.MultiContent {
		if part.Type == openai.ChatMessagePartTypeText && strings.TrimSpace(part.Text) != "" {
			return true
		}
	}
	return false
}

func truncateChatMessageToReduceTokens(m model.Model, msg openai.ChatCompletionMessage, excessTokens int, preserveTail bool) (openai.ChatCompletionMessage, bool) {
	currentTokens := estimateChatMessageTokens(m, msg)
	if currentTokens <= 0 {
		return msg, false
	}
	targetTokens := currentTokens - excessTokens - 32
	if targetTokens < 64 {
		targetTokens = 64
	}
	if targetTokens >= currentTokens {
		targetTokens = currentTokens - 1
	}
	if targetTokens <= 0 {
		return msg, false
	}

	apply := func(maxRunes int) openai.ChatCompletionMessage {
		next := msg
		if next.Content != "" {
			next.Content = clipStringForCerebras(next.Content, maxRunes, preserveTail)
		}
		if len(next.MultiContent) > 0 {
			for i := range next.MultiContent {
				if next.MultiContent[i].Type == openai.ChatMessagePartTypeText {
					next.MultiContent[i].Text = clipStringForCerebras(next.MultiContent[i].Text, maxRunes, preserveTail)
				}
			}
		}
		return next
	}

	totalRunes := messageTextRuneCount(msg)
	if totalRunes <= 1 {
		return msg, false
	}

	low, high := 1, totalRunes
	best := msg
	changed := false
	for low <= high {
		mid := (low + high) / 2
		candidate := apply(mid)
		candidateTokens := estimateChatMessageTokens(m, candidate)
		if candidateTokens <= targetTokens {
			best = candidate
			changed = !chatMessagesEqual(msg, candidate)
			low = mid + 1
		} else {
			high = mid - 1
		}
	}

	if !changed {
		candidate := apply(max(1, totalRunes/2))
		if chatMessagesEqual(msg, candidate) {
			return msg, false
		}
		return candidate, true
	}
	return best, true
}

func messageTextRuneCount(msg openai.ChatCompletionMessage) int {
	total := utf8.RuneCountInString(msg.Content)
	for _, part := range msg.MultiContent {
		if part.Type == openai.ChatMessagePartTypeText {
			total += utf8.RuneCountInString(part.Text)
		}
	}
	return total
}

func clipStringForCerebras(s string, maxRunes int, preserveTail bool) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	if preserveTail {
		return string(runes[len(runes)-maxRunes:])
	}
	return string(runes[:maxRunes])
}

func chatMessagesEqual(a, b openai.ChatCompletionMessage) bool {
	if a.Role != b.Role || a.Content != b.Content || len(a.MultiContent) != len(b.MultiContent) {
		return false
	}
	for i := range a.MultiContent {
		if a.MultiContent[i].Type != b.MultiContent[i].Type || a.MultiContent[i].Text != b.MultiContent[i].Text {
			return false
		}
	}
	return true
}

func estimateCompletionUsage(m model.Model, messages []openai.ChatCompletionMessage, response string) Usage {
	start := time.Now()
	usage := Usage{
		PromptTokens:   estimateChatPromptTokens(m, messages),
		ResponseTokens: tokenCount(m, response),
	}.WithTotal()
	slog.Debug("estimated completion token usage", slog.String("usage", usage.String()), slog.Duration("in", time.Since(start)))
	return usage
}

func completionTextForUsage(reasoning, text string) string {
	reasoning = strings.TrimSpace(reasoning)
	text = strings.TrimSpace(text)
	if reasoning == "" {
		return text
	}
	if text == "" {
		return reasoning
	}
	return reasoning + "\n" + text
}

func (l Llmer) estimateUsage(m model.Model) Usage {
	start := time.Now()
	var usage Usage
	responseIdx := -1
	numImages := 0
	for i := len(l.Messages) - 1; i >= 0; i-- {
		if l.Messages[i].Role == RoleAssistant {
			responseIdx = i
			break
		}
	}

	for i, msg := range l.Messages {
		if i == responseIdx {
			usage.ResponseTokens = tokenCount(m, msg.Content)
		} else {
			usage.PromptTokens += tokenCount(m, msg.Content)
		}
		if len(msg.Images) > 0 {
			numImages += len(msg.Images)
		}
	}

	usage = usage.WithTotal()
	slog.Debug("estimated token usage", slog.String("usage", usage.String()), slog.Duration("in", time.Since(start)), slog.Int("images", numImages))
	return usage
}

var weirdEndRegexp = regexp.MustCompile(`(>[\./w]+)$`)

const (
	toolNameWebSearch     = "web_search"
	toolNameDiscordSearch = "discord_search"
)

var webSearchTool = openai.Tool{
	Type: openai.ToolTypeFunction,
	Function: &openai.FunctionDefinition{
		Name:        toolNameWebSearch,
		Description: "Search the web for current or external information. Use this for web results. Returns numbered sources that can be cited as [1], [2], etc.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The web search query.",
				},
			},
			"additionalProperties": false,
		},
	},
}

var discordSearchTool = openai.Tool{
	Type: openai.ToolTypeFunction,
	Function: &openai.FunctionDefinition{
		Name:        toolNameDiscordSearch,
		Description: "Search messages in the current Discord server. Use this when the answer is likely in this server's chat history. Supports filters like from:, in:#channel, in:channel_id, has:image, mentions:, before:, after:, pinned:, and page:. If the user asks how many messages someone sent, use a from: filter and answer from the total count.",
		Parameters: map[string]any{
			"type":     "object",
			"required": []string{"query"},
			"properties": map[string]any{
				"query": map[string]any{
					"type":        "string",
					"description": "The Discord server search query.",
				},
			},
			"additionalProperties": false,
		},
	},
}

type toolArguments struct {
	Query string `json:"query"`
}

func modelUsesNativeToolCalling(m model.Model, provider string) bool {
	_, ok := m.Providers[provider]
	return ok && model.ProviderUsesNativeToolCalling(provider)
}

func (l Llmer) searchToolsEnabled() bool {
	return l.ToolsEnabled == nil || *l.ToolsEnabled
}

func (l Llmer) availableSearchTools() []openai.Tool {
	if !l.searchToolsEnabled() {
		return nil
	}

	tools := []openai.Tool{webSearchTool}
	if l.GuildID != nil && l.DiscordSearchCallback != nil {
		tools = append(tools, discordSearchTool)
	}
	return tools
}

func toolCallID(call openai.ToolCall, index int) string {
	if call.ID != "" {
		return call.ID
	}
	return fmt.Sprintf("call_%d", index)
}

func parseToolQuery(arguments string) string {
	arguments = strings.TrimSpace(arguments)
	var args toolArguments
	if err := json.Unmarshal([]byte(arguments), &args); err == nil && strings.TrimSpace(args.Query) != "" {
		return strings.TrimSpace(args.Query)
	}
	var obj map[string]any
	if err := json.Unmarshal([]byte(arguments), &obj); err == nil {
		for _, key := range []string{"query", "q", "search", "search_query", "content"} {
			if value, ok := obj[key].(string); ok && strings.TrimSpace(value) != "" {
				return strings.TrimSpace(value)
			}
		}
		var onlyString string
		stringFields := 0
		for _, value := range obj {
			if s, ok := value.(string); ok && strings.TrimSpace(s) != "" {
				onlyString = strings.TrimSpace(s)
				stringFields++
			}
		}
		if stringFields == 1 {
			return onlyString
		}
	}
	var raw string
	if err := json.Unmarshal([]byte(arguments), &raw); err == nil {
		return strings.TrimSpace(raw)
	}
	return arguments
}

func (l *Llmer) executeSearchTool(ctx context.Context, name, query string, searchCitemap map[int]string) (string, map[int]string) {
	query = strings.TrimSpace(query)
	var results string
	var citemap map[int]string
	switch name {
	case toolNameWebSearch:
		results, citemap = getSearchResults(query)
	case toolNameDiscordSearch:
		results, citemap = l.getDiscordSearchResults(ctx, query)
	default:
		return fmt.Sprintf("<unknown tool %q>", name), searchCitemap
	}

	results, citemap = remapSearchCites(results, citemap, maxCiteID(searchCitemap))
	return results, mergeCitemaps(searchCitemap, citemap)
}

func (l *Llmer) requestCompletionInternal2(
	m model.Model,
	codename,
	provider string,
	settings persona.InferenceSettings,
	client *openai.Client,
	prepend string,
	ctx context.Context,
	searchDepth int,
	searchCitemap map[int]string,
	searchResults string,
) (string, Usage, error) {
	if m.Limited {
		settings = persona.InferenceSettings{}
	}
	req := openai.ChatCompletionRequest{
		Model:       codename,
		Messages:    l.convertMessages(m.Vision, provider != model.ProviderOllama && provider != model.ProviderCloudflare && provider != model.ProviderCerebras, prepend, searchResults, ctx), // ollama cloud doesn't support fetching from image URLs, how nice :)
		Temperature: settings.Temperature,
		TopP:        settings.TopP,
		// MinP anyone?
		FrequencyPenalty: settings.FrequencyPenalty,
		Seed:             settings.Seed,
		Private:          provider == model.ProviderPollinations,
	}
	if m.Reasoning {
		applyReasoningSettings(&req, provider, settings.Reasoning)
	}
	nativeSearchTools := l.availableSearchTools()
	nativeToolCalling := modelUsesNativeToolCalling(m, provider) && len(nativeSearchTools) > 0
	if nativeToolCalling {
		req.Tools = nativeSearchTools
		req.ToolChoice = "auto"
		for i := range req.Messages {
			if req.Messages[i].Role == RoleSystem {
				req.Messages[i].Content = persona.StripLegacySearchSystemPrompt(req.Messages[i].Content)
				break
			}
		}
	}

	completionStart := time.Now()
	ctx, cancel := context.WithDeadline(ctx, completionStart.Add(5*time.Minute))
	defer cancel()

	var message openai.ChatCompletionMessage
	var response openai.ChatCompletionResponse
	var usage Usage
	for toolDepth := 0; ; toolDepth++ {
		if provider == model.ProviderCerebras {
			beforeTokens := estimateChatPromptTokens(m, req.Messages)
			req.Messages = trimMessagesForCerebras(m, req.Messages, providerPromptTrimTarget)
			afterTokens := estimateChatPromptTokens(m, req.Messages)
			if afterTokens < beforeTokens {
				slog.Warn(
					"trimmed messages for cerebras before request",
					"model", m.Name,
					"codename", codename,
					"provider", provider,
					"beforeTokens", beforeTokens,
					"afterTokens", afterTokens,
					"messages", len(req.Messages),
				)
			}
		}
		var err error
		response, err = client.CreateChatCompletion(ctx, req)
		if err != nil {
			return "", Usage{}, err
		}

		if len(response.Choices) == 0 {
			return "", Usage{}, errors.New("completion response had no choices")
		}

		message = response.Choices[0].Message
		text := message.Content
		reasoning := ""
		if settings.Reasoning {
			reasoning = strings.TrimSpace(firstNonEmpty(message.ReasoningContent, message.Reasoning))
		}
		currentUsage := Usage{
			PromptTokens:   response.Usage.PromptTokens,
			ResponseTokens: response.Usage.CompletionTokens,
			TotalTokens:    response.Usage.TotalTokens,
		}
		estimatedUsage := estimateCompletionUsage(m, req.Messages, completionTextForUsage(reasoning, text))
		if currentUsage.IsEmpty() {
			currentUsage = estimatedUsage
		} else {
			if currentUsage.PromptTokens <= 0 {
				currentUsage.PromptTokens = estimatedUsage.PromptTokens
			}
			if currentUsage.ResponseTokens <= 1 {
				currentUsage.ResponseTokens = estimatedUsage.ResponseTokens
			}
			if currentUsage.TotalTokens <= 0 || currentUsage.TotalTokens < currentUsage.PromptTokens+currentUsage.ResponseTokens {
				currentUsage = currentUsage.WithTotal()
			}
		}
		usage = usage.Add(currentUsage)

		if !nativeToolCalling || len(message.ToolCalls) == 0 {
			break
		}
		if toolDepth >= 3 {
			slog.Warn("native tool call depth limit reached", "model", m.Name, "provider", provider)
			break
		}

		toolCalls := append([]openai.ToolCall(nil), message.ToolCalls...)
		for i := range toolCalls {
			toolCalls[i].ID = toolCallID(toolCalls[i], i)
		}

		req.Messages = append(req.Messages, openai.ChatCompletionMessage{
			Role:      RoleAssistant,
			Content:   message.Content,
			ToolCalls: toolCalls,
		})
		req.Tools = nil
		req.ToolChoice = nil
		nativeToolCalling = false
		for i, call := range toolCalls {
			query := parseToolQuery(call.Function.Arguments)
			slog.Info("executing native tool call", "tool", call.Function.Name, "query", query, "arguments", call.Function.Arguments)
			results, updatedCitemap := l.executeSearchTool(ctx, call.Function.Name, query, searchCitemap)
			searchCitemap = updatedCitemap
			req.Messages = append(req.Messages, openai.ChatCompletionMessage{
				Role:       openai.ChatMessageRoleTool,
				Content:    results,
				ToolCallID: toolCallID(call, i),
			})
		}
	}

	text := message.Content
	reasoning := ""
	if settings.Reasoning {
		reasoning = strings.TrimSpace(firstNonEmpty(message.ReasoningContent, message.Reasoning))
	}

	elapsed := time.Since(completionStart)
	tokPerSec := 0.0
	if elapsed > 0 {
		tokPerSec = float64(usage.ResponseTokens) / elapsed.Seconds()
	}
	slog.Info("completion received", "duration", elapsed, "tok/s", tokPerSec, "has_reasoning", reasoning != "")

	unescaped := html.UnescapeString(text)
	unescaped = strings.TrimSpace(unescaped)
	unescaped = weirdEndRegexp.ReplaceAllString(unescaped, "$1<")

	if searchDepth < 4 && l.searchToolsEnabled() {
		if search := extractDiscordSearch(unescaped); search != "" {
			results, citemap := l.getDiscordSearchResults(ctx, search)
			nextRes, nextUsage, nextErr := l.requestCompletionInternal2(
				m, codename, provider, settings, client, prepend, ctx,
				searchDepth+1, citemap, results,
			)
			return nextRes, usage.Add(nextUsage), nextErr
		}
		if search := extractSearch(unescaped); search != "" {
			results, citemap := getSearchResults(search)
			nextRes, nextUsage, nextErr := l.requestCompletionInternal2(
				m, codename, provider, settings, client, prepend, ctx,
				searchDepth+1, citemap, results,
			)
			return nextRes, usage.Add(nextUsage), nextErr
		}
	}

	// cool
	unescaped = strings.ReplaceAll(unescaped, "<new_message]", "<new_message>")

	// cites like [1] get turned into [1](<https://google.com/>)
	if searchDepth > 0 || len(searchCitemap) > 0 {
		unescaped = formatCites(unescaped, searchCitemap)
	}

	// and trim spaces again after our checks, for good measure
	unescaped = strings.TrimSpace(unescaped)
	slog.Info("response", "len", len(unescaped), "duration", time.Since(completionStart), "model", m.Name, "provider", provider)

	l.Messages = append(l.Messages, Message{
		Role:    RoleAssistant,
		Content: unescaped,
	})

	display := unescaped
	if reasoning != "" {
		display = "<think>" + reasoning + "</think>\n" + display
	}

	return display, usage, nil
}

func (l *Llmer) requestCompletionInternal(
	m model.Model,
	provider string,
	settings persona.InferenceSettings,
	prepend string,
	ctx context.Context,
) (string, Usage, error) {
	slog.Debug(
		"request completion.. message history follows..",
		slog.String("model", m.Name),
		slog.String("provider", provider),
		slog.Float64("temperature", float64(settings.Temperature)),
		slog.Float64("top_p", float64(settings.TopP)),
		slog.Float64("frequency_penalty", float64(settings.FrequencyPenalty)),
	)
	for _, msg := range l.Messages {
		slog.Debug("    message", slog.String("role", msg.Role), slog.String("content", msg.Content), slog.Int("images", len(msg.Images)))
	}

	baseUrls, tokens, codenames := m.Client(provider)
	if len(baseUrls) == 0 || len(tokens) == 0 || len(codenames) == 0 {
		return "", Usage{}, fmt.Errorf("no valid client configurations for provider %s", provider)
	}

	var lastErr error
	for i, baseUrl := range baseUrls {
		tokensToTry := tokens

		if len(baseUrls) > 1 && len(baseUrls) == len(tokens) {
			if i < len(tokens) {
				tokensToTry = []string{tokens[i]}
			} else {
				continue
			}
		}

		for _, token := range tokensToTry {
			config := openai.DefaultConfig(token)
			config.BaseURL = baseUrl
			if provider == model.ProviderGithub { // for github we need an azure config, idfk why
				config = openai.DefaultAzureConfig(token, baseUrl)
				config.APIType = openai.APITypeOpenAI
			}
			client := openai.NewClientWithConfig(config)

			var tokenSucceeded bool
			for _, codename := range codenames {
				slog.Info("attempting request", "provider", provider, "baseUrl", baseUrl, "codename", codename)
				res, usage, err := l.requestCompletionInternal2(m, codename, provider, settings, client, prepend, ctx, 0, nil, "")
				if err == nil {
					tokenSucceeded = true
					// we got a response, but if we used a prefill, we should indicate that it was used
					// (prepend it to the response in bold)
					if prepend != "" {
						res = fmt.Sprintf("**%s** %s", strings.ReplaceAll(strings.TrimSpace(prepend), "**", ""), res)
					}
					return res, usage, nil
				}
				lastErr = err
				slog.Warn("request failed, trying next config", "provider", provider, "baseUrl", baseUrl, "codename", codename, "error", err)
			}
			if !tokenSucceeded {
				model.RecordTokenError(token)
			}
		}
	}

	return "", Usage{}, fmt.Errorf("all configurations for provider %s failed: %w", provider, lastErr) // all baseUrls/tokens/codenames errored
}

func (l *Llmer) getDiscordSearchResults(ctx context.Context, search string) (string, map[int]string) {
	citemap := make(map[int]string)
	search = strings.TrimSpace(search)
	if search == "" {
		return "<discord search query was empty>", citemap
	}
	if l.GuildID == nil {
		return "<discord search is only available in guild channels>", citemap
	}
	if l.DiscordSearchCallback == nil {
		return "<discord search is not configured for this bot>", citemap
	}
	return l.DiscordSearchCallback(ctx, *l.GuildID, search)
}

// removes `name:` prefix
func desugarContent(content string) string {
	_, after, found := strings.Cut(content, ": ")
	if found {
		return after
	}
	return content
}

func (l *Llmer) inferMarkovChain() string {
	if len(l.Messages) == 0 {
		return ""
	}

	chain := markov.NewChain(markovChainOrder)
	totalWords := 0
	for _, msg := range l.Messages {
		//if len(l.Messages) > 1 && msg.Role == RoleSystem {
		//	continue
		//}
		content := desugarContent(msg.Content)
		words := strings.Fields(content)
		if len(words) > 0 {
			chain.Add(words)
			totalWords += len(words)
		}
	}

	current := make(markov.NGram, markovChainOrder)
	for i := range current {
		current[i] = markov.StartToken
	}

	var sb strings.Builder

	for range markovMaxLength {
		nextToken, err := chain.Generate(current)

		if err != nil {
			slog.Error("markov generation error", slog.Any("error", err), "current_state", current)
			if errors.Is(err, markov.ErrUnknownNgramState) && sb.Len() > 0 {
				break
			}
			return ""
		}

		if nextToken == "" || nextToken == markov.EndToken {
			break
		}

		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		sb.WriteString(nextToken)

		current = append(current[1:], nextToken)
	}

	return strings.TrimSpace(sb.String())
}

func (l *Llmer) inferEliza() string {
	msg := l.Messages[len(l.Messages)-1]
	content := desugarContent(msg.Content)
	if anal, err := eliza.AnalyzeString(content); err == nil {
		return anal
	}
	return "IDK"
}

var weirdAliceReplacer = strings.NewReplacer(" .", ".", ", ,", ",", " ,", ",", " ?", "?")

func (l *Llmer) inferAlice() string {
	msg := l.Messages[len(l.Messages)-1]
	content := desugarContent(msg.Content)
	sessionID := l.ConversationID
	if sessionID == "" {
		sessionID = l.ChannelID.String()
	}
	response := gAlice.Respond(content, sessionID)
	return weirdAliceReplacer.Replace(strings.Join(strings.Fields(strings.TrimSpace(response)), " "))
}

// hasRecentUserImage returns true if any of the last 4 non-system messages had user images.
func (l Llmer) hasRecentUserImage() bool {
	seen := 0
	for i := len(l.Messages) - 1; i >= 0 && seen < 4; i-- {
		message := l.Messages[i]
		if message.Role == RoleSystem {
			continue
		}
		seen++
		if message.Role == RoleUser && len(message.Images) > 0 {
			return true
		}
	}
	return false
}

func (l Llmer) applyFallbackVisionModels(models []model.Model) []model.Model {
	if !l.hasRecentUserImage() {
		return models
	}

	swapped := false
	modelsToTry := make([]model.Model, 0, len(models))
	for _, m := range models {
		if m.FallbackVisionModel == "" {
			modelsToTry = append(modelsToTry, m)
			continue
		}
		if strings.EqualFold(m.FallbackVisionModel, "Default") {
			fallbacks := model.GetModelsByNames(model.DefaultVisionModels)
			validFallbacks := make([]model.Model, 0, len(fallbacks))
			for _, fallback := range fallbacks {
				if fallback.Vision {
					validFallbacks = append(validFallbacks, fallback)
				} else {
					slog.Warn("default vision model is not vision-capable; skipping", "model", fallback.Name)
				}
			}
			if len(validFallbacks) == 0 {
				slog.Warn("fallback vision model is Default, but no valid default vision models are configured", "model", m.Name)
				modelsToTry = append(modelsToTry, m)
				continue
			}
			slog.Info("recent image found; swapping to default vision models", "model", m.Name, "fallbacks", model.DefaultVisionModels)
			modelsToTry = append(modelsToTry, validFallbacks...)
			swapped = true
			continue
		}
		fallback := model.GetModelByName(m.FallbackVisionModel)
		if !fallback.Vision {
			slog.Warn("fallback vision model is not vision-capable; keeping original model", "model", m.Name, "fallback", m.FallbackVisionModel)
			modelsToTry = append(modelsToTry, m)
			continue
		}
		slog.Info("recent image found; swapping to fallback vision model", "model", m.Name, "fallback", fallback.Name)
		modelsToTry = append(modelsToTry, fallback)
		swapped = true
	}
	if !swapped {
		return models
	}
	return modelsToTry
}

func (l *Llmer) RequestCompletion(models []model.Model, settings persona.InferenceSettings, prepend string, ctx context.Context) (res string, usage Usage, err error) {
	if len(models) == 0 {
		err = errNoModelsForCompletion
		return
	}

	if models[0].IsMarkov {
		res = l.inferMarkovChain()
		usage = Usage{}
		err = nil
		return
	}
	if models[0].IsEliza {
		res = l.inferEliza()
		usage = Usage{}
		err = nil
		return
	}
	if models[0].IsAlice {
		res = l.inferAlice()
		usage = Usage{}
		err = nil
		return
	}

	settings.Remap() // remap values (1.0 temp -> 0.6 temp)

	modelsToTry := l.applyFallbackVisionModels(models)

	var lastErr error

	for _, m := range modelsToTry {
		if m.IsMarkov || m.IsEliza || m.IsAlice {
			continue
		}

		slog.Info("attempting completion with model", "model", m.Name)
		for _, provider := range model.ProvidersForModel(m, m.Reasoning && settings.Reasoning) {
			adaptiveFallbackTriggered := false
			for retries := 0; retries < 3; retries++ {
				slog.Info("requesting completion", "model", m.Name, "provider", provider.Name, "providerErrors", provider.Errors, "retries", retries)

				if ctx.Err() != nil {
					return "", Usage{}, ctx.Err()
				}

				requestStart := time.Now()
				res, usage, err = l.requestCompletionInternal(m, provider.Name, settings.Fixup(), prepend, ctx)
				requestDuration := time.Since(requestStart)

				// check for empty response first
				if err == nil && res == "" {
					slog.Warn("got an empty response from requestCompletionInternal", "model", m.Name, "provider", provider.Name)
					err = errors.New("empty response received")
				}

				if err != nil {
					// A cancelled caller is not evidence that this provider is unhealthy.
					adaptiveFallback := ctx.Err() == nil && model.RecordProviderFailure(m, provider.Name)
					slog.Warn("requestCompletionInternal failed", "model", m.Name, "provider", provider.Name, "error", err, "retries", retries, "adaptiveFallback", adaptiveFallback)
					lastErr = err
					provider.Errors++
					if adaptiveFallback {
						// A probe or temporary preferred provider only gets one request.
						// The next configured provider is the adaptive fallback.
						adaptiveFallbackTriggered = true
						break
					}
					continue
				}
				model.RecordProviderSuccess(m, provider.Name, requestDuration)

				if usage.IsEmpty() {
					usage = l.estimateUsage(m)
				} else if usage.ResponseTokens <= 1 {
					// unrealistic
					estimatedUsage := l.estimateUsage(m)
					usage.ResponseTokens = estimatedUsage.ResponseTokens
					usage = usage.WithTotal()
				} else if usage.TotalTokens <= 0 || usage.TotalTokens < usage.PromptTokens+usage.ResponseTokens {
					usage = usage.WithTotal()
				}

				slog.Info("request successful", "model", m.Name, "provider", provider.Name, "usage", usage.String())
				return res, usage, nil // return on success
			}
			if adaptiveFallbackTriggered {
				slog.Info("adaptive provider failed; trying fallback", "provider", provider.Name, "model", m.Name)
			} else {
				slog.Warn("max retries reached for provider", "provider", provider.Name, "model", m.Name)
			}
		}
		slog.Warn("all providers failed for model", "model", m.Name)
	}

	slog.Error("all models failed to provide a completion", "lastError", lastErr)
	err = fmt.Errorf("all models failed: %w", lastErr)
	return "", Usage{}, err
}
