package commands

import (
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
)

// HandleNotFound is the handler for unrecognized commands.
func HandleNotFound(event *handler.InteractionEvent) error {
	return event.CreateMessage(discord.MessageCreate{
		Content: "Command not found.",
		Flags:   discord.MessageFlagEphemeral,
	})
}
