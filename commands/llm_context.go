package commands

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"

	"slices"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
)

var lobotomyMessagesRegex = regexp.MustCompile(`Removed last (\d+) messages from the context`)

// getLobotomyAmountFromMessage extracts the number of messages removed from a lobotomy confirmation message.
func getLobotomyAmountFromMessage(msg discord.Message) int {
	matches := lobotomyMessagesRegex.FindStringSubmatch(msg.Content)
	if len(matches) != 2 {
		return 0
	}
	n, err := strconv.Atoi(matches[1])
	if err != nil {
		return 0
	}
	return n
}

// isLobotomyMessage checks if a message is a confirmation from /lobotomy, /persona, or /random_dms.
func isLobotomyMessage(msg discord.Message) bool {
	// These commands might clear context implicitly or explicitly
	return msg.Interaction != nil &&
		(msg.Interaction.Name == "lobotomy" || msg.Interaction.Name == "persona" || msg.Interaction.Name == "random_dms")
}

// formatMsg formats a message content with username and reply context.
func formatMsg(msg, username string, reference *discord.Message) string {
	// Trim the zero-width space sometimes added by sendMessageSplits
	trimmedRefContent := ""
	if reference != nil {
		trimmedRefContent = strings.TrimSuffix(reference.Content, "\u200B")
	}

	if reference != nil && trimmedRefContent != "" { // Only include reply if reference exists and has content
		return fmt.Sprintf(
			"<in reply to %s: \"%s\">\n%s: %s",
			reference.Author.EffectiveName(),
			strings.TrimSpace(trimmedRefContent), // Trim spaces from reference content as well
			username,
			msg,
		)
	}
	return fmt.Sprintf("%s: %s", username, msg)
}

// isImageAttachment checks if a Discord attachment is an image.
func isImageAttachment(attachment discord.Attachment) bool {
	return attachment.ContentType != nil && strings.HasPrefix(*attachment.ContentType, "image/")
}

// addImageAttachments adds image URLs from attachments to the Llmer.
func addImageAttachments(llmer *llm.Llmer, attachments []discord.Attachment) {
	if attachments == nil {
		return
	}
	for _, attachment := range attachments {
		if isImageAttachment(attachment) {
			slog.Debug("adding image attachment to llmer", slog.String("url", attachment.URL))
			llmer.AddImage(attachment.URL)
		}
	}
}

// writeTxtCache saves downloaded text attachment content to a local cache file.
func writeTxtCache(attachmentID snowflake.ID, content []byte) error {
	// Ensure cache directory exists
	if err := os.MkdirAll("x3-txt-cache", 0755); err != nil {
		return fmt.Errorf("failed to create txt cache directory: %w", err)
	}
	filePath := fmt.Sprintf("x3-txt-cache/%s.txt", attachmentID)
	return os.WriteFile(filePath, content, 0644)
}

// readTxtCache reads text attachment content from the local cache if available.
func readTxtCache(attachmentID snowflake.ID) ([]byte, bool) {
	filePath := fmt.Sprintf("x3-txt-cache/%s.txt", attachmentID)
	content, err := os.ReadFile(filePath)
	return content, err == nil // Return true only if read was successful (err is nil)
}

// getMessageContent extracts the text content from a message, including text attachments.
// It respects whitelist status for attachment size limits.
func getMessageContent(message discord.Message, isWhitelisted bool) string {
	content := message.Content

	// Process text attachments
	for i, attachment := range message.Attachments {
		// Check size limits
		maxSize := 16 * 1024 // Default limit
		if isWhitelisted {
			maxSize = 256 * 1024
		}
		if attachment.Size > maxSize {
			slog.Debug("skipping large attachment", slog.String("id", attachment.ID.String()), slog.Int("size", attachment.Size))
			continue
		}

		// Check content type
		if attachment.ContentType == nil || !strings.Contains(*attachment.ContentType, "text/plain") {
			continue
		}

		// Add newline before first attachment content if message has text
		if i == 0 && content != "" {
			content += "\n"
		}

		var body []byte
		var readErr error
		// Try reading from cache first
		if b, ok := readTxtCache(attachment.ID); ok {
			body = b
			slog.Debug("read attachment from cache", slog.String("id", attachment.ID.String()))
		} else {
			// Download if not cached
			slog.Debug("downloading attachment", slog.String("url", attachment.URL))
			resp, err := http.Get(attachment.URL)
			if err != nil {
				slog.Error("failed to fetch attachment", slog.Any("err", err), slog.String("url", attachment.URL))
				continue // Skip this attachment on download error
			}
			defer resp.Body.Close()

			// Limit reader to prevent OOM on massive files that bypass initial size check somehow
			limitedReader := io.LimitReader(resp.Body, int64(maxSize)+1)
			body, readErr = io.ReadAll(limitedReader)
			if readErr != nil {
				slog.Error("failed to read attachment body", slog.Any("err", readErr), slog.String("url", attachment.URL))
				continue // Skip this attachment on read error
			}
			if len(body) > maxSize {
				slog.Warn("attachment body exceeded size limit after download", slog.String("id", attachment.ID.String()), slog.Int("size", len(body)))
				continue // Skip if somehow larger than limit
			}

			// Validate UTF-8
			if !utf8.Valid(body) {
				slog.Warn("attachment body is not valid utf8", slog.String("id", attachment.ID.String()))
				continue // Skip invalid UTF-8
			}

			// Write to cache
			if err := writeTxtCache(attachment.ID, body); err != nil {
				slog.Error("failed to write txt cache", slog.Any("err", err), slog.String("id", attachment.ID.String()))
				// Continue even if caching fails
			}
		}

		// Append downloaded/cached content
		content += string(body)
	}

	// Replace mentions with readable names
	for _, mention := range message.Mentions {
		content = strings.ReplaceAll(content, mention.Mention(), "@"+mention.EffectiveName())
	}
	for _, channel := range message.MentionChannels {
		content = strings.ReplaceAll(content, fmt.Sprintf("<#%d>", channel.ID), "#"+channel.Name)
	}

	return content
}

