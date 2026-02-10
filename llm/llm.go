package llm

import (
	"context"
	"embed"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

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
	RoleUser      = openai.ChatMessageRoleUser
	RoleAssistant = openai.ChatMessageRoleAssistant
	RoleSystem    = openai.ChatMessageRoleSystem
)

var (
	gImageCache *imageCache = NewImageCache(100*1024*1024, 24*time.Hour)
	gAlice      *aiml.Kernel
)

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

type Message struct {
	Role    string       `json:"role"`
	Content string       `json:"content"`
	Images  []string     `json:"images"` // image URIs or base64
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
	Messages  []Message    `json:"messages"`
	ChannelID snowflake.ID `json:"channel_id"`
}

func NewLlmer(channelID snowflake.ID) *Llmer {
	return &Llmer{
		ChannelID: channelID,
	}
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
				lastMsg.Content = "*SYSTEM MESSAGE: you've used 5 `<new_message>` splits in your previous message, try staying within 1-3 splits!*\n" + lastMsg.Content
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

func (l Llmer) convertMessages(hasVision, supportsImageURL bool, prepend, searchResults string, ctx context.Context) []openai.ChatCompletionMessage {
	// find the index of the last message with images
	imageIdx := -1
	for i := len(l.Messages) - 1; i >= 0; i-- {
		if len(l.Messages[i].Images) > 0 {
			imageIdx = i
			break
		}
	}

	if imageIdx != -1 && len(l.Messages)-imageIdx >= 8 {
		// older than 8 messages, we can probably let it go
		imageIdx = -1
	}

	var messages []openai.ChatCompletionMessage
	for i, msg := range l.Messages {
		if msg.Content == "" && len(msg.Images) == 0 {
			continue // skip empty messages. HACK: they seem to appear after lobotomy, this is a hack
		}
		if len(msg.Images) == 0 || !hasVision || i != imageIdx {
			var content strings.Builder
			content.WriteString(msg.Content)

			// If this message has images but we don't have vision, generate/use descriptions
			if len(msg.Images) > 0 && !hasVision {
				for _, imageURL := range msg.Images {
					description, err := generateImageDescriptionCallback(imageURL, ctx)
					if err != nil {
						slog.Warn("failed to generate image description", "err", err, "url", imageURL)
						continue
					}
					if description != "" {
						// Extract filename from URL
						filename := "image.png"
						if idx := strings.LastIndex(imageURL, "/"); idx != -1 {
							filename = imageURL[idx+1:]
							// Remove query parameters if any
							if qIdx := strings.Index(filename, "?"); qIdx != -1 {
								filename = filename[:qIdx]
							}
						}
						fmt.Fprintf(&content, "\n[attached %s: %s]", filename, description)
					}
				}
			}

			role := msg.Role
			messages = append(messages, openai.ChatCompletionMessage{
				Role:    role,
				Content: content.String(),
			})
		} else {
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
						URL: msg.Images[0],
					},
				})
			} else { // api needs base64, will fetch image and store in memory cache
				data := gImageCache.MemoizedImageBase64(msg.Images[0])
				if data == "" {
					slog.Error("failed to fetch image!")
					parts[0].Text += fmt.Sprintf("\n<failed to fetch image `%s`>", msg.Images[0]) // notify llm
				} else {
					parts = append(parts, openai.ChatMessagePart{
						Type: openai.ChatMessagePartTypeImageURL,
						ImageURL: &openai.ChatMessageImageURL{
							URL: data,
						},
					})
				}
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
		Model:    codename,
		Messages: l.convertMessages(m.Vision, provider != model.ProviderOllama, prepend, searchResults, ctx), // ollama cloud doesn't support fetching from image URLs, how nice :)
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
	ctx, cancel := context.WithDeadline(ctx, completionStart.Add(5*time.Minute))
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
		text.WriteString(response.Choices[0].Delta.Content)
	}

	in := time.Since(firstTokenTime)
	slog.Info("stream closed", "sinceFirst", in, "sinceStart", time.Since(completionStart), "tok/s", float64(usage.ResponseTokens)/in.Seconds())

	unescaped := strings.TrimSpace(text.String())
	unescaped = replaceEnd(unescaped, ">w", ">w<", ">///", ">///<", ">.", ">.<")

	if searchDepth < 4 {
		if search := extractSearch(unescaped); search != "" {
			results, citemap := getSearchResults(search)
			return l.requestCompletionInternal2(
				m,
				codename,
				provider,
				settings,
				client,
				prepend,
				ctx,
				searchDepth+1,
				citemap,
				results,
			)
		}
	}

	// cool
	unescaped = strings.ReplaceAll(unescaped, "<new_message]", "<new_message>")

	// cites like [1] get turned into [1](<https://google.com/>)
	if searchDepth > 0 {
		unescaped = formatCites(unescaped, searchCitemap)
	}

	// and trim spaces again after our checks, for good measure
	unescaped = strings.TrimSpace(unescaped)
	slog.Info("response", "len", len(unescaped), "duration", time.Since(completionStart), "model", m.Name, "provider", provider)

	l.Messages = append(l.Messages, Message{
		Role:    RoleAssistant,
		Content: unescaped,
	})

	return unescaped, usage, nil
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

		// TODO: ability to do this with any provider
		if provider == model.ProviderCloudflare && len(baseUrls) > 1 && len(baseUrls) == len(tokens) {
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

			for _, codename := range codenames {
				slog.Info("attempting request", "provider", provider, "baseUrl", baseUrl, "codename", codename)
				res, usage, err := l.requestCompletionInternal2(m, codename, provider, settings, client, prepend, ctx, 0, nil, "")
				if err == nil {
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
		}
	}

	return "", Usage{}, fmt.Errorf("all configurations for provider %s failed: %w", provider, lastErr) // all baseUrls/tokens/codenames errored
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
	response := gAlice.Respond(content, l.ChannelID.String())
	return weirdAliceReplacer.Replace(strings.Join(strings.Fields(strings.TrimSpace(response)), " "))
}

