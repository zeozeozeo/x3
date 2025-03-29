package commands

import (
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
)

// MemoryCommand is the definition for the /memory command
var MemoryCommand = discord.SlashCommandCreate{
	Name:        "memory",
	Description: "Manage x3 memories about you",
	IntegrationTypes: []discord.ApplicationIntegrationType{
		discord.ApplicationIntegrationTypeGuildInstall,
		discord.ApplicationIntegrationTypeUserInstall,
	},
	Contexts: []discord.InteractionContextType{
		discord.InteractionContextTypeGuild,
		discord.InteractionContextTypeBotDM,
		discord.InteractionContextTypePrivateChannel,
	},
	Options: []discord.ApplicationCommandOption{
		discord.ApplicationCommandOptionSubCommand{
			Name:        "list",
			Description: "List things that x3 knows about you",
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "clear",
			Description: "Make x3 forget everything about you :(",
		},
	},
}

// HandleMemoryList handles the /memory list subcommand.
func HandleMemoryList(event *handler.CommandEvent) error {
	memories := db.GetMemories(event.User().ID)

	builder := discord.NewEmbedBuilder().
		SetTitle("🧠 Memories").
		SetColor(0x0085ff).
		SetFooter("x3", x3Icon).
		SetTimestamp(time.Now())

	if len(memories) == 0 {
		builder.SetDescription("I don't have any specific memories saved about you yet")
	} else {
		var b strings.Builder
		for i, memory := range memories {
			b.WriteString(strconv.Itoa(i + 1))
			b.WriteString(". ")
			b.WriteString(memory)
			b.WriteRune('\n')
		}
		// Trim trailing newline if any
		listContent := strings.TrimSpace(b.String())

		builder.SetDescription("Here are the things I remember about you (newest first):")
		// AddField has limits, check if content fits
		if len(listContent) <= 1024 {
			builder.AddField("List", listContent, false)
		} else {
			builder.AddField("List (truncated)", ellipsisTrim(listContent, 1024), false)
			builder.AddField("Note", "The memory list is too long to display fully here", false)
		}
		builder.AddField("Count", pluralize(len(memories), "memory"), true)
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetEmbeds(builder.Build()).
			SetEphemeral(true).
			Build(),
	)
}

// HandleMemoryClear handles the /memory clear subcommand.
func HandleMemoryClear(event *handler.CommandEvent) error {
	err := db.DeleteMemories(event.User().ID)
	if err != nil {
		return sendInteractionError(event, "failed to clear memories", true)
	}
	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetContent("🗑️ Cleared all memories about you").
			SetEphemeral(true).
			Build(),
	)
}
