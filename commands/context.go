package commands

import (
	"fmt"
	"strings"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

var contextCommand = discord.SlashCommandCreate{
	Name:        "context",
	Description: "Manage user-defined context for this channel",
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "add",
			Description: "Add a new context item",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "text",
					Description: "The context text to add",
					Required:    true,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "clear",
			Description: "Clear all context items",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "list",
			Description: "List current context items",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
				},
			},
		},
	},
}

func handleContext(e *handler.CommandEvent) error {
	subcommand := *e.SlashCommandInteractionData().SubCommandName
	channelID := e.Channel().ID()
	cache := db.GetChannelCache(channelID)

	switch subcommand {
	case "add":
		text := e.SlashCommandInteractionData().String("text")
		if text == "" {
			return sendInteractionError(e, "Context text cannot be empty.", true)
		}
		cache.Context = append(cache.Context, text)
		if err := cache.Write(channelID); err != nil {
			return sendInteractionError(e, "Failed to save context: "+err.Error(), true)
		}
		return sendInteractionOk(e, "Context added", fmt.Sprintf("Added to context: `%s`", text), false)

	case "clear":
		prevLen := len(cache.Context)
		cache.Context = []string{}
		if err := cache.Write(channelID); err != nil {
			return sendInteractionError(e, "Failed to clear context: "+err.Error(), true)
		}
		return sendInteractionOk(e, "Context cleared", fmt.Sprintf("Cleared %s from context.", pluralize(prevLen, "item")), false)

	case "list":
		if len(cache.Context) == 0 {
			return sendInteractionOk(e, "Chat context", "No context set for this channel.", false)
		}
		var b strings.Builder
		b.WriteString("**Current Context:**\n")
		for i, ctx := range cache.Context {
			fmt.Fprintf(&b, "%d. %s\n", i+1, ctx)
		}
		return sendInteractionOk(e, "Chat context", ellipsisTrim(b.String(), 1024), e.SlashCommandInteractionData().Bool("ephemeral"))
	}

	return nil
}
