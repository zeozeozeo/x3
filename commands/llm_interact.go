package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"

	// "time" // Removed unused import
	"unicode"
	"unicode/utf8"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

var (
	errTimeInteractionNoMessages = errors.New("empty dm channel for time interaction")
	errRegenerateNoMessage       = errors.New("cannot find last response to regenerate")
)

// endsWithWhitespace checks if a string ends with a whitespace character.
func endsWithWhitespace(s string) bool {
	if len(s) == 0 {
		return false
	}
	return unicode.IsSpace(rune(s[len(s)-1]))
}

// replaceLlmTagsWithNewlines replaces <new_message> tags with newlines and handles <memory> tags.
func replaceLlmTagsWithNewlines(response string, userID snowflake.ID) string {
	var b strings.Builder
	messages, memories := splitLlmTags(response)
	if err := db.HandleMemories(userID, memories); err != nil {
		slog.Error("failed to handle memories", slog.Any("err", err))
		// Continue processing messages even if memory saving fails
	}
	for i, message := range messages {
		b.WriteString(message)
		if i < len(messages)-1 { // Add newline between messages
			b.WriteRune('\n')
		}
	}
	return b.String()
}

// splitLlmTags splits the response by <new_message> and extracts <memory> tags.
func splitLlmTags(response string) (messages []string, memories []string) {
	parts := strings.Split(response, "<new_message>")
	for _, content := range parts {
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		startIdx := strings.Index(content, "<memory>")
		endIdx := strings.Index(content, "</memory>")

		if startIdx != -1 && endIdx != -1 && startIdx < endIdx {
			// Extract memory
			memory := strings.TrimSpace(content[startIdx+len("<memory>") : endIdx])
			if memory != "" {
				memories = append(memories, memory)
			}

			// Extract content before and after memory tag
			beforeMemory := strings.TrimSpace(content[:startIdx])
			afterMemory := strings.TrimSpace(content[endIdx+len("</memory>"):])

			if beforeMemory != "" {
				messages = append(messages, beforeMemory)
			}
			if afterMemory != "" {
				// Recursively split the part after memory in case of multiple tags
				subMessages, subMemories := splitLlmTags(afterMemory)
				messages = append(messages, subMessages...)
				memories = append(memories, subMemories...)
			}
		} else {
			messages = append(messages, content)
		}
	}
	return
}

