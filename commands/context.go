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
		discord.ApplicationCommandOptionSubCommand{
			Name:        "delete",
			Description: "Remove a context item by index",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{
					Name:         "n",
					Description:  "Index of the context item to remove",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "edit",
			Description: "Edit a context item",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{
					Name:         "n",
					Description:  "Index of the context item to edit",
					Required:     true,
					Autocomplete: true,
				},
				discord.ApplicationCommandOptionString{
					Name:        "new",
					Description: "New text for the context item",
					Required:    true,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "get",
			Description: "Retrieve a specific context item",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionInt{
					Name:         "n",
					Description:  "Index of the context item to retrieve",
					Required:     true,
					Autocomplete: true,
				},
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
	ephemeral := e.SlashCommandInteractionData().Bool("ephemeral")

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
		return sendInteractionOk(e, "Chat context", ellipsisTrim(b.String(), 1024), ephemeral)

	case "delete":
		n := e.SlashCommandInteractionData().Int("n")
		if n < 1 || n > len(cache.Context) {
			return sendInteractionError(e, fmt.Sprintf("Invalid index %d. Valid range is 1-%d.", n, len(cache.Context)), true)
		}
		idx := n - 1
		removedText := cache.Context[idx]
		cache.Context = append(cache.Context[:idx], cache.Context[idx+1:]...)
		if err := cache.Write(channelID); err != nil {
			return sendInteractionError(e, "Failed to save context: "+err.Error(), true)
		}
		return sendInteractionOk(e, "Context item removed", fmt.Sprintf("Removed item #%d: `%s`", n, removedText), false)

	case "edit":
		n := e.SlashCommandInteractionData().Int("n")
		newText := e.SlashCommandInteractionData().String("new")
		if n < 1 || n > len(cache.Context) {
			return sendInteractionError(e, fmt.Sprintf("Invalid index %d. Valid range is 1-%d.", n, len(cache.Context)), true)
		}
		if newText == "" {
			return sendInteractionError(e, "New text cannot be empty.", true)
		}
		idx := n - 1
		oldText := cache.Context[idx]
		cache.Context[idx] = newText
		if err := cache.Write(channelID); err != nil {
			return sendInteractionError(e, "Failed to save context: "+err.Error(), true)
		}
		return sendInteractionOk(e, "Context item edited", fmt.Sprintf("Updated item #%d:\nOld: `%s`\nNew: `%s`", n, oldText, newText), false)

	case "get":
		n := e.SlashCommandInteractionData().Int("n")
		if n < 1 || n > len(cache.Context) {
			return sendInteractionError(e, fmt.Sprintf("Invalid index %d. Valid range is 1-%d.", n, len(cache.Context)), true)
		}
		idx := n - 1
		return sendInteractionOk(e, fmt.Sprintf("Context item #%d", n), cache.Context[idx], ephemeral)
	}

	return nil
}

func handleContextAutocomplete(e *handler.AutocompleteEvent) error {
	channelID := e.Channel().ID()
	cache := db.GetChannelCache(channelID)

	return HandleGenericAutocomplete(e, "n", cache.Context, func(item any, index int) (string, string) {
		ctx := item.(string)
		name := fmt.Sprintf("#%d: %s", index+1, ctx)
		value := fmt.Sprintf("%d", index+1) // 1-based index
		return name, value
	})
}
