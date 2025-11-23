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
	generateImageTag          = "<generate_image>"
	newMessageTag             = "<new_message>"
	excessiveSplitPunishThres = 5
)

// replaceLlmTagsWithNewlines replaces <new_message> tags with newlines
func replaceLlmTagsWithNewlines(response string, personaMeta *persona.PersonaMeta) string {
	var b strings.Builder
	messages := splitLlmTags(response, personaMeta)
	for i, message := range messages {
		b.WriteString(message)
		if i < len(messages)-1 { // add newline between messages
			b.WriteRune('\n')
		}
	}
	return b.String()
}

// splitLlmTags splits the response by <new_message>
func splitLlmTags(response string, personaMeta *persona.PersonaMeta) (messages []string) {
	defer func() {
		if personaMeta != nil {
			personaMeta.ExcessiveSplit = len(messages) >= excessiveSplitPunishThres
		}
	}()

	hasSplit := strings.Contains(response, newMessageTag)
	for content := range strings.SplitSeq(response, newMessageTag) {
		content = strings.TrimSpace(content)
		if content == "" {
			continue
		}

		if hasSplit {
			// deepseek sometimes does this instead of starting a new message
			messages = append(messages, strings.Split(content, "\n\n")...)
		} else {
			messages = append(messages, content)
		}
	}
	return
}

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
	isDM bool, // Whether this is a DM
) (string, snowflake.ID, error) { // (Returns jumpURL if regenerating, otherwise response), the bot message id and error
	cache := db.GetChannelCache(channelID)

	// might be the first message for a character card
	exit, err := handleCard(client, channelID, messageID, cache, preMsgWg)
	if err != nil {
		slog.Error("handleCard failed", "err", err, slog.String("channel_id", channelID.String()))
		return "", 0, fmt.Errorf("failed to handle character card: %w", err)
	}
	if exit {
		slog.Debug("handleCard indicated exit", slog.String("channel_id", channelID.String()))
		return "", 0, nil // Card message was sent, no further LLM interaction needed now.
	}

	llmer := llm.NewLlmer()
	models := cache.PersonaMeta.GetModels()

	// fetch surrounding messages for context
	ctxLen := cache.ContextLength
	if models[0].IsMarkov {
		ctxLen = 200
	} else if models[0].IsEliza {
		ctxLen = 0
	}
	numCtxMessages, usernames, lastResponseMessage, lastAssistantMessageID, lastUserID := addContextMessages(
		client,
		llmer,
		channelID,
		messageID,
		ctxLen,
	)

	// sanity check
	if timeInteraction && numCtxMessages == 0 {
		slog.Warn("time interaction triggered in empty channel", "channel_id", channelID)
		return "", 0, errTimeInteractionNoMessages
	}
	if isRegenerate && lastResponseMessage == nil {
		slog.Warn("regenerate called but no previous assistant message found", "channel_id", channelID)
		return "", 0, errRegenerateNoMessage
	}
	if timeInteraction && userID == 0 {
		if lastUserID == 0 {
			slog.Error("time interaction failed: cannot determine target user ID", "channel_id", channelID)
			return "", 0, errors.New("cannot determine target user for time interaction")
		}
		userID = lastUserID
	}

	p := persona.GetPersonaByMeta(cache.PersonaMeta, cache.Summary, username, isDM, db.GetInteractionTime(userID), cache.Context)

	// avoid formatting reply if the reference is the message we're about to regenerate
	if reference != nil && isRegenerate && reference.ID == lastResponseMessage.ID {
		reference = nil
	}
	// avoid formatting reply if the reference is the last assistant message
	if reference != nil && reference.ID == lastAssistantMessageID {
		reference = nil
	}
	llmer.AddMessage(llm.RoleUser, formatMsg(content, username, reference), messageID)
	addImageAttachments(llmer, attachments)

	if isRegenerate {
		// remove messages up to (but not including) the message being regenerated
		llmer.LobotomizeUntilID(lastResponseMessage.ID)
		slog.Debug("regenerating: lobotomized history", "until_id", lastResponseMessage.ID, "remaining_messages", llmer.NumMessages())
	}

	if systemPromptOverride != nil {
		p.System = *systemPromptOverride
	}
	llmer.SetPersona(p, &cache.PersonaMeta.ExcessiveSplit)

	var prepend string
	if regeneratePrepend != "" {
		prepend = regeneratePrepend
	} else {
		prepend = cache.PersonaMeta.Prepend
	}

	slog.Info("requesting LLM completion",
		"num_models", len(models),
		"num_messages", llmer.NumMessages(),
		"is_regenerate", isRegenerate,
		"prepend", prepend,
		"isDM", isDM,
	)
	response, usage, err := llmer.RequestCompletion(models, cache.PersonaMeta.Settings, prepend)
	if err != nil {
		slog.Error("LLM request failed", "err", err)
		return "", 0, fmt.Errorf("LLM request failed: %w", err)
	}

	// maybe add random DM reminder
	if timeInteraction && !cache.EverUsedRandomDMs && !isRegenerate {
		response += interactionReminder
	}
	if isImpersonate {
		// prefix with a \u200B to detect this inside addContextMessages
		response = "\u200B" + response
	}

	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage

	// extract <think> tags
	var thinking string
	{
		var answer string
		thinking, answer = llm.ExtractThinking(response)
		if thinking != "" && answer != "" {
			response = answer
			slog.Debug("extracted reasoning", slog.Int("thinking_len", len(thinking)), slog.Int("answer_len", len(response)))
		}
	}

	// prepare to send reasoning trace as a file
	var files []*discord.File
	if thinking != "" {
		files = append(files, &discord.File{
			Name:   "reasoning.txt",
			Reader: strings.NewReader(thinking),
		})
	}

	if preMsgWg != nil {
		preMsgWg.Wait() // wait for e.g., typing indicator to finish
	}

	var jumpURL string
	var botMessage *discord.Message
	var messages []string
	if isRegenerate {
		// edit the previous message for regeneration
		response = replaceLlmTagsWithNewlines(response, &cache.PersonaMeta)

		builder := discord.NewMessageUpdateBuilder().SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
		if utf8.RuneCountInString(response) > 2000 {
			// if too long, send as file attachment
			builder.SetContent("")
			builder.AddFiles(&discord.File{
				Reader: strings.NewReader(response),
				Name:   fmt.Sprintf("response-%v.txt", lastResponseMessage.ID),
			})
			// add reasoning file if it exists
			if len(files) > 0 {
				builder.AddFiles(files...)
			}
		} else {
			builder.SetContent(response)
			if len(files) > 0 {
				builder.AddFiles(files...)
			}
		}

		_, err = client.Rest().UpdateMessage(channelID, lastResponseMessage.ID, builder.Build())
		if err != nil {
			slog.Error("failed to update message for regeneration", "err", err)
			return response, 0, fmt.Errorf("failed to update message: %w", err)
		}
		jumpURL = lastResponseMessage.JumpURL()
	} else {
		messages = splitLlmTags(response, &cache.PersonaMeta)

		for i, content := range messages {
			content = strings.TrimSpace(content)
			if content == "" {
				continue // skip empty splits
			}

			// only attach files to the last split
			currentFiles := []*discord.File{}
			if i == len(messages)-1 {
				currentFiles = files
			}

			// use messageID for reply reference only on the first split
			replyMessageID := snowflake.ID(0)
			if i == 0 {
				replyMessageID = messageID
			}

			// send the split
			botMessage, err = sendMessageSplits(client, replyMessageID, event, 0, channelID, content, currentFiles, i != len(messages)-1, usernames)
			if err != nil {
				slog.Error("failed to send message split", "err", err, slog.Int("split_index", i))
				return response, 0, fmt.Errorf("failed to send message split %d: %w", i+1, err)
			}
			event = nil // updated the event, send the next split as a new message
		}
	}

	{
		// since this function may run for seconds,
		// the persona may have changed, refetch it for good measure
		excessiveSplit := cache.PersonaMeta.ExcessiveSplit
		newCache := db.GetChannelCache(channelID)
		cache.PersonaMeta = newCache.PersonaMeta
		cache.PersonaMeta.ExcessiveSplit = excessiveSplit
	}

	cache.MessagesSinceSummary++
	if cache.MessagesSinceSummary >= 30 {
		// trigger summary generation
		GetNarrator().QueueSummaryGeneration(channelID, *llmer)
		cache.MessagesSinceSummary = 0
	}

	cache.IsLastRandomDM = timeInteraction
	cache.UpdateInteractionTime()
	if err := cache.Write(channelID); err != nil {
		slog.Error("failed to write channel cache after interaction", "err", err, slog.String("channel_id", channelID.String()))
	}
	if err := db.UpdateGlobalStats(usage); err != nil {
		slog.Error("failed to update global stats after interaction", "err", err)
	}
	db.SetInteractionTime(userID, time.Now())

	// maybe queue narration + generation
	if !isImpersonate && !models[0].IsMarkov {
		if !db.IsChannelInImageBlacklist(channelID) &&
			(strings.Contains(response, generateImageTag) ||
				(cache.PersonaMeta.EnableImages && horder.GetHorder().IsFree())) {
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
			slog.Info("narrator: skipping narration", slog.Bool("enableImages", cache.PersonaMeta.EnableImages), slog.Bool("timeSinceLastInteraction", time.Since(GetNarrator().LastInteractionTime()) > 2*time.Minute), slog.Bool("isFree", horder.GetHorder().IsFree()))
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
	// remove garbage from response
	replacer := strings.NewReplacer(
		"**", "",
		"_", " ",
		"```json", "",
		"```", "",
	)
	response = replacer.Replace(response)
	response = strings.TrimSpace(response)

	// try to find the innermost JSON object
	startIdx := strings.LastIndex(response, "{")
	endIdx := strings.LastIndex(response, "}")
	if startIdx != -1 && endIdx != -1 && endIdx > startIdx {
		jsonStr := response[startIdx : endIdx+1]

		// try to parse as {"tags": "..."}
		var t struct {
			Tags string `json:"tags"`
		}
		if err := json.Unmarshal([]byte(jsonStr), &t); err == nil {
			t.Tags = strings.TrimSpace(t.Tags)
			if t.Tags != "" && strings.Contains(t.Tags, ",") {
				return t.Tags, nil
			}
		}

		// if that fails, try to parse as just the tag string directly
		var tags string
		if err := json.Unmarshal([]byte(jsonStr), &tags); err == nil {
			tags = strings.TrimSpace(tags)
			if tags != "" && strings.Contains(tags, ",") {
				return tags, nil
			}
		}
	}

	// if all else fails, try to extract tags by looking for the content after "tags":
	tagsPrefix := `"tags":`
	tagsIdx := strings.Index(response, tagsPrefix)
	if tagsIdx != -1 {
		tagsStr := response[tagsIdx+len(tagsPrefix):]
		// find the closing quote or brace
		endQuote := strings.Index(tagsStr, `"`)
		if endQuote != -1 {
			tags := strings.Trim(tagsStr[:endQuote], ` "`)
			if tags != "" && strings.Contains(tags, ",") {
				return tags, nil
			}
		}
	}

	slog.Error("narrator: failed to parse tags", slog.String("response", response))
	return "", fmt.Errorf("narrator: failed to parse tags")
}

func handleNarration(client bot.Client, channelID, messageID snowflake.ID, llmer llm.Llmer, triggerContent string) {
	llmer.LobotomizeKeepLast(4) // keep last 4 turns
	GetNarrator().QueueNarration(llmer, stableNarratorPrepend, func(llmer *llm.Llmer, response string) {
		tags, err := parseStableNarratorTags(response)
		if err != nil {
			return
		}
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
		slog.Error("failed to fetch image models", "err", err)
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

	id, err := h.Generate(model, prompt, defaultNegativePrompt, steps, n, cfgScale, clipSkip, isNSFW)
	if err != nil {
		slog.Error("handleNarrationGenerate: failed to start generation", "err", err)
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
			slog.Error("handleNarrationGenerate: failed to get generation status", "err", err, slog.String("id", id))
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
		slog.Error("handleNarrationGenerate: failed to get final status", "err", err, slog.String("id", id))
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
			slog.Error("handleNarrationGenerate: failed to process image data", "err", err, slog.String("img_src", gen.Img))
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
		slog.Error("handleNarrationGenerate: failed to send image reply", "err", err, slog.String("channel_id", channelID.String()), slog.String("message_id", messageID.String()))
	} else {
		slog.Info("handleNarrationGenerate: narration image sent successfully", slog.String("channel_id", channelID.String()), slog.String("message_id", messageID.String()), slog.String("model", model), slog.String("prompt", prompt))

		stats, err := db.GetGlobalStats()
		if err == nil {
			stats.ImagesGenerated++
			if err := stats.Write(); err != nil {
				slog.Error("handleNarrationGenerate: failed to write global stats", "err", err)
			}
		} else {
			slog.Error("handleNarrationGenerate: failed to get global stats", "err", err)
		}
	}
}
