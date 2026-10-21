package opencode

import (
	"context"
	"errors"
	"io"
	"strings"

	"github.com/zeozeozeo/x3/openai"
)

// CreateChatCompletion performs a chat completion against the OpenCode Zen
// free tier.
func CreateChatCompletion(ctx context.Context, client *openai.Client, req openai.ChatCompletionRequest) (openai.ChatCompletionResponse, error) {
	req.Stream = true

	tools := make([]openai.Tool, len(req.Tools))
	copy(tools, req.Tools)
	req.Tools = EnsureGateTool(tools)

	stream, err := client.CreateChatCompletionStream(ctx, req)
	if err != nil {
		return openai.ChatCompletionResponse{}, err
	}
	defer stream.Close()

	var (
		content   strings.Builder
		reasoning strings.Builder
		toolCalls = map[int]*openai.ToolCall{}
		toolOrder []int
		finish    = openai.FinishReasonStop
	)
	response := openai.ChatCompletionResponse{
		Model: req.Model,
	}

	for {
		chunk, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return openai.ChatCompletionResponse{}, err
		}

		if chunk.ID != "" {
			response.ID = chunk.ID
		}
		if chunk.Model != "" {
			response.Model = chunk.Model
		}
		if chunk.Usage != nil {
			response.Usage = *chunk.Usage
		}

		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" && choice.FinishReason != openai.FinishReasonNull {
				finish = choice.FinishReason
			}
			content.WriteString(choice.Delta.Content)
			if choice.Delta.ReasoningContent != "" {
				reasoning.WriteString(choice.Delta.ReasoningContent)
			} else if choice.Delta.Reasoning != "" {
				reasoning.WriteString(choice.Delta.Reasoning)
			}
			for _, call := range choice.Delta.ToolCalls {
				index := 0
				if call.Index != nil {
					index = *call.Index
				}
				existing, ok := toolCalls[index]
				if !ok {
					merged := call
					toolCalls[index] = &merged
					toolOrder = append(toolOrder, index)
					continue
				}
				if call.ID != "" {
					existing.ID = call.ID
				}
				if call.Type != "" {
					existing.Type = call.Type
				}
				if call.Function.Name != "" {
					existing.Function.Name = call.Function.Name
				}
				existing.Function.Arguments += call.Function.Arguments
			}
		}
	}

	message := openai.ChatCompletionMessage{
		Role:             openai.ChatMessageRoleAssistant,
		Content:          content.String(),
		ReasoningContent: reasoning.String(),
	}
	if len(toolOrder) > 0 {
		message.ToolCalls = make([]openai.ToolCall, 0, len(toolOrder))
		for _, index := range toolOrder {
			message.ToolCalls = append(message.ToolCalls, *toolCalls[index])
		}
	}

	response.Choices = []openai.ChatCompletionChoice{{
		Index:        0,
		Message:      message,
		FinishReason: finish,
	}}

	return response, nil
}
