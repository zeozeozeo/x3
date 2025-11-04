package commands

import (
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/persona"
)

var ImpersonateCommand = discord.SlashCommandCreate{
	Name:        "impersonate",
	Description: "Make the AI write a response as me for a set amount of turns",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
		discord.InteractionContextTypeBotDM,
		//discord.InteractionContextTypePrivateChannel,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionInt{
			Name:        "turns",
			Description: "Number of impersonated turns to generate (default: 1)",
			MinValue:    ptr(1),
			MaxValue:    ptr(50),
		},
	},
}

func HandleImpersonate(event *handler.CommandEvent) error {
	turns := event.SlashCommandInteractionData().Int("turns")
	turns = min(max(1, turns), 50)
	isInWhitelist := db.IsInWhitelist(event.User().ID)
	if turns > 5 && !isInWhitelist {
		return event.CreateMessage(
			discord.MessageCreate{
				Content: "Non-whitelisted users cannot impersonate for more than 5 turns (DM <@890686470556356619> to request access)",
				Flags:   discord.MessageFlagEphemeral,
			},
		)
	}

	if err := event.DeferCreateMessage(false); err != nil {
		return err
	}

	//cache := db.GetChannelCache(event.Channel().ID())

	impersonatePersona := persona.GetPersonaByMeta(persona.PersonaImpersonate, "", event.User().EffectiveName(), false, time.Time{})

	var prevResponse string
	var referenceID snowflake.ID
	interactionEvent := event
	for i := range turns * 2 {
		isImpersonateTurn := i%2 == 0
		var systemPromptOverride *string
		if isImpersonateTurn {
			systemPromptOverride = ptr(impersonatePersona.System)
		}

		// format when impersonating: `system message: <generate the next response as USER>`
		var trigger string
		if isImpersonateTurn {
			trigger = fmt.Sprintf("<generate the next response as %s; keep the response short and concise>", event.User().EffectiveName())
		} else {
			trigger = prevResponse
		}
		var username string
		if isImpersonateTurn {
			username = "system message"
		}
		var prepend string
		if isImpersonateTurn {
			prepend = event.User().EffectiveName() + ": "
		}

		var wg sync.WaitGroup
		if i > 0 { // don't wait for the first iter because event is nonnil
			wg.Add(1)
			go sendTypingWithLog(event.Client(), event.Channel().ID(), &wg)
		}
		response, botMessageID, err := handleLlmInteraction2(
			event.Client(),
			event.Channel().ID(), // channel ID
			referenceID,
			trigger, // system instruction/trigger
			username,
			0,     // determine uid from history
			nil,   // no attachments
			false, // not a timeInteraction
			false, // not a regenerate
			prepend,
			&wg,
			nil,              // no reference
			interactionEvent, // we want the first split to be sent as an update of this event
			systemPromptOverride,
			isImpersonateTurn,
			event.Channel().Type() == discord.ChannelTypeDM,
		)
		if err != nil {
			slog.Error("handleLlmInteraction2 failed", "err", err, "channel_id", event.Channel().ID())
			return sendInteractionError(event, err.Error(), false)
		}

		prevResponse = response
		interactionEvent = nil
		referenceID = botMessageID
	}

	return nil
}
