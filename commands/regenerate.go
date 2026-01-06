package commands

import (
	"context"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

var RegenerateCommand = discord.SlashCommandCreate{
	Name:        "regenerate",
	Description: "Regenerate the last response, optionally prefill the response",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
		discord.InteractionContextTypeBotDM,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionString{
			Name:        "prepend",
			Description: "Text to start the response with",
		},
	},
}

// HandleRegenerate handles the /regenerate command.
func HandleRegenerate(event *handler.CommandEvent) error {
	prepend := event.SlashCommandInteractionData().String("prepend")
	prepend = strings.ReplaceAll(prepend, "\\n", "\n") // allow user to specify newlines
	if prepend != "" && !endsWithWhitespace(prepend) {
		prepend += " "
	}

	err := event.DeferCreateMessage(true)
	if err != nil {
		return err
	}

	jumpURL, _, err := handleLlmInteraction2(
		event.Client(),
		event.Channel().ID(),
		0,     // messageID is determined by handleLlmInteraction2 when regenerating
		"",    // content is empty for regeneration
		"",    // no username
		0,     // no memory
		nil,   // no attachments
		false, // timeInteraction
		true,  // isRegenerate
		prepend,
		nil,   // no wg
		nil,   // no reference
		nil,   // no event
		nil,   // no system prompt override
		false, // not impersonate
		event.Channel().Type() == discord.ChannelTypeDM,
		context.Background(),
	)
	if err != nil {
		return updateInteractionError(event, err.Error())
	}

	_, err = event.UpdateInteractionResponse(
		discord.NewMessageUpdateBuilder().
			SetFlags(discord.MessageFlagEphemeral).
			SetContentf("Regenerated message %s", jumpURL).
			Build(),
	)
	return err
}
