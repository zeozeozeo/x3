// TODO: get rid of the fuckass code in here

package commands

import (
	"fmt"
	"log/slog"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
)

// makeGptCommand creates the base structure for a generic LLM slash command.
func makeGptCommand(name, desc string) discord.SlashCommandCreate {
	return discord.SlashCommandCreate{
		Name:        name,
		Description: desc,
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
			discord.ApplicationCommandOptionString{
				Name:        "prompt",
				Description: "Your message to the LLM",
				Required:    true,
			},
			discord.ApplicationCommandOptionBool{
				Name:        "ephemeral",
				Description: "If the response should only be visible to you",
				Required:    false,
			},
		},
	}
}

// makeGptCommands generates the slash command definitions for all registered LLM models.
func makeGptCommands() []discord.ApplicationCommandCreate {
	var commands []discord.ApplicationCommandCreate
	for _, m := range model.AllModels {
		if m.Command != "chat" {
			commands = append(commands, makeGptCommand(m.Command, formatModel(m)))
		}
	}
	return commands
}

// ChatCommand defines the specific /chat command.
var ChatCommand discord.ApplicationCommandCreate = makeGptCommand("chat", "Chat with the current persona")

// GptCommands holds the definitions for all model-specific commands.
var GptCommands = makeGptCommands()

