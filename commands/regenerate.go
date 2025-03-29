package commands

import (
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

// RegenerateCommand is the definition for the /regenerate command
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

// HandleRegenerate handles the /regenerate command logic.
func HandleRegenerate(event *handler.CommandEvent) error {
	prepend := event.SlashCommandInteractionData().String("prepend")
	prepend = strings.ReplaceAll(prepend, "\\n", "\n") // Allow user to specify newlines
	if prepend != "" && !endsWithWhitespace(prepend) {
		prepend += " " // Add trailing space if needed for better LLM continuation
	}

	// Defer ephemeral response for confirmation/error
	err := event.DeferCreateMessage(true)
	if err != nil {
		// Log error? This defer failing is unusual.
		return err
	}

	// Call the core interaction logic with regenerate flag
	jumpURL, err := handleLlmInteraction2(
		event.Client(),
		event.Channel().ID(),
		0,  // messageID is determined by handleLlmInteraction2 when regenerating
		"", // content is empty for regeneration
		"", // username is not needed for regeneration
		0,  // userID for memory is not relevant here (or should it be event.User().ID?) - Assuming 0 for now.
		nil, // attachments are not relevant for regeneration
		false, // timeInteraction
		true,  // isRegenerate
		prepend,
		nil, // preMsgWg - no typing indicator needed for ephemeral response
		nil, // reference - determined by handleLlmInteraction2
	)
	if err != nil {
		// Send error back to the user ephemerally
		return updateInteractionError(event, err.Error())
	}

	// Send ephemeral confirmation message with jump URL
	_, err = event.UpdateInteractionResponse(
		discord.NewMessageUpdateBuilder().
			SetFlags(discord.MessageFlagEphemeral).
			SetContentf("Regenerated message %s", jumpURL).
			Build(),
	)
	return err
}