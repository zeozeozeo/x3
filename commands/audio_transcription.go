package commands

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/zeozeozeo/x3/cache"
	"github.com/zeozeozeo/x3/ffmpegbox"
	"github.com/zeozeozeo/x3/videothumb"
	"github.com/zeozeozeo/x3/whisper"
)

const maxTranscriptRunes = 8000
const noSpeechTranscript = `"(no speech detected)"`

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
	return isAudioFilename(a.Filename)
}

// isAudioFilename reports whether the filename extension conventionally
// denotes an audio file.
func isAudioFilename(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
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

type audioMediaKind int

const (
	audioMediaAudio audioMediaKind = iota
	audioMediaVoice
	audioMediaVideo
)

type audioMedia struct {
	ID       string
	Filename string
	Size     int
	Duration time.Duration
	Kind     audioMediaKind
	Download func(context.Context) ([]byte, error)
}

func (m audioMedia) label() string {
	switch m.Kind {
	case audioMediaVoice:
		return "voice message"
	case audioMediaVideo:
		return "video attachment: " + m.Filename
	default:
		return "audio attachment: " + m.Filename
	}
}

func (m audioMedia) tooLongLine() string {
	return fmt.Sprintf("[%s: audio is too long to transcribe (over 3 minutes)]", m.label())
}

func (m audioMedia) unavailableLine(reason string) string {
	return fmt.Sprintf("[%s: transcription unavailable (%s)]", m.label(), reason)
}

func (m audioMedia) tooLargeLine() string {
	return fmt.Sprintf("[%s: file too large to transcribe]", m.label())
}

// transcriptLine formats a normalized transcript for the prompt.
func (m audioMedia) transcriptLine(transcript string) string {
	value := noSpeechTranscript
	if transcript != "" {
		value = fmt.Sprintf("%q", transcript)
	}
	switch m.Kind {
	case audioMediaVoice:
		return fmt.Sprintf("[voice message transcript: %s]", value)
	case audioMediaVideo:
		return fmt.Sprintf("[video transcript (%s): %s]", m.Filename, value)
	default:
		return fmt.Sprintf("[audio transcript (%s): %s]", m.Filename, value)
	}
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
	return transcribeAudioMedia(ctx, discordAudioMedia(attachment))
}

// discordAudioMedia adapts a Discord attachment to the shared transcription
// pipeline.
func discordAudioMedia(attachment discord.Attachment) audioMedia {
	kind := audioMediaAudio
	switch {
	case isVoiceMessageAttachment(attachment):
		kind = audioMediaVoice
	case isVideoAttachment(attachment):
		kind = audioMediaVideo
	}
	media := audioMedia{
		ID:       attachment.ID.String(),
		Filename: attachment.Filename,
		Size:     attachment.Size,
		Kind:     kind,
	}
	if duration, ok := attachmentDuration(attachment); ok {
		media.Duration = duration
	}
	if attachment.URL != "" {
		media.Download = func(ctx context.Context) ([]byte, error) {
			return downloadMedia(ctx, attachment.URL, videothumb.MaxDownloadBytes())
		}
	}
	return media
}

// transcribeAudioMedia runs the shared transcription pipeline: cache, size and
// duration gates, download, ffprobe, audio extraction, Whisper, and prompt
// formatting.
func transcribeAudioMedia(ctx context.Context, media audioMedia) string {
	cfg := whisper.ConfigFromEnv()
	if !cfg.Enabled {
		return ""
	}
	if transcript, ok := readAudioTranscriptCache(media.ID); ok {
		return media.transcriptLine(transcript)
	}
	if media.Duration > whisper.MaxDuration {
		return media.tooLongLine()
	}
	limit := videothumb.MaxDownloadBytes()
	if media.Size > limit {
		return media.unavailableLine("media exceeds the download limit")
	}
	if media.Download == nil {
		return media.unavailableLine("missing URL")
	}
	if len(cfg.Tokens) == 0 {
		return ""
	}

	data, err := media.Download(ctx)
	if err != nil {
		slog.Warn("audio transcription: download failed",
			"err", err, "attachment_id", media.ID, "filename", media.Filename)
		return media.unavailableLine("download failed")
	}

	if media.Duration == 0 {
		if !mediaBackend("ffprobe") {
			return media.unavailableLine("duration unknown")
		}
		probed, probeErr := probeDuration(ctx, data)
		if probeErr != nil {
			slog.Warn("audio transcription: duration probe failed",
				"err", probeErr, "attachment_id", media.ID, "filename", media.Filename)
			return media.unavailableLine("duration unreadable")
		}
		if probed > whisper.MaxDuration {
			return media.tooLongLine()
		}
	}

	filename := media.Filename
	audio := data
	if media.Kind == audioMediaVideo {
		if !mediaBackend("ffmpeg") {
			return media.unavailableLine("ffmpeg backend unavailable")
		}
		extracted, extractErr := extractAudio(ctx, data, cfg.MaxAudioBytes)
		if extractErr != nil {
			slog.Warn("audio transcription: audio extraction failed",
				"err", extractErr, "attachment_id", media.ID, "filename", media.Filename)
			return media.unavailableLine("audio extraction failed")
		}
		audio = extracted
		filename = audioFlacName(media.Filename)
	} else if !groqSupportedAudioFilename(media.Filename) || len(data) > cfg.MaxAudioBytes {
		if len(data) > cfg.MaxAudioBytes && !mediaBackend("ffmpeg") {
			return media.tooLargeLine()
		}
		if !mediaBackend("ffmpeg") {
			return media.unavailableLine("ffmpeg backend unavailable")
		}
		extracted, extractErr := extractAudio(ctx, data, cfg.MaxAudioBytes)
		if extractErr != nil {
			slog.Warn("audio transcription: audio extraction failed",
				"err", extractErr, "attachment_id", media.ID, "filename", media.Filename)
			return media.unavailableLine("audio extraction failed")
		}
		audio = extracted
		filename = audioFlacName(media.Filename)
	}

	transcript, err := whisperTranscribe(ctx, filename, audio)
	if err != nil {
		slog.Warn("audio transcription: transcription failed",
			"err", err, "attachment_id", media.ID, "filename", media.Filename)
		return media.unavailableLine("transcription failed")
	}

	normalized := normalizeTranscript(transcript)
	if normalized != "" {
		writeAudioTranscriptCache(media.ID, normalized)
	}
	return media.transcriptLine(normalized)
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

func readAudioTranscriptCache(id string) (string, bool) {
	if id == "" || id == "0" {
		return "", false
	}
	data, ok, err := cache.Get(cache.NamespaceAudioTranscript, id)
	if err != nil {
		slog.Warn("audio transcription: cache read failed", "err", err, "attachment_id", id)
		return "", false
	}
	if !ok {
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
	if err := cache.Put(cache.NamespaceAudioTranscript, id, []byte(transcript)); err != nil {
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