// getMessageContentNoWhitelist is a convenience wrapper for getMessageContent that checks whitelist status internally.
func getMessageContentNoWhitelist(message discord.Message) string {
	// Assuming isInWhitelist is available in this package or globally
	// If not, this needs adjustment (e.g., pass whitelist status as arg)
	return getMessageContent(message, db.IsInWhitelist(message.Author.ID))
}

func isNarrationMessage(message discord.Message) bool {
	return slices.ContainsFunc(message.Attachments, func(a discord.Attachment) bool {
		return strings.HasPrefix(a.Filename, "narration-")
	})
}

// addContextMessagesIfPossible fetches message history and adds it to the Llmer context.
// Returns: number of messages fetched, map of usernames, last assistant response message, last assistant message ID, last user ID.
func addContextMessagesIfPossible(
	client bot.Client,
	llmer *llm.Llmer,
	channelID,
	messageID snowflake.ID, // The ID of the message *before* which to fetch context
	contextLen int,
) (int, map[string]bool, *discord.Message, snowflake.ID, snowflake.ID) {

	if contextLen <= 0 {
		return 0, make(map[string]bool), nil, 0, 0 // No context requested
	}

	// Fetch messages *before* the given messageID
	messages, err := client.Rest().GetMessages(channelID, 0, messageID, 0, contextLen)
	if err != nil {
		slog.Error("failed to get context messages", slog.Any("err", err), slog.String("channel_id", channelID.String()))
		return 0, make(map[string]bool), nil, 0, 0
	}

	// --- Process fetched messages (newest to oldest) ---
	latestImageAttachmentIdx := -1
	for i, msg := range messages { // newest to oldest
		if len(msg.Attachments) > 0 {
			if slices.ContainsFunc(msg.Attachments, isImageAttachment) {
				latestImageAttachmentIdx = i // Only need the newest one
			}
			if latestImageAttachmentIdx == i {
				break // Found the newest image, no need to check older messages
			}
		}
	}

	usernames := make(map[string]bool)
	var lastResponseMessage *discord.Message
	var lastAssistantMessageID snowflake.ID
	var lastUserID snowflake.ID

	// --- Add messages to Llmer (oldest to newest) ---
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role := llm.RoleUser
		content := "" // Initialize content for this message

		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
			lastAssistantMessageID = msg.ID // Track the last message ID from the bot

			// Handle special bot message types
			if isLobotomyMessage(msg) {
				amount := getLobotomyAmountFromMessage(msg)
				llmer.Lobotomize(amount)
				slog.Debug("handled lobotomy message in context", slog.Int("amount", amount), slog.Int("num_messages", llmer.NumMessages()))
				continue // Don't add the lobotomy confirmation itself to context
			} else if isCardMessage(msg) {
				// For card messages, extract the actual content after the header
				_, cardContent, found := strings.Cut(msg.Content, "\n\n")
				if found {
					content = cardContent
				} else {
					content = msg.Content // Fallback if format is unexpected
				}
				// Remove the user's trigger message that led to this card message
				llmer.Lobotomize(1)
			} else if msg.EditedTimestamp != nil && strings.HasPrefix(msg.Content, "**") && strings.Count(msg.Content, "**") >= 2 {
				// Handle regenerated message with prefill
				content = strings.Replace(msg.Content, "**", "", 2)
			} else if isNarrationMessage(msg) {
				continue // skip narration images from handleNarrationGenerate
			} else {
				content = getMessageContentNoWhitelist(msg) // Get content normally for other bot messages
			}

			// Store the message object if it's a reply target
			if msg.ReferencedMessage != nil {
				lastResponseMessage = &msg
			}

			// Handle potential message splitting indicator (\u200B)
			if strings.HasSuffix(content, "\u200B") {
				content = strings.TrimSuffix(content, "\u200B") + " <new_message> "
			}

			// Remove random DM reminder if present
			content = strings.TrimSuffix(content, interactionReminder)
		} else {
			// --- Process User Message ---
			role = llm.RoleUser
			lastUserID = msg.Author.ID // Track the last user who spoke

			// Check if this user message was an interaction trigger cached previously
			interactionPrompt, promptErr := db.GetMessageInteractionPrompt(msg.ID)
			if promptErr == nil && interactionPrompt != "" {
				// Use the cached interaction prompt instead of the message content
				content = interactionPrompt
				slog.Debug("using cached interaction prompt for message", slog.String("msg_id", msg.ID.String()))
			} else {
				// Get regular message content (including attachments)
				content = getMessageContentNoWhitelist(msg)
			}

			// Format with username and reply context (if applicable)
			reference := msg.ReferencedMessage
			// Don't format reply if it refers to the immediately preceding assistant message
			if reference != nil && reference.ID == lastAssistantMessageID {
				reference = nil
			}
			content = formatMsg(content, msg.Author.EffectiveName(), reference)
		}

		// Add the processed message to the llmer
		if content != "" { // Avoid adding empty messages
			llmer.AddMessage(role, content, msg.ID)
			usernames[msg.Author.EffectiveName()] = true // Track username
		}

		// Add image attachments if this is the newest message containing one
		if i == latestImageAttachmentIdx {
			addImageAttachments(llmer, msg.Attachments)
		}
	}

	return len(messages), usernames, lastResponseMessage, lastAssistantMessageID, lastUserID
}
