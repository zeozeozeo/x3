package commands

import (
	"errors"
	"fmt"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
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

	if err := event.DeferCreateMessage(ephemeral); err != nil {
		return err
	}

	archive, err := buildChatArchive(event)
	attachArchive := true
	if errors.Is(err, errNoChatInteractions) {
		attachArchive = false
	} else if err != nil {
		return updateInteractionError(event, err.Error())
	}
	var archiveData []byte
	if attachArchive {
		if chatArchiveIsEmpty(archive) {
			attachArchive = false
		} else {
			archiveData, err = marshalChatArchive(archive)
			if err != nil {
				return updateInteractionError(event, err.Error())
			}
		}
	}

	writeCache := false
	if resetPersona {
		cache.PersonaMeta = db.NewChannelCache().PersonaMeta
		writeCache = true
	}
	if cache.Llmer != nil {
		cache.Llmer.Lobotomize(amount)
		writeCache = true
	}
	if cache.ImportedHistory != nil {
		writeCache = true
		if amount > 0 {
			if cache.Llmer == nil {
				cache.Llmer = llm.NewLlmer(event.Channel().ID())
				cache.Llmer.Messages = append([]llm.Message(nil), cache.ImportedHistory.Messages...)
				cache.Llmer.Lobotomize(amount)
			}
			cache.ImportedHistory.Messages = nonSystemMessages(cache.Llmer.Messages)
			if len(cache.ImportedHistory.Messages) == 0 {
				cache.ImportedHistory = nil
				cache.Llmer = nil
			}
		} else {
			cache.ImportedHistory = nil
			cache.Llmer = nil
		}
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
	if len(cache.Summaries) > 0 {
		writeCache = true
		cache.Summaries = nil
	}

	if writeCache {
		if err := cache.Write(event.Channel().ID()); err != nil {
			return updateInteractionError(event, err.Error())
		}
	}

	var flags discord.MessageFlags
	if ephemeral {
		flags = discord.MessageFlagEphemeral
	}

	update := discord.NewMessageUpdate().
		WithFlags(flags)
	if attachArchive {
		update = update.AddFiles(newChatArchiveFile(archiveData))
	}
	if amount > 0 {
		_, err = event.UpdateInteractionResponse(update.WithContentf("Removed last %d messages from the context", amount))
		return err
	}
	_, err = event.UpdateInteractionResponse(update.WithContent("Lobotomized for this channel"))
	return err
}

func nonSystemMessages(messages []llm.Message) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Role != llm.RoleSystem {
			out = append(out, msg)
		}
	}
	return out
}

func chatArchiveIsEmpty(archive chatArchive) bool {
	return len(archive.Messages) == 0 && len(archive.Summaries) == 0 && len(archive.Context) == 0
}
