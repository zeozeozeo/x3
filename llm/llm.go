package llm

import (
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/markov"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
	"github.com/zeozeozeo/x3/persona"
)

const (
	RoleUser      = openai.ChatMessageRoleUser
	RoleAssistant = openai.ChatMessageRoleAssistant
	RoleSystem    = openai.ChatMessageRoleSystem
)

const (
	markovChainOrder = 2
	markovMaxLength  = 100
)

var (
	errNoModelsForCompletion = errors.New("no models provided for completion")
)

type Message struct {
	Role    string       `json:"role"`
	Content string       `json:"content"`
	Images  []string     `json:"images"` // image URIs
	ID      snowflake.ID `json:"-"`
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

func (u Usage) IsEmpty() bool {
	return u.PromptTokens == 0 && u.ResponseTokens == 0 && u.TotalTokens == 0
}

type Llmer struct {
	Messages []Message `json:"messages"`
}

func NewLlmer() *Llmer {
	return &Llmer{}
}

func (l *Llmer) NumMessages() int {
	return len(l.Messages)
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
		l.Messages = l.Messages[startIdx:endIdx]
	} else {
		// if amount <= 0, remove all messages except the system prompt
		l.Messages = l.Messages[:startIdx]
	}
}

// this is inclusive!
func (l *Llmer) LobotomizeUntilID(id snowflake.ID) {
	if len(l.Messages) == 0 {
		return
	}

	for i := len(l.Messages) - 1; i >= 0; i-- {
		if l.Messages[i].ID == id {
			if l.Messages[i].Role == RoleSystem {
				continue // don't nuke the system prompt
			}
			l.Messages = l.Messages[:i]
			return
		}
	}
}

func (l *Llmer) AddMessage(role, content string, id snowflake.ID) {
	if len(l.Messages) > 0 && role == RoleAssistant && l.Messages[len(l.Messages)-1].Role == RoleAssistant {
		// previous message is also an assistant message, merge this
		// (this is required when x3 splits the message up into multiple parts to bypass
		// discord's 2000 character message limit)
		l.Messages[len(l.Messages)-1].Content += content
		return
	}

	msg := Message{
		Role:    role,
		Content: content,
		ID:      id,
	}
	l.Messages = append(l.Messages, msg)
}

