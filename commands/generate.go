package commands

import (
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/zeozeozeo/x3/horder"
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
			Name:         "model",
			Description:  "Model to use for this image",
			Autocomplete: true,
			Required:     true,
		},
		discord.ApplicationCommandOptionString{
			Name:        "prompt",
			Description: "Prompt to use for this image",
			Required:    true,
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
		discord.ApplicationCommandOptionBool{
			Name:        "ephemeral",
			Description: "If the response should only be visible to you",
			Required:    false,
		},
	},
}

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

	return fmt.Sprintf("%s: `%s` %d/%d ", message, bar, current, total)
}

// HandleGenerate handles the /generate command
func HandleGenerate(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	model := data.String("model")
	prompt := data.String("prompt")
	negative := data.String("negative")
	steps := data.Int("steps")
	n := data.Int("n")
	cfgScale := data.Float("cfg_scale")
	ephemeral := data.Bool("ephemeral")
	isNSFW := true // in dms

	// check if its an nsfw channel; if it's not we'll rely on stablehorde to censor nsfw content
	channel, err := event.Client().Rest().GetChannel(event.Channel().ID())
	if err != nil {
		return err
	}
	if guildChannel, ok := channel.(discord.GuildMessageChannel); ok {
		isNSFW = guildChannel.NSFW()
	}

	if err := event.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	id, err := horder.GetHorder().Generate(model, prompt, negative, steps, n, cfgScale, isNSFW)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	failures := 0
	firstQueuePos := 0
	firstWaitTime := 0
	impossibleWaitTime := 30
	impossibleFail := false
	numDots := 2
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

		message := "Waiting for job to start" + strings.Repeat(".", numDots)
		if status.Processing != 0 && status.Restarted != 0 {
			message = fmt.Sprintf("Waiting for %s; %d restarted", pluralize(status.Processing, "job"), status.Restarted)
		}
		if status.QueuePosition > 0 {
			if firstQueuePos == 0 {
				firstQueuePos = status.QueuePosition
			}
			message = progressBar("Queued", firstQueuePos-status.QueuePosition, firstQueuePos, 50)
		} else if status.WaitTime > 0 {
			if firstWaitTime == 0 {
				firstWaitTime = status.WaitTime
			}
			message = progressBar("Diffusing", firstWaitTime-status.WaitTime, firstWaitTime, 50)
		} else if !status.IsPossible {
			if impossibleWaitTime == 0 {
				impossibleFail = true
				break
			}
			message = fmt.Sprintf("Generation is impossible with current pool of workers. Waiting %ds", impossibleWaitTime)
			impossibleWaitTime--
		}

		message += fmt.Sprintf(" (eta: %ds)", status.WaitTime)

		slog.Info(
			"HandleGenerate: progress",
			slog.Int("queue_pos", status.QueuePosition),
			slog.Int("wait_time", status.WaitTime),
			slog.Bool("done", status.Done),
			slog.Bool("faulted", status.Faulted),
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
						CustomID: fmt.Sprintf("cancel_%s", id),
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

	var sb strings.Builder
	for i, gen := range finalStatus.Generations {
		sb.WriteString(gen.Img)
		if i < len(finalStatus.Generations)-1 {
			sb.WriteRune('\n')
		}
	}

	event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
		SetContent(sb.String()).
		SetContainerComponents().
		Build())

	return nil
}

func HandleGenerateCancel(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	_, id, ok := strings.Cut(data.CustomID(), ":")
	if !ok {
		return fmt.Errorf("invalid custom id: %s", data.CustomID())
	}

	if err := horder.GetHorder().Cancel(id); err != nil {
		slog.Error("HandleGenerateCancel: failed to cancel", slog.Any("err", err))
		_, err := event.UpdateInteractionResponse(discord.NewMessageUpdateBuilder().
			SetContent(err.Error()).
			Build())
		return err
	}

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
