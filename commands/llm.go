package commands

import (
	"fmt"
	"log/slog"
	"strings"
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
				Description: "Your message to the LLM", // Simplified description
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
		// Skip adding the generic "chat" command here, it's defined separately
		if m.Command != "chat" {
			commands = append(commands, makeGptCommand(m.Command, formatModel(m))) // formatModel is in persona.go
		}
	}
	return commands
}

// ChatCommand defines the specific /chat command.
var ChatCommand discord.ApplicationCommandCreate = makeGptCommand("chat", "Chat with the current persona")

// GptCommands holds the definitions for all model-specific commands.
var GptCommands = makeGptCommands()

// HandleLlm is the main handler for all LLM-based slash commands (/chat, /gpt4o, etc.).
// If 'models' is nil, it handles the generic /chat command using the channel's current persona model.
// Otherwise, it uses the specified model 'm'.
func HandleLlm(event *handler.CommandEvent, models []model.Model) error {
	data := event.SlashCommandInteractionData()
	prompt := data.String("prompt")
	ephemeral := data.Bool("ephemeral")

	// Determine the model to use
	cache := db.GetChannelCache(event.Channel().ID())
	var targetModels []model.Model
	if len(models) == 0 {
		// Generic /chat command, use the model from channel cache/persona
		targetModels = cache.PersonaMeta.GetModels()
	} else {
		// Specific model command (e.g., /gpt4o)
		targetModels = models
	}

	// Check whitelist if necessary
	for _, targetModel := range targetModels {
		if targetModel.Whitelisted && !db.IsInWhitelist(event.User().ID) {
			return event.CreateMessage(discord.MessageCreate{
				Content: fmt.Sprintf("You are not whitelisted to use the `%s` model. Try `/chat`.", targetModel.Command),
				Flags:   discord.MessageFlagEphemeral,
			})
		}
	}

	// --- Prepare LLM context ---
	var llmer *llm.Llmer
	// Check if we need to use the cache (e.g., in DMs where history isn't readable)
	// Note: AppPermissions might be nil in DMs, handle that.
	useCache := event.GuildID() == nil || (event.AppPermissions() != nil && !event.AppPermissions().Has(discord.PermissionReadMessageHistory))

	if useCache {
		slog.Debug("using channel cache for LLM context", slog.String("channel_id", event.Channel().ID().String()))
		llmer = cache.Llmer
		if llmer == nil {
			slog.Debug("no llmer in cache; creating new", slog.String("channel_id", event.Channel().ID().String()))
			llmer = llm.NewLlmer()
			// Don't write cache here, wait until after successful interaction
		}
	} else {
		// Not using cache, fetch history
		slog.Debug("fetching message history for LLM context", slog.String("channel_id", event.Channel().ID().String()))
		llmer = llm.NewLlmer()
		lastMessage := event.Channel().MessageChannel.LastMessageID()
		if lastMessage != nil {
			// Add context messages from history
			_, _, _, _, _ = addContextMessagesIfPossible(event.Client(), llmer, event.Channel().ID(), *lastMessage, cache.ContextLength)

			// Add the very last message in the channel if it wasn't the interaction itself
			// This might fetch the interaction trigger message again, but addContextMessagesIfPossible handles duplicates by ID.
			msg, err := event.Client().Rest().GetMessage(event.Channel().ID(), *lastMessage)
			if err == nil && msg != nil && msg.Interaction == nil { // Don't add the interaction trigger message itself here
				if isLobotomyMessage(*msg) {
					llmer.Lobotomize(getLobotomyAmountFromMessage(*msg))
				} else {
					// Use the user ID from the fetched message for memory retrieval
					msgPersona := persona.GetPersonaByMeta(cache.PersonaMeta, db.GetMemories(msg.Author.ID, 0), msg.Author.EffectiveName())
					llmer.SetPersona(msgPersona) // Temporarily set persona for formatting
					llmer.AddMessage(llm.RoleUser, formatMsg(getMessageContentNoWhitelist(*msg), msg.Author.EffectiveName(), msg.ReferencedMessage), msg.ID)
					addImageAttachments(llmer, msg.Attachments)
				}
			}
		}
	}
	slog.Debug("prepared initial context", slog.Int("num_messages", llmer.NumMessages()))

	// Set the final persona for the actual request
	currentPersona := persona.GetPersonaByMeta(cache.PersonaMeta, db.GetMemories(event.User().ID, 0), event.User().EffectiveName())
	llmer.SetPersona(currentPersona)

	// Add the user's prompt from the slash command
	llmer.AddMessage(llm.RoleUser, formatMsg(prompt, event.User().EffectiveName(), nil), 0) // ID 0 for interaction message

	// Defer response (required within 3s)
	err := event.DeferCreateMessage(ephemeral)
	if err != nil {
		slog.Error("failed to defer interaction response", slog.Any("err", err))
		// Attempt to send an immediate error message if defer fails
		return sendInteractionError(event, "failed to acknowledge command", true)
	}

	// --- Execute LLM Request ---
	slog.Debug("requesting LLM completion via slash command",
		slog.Int("num_models", len(targetModels)),
		slog.Int("num_messages", llmer.NumMessages()),
		slog.String("prepend", cache.PersonaMeta.Prepend),
	)
	response, usage, err := llmer.RequestCompletion(targetModels, nil, cache.PersonaMeta.Settings, cache.PersonaMeta.Prepend) // Pass nil for usernames map as it's not easily available here
	if err != nil {
		slog.Error("LLM request failed", slog.Any("err", err))
		return updateInteractionError(event, fmt.Sprintf("LLM request failed: %s", err.Error())) // Use updateInteractionError as we deferred
	}
	slog.Debug("LLM response received", slog.Int("response_len", len(response)), slog.String("usage", usage.String()))
	// --- End LLM Request ---

	// Update stats
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage
	if err := db.UpdateGlobalStats(usage); err != nil {
		slog.Error("failed to update global stats", slog.Any("err", err)) // Log error but continue
	}

	// Process response (thinking tags, memory tags, splitting)
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

	// Handle memory tags and message splitting
	// If ephemeral or using cache, we can't rely on sendMessageSplits' multi-message capability.
	// We must combine messages and handle memories before sending.
	if ephemeral || useCache {
		response = replaceLlmTagsWithNewlines(response, event.User().ID)
	}

	// --- Send/Update Response ---
	var botMessage *discord.Message

	if ephemeral || useCache {
		// Send single message (potentially truncated or as file)
		update := discord.NewMessageUpdateBuilder().SetFlags(flagsFromEphemeral(ephemeral)) // Get flags
		if utf8.RuneCountInString(response) > 2000 {
			update.SetContent("")
			update.AddFiles(&discord.File{
				Name:   fmt.Sprintf("response-%v.txt", event.ID()),
				Reader: strings.NewReader(response),
			})
			if len(files) > 0 { // Add reasoning file if exists
				update.AddFiles(files...)
			}
		} else {
			update.SetContent(response)
			if len(files) > 0 { // Add reasoning file if exists
				update.AddFiles(files...)
			}
		}
		botMessage, err = event.UpdateInteractionResponse(update.Build())

	} else {
		// Use sendMessageSplits for non-ephemeral, non-cached responses
		// This requires splitting tags *after* sending potentially.
		// Let's adjust: handle memories first, then send splits.
		messages, memories := splitLlmTags(response)
		if err := db.HandleMemories(event.User().ID, memories); err != nil {
			slog.Error("failed to handle memories", slog.Any("err", err))
		}

		// Send the message parts
		currentEvent := event // Keep track of the event for the first split
		for i, content := range messages {
			content = strings.TrimSpace(content)
			if content == "" {
				continue
			}

			// Attach files only to the last message split
			currentFiles := []*discord.File{}
			if i == len(messages)-1 {
				currentFiles = files
			}

			// Use sendMessageSplits
			splitMessage, splitErr := sendMessageSplits(
				event.Client(),
				0,            // Not replying to a specific message
				currentEvent, // Pass event only for the first split
				flagsFromEphemeral(ephemeral),
				event.Channel().ID(),
				[]rune(content),
				currentFiles,
				i != len(messages)-1, // Add separator if not the last message
			)
			if splitErr != nil {
				slog.Error("failed to send message split", slog.Any("splitErr", splitErr), slog.Int("split_index", i))
				// If the first split fails, update interaction with error. Otherwise, log and continue?
				if i == 0 {
					return updateInteractionError(event, fmt.Sprintf("failed to send response: %s", splitErr.Error()))
				}
				break // Stop sending further splits if one fails
			}
			if i == 0 {
				botMessage = splitMessage // Store the first message sent/updated
			}
			currentEvent = nil // Don't use event for subsequent splits
		}
		// If botMessage is still nil after loop (e.g., all splits were empty), handle potential error or no-op?
		if botMessage == nil && len(messages) > 0 {
			// This case might occur if all message parts were empty strings after splitting/trimming
			slog.Warn("no message was sent despite having non-zero message parts after splitting tags")
			// Send a fallback message?
			fallbackEmptyResponse := "<empty response>"
			_, err = event.UpdateInteractionResponse(discord.MessageUpdate{Content: &fallbackEmptyResponse})
		}
	}

	if err != nil {
		slog.Error("failed to send/update interaction response", slog.Any("err", err))
		// Don't return error here as we already tried updating interaction
		return nil
	}

	// Cache the interaction prompt only if a message was successfully sent/updated
	if botMessage != nil {
		if err := db.WriteMessageInteractionPrompt(botMessage.ID, prompt); err != nil {
			slog.Error("failed to write message interaction prompt cache", slog.Any("err", err))
		}
	}

	// Update cache if it was used and potentially modified
	if useCache {
		cache.Llmer = llmer // Store the updated llmer state
		cache.UpdateInteractionTime()
		if err := cache.Write(event.Channel().ID()); err != nil {
			slog.Error("failed to save channel cache", slog.Any("err", err))
		}
	} else {
		// Even if not using cache for context, update usage stats and interaction time
		if err := cache.Write(event.Channel().ID()); err != nil {
			slog.Error("failed to save channel cache after non-cached interaction", slog.Any("err", err))
		}
	}

	return nil
}

// flagsFromEphemeral returns the appropriate message flags based on ephemeral.
func flagsFromEphemeral(ephemeral bool) discord.MessageFlags {
	if ephemeral {
		return discord.MessageFlagEphemeral
	}
	return discord.MessageFlagsNone
}
