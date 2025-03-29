package commands

import (
	"fmt"
	"log/slog"
	"math/rand"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events" // Added for HandleQuoteReply
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/lithammer/fuzzysearch/fuzzy"
	"github.com/zeozeozeo/x3/db"
)

// QuoteCommand is the definition for the /quote command
var QuoteCommand = discord.SlashCommandCreate{
	Name:        "quote",
	Description: "Get a server quote. Reply to a message with \"x3 quote\" to make a new quote",
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
			Name:        "get",
			Description: "Get a quote by name",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					// autocompleted
					Name:         "name",
					Description:  "Name of the quote (use #number or search text)",
					Required:     true,
					Autocomplete: true,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "random",
			Description: "Get a random quote",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "new",
			Description: "Make a quote of your own. Available in DMs",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					Name:        "text",
					Description: "Text of the quote",
					Required:    true,
				},
				discord.ApplicationCommandOptionBool{
					Name:        "ephemeral",
					Description: "If the response should only be visible to you",
					Required:    false,
				},
			},
		},
		discord.ApplicationCommandOptionSubCommand{
			Name:        "remove",
			Description: "Remove a quote. Only available to server moderators",
			Options: []discord.ApplicationCommandOption{
				discord.ApplicationCommandOptionString{
					// autocompleted
					Name:         "name",
					Description:  "Name of the quote to remove (use #number or search text)",
					Required:     true,
					Autocomplete: true,
				},
			},
		},
	},
}

// sendQuote sends a formatted quote embed.
func sendQuote(event *handler.CommandEvent, client bot.Client, channelID, messageID snowflake.ID, quote db.Quote, nr int) error {
	text := fmt.Sprintf(
		"“%s”\n\n\\- <@%d> in <#%d>, quoted by <@%d>",
		quote.Text,
		quote.AuthorID,
		quote.Channel,
		quote.Quoter,
	)

	builder := discord.NewEmbedBuilder().
		SetColor(0xFFD700).
		SetTitle(fmt.Sprintf("📜 Quote #%d", nr)).
		SetTimestamp(quote.Timestamp).
		SetDescription(text)
	if quote.AttachmentURL != "" {
		builder.SetImage(quote.AttachmentURL)
	}

	if channelID != 0 && messageID != 0 { // Used for replying to the "x3 quote" message
		_, err := client.Rest().CreateMessage(
			channelID,
			discord.NewMessageCreateBuilder().
				SetMessageReferenceByID(messageID).
				SetAllowedMentions(&discord.AllowedMentions{
					RepliedUser: false,
				}).
				AddEmbeds(builder.Build()).
				Build(),
		)
		return err
	} else if event != nil { // Used for slash command responses
		return event.CreateMessage(
			discord.NewMessageCreateBuilder().
				AddEmbeds(builder.Build()).
				SetEphemeral(event.SlashCommandInteractionData().Bool("ephemeral")).
				Build(),
		)
	}
	// Should not happen
	return fmt.Errorf("sendQuote called with no event and no channel/message ID")
}

// getServerFromEvent gets the ServerStats and server ID based on the command event context.
func getServerFromEvent(event *handler.CommandEvent) (db.ServerStats, snowflake.ID, error) {
	var serverID snowflake.ID
	if event.GuildID() != nil {
		serverID = *event.GuildID()
	} else {
		serverID = event.Channel().ID() // Use channel ID for DMs
	}

	server, err := db.GetServerStats(serverID)
	if err != nil {
		slog.Error("failed to get server stats", slog.Any("err", err), slog.String("server_id", serverID.String()))
		// Don't return the error here, let the handler decide how to respond
	}
	return server, serverID, err
}

