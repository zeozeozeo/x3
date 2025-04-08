package commands

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/aihorde-go"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/horder"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

var (
	errTimeInteractionNoMessages = errors.New("empty dm channel for time interaction")
	errRegenerateNoMessage       = errors.New("cannot find last response to regenerate")
)

// replaceLlmTagsWithNewlines replaces <new_message> tags with newlines and handles <memory> tags.
// This is used for post-processing the *full* response, not during streaming splits.
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
	// This function is primarily for extracting memories after the full response is received.
	// The <new_message> splitting for Discord messages is handled during the streaming process itself.
	currentMessage := ""
	remaining := response

	for {
		memStartIdx := strings.Index(remaining, "<memory>")
		memEndIdx := strings.Index(remaining, "</memory>")
		tagStartIdx := strings.Index(remaining, "<new_message>") // Also check for new_message

		// Find the earliest tag
		firstTagIdx := -1
		firstTagType := "" // "memory" or "new_message"

		if memStartIdx != -1 {
			firstTagIdx = memStartIdx
			firstTagType = "memory"
		}
		if tagStartIdx != -1 && (firstTagIdx == -1 || tagStartIdx < firstTagIdx) {
			firstTagIdx = tagStartIdx
			firstTagType = "new_message"
		}

		if firstTagIdx == -1 {
			// No more tags found
			currentMessage += remaining
			break
		}

		// Add content before the tag to the current message part
		currentMessage += remaining[:firstTagIdx]

		if firstTagType == "memory" {
			if memEndIdx != -1 && memStartIdx < memEndIdx {
				// Extract memory
				memory := strings.TrimSpace(remaining[memStartIdx+len("<memory>") : memEndIdx])
				if memory != "" {
					memories = append(memories, memory)
				}
				// Update remaining content
				remaining = remaining[memEndIdx+len("</memory>"):]
			} else {
				// Malformed memory tag, skip it and continue processing
				currentMessage += remaining[memStartIdx : memStartIdx+len("<memory>")] // Keep the opening tag
				remaining = remaining[memStartIdx+len("<memory>"):]
			}
		} else { // firstTagType == "new_message"
			// Finalize the current message part before the tag
			trimmedMsg := strings.TrimSpace(currentMessage)
			if trimmedMsg != "" {
				messages = append(messages, trimmedMsg)
			}
			currentMessage = "" // Start a new message part

			// Update remaining content
			remaining = remaining[tagStartIdx+len("<new_message>"):]
		}
	}

	// Add the last message part if it's not empty
	trimmedMsg := strings.TrimSpace(currentMessage)
	if trimmedMsg != "" {
		messages = append(messages, trimmedMsg)
	}

	// If no messages were extracted but the original response wasn't empty (e.g., only memory tags)
	if len(messages) == 0 && strings.TrimSpace(response) != "" && len(memories) > 0 {
		// This case means the response only contained memory tags or text within them.
		// We don't need to add an empty message here as the primary goal is memory extraction.
	}

	return
}

