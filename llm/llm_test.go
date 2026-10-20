package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/zeozeozeo/x3/openai"
)

func TestConvertMessagesAppendsTailTextToMultiContent(t *testing.T) {
	imageServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n"))
	}))
	defer imageServer.Close()

	l := Llmer{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "what is in this image?",
				Images:  []string{imageServer.URL + "/image.png"},
			},
		},
		WorldBible: "world facts",
	}

	messages := l.convertMessages(true, true, "", "", "\nsearch results", context.Background())
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	msg := messages[0]
	if msg.Content != "" {
		t.Fatalf("Content must stay empty for multipart messages, got %q", msg.Content)
	}
	if len(msg.MultiContent) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(msg.MultiContent))
	}
	if msg.MultiContent[0].Type != openai.ChatMessagePartTypeText {
		t.Fatalf("expected first part to be text, got %q", msg.MultiContent[0].Type)
	}
	for _, want := range []string{"what is in this image?", "\nsearch results", "world facts"} {
		if !strings.Contains(msg.MultiContent[0].Text, want) {
			t.Fatalf("text part %q is missing %q", msg.MultiContent[0].Text, want)
		}
	}
	if msg.MultiContent[1].Type != openai.ChatMessagePartTypeImageURL {
		t.Fatalf("expected second part to be an image, got %q", msg.MultiContent[1].Type)
	}

	if _, err := json.Marshal(msg); err != nil {
		t.Fatalf("multipart message must marshal: %v", err)
	}
}

func TestConvertMessagesAppendsTailTextToContent(t *testing.T) {
	l := Llmer{
		Messages: []Message{
			{Role: RoleUser, Content: "hello"},
		},
		WorldBible: "world facts",
	}

	messages := l.convertMessages(true, true, "", "", "\nsearch results", context.Background())
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	if want := "hello\nsearch resultsworld facts"; messages[0].Content != want {
		t.Fatalf("unexpected content: got %q, want %q", messages[0].Content, want)
	}
	if len(messages[0].MultiContent) != 0 {
		t.Fatalf("expected no multipart parts, got %d", len(messages[0].MultiContent))
	}
}

func TestAppendChatMessageTextAddsMissingTextPart(t *testing.T) {
	msg := openai.ChatCompletionMessage{
		Role: openai.ChatMessageRoleUser,
		MultiContent: []openai.ChatMessagePart{
			{
				Type:     openai.ChatMessagePartTypeImageURL,
				ImageURL: &openai.ChatMessageImageURL{URL: "https://example.com/image.png"},
			},
		},
	}

	appendChatMessageText(&msg, "caption")

	if msg.Content != "" {
		t.Fatalf("Content must stay empty, got %q", msg.Content)
	}
	if len(msg.MultiContent) != 2 {
		t.Fatalf("expected 2 parts, got %d", len(msg.MultiContent))
	}
	if msg.MultiContent[0].Type != openai.ChatMessagePartTypeText || msg.MultiContent[0].Text != "caption" {
		t.Fatalf("unexpected first part: %#v", msg.MultiContent[0])
	}
	if _, err := json.Marshal(msg); err != nil {
		t.Fatalf("message must marshal: %v", err)
	}
}