// HandleQuoteGetAutocomplete handles autocomplete for quote name/number.
func HandleQuoteGetAutocomplete(event *handler.AutocompleteEvent) error {
	var serverID snowflake.ID
	if event.GuildID() != nil {
		serverID = *event.GuildID()
	} else {
		serverID = event.Channel().ID() // Use channel ID for DMs
	}

	server, err := db.GetServerStats(serverID)
	if err != nil {
		slog.Error("autocomplete: failed to get server stats", slog.Any("err", err))
		return event.AutocompleteResult(nil) // Return empty choices on error
	}

	name := event.Data.String("name")
	slog.Debug("handling quote autocomplete", slog.String("name", name), slog.String("server_id", serverID.String()))

	var names []string
	for i, quote := range server.Quotes {
		// Include both text and author for better searchability
		names = append(names, fmt.Sprintf("#%d %s by %s", i+1, quote.Text, quote.AuthorUser))
	}

	matches := fuzzy.RankFindNormalizedFold(name, names)
	sort.Sort(matches)

	var choices []discord.AutocompleteChoice
	for _, match := range matches {
		if len(choices) >= 25 {
			break
		}
		quote := server.Quotes[match.OriginalIndex]
		// Display format: #Number: Quote Text (Author)
		res := fmt.Sprintf("#%d: %s (%s)", match.OriginalIndex+1, quote.Text, quote.AuthorUser)
		choices = append(choices, discord.AutocompleteChoiceString{
			Name:  ellipsisTrim(res, 100),                   // Trim for Discord's limit
			Value: fmt.Sprintf("%d", match.OriginalIndex+1), // Value is the 1-based index
		})
	}

	return event.AutocompleteResult(choices)
}

// HandleQuoteGet handles the /quote get subcommand.
func HandleQuoteGet(event *handler.CommandEvent) error {
	// The value from autocomplete is the 1-based index as a string
	idxStr := event.SlashCommandInteractionData().String("name")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		// This might happen if the user manually types something invalid
		return sendInteractionError(event, fmt.Sprintf("Invalid quote number: %s. Please use the autocomplete suggestions.", idxStr), true)
	}

	// Convert to 0-based index
	idx--

	server, _, err := getServerFromEvent(event)
	if err != nil {
		return sendInteractionError(event, "Failed to load quotes for this server.", true)
	}

	if idx < 0 || idx >= len(server.Quotes) {
		return sendInteractionError(event, fmt.Sprintf("Quote #%d does not exist.", idx+1), true)
	}

	return sendQuote(event, event.Client(), 0, 0, server.Quotes[idx], idx+1)
}

// HandleQuoteRandom handles the /quote random subcommand.
func HandleQuoteRandom(event *handler.CommandEvent) error {
	server, _, err := getServerFromEvent(event)
	if err != nil {
		return sendInteractionError(event, "Failed to load quotes for this server.", true)
	}

	if len(server.Quotes) == 0 {
		return sendInteractionError(event, "No quotes found in this server/DM.", true)
	}

	nr := rand.Intn(len(server.Quotes))
	return sendQuote(event, event.Client(), 0, 0, server.Quotes[nr], nr+1)
}

// HandleQuoteNew handles the /quote new subcommand.
func HandleQuoteNew(event *handler.CommandEvent) error {
	text := strings.TrimSpace(event.SlashCommandInteractionData().String("text"))

	if len(text) == 0 {
		return sendInteractionError(event, "Cannot create an empty quote.", true)
	}

	server, serverID, err := getServerFromEvent(event)
	if err != nil {
		return sendInteractionError(event, "Failed to load quotes for this server.", true)
	}

	quote := db.Quote{
		// MessageID is 0 for manually created quotes
		Quoter:     event.User().ID,
		AuthorID:   event.User().ID, // Author is the quoter
		AuthorUser: event.User().EffectiveName(),
		Channel:    event.Channel().ID(),
		Text:       text,
		Timestamp:  event.CreatedAt(),
	}

	if exists, nr := server.QuoteExists(quote); exists {
		return sendInteractionError(event, fmt.Sprintf("This exact quote already exists (#%d).", nr+1), true)
	}

	nr := server.AddQuote(quote)

	if err := server.Write(serverID); err != nil {
		slog.Error("failed to save server stats after adding quote", slog.Any("err", err))
		return sendInteractionError(event, "Failed to save the new quote.", true)
	}

	// Send the newly created quote back
	return sendQuote(event, event.Client(), 0, 0, server.Quotes[nr-1], nr) // Use nr-1 for 0-based index access
}

