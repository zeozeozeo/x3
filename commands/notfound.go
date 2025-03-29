package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

// HandleNotFound is the handler for unrecognized commands.
func HandleNotFound(event *handler.InteractionEvent) error {
	// Use the specific event type if possible, otherwise InteractionEvent is fine
	// for just sending an ephemeral message.
	return event.CreateMessage(discord.MessageCreate{
		Content: "Command not found.",
		Flags:   discord.MessageFlagEphemeral,
	})
}