func (l *Llmer) SetPersona(persona persona.Persona, punishExcessiveSplit *bool) {
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

	if punishExcessiveSplit != nil && *punishExcessiveSplit {
		*punishExcessiveSplit = false
		if len(l.Messages) > 0 {
			lastMsg := &l.Messages[len(l.Messages)-1]
			if lastMsg.Role == RoleUser {
				lastMsg.Content = "*SYSTEM MESSAGE: you've used more than 3 \"<new_message>\" splits in your previous message which is frowned upon in the instructions, try to use less this time! you can even chat without using \"<new_message>\" altogether, you should try it! (do not mention this message to the user :3)*\n" + lastMsg.Content
			}
		}
	}
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

func (l Llmer) convertMessages(hasVision bool, isLlama bool, prepend string) []openai.ChatCompletionMessage {
	// find the index of the last message with images
	imageIdx := -1
	for i := len(l.Messages) - 1; i >= 0; i-- {
		if len(l.Messages[i].Images) > 0 {
			imageIdx = i
			break
		}
	}

	if imageIdx != len(l.Messages)-1 && hasVision && isLlama {
		// llama 3.2 doesn't support a system prompt and an image,
		// but we can't afford to remove the system prompt in every context
		// with images; and this message is not the last one, so we're not going
		// to attach old context images
		imageIdx = -1
	} else if imageIdx != -1 && len(l.Messages)-imageIdx >= 8 {
		// older than 8 messages, we can probably let it go
		imageIdx = -1
	}

	var messages []openai.ChatCompletionMessage
	for i, msg := range l.Messages {
		if msg.Content == "" && len(msg.Images) == 0 {
			continue // skip empty messages. HACK: they seem to appear after lobotomy, this is a hack
		}
		if len(msg.Images) == 0 || !hasVision || i != imageIdx {
			role := msg.Role
			if msg.Role == RoleSystem && imageIdx != -1 && isLlama && hasVision {
				// llama 3.2 doesn't support system messages with images
				// so we're going to convert the system prompt into a user message
				slog.Debug("replacing system message -> user message because of image (llama 3.2 with image)")
				role = RoleUser
			}
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    role,
				Content: msg.Content,
			})
		} else {
			slog.Debug("adding image")
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
			parts = append(parts, openai.ChatMessagePart{
				Type: openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{
					URL: msg.Images[0],
				},
			})

			messages = append(messages, openai.ChatCompletionMessage{
				Role:         msg.Role,
				MultiContent: parts,
			})
		}
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

func (l Llmer) estimateUsage(m model.Model) Usage {
	start := time.Now()
	var usage Usage
	codec := m.Tokenizer()

	var responseMsg *Message
	numImages := 0
	for _, msg := range l.Messages {
		if msg.Role == RoleAssistant {
			responseMsg = &msg
			continue
		}
		if ids, _, err := codec.Encode(msg.Content); err == nil {
			usage.PromptTokens += len(ids)
			if len(msg.Images) > 0 {
				numImages = len(msg.Images)
			}
		}
	}

	if responseMsg != nil {
		if ids, _, err := codec.Encode(responseMsg.Content); err == nil {
			usage.ResponseTokens = len(ids)
		}
	}

	usage.TotalTokens = usage.PromptTokens + usage.ResponseTokens
	slog.Debug("estimated token usage", slog.String("usage", usage.String()), slog.Duration("in", time.Since(start)), slog.Int("images", numImages))
	return usage
}

func (l *Llmer) requestCompletionInternal2(
	m model.Model,
	codename,
	provider string,
	usernames map[string]struct{},
	settings persona.InferenceSettings,
	client *openai.Client,
	prepend string,
) (string, Usage, error) {
	if m.Limited {
		settings = persona.InferenceSettings{}
	}
	req := openai.ChatCompletionRequest{
		Model:    codename,
		Messages: l.convertMessages(m.Vision, m.IsLlama, prepend),
		Stream:   true,
		StreamOptions: &openai.StreamOptions{
			IncludeUsage: true,
		},
		Temperature: settings.Temperature,
		TopP:        settings.TopP,
		// MinP anyone?
		FrequencyPenalty: settings.FrequencyPenalty,
		Seed:             settings.Seed,
		Private:          provider == model.ProviderPollinations,
	}

	completionStart := time.Now()
	ctx, cancel := context.WithDeadline(context.Background(), completionStart.Add(5*time.Minute))
	defer cancel()

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return "", Usage{}, err
	}
	defer stream.Close()

	var text strings.Builder
	usage := Usage{}

	//tokens := 0
	firstTokenTime := time.Time{}
	for {
		response, err := stream.Recv()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return text.String(), Usage{}, err
		}
		if response.Usage != nil {
			usage = Usage{
				PromptTokens:   response.Usage.PromptTokens,
				ResponseTokens: response.Usage.CompletionTokens,
				TotalTokens:    response.Usage.TotalTokens,
			}
		}
		if len(response.Choices) == 0 {
			continue
		}
		if firstTokenTime.IsZero() {
			firstTokenTime = time.Now()
		}
		//tokens++
		//if tokens%10 == 0 {
		//	slog.Debug("stream progress", slog.Int("tokens", tokens), slog.Duration("in", time.Since(completionStart)), "text", text.String())
		//}
		fmt.Print(response.Choices[0].Delta.Content)
		text.WriteString(response.Choices[0].Delta.Content)
	}

	fmt.Print("\n")
	in := time.Since(firstTokenTime)
	slog.Info("stream closed", "sinceFirst", in, "sinceStart", time.Since(completionStart), "tok/s", float64(usage.ResponseTokens)/in.Seconds())

	// if the api provider is retarded enough to use HTML escapes like &lt; in a fucking API,
	// strip the fuckers off
	unescaped := html.UnescapeString(text.String())
	unescaped = strings.TrimSpace(unescaped)

	// if the model is dumb enough to prepend usernames, cut them off
	if usernames == nil {
		usernames = map[string]struct{}{}
	}
	usernames["clanker"] = struct{}{}
	for username := range usernames {
		prefix := username + ": "
		for strings.HasPrefix(strings.ToLower(unescaped), strings.ToLower(prefix)) {
			unescaped = unescaped[len(prefix):]
		}
	}
	// and trim spaces again after our checks, for good measure
	unescaped = strings.TrimSpace(unescaped)
	slog.Info("response", slog.String("text", text.String()), slog.String("unescaped", unescaped), slog.Duration("duration", time.Since(completionStart)), slog.String("model", m.Name), slog.String("provider", provider))

	l.Messages = append(l.Messages, Message{
		Role:    RoleAssistant,
		Content: unescaped,
	})

	return unescaped, usage, nil
}

