package commands

import (
	"encoding/base64"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
)

const (
	x3Icon              = "https://i.imgur.com/ckpztZY.png"
	x3ErrorIcon         = "https://i.imgur.com/hCF06SC.png"
	interactionReminder = "\n-# if you wish to disable this, use `/random_dms enable: false`"
)

// sendInteractionError sends a formatted error message as a response to a command event.
func sendInteractionError(event *handler.CommandEvent, msg string, ephemeral bool) error {
	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			SetEphemeral(ephemeral).
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0xf54242).
					SetTitle("❌ Error").
					SetFooter("x3", x3ErrorIcon).
					SetTimestamp(time.Now()).
					SetDescription(toTitle(msg)).
					Build(),
			).
			Build(),
	)
}

// updateInteractionError updates an interaction response with a formatted error message.
func updateInteractionError(event *handler.CommandEvent, msg string) error {
	_, err := event.UpdateInteractionResponse(
		discord.NewMessageUpdateBuilder().
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0xf54242).
					SetTitle("❌ Error").
					SetFooter("x3", x3ErrorIcon).
					SetTimestamp(time.Now()).
					SetDescription(toTitle(msg)).
					Build(),
			).
			Build(),
	)
	return err
}

// sendPrettyError sends a formatted error message as a reply to a regular message.
func sendPrettyError(client bot.Client, msg string, channelID, messageID snowflake.ID) error {
	_, err := client.Rest().CreateMessage(
		channelID,
		discord.NewMessageCreateBuilder().
			SetMessageReferenceByID(messageID).
			SetAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0xf54242).
					SetTitle("❌ Error").
					SetFooter("x3", x3ErrorIcon).
					SetTimestamp(time.Now()).
					SetDescription(toTitle(msg)).
					Build(),
			).
			Build(),
	)
	return err
}

// sendTypingWithLog sends a typing indicator and logs any errors. Runs in a goroutine.
func sendTypingWithLog(client bot.Client, channelID snowflake.ID, wg *sync.WaitGroup) {
	defer wg.Done()
	// typing state lasts for 10s
	if err := client.Rest().SendTyping(channelID); err != nil {
		slog.Error("failed to SendTyping", slog.Any("err", err), slog.String("channel_id", channelID.String()))
	}
}

// toTitle capitalizes the first letter of a string.
func toTitle(str string) string {
	if len(str) == 0 {
		return str
	}
	runes := []rune(str)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// endsWithWhitespace checks if a string ends with a whitespace character.
func endsWithWhitespace(s string) bool {
	if len(s) == 0 {
		return false
	}
	return unicode.IsSpace(rune(s[len(s)-1]))
}

// ftoa converts a float32 to a string.
func ftoa(f float32) string {
	return strconv.FormatFloat(float64(f), 'g', 6, 32)
}

// dtoa converts a float64 to a string.
func dtoa(f float64) string {
	return strconv.FormatFloat(f, 'g', 6, 64)
}

// zifnil returns 0 if the int pointer is nil, otherwise returns the dereferenced value.
func zifnil(val *int) int {
	if val == nil {
		return 0
	}
	return *val
}

// ellipsisTrim trims a string to a maximum length, adding an ellipsis if trimmed.
func ellipsisTrim(s string, length int) string {
	r := []rune(s)
	if len(r) > length {
		return string(r[:length-1]) + "…"
	}
	return s
}

// isModerator checks if the user has moderator permissions.
func isModerator(p discord.Permissions) bool {
	return p.Has(discord.PermissionManageRoles) ||
		p.Has(discord.PermissionAdministrator) ||
		p.Has(discord.PermissionModerateMembers)
}

// pluralize returns the singular or plural form of a word based on the count.
// Handles basic English pluralization rules.
func pluralize(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}

	plural := singular + "s"
	// Handle special cases (basic English rules)
	if strings.HasSuffix(singular, "y") && !strings.ContainsAny(string(singular[len(singular)-2]), "aeiou") {
		plural = singular[:len(singular)-1] + "ies"
	} else if strings.HasSuffix(singular, "ch") || strings.HasSuffix(singular, "sh") || strings.HasSuffix(singular, "x") || strings.HasSuffix(singular, "s") || strings.HasSuffix(singular, "z") {
		plural = singular + "es"
	}

	return fmt.Sprintf("%d %s", count, plural)
}

