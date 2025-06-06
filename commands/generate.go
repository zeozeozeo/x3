package commands

import (
	"bytes"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/horder"
	"github.com/zeozeozeo/x3/llm"
)

var GenerateCommand = discord.SlashCommandCreate{
	Name:        "generate",
	Description: "Generate an image",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
		discord.InteractionContextTypeBotDM,
		discord.InteractionContextTypePrivateChannel,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "prompt",
			Description: "Prompt to use for this image",
			Required:    true,
		},
		discord.ApplicationCommandOptionString{
			Name:         "model",
			Description:  "Model to use for this image",
			Autocomplete: true,
			Required:     false,
		},
		discord.ApplicationCommandOptionString{
			Name:        "negative",
			Description: "Negative prompt to use for this image",
			Required:    false,
		},
		discord.ApplicationCommandOptionInt{
			Name:        "steps",
			Description: "Number of steps to use for this image (20..50 is recommended)",
			Required:    false,
			MinValue:    ptr(0),
		},
		discord.ApplicationCommandOptionInt{
			Name:        "n",
			Description: "Number of images to generate (1..10)",
			MinValue:    ptr(1),
			MaxValue:    ptr(10),
		},
		discord.ApplicationCommandOptionFloat{
			Name:        "cfg_scale",
			Description: "How close the image should be to the prompt (7..14 is recommended)",
			MinValue:    ptr(0.0),
		},
		discord.ApplicationCommandOptionInt{
			Name:        "clip_skip",
			Description: "CLIP layer to skip to (use 2 for Pony)",
			MinValue:    ptr(1),
		},
		discord.ApplicationCommandOptionBool{
			Name:        "auto_tag",
			Description: "Automatically convert the prompt to a sequence of Danbooru tags",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "ephemeral",
			Description: "If the response should only be visible to you",
			Required:    false,
		},
	},
}

const (
	defaultNegativePrompt = "modern, recent, old, oldest, cartoon, graphic, text, painting, crayon, graphite, abstract, glitch, deformed, mutated, ugly, disfigured, long body, lowres, bad anatomy, bad hands, missing fingers, extra fingers, extra digits, fewer digits, cropped, very displeasing, (worst quality, bad quality:1.2), sketch, jpeg artifacts, signature, watermark, username, (censored, bar_censor, mosaic_censor:1.2), simple background, conjoined, bad, ai-generated"
	defaultPromptPrepend  = "masterpiece, best quality, amazing quality, very aesthetic, high resolution, ultra-detailed, absurdres, newest, scenery, "
	defaultImageModel     = "Nova Anime XL"
)

func progressBar(message string, current, total, barWidth int) string {
	if total <= 0 {
		total = 1
		current = 0
	}
	if current < 0 {
		current = 0
	}
	if current > total {
		current = total
	}
	if barWidth <= 0 {
		barWidth = 50
	}

	percentage := float64(current) / float64(total)
	filledWidth := int(percentage * float64(barWidth))
	filledPart := strings.Repeat("=", filledWidth)
	emptyPart := strings.Repeat(" ", barWidth-filledWidth)

	bar := ""
	if filledWidth > 0 && filledWidth < barWidth {
		filledPart = strings.Repeat("=", filledWidth-1)
		bar = fmt.Sprintf("[%s>%s]", filledPart, emptyPart)
	} else if filledWidth == barWidth {
		bar = fmt.Sprintf("[%s]", filledPart)
	} else {
		bar = fmt.Sprintf("[%s]", emptyPart)
	}

	return fmt.Sprintf("%s`%s` %d/%d", message, bar, current, total)
}

