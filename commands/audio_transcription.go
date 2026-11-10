package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/zeozeozeo/x3/ffmpegbox"
	"github.com/zeozeozeo/x3/videothumb"
	"github.com/zeozeozeo/x3/whisper"
)

const maxTranscriptRunes = 8000
const noSpeechTranscript = `"(no speech detected)"`
const audioCacheDir = "x3-audio-cache"

var (
	whisperTranscribe = whisper.Transcribe
	extractAudio      = ffmpegbox.ExtractAudio
	probeDuration     = ffmpegbox.ProbeDuration
	downloadMedia     = downloadMediaAttachment
	mediaBackend      = ffmpegbox.Available
)

var audioDownloadClient = &http.Client{Timeout: 60 * time.Second}

func isAudioAttachment(a discord.Attachment) bool {
	if isVideoAttachment(a) {
		return false
	}
	if a.ContentType != nil && strings.HasPrefix(strings.ToLower(*a.ContentType), "audio/") {
		return true
	}
	switch strings.ToLower(filepath.Ext(a.Filename)) {
	case ".mp3", ".m4a", ".aac", ".ogg", ".oga", ".opus", ".wav", ".flac", ".wma", ".amr", ".mp4":
		return true
	}
	return false
}

func isVoiceMessageAttachment(a discord.Attachment) bool {
	if a.Waveform != nil && *a.Waveform != "" {
		return true
	}
	return strings.HasPrefix(strings.ToLower(a.Filename), "voice-message")
}

func audioAttachmentNote(a discord.Attachment) string {
	if isVoiceMessageAttachment(a) {
		return "[voice message]"
	}
	return fmt.Sprintf("[audio attachment: %s]", a.Filename)
}

func audioLabel(a discord.Attachment) string {
	switch {
	case isVoiceMessageAttachment(a):
		return "voice message"
	case isVideoAttachment(a):
		return "video attachment: " + a.Filename
	default:
		return "audio attachment: " + a.Filename
	}
}

func tooLongAudioLine(a discord.Attachment) string {
	return fmt.Sprintf("[%s: audio is too long to transcribe (over 3 minutes)]", audioLabel(a))
}

func unavailableAudioLine(a discord.Attachment, reason string) string {
	return fmt.Sprintf("[%s: transcription unavailable (%s)]", audioLabel(a), reason)
}

func tooLargeAudioLine(a discord.Attachment) string {
	return fmt.Sprintf("[%s: file too large to transcribe]", audioLabel(a))
}

func appendAudioTranscriptionLines(ctx context.Context, content string, attachments []discord.Attachment) string {
	for _, attachment := range attachments {
		if !isAudioAttachment(attachment) && !isVideoAttachment(attachment) {
			continue
		}
		content = appendContextLine(content, audioTranscriptionLine(ctx, attachment))
	}
	return content
}

func audioTranscriptionLine(ctx context.Context, attachment discord.Attachment) string {
	cfg := whisper.ConfigFromEnv()
	if !cfg.Enabled {
		return ""
	}
	if transcript, ok := readAudioTranscriptCache(attachment.ID.String()); ok {
		return transcriptLine(attachment, transcript)
	}
	if known, ok := attachmentDuration(attachment); ok && known > whisper.MaxDuration {
		return tooLongAudioLine(attachment)
	}
	limit := videothumb.MaxDownloadBytes()
	if attachment.Size > limit {
		return unavailableAudioLine(attachment, "media exceeds the download limit")
	}
	if attachment.URL == "" {
		return unavailableAudioLine(attachment, "missing URL")
	}
	if len(cfg.Tokens) == 0 {
		return ""
	}

	data, err := downloadMedia(ctx, attachment.URL, limit)
	if err != nil {
		slog.Warn("audio transcription: download failed",
			"err", err, "attachment_id", attachment.ID.String(), "filename", attachment.Filename)
		return unavailableAudioLine(attachment, "download failed")
	}

	if _, ok := attachmentDuration(attachment); !ok {
		if !mediaBackend("ffprobe") {
			return unavailableAudioLine(attachment, "duration unknown")
		}
		probed, probeErr := probeDuration(ctx, data)
		if probeErr != nil {
			slog.Warn("audio transcription: duration probe failed",
				"err", probeErr, "attachment_id", attachment.ID.String(), "filename", attachment.Filename)
			return unavailableAudioLine(attachment, "duration unreadable")
		}
		if probed > whisper.MaxDuration {
			return tooLongAudioLine(attachment)
		}
	}

	filename := attachment.Filename
	audio := data
	if isVideoAttachment(attachment) {
		if !mediaBackend("ffmpeg") {
			return unavailableAudioLine(attachment, "ffmpeg backend unavailable")
		}
		extracted, extractErr := extractAudio(ctx, data, cfg.MaxAudioBytes)
		if extractErr != nil {
			slog.Warn("audio transcription: audio extraction failed",
				"err", extractErr, "attachment_id", attachment.ID.String(), "filename", attachment.Filename)
			return unavailableAudioLine(attachment, "audio extraction failed")
		}
		audio = extracted
		filename = audioFlacName(attachment.Filename)
	} else if !groqSupportedAudioFilename(attachment.Filename) || len(data) > cfg.MaxAudioBytes {
		if len(data) > cfg.MaxAudioBytes && !mediaBackend("ffmpeg") {
			return tooLargeAudioLine(attachment)
		}
		if !mediaBackend("ffmpeg") {
			return unavailableAudioLine(attachment, "ffmpeg backend unavailable")
		}
		extracted, extractErr := extractAudio(ctx, data, cfg.MaxAudioBytes)
		if extractErr != nil {
			slog.Warn("audio transcription: audio extraction failed",
				"err", extractErr, "attachment_id", attachment.ID.String(), "filename", attachment.Filename)
			return unavailableAudioLine(attachment, "audio extraction failed")
		}
		audio = extracted
		filename = audioFlacName(attachment.Filename)
	}

	transcript, err := whisperTranscribe(ctx, filename, audio)
	if err != nil {
		slog.Warn("audio transcription: transcription failed",
			"err", err, "attachment_id", attachment.ID.String(), "filename", attachment.Filename)
		return unavailableAudioLine(attachment, "transcription failed")
	}

	normalized := normalizeTranscript(transcript)
	if normalized != "" {
		writeAudioTranscriptCache(attachment.ID.String(), normalized)
	}
	return transcriptLine(attachment, normalized)
}