// sendMessageSplits sends a potentially long message by splitting it into multiple messages if necessary.
// It handles interaction responses vs regular messages and message references.
func sendMessageSplits(
	client bot.Client,
	messageID snowflake.ID, // Message to reply to (0 if not replying or using interaction event)
	event *handler.CommandEvent, // Interaction event (nil if sending regular message)
	flags discord.MessageFlags, // Message flags (e.g., ephemeral)
	channelID snowflake.ID, // Channel to send to
	runes []rune, // Content runes
	files []*discord.File, // Files to attach (only sent with the last split)
	sepFlag bool, // Add an invisible character to the last split to indicate joining needed
) (*discord.Message, error) {
	maxLen := 2000
	if sepFlag {
		maxLen-- // Reserve one char for the separator
	}
	messageLen := len(runes)
	numMessages := (messageLen + maxLen - 1) / maxLen
	if numMessages == 0 && len(files) > 0 { // Handle case where only files are sent
		numMessages = 1
	} else if numMessages == 0 {
		return nil, nil // Nothing to send
	}

	var firstBotMessage *discord.Message // Store the first message sent/updated

	for i := range numMessages {
		start := i * maxLen
		end := min(start+maxLen, messageLen)
		segment := ""
		if start < end { // Avoid creating empty segments if only files are sent
			segment = string(runes[start:end])
		}

		// Add separator to the last message if needed
		if sepFlag && i == numMessages-1 {
			segment += "\u200B" // Zero width space
		}

		// Attach files only to the last message
		currentFiles := []*discord.File{}
		if i == numMessages-1 {
			currentFiles = files
		}

		var message *discord.Message
		var err error

		// Determine how to send: interaction update, reply, or new message
		if i == 0 && event != nil {
			// First message and it's an interaction response
			message, err = event.UpdateInteractionResponse(discord.MessageUpdate{
				Content: &segment,
				Flags:   &flags,
				Files:   currentFiles, // Attach files here if it's the only message
			})
		} else {
			// Subsequent splits or non-interaction message
			builder := discord.NewMessageCreateBuilder().
				SetContent(segment).
				SetFlags(flags). // Flags apply to all splits? Check Discord behavior. Ephemeral likely only works on first.
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				AddFiles(currentFiles...)

			// Add reply reference only to the very first message split if messageID is provided
			if i == 0 && messageID != 0 {
				builder.SetMessageReferenceByID(messageID)
			}

			message, err = client.Rest().CreateMessage(channelID, builder.Build())
		}

		if err != nil {
			// Log error? Return immediately?
			return firstBotMessage, fmt.Errorf("failed to send message split %d: %w", i+1, err)
		}

		// Store the first message object
		if i == 0 {
			firstBotMessage = message
		}

		// If it was an interaction event, subsequent messages must be follow-ups (or regular messages)
		event = nil
		messageID = 0 // Don't reply to the original message on subsequent splits
	}

	return firstBotMessage, nil
}

func extractFilenameFromURL(rawURL string) (string, error) {
	// parse to exclude query params
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL '%s': %w", rawURL, err)
	}

	urlPath := parsedURL.Path
	if urlPath == "" || urlPath == "/" {
		return "", fmt.Errorf("URL '%s' has no path component to extract a filename from", rawURL)
	}

	filename := filepath.Base(urlPath)
	if filename == "." || filename == "/" {
		return "", fmt.Errorf("could not extract a valid filename from path '%s'", urlPath)
	}

	return filename, nil
}

// processImageData fetches or decodes image data and determines a safe filename.
func processImageData(imgSrc string, base string) ([]byte, string, error) {
	if base == "" {
		base = "image"
	}
	var body []byte
	var ext string
	var err error

	if strings.HasPrefix(imgSrc, "https://") || strings.HasPrefix(imgSrc, "http://") {
		resp, err := http.Get(imgSrc)
		if err != nil {
			return nil, "", fmt.Errorf("failed to fetch image URL %s: %w", imgSrc, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("failed to fetch image URL %s: status %d", imgSrc, resp.StatusCode)
		}

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read image body from %s: %w", imgSrc, err)
		}

		// try to get filename from URL
		urlFilename, _ := extractFilenameFromURL(imgSrc)
		if urlFilename != "" {
			ext = filepath.Ext(urlFilename)
		}
	} else {
		// base64 encoded webp
		body, err = base64.StdEncoding.DecodeString(imgSrc)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode base64 image: %w", err)
		}
	}

	if ext == "" {
		ext = ".webp"
	}

	return body, base + ext, nil
}

func makeSpoilerFlag(isSpoiler bool) discord.FileFlags {
	if isSpoiler {
		return discord.FileFlagSpoiler
	}
	return discord.FileFlagsNone
}
