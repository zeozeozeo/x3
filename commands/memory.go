package commands

import (
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/lithammer/fuzzysearch/fuzzy"
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
		discord.ApplicationCommandOptionSubCommand{
			Name:        "delete",
			Description: "Remove a specific memory",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					// autocompleted
					Name:         "memory",
					Description:  "Memory to remove",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "add",
			Description: "Add a memory about you",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "memory",
					Description: "Memory to add",
					Required:    true,
				},
			},
		},
	},
}

// HandleMemoryList handles the /memory list subcommand.
func HandleMemoryList(event *handler.CommandEvent) error {
	memories := db.GetMemories(event.User().ID, 35)

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
		if utf8.RuneCountInString(listContent) <= 1024 {
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

func HandleMemoryDeleteAutocomplete(event *handler.AutocompleteEvent) error {
	dataMemory := event.Data.String("memory")

	memories := db.GetMemories(event.User().ID, -1)
	for i, m := range memories {
		memories[i] = strconv.Itoa(i+1) + ". " + m
	}
	var matches fuzzy.Ranks
	if dataMemory != "" {
		matches = fuzzy.RankFindNormalizedFold(dataMemory, memories)
		sort.Sort(matches)
	} else {
		// fake it to keep the order
		matches = make(fuzzy.Ranks, 0, len(memories))
		for i, m := range memories {
			matches = append(matches, fuzzy.Rank{
				Source:        "",
				Target:        m,
				OriginalIndex: i,
			})
		}
	}

	choices := make([]discord.AutocompleteChoice, 0, 25)
	for _, m := range matches {
		if len(choices) >= 25 {
			break
		}
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  ellipsisTrim(m.Target, 100),
			Value: strconv.Itoa(m.OriginalIndex),
		})
	}

	return event.AutocompleteResult(choices)
}

func HandleMemoryDelete(event *handler.CommandEvent) error {
	memoryIndex, err := strconv.Atoi(event.SlashCommandInteractionData().String("memory"))
	if err != nil {
		return sendInteractionError(event, "failed to parse memory index", true)
	}

	if err := db.DeleteMemory(event.User().ID, memoryIndex); err != nil {
		return sendInteractionError(event, "failed to delete memory: "+err.Error(), true)
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetContentf("🗑️ Deleted memory #%d", memoryIndex+1).
			SetEphemeral(true).
			Build(),
	)
}

func HandleMemoryAdd(event *handler.CommandEvent) error {
	if err := db.AddMemory(event.User().ID, event.SlashCommandInteractionData().String("memory")); err != nil {
		return sendInteractionError(event, "failed to add memory: "+err.Error(), true)
	}
	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetContent("🧠 Added a memory about you").
			SetEphemeral(true).
			Build(),
	)
}