// handleLlmInteraction2 handles the core logic for generating and sending LLM responses.
func handleLlmInteraction2(
	client bot.Client,
	channelID,
	messageID snowflake.ID, // ID of the triggering message
	content string,
	username string,
	userID snowflake.ID,
	attachments []discord.Attachment,
	timeInteraction bool,
	isRegenerate bool,
	regeneratePrepend string,
	preMsgWg *sync.WaitGroup,
	reference *discord.Message,
) (string, error) {
	cache := db.GetChannelCache(channelID)

	exit, err := handleCard(client, channelID, messageID, cache, preMsgWg)
	if err != nil {
		slog.Error("handleCard failed", slog.Any("err", err), slog.String("channel_id", channelID.String()))
		return "", fmt.Errorf("failed to handle character card: %w", err)
	}
	if exit {
		slog.Debug("handleCard indicated exit", slog.String("channel_id", channelID.String()))
		return "", nil
	}

	llmer := llm.NewLlmer()
	numCtxMessages, usernames, lastResponseMessage, lastAssistantMessageID, lastUserID := addContextMessagesIfPossible(
		client, llmer, channelID, messageID, cache.ContextLength,
	)
	slog.Debug("interaction; added context messages", slog.Int("added", numCtxMessages), slog.Int("count", llmer.NumMessages()))

	// --- Input Validation ---
	if timeInteraction && numCtxMessages == 0 {
		return "", errTimeInteractionNoMessages
	}
	if isRegenerate && lastResponseMessage == nil {
		return "", errRegenerateNoMessage
	}
	if timeInteraction && userID == 0 {
		if lastUserID == 0 {
			return "", errors.New("cannot determine target user for time interaction")
		}
		userID = lastUserID
		slog.Debug("inferred user ID for time interaction", slog.String("user_id", userID.String()))
	}
	// --- End Validation ---

	p := persona.GetPersonaByMeta(cache.PersonaMeta, db.GetMemories(userID), username)

	if content != "" {
		if reference != nil && isRegenerate && reference.ID == lastResponseMessage.ID {
			reference = nil
		}
		if reference != nil && reference.ID == lastAssistantMessageID {
			reference = nil
		}
		llmer.AddMessage(llm.RoleUser, formatMsg(content, username, reference), messageID)
		addImageAttachments(llmer, attachments)
	}

	if isRegenerate {
		llmer.LobotomizeUntilID(lastResponseMessage.ID)
		slog.Debug("regenerating: lobotomized history", slog.String("until_id", lastResponseMessage.ID.String()), slog.Int("remaining_messages", llmer.NumMessages()))
	}

	llmer.SetPersona(p)

	var prepend string
	if isRegenerate && regeneratePrepend != "" {
		prepend = regeneratePrepend
	} else {
		prepend = cache.PersonaMeta.Prepend
	}

	// --- Generate LLM Response (Streaming) ---
	m := model.GetModelByName(cache.PersonaMeta.Model)
	slog.Debug("requesting LLM completion stream", slog.String("model", m.Name), slog.Int("num_messages", llmer.NumMessages()), slog.Bool("is_regenerate", isRegenerate), slog.String("prepend", prepend))

	llmChan, err := llmer.RequestCompletion(m, usernames, cache.PersonaMeta.Settings, prepend)
	if err != nil {
		slog.Error("LLM stream request failed immediately", slog.Any("err", err))
		return "", fmt.Errorf("LLM request failed: %w", err)
	}
	slog.Debug("LLM stream channel received")

	// --- Stream Processing Setup ---
	var (
		responseBuffer        strings.Builder   // Buffer for current message part being built/edited
		fullResponse          strings.Builder   // Accumulates the entire response for post-processing & history
		sentMessageIDs        []snowflake.ID    // IDs of messages sent/edited for this response stream
		lastMessageContentLen int               // Rune count of the last sent/edited message part. Reset to 0 to force new message.
		remainderAfterTag     string            // Stores content after a <new_message> tag from the previous chunk
		finalUsage            llm.Usage         // Usage info from the stream
		streamErr             error             // Error encountered during streaming
		mu                    sync.Mutex        // Mutex to protect shared state accessed by ticker goroutine
		wg                    sync.WaitGroup    // WaitGroup for the update goroutine
		updateInterval        = 3 * time.Second // How often to update Discord
		discordMessageLimit   = 2000            // Discord message rune limit
		isStreamComplete      = false           // Flag to indicate stream has finished
		firstChunkProcessed   = false           // Flag to handle username stripping only on the first content chunk
		firstUpdateComplete   = false           // Flag to ensure initial message is sent/edited before ticker updates
		originalEditMessageID = snowflake.ID(0) // ID of the message to edit if regenerating
	)
	if isRegenerate && lastResponseMessage != nil {
		originalEditMessageID = lastResponseMessage.ID
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	ticker := time.NewTicker(updateInterval)
	defer ticker.Stop()

	// Goroutine to handle Discord updates periodically
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-ticker.C:
				mu.Lock()
				if !isStreamComplete && firstUpdateComplete {
					updateDiscordMessage(client, channelID, &responseBuffer, &sentMessageIDs, &lastMessageContentLen, isRegenerate, originalEditMessageID, messageID, discordMessageLimit, &mu)
				}
				mu.Unlock()
			case <-ctx.Done():
				slog.Debug("Update ticker goroutine cancelled")
				return
			}
		}
	}()

	// --- Consume Stream ---
	slog.Debug("Starting LLM stream consumption loop")
	streamLoopStart := time.Now()
	for chunk := range llmChan {
		mu.Lock()

		if chunk.Err != nil {
			streamErr = chunk.Err
			slog.Error("LLM stream error received", slog.Any("err", streamErr))
			isStreamComplete = true
			mu.Unlock()
			break
		}

		if chunk.Done {
			finalUsage = chunk.Usage
			isStreamComplete = true
			slog.Debug("LLM stream 'Done' chunk received", slog.String("usage", finalUsage.String()))
			mu.Unlock()
			break
		}

		// Process content chunk
		if chunk.Content != "" || remainderAfterTag != "" {
			currentProcessingContent := remainderAfterTag + chunk.Content
			remainderAfterTag = "" // Clear remainder

			// --- Username Stripping (only on first actual content) ---
			if !firstChunkProcessed && fullResponse.Len() == 0 && len(currentProcessingContent) > 0 {
				tempUsernames := usernames
				if tempUsernames == nil {
					tempUsernames = map[string]bool{}
				}
				tempUsernames["x3"] = true
				originalLen := len(currentProcessingContent)
				for username := range tempUsernames {
					prefix := username + ": "
					if len(currentProcessingContent) >= len(prefix) && strings.EqualFold(currentProcessingContent[:len(prefix)], prefix) {
						currentProcessingContent = strings.TrimSpace(currentProcessingContent[len(prefix):])
						slog.Debug("Stripped username prefix", slog.String("prefix", prefix), slog.String("remaining_content", currentProcessingContent))
						break
					}
				}
				if originalLen > 0 {
					firstChunkProcessed = true
				}
			}

			// --- <new_message> Tag Handling Loop ---
			tag := "<new_message>"
			for {
				idx := strings.Index(currentProcessingContent, tag)
				if idx == -1 {
					// No more tags in this part
					if currentProcessingContent != "" {
						responseBuffer.WriteString(currentProcessingContent)
						fullResponse.WriteString(currentProcessingContent) // Add to full history as well
					}
					break // Exit tag loop
				}

				// Tag found
				beforeTag := currentProcessingContent[:idx]
				afterTag := currentProcessingContent[idx+len(tag):]
				slog.Debug("Found <new_message> tag", slog.String("before", beforeTag), slog.String("after", afterTag))

				// Process content before the tag
				if beforeTag != "" {
					responseBuffer.WriteString(beforeTag)
					fullResponse.WriteString(beforeTag) // Add to full history
				}

				// Flush message before the tag
				if responseBuffer.Len() > 0 {
					slog.Debug("Flushing message before <new_message>")
					updateDiscordMessage(client, channelID, &responseBuffer, &sentMessageIDs, &lastMessageContentLen, isRegenerate, originalEditMessageID, messageID, discordMessageLimit, &mu)
					if !firstUpdateComplete {
						firstUpdateComplete = true
					}
				}

				// Signal to start a new message by resetting the length counter
				slog.Debug("Resetting lastMessageContentLen to force new message")
				lastMessageContentLen = 0 // Reset length directly

				// The content after the tag becomes the start of the next processing cycle
				currentProcessingContent = afterTag
			} // End tag processing loop

			// After processing tags, if there's content in the buffer AND we need to start the first message, send it
			if !firstUpdateComplete && responseBuffer.Len() > 0 {
				updateDiscordMessage(client, channelID, &responseBuffer, &sentMessageIDs, &lastMessageContentLen, isRegenerate, originalEditMessageID, messageID, discordMessageLimit, &mu)
				firstUpdateComplete = true
			}
		}

		mu.Unlock()
	}
	slog.Debug("Finished LLM stream consumption loop", slog.Duration("duration", time.Since(streamLoopStart)))

	cancel()
	wg.Wait()
	slog.Debug("Update ticker goroutine finished")

	mu.Lock()
	updateDiscordMessage(client, channelID, &responseBuffer, &sentMessageIDs, &lastMessageContentLen, isRegenerate, originalEditMessageID, messageID, discordMessageLimit, &mu)
	mu.Unlock()
	slog.Debug("Final Discord message update executed")

	if streamErr != nil {
		_, errSend := client.Rest().CreateMessage(channelID, discord.NewMessageCreateBuilder().
			SetContentf("⚠️ Error during response generation: %v", streamErr).
			SetMessageReference(&discord.MessageReference{MessageID: &messageID}).
			SetAllowedMentions(&discord.AllowedMentions{}).Build())
		if errSend != nil {
			slog.Error("Failed to send stream error message", slog.Any("err", errSend))
		}
		return "", fmt.Errorf("LLM stream failed: %w", streamErr)
	}

	// --- Post-Stream Processing ---
	slog.Debug("Starting post-stream processing")
	processedResponse := fullResponse.String() // Use the fully accumulated response

	if timeInteraction && !cache.EverUsedRandomDMs && !isRegenerate {
		processedResponse += interactionReminder
	}

	cache.Usage = cache.Usage.Add(finalUsage)
	cache.LastUsage = finalUsage
	if finalUsage.IsEmpty() {
		slog.Warn("LLM stream finished but usage is empty, estimating...")
		estimatedUsage := llmer.EstimateUsage(m)
		cache.Usage = cache.Usage.Add(estimatedUsage)
		cache.LastUsage = estimatedUsage
	} else if finalUsage.ResponseTokens <= 1 && len(processedResponse) > 10 {
		slog.Warn("LLM stream usage looks unrealistic, estimating response tokens...", slog.Int("api_resp_tokens", finalUsage.ResponseTokens))
		estimatedUsage := llmer.EstimateUsage(m)
		cache.LastUsage.ResponseTokens = estimatedUsage.ResponseTokens
		cache.Usage = cache.Usage.Add(llm.Usage{ResponseTokens: estimatedUsage.ResponseTokens - finalUsage.ResponseTokens})
	}
	slog.Debug("Updated cache usage", slog.String("total_usage", cache.Usage.String()), slog.String("last_usage", cache.LastUsage.String()))

	var thinking string
	if m.Reasoning {
		var answer string
		thinking, answer = llm.ExtractThinking(processedResponse)
		if thinking != "" && answer != "" {
			processedResponse = answer
			slog.Debug("Extracted reasoning", slog.Int("thinking_len", len(thinking)), slog.Int("answer_len", len(processedResponse)))
		}
	}

	// Extract memories from the full response
	_, memories := splitLlmTags(processedResponse) // Only need memories now
	if err := db.HandleMemories(userID, memories); err != nil {
		slog.Error("Failed to handle memories after stream", slog.Any("err", err))
	}
	slog.Debug("Handled memories", slog.Int("count", len(memories)))

	// Perform final post-processing (unescape, trim) on the *full* response
	// Username stripping was handled during streaming.
	finalProcessedResponse := llm.PostProcessResponse(fullResponse.String(), nil, m) // Pass nil for usernames
	slog.Debug("Post-processed final response", slog.Int("original_len", fullResponse.Len()), slog.Int("final_len", len(finalProcessedResponse)))

	llmer.AddToHistory(finalProcessedResponse)
	slog.Debug("Added final response to LLM history")

	var files []*discord.File
	if thinking != "" {
		files = append(files, &discord.File{Name: "reasoning.txt", Reader: strings.NewReader(thinking)})
		slog.Debug("Prepared reasoning file")
		if len(sentMessageIDs) > 0 {
			lastMsgID := sentMessageIDs[len(sentMessageIDs)-1]
			lastMsg, err := client.Rest().GetMessage(channelID, lastMsgID)
			if err == nil {
				// Avoid overwriting content if message is just the reasoning file placeholder
				contentUpdate := lastMsg.Content
				if contentUpdate == "" && len(lastMsg.Attachments) == 0 { // Check if message is empty before adding file
					// If the message was empty, maybe don't set content? Or set a placeholder?
					// For now, let's keep existing content (which might be empty)
				}
				updateBuilder := discord.NewMessageUpdateBuilder().SetContent(contentUpdate).AddFiles(files...)
				_, err = client.Rest().UpdateMessage(channelID, lastMsgID, updateBuilder.Build())
				if err != nil {
					slog.Error("Failed to add reasoning file to last message", slog.Any("err", err), slog.String("message_id", lastMsgID.String()))
				} else {
					slog.Debug("Added reasoning file to last message", slog.String("message_id", lastMsgID.String()))
				}
			} else {
				slog.Error("Failed to get last message to add reasoning file", slog.Any("err", err), slog.String("message_id", lastMsgID.String()))
			}
		}
	}

	if preMsgWg != nil {
		preMsgWg.Wait()
	}

	var jumpURL string
	if isRegenerate && len(sentMessageIDs) > 0 {
		firstMsgID := sentMessageIDs[0]
		ch, err := client.Rest().GetChannel(channelID)
		guildID := snowflake.ID(0)
		if err == nil {
			if gc, ok := ch.(discord.GuildChannel); ok {
				guildID = gc.GuildID()
			}
		}
		if guildID != 0 {
			jumpURL = fmt.Sprintf("https://discord.com/channels/%s/%s/%s", guildID, channelID, firstMsgID)
		} else {
			jumpURL = fmt.Sprintf("https://discord.com/channels/@me/%s/%s", channelID, firstMsgID)
		}
		slog.Debug("Calculated jump URL for regenerate", slog.String("url", jumpURL))
	}

	botMessageIDForNarration := snowflake.ID(0)
	if len(sentMessageIDs) > 0 {
		botMessageIDForNarration = sentMessageIDs[len(sentMessageIDs)-1]
	} else {
		botMessageIDForNarration = messageID
	}

	{
		newCache := db.GetChannelCache(channelID)
		cache.PersonaMeta = newCache.PersonaMeta
	}

	cache.IsLastRandomDM = timeInteraction
	cache.UpdateInteractionTime()
	if err := cache.Write(channelID); err != nil {
		slog.Error("failed to write channel cache after interaction", slog.Any("err", err), slog.String("channel_id", channelID.String()))
	}
	if err := db.UpdateGlobalStats(finalUsage); err != nil {
		slog.Error("failed to update global stats after interaction", slog.Any("err", err))
	}

	{
		realMeta, _ := persona.GetMetaByName(cache.PersonaMeta.Name)
		disableRandomNarrations := realMeta.DisableImages
		if strings.Contains(finalProcessedResponse, "<generate_image>") || (!disableRandomNarrations && horder.GetHorder().IsFree()) {
			handleNarration(client, channelID, botMessageIDForNarration, *llmer)
		} else {
			slog.Info("narrator: skipping narration", slog.Bool("disableImages", disableRandomNarrations), slog.Bool("timeSinceLastInteraction", time.Since(GetNarrator().LastInteractionTime()) > 2*time.Minute), slog.Bool("isFree", horder.GetHorder().IsFree()))
		}
	}

	return jumpURL, nil
}

