package commands

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/persona"
)

var PersonaMakerCommand = discord.SlashCommandCreate{
	Name:        "personamaker",
	Description: "Create, edit, or delete custom personas (in DMs only)",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "new",
			Description: "Create a new custom persona",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "edit",
			Description: "Edit your current persona",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "delete",
			Description: "Delete a custom persona you created",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:         "name",
					Description:  "Name of the persona to delete",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	},
}

func formatCardField(s string) string {
	if s == "" {
		return "`<unset>`"
	}
	s = strings.NewReplacer("{{user}}", "`{{user}}`", "{{char}}", "`{{char}}`").Replace(s)
	return ellipsisTrim(s, 1024)
}

type SettingWhatId string

const (
	SettingPersonaName  SettingWhatId = "setpersonaname"
	SettingCharName     SettingWhatId = "setcharname"
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
	if flow.Card.PersonaName == "" {
		requiredFieldsLeft = append(requiredFieldsLeft, "Persona name (unique identifier for this persona)")
	}
	if flow.Card.Name == "" {
		requiredFieldsLeft = append(requiredFieldsLeft, "Character name (the character's name, used in `{{char}}`)")
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
		SetDescriptionf("Suggestions:\n%s\n\nUse `{{char}}` for character name and `{{user}}` for user name.", adviceStr).
		AddFields(
			discord.EmbedField{
				Name:  "Persona name (required, unique)",
				Value: formatCardField(flow.Card.PersonaName),
			},
			discord.EmbedField{
				Name:  "Character name (required)",
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
		dynamicButton(formatRequired("Persona name", len(flow.Card.PersonaName)), "/personamaker/setpersonaname", len(flow.Card.PersonaName)),
		dynamicButton(formatRequired("Character name", len(flow.Card.Name)), "/personamaker/setcharname", len(flow.Card.Name)),
		dynamicButton(formatRequired("Personality", len(flow.Card.Personality)), "/personamaker/setpersonality", len(flow.Card.Personality)),
		dynamicButton("Scenario", "/personamaker/setscenario", len(flow.Card.Scenario)),
		dynamicButton("System prompt", "/personamaker/setsystemprompt", len(flow.Card.SystemPrompt)),
		dynamicButton("Greeting", "/personamaker/setgreeting", len(flow.Card.FirstMes)),
		dynamicButton("Creator notes", "/personamaker/setcreatornotes", len(flow.Card.CreatorNotes)),
		dynamicButton("Description", "/personamaker/setdescription", len(flow.Card.Description)),
		dynamicButton("Tags", "/personamaker/settags", len(flow.Card.Tags)),
		discord.ButtonComponent{
			Style: discord.ButtonStyleSecondary,
			Label: "Done",
			Emoji: &discord.ComponentEmoji{
				Name: "✅",
			},
			CustomID: "/personamaker/done",
		},
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

func HandlePersonaMaker(event *handler.CommandEvent) error {
	if event.GuildID() != nil || event.Context() != discord.InteractionContextTypeBotDM {
		return sendInteractionError(event, "Persona maker can only be used in a DM with me", true)
	}

	subcommand := *event.SlashCommandInteractionData().SubCommandName

	switch subcommand {
	case "new":
		return handlePersonaMakerNew(event)
	case "edit":
		return handlePersonaMakerEdit(event)
	case "delete":
		return handlePersonaMakerDelete(event)
	default:
		return sendInteractionError(event, "Unknown subcommand", true)
	}
}

func handlePersonaMakerNew(event *handler.CommandEvent) error {
	cache := db.GetChannelCache(event.Channel().ID())
	if cache.PersonaNewFlow != nil && cache.PersonaNewFlow.FlowMessageID != 0 {
		event.Client().Rest().DeleteMessage(event.Channel().ID(), cache.PersonaNewFlow.FlowMessageID)
	}
	cache.PersonaNewFlow = &db.PersonaNewFlow{}
	cache.Write(event.Channel().ID())

	_, err := sendOrReplyWithFlowEmbed(event, 0, nil, cache.PersonaNewFlow)
	return err
}

func handlePersonaMakerEdit(event *handler.CommandEvent) error {
	cache := db.GetChannelCache(event.Channel().ID())
	userID := event.User().ID

	if cache.PersonaMeta.TavernCard != nil {
		card := *cache.PersonaMeta.TavernCard.Data
		card.PersonaName = cache.PersonaMeta.Name
		cache.PersonaNewFlow = &db.PersonaNewFlow{
			Card:          card,
			EditingCardID: cache.PersonaMeta.Name,
		}
	} else if cache.PersonaMeta.System != "" {
		cache.PersonaNewFlow = &db.PersonaNewFlow{
			Card: persona.TavernCardV1{
				PersonaName:  cache.PersonaMeta.Name,
				Name:         cache.PersonaMeta.Name,
				Personality:  cache.PersonaMeta.System,
				SystemPrompt: cache.PersonaMeta.System,
			},
		}
	} else {
		userCache := db.GetUserCache(userID)
		var foundCard *persona.TavernCardV1
		for i := range userCache.Personas {
			if userCache.Personas[i].PersonaName == cache.PersonaMeta.Name {
				foundCard = &userCache.Personas[i]
				break
			}
		}
		if foundCard == nil {
			return sendInteractionError(event, "No custom persona is currently active. Use `/personamaker new` to create one.", true)
		}
		cache.PersonaNewFlow = &db.PersonaNewFlow{
			Card:          *foundCard,
			EditingCardID: foundCard.PersonaName,
		}
	}

	if cache.PersonaNewFlow.FlowMessageID != 0 {
		event.Client().Rest().DeleteMessage(event.Channel().ID(), cache.PersonaNewFlow.FlowMessageID)
	}
	cache.Write(event.Channel().ID())

	_, err := sendOrReplyWithFlowEmbed(event, 0, nil, cache.PersonaNewFlow)
	return err
}

func handlePersonaMakerDelete(event *handler.CommandEvent) error {
	name := event.SlashCommandInteractionData().String("name")
	userID := event.User().ID
	userCache := db.GetUserCache(userID)

	foundIdx := -1
	for i := range userCache.Personas {
		if userCache.Personas[i].PersonaName == name {
			foundIdx = i
			break
		}
	}

	if foundIdx == -1 {
		return sendInteractionError(event, fmt.Sprintf("Persona `%s` not found in your custom personas.", name), true)
	}

	userCache.Personas = append(userCache.Personas[:foundIdx], userCache.Personas[foundIdx+1:]...)
	if err := userCache.Write(userID); err != nil {
		return sendInteractionError(event, "Failed to delete persona: "+err.Error(), true)
	}

	return sendInteractionOk(event, "Persona deleted", fmt.Sprintf("Deleted custom persona `%s`", name), false)
}

func HandlePersonaMakerDeleteAutocomplete(event *handler.AutocompleteEvent) error {
	userID := event.User().ID
	userCache := db.GetUserCache(userID)
	return HandleGenericAutocomplete(event, "name", userCache.Personas, func(item any, index int) (string, string) {
		card := item.(persona.TavernCardV1)
		displayName := card.PersonaName
		if displayName == "" {
			displayName = card.Name
		}
		return displayName, displayName
	})
}

func v1ToV2(card persona.TavernCardV1) *persona.TavernCardV2 {
	return &persona.TavernCardV2{
		Spec:        "chara_card_v2",
		SpecVersion: "2.0",
		Data: &persona.TavernCardV1{
			Name:               card.Name,
			Description:        card.Description,
			Personality:        card.Personality,
			Scenario:           card.Scenario,
			FirstMes:           card.FirstMes,
			AlternateGreetings: card.AlternateGreetings,
			Tags:               card.Tags,
			SystemPrompt:       card.SystemPrompt,
			CreatorNotes:       card.CreatorNotes,
		},
	}
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

	if customID == "done" {
		if cache.PersonaNewFlow == nil {
			return sendInteractionErrorComponent(event, "Persona maker not running", true)
		}
		var missingFields []string
		if cache.PersonaNewFlow.Card.PersonaName == "" {
			missingFields = append(missingFields, "Persona name")
		}
		if cache.PersonaNewFlow.Card.Name == "" {
			missingFields = append(missingFields, "Character name")
		}
		if cache.PersonaNewFlow.Card.Personality == "" {
			missingFields = append(missingFields, "Personality")
		}
		if len(missingFields) > 0 {
			return sendInteractionErrorComponent(event, fmt.Sprintf("Missing required fields: %s", strings.Join(missingFields, ", ")), true)
		}

		userID := event.User().ID
		userCache := db.GetUserCache(userID)

		if cache.PersonaNewFlow.EditingCardID != "" {
			for i := range userCache.Personas {
				if userCache.Personas[i].PersonaName == cache.PersonaNewFlow.EditingCardID {
					userCache.Personas = append(userCache.Personas[:i], userCache.Personas[i+1:]...)
					break
				}
			}
		}

		for i := range userCache.Personas {
			if userCache.Personas[i].PersonaName == cache.PersonaNewFlow.Card.PersonaName {
				return sendInteractionErrorComponent(event, fmt.Sprintf("A persona named `%s` already exists. Choose a different persona name.", cache.PersonaNewFlow.Card.PersonaName), true)
			}
		}

		userCache.Personas = append(userCache.Personas, cache.PersonaNewFlow.Card)
		if err := userCache.Write(userID); err != nil {
			return sendInteractionErrorComponent(event, "Failed to save persona: "+err.Error(), true)
		}

		personaName := cache.PersonaNewFlow.Card.PersonaName
		cache.PersonaMeta = persona.PersonaMeta{
			Name:          personaName,
			TavernCard:    v1ToV2(cache.PersonaNewFlow.Card),
			Models:        persona.PersonaProto.Models,
			Settings:      persona.PersonaProto.Settings,
			NeedSummaries: true,
		}
		flowMsgID := cache.PersonaNewFlow.FlowMessageID
		cache.PersonaNewFlow = nil
		if err := cache.Write(event.Channel().ID()); err != nil {
			return sendInteractionErrorComponent(event, "Failed to apply persona: "+err.Error(), true)
		}

		if flowMsgID != 0 {
			if err := purgeBotMessagesAfter(event.Client(), flowMsgID, event.Channel().ID(), true, true); err != nil {
				slog.Error("purgeBotMessagesAfter failed", "err", err)
			}
		}

		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				AddEmbeds(
					discord.NewEmbedBuilder().
						SetColor(0x0085ff).
						SetTitle("Persona created").
						SetDescriptionf("Created and applied custom persona `%s` (character: `%s`)\n\nUse `/persona persona:<name>` to apply the persona, `/personamaker delete name:<name>` to delete it. **This is only visible for you!**", personaName, cache.PersonaMeta.TavernCard.Data.Name).
						SetFooter("x3", x3Icon).
						SetTimestamp(time.Now()).
						Build(),
				).
				Build(),
		)
	}

	if cache.PersonaNewFlow == nil {
		return errors.New("persona maker not running")
	}

	var settingWhat string
	var field string
	switch SettingWhatId(customID) {
	case SettingPersonaName:
		settingWhat = "persona name (unique identifier)"
		field = cache.PersonaNewFlow.Card.PersonaName
	case SettingCharName:
		settingWhat = "character name (used in {{char}})"
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

	var files []*discord.File
	if utf8.RuneCountInString(field) > 1024 {
		files = append(files, &discord.File{
			Name:   "full.txt",
			Reader: strings.NewReader(field),
		})
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
			AddFiles(files...).
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
	case SettingPersonaName:
		flow.Card.PersonaName = content
	case SettingCharName:
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

	if err := purgeBotMessagesAfter(event.Client(), flow.FlowMessageID, event.ChannelID, true, true); err != nil {
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
