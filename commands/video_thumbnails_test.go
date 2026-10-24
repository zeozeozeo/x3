package commands

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/llm"
)

func TestIsVideoAttachment(t *testing.T) {
	videoType := "video/mp4"
	pngType := "image/png"
	tests := []struct {
		name       string
		attachment discord.Attachment
		want       bool
	}{
		{"mp4 content type", discord.Attachment{Filename: "clip", ContentType: &videoType}, true},
		{"webm content type", discord.Attachment{Filename: "clip", ContentType: ptr("video/webm")}, true},
		{"extension fallback", discord.Attachment{Filename: "clip.MOV"}, true},
		{"no indicator", discord.Attachment{Filename: "notes.txt", ContentType: nil}, false},
		{"image is not video", discord.Attachment{Filename: "pic.png", ContentType: &pngType}, false},
	}
	for _, tt := range tests {
		if got := isVideoAttachment(tt.attachment); got != tt.want {
			t.Errorf("%s: isVideoAttachment = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestDiscordVideoThumbnailURLKeepsSignedParams(t *testing.T) {
	signed := "https://media.discordapp.net/attachments/1/2/clip.mp4?ex=66&is=77&hm=aa"
	got := discordVideoThumbnailURL(signed)
	if !strings.HasPrefix(got, signed+"&") {
		t.Errorf("signed params must be preserved: %q", got)
	}
	for _, required := range []string{"format=jpeg", "width=640", "height=640"} {
		if !strings.Contains(got, required) {
			t.Errorf("thumbnail URL missing %q: %q", required, got)
		}
	}
	unsigned := "https://media.discordapp.net/attachments/1/2/clip.mp4"
	if got := discordVideoThumbnailURL(unsigned); !strings.Contains(got, unsigned+"?") {
		t.Errorf("query must start fresh on unsigned URLs: %q", got)
	}
	if got := discordVideoThumbnailURL(""); got != "" {
		t.Errorf("empty proxy URL must return empty, got %q", got)
	}
}

func TestDiscordVideoThumbnailProbe(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x3A, 0x99}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/good.mp4" {
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(jpeg)
			return
		}
		if r.URL.Path == "/notimage.mp4" {
			w.Header().Set("Content-Type", "video/mp4")
			_, _ = w.Write([]byte("videobytes"))
			return
		}
		w.WriteHeader(http.StatusNotFound)
	}))
	defer server.Close()

	if uri := discordVideoThumbnailProbe(context.Background(), server.URL+"/good.mp4"); !strings.HasPrefix(uri, "data:image/jpeg;base64,") {
		t.Errorf("probe on image response = %q", uri)
	}
	if uri := discordVideoThumbnailProbe(context.Background(), server.URL+"/notimage.mp4"); uri != "" {
		t.Errorf("probe must reject non-image responses, got %q", uri)
	}
	if uri := discordVideoThumbnailProbe(context.Background(), server.URL+"/missing.mp4"); uri != "" {
		t.Errorf("probe must reject 404s, got %q", uri)
	}
}

func TestGetMessageContentNotesVideoAttachments(t *testing.T) {
	videoType := "video/mp4"
	msg := discord.Message{
		Content: "look at this",
		Attachments: []discord.Attachment{
			{ID: snowflake.ID(1), Filename: "cat.mp4", ContentType: &videoType},
		},
	}
	got := getMessageContent(msg)
	if !strings.Contains(got, "look at this") || !strings.Contains(got, "[video attachment: cat.mp4]") {
		t.Errorf("getMessageContent = %q, want video note appended", got)
	}
}

func TestAddVideoThumbnailSourcesAttachesProbeResult(t *testing.T) {
	jpeg := []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		_, _ = w.Write(jpeg)
	}))
	defer server.Close()

	videoType := "video/mp4"
	attachments := []discord.Attachment{
		{ID: snowflake.ID(4242), Filename: "clip.mp4", ContentType: &videoType, URL: server.URL + "/clip.mp4", ProxyURL: server.URL + "/clip.mp4"},
		{ID: snowflake.ID(4243), Filename: "pic.png", ContentType: ptr("image/png")},
	}

	llmer := llm.NewLlmer(123)
	llmer.AddMessage(llm.RoleUser, "check this video", 1)
	addVideoThumbnailSources(llmer, attachments)

	if len(llmer.Messages) == 0 {
		t.Fatal("no messages")
	}
	images := llmer.Messages[len(llmer.Messages)-1].Images
	if len(images) != 1 || !strings.HasPrefix(images[0], "data:image/jpeg;base64,") {
		t.Fatalf("video thumbnail not attached as image, got %#v", images)
	}
}