// HandleGenerate handles the /generate command
func HandleGenerate(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	m := data.String("model")
	prompt := data.String("prompt")
	negative := data.String("negative")
	steps := data.Int("steps")
	n := data.Int("n")
	cfgScale := data.Float("cfg_scale")
	clipSkip := data.Int("clip_skip")
	autoTag, hasAutoTag := data.OptBool("auto_tag")
	ephemeral := data.Bool("ephemeral")
	isNSFW := true // in dms

	// default values
	if steps == 0 {
		steps = 20
	}
	if cfgScale == 0 {
		cfgScale = 7
	}
	if clipSkip == 0 {
		clipSkip = 2
	}
	if m == "" {
		m = defaultImageModel
	}
	isImplicitAutoTag := false
	if !hasAutoTag && m != "Flux.1-Schnell fp8 (Compact)" && !strings.Contains(prompt, ",") {
		// no commas = likely no tags, need to auto-tag
		autoTag = true
		isImplicitAutoTag = true
	}
	n = max(n, 1)

	// check if its an nsfw channel; if it's not we'll rely on stablehorde to censor nsfw content
	channel, err := event.Client().Rest().GetChannel(event.Channel().ID())
	if err == nil {
		if guildChannel, ok := channel.(discord.GuildMessageChannel); ok {
			isNSFW = guildChannel.NSFW()
		}
	}

	if err := event.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	if autoTag {
		if negative == "" {
			negative = defaultNegativePrompt
		}
		tagChan := make(chan string)

		llmer := llm.NewLlmer()
		llmer.AddMessage(llm.RoleUser, prompt, 0)
		GetNarrator().QueueNarration(*llmer, stableNarratorPrepend, func(llmer *llm.Llmer, response string) {
			tags, err := parseStableNarratorTags(response)
			if err != nil {
				tagChan <- ""
				return
			}
			slog.Info("HandleGenerate: got prompt improvement narrator tags", slog.String("tags", tags))
			tagChan <- tags
		})

		msg := "Auto-tagging prompt..."
		if isImplicitAutoTag {
			msg = "Auto-tagging prompt (no Danbooru tags detected)..."
		}
		event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetContent(msg).
			Build())

		tags := <-tagChan
		if tags == "" {
			return updateInteractionError(event, "failed to auto-tag prompt, try again later or with a different prompt")
		}
		prompt = defaultPromptPrepend + tags
	}

	isPromptNSFW := horder.IsPromptNSFW(prompt)
	if isPromptNSFW && !isNSFW {
		return updateInteractionError(event, "prompt contains NSFW content (this channel is not NSFW)")
	}

	id, err := horder.GetHorder().Generate(m, prompt, negative, steps, n, cfgScale, clipSkip, isNSFW)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}
	defer horder.GetHorder().Done() // decrements the active request counter

	failures := 0
	firstQueuePos := 0
	firstWaitTime := 0
	impossibleWaitTime := 30
	impossibleFail := false
	numDots := 2
	wasDiffusing := false
	diffuseStart := time.Time{}
	for {
		numDots++
		status, err := horder.GetHorder().GetStatus(id)
		if err != nil {
			if failures >= 10 {
				return updateInteractionError(event, err.Error())
			}
			slog.Error("HandleGenerate: failed to query progress", slog.Any("err", err))
			failures++
			continue
		}

		if status.Done || status.Faulted {
			break
		}

		message := "Waiting for job to start"
		if autoTag {
			message += fmt.Sprintf(" (improved prompt: `%s`)", prompt)
		}
		addETA := false
		if wasDiffusing {
			message = "Final touches"
		}
		message += strings.Repeat(".", numDots)
		if status.Processing != 0 && status.Restarted != 0 {
			message = fmt.Sprintf("Waiting for %s; %d restarted", pluralize(status.Processing, "job"), status.Restarted)
		}
		if status.QueuePosition > 0 {
			numDots = 2
			addETA = true
			if firstQueuePos == 0 {
				firstQueuePos = status.QueuePosition
			}
			message = progressBar("Queued: ", firstQueuePos-status.QueuePosition, firstQueuePos, 25)
		} else if status.WaitTime > 0 {
			numDots = 2
			addETA = true
			if !wasDiffusing {
				diffuseStart = time.Now()
			}
			wasDiffusing = true
			firstWaitTime = max(firstWaitTime, status.WaitTime)
			message = progressBar("Diffusing: ", firstWaitTime-status.WaitTime, firstWaitTime, 25)
		} else if !status.IsPossible {
			numDots = 2
			addETA = false
			if impossibleWaitTime == 0 {
				impossibleFail = true
				break
			}
			message = fmt.Sprintf("Generation is impossible with current pool of workers. Waiting %ds", impossibleWaitTime)
			impossibleWaitTime--
		}

		if addETA {
			message += fmt.Sprintf(" (eta: %ds)", status.WaitTime)
		}

		slog.Info(
			"HandleGenerate: progress",
			slog.Int("queue_pos", status.QueuePosition),
			slog.Int("wait_time", status.WaitTime),
			slog.Bool("done", status.Done),
			slog.Bool("faulted", status.Faulted),
			slog.Float64("kudos", status.Kudos),
			slog.String("id", id),
		)
		event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetContent(message).
			SetContainerComponents(
				discord.NewActionRow(
					discord.ButtonComponent{
						Style: discord.ButtonStyleSecondary,
						Emoji: &discord.ComponentEmoji{
							Name: "❌",
						},
						CustomID: fmt.Sprintf("/cancel/%s:%d", id, event.User().ID),
					},
				),
			).
			Build())

		time.Sleep(3 * time.Second)
	}

	if impossibleFail {
		return event.DeleteInteractionResponse()
	}

	finalStatus, err := horder.GetHorder().GetFinalStatus(id)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	if len(finalStatus.Generations) == 0 {
		return event.DeleteInteractionResponse()
	}

	diffuseElapsed := time.Since(diffuseStart)

	var files []*discord.File
	for i, gen := range finalStatus.Generations {
		imgData, filename, err := processImageData(gen.Img, fmt.Sprintf("%d", i+1))
		if err != nil {
			slog.Error("HandleGenerate: failed to process image data", slog.Any("err", err), slog.String("img_src", gen.Img))
			continue
		}

		nsfw, csam := horder.GetCensorship(gen)
		if csam {
			slog.Warn("HandleGenerate: generation contains CSAM", slog.String("id", id), slog.String("prompt", prompt))
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
		return event.DeleteInteractionResponse()
	}

	var sb strings.Builder
	sb.WriteString("Image")
	if len(files) > 1 {
		sb.WriteRune('s')
	}
	sb.WriteString(" generated by model `")
	sb.WriteString(m)
	sb.WriteString("` in ")
	sb.WriteString(fmt.Sprintf("%.1fs", diffuseElapsed.Seconds()))
	sb.WriteString(".\n### Prompt:\n```\n")
	sb.WriteString(prompt)
	sb.WriteString("\n```")
	if negative != "" {
		sb.WriteString("\n### Negative prompt:\n```\n")
		sb.WriteString(negative)
		sb.WriteString("\n```")
	}
	sb.WriteString(fmt.Sprintf("\n-# steps=%d, n=%d, cfg_scale=%s, clip_skip=%d", steps, n, dtoa(cfgScale), clipSkip))

	_, err = event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
		SetContent(sb.String()).
		SetFiles(files...).
		SetContainerComponents().
		Build())

	if err == nil {
		stats, err := db.GetGlobalStats()
		if err == nil {
			stats.ImagesGenerated++
			if err := stats.Write(); err != nil {
				slog.Error("handleNarrationGenerate: failed to write global stats", slog.Any("err", err))
			}
		}
	}

	return err
}