// updateDiscordMessage handles sending/editing Discord messages based on buffer content and limits.
// It needs to be called with a mutex locked.
func updateDiscordMessage(
	client bot.Client,
	channelID snowflake.ID,
	responseBuffer *strings.Builder,
	sentMessageIDs *[]snowflake.ID,
	lastMessageContentLen *int, // Pointer to store rune count of the last sent part
	isRegenerate bool,
	originalEditMessageID snowflake.ID, // The very first message to edit if regenerating
	replyToMessageID snowflake.ID, // The user message to reply to initially
	limit int,
	mu *sync.Mutex,
) {
	if responseBuffer.Len() == 0 {
		return
	}

	currentContent := strings.TrimSpace(responseBuffer.String())
	if currentContent == "" {
		responseBuffer.Reset()
		return
	}
	currentRunes := utf8.RuneCountInString(currentContent)

	// Determine if a new message should be sent
	shouldSendNew := len(*sentMessageIDs) == 0 || *lastMessageContentLen == 0
	if shouldSendNew {
		if *lastMessageContentLen == 0 && len(*sentMessageIDs) > 0 {
			slog.Debug("updateDiscordMessage: Forcing new message due to lastMessageContentLen reset")
		}

		sendContent := currentContent
		remainingContent := ""
		sendRunes := currentRunes

		if currentRunes > limit {
			sendContent = limitRunes(currentContent, limit) // Use corrected limitRunes
			sendRunes = utf8.RuneCountInString(sendContent) // Recalculate runes after limiting
			// Calculate remaining content based on the limited sendContent
			if len(currentContent) > len(sendContent) {
				// Find the byte boundary corresponding to the rune limit
				byteIndex := 0
				runeCount := 0
				for i := range currentContent {
					runeCount++
					if runeCount > sendRunes { // Find the start of the rune *after* the limit
						byteIndex = i
						break
					}
				}
				if byteIndex > 0 {
					remainingContent = currentContent[byteIndex:]
				} else if len(currentContent) > len(sendContent) {
					// Fallback if byte index calculation failed somehow (shouldn't happen often)
					remainingContent = currentContent[len(sendContent):]
				}
			}
		}

		var sentMsg *discord.Message
		var err error

		// Determine if we edit the original message (only if regenerating and it's the very first message part)
		canEditOriginal := isRegenerate && originalEditMessageID != 0 && len(*sentMessageIDs) == 0

		if canEditOriginal {
			slog.Debug("Streaming: Editing original message", slog.String("id", originalEditMessageID.String()), slog.Int("runes", sendRunes))
			builder := discord.NewMessageUpdateBuilder().
				SetContent(sendContent).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
			sentMsg, err = client.Rest().UpdateMessage(channelID, originalEditMessageID, builder.Build())
			if err != nil {
				slog.Error("Streaming: Failed to edit original message", slog.Any("err", err), slog.String("id", originalEditMessageID.String()))
				return
			}
			*sentMessageIDs = append(*sentMessageIDs, originalEditMessageID)
			*lastMessageContentLen = sendRunes
		} else {
			// Send a new message
			slog.Debug("Streaming: Sending new message", slog.Int("runes", sendRunes))
			builder := discord.NewMessageCreateBuilder().
				SetContent(sendContent).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
			// Only reply to the user's message on the very first message sent by the bot overall
			if replyToMessageID != 0 && len(*sentMessageIDs) == 0 {
				builder.SetMessageReference(&discord.MessageReference{MessageID: &replyToMessageID})
			}
			sentMsg, err = client.Rest().CreateMessage(channelID, builder.Build())
			if err != nil {
				slog.Error("Streaming: Failed to send new message", slog.Any("err", err))
				return
			}
			*sentMessageIDs = append(*sentMessageIDs, sentMsg.ID)
			*lastMessageContentLen = sendRunes
		}

		responseBuffer.Reset()
		responseBuffer.WriteString(remainingContent)

	} else {
		// --- Subsequent edit/send logic ---
		lastMsgID := (*sentMessageIDs)[len(*sentMessageIDs)-1]
		// Add 1 for the space we'll add during edit
		combinedLen := *lastMessageContentLen + currentRunes + 1
		shouldSendNewSplit := false

		if combinedLen <= limit {
			lastMsg, err := client.Rest().GetMessage(channelID, lastMsgID)
			if err != nil {
				slog.Error("Streaming: Failed to get last message for edit, falling back to new message", slog.Any("err", err), slog.String("id", lastMsgID.String()))
				shouldSendNewSplit = true
			} else {
				separator := ""
				if lastMsg.Content != "" { // Only add space if previous content exists
					separator = " "
				}
				newContent := lastMsg.Content + separator + currentContent
				newContentRunes := utf8.RuneCountInString(newContent)

				if newContentRunes <= limit { // Double-check limit after adding space
					slog.Debug("Streaming: Attempting to edit last message", slog.String("id", lastMsgID.String()), slog.Int("new_runes", newContentRunes))
					builder := discord.NewMessageUpdateBuilder().
						SetContent(newContent).
						SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
					_, err = client.Rest().UpdateMessage(channelID, lastMsgID, builder.Build())
					if err == nil {
						*lastMessageContentLen = newContentRunes
						responseBuffer.Reset()
						slog.Debug("Streaming: Edit successful", slog.String("id", lastMsgID.String()))
					} else {
						slog.Warn("Streaming: Edit failed, falling back to new message", slog.Any("err", err), slog.String("id", lastMsgID.String()))
						shouldSendNewSplit = true
					}
				} else {
					slog.Warn("Streaming: Combined length exceeded limit after adding space", slog.Int("combined", combinedLen), slog.Int("new_runes", newContentRunes), slog.String("id", lastMsgID.String()))
					shouldSendNewSplit = true
				}
			}
		} else {
			shouldSendNewSplit = true
		}

		// --- Send New Split Message (if required) ---
		if shouldSendNewSplit {
			sendContent := currentContent
			remainingContent := ""
			sendRunes := currentRunes

			if currentRunes > limit {
				sendContent = limitRunes(currentContent, limit) // Use corrected limitRunes
				sendRunes = utf8.RuneCountInString(sendContent) // Recalculate runes after limiting
				// Calculate remaining content based on the limited sendContent
				if len(currentContent) > len(sendContent) {
					byteIndex := 0
					runeCount := 0
					for i := range currentContent {
						runeCount++
						if runeCount > sendRunes {
							byteIndex = i
							break
						}
					}
					if byteIndex > 0 {
						remainingContent = currentContent[byteIndex:]
					} else if len(currentContent) > len(sendContent) {
						remainingContent = currentContent[len(sendContent):]
					}
				}
			}

			slog.Debug("Streaming: Sending new split message", slog.Int("runes", sendRunes))
			builder := discord.NewMessageCreateBuilder().
				SetContent(sendContent).
				SetAllowedMentions(&discord.AllowedMentions{})

			sentMsg, err := client.Rest().CreateMessage(channelID, builder.Build())
			if err != nil {
				slog.Error("Streaming: Failed to send new split message", slog.Any("err", err))
				return
			}
			*sentMessageIDs = append(*sentMessageIDs, sentMsg.ID)
			*lastMessageContentLen = sendRunes

			responseBuffer.Reset()
			responseBuffer.WriteString(remainingContent)
		}
	}
}

