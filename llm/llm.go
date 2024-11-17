package llm

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"time"

	azopenai "github.com/Azure/azure-sdk-for-go/sdk/ai/azopenai"
	"github.com/Azure/azure-sdk-for-go/sdk/azcore"
)

var (
	githubToken   = os.Getenv("X3ZEO_GITHUB_TOKEN")
	errNoResponse = errors.New("no response")
)

const (
	azureBaseURL = "https://models.inference.ai.azure.com"
)

const (
	RoleUser      = "user"
	RoleAssistant = "assistant"
)

type Message struct {
	Content string `json:"content"`
	Role    string `json:"role"`
}

const (
	ModelGpt4o                    = "gpt-4o"
	ModelMistralNemo              = "Mistral-Nemo"
	ModelCohereCommandR082024     = "Cohere-command-r-08-2024"
	ModelLlama11bVision           = "Llama-3.2-11B-Vision-Instruct"
	ModelGpt4oMini                = "gpt-4o-mini"
	ModelLlama405b                = "Meta-Llama-3.1-405B-Instruct"
	ModelMistralLarge             = "Mistral-large-2407"
	ModelCohereCommandRPlus082024 = "Cohere-command-r-plus-08-2024"
	ModelLlama90bVision           = "Llama-3.2-90B-Vision-Instruct"
)

type Llmer struct {
	Messages []Message `json:"messages"`
}

func NewLlmer() *Llmer {
	return &Llmer{}
}

func newClient() (*azopenai.Client, error) {
	cred := azcore.NewKeyCredential(githubToken)
	return azopenai.NewClientWithKeyCredential(azureBaseURL, cred, nil)
}

func (l *Llmer) NumMessages() int {
	return len(l.Messages)
}

func (l *Llmer) TruncateMessages(max int) {
	if len(l.Messages) > max {
		l.Messages = l.Messages[len(l.Messages)-max:]
	}
}

func (l *Llmer) AddMessage(message Message) {
	l.Messages = append(l.Messages, message)
}

func (l *Llmer) convertMessageHistory() []azopenai.ChatRequestMessageClassification {
	var messages []azopenai.ChatRequestMessageClassification

	// WTF is this API O_o
	for _, msg := range l.Messages {
		switch msg.Role {
		case RoleUser:
			messages = append(messages, &azopenai.ChatRequestUserMessage{
				Content: azopenai.NewChatRequestUserMessageContent(msg.Content),
			})
		case RoleAssistant:
			messages = append(messages, &azopenai.ChatRequestAssistantMessage{
				Content: azopenai.NewChatRequestAssistantMessageContent(msg.Content),
			})
		}
	}

	return messages
}

func (l *Llmer) RequestCompletion(model string) (string, error) {
	//return "BOT TESTING", nil

	slog.Debug("request completion.. message history follows..", slog.String("model", model))
	for _, msg := range l.Messages {
		slog.Debug("    message", slog.String("role", msg.Role), slog.String("content", msg.Content))
	}

	client, err := newClient()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Minute))
	defer cancel()
	resp, err := client.GetChatCompletions(ctx, azopenai.ChatCompletionsOptions{
		Messages:       l.convertMessageHistory(),
		DeploymentName: &model,
	}, nil)
	if err != nil {
		return "", err
	}

	for _, choice := range resp.Choices {
		if choice.Delta != nil && choice.Delta.Content != nil {
			slog.Debug("----------------------------------", slog.String("text", *choice.Delta.Content))
		}
	}

	if len(resp.Choices) == 0 || resp.Choices[0].Message == nil || resp.Choices[0].Message.Content == nil {
		return "", errNoResponse
	}
	slog.Debug("response message content", slog.String("text", *resp.Choices[0].Message.Content))
	return *resp.Choices[0].Message.Content, nil

	/*
		completionStart := time.Now()
		client := newClient()

		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(5*time.Minute))
		defer cancel()

		resp, err := client.CreateChatCompletion(ctx, req)
		if err != nil {
			return "", err
		}

		if len(resp.Choices) == 0 {
			slog.Error("no response", slog.String("model", model), slog.Duration("duration", time.Since(completionStart)))
			return "", errNoResponse
		}

		slog.Debug("response", slog.String("text", resp.Choices[0].Message.Content), slog.Duration("duration", time.Since(completionStart)))
		return resp.Choices[0].Message.Content, nil

		/*
			stream, err := client.CreateChatCompletionStream(ctx, req)
			if err != nil {
				return "", err
			}
			defer stream.Close()

			var text strings.Builder
			for {
				response, err := stream.Recv()
				if errors.Is(err, io.EOF) {
					break
				}
				if err != nil {
					return text.String(), err
				}
				if len(response.Choices) > 0 {
					text.WriteString(response.Choices[0].Delta.Content)
				}
			}

			slog.Debug("response", slog.String("text", text.String()), slog.Duration("duration", time.Since(completionStart)))

			l.Messages = append(l.Messages, azopenai.ChatCompletionMessage{
				Role:    RoleAssistant,
				Content: text.String(),
			})
			return text.String(), nil
	*/
}
