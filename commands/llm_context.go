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

func isLobotomyMessage(msg discord.Message) bool {
	return msg.Interaction != nil &&
		(msg.Interaction.Name == "lobotomy" || msg.Interaction.Name == "persona" || msg.Interaction.Name == "random_dms")
}

func formatMsg(msg, username string, reference *discord.Message) string {
	trimmedRefContent := ""
	if reference != nil {
		trimmedRefContent = strings.TrimSuffix(reference.Content, "\u200B")
	}

	if reference != nil && trimmedRefContent != "" {
		return fmt.Sprintf(
			"<in reply to %s: \"%s\">\n%s: %s",
			reference.Author.EffectiveName(),
			strings.TrimSpace(trimmedRefContent),
			username,
			msg,
		)
	}
	if username != "" {
		return fmt.Sprintf("%s: %s", username, msg)
	}
	return msg
}

// addImageAttachments adds image URLs from attachments.
func addImageAttachments(llmer *llm.Llmer, attachments []discord.Attachment) {
	if attachments == nil {
		return
	}
	for _, attachment := range attachments {
		if isImageAttachment(attachment) {
			llmer.AddImage(attachment.URL)
		}
	}
}

// writeTxtCache saves downloaded text attachment content to a local cache file
func writeTxtCache(attachmentID snowflake.ID, content []byte) error {
	if err := os.MkdirAll("x3-txt-cache", 0755); err != nil {
		return fmt.Errorf("failed to create txt cache directory: %w", err)
	}
	filePath := fmt.Sprintf("x3-txt-cache/%s.txt", attachmentID)
	return os.WriteFile(filePath, content, 0644)
}

// readTxtCache reads text attachment content from the local cache if available
func readTxtCache(attachmentID snowflake.ID) ([]byte, bool) {
	filePath := fmt.Sprintf("x3-txt-cache/%s.txt", attachmentID)
	content, err := os.ReadFile(filePath)
	return content, err == nil
}

// getMessageContent extracts the text content from a message, including text attachments
func getMessageContent(message discord.Message) string {
	content := message.Content

	// rebuild message
	var sb strings.Builder
	sb.Grow(len(content))
	i := 0
	for line := range strings.SplitSeq(content, "\n") {
		if i > 0 {
			sb.WriteRune('\n')
		}
		// handleNarrationGenerate will add progress updates to the end of the messages, strip them
		if !strings.HasPrefix(line, "-# `[") && !strings.HasPrefix(line, "-# r-esrgan") {
			sb.WriteString(line)
		}
		i++
	}

	content = sb.String()

	// process text attachments
	for i, attachment := range message.Attachments {
		if attachment.Filename == "reasoning.txt" {
			continue
		}
		maxSize := 16 * 1024
		if attachment.Size > maxSize {
			continue
		}

		if attachment.ContentType == nil || !strings.Contains(*attachment.ContentType, "text/plain") {
			continue
		}

		if i == 0 && content != "" {
			content += "\n"
		}

		var body []byte
		var readErr error
		// try reading from cache first
		if b, ok := readTxtCache(attachment.ID); ok {
			body = b
		} else {
			// download if not cached
			slog.Info("downloading attachment", slog.String("url", attachment.URL))
			resp, err := http.Get(attachment.URL)
			if err != nil {
				slog.Error("failed to fetch attachment", "err", err, slog.String("url", attachment.URL))
				continue
			}
			defer resp.Body.Close()

			limitedReader := io.LimitReader(resp.Body, int64(maxSize)+1)
			body, readErr = io.ReadAll(limitedReader)
			if readErr != nil {
				slog.Error("failed to read attachment body", slog.Any("err", readErr), slog.String("url", attachment.URL))
				continue
			}
			if len(body) > maxSize {
				slog.Warn("attachment body exceeded size limit after download", slog.String("id", attachment.ID.String()), slog.Int("size", len(body)))
				continue
			}
			if !utf8.Valid(body) {
				slog.Warn("attachment body is not valid utf8", slog.String("id", attachment.ID.String()))
				continue
			}

			// write to cache
			if err := writeTxtCache(attachment.ID, body); err != nil {
				slog.Error("failed to write txt cache", "err", err, slog.String("id", attachment.ID.String()))
			}
		}

		content += string(body)
	}

	// replace mentions with readable names
	replacements := make([]string, 0, len(message.Mentions)+len(message.MentionChannels))
	for _, mention := range message.Mentions {
		replacements = append(replacements,
			mention.Mention(), "@"+mention.EffectiveName())
	}
	for _, channel := range message.MentionChannels {
		replacements = append(replacements,
			fmt.Sprintf("<#%d>", channel.ID), "#"+channel.Name)
	}

	replacer := strings.NewReplacer(replacements...)
	return replacer.Replace(content)
}