// limitRunes truncates a string to a maximum number of runes.
func limitRunes(s string, n int) string {
	if n <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= n {
		return s
	}

	// Safer approach: iterate runes
	var b strings.Builder
	b.Grow(n) // Approximate size
	count := 0
	for _, r := range s {
		if count >= n {
			break
		}
		b.WriteRune(r)
		count++
	}
	return b.String()
}

const stableNarratorPrepend = "```json\n{\n  \"tags\":"

func parseStableNarratorTags(response string) (string, error) {
	response = strings.TrimPrefix(strings.Replace(response, "**", "", 2), stableNarratorPrepend)
	replacer := strings.NewReplacer(
		"**", "",
		"_", " ",
		"```json", "",
		"```", "",
	)
	response = replacer.Replace(response)

	var t struct {
		Tags string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(response), &t); err != nil {
		slog.Error("narrator: failed to unmarshal json", slog.Any("err", err), slog.String("response", response))
		return "", err
	}

	t.Tags = strings.TrimSpace(t.Tags)
	if t.Tags == "" || !strings.Contains(t.Tags, ",") {
		slog.Info("narrator: model deemed text irrelevant (no tags)", slog.String("response", response))
		return "", fmt.Errorf("narrator: model deemed text irrelevant (no tags)")
	}

	return t.Tags, nil
}