func HandleGenerateCancel(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	_, customID, ok := strings.Cut(data.CustomID()[1:], "/")
	slog.Debug("HandleGenerateCancel", slog.String("id", customID), slog.String("custom_id", data.CustomID()))
	if !ok {
		slog.Warn("HandleGenerateCancel: invalid custom id", slog.String("custom_id", data.CustomID()))
		return fmt.Errorf("invalid custom id: %s", data.CustomID())
	}

	id, userID, ok := strings.Cut(customID, ":")
	if !ok {
		slog.Warn("HandleGenerateCancel: invalid custom id (no user)", slog.String("custom_id", data.CustomID()))
		return fmt.Errorf("invalid custom id: %s", data.CustomID())
	}

	if userID != event.User().ID.String() {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContent("❌ Cannot cancel generation of another user").
				SetEphemeral(true).
				Build(),
		)
	}

	event.DeferUpdateMessage()

	if err := horder.GetHorder().Cancel(id); err != nil {
		slog.Error("HandleGenerateCancel: failed to cancel", slog.Any("err", err))
		// TODO: should we update or create here? doubt this is documented
		_, err := event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetContent("❌ " + err.Error()).
			Build())
		return err
	}

	slog.Info("HandleGenerateCancel: canceled", slog.String("id", id))
	return event.DeleteInteractionResponse()
}

func HandleGenerateModelAutocomplete(event *handler.AutocompleteEvent) error {
	dataModel := event.Data.String("model")

	scoredModels := horder.GetHorder().ScoreModels()
	models := make([]string, 0, len(scoredModels))
	for _, m := range scoredModels {
		name := m.String()
		if m.Detail.Description != "" {
			name += ": " + m.Detail.Description
		}
		models = append(models, name)
	}

	var matches fuzzy.Ranks
	if dataModel != "" {
		matches = fuzzy.RankFindNormalizedFold(dataModel, models)
		sort.Sort(matches)
	} else {
		// fake it to keep the order
		matches = fuzzy.Ranks{}
		for i, m := range models {
			matches = append(matches, fuzzy.Rank{
				Source:        "",
				Target:        m,
				OriginalIndex: i,
			})
		}
	}

	var choices []discord.AutocompleteChoice
	for _, match := range matches {
		if len(choices) >= 25 {
			break
		}
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  ellipsisTrim(match.Target, 100),
			Value: scoredModels[match.OriginalIndex].Model.Name,
		})
	}

	return event.AutocompleteResult(choices)
}