/*
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
*/

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

	modelsToTry := models

	/*
		if l.shouldSwapToVision() {
			visionModels := []model.Model{}
			for _, mod := range models {
				if mod.Vision {
					visionModels = append(visionModels, mod)
				}
			}
			if len(visionModels) > 0 {
				modelsToTry = visionModels
			} else {
				slog.Info("last message has image, but no vision models provided in the list; swapping to DefaultVisionModels")
				modelsToTry = model.GetModelsByNames(model.DefaultVisionModels)
			}
		}
	*/

	var lastErr error

	for _, m := range modelsToTry {
		if m.IsMarkov || m.IsEliza || m.IsAlice {
			continue
		}

		slog.Info("attempting completion with model", "model", m.Name)
		for _, provider := range model.ScoreProviders(m.Reasoning) {
			retries := 0
		retry:
			if retries >= 3 {
				slog.Warn("max retries reached for provider", "provider", provider.Name, "model", m.Name)
				continue
			}
			if _, ok := m.Providers[provider.Name]; !ok {
				continue
			}
			slog.Info("requesting completion", "model", m.Name, "provider", provider.Name, "providerErrors", provider.Errors, "retries", retries)

			if ctx.Err() != nil {
				return "", Usage{}, ctx.Err()
			}

			res, usage, err = l.requestCompletionInternal(m, provider.Name, settings.Fixup(), prepend, ctx)

			// check for empty response first
			if err == nil && res == "" {
				slog.Warn("got an empty response from requestCompletionInternal", "model", m.Name, "provider", provider.Name)
				err = errors.New("empty response received")
			}

			if err != nil {
				slog.Warn("requestCompletionInternal failed", "model", m.Name, "provider", provider.Name, "error", err, "retries", retries)
				lastErr = err
				retries++
				provider.Errors++
				goto retry
			}

			if usage.IsEmpty() {
				usage = l.estimateUsage(m)
			} else if usage.ResponseTokens <= 1 {
				// unrealistic
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
