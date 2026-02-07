package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
)

var PersonaMakerCommand = discord.SlashCommandCreate{
	Name:        "personamaker",
	Description: "Create a new persona (in DMs only)",
}

func formatCardField(s string) string {
	if s == "" {
		return "`<unset>`"
	}
	return ellipsisTrim(s, 1024)
}

type SettingWhatId string

const (
	SettingName         SettingWhatId = "setname"
	SettingPersonality  SettingWhatId = "setpersonality"
	SettingScenario     SettingWhatId = "setscenario"
	SettingCreatorNotes SettingWhatId = "setcreatornotes"
	SettingSystemPrompt SettingWhatId = "setsystemprompt"
	SettingDescription  SettingWhatId = "setdescription"
	SettingTags         SettingWhatId = "settags"
	SettingGreeting     SettingWhatId = "setgreeting"
)

func dynamicButton(label string, customID string, fieldLen int) discord.ButtonComponent {
	style := discord.ButtonStyleSecondary
	if fieldLen > 0 {
		style = discord.ButtonStylePrimary
	}
	return discord.ButtonComponent{
		Style:    style,
		Label:    label,
		CustomID: customID,
	}
}

func sortedActionRow(buttons []discord.InteractiveComponent) []discord.ContainerComponent {
	sort.SliceStable(buttons, func(i, j int) bool {
		btnI, okI := buttons[i].(discord.ButtonComponent)
		btnJ, okJ := buttons[j].(discord.ButtonComponent)
		if !okI || !okJ {
			return false
		}
		return btnI.Style < btnJ.Style
	})

	var rows []discord.ContainerComponent
	const maxButtonsPerRow = 5

	for i := 0; i < len(buttons); i += maxButtonsPerRow {
		end := i + maxButtonsPerRow
		end = min(end, len(buttons))
		rows = append(rows, discord.NewActionRow(buttons[i:end]...))
	}

	return rows
}

func formatRequired(s string, length int) string {
	if length == 0 {
		return "*" + s
	}
	return s
}

func sendOrReplyWithFlowEmbed(event *handler.CommandEvent, channelID snowflake.ID, client bot.Client, flow *db.PersonaNewFlow) (snowflake.ID, error) {
	requiredFieldsLeft := []string{}
	if flow.Card.Name == "" {
		requiredFieldsLeft = append(requiredFieldsLeft, "Name (display name of your character)")
	}
	if flow.Card.Personality == "" {
		requiredFieldsLeft = append(requiredFieldsLeft, "Personality (detailed description of your character)")
	}

	var adviceStr string
	if len(requiredFieldsLeft) > 0 {
		adviceStr = fmt.Sprintf("* Required fields left: %s", strings.Join(requiredFieldsLeft, ", "))
	} else if flow.Card.Description == "" || len(flow.Card.Tags) == 0 {
		adviceStr = "* Add metadata (description, tags, creator notes) if you are going to export your card later"
	} else if flow.Card.CreatorNotes == "" {
		adviceStr = "* All good, maybe add creator notes"
	} else if flow.Card.Scenario != "" && flow.Card.FirstMes != "" {
		adviceStr = "* All good!"
	}
	if flow.Card.Scenario == "" || flow.Card.FirstMes == "" {
		adviceStr += "\n* Set scenario and greetings if you want to start the roleplay with something"
	}

	builder := discord.NewEmbedBuilder().
		SetTitle("Persona maker").
		SetDescriptionf("Suggestions:\n%s", adviceStr).
		AddFields(
			discord.EmbedField{
				Name:  "Name (required)",
				Value: formatCardField(flow.Card.Name),
			},
			discord.EmbedField{
				Name:  "Personality (required)",
				Value: formatCardField(flow.Card.Personality),
			},
			discord.EmbedField{
				Name:  "Scenario",
				Value: formatCardField(flow.Card.Scenario),
			},
			discord.EmbedField{
				Name:  "System prompt",
				Value: formatCardField(flow.Card.SystemPrompt),
			},
			discord.EmbedField{
				Name:  "Creator notes (metadata)",
				Value: formatCardField(flow.Card.CreatorNotes),
			},
			discord.EmbedField{
				Name:  "Card description (metadata)",
				Value: formatCardField(flow.Card.Description),
			},
			discord.EmbedField{
				Name:  "Card tags (metadata)",
				Value: formatCardField(strings.Join(flow.Card.Tags, ", ")),
			},
		).
		SetColor(0x0085ff).
		SetFooter("x3", x3Icon).
		SetTimestamp(time.Now())

	for i, greeting := range flow.Card.AllGreetings() {
		builder = builder.AddField(fmt.Sprintf("Greeting #%d", i+1), formatCardField(greeting), false)
	}

	buttons := []discord.InteractiveComponent{
		dynamicButton(formatRequired("Name", len(flow.Card.Name)), "/personamaker/setname", len(flow.Card.Name)),
		dynamicButton(formatRequired("Personality", len(flow.Card.Personality)), "/personamaker/setpersonality", len(flow.Card.Personality)),
		dynamicButton("Scenario", "/personamaker/setscenario", len(flow.Card.Scenario)),
		dynamicButton("System prompt", "/personamaker/setsystemprompt", len(flow.Card.SystemPrompt)),
		dynamicButton("Greeting", "/personamaker/setgreeting", len(flow.Card.FirstMes)),
		dynamicButton("Creator notes", "/personamaker/setcreatornotes", len(flow.Card.CreatorNotes)),
		dynamicButton("Description", "/personamaker/setdescription", len(flow.Card.Description)),
		dynamicButton("Tags", "/personamaker/settags", len(flow.Card.Tags)),
		discord.ButtonComponent{
			Style: discord.ButtonStyleSecondary,
			Label: "Cancel",
			Emoji: &discord.ComponentEmoji{
				Name: "❌",
			},
			CustomID: "/personamaker/cancelnew",
		},
	}

	messageCreate := discord.NewMessageCreateBuilder().
		SetAllowedMentions(&discord.AllowedMentions{
			RepliedUser: false,
		}).
		AddEmbeds(builder.Build()).
		SetContainerComponents(sortedActionRow(buttons)...).
		Build()

	if channelID != 0 {
		message, err := client.Rest().CreateMessage(channelID, messageCreate)
		return message.ID, err
	}
	return 0, event.CreateMessage(messageCreate)
}