// transcriptLine formats a normalized transcript for the prompt.
func transcriptLine(attachment discord.Attachment, transcript string) string {
	value := noSpeechTranscript
	if transcript != "" {
		value = fmt.Sprintf("%q", transcript)
	}
	switch {
	case isVoiceMessageAttachment(attachment):
		return fmt.Sprintf("[voice message transcript: %s]", value)
	case isVideoAttachment(attachment):
		return fmt.Sprintf("[video transcript (%s): %s]", attachment.Filename, value)
	default:
		return fmt.Sprintf("[audio transcript (%s): %s]", attachment.Filename, value)
	}
}

func normalizeTranscript(transcript string) string {
	normalized := strings.Join(strings.Fields(transcript), " ")
	if utf8.RuneCountInString(normalized) <= maxTranscriptRunes {
		return normalized
	}
	runes := []rune(normalized)
	return string(runes[:maxTranscriptRunes]) + "…"
}

func audioFlacName(filename string) string {
	base := strings.TrimSuffix(filename, filepath.Ext(filename))
	if base == "" {
		return "audio.flac"
	}
	return base + ".flac"
}

func groqSupportedAudioFilename(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".flac", ".mp3", ".mp4", ".mpeg", ".mpga", ".m4a", ".ogg", ".oga", ".wav", ".webm":
		return true
	}
	return false
}

func attachmentDuration(a discord.Attachment) (time.Duration, bool) {
	if a.DurationSecs == nil || *a.DurationSecs <= 0 {
		return 0, false
	}
	return time.Duration(*a.DurationSecs * float64(time.Second)), true
}

func audioTranscriptCachePath(id string) string {
	return filepath.Join(audioCacheDir, id+".txt")
}

func readAudioTranscriptCache(id string) (string, bool) {
	if id == "" || id == "0" {
		return "", false
	}
	data, err := os.ReadFile(audioTranscriptCachePath(id))
	if err != nil {
		return "", false
	}
	transcript := strings.TrimSpace(string(data))
	if transcript == "" {
		return "", false
	}
	return transcript, true
}

func writeAudioTranscriptCache(id, transcript string) {
	if id == "" || id == "0" {
		return
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return
	}
	if err := os.MkdirAll(audioCacheDir, 0755); err != nil {
		slog.Warn("audio transcription: create cache dir failed", "err", err)
		return
	}
	if err := os.WriteFile(audioTranscriptCachePath(id), []byte(transcript), 0644); err != nil {
		slog.Warn("audio transcription: cache write failed", "err", err, "attachment_id", id)
	}
}

func downloadMediaAttachment(ctx context.Context, url string, limit int) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "x3/1.0")
	resp, err := audioDownloadClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("bad status %d", resp.StatusCode)
	}
	if resp.ContentLength > int64(limit) {
		return nil, fmt.Errorf("media exceeds limit (%d > %d bytes)", resp.ContentLength, limit)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, int64(limit)+1))
	if err != nil {
		return nil, err
	}
	if len(data) > limit {
		return nil, fmt.Errorf("media exceeds limit after download (%d > %d bytes)", len(data), limit)
	}
	if len(data) == 0 {
		return nil, errors.New("empty media response")
	}
	return data, nil
}
