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
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/aihorde-go"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/horder"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
)

var (
	errTimeInteractionNoMessages = errors.New("empty dm channel for time interaction")
	errRegenerateNoMessage       = errors.New("cannot find last response to regenerate")
)

const (
	generateImageTag    = "<generate_image>"
	memoryUpdatedAppend = "\n-# memory updated"
)

// replaceLlmTagsWithNewlines replaces <new_message> tags with newlines and handles <memory> tags.
// Returns the modified response and a boolean indicating if any <memory> tags were found.
func replaceLlmTagsWithNewlines(response string, userID snowflake.ID) (string, bool) {
	var b strings.Builder
	messages, memories := splitLlmTags(response)
	memoryUpdated := len(memories) > 0
	if err := db.HandleMemories(userID, memories); err != nil {
		slog.Error("failed to handle memories", slog.Any("err", err))
		memoryUpdated = false
		// Continue processing messages even if memory saving fails
	}
	for i, message := range messages {
		b.WriteString(message)
		if i < len(messages)-1 { // Add newline between messages
			b.WriteRune('\n')
		}
	}
	return b.String(), memoryUpdated
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
	event *handler.CommandEvent, // Event to update the interaction of for the first split (optional)
	systemPromptOverride *string, // Optional override for the system prompt
	isImpersonate bool, // Whether this is an impersonate interaction
) (string, snowflake.ID, error) { // (Returns jumpURL if regenerating, otherwise response), the bot message id and error
	cache := db.GetChannelCache(channelID)

	// Handle character card logic first if applicable
	// Note: handleCard might modify the cache (IsFirstMes, NextMes) and writes it.
	exit, err := handleCard(client, channelID, messageID, cache, preMsgWg)
	if err != nil {
		slog.Error("handleCard failed", slog.Any("err", err), slog.String("channel_id", channelID.String()))
		return "", 0, fmt.Errorf("failed to handle character card: %w", err)
	}
	if exit {
		slog.Debug("handleCard indicated exit", slog.String("channel_id", channelID.String()))
		return "", 0, nil // Card message was sent, no further LLM interaction needed now.
	}

	llmer := llm.NewLlmer()
	models := cache.PersonaMeta.GetModels()

	// Fetch surrounding messages for context
	// Note: addContextMessagesIfPossible modifies the llmer by adding messages.
	ctxLen := cache.ContextLength
	if models[0].IsMarkov {
		ctxLen = 70
	}
	numCtxMessages, usernames, lastResponseMessage, lastAssistantMessageID, lastUserID := addContextMessagesIfPossible(
		client,
		llmer,
		channelID,
		messageID,
		ctxLen,
	)
	slog.Debug("interaction; added context messages", slog.Int("added", numCtxMessages), slog.Int("count", llmer.NumMessages()))

	// --- Input Validation and Error Handling ---
	if timeInteraction && numCtxMessages == 0 {
		slog.Warn("time interaction triggered in empty channel", slog.String("channel_id", channelID.String()))
		return "", 0, errTimeInteractionNoMessages
	}
	if isRegenerate && lastResponseMessage == nil {
		slog.Warn("regenerate called but no previous assistant message found", slog.String("channel_id", channelID.String()))
		return "", 0, errRegenerateNoMessage
	}
	if timeInteraction && userID == 0 {
		if lastUserID == 0 {
			slog.Error("time interaction failed: cannot determine target user ID", slog.String("channel_id", channelID.String()))
			return "", 0, errors.New("cannot determine target user for time interaction")
		}
		userID = lastUserID // Use the ID of the last user who sent a message
		slog.Debug("inferred user ID for time interaction", slog.String("user_id", userID.String()))
	}
	// --- End Validation ---

	// Get persona using the determined userID
	p := persona.GetPersonaByMeta(cache.PersonaMeta, db.GetMemories(userID, 0), username)

	// Avoid formatting reply if the reference is the message we're about to regenerate
	if reference != nil && isRegenerate && reference.ID == lastResponseMessage.ID {
		reference = nil
	}
	// Avoid formatting reply if the reference is the last assistant message (prevents self-reply formatting)
	if reference != nil && reference.ID == lastAssistantMessageID {
		reference = nil
	}
	llmer.AddMessage(llm.RoleUser, formatMsg(content, username, reference), messageID)
	addImageAttachments(llmer, attachments)

	// Handle regeneration logic
	if isRegenerate {
		// Remove messages up to (but not including) the message being regenerated
		llmer.LobotomizeUntilID(lastResponseMessage.ID)
		slog.Debug("regenerating: lobotomized history", slog.String("until_id", lastResponseMessage.ID.String()), slog.Int("remaining_messages", llmer.NumMessages()))
	}

	if systemPromptOverride != nil {
		p.System = *systemPromptOverride
	}
	llmer.SetPersona(p)

	// Determine prepend text for the assistant response
	var prepend string
	if regeneratePrepend != "" {
		prepend = regeneratePrepend
	} else {
		prepend = cache.PersonaMeta.Prepend // Use persona's default prepend if not regenerating with specific prepend
	}

	// --- Generate LLM Response ---
	slog.Debug("requesting LLM completion",
		slog.Int("num_models", len(models)),
		slog.Int("num_messages", llmer.NumMessages()),
		slog.Bool("is_regenerate", isRegenerate),
		slog.String("prepend", prepend),
	)
	response, usage, err := llmer.RequestCompletion(models, usernames, cache.PersonaMeta.Settings, prepend)
	if err != nil {
		slog.Error("LLM request failed", slog.Any("err", err))
		return "", 0, fmt.Errorf("LLM request failed: %w", err)
	}
	slog.Debug("LLM response received", slog.Int("response_len", len(response)), slog.String("usage", usage.String()))
	// --- End Generation ---

	// Add random DM reminder if applicable
	if timeInteraction && !cache.EverUsedRandomDMs && !isRegenerate {
		response += interactionReminder
	}

	if isImpersonate {
		// Prefix with a \u200B to detect this inside addContextMessagesIfPossible
		response = "\u200B" + response
	}

	// Update channel usage stats (before potentially erroring out on send)
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage

	// Extract <think> tags if model supports reasoning
	var thinking string
	{
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
	var messages []string
	if isRegenerate {
		// Edit the previous message for regeneration
		var memoryUpdated bool
		response, memoryUpdated = replaceLlmTagsWithNewlines(response, userID) // Handle tags before sending
		if memoryUpdated {
			response += memoryUpdatedAppend
		}

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
			return response, 0, fmt.Errorf("failed to update message: %w", err)
		}
		jumpURL = lastResponseMessage.JumpURL()

	} else {
		// Send new message(s)
		var memories []string
		messages, memories = splitLlmTags(response)
		if err := db.HandleMemories(userID, memories); err != nil {
			// Log error but continue sending messages
			slog.Error("failed to handle memories during send", slog.Any("err", err))
		} else if len(memories) > 0 && len(messages) > 0 {
			messages[len(messages)-1] += memoryUpdatedAppend + memories[0]
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
			botMessage, err = sendMessageSplits(client, replyMessageID, event, 0, channelID, []rune(content), currentFiles, i != len(messages)-1)
			if err != nil {
				slog.Error("failed to send message split", slog.Any("err", err), slog.Int("split_index", i))
				// Attempt to continue sending other splits? Or return error?
				// For now, return the error encountered.
				return response, 0, fmt.Errorf("failed to send message split %d: %w", i+1, err)
			}
			if i == 0 && botMessage != nil {
				firstBotMessage = botMessage // Keep track of the first message sents
			}
			event = nil // updated the event, send the next split as a new message
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
	if !isImpersonate && !models[0].IsMarkov {
		realMeta, _ := persona.GetMetaByName(cache.PersonaMeta.Name)
		disableRandomNarrations := realMeta.DisableImages
		if strings.Contains(response, generateImageTag) ||
			(!disableRandomNarrations && horder.GetHorder().IsFree()) {
			narrationMessageID := messageID
			if botMessage != nil {
				narrationMessageID = botMessage.ID
			}
			content := response
			if len(messages) > 0 {
				content = messages[len(messages)-1]
			}
			handleNarration(client, channelID, narrationMessageID, *llmer, content)
		} else {
			slog.Info("narrator: skipping narration", slog.Bool("disableImages", disableRandomNarrations), slog.Bool("timeSinceLastInteraction", time.Since(GetNarrator().LastInteractionTime()) > 2*time.Minute), slog.Bool("isFree", horder.GetHorder().IsFree()))
		}
	}

	var botMessageID snowflake.ID
	if botMessage != nil {
		botMessageID = botMessage.ID
	}
	if isRegenerate {
		return jumpURL, botMessageID, nil
	}
	return response, botMessageID, nil
}

const stableNarratorPrepend = "```json\n{\n  \"tags\":"

func parseStableNarratorTags(response string) (string, error) {
	//_, response = llm.ExtractThinking(response)
	response = strings.Replace(response, "**", "", 2)
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
		return "", err
	}

	t.Tags = strings.TrimSpace(t.Tags)
	if t.Tags == "" || !strings.Contains(t.Tags, ",") {
		slog.Info("narrator: model deemed text irrelevant (no tags)", slog.String("response", response))
		return "", fmt.Errorf("narrator: model deemed text irrelevant (no tags)")
	}

	return t.Tags, nil
}

func handleNarration(client bot.Client, channelID, messageID snowflake.ID, llmer llm.Llmer, triggerContent string) {
	llmer.LobotomizeKeepLast(4) // keep last 4 turns
	GetNarrator().QueueNarration(llmer, stableNarratorPrepend, func(llmer *llm.Llmer, response string) {
		tags, err := parseStableNarratorTags(response)
		if err != nil {
			return
		}
		slog.Info("narration callback", slog.String("tags", tags))
		go handleNarrationGenerate(client, channelID, messageID, tags, triggerContent)
	})
}

// handleNarrationGenerate generates an image based on LLM-provided tags and sends it as a reply.
func handleNarrationGenerate(client bot.Client, channelID, messageID snowflake.ID, tags, triggerContent string) {
	if tags == "" {
		return
	}

	triggerContent = strings.ReplaceAll(triggerContent, generateImageTag, "")

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
	if !slices.ContainsFunc(models, func(model aihorde.ActiveModel) bool {
		return model.Name == defaultImageModel
	}) {
		slog.Warn("narrator image model not available; skipping generation", slog.String("model", defaultImageModel))
		return
	}

	isNSFW := true
	channel, err := client.Rest().GetChannel(channelID)
	if err == nil {
		if guildChannel, ok := channel.(discord.GuildMessageChannel); ok {
			isNSFW = guildChannel.NSFW()
		}
	}

	model := defaultImageModel
	prompt := tags
	steps := 20
	n := 4
	cfgScale := 7.0
	clipSkip := 2

	isPromptNSFW := horder.IsPromptNSFW(prompt)
	if !isNSFW && isPromptNSFW {
		return // prompt is nsfw, but channel is not
	}

	slog.Info("starting narration image generation", slog.String("model", model), slog.String("prompt", prompt), slog.String("channel_id", channelID.String()))

	id, err := h.Generate(model, prompt, defaultNegativePrompt, steps, n, cfgScale, clipSkip, isNSFW)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to start generation", slog.Any("err", err))
		return
	}
	defer h.Done()

	failures := 0
	dotAnim := 2 // ...
	firstWaitTime := 0
	firstQueuePos := 0
	wasDiffusing := false
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

		firstWaitTime = max(firstWaitTime, status.WaitTime)

		// update <generate_image> tag with a progressbar
		waitTime := status.WaitTime
		if status.Done {
			waitTime = 0
		}
		if status.QueuePosition == 0 && status.WaitTime > 0 {
			wasDiffusing = true
		}
		var bar string
		if status.QueuePosition > 0 {
			if firstQueuePos == 0 {
				firstQueuePos = status.QueuePosition
			}
			bar = progressBar("", firstQueuePos-status.QueuePosition, firstQueuePos, len(generateImageTag)-2)
		} else {
			bar = progressBar("", firstWaitTime-waitTime, firstWaitTime, len(generateImageTag)-2) + "s"
		}

		var updatedContent string
		if wasDiffusing && status.QueuePosition == 0 && status.WaitTime == 0 {
			updatedContent = triggerContent + "\n-# r-esrgan" + strings.Repeat(".", dotAnim%3+1)
			dotAnim++
		} else {
			updatedContent = triggerContent + "\n-# " + bar
		}
		client.Rest().UpdateMessage(
			channelID,
			messageID,
			discord.NewMessageUpdateBuilder().
				SetContent(updatedContent).
				SetAllowedMentions(&discord.AllowedMentions{
					RepliedUser: false,
				}).
				Build(),
		)

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

	if len(finalStatus.Generations) == 0 {
		slog.Warn("handleNarrationGenerate: no image generated or generation faulted", slog.String("id", id))
		return
	}

	var files []*discord.File
	for i, gen := range finalStatus.Generations {
		// the filename MUST start with "narration", that's how addContextMessagesIfPossible knows to ignore it
		imgData, filename, err := processImageData(gen.Img, fmt.Sprintf("narration-%d", i+1))
		if err != nil {
			slog.Error("handleNarrationGenerate: failed to process image data", slog.Any("err", err), slog.String("img_src", gen.Img))
			continue
		}

		nsfw, csam := horder.GetCensorship(gen)
		if csam {
			slog.Warn("handleNarrationGenerate: generation contains CSAM", slog.String("id", id), slog.String("prompt", prompt))
			continue
		}
		if isPromptNSFW {
			nsfw = true
		}
		if nsfw && !isNSFW {
			continue // avoid sending NSFW images
		}

		files = append(files, &discord.File{
			Reader: bytes.NewReader(imgData),
			Name:   filename,
			Flags:  makeSpoilerFlag(nsfw),
		})
	}

	if len(files) == 0 {
		slog.Warn("handleNarrationGenerate: no images generated or generation faulted", slog.String("id", id), slog.String("prompt", prompt))
		return
	}

	// send the image as a reply
	_, err = client.Rest().CreateMessage(channelID, discord.NewMessageCreateBuilder().
		SetContentf("-# %s", prompt).
		SetFiles(files...).
		SetMessageReference(&discord.MessageReference{MessageID: &messageID, ChannelID: &channelID}).
		SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
		Build())

	// remove the progressbar
	client.Rest().UpdateMessage(
		channelID,
		messageID,
		discord.NewMessageUpdateBuilder().
			SetContent(triggerContent).
			SetAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			Build(),
	)

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
