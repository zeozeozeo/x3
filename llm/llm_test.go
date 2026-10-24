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

var testDataURIPayload = "data:image/png;base64," + strings.Repeat("QUJD", 100)

func TestConvertMessagesDataURIImage(t *testing.T) {
	l := Llmer{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "what is in this image?",
				Images:  []string{testDataURIPayload},
			},
		},
	}

	for _, supportsImageURL := range []bool{true, false} {
		messages := l.convertMessages(true, supportsImageURL, "", "", "", context.Background())
		if len(messages) != 1 {
			t.Fatalf("supportsImageURL=%v: expected 1 message, got %d", supportsImageURL, len(messages))
		}
		msg := messages[0]
		if strings.Contains(msg.Content, "failed to fetch") || strings.Contains(msg.Content, "QUJD") {
			t.Fatalf("supportsImageURL=%v: base64 leaked into content: %q", supportsImageURL, msg.Content)
		}
		if len(msg.MultiContent) != 2 {
			t.Fatalf("supportsImageURL=%v: expected 2 parts, got %d", supportsImageURL, len(msg.MultiContent))
		}
		if msg.MultiContent[0].Type != openai.ChatMessagePartTypeText || msg.MultiContent[0].Text != "what is in this image?" {
			t.Fatalf("supportsImageURL=%v: unexpected text part: %#v", supportsImageURL, msg.MultiContent[0])
		}
		if msg.MultiContent[1].Type != openai.ChatMessagePartTypeImageURL ||
			msg.MultiContent[1].ImageURL == nil ||
			msg.MultiContent[1].ImageURL.URL != testDataURIPayload {
			t.Fatalf("supportsImageURL=%v: expected image part with the data URI, got %#v", supportsImageURL, msg.MultiContent[1])
		}
	}
}

func TestConvertMessagesDataURINoVision(t *testing.T) {
	origCallback := generateImageDescriptionCallback
	generateImageDescriptionCallback = func(imageURL string, ctx context.Context) (string, error) {
		return "a red square", nil
	}
	defer func() { generateImageDescriptionCallback = origCallback }()

	l := Llmer{
		Messages: []Message{
			{
				Role:    RoleUser,
				Content: "what is in this image?",
				Images:  []string{testDataURIPayload},
			},
		},
	}

	messages := l.convertMessages(false, true, "", "", "", context.Background())
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].Content
	if strings.Contains(content, "QUJD") {
		t.Fatalf("base64 leaked into content: %q", content)
	}
	if want := "what is in this image?\n[attached image.png: a red square]"; content != want {
		t.Fatalf("unexpected content: got %q, want %q", content, want)
	}
}

func TestConvertMessagesFailedFetchTruncatesURL(t *testing.T) {
	longURL := "http://127.0.0.1:1/" + strings.Repeat("a", 5000) + ".png"
	l := Llmer{
		Messages: []Message{
			{Role: RoleUser, Content: "what is in this image?", Images: []string{longURL}},
		},
	}

	messages := l.convertMessages(true, true, "", "", "", context.Background())
	if len(messages) != 1 {
		t.Fatalf("expected 1 message, got %d", len(messages))
	}
	content := messages[0].Content
	if !strings.Contains(content, "<failed to fetch image `") || !strings.Contains(content, "(truncated)") {
		t.Fatalf("expected truncated failure marker, got %q", content)
	}
	if len(content) > 1000 {
		t.Fatalf("failure message inlined a huge URL: %d chars", len(content))
	}
}

func TestImageFilename(t *testing.T) {
	cases := []struct {
		imageURL string
		want     string
	}{
		{"data:image/png;base64,AAAA", "image.png"},
		{"data:image/jpeg;base64,AAAA", "image.jpeg"},
		{"data:;base64,AAAA", "image.png"},
		{"data:text/html;base64,AAAA", "image.png"},
		{"https://cdn.example.com/photos/cat.png?size=large", "cat.png"},
		{"not a url", "not a url"},
	}
	for _, c := range cases {
		if got := imageFilename(c.imageURL); got != c.want {
			t.Errorf("imageFilename(%q) = %q, want %q", c.imageURL, got, c.want)
		}
	}
}

func TestTrimPersistedMessages(t *testing.T) {
	in := []Message{
		{Role: RoleUser, Content: "old", Images: []string{"data:image/png;base64,AAAA", "data:image/png;base64,BBBB"}},
		{Role: RoleAssistant, Content: "hi"},
		{Role: RoleUser, Content: "new", Images: []string{"data:image/jpeg;base64,CCCC", "data:image/jpeg;base64,DDDD"}},
		{Role: RoleUser, Content: "no image"},
	}

	out := TrimPersistedMessages(in)

	if len(out[0].Images) != 0 {
		t.Errorf("older image message should have Images cleared, got %v", out[0].Images)
	}
	if len(out[2].Images) != 1 || out[2].Images[0] != "data:image/jpeg;base64,CCCC" {
		t.Errorf("newest image message should keep only its first image, got %v", out[2].Images)
	}
	if len(out[3].Images) != 0 {
		t.Errorf("message without images must stay without images")
	}
	// input must not be mutated
	if len(in[0].Images) != 2 || len(in[2].Images) != 2 {
		t.Fatalf("input messages were mutated: %v %v", in[0].Images, in[2].Images)
	}

	empty := TrimPersistedMessages([]Message{{Role: RoleUser, Content: "hi"}})
	if len(empty) != 1 || len(empty[0].Images) != 0 {
		t.Errorf("unexpected output for imageless history: %#v", empty)
	}
}
