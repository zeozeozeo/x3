package commands

import (
	"bytes"
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

const (
	narratorImageModel = "Nova Anime XL"
)

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
	for content := range strings.SplitSeq(response, "<new_message>") {
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
		slog.Warn("time interaction triggered in empty channel", slog.String("channel_id", channelID.String()))
		return "", errTimeInteractionNoMessages
	}
	if isRegenerate && lastResponseMessage == nil {
		slog.Warn("regenerate called but no previous assistant message found", slog.String("channel_id", channelID.String()))
		return "", errRegenerateNoMessage
	}
	if timeInteraction && userID == 0 {
		if lastUserID == 0 {
			slog.Error("rime interaction failed: cannot determine target user ID", slog.String("channel_id", channelID.String()))
			return "", errors.New("cannot determine target user for time interaction")
		}
		userID = lastUserID // Use the ID of the last user who sent a message
		slog.Debug("inferred user ID for time interaction", slog.String("user_id", userID.String()))
	}
	// --- End Validation ---

	// Get persona using the determined userID
	p := persona.GetPersonaByMeta(cache.PersonaMeta, db.GetMemories(userID), username)

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
		slog.Debug("regenerating: lobotomized history", slog.String("until_id", lastResponseMessage.ID.String()), slog.Int("remaining_messages", llmer.NumMessages()))
	}

	llmer.SetPersona(p)

	// Determine prepend text for the assistant response
	var prepend string
	if isRegenerate && regeneratePrepend != "" {
		prepend = regeneratePrepend
	} else {
		prepend = cache.PersonaMeta.Prepend // Use persona's default prepend if not regenerating with specific prepend
	}

	// --- Generate LLM Response ---
	m := model.GetModelByName(cache.PersonaMeta.Model)
	slog.Debug("requesting LLM completion",
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
			slog.Debug("extracted reasoning", slog.Int("thinking_len", len(thinking)), slog.Int("answer_len", len(response)))
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
	var botMessage *discord.Message
	if isRegenerate {
		// Edit the previous message for regeneration
		response = replaceLlmTagsWithNewlines(response, userID) // Handle tags before sending

		builder := discord.NewMessageUpdateBuilder().SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
		if utf8.RuneCountInString(response) > 2000 {
			// If too long, send as file attachment
			builder.SetContent("")
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

		slog.Debug("updating message for regeneration", slog.String("message_id", lastResponseMessage.ID.String()))
		_, err = client.Rest().UpdateMessage(channelID, lastResponseMessage.ID, builder.Build())
		if err != nil {
			slog.Error("failed to update message for regeneration", slog.Any("err", err))
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
			botMessage, err = sendMessageSplits(client, replyMessageID, nil, 0, channelID, []rune(content), currentFiles, i != len(messages)-1)
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

	{
		// since this function may run for seconds,
		// the persona may have changed, refetch it for good measure
		newCache := db.GetChannelCache(channelID)
		cache.PersonaMeta = newCache.PersonaMeta
	}

	// Update cache and global stats after successful send/edit
	cache.IsLastRandomDM = timeInteraction
	cache.UpdateInteractionTime()
	if err := cache.Write(channelID); err != nil {
		// Log error but don't fail the interaction
		slog.Error("failed to write channel cache after interaction", slog.Any("err", err), slog.String("channel_id", channelID.String()))
	}
	if err := db.UpdateGlobalStats(usage); err != nil {
		// Log error but don't fail the interaction
		slog.Error("failed to update global stats after interaction", slog.Any("err", err))
	}

	// maybe queue narration + generation
	{
		realMeta, _ := persona.GetMetaByName(cache.PersonaMeta.Name)
		disableRandomNarrations := realMeta.DisableImages
		if strings.Contains(response, "<generate_image>") ||
			(!disableRandomNarrations &&
				horder.GetHorder().IsFree() &&
				time.Since(GetNarrator().LastInteractionTime()) > 2*time.Minute) {
			narrationMessageID := messageID
			if botMessage != nil {
				narrationMessageID = botMessage.ID
			}
			handleNarration(client, channelID, narrationMessageID, *llmer)
		} else {
			slog.Info("narrator: skipping narration", slog.Bool("disableImages", disableRandomNarrations), slog.Bool("timeSinceLastInteraction", time.Since(GetNarrator().LastInteractionTime()) > 2*time.Minute), slog.Bool("isFree", horder.GetHorder().IsFree()))
		}
	}

	return jumpURL, nil
}

func handleNarration(client bot.Client, channelID snowflake.ID, messageID snowflake.ID, llmer llm.Llmer) {
	const prepend = "```json\n{\n  \"tags\":"
	GetNarrator().QueueNarration(llmer, prepend, func(llmer *llm.Llmer, response string) {
		response = strings.TrimPrefix(strings.Replace(response, "**", "", 2), prepend)
		replacer := strings.NewReplacer(
			"**", "",
			"_", " ",
			"```json", "",
			"```", "",
		)
		response = replacer.Replace(response)

		// unmarshal json ({"tags": "tag1, tag2, tag3, ..."})
		var t struct {
			Tags string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(response), &t); err != nil {
			// perhaps the model just wanted to yap about something
			slog.Error("narrator: failed to unmarshal json", slog.Any("err", err), slog.String("response", response))
			return
		}

		t.Tags = strings.TrimSpace(t.Tags)
		if t.Tags == "" || !strings.Contains(t.Tags, ",") {
			slog.Info("narrator: model deemed text irrelevant (no tags)", slog.String("response", response))
			return
		}

		slog.Info("narration callback", slog.String("tags", t.Tags))

		go handleNarrationGenerate(client, channelID, messageID, t.Tags)
	})
}

// handleNarrationGenerate generates an image based on LLM-provided tags and sends it as a reply.
func handleNarrationGenerate(client bot.Client, channelID snowflake.ID, messageID snowflake.ID, tags string) {
	if tags == "" {
		slog.Warn("handleNarrationGenerate called with empty tags")
		return
	}

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
	if !slices.ContainsFunc(models, func(model aihorde.ActiveModel) bool {
		return model.Name == narratorImageModel
	}) {
		slog.Warn("narrator image model not available; skipping generation", slog.String("model", narratorImageModel))
		return
	}

	isNSFW := true
	channel, err := client.Rest().GetChannel(channelID)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to get channel details", slog.Any("err", err), slog.String("channel_id", channelID.String()))
	} else if guildChannel, ok := channel.(discord.GuildMessageChannel); ok {
		isNSFW = guildChannel.NSFW()
	}

	model := narratorImageModel
	prompt := tags
	//negativePrompt := "worst quality, low quality, blurry, deformed"
	steps := 20
	n := 1
	cfgScale := 7.0
	clipSkip := 2

	slog.Info("starting narration image generation", slog.String("model", model), slog.String("prompt", prompt), slog.String("channel_id", channelID.String()))

	id, err := h.Generate(model, prompt, "", steps, n, cfgScale, clipSkip, isNSFW)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to start generation", slog.Any("err", err))
		return
	}
	defer h.Done()

	failures := 0
	//i := 0
	for {
		//i++
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

		/*
			if i > 3 && status.WaitTime > 150 {
				// too long, give up
				slog.Warn("narration generation will take too long, giving up", slog.Int("wait_time", status.WaitTime), slog.String("id", id))
				h.Cancel(id)
				return
			}
		*/

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

	// n=1
	imgData, filename, err := processImageData(finalStatus.Generations[0].Img, tags)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to process image data", slog.Any("err", err), slog.String("id", id))
		return
	}

	// send the image as a reply
	_, err = client.Rest().CreateMessage(channelID, discord.NewMessageCreateBuilder().
		SetFiles(&discord.File{Name: filename, Reader: bytes.NewReader(imgData)}).
		SetMessageReference(&discord.MessageReference{MessageID: &messageID, ChannelID: &channelID}).
		SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
		Build())

	if err != nil {
		slog.Error("handleNarrationGenerate: failed to send image reply", slog.Any("err", err), slog.String("channel_id", channelID.String()), slog.String("message_id", messageID.String()))
	} else {
		slog.Info("narration image sent successfully", slog.String("channel_id", channelID.String()), slog.String("message_id", messageID.String()))
	}
}
