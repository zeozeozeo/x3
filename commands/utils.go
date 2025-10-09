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

// sendInteractionError sends a formatted error message as a response to a command event
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

// sendTypingWithLog sends a typing indicator and logs any errors.
func sendTypingWithLog(client bot.Client, channelID snowflake.ID, wg *sync.WaitGroup) {
	defer wg.Done()
	// typing state lasts for 10s
	if err := client.Rest().SendTyping(channelID); err != nil {
		slog.Error("failed to SendTyping", "err", err, slog.String("channel_id", channelID.String()))
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
	if strings.HasSuffix(singular, "y") && !strings.ContainsAny(string(singular[len(singular)-2]), "aeiou") {
		plural = singular[:len(singular)-1] + "ies"
	} else if strings.HasSuffix(singular, "ch") || strings.HasSuffix(singular, "sh") || strings.HasSuffix(singular, "x") || strings.HasSuffix(singular, "s") || strings.HasSuffix(singular, "z") {
		plural = singular + "es"
	}

	return fmt.Sprintf("%d %s", count, plural)
}

type messagePart struct {
	Content string
	IsLatex bool
}

const latexAPI = `https://latex.codecogs.com/png.image?\huge&space;\dpi{80}\bg{white}`

func toLatexAPI(equation string) string {
	return latexAPI + url.PathEscape(equation)
}

func pathUnescape(path string) string {
	s, err := url.PathUnescape(path)
	if err != nil {
		return path
	}
	return s
}

func parseMessageForLatex(content string) []messagePart {
	var parts []messagePart
	scanner := 0
	textStart := 0

	for scanner < len(content) {
		if scanner+1 < len(content) && content[scanner:scanner+2] == "$$" {
			if scanner > textStart {
				parts = append(parts, messagePart{Content: content[textStart:scanner], IsLatex: false})
			}
			end := strings.Index(content[scanner+2:], "$$")
			if end == -1 {
				textStart = scanner
				break
			}

			latexContent := content[scanner+2 : scanner+2+end]
			parts = append(parts, messagePart{Content: toLatexAPI(latexContent), IsLatex: true})

			scanner += 2 + end + 2
			textStart = scanner
		} else if content[scanner] == '$' {
			end := -1
			searchStart := scanner + 1
			for {
				next := strings.Index(content[searchStart:], "$")
				if next == -1 {
					break
				}
				if searchStart+next > 0 && content[searchStart+next-1] == '\\' {
					searchStart += next + 1
					continue
				}
				end = searchStart + next
				break
			}
			if end != -1 {
				latexContent := content[scanner+1 : end]
				isValidLatex := len(latexContent) > 0 &&
					!strings.HasPrefix(latexContent, " ") && !strings.HasSuffix(latexContent, " ") &&
					!strings.HasPrefix(latexContent, "\n") && !strings.HasSuffix(latexContent, "\n")
				if isValidLatex {
					if scanner > textStart {
						parts = append(parts, messagePart{Content: content[textStart:scanner], IsLatex: false})
					}
					parts = append(parts, messagePart{Content: toLatexAPI(latexContent), IsLatex: true})
					scanner = end + 1
					textStart = scanner
					continue
				}
			}
			scanner++
		} else {
			scanner++
		}
	}
	if textStart < len(content) {
		parts = append(parts, messagePart{Content: content[textStart:], IsLatex: false})
	}
	return parts
}

func sendMessageSplits(
	client bot.Client,
	messageID snowflake.ID, // message to reply to (0 if not replying or using interaction event)
	event *handler.CommandEvent, // interaction event (nil if sending regular message)
	flags discord.MessageFlags, // message flags (e.g., ephemeral)
	channelID snowflake.ID, // channel to send to
	runes []rune, // content runes
	files []*discord.File, // files to attach (only sent with the last split)
	sepFlag bool, // add an invisible character to the last split to indicate joining needed
) (*discord.Message, error) {
	content := string(runes)
	parts := parseMessageForLatex(content)

	if len(parts) == 0 && len(files) == 0 {
		return nil, nil
	}

	var firstBotMessage *discord.Message
	isFirstMessage := true

	for i, part := range parts {
		currentFiles := []*discord.File{}
		if i == len(parts)-1 {
			currentFiles = files
		}

		// add separator flag only to the very last part
		currentSepFlag := sepFlag && (i == len(parts)-1)

		msg, err := sendTextPart(client, &messageID, &event, flags, channelID, []rune(part.Content), currentFiles, currentSepFlag, &isFirstMessage, part.IsLatex)
		if err != nil {
			return firstBotMessage, err
		}
		if isFirstMessage && msg != nil {
			firstBotMessage = msg
			isFirstMessage = false
		}
	}

	// no content, only files
	if len(parts) == 0 && len(files) > 0 {
		return sendTextPart(client, &messageID, &event, flags, channelID, []rune{}, files, sepFlag, &isFirstMessage, false)
	}

	return firstBotMessage, nil
}

func sendTextPart(
	client bot.Client,
	messageID *snowflake.ID,
	event **handler.CommandEvent,
	flags discord.MessageFlags,
	channelID snowflake.ID,
	runes []rune,
	files []*discord.File,
	sepFlag bool,
	isFirst *bool,
	latex bool,
) (*discord.Message, error) {
	maxLen := 2000
	if sepFlag {
		maxLen-- // reserve one char for the separator
	}
	messageLen := len(runes)
	numMessages := (messageLen + maxLen - 1) / maxLen
	if numMessages == 0 && len(files) > 0 {
		numMessages = 1
	} else if numMessages == 0 {
		return nil, nil
	}

	var firstSentMessage *discord.Message

	for i := 0; i < numMessages; i++ {
		start := i * maxLen
		end := min(start+maxLen, messageLen)
		segment := ""
		if start < end {
			segment = string(runes[start:end])
		}

		// add separator to the last message if needed
		if !latex && sepFlag && i == numMessages-1 {
			segment += "\u200B"
		}

		// attach files only to the last message
		currentFiles := []*discord.File{}
		if i == numMessages-1 {
			currentFiles = files
		}

		var message *discord.Message
		var err error

		if i == 0 && *isFirst && event != nil && *event != nil {
			message, err = (*event).UpdateInteractionResponse(discord.MessageUpdate{
				Content: &segment,
				Flags:   &flags,
				Files:   currentFiles,
			})
		} else {
			builder := discord.NewMessageCreateBuilder().
				SetContent(segment).
				SetFlags(flags).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				AddFiles(currentFiles...)
			if i == 0 && *isFirst && *messageID != 0 {
				builder.SetMessageReferenceByID(*messageID)
			}

			message, err = client.Rest().CreateMessage(channelID, builder.Build())
		}

		if err != nil {
			return firstSentMessage, fmt.Errorf("failed to send message split %d: %w", i+1, err)
		}

		if i == 0 && *isFirst {
			firstSentMessage = message
		}
	}

	if numMessages > 0 {
		*isFirst = false
		if event != nil {
			*event = nil
		}
		*messageID = 0
	}

	return firstSentMessage, nil
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

// ptr is a helper function to create a pointer to a value.
func ptr[T any](v T) *T {
	return &v
}

// isImageAttachment checks if a message attachment is an image.
func isImageAttachment(attachment discord.Attachment) bool {
	return attachment.ContentType != nil && strings.HasPrefix(*attachment.ContentType, "image/")
}
