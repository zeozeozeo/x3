package commands

import (
	"bytes"
	"time"

	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/media"
)

var (
	containsX3Regex       = regexp.MustCompile(`(?i)(^|\P{L})(?:x3|х3|clanker|clanky|кланкер)(\P{L}|$)`)
	containsProtogenRegex = regexp.MustCompile(`(?i)(^|\P{L})(?:protogen|протоген)(\P{L}|$)`)
	containsSigmaRegex    = regexp.MustCompile(`(?i)(^|\P{L})(?:sigma|сигма)(\P{L}|$)`)
)

func handleLlmInteraction(event *events.MessageCreate) error {
	var wg sync.WaitGroup
	wg.Add(1)
	go sendTypingWithLog(event.Client(), event.ChannelID, &wg)

	content := getMessageContent(event.Message)

	_, _, err := handleLlmInteraction2(
		event.Client(),
		event.ChannelID,
		event.MessageID,
		content,
		event.Message.Author.EffectiveName(),
		event.Message.Author.ID,
		event.Message.Attachments,
		false, // not a time interaction
		false, // not a regenerate
		"",    // no regenerate prepend
		&wg,
		event.Message.ReferencedMessage,
		nil, // no event
		nil,
		false,
		event.GuildID == nil, // if no guild this is a dm
	)

	if err != nil {
		slog.Error("handleLlmInteraction failed", "err", err)
		sendPrettyError(event.Client(), "Epic fail", event.ChannelID, event.MessageID)
	}
	return err
}

var triggerCommandBotNamePrefixes = []string{"x3 ", "clanker "}

func isTriggerCommand(event *events.MessageCreate, cmd string) bool {
	content := strings.TrimSpace(event.Message.Content)
	for _, p := range triggerCommandBotNamePrefixes {
		fullCmd := p + cmd
		if strings.HasSuffix(content, fullCmd) || strings.HasPrefix(content, fullCmd) {
			return true
		}
	}
	return false
}

var llmInteractionsInProgress sync.Map

func OnMessageCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot {
		return
	}

	// trigger commands (e.g. "x3 say" "x3 quote"), available when blacklisted
	if isTriggerCommand(event, "quote") {
		if err := HandleQuoteReply(event); err != nil {
			slog.Error("HandleQuoteReply failed", "err", err)
		}
		return
	}
	if isTriggerCommand(event, "say") {
		if err := HandleSay(event, false); err != nil {
			slog.Error("HandleSay failed", "err", err)
		}
		return
	}

	if event.GuildID != nil && db.IsChannelInBlacklist(event.ChannelID) {
		return // blacklisted in this channel
	}
	if event.Message.Content == "" && len(event.Message.Attachments) == 0 {
		return // might be a poll/pin message etc
	}

	shouldTriggerLlm := false

	// are we @mentioned?
	for _, user := range event.Message.Mentions {
		if user.ID == event.Client().ID() {
			shouldTriggerLlm = true
			break
		}
	}

	// is this a reply to our message?
	if !shouldTriggerLlm && event.Message.ReferencedMessage != nil && event.Message.ReferencedMessage.Author.ID == event.Client().ID() {
		if !isLobotomyMessage(*event.Message.ReferencedMessage) && !isCardMessage(*event.Message.ReferencedMessage) {
			shouldTriggerLlm = true
		}
	}

	// "clanker"
	if !shouldTriggerLlm && containsX3Regex.MatchString(event.Message.Content) {
		shouldTriggerLlm = true
	}

	// recent interaction?
	if !shouldTriggerLlm {
		cache := db.GetChannelCache(event.ChannelID)
		if time.Since(cache.LastInteraction) < 30*time.Second {
			shouldTriggerLlm = true
		}
	}

	// is this a DM?
	if !shouldTriggerLlm && event.GuildID == nil {
		shouldTriggerLlm = true
	}

	if shouldTriggerLlm {
		if _, loaded := llmInteractionsInProgress.LoadOrStore(event.ChannelID, true); loaded {
			return
		}

		defer llmInteractionsInProgress.Delete(event.ChannelID)

		handleLlmInteraction(event)
		return
	}

	// real?
	if containsProtogenRegex.MatchString(event.Message.Content) {
		event.Client().Rest().CreateMessage(
			event.ChannelID,
			discord.NewMessageCreateBuilder().
				SetContent("https://tenor.com/view/protogen-vrchat-hello-hi-jumping-gif-18406743932972249866").
				SetMessageReferenceByID(event.MessageID).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				Build(),
		)
		return
	}
	if containsSigmaRegex.MatchString(event.Message.Content) {
		event.Client().Rest().CreateMessage(
			event.ChannelID,
			discord.NewMessageCreateBuilder().
				SetMessageReferenceByID(event.MessageID).
				SetAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				AddFile("sigma-boy.mp4", "", bytes.NewReader(media.SigmaBoyMp4)).
				Build(),
		)
		return
	}

	if event.GuildID == nil {
		handleLlmInteraction(event)
		return
	}
}
