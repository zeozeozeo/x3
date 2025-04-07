package commands

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"path"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

// ptr is a helper function to create a pointer to a value.
func ptr[T any](v T) *T {
	return &v
}

// makePersonaOptionChoices generates the choices for the persona command option.
func makePersonaOptionChoices() []discord.ApplicationCommandOptionChoiceString {
	var choices []discord.ApplicationCommandOptionChoiceString
	for _, p := range persona.AllPersonas {
		choices = append(choices, discord.ApplicationCommandOptionChoiceString{Name: p.String(), Value: p.Name})
	}
	return choices
}

// formatModel formats the model name with additional info like (Default), (Whitelist), emojis.
func formatModel(m model.Model) string {
	var sb strings.Builder
	sb.WriteString(m.Name)
	if m.Name == model.DefaultModel.Name {
		sb.WriteString(" (Default)")
	}
	if m.Whitelisted {
		sb.WriteString(" (Whitelist)")
	}
	if m.Vision {
		sb.WriteString(" 👀")
	}
	if m.Reasoning {
		sb.WriteString(" 🧠")
	}
	return sb.String()
}

// PersonaCommand is the definition for the /persona command
var PersonaCommand = discord.SlashCommandCreate{
	Name:        "persona",
	Description: "Set persona, model or system prompt for this channel",
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
			Name:        "persona",
			Description: "Choose a pre-made persona for this chat",
			Choices:     makePersonaOptionChoices(),
			Required:    false,
		},
		discord.ApplicationCommandOptionString{
			Name:        "system",
			Description: "Set a custom system prompt for this chat",
			Required:    false,
		},
		discord.ApplicationCommandOptionString{
			Name:         "model",
			Description:  "Set a model to use for this chat",
			Autocomplete: true, // since discord limits us to 25 choices, we will hack it
			Required:     false,
		},
		discord.ApplicationCommandOptionString{
			Name:        "card",
			Description: "SillyTavern character card URL (image or json, get them from chub.ai or jannyai.com)",
			Required:    false,
		},
		discord.ApplicationCommandOptionInt{
			Name:        "context",
			Description: "Amount of surrounding messages to use as context. Pass a negative number to reset",
			Required:    false,
		},
		discord.ApplicationCommandOptionFloat{
			Name:        "temperature",
			Description: "Controls randomness in LLM predictions; 0 or 1 to reset",
			Required:    false,
			MinValue:    ptr(0.0),
			MaxValue:    ptr(2.0 + 0.4), // remaps to 2.0
		},
		discord.ApplicationCommandOptionFloat{
			Name:        "top_p",
			Description: "Controls cumulative probability of token selection; 0 or 1 to reset",
			Required:    false,
			MinValue:    ptr(0.0),
			MaxValue:    ptr(1.0 + 0.1), // remaps to 1.0
		},
		discord.ApplicationCommandOptionFloat{
			Name:        "frequency_penalty",
			Description: "Penalizes frequent tokens to reduce repetition; 0 to reset",
			Required:    false,
			MinValue:    ptr(-2.0),
			MaxValue:    ptr(2.0),
		},
		discord.ApplicationCommandOptionInt{
			Name:        "seed",
			Description: "Set a seed for LLM predictions; 0 to reset",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "ephemeral",
			Description: "If the response should only be visible to you",
			Required:    false,
		},
	},
}

// handlePersonaInfo displays the current persona settings for the channel.
func handlePersonaInfo(event *handler.CommandEvent, ephemeral bool) error {
	cache := db.GetChannelCache(event.Channel().ID())

	meta, _ := persona.GetMetaByName(cache.PersonaMeta.Name)

	settings := cache.PersonaMeta.Settings.Fixup()
	remappedSettings := settings
	remappedSettings.Remap()

	builder := discord.NewEmbedBuilder().
		SetTitle("Persona").
		SetColor(0x0085ff).
		SetDescription("Current persona settings in channel. Use `/stats` to view usage stats.").
		SetFooter("x3", x3Icon).
		SetTimestamp(time.Now()).
		AddField("Name", cache.PersonaMeta.Name, true).
		AddField("Description", meta.Desc, true).
		AddField("Temperature", fmt.Sprintf("%s (remapped to %s)", ftoa(settings.Temperature), ftoa(remappedSettings.Temperature)), true).
		AddField("Top P", fmt.Sprintf("%s (remapped to %s)", ftoa(settings.TopP), ftoa(remappedSettings.TopP)), true).
		AddField("Frequency Penalty", ftoa(settings.FrequencyPenalty), true).
		AddField("Model", model.GetModelByName(cache.PersonaMeta.Model).Name, false)

	var files []*discord.File
	if cache.PersonaMeta.System != "" {
		builder.AddField("System prompt", ellipsisTrim(cache.PersonaMeta.System, 1024), false)
		// if the system prompt is > 1024 chars, attach it as a file
		if utf8.RuneCountInString(cache.PersonaMeta.System) > 1024 {
			files = append(files, &discord.File{
				Name:   "system-prompt-full.txt",
				Reader: strings.NewReader(cache.PersonaMeta.System),
			})
		}
	}

	builder.AddField("Context length", fmt.Sprintf("%d", cache.ContextLength), false)
	if cache.Llmer != nil {
		builder.AddField("Message cache", fmt.Sprintf("%d messages", cache.Llmer.NumMessages()), false)
	}
	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			AddEmbeds(builder.Build()).
			SetEphemeral(ephemeral).
			AddFiles(files...).
			Build(),
	)
}