func isNarrationMessage(message discord.Message) bool {
	return slices.ContainsFunc(message.Attachments, func(a discord.Attachment) bool {
		return strings.HasPrefix(a.Filename, "narration-") || strings.HasPrefix(a.Filename, "SPOILER_narration-")
	})
}

func fetchMessagesBefore(
	client bot.Client,
	channelID, beforeID snowflake.ID,
	wanted int,
) ([]discord.Message, error) {
	var all []discord.Message
	lastID := beforeID

	for wanted > 0 {
		msgs, err := client.Rest().GetMessages(channelID, 0, lastID, 0, min(wanted, 100))
		if err != nil {
			if len(all) != 0 {
				return all, nil
			}
			return nil, err
		}
		if len(msgs) == 0 {
			break
		}

		all = append(all, msgs...)
		wanted -= len(msgs)
		lastID = msgs[len(msgs)-1].ID
	}

	return all, nil
}

// Returns number of messages fetched, map of usernames, last assistant response message, last assistant message ID, last user ID
func addContextMessages(
	client bot.Client,
	llmer *llm.Llmer,
	channelID,
	messageID snowflake.ID, // The ID of the message *before* which to fetch context
	contextLen int,
) (int, map[string]struct{}, *discord.Message, snowflake.ID, snowflake.ID) {
	if contextLen <= 0 {
		return 0, make(map[string]struct{}), nil, 0, 0
	}

	messages, err := fetchMessagesBefore(client, channelID, messageID, contextLen)
	if err != nil {
		slog.Error("failed to get context messages", "err", err, "channel_id", channelID)
		return 0, make(map[string]struct{}), nil, 0, 0
	}

	latestImageAttachmentIdx := -1
	for i, msg := range messages { // newest to oldest
		if len(msg.Attachments) > 0 {
			if slices.ContainsFunc(msg.Attachments, isImageAttachment) {
				latestImageAttachmentIdx = i
			}
			if latestImageAttachmentIdx == i {
				break // found the newest image
			}
		}
	}

	usernames := make(map[string]struct{})
	var lastResponseMessage *discord.Message
	var lastAssistantMessageID snowflake.ID
	var lastUserID snowflake.ID

	// add messages (oldest to newest)
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		role := llm.RoleUser
		content := ""

		if msg.Author.ID == client.ID() {
			role = llm.RoleAssistant
			lastAssistantMessageID = msg.ID

			if isLobotomyMessage(msg) {
				// for lobotomy messages, delete the context
				llmer.Lobotomize(getLobotomyAmountFromMessage(msg))
				continue
			} else if isCardMessage(msg) {
				// for card messages, extract the actual content after the header
				_, cardContent, found := strings.Cut(msg.Content, "\n\n")
				if found {
					content = cardContent
				} else {
					content = msg.Content
				}
				// remove the trigger message
				llmer.Lobotomize(1)
			} else if msg.EditedTimestamp != nil && strings.HasPrefix(msg.Content, "**") && strings.Count(msg.Content, "**") >= 2 {
				// handle regenerated message with prefill
				content = strings.Replace(msg.Content, "**", "", 2)
			} else if isNarrationMessage(msg) {
				continue // skip narration images from handleNarrationGenerate
			} else {
				content = getMessageContent(msg)
			}

			if msg.ReferencedMessage != nil {
				lastResponseMessage = &msg
			}

			// potential message split
			if strings.HasSuffix(content, "\u200B") {
				content = content + " " + newMessageTag + " "
			}
			if strings.HasPrefix(content, "\u200B") {
				// means impersonate message!
				role = llm.RoleUser
			}
			content = strings.ReplaceAll(content, "\u200B", "")

			// remove appends
			content = strings.TrimSuffix(content, interactionReminder)
			content = strings.TrimSuffix(content, memoryUpdatedAppend)
		} else { // (user message)
			role = llm.RoleUser
			lastUserID = msg.Author.ID

			// check if this user message was an interaction trigger cached previously
			if msg.Interaction != nil {
				interactionPrompt, promptErr := db.GetMessageInteractionPrompt(msg.ID)
				if promptErr == nil && interactionPrompt != "" {
					content = interactionPrompt
				} else {
					content = getMessageContent(msg)
				}
			} else {
				content = getMessageContent(msg)
			}

			reference := msg.ReferencedMessage
			// don't format reply if it refers to the immediately preceding assistant message
			if reference != nil && reference.ID == lastAssistantMessageID {
				reference = nil
			}
			content = formatMsg(content, msg.Author.EffectiveName(), reference)
		}

		if content != "" {
			llmer.AddMessage(role, content, msg.ID)
			usernames[msg.Author.EffectiveName()] = struct{}{} // track username
		}

		// add image attachments if this is the newest message containing one
		if i == latestImageAttachmentIdx {
			addImageAttachments(llmer, msg.Attachments)
		}
	}

	return len(messages), usernames, lastResponseMessage, lastAssistantMessageID, lastUserID
}