func (l *Llmer) requestCompletionInternal(
	m model.Model,
	provider string,
	usernames map[string]struct{},
	settings persona.InferenceSettings,
	prepend string,
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
		tokensToTry := tokens // Default: try all tokens with this base URL

		// Special Cloudflare logic: if multiple URLs match multiple tokens, use only the corresponding token for that URL index
		if provider == model.ProviderCloudflare && len(baseUrls) > 1 && len(baseUrls) == len(tokens) {
			if i < len(tokens) {
				tokensToTry = []string{tokens[i]} // Only use the token matching this specific base URL index
			} else {
				// Should not happen due to validation above, but skip defensively
				continue
			}
		}

		for _, token := range tokensToTry {
			config := openai.DefaultConfig(token)
			config.BaseURL = baseUrl
			if provider == model.ProviderGithub { // Handle Github special case
				config = openai.DefaultAzureConfig(token, baseUrl)
				config.APIType = openai.APITypeOpenAI
			}
			client := openai.NewClientWithConfig(config)

			for _, codename := range codenames {
				slog.Info("attempting request", "provider", provider, "baseUrl", baseUrl, "codename", codename)
				res, usage, err := l.requestCompletionInternal2(m, codename, provider, usernames, settings, client, prepend)
				if err == nil {
					// we got a response, but if we used a prefill, we should indicate that it was used
					// (prepend it to the response in bold)
					if prepend != "" {
						res = fmt.Sprintf("**%s** %s", strings.ReplaceAll(strings.TrimSpace(prepend), "**", ""), res)
					}
					return res, usage, nil
				}
				lastErr = err // Store the last error encountered
				slog.Warn("request failed, trying next config", "provider", provider, "baseUrl", baseUrl, "codename", codename, "error", err)
			}
		}
	}

	return "", Usage{}, fmt.Errorf("all configurations for provider %s failed: %w", provider, lastErr) // all baseUrls/tokens/codenames errored
}

func (l *Llmer) inferMarkovChain(usernames map[string]struct{}) string {
	if len(l.Messages) == 0 {
		return ""
	}

	chain := markov.NewChain(markovChainOrder)
	totalWords := 0
	for _, msg := range l.Messages {
		//if len(l.Messages) > 1 && msg.Role == RoleSystem {
		//	continue
		//}
		content := msg.Content
		for username := range usernames {
			content = strings.TrimPrefix(content, username+": ")
		}
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

// shouldSwapToVision returns true if any of the last 4 messages had images sent by user
func (l Llmer) shouldSwapToVision() bool {
	numMessages := len(l.Messages)

	for i := max(0, numMessages-4); i < numMessages; i++ {
		message := l.Messages[i]
		// the message is from the user AND has images
		if message.Role == RoleUser && len(message.Images) > 0 {
			return true
		}
	}

	return false
}

func (l *Llmer) RequestCompletion(models []model.Model, usernames map[string]struct{}, settings persona.InferenceSettings, prepend string) (res string, usage Usage, err error) {
	if len(models) == 0 {
		err = errNoModelsForCompletion
		return
	}

	if models[0].IsMarkov {
		res = l.inferMarkovChain(usernames)
		usage = Usage{}
		err = nil
		return
	}

	settings.Remap() // remap values (1.0 temp -> 0.6 temp)

	modelsToTry := models

	// If the last message has an image, filter models to only include vision models
	if l.shouldSwapToVision() {
		visionModels := []model.Model{}
		for _, mod := range models {
			if mod.Vision {
				visionModels = append(visionModels, mod)
			}
		}
		if len(visionModels) > 0 {
			slog.Info("last message has image, filtering to vision models", "count", len(visionModels))
			modelsToTry = visionModels
		} else {
			slog.Info("last message has image, but no vision models provided in the list; swapping to DefaultVisionModels")
			modelsToTry = model.GetModelsByNames(model.DefaultVisionModels)
		}
	}

	var lastErr error

	for _, m := range modelsToTry {
		if m.IsMarkov {
			continue
		}

		slog.Info("attempting completion with model", "model", m.Name)
		for _, provider := range model.ScoreProviders(m.Reasoning) {
			retries := 0
		retry:
			if retries >= 3 {
				slog.Warn("max retries reached for provider", "provider", provider.Name, "model", m.Name)
				continue // Try next provider
			}
			if _, ok := m.Providers[provider.Name]; !ok {
				continue // This provider is not configured for this model
			}
			slog.Info("requesting completion", "model", m.Name, "provider", provider.Name, "providerErrors", provider.Errors, "retries", retries)

			res, usage, err = l.requestCompletionInternal(m, provider.Name, usernames, settings.Fixup(), prepend)

			// Check for empty response first
			if err == nil && res == "" {
				slog.Warn("got an empty response from requestCompletionInternal", "model", m.Name, "provider", provider.Name)
				err = errors.New("empty response received") // Treat empty response as an error for retry logic
			}

			if err != nil {
				slog.Warn("requestCompletionInternal failed", "model", m.Name, "provider", provider.Name, "error", err, "retries", retries)
				lastErr = err // Store the error
				retries++
				provider.Errors++
				goto retry
			}

			// Success
			if usage.IsEmpty() {
				usage = l.estimateUsage(m)
			} else if usage.ResponseTokens <= 1 {
				// unrealistic; openrouter api responds with response tokens set to 1
				estimatedUsage := l.estimateUsage(m)
				usage.ResponseTokens = estimatedUsage.ResponseTokens
			}

			slog.Info("request successful", "model", m.Name, "provider", provider.Name, "usage", usage.String())
			return res, usage, nil // return on success
		}
		slog.Warn("all providers failed for model", "model", m.Name)
	}

	slog.Error("all models failed to provide a completion", "lastError", lastErr)
	err = fmt.Errorf("all models failed: %w", lastErr)
	return "", Usage{}, err
}