// HandlePersona handles the /persona command logic.
func HandlePersona(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	dataPersona := data.String("persona")
	dataModel := data.String("model")
	dataSystem := data.String("system")
	dataCard := data.String("card")
	dataContext, hasContext := data.OptInt("context")
	dataTemperature, hasTemperature := data.OptFloat("temperature")
	dataTopP, hasTopP := data.OptFloat("top_p")
	dataFreqPenalty, hasFreqPenalty := data.OptFloat("frequency_penalty")
	dataSeed, hasDataSeed := data.OptInt("seed")
	ephemeral := data.Bool("ephemeral")

	if dataPersona == "" && dataModel == "" && dataSystem == "" && dataCard == "" && !hasContext && !hasTemperature && !hasTopP && !hasFreqPenalty && !hasDataSeed {
		return handlePersonaInfo(event, ephemeral)
	}

	if dataCard != "" {
		// might take some time to fetch the character card
		err := event.DeferCreateMessage(ephemeral)
		if err != nil {
			return err
		}
	}

	m := model.GetModelByName(dataModel)

	// default settings for lunaris (TODO: maybe add per-model default settings instead of this HACK ?):
	// temp = 1.4, top_p = 0.9
	if m.IsLunaris {
		if !hasTemperature {
			dataTemperature = 1.4 + 0.4 // remaps to 1.4
		}
		if !hasTopP {
			dataTopP = 1.0 // remaps to 0.9
		}
	}

	// only query whitelist if we need to
	inWhitelist := false
	if m.Whitelisted || dataContext > 50 {
		inWhitelist = db.IsInWhitelist(event.User().ID)
	}

	if m.Whitelisted && !inWhitelist {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContentf("You need to be whitelisted to set the model `%s`. Try `%s`", dataModel, model.ModelGpt4oMini.Name).
				SetEphemeral(true).
				Build(),
		)
	}
	if dataContext > 50 && !inWhitelist {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				SetContent("The maximum allowed context length for users outside the whitelist is 50").
				SetEphemeral(true).
				Build(),
		)
	}

	cache := db.GetChannelCache(event.Channel().ID())
	personaMeta, err := persona.GetMetaByName(dataPersona)
	if err != nil {
		personaMeta = cache.PersonaMeta
	}

	// update persona meta in channel cache
	prevMeta := cache.PersonaMeta
	if prevMeta.System == "" {
		prevMeta.System = persona.GetPersonaByMeta(cache.PersonaMeta, nil, "").System
	}
	cache.PersonaMeta = personaMeta
	if dataPersona != "" {
		cache.PersonaMeta.Name = dataPersona
	}
	if dataSystem != "" {
		cache.PersonaMeta.System = dataSystem
	}
	if dataModel != "" {
		cache.PersonaMeta.Model = dataModel
	}
	prevContextLen := cache.ContextLength
	if hasContext {
		if dataContext < 0 {
			dataContext = db.DefaultContextMessages
		}
		cache.ContextLength = dataContext
	}
	var seed *int
	if dataSeed != 0 {
		seed = &dataSeed
	}
	cache.PersonaMeta.Settings = persona.InferenceSettings{
		Temperature:      float32(dataTemperature),
		TopP:             float32(dataTopP),
		FrequencyPenalty: float32(dataFreqPenalty),
		Seed:             seed,
	}.Fixup()

	// apply character card
	didWhat := []string{}
	creatorNotes := ""
	if dataCard != "" {
		// fetch from url (this is pretty scary)
		slog.Debug("fetching character card", slog.String("url", dataCard))
		resp, err := http.Get(dataCard)
		if err != nil {
			slog.Error("failed to fetch character card", slog.Any("err", err))
			return updateInteractionError(event, err.Error())
		}
		defer resp.Body.Close()
		body, err := io.ReadAll(resp.Body)
		if err != nil {
			slog.Error("failed to read character card resp body", slog.Any("err", err))
			return updateInteractionError(event, err.Error())
		}
		card, err := cache.PersonaMeta.ApplyChara(body, event.User().EffectiveName())
		if err != nil {
			slog.Error("failed to apply character card", slog.Any("err", err))
			return updateInteractionError(event, err.Error())
		}
		if card.Data != nil {
			creatorNotes = card.Data.CreatorNotes
		}
		filename := path.Base(dataCard)
		filename, _, _ = strings.Cut(filename, "?")
		didWhat = append(didWhat, fmt.Sprintf("set character card to `%s`", filename))
	}

	if err := cache.Write(event.Channel().ID()); err != nil {
		if dataCard != "" {
			return updateInteractionError(event, err.Error())
		}
		return sendInteractionError(event, err.Error(), true)
	}

	var sb strings.Builder
	remappedSettings := cache.PersonaMeta.Settings
	remappedSettings.Remap()
	if cache.PersonaMeta.Name != prevMeta.Name && cache.PersonaMeta.Name != "" {
		didWhat = append(didWhat, fmt.Sprintf("set persona to `%s`", cache.PersonaMeta.Name))
	}
	if cache.PersonaMeta.Model != prevMeta.Model && cache.PersonaMeta.Model != "" {
		didWhat = append(didWhat, fmt.Sprintf("set model to `%s`", cache.PersonaMeta.Model))
	}
	if cache.PersonaMeta.System != prevMeta.System && cache.PersonaMeta.System != "" {
		didWhat = append(didWhat, "updated the system prompt")
	}
	if cache.ContextLength != prevContextLen {
		didWhat = append(didWhat, fmt.Sprintf("updated context length %d → %d", prevContextLen, cache.ContextLength))
	}
	if cache.PersonaMeta.Settings.Temperature != prevMeta.Settings.Temperature {
		didWhat = append(didWhat, fmt.Sprintf("updated temperature %s → %s (remapped to %s)", ftoa(prevMeta.Settings.Temperature), ftoa(cache.PersonaMeta.Settings.Temperature), ftoa(remappedSettings.Temperature)))
	}
	if cache.PersonaMeta.Settings.TopP != prevMeta.Settings.TopP {
		didWhat = append(didWhat, fmt.Sprintf("updated top_p %s → %s (remapped to %s)", ftoa(prevMeta.Settings.TopP), ftoa(cache.PersonaMeta.Settings.TopP), ftoa(remappedSettings.TopP)))
	}
	if cache.PersonaMeta.Settings.FrequencyPenalty != prevMeta.Settings.FrequencyPenalty {
		didWhat = append(didWhat, fmt.Sprintf("updated frequency_penalty %s → %s", ftoa(prevMeta.Settings.FrequencyPenalty), ftoa(cache.PersonaMeta.Settings.FrequencyPenalty)))
	}
	if zifnil(cache.PersonaMeta.Settings.Seed) != zifnil(prevMeta.Settings.Seed) {
		prevSeed := "`<random>`"
		if prevMeta.Settings.Seed != nil {
			prevSeed = strconv.Itoa(*prevMeta.Settings.Seed)
		}
		newSeed := "`<random>`"
		if cache.PersonaMeta.Settings.Seed != nil {
			newSeed = strconv.Itoa(*cache.PersonaMeta.Settings.Seed)
		}
		didWhat = append(didWhat, fmt.Sprintf("updated seed %s → %s", prevSeed, newSeed))
	}

	if len(didWhat) > 0 {
		sb.WriteString("Updated persona for this channel")
		sb.WriteString(" (")
		sb.WriteString(strings.Join(didWhat, ", "))
		sb.WriteString(")")
	} else {
		sb.WriteString("No changes made")
	}

	builder := discord.NewEmbedBuilder().
		SetColor(0x0085ff).
		SetTitle("Updated persona").
		SetFooter("x3", x3Icon).
		SetTimestamp(time.Now()).
		SetDescription(sb.String())

	files := []*discord.File{}
	if creatorNotes != "" {
		builder.AddField("Creator notes", ellipsisTrim(creatorNotes, 1024), false)
		// if creator notes are > 1024 chars, attach them as a file
		if utf8.RuneCountInString(creatorNotes) > 1024 {
			files = append(files, &discord.File{
				Reader: strings.NewReader(creatorNotes),
				Name:   "creator-notes-full.txt",
			})
		}
	}

	if dataCard == "" {
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				AddEmbeds(builder.Build()).
				SetEphemeral(ephemeral).
				AddFiles(files...).
				Build(),
		)
	} else {
		_, err := event.UpdateInteractionResponse(
			discord.NewMessageUpdateBuilder().
				AddEmbeds(builder.Build()).
				AddFiles(files...).
				Build(),
		)
		return err
	}
}

// HandlePersonaModelAutocomplete handles the autocomplete for the model option in the /persona command.
func HandlePersonaModelAutocomplete(event *handler.AutocompleteEvent) error {
	dataModel := event.Data.String("model")

	models := []string{}
	inWhitelist := db.IsInWhitelist(event.User().ID)
	for _, m := range model.AllModels {
		if m.Whitelisted && !inWhitelist {
			continue
		}
		models = append(models, formatModel(m))
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
			Value: model.AllModels[match.OriginalIndex].Name,
		})
	}

	return event.AutocompleteResult(choices)
}