func handleNarration(client bot.Client, channelID snowflake.ID, messageID snowflake.ID, llmer llm.Llmer) {
	GetNarrator().QueueNarration(llmer, stableNarratorPrepend, func(llmer *llm.Llmer, response string) {
		tags, err := parseStableNarratorTags(response)
		if err != nil {
			return
		}
		slog.Info("narration callback", slog.String("tags", tags))
		go handleNarrationGenerate(client, channelID, messageID, tags)
	})
}

// handleNarrationGenerate generates an image based on LLM-provided tags and sends it as a reply.
func handleNarrationGenerate(client bot.Client, channelID snowflake.ID, messageID snowflake.ID, tags string) {
	if tags == "" {
		return
	}

	tags = defaultPromptPrepend + tags

	h := horder.GetHorder()
	if h == nil {
		slog.Error("handleNarrationGenerate: Horder not initialized")
		return
	}
	models, err := h.FetchImageModels()
	if err != nil {
		slog.Error("failed to fetch image models", slog.Any("err", err))
		return
	}
	if !slices.ContainsFunc(models, func(model aihorde.ActiveModel) bool { return model.Name == defaultImageModel }) {
		slog.Warn("narrator image model not available; skipping generation", slog.String("model", defaultImageModel))
		return
	}

	isNSFW := true
	channel, err := client.Rest().GetChannel(channelID)
	if err == nil {
		if textChannel, ok := channel.(discord.GuildMessageChannel); ok {
			isNSFW = textChannel.NSFW()
		}
	}

	model := defaultImageModel
	prompt := tags
	steps := 20
	n := 1
	cfgScale := 7.0
	clipSkip := 2

	isPromptNSFW := horder.IsPromptNSFW(prompt)
	if !isNSFW && isPromptNSFW {
		return
	}

	slog.Info("starting narration image generation", slog.String("model", model), slog.String("prompt", prompt), slog.String("channel_id", channelID.String()))

	id, err := h.Generate(model, prompt, defaultNegativePrompt, steps, n, cfgScale, clipSkip, isNSFW)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to start generation", slog.Any("err", err))
		return
	}
	defer h.Done()

	failures := 0
	for {
		status, err := h.GetStatus(id)
		if err != nil {
			slog.Error("handleNarrationGenerate: failed to get generation status", slog.Any("err", err), slog.String("id", id))
			failures++
			if failures > 8 {
				return
			}
			time.Sleep(5 * time.Second)
			continue
		}

		if status.Done || status.Faulted {
			slog.Info("narration generation finished", slog.Bool("done", status.Done), slog.Bool("faulted", status.Faulted), slog.String("id", id))
			break
		}

		slog.Info("narration generation progress", slog.Int("queue_pos", status.QueuePosition), slog.Int("wait_time", status.WaitTime), slog.String("id", id))
		time.Sleep(5 * time.Second)
	}

	finalStatus, err := h.GetFinalStatus(id)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to get final status", slog.Any("err", err), slog.String("id", id))
		return
	}

	if len(finalStatus.Generations) == 0 || finalStatus.Generations[0].Img == "" {
		slog.Warn("handleNarrationGenerate: no image generated or generation faulted", slog.String("id", id))
		return
	}

	slog.Info("finalStatus generation metadata", slog.Any("metadata", finalStatus.Generations[0].GenMetadata))

	nsfw, csam := horder.GetCensorship(finalStatus.Generations[0])
	if csam {
		slog.Warn("handleNarrationGenerate: generation contains CSAM", slog.String("id", id), slog.String("prompt", prompt))
		return
	}
	if isPromptNSFW {
		nsfw = true
	}
	if !isNSFW && nsfw {
		return
	}

	imgData, filename, err := processImageData(finalStatus.Generations[0].Img, "narration")
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to process image data", slog.Any("err", err), slog.String("id", id))
		return
	}

	_, err = client.Rest().CreateMessage(channelID, discord.NewMessageCreateBuilder().
		SetContentf("-# %s", prompt).
		SetFiles(&discord.File{Name: filename, Reader: bytes.NewReader(imgData), Flags: makeSpoilerFlag(nsfw)}).
		SetMessageReference(&discord.MessageReference{MessageID: &messageID, ChannelID: &channelID}).
		SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
		Build())

	if err != nil {
		slog.Error("handleNarrationGenerate: failed to send image reply", slog.Any("err", err), slog.String("channel_id", channelID.String()), slog.String("message_id", messageID.String()))
	} else {
		slog.Info("handleNarrationGenerate: narration image sent successfully", slog.String("channel_id", channelID.String()), slog.String("message_id", messageID.String()), slog.String("model", model), slog.String("prompt", prompt))
		stats, err := db.GetGlobalStats()
		if err == nil {
			stats.ImagesGenerated++
			if err := stats.Write(); err != nil {
				slog.Error("handleNarrationGenerate: failed to write global stats", slog.Any("err", err))
			}
		} else {
			slog.Error("handleNarrationGenerate: failed to get global stats", slog.Any("err", err))
		}
	}
}