// handleLlmInteraction2 handles the core logic for generating and sending LLM responses.
// It takes various parameters to control context, regeneration, and interaction type.
// If isRegenerate is true and err is nil, the first return is the jump url to the edited message.
// Doesn't call SendTyping!
func handleLlmInteraction2(
	client bot.Client,
	channelID,
	messageID snowflake.ID, // ID of the triggering message (user message or interaction)
	content string, // Content of the triggering message (can be empty for regenerate/time interaction)
	username string, // Username of the triggering user
	userID snowflake.ID, // ID of the triggering user
	attachments []discord.Attachment, // Attachments from the triggering message
	timeInteraction bool, // Whether this is a proactive time-based interaction
	isRegenerate bool, // Whether to regenerate the last response
	regeneratePrepend string, // Text to prepend to the regenerated response
	preMsgWg *sync.WaitGroup, // WaitGroup to wait on before sending the message (e.g., for typing indicator)
	reference *discord.Message, // Message being replied to (if any)
) (string, error) { // Returns jumpURL (if regenerating) and error
	cache := db.GetChannelCache(channelID)

	// Handle character card logic first if applicable
	// Note: handleCard might modify the cache (IsFirstMes, NextMes) and writes it.
	exit, err := handleCard(client, channelID, messageID, cache, preMsgWg)
	if err != nil {
		slog.Error("handleCard failed", slog.Any("err", err), slog.String("channel_id", channelID.String()))
		return "", fmt.Errorf("failed to handle character card: %w", err)
	}
	if exit {
		slog.Debug("handleCard indicated exit", slog.String("channel_id", channelID.String()))
		return "", nil // Card message was sent, no further LLM interaction needed now.
	}

	llmer := llm.NewLlmer()

	// Fetch surrounding messages for context
	// Note: addContextMessagesIfPossible modifies the llmer by adding messages.
	numCtxMessages, usernames, lastResponseMessage, lastAssistantMessageID, lastUserID := addContextMessagesIfPossible(
		client,
		llmer,
		channelID,
		messageID,
		cache.ContextLength,
	)
	slog.Debug("interaction; added context messages", slog.Int("added", numCtxMessages), slog.Int("count", llmer.NumMessages()))

	// --- Input Validation and Error Handling ---
	if timeInteraction && numCtxMessages == 0 {
		slog.Warn("Time interaction triggered in empty channel", slog.String("channel_id", channelID.String()))
		return "", errTimeInteractionNoMessages
	}
	if isRegenerate && lastResponseMessage == nil {
		slog.Warn("Regenerate called but no previous assistant message found", slog.String("channel_id", channelID.String()))
		return "", errRegenerateNoMessage
	}
	if timeInteraction && userID == 0 {
		if lastUserID == 0 {
			slog.Error("Time interaction failed: cannot determine target user ID", slog.String("channel_id", channelID.String()))
			return "", errors.New("cannot determine target user for time interaction")
		}
		userID = lastUserID // Use the ID of the last user who sent a message
		slog.Debug("Inferred user ID for time interaction", slog.String("user_id", userID.String()))
	}
	// --- End Validation ---

	// Get persona using the determined userID
	persona := persona.GetPersonaByMeta(cache.PersonaMeta, db.GetMemories(userID), username)

	// Add the current user message/interaction content if provided
	if content != "" {
		// Avoid formatting reply if the reference is the message we're about to regenerate
		if reference != nil && isRegenerate && reference.ID == lastResponseMessage.ID {
			reference = nil
		}
		// Avoid formatting reply if the reference is the last assistant message (prevents self-reply formatting)
		if reference != nil && reference.ID == lastAssistantMessageID {
			reference = nil
		}
		llmer.AddMessage(llm.RoleUser, formatMsg(content, username, reference), messageID)
		addImageAttachments(llmer, attachments) // Add attachments from the *current* message
	}

	// Handle regeneration logic
	if isRegenerate {
		// Remove messages up to (but not including) the message being regenerated
		llmer.LobotomizeUntilID(lastResponseMessage.ID)
		slog.Debug("Regenerating: Lobotomized history", slog.String("until_id", lastResponseMessage.ID.String()), slog.Int("remaining_messages", llmer.NumMessages()))
	}

	llmer.SetPersona(persona)

	// Determine prepend text for the assistant response
	var prepend string
	if isRegenerate && regeneratePrepend != "" {
		prepend = regeneratePrepend
	} else {
		prepend = cache.PersonaMeta.Prepend // Use persona's default prepend if not regenerating with specific prepend
	}

	// --- Generate LLM Response ---
	m := model.GetModelByName(cache.PersonaMeta.Model)
	slog.Debug("Requesting LLM completion",
		slog.String("model", m.Name),
		slog.Int("num_messages", llmer.NumMessages()),
		slog.Bool("is_regenerate", isRegenerate),
		slog.String("prepend", prepend),
	)
	response, usage, err := llmer.RequestCompletion(m, usernames, cache.PersonaMeta.Settings, prepend)
	if err != nil {
		slog.Error("LLM request failed", slog.Any("err", err))
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	slog.Debug("LLM response received", slog.Int("response_len", len(response)), slog.String("usage", usage.String()))
	// --- End Generation ---

	// Add random DM reminder if applicable
	if timeInteraction && !cache.EverUsedRandomDMs && !isRegenerate {
		response += interactionReminder
	}

	// Update channel usage stats (before potentially erroring out on send)
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage

	// Extract <think> tags if model supports reasoning
	var thinking string
	if m.Reasoning {
		var answer string
		thinking, answer = llm.ExtractThinking(response)
		if thinking != "" && answer != "" {
			response = answer
			slog.Debug("Extracted reasoning", slog.Int("thinking_len", len(thinking)), slog.Int("answer_len", len(response)))
		}
	}

	// Prepare reasoning file if present
	var files []*discord.File
	if thinking != "" {
		files = append(files, &discord.File{
			Name:   "reasoning.txt",
			Reader: strings.NewReader(thinking),
		})
	}

	// --- Send Response ---
	if preMsgWg != nil {
		preMsgWg.Wait() // Wait for e.g., typing indicator to finish
	}

	var jumpURL string
	if isRegenerate {
		// Edit the previous message for regeneration
		response = replaceLlmTagsWithNewlines(response, userID) // Handle tags before sending

		builder := discord.NewMessageUpdateBuilder().SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
		if utf8.RuneCountInString(response) > 2000 {
			// If too long, send as file attachment
			builder.SetContent("Response too long, see attached file.") // Clear original content
			builder.AddFiles(&discord.File{
				Reader: strings.NewReader(response),
				Name:   fmt.Sprintf("response-%v.txt", lastResponseMessage.ID),
			})
			// Add reasoning file if it exists
			if len(files) > 0 {
				builder.AddFiles(files...)
			}
		} else {
			builder.SetContent(response)
			// Attach reasoning file if it exists
			if len(files) > 0 {
				builder.AddFiles(files...)
			}
		}

		slog.Debug("Updating message for regeneration", slog.String("message_id", lastResponseMessage.ID.String()))
		_, err = client.Rest().UpdateMessage(channelID, lastResponseMessage.ID, builder.Build())
		if err != nil {
			slog.Error("Failed to update message for regeneration", slog.Any("err", err))
			return "", fmt.Errorf("failed to update message: %w", err)
		}
		jumpURL = lastResponseMessage.JumpURL()

	} else {
		// Send new message(s)
		messages, memories := splitLlmTags(response)
		if err := db.HandleMemories(userID, memories); err != nil {
			// Log error but continue sending messages
			slog.Error("failed to handle memories during send", slog.Any("err", err))
		}

		var firstBotMessage *discord.Message
		for i, content := range messages {
			content = strings.TrimSpace(content)
			if content == "" {
				continue // Skip empty splits
			}

			// Only attach files to the last message split
			currentFiles := []*discord.File{}
			if i == len(messages)-1 {
				currentFiles = files
			}

			// Use messageID for reply reference only on the first split
			replyMessageID := snowflake.ID(0)
			if i == 0 {
				replyMessageID = messageID
			}

			// Send the split
			botMessage, err := sendMessageSplits(client, replyMessageID, nil, 0, channelID, []rune(content), currentFiles, i != len(messages)-1)
			if err != nil {
				slog.Error("failed to send message split", slog.Any("err", err), slog.Int("split_index", i))
				// Attempt to continue sending other splits? Or return error?
				// For now, return the error encountered.
				return "", fmt.Errorf("failed to send message split %d: %w", i+1, err)
			}
			if i == 0 && botMessage != nil {
				firstBotMessage = botMessage // Keep track of the first message sent
			}
		}
		// If the first message was sent successfully, use its jump URL if needed elsewhere (though not returned here)
		if firstBotMessage != nil {
			// jumpURL = firstBotMessage.JumpURL() // Not returned for non-regenerate
		}
	}
	// --- End Send ---

	// Update cache and global stats after successful send/edit
	cache.IsLastRandomDM = timeInteraction
	cache.UpdateInteractionTime()
	if err := cache.Write(channelID); err != nil {
		// Log error but don't fail the interaction
		slog.Error("Failed to write channel cache after interaction", slog.Any("err", err), slog.String("channel_id", channelID.String()))
	}
	if err := db.UpdateGlobalStats(usage); err != nil {
		// Log error but don't fail the interaction
		slog.Error("Failed to update global stats after interaction", slog.Any("err", err))
	}

	return jumpURL, nil
}
