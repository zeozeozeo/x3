package commands

import (
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

// LobotomyCommand is the definition for the /lobotomy command
var LobotomyCommand = discord.SlashCommandCreate{
	Name:        "lobotomy",
	Description: "Forget local context",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
		discord.InteractionContextTypeBotDM,
		discord.InteractionContextTypePrivateChannel,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionBool{
			Name:        "ephemeral",
			Description: "If the response should only be visible to you",
			Required:    false,
		},
		discord.ApplicationCommandOptionInt{
			Name:        "amount",
			Description: "The amount of last messages to forget. By default, removes all",
			Required:    false,
		},
		discord.ApplicationCommandOptionBool{
			Name:        "reset_persona",
			Description: "Also set the persona to the default one",
			Required:    false,
		},
		discord.ApplicationCommandOptionInt{
			Name:        "next_card",
			Description: "Index of the first message in the card for the next response",
			Required:    false,
		},
	},
}

// HandleLobotomy handles the /lobotomy command logic
func HandleLobotomy(event *handler.CommandEvent) error {
	data := event.SlashCommandInteractionData()
	ephemeral := data.Bool("ephemeral")
	amount := data.Int("amount")
	nextCard, hasNextCard := data.OptInt("next_card")
	resetPersona := data.Bool("reset_persona")

	cache := db.GetChannelCache(event.Channel().ID())

	writeCache := false
	if resetPersona {
		cache.PersonaMeta = db.NewChannelCache().PersonaMeta
		writeCache = true
	}
	if cache.Llmer != nil {
		cache.Llmer.Lobotomize(amount)
		writeCache = true
	}
	// in card mode, resend the card preset message
	if len(cache.PersonaMeta.FirstMes) > 0 && amount == 0 {
		cache.PersonaMeta.IsFirstMes = true
		writeCache = true
		if hasNextCard && nextCard > 0 {
			idx := nextCard - 1
			if idx < len(cache.PersonaMeta.FirstMes) {
				cache.PersonaMeta.NextMes = &idx
			} else {
				sendInteractionError(event, fmt.Sprintf("next card index out of range (1..=%d)", len(cache.PersonaMeta.FirstMes)), true)
			}
		}
	}
	if writeCache {
		if err := cache.Write(event.Channel().ID()); err != nil {
			return sendInteractionError(event, err.Error(), true)
		}
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	if amount > 0 {
		return event.CreateMessage(discord.MessageCreate{
			Content: fmt.Sprintf("Removed last %d messages from the context", amount),
			Flags:   flags,
		})
	}
	return event.CreateMessage(discord.MessageCreate{
		Content: "Lobotomized for this channel",
		Flags:   flags,
	})
}