// HandleQuoteRemove handles the /quote remove subcommand.
func HandleQuoteRemove(event *handler.CommandEvent) error {
	// Check permissions only if in a guild context
	if event.Member() != nil && !isModerator(event.Member().Permissions) {
		return sendInteractionError(event, "Only server moderators can remove quotes.", true)
	}

	// The value from autocomplete is the 1-based index as a string
	idxStr := event.SlashCommandInteractionData().String("name")
	idx, err := strconv.Atoi(idxStr)
	if err != nil {
		return sendInteractionError(event, fmt.Sprintf("Invalid quote number: %s. Please use the autocomplete suggestions.", idxStr), true)
	}

	// Convert to 0-based index
	idx--

	server, serverID, err := getServerFromEvent(event)
	if err != nil {
		return sendInteractionError(event, "Failed to load quotes for this server.", true)
	}

	if idx < 0 || idx >= len(server.Quotes) {
		return sendInteractionError(event, fmt.Sprintf("Quote #%d does not exist.", idx+1), true)
	}

	// Store quote details before removing for the confirmation message
	removedQuoteText := server.Quotes[idx].Text
	removedQuoteAuthor := server.Quotes[idx].AuthorUser

	server.RemoveQuote(idx)

	if err := server.Write(serverID); err != nil {
		slog.Error("failed to save server stats after removing quote", slog.Any("err", err))
		// Attempt to add the quote back? Maybe too complex. Just report error.
		return sendInteractionError(event, "Failed to save changes after removing the quote.", true)
	}

	return event.CreateMessage(
		discord.NewMessageCreateBuilder().
			SetEphemeral(true). // Removal confirmation is ephemeral
			AddEmbeds(
				discord.NewEmbedBuilder().
					SetTitle("🗑️ Quote Removed").
					SetColor(0x0085ff).
					SetDescription(fmt.Sprintf("Removed quote #%d: \"%s\" by %s", idx+1, ellipsisTrim(removedQuoteText, 50), removedQuoteAuthor)).
					SetFooter("x3", x3Icon).
					SetTimestamp(time.Now()).
					Build(),
			).
			Build(),
	)
}

// HandleQuoteReply handles creating a quote via replying "x3 quote" to a message.
func HandleQuoteReply(event *events.MessageCreate) error {
	if event.Message.ReferencedMessage == nil {
		return sendPrettyError(
			event.Client(),
			"You must reply to a message to quote it.",
			event.ChannelID,
			event.MessageID,
		)
	}

	var serverID snowflake.ID
	if event.GuildID != nil {
		serverID = *event.GuildID
	} else {
		serverID = event.ChannelID // Use channel ID for DMs
	}

	server, err := db.GetServerStats(serverID)
	if err != nil {
		slog.Error("HandleQuoteReply: failed to get server stats", slog.Any("err", err))
		return sendPrettyError(event.Client(), "Failed to load server quotes.", event.ChannelID, event.MessageID)
	}

	// Prepare quote data from the referenced message
	refMsg := event.Message.ReferencedMessage
	var attachmentURL string
	content := refMsg.Content
	if len(refMsg.Attachments) > 0 {
		// Prioritize image attachments for the embed image
		for _, att := range refMsg.Attachments {
			if isImageAttachment(att) { // isImageAttachment is in llm_context.go
				attachmentURL = att.URL
				break
			}
		}
		// If no image, use the first attachment's URL (if any)
		if attachmentURL == "" {
			attachmentURL = refMsg.Attachments[0].URL
		}
		// Append filename to content for context
		content += fmt.Sprintf(" (attached %s)", refMsg.Attachments[0].Filename)
	}
	content = strings.TrimSpace(content)
	if content == "" && attachmentURL == "" {
		return sendPrettyError(event.Client(), "Cannot quote an empty message with no attachments.", event.ChannelID, event.MessageID)
	}

	quote := db.Quote{
		MessageID:     refMsg.ID,
		Quoter:        event.Message.Author.ID, // The user who sent "x3 quote"
		AuthorID:      refMsg.Author.ID,        // The author of the message being quoted
		AuthorUser:    refMsg.Author.EffectiveName(),
		Channel:       refMsg.ChannelID,
		Text:          content,
		AttachmentURL: attachmentURL,
		Timestamp:     refMsg.CreatedAt,
	}

	if exists, nr := server.QuoteExists(quote); exists {
		return sendPrettyError(
			event.Client(),
			fmt.Sprintf("This message is already quote #%d.", nr+1),
			event.ChannelID,
			event.MessageID,
		)
	}

	nr := server.AddQuote(quote)

	if err := server.Write(serverID); err != nil {
		slog.Error("HandleQuoteReply: failed to save server stats", slog.Any("err", err))
		return sendPrettyError(event.Client(), "Failed to save the new quote.", event.ChannelID, event.MessageID)
	}

	// Send the quote embed as a reply to the "x3 quote" message
	return sendQuote(
		nil, // No command event for message create
		event.Client(),
		event.ChannelID,
		event.MessageID,     // Reply to the "x3 quote" message itself
		server.Quotes[nr-1], // Use nr-1 for 0-based index access
		nr,
	)
}