// HandleLlm handles an LLM slash command.
func HandleLlm(event *handler.CommandEvent, models []model.Model) error {
	data := event.SlashCommandInteractionData()
	prompt := data.String("prompt")
	ephemeral := data.Bool("ephemeral")

	cache := db.GetChannelCache(event.Channel().ID())
	var targetModels []model.Model
	if len(models) == 0 {
		// /chat command, use model from persona
		targetModels = cache.PersonaMeta.GetModels()
	} else {
		targetModels = models
	}

	for _, targetModel := range targetModels {
		if targetModel.Whitelisted && !db.IsInWhitelist(event.User().ID) {
			return event.CreateMessage(discord.MessageCreate{
				Content: fmt.Sprintf("You are not whitelisted to use the `%s` model. Try `/chat`.", targetModel.Command),
				Flags:   discord.MessageFlagEphemeral,
			})
		}
	}

	var llmer *llm.Llmer
	// if we can't read the message history we'll use the cache
	useCache := event.GuildID() == nil || (event.AppPermissions() != nil && !event.AppPermissions().Has(discord.PermissionReadMessageHistory))
	isDM := event.Channel().Type() == discord.ChannelTypeDM
	lastInteracted := db.GetInteractionTime(event.User().ID)

	usernames := map[string]struct{}{}

	if useCache {
		llmer = cache.Llmer
		if llmer == nil {
			llmer = llm.NewLlmer()
		}
	} else {
		llmer = llm.NewLlmer()
		lastMessage := event.Channel().MessageChannel.LastMessageID()
		if lastMessage != nil {
			_, usernames, _, _, _ = addContextMessages(event.Client(), llmer, event.Channel().ID(), *lastMessage, cache.ContextLength)

			msg, err := event.Client().Rest().GetMessage(event.Channel().ID(), *lastMessage)
			if err == nil && msg != nil && msg.Interaction == nil {
				if isLobotomyMessage(*msg) {
					llmer.Lobotomize(getLobotomyAmountFromMessage(*msg))
				} else {
					msgPersona := persona.GetPersonaByMeta(cache.PersonaMeta, cache.Summary, msg.Author.EffectiveName(), isDM, lastInteracted, cache.Context)
					llmer.SetPersona(msgPersona, nil)
					llmer.AddMessage(llm.RoleUser, formatMsg(getMessageContent(*msg), msg.Author.EffectiveName(), msg.ReferencedMessage), msg.ID)
					addImageAttachments(llmer, msg.Attachments)
				}
			}
		}
	}
	usernames[event.User().EffectiveName()] = struct{}{} // to be safe when not using cache
	slog.Debug("prepared initial context", slog.Int("num_messages", llmer.NumMessages()))

	currentPersona := persona.GetPersonaByMeta(cache.PersonaMeta, cache.Summary, event.User().EffectiveName(), isDM, lastInteracted, cache.Context)
	llmer.SetPersona(currentPersona, &cache.PersonaMeta.ExcessiveSplit)
	llmer.AddMessage(llm.RoleUser, formatMsg(prompt, event.User().EffectiveName(), nil), 0)

	err := event.DeferCreateMessage(ephemeral)
	if err != nil {
		slog.Error("failed to defer interaction response", "err", err)
		return sendInteractionError(event, "failed to acknowledge command", true)
	}

	slog.Debug("requesting LLM completion via slash command",
		slog.Int("num_models", len(targetModels)),
		slog.Int("num_messages", llmer.NumMessages()),
		slog.String("prepend", cache.PersonaMeta.Prepend),
	)
	response, usage, err := llmer.RequestCompletion(targetModels, cache.PersonaMeta.Settings, cache.PersonaMeta.Prepend)
	if err != nil {
		slog.Error("LLM request failed", "err", err)
		return updateInteractionError(event, fmt.Sprintf("LLM request failed: %s", err.Error()))
	}
	slog.Debug("LLM response received", "len", len(response), "usage", usage.String())

	// update stats
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage
	if err := db.UpdateGlobalStats(usage); err != nil {
		slog.Error("failed to update global stats", "err", err)
	}

	// handle thinking tag
	var thinking string
	{
		var answer string
		thinking, answer = llm.ExtractThinking(response)
		if thinking != "" && answer != "" {
			response = answer
		}
	}

	var files []*discord.File
	if thinking != "" {
		files = append(files, &discord.File{
			Name:   "reasoning.txt",
			Reader: strings.NewReader(thinking),
		})
	}

	if ephemeral || useCache {
		response = replaceLlmTagsWithNewlines(response, &cache.PersonaMeta)
	}

	// send response
	var botMessage *discord.Message

	if ephemeral || useCache { // (single response)
		update := discord.NewMessageUpdateBuilder().SetFlags(flagsFromEphemeral(ephemeral)) // Get flags
		if utf8.RuneCountInString(response) > 2000 {
			update.SetContent("")
			update.AddFiles(&discord.File{
				Name:   fmt.Sprintf("response-%v.txt", event.ID()),
				Reader: strings.NewReader(response),
			})
			if len(files) > 0 {
				update.AddFiles(files...)
			}
		} else {
			update.SetContent(response)
			if len(files) > 0 {
				update.AddFiles(files...)
			}
		}
		botMessage, err = event.UpdateInteractionResponse(update.Build())

	} else { // (splits)
		messages := splitLlmTags(response, &cache.PersonaMeta)

		currentEvent := event
		for i, content := range messages {
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}

			currentFiles := []*discord.File{}
			if i == len(messages)-1 {
				currentFiles = files
			}

			splitMessage, splitErr := sendMessageSplits(
				event.Client(),
				0,            // not replying
				currentEvent, // only pass for the first split
				flagsFromEphemeral(ephemeral),
				event.Channel().ID(),
				content,
				currentFiles,
				i != len(messages)-1, // add zws if not the last message
				usernames,
			)
			if splitErr != nil {
				slog.Error("failed to send message split", "err", splitErr, "split_index", i)
				if i == 0 {
					return updateInteractionError(event, fmt.Sprintf("failed to send response: %s", splitErr.Error()))
				}
				break
			}
			if i == 0 {
				botMessage = splitMessage
			}
			currentEvent = nil
		}

		if botMessage == nil && len(messages) > 0 {
			fallbackEmptyResponse := "<empty response>"
			_, err = event.UpdateInteractionResponse(discord.MessageUpdate{Content: &fallbackEmptyResponse})
		}
	}

	if err != nil {
		slog.Error("failed to send/update interaction response", "err", err)
		return nil
	}

	if botMessage != nil {
		if err := db.WriteMessageInteractionPrompt(botMessage.ID, prompt); err != nil {
			slog.Error("failed to write message interaction prompt cache", "err", err)
		}
	}

	// write cache
	cache.Summary.Age++
	if useCache {
		cache.Llmer = llmer
		cache.UpdateInteractionTime()
		if err := cache.Write(event.Channel().ID()); err != nil {
			slog.Error("failed to save channel cache", "err", err)
		}
	} else {
		if err := cache.Write(event.Channel().ID()); err != nil {
			slog.Error("failed to save channel cache after non-cached interaction", "err", err)
		}
	}
	db.SetInteractionTime(event.User().ID, time.Now())

	return nil
}

// flagsFromEphemeral returns the appropriate message flags based on ephemeral.
func flagsFromEphemeral(ephemeral bool) discord.MessageFlags {
	if ephemeral {
		return discord.MessageFlagEphemeral
	}
	return discord.MessageFlagsNone
}
