package commands

import (
	"bytes"
	// _ "embed" // Removed embed directive
	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/zeozeozeo/x3/db"
)

var (
	containsX3Regex       = regexp.MustCompile(`(?i)(^|\P{L})[Xx]3(\P{L}|$)`)
	containsProtogenRegex = regexp.MustCompile(`(?i)(^|\W)(protogen|протоген)($|\W)`)
	containsSigmaRegex    = regexp.MustCompile(`(?i)(^|\W)(sigma|сигма)($|\W)`)
)

// Removed embed directive and local variable
// //go:embed ../media/sigma-boy.mp4
// var sigmaBoyMp4 []byte

// handleLlmInteraction is called by OnMessageCreate for non-command LLM triggers (mentions, replies, "x3").
func handleLlmInteraction(event *events.MessageCreate) error {
	// Send typing indicator while processing
	var wg sync.WaitGroup
	wg.Add(1)
	go sendTypingWithLog(event.Client(), event.ChannelID, &wg) // sendTypingWithLog is in utils.go

	// Get message content, potentially stripping "x3"
	content := getMessageContentNoWhitelist(event.Message) // getMessageContentNoWhitelist is in llm_context.go

	// Call the core LLM interaction logic
	// handleLlmInteraction2 is in llm_interact.go
	_, err := handleLlmInteraction2(
		event.Client(),
		event.ChannelID,
		event.MessageID,
		content,
		event.Message.Author.EffectiveName(),
		event.Message.Author.ID,
		event.Message.Attachments,
		false, // Not a time interaction
		false, // Not a regenerate
		"",    // No regenerate prepend
		&wg,   // Pass WaitGroup for typing indicator coordination
		event.Message.ReferencedMessage,
	)

	// Handle potential errors from the interaction logic
	if err != nil {
		slog.Error("handleLlmInteraction failed", slog.Any("err", err))
		// Send a user-facing error message
		// sendPrettyError is in utils.go
		sendPrettyError(event.Client(), "Sorry, I couldn't generate a response. Please try again.", event.ChannelID, event.MessageID)
	}
	return err // Return the original error for potential logging upstream
}

// OnMessageCreate is the event listener for new messages.
// It handles various triggers like mentions, replies, keywords, and DMs.
func OnMessageCreate(event *events.MessageCreate) {
	// Ignore bots and blacklisted channels
	if event.Message.Author.Bot || (event.GuildID != nil && db.IsChannelInBlacklist(*event.GuildID)) {
		return
	}
	if event.Message.Content == "" {
		return
	}

	// --- DM Handling ---
	if event.GuildID == nil {
		// Direct Message
		slog.Debug("Handling DM interaction")
		handleLlmInteraction(event)
		return // Stop processing after handling DM
	}

	// --- Guild Message Handling ---

	// Check for direct @mention
	for _, user := range event.Message.Mentions {
		if user.ID == event.Client().ID() {
			slog.Debug("Handling @mention interaction")
			handleLlmInteraction(event)
			return
		}
	}

	// Check for reply to bot's message
	if event.Message.ReferencedMessage != nil && event.Message.ReferencedMessage.Author.ID == event.Client().ID() {
		// Don't trigger on replies to /lobotomy or /persona confirmations etc.
		if !isLobotomyMessage(*event.Message.ReferencedMessage) && !isCardMessage(*event.Message.ReferencedMessage) {
			slog.Debug("handling reply interaction")
			handleLlmInteraction(event)
			return
		}
	}

	// Check for "x3" keyword trigger
	if containsX3Regex.MatchString(event.Message.Content) {
		trimmed := strings.TrimSpace(event.Message.Content)
		// Check for "x3 quote" trigger first
		if trimmed == "x3 quote" ||
			trimmed == "x3 quote this" ||
			strings.HasSuffix(trimmed, " x3 quote") ||
			strings.HasSuffix(trimmed, " x3 quote this") {
			slog.Debug("handling 'x3 quote' reply trigger")
			if err := HandleQuoteReply(event); err != nil { // HandleQuoteReply is in quote.go
				slog.Error("HandleQuoteReply failed", slog.Any("err", err))
			}
			return // Stop processing, quote handled
		}

		// Otherwise, treat as LLM trigger, erasing "x3"
		slog.Debug("handling 'x3' keyword interaction")
		handleLlmInteraction(event)
		return
	}

	// Check for "protogen" keyword
	if containsProtogenRegex.MatchString(event.Message.Content) {
		slog.Debug("handling 'protogen' keyword")
		_, err := event.Client().Rest().CreateMessage(
			event.ChannelID,
			discord.NewMessageCreateBuilder().
				SetContent("https://tenor.com/view/protogen-vrchat-hello-hi-jumping-gif-18406743932972249866").
				SetMessageReferenceByID(event.MessageID).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				Build(),
		)
		if err != nil {
			slog.Error("failed to send protogen response", slog.Any("err", err))
		}
		return
	}

	// Check for "sigma" keyword
	if containsSigmaRegex.MatchString(event.Message.Content) {
		slog.Debug("handling 'sigma' keyword")
		// Use the package variable commands.SigmaBoyMp4
		if len(SigmaBoyMp4) == 0 {
			slog.Error("SigmaBoyMp4 data is empty!") // Log error if data wasn't loaded
			return
		}
		_, err := event.Client().Rest().CreateMessage(
			event.ChannelID,
			discord.NewMessageCreateBuilder().
				SetMessageReferenceByID(event.MessageID).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				AddFile("sigma-boy.mp4", "", bytes.NewReader(SigmaBoyMp4)). // Use package variable
				Build(),
		)
		if err != nil {
			slog.Error("failed to send sigma response", slog.Any("err", err))
		}
		return
	}

	// No relevant trigger found in guild message
}