func HandlePersonaNew(event *handler.CommandEvent) error {
	if event.GuildID() != nil || event.Context() != discord.InteractionContextTypeBotDM {
		return sendInteractionError(event, "Persona maker can only be used in a DM with me", true)
	}

	cache := db.GetChannelCache(event.Channel().ID())
	cache.PersonaNewFlow = &db.PersonaNewFlow{}
	cache.Write(event.Channel().ID())

	_, err := sendOrReplyWithFlowEmbed(event, 0, nil, cache.PersonaNewFlow)
	return err
}

func HandlePersonaNewSetButton(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	cache := db.GetChannelCache(event.Channel().ID())
	_, customID, ok := strings.Cut(data.CustomID()[1:], "/")
	if !ok {
		return errors.New("invalid custom ID")
	}

	if customID == "cancelnew" {
		cache.PersonaNewFlow = nil
		if err := cache.Write(event.Channel().ID()); err != nil {
			return err
		}
		return event.Client().Rest().DeleteMessage(event.Channel().ID(), event.Message.ID)
	}

	if cache.PersonaNewFlow == nil {
		return errors.New("persona maker not running")
	}

	var settingWhat string
	var field string
	switch SettingWhatId(customID) {
	case SettingName:
		settingWhat = "name"
		field = cache.PersonaNewFlow.Card.Name
	case SettingPersonality:
		settingWhat = "personality"
		field = cache.PersonaNewFlow.Card.Personality
	case SettingScenario:
		settingWhat = "scenario"
		field = cache.PersonaNewFlow.Card.Scenario
	case SettingCreatorNotes:
		settingWhat = "creator notes"
		field = cache.PersonaNewFlow.Card.CreatorNotes
	case SettingSystemPrompt:
		settingWhat = "system prompt"
		field = cache.PersonaNewFlow.Card.SystemPrompt
	case SettingDescription:
		settingWhat = "description"
		field = cache.PersonaNewFlow.Card.Description
	case SettingTags:
		settingWhat = "tags (separated by commas)"
		field = strings.Join(cache.PersonaNewFlow.Card.Tags, ", ")
	case SettingGreeting:
		settingWhat = "greetings"
		field = pluralize(len(cache.PersonaNewFlow.Card.AllGreetings()), "greeting")
	default:
		slog.Error("WTF is the button id", "id", customID)
		return errors.New("unknown button ID")
	}

	cache.PersonaNewFlow.FlowMessageID = event.Message.ID
	cache.PersonaNewFlow.SettingWhat = customID
	if err := cache.Write(event.Channel().ID()); err != nil {
		return err
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			SetEphemeral(false).
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetColor(0x0085ff).
					SetTitlef("Changing %s", settingWhat).
					SetFooter("x3", x3Icon).
					SetDescription(formatCardField(field)).
					Build(),
			).
			Build(),
	)
}

func maybeHandlePersonaNewFlowMessage(event *events.MessageCreate) bool {
	if event.GuildID != nil || event.Message.Content == "" {
		return false
	}

	cache := db.GetChannelCache(event.ChannelID)
	if cache.PersonaNewFlow == nil {
		return false
	}

	content := event.Message.Content
	flow := cache.PersonaNewFlow
	switch SettingWhatId(flow.SettingWhat) {
	case SettingName:
		flow.Card.Name = content
	case SettingPersonality:
		flow.Card.Personality = content
	case SettingScenario:
		flow.Card.Scenario = content
	case SettingCreatorNotes:
		flow.Card.CreatorNotes = content
	case SettingSystemPrompt:
		flow.Card.SystemPrompt = content
	case SettingDescription:
		flow.Card.Description = content
	case SettingTags:
		for tag := range strings.SplitSeq(content, ",") {
			flow.Card.Tags = append(flow.Card.Tags, strings.TrimSpace(tag))
		}
	case SettingGreeting:
		if flow.Card.FirstMes == "" {
			flow.Card.FirstMes = content
		} else {
			flow.Card.AlternateGreetings = append(flow.Card.AlternateGreetings, content)
		}
	}

	flow.SettingWhat = ""

	if err := purgeBotMessagesAfter(event.Client(), flow.FlowMessageID, event.ChannelID, true /* inclusive */); err != nil {
		slog.Error("purgeBotMessagesAfter failed", "err", err)
	}

	flowMessageID, err := sendOrReplyWithFlowEmbed(nil, event.ChannelID, event.Client(), flow)
	flow.FlowMessageID = flowMessageID
	if err != nil {
		slog.Error("sendOrReplyWithFlowEmbed failed", "err", err)
		sendPrettyError(event.Client(), err.Error(), event.ChannelID, event.MessageID)
	}

	cache.Write(event.ChannelID)

	return true
}
