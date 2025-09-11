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
)

var (
	containsX3Regex       = regexp.MustCompile(`(?i)(^|\P{L})(clanker|clanky|кланкер)(\P{L}|$)`)
	containsProtogenRegex = regexp.MustCompile(`(?i)(^|\W)(protogen|протоген)($|\W)`)
	containsSigmaRegex    = regexp.MustCompile(`(?i)(^|\W)(sigma|сигма)($|\W)`)
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
		slog.Error("handleLlmInteraction failed", slog.Any("err", err))
		sendPrettyError(event.Client(), "Epic fail", event.ChannelID, event.MessageID)
	}
	return err
}

func OnMessageCreate(event *events.MessageCreate) {
	if event.Message.Author.Bot || (event.GuildID != nil && db.IsChannelInBlacklist(event.ChannelID)) {
		return
	}
	if event.Message.Content == "" && len(event.Message.Attachments) == 0 {
		return // might be a poll/pin message etc
	}

	if event.GuildID == nil {
		handleLlmInteraction(event)
		return
	}

	// are we @mentioned?
	for _, user := range event.Message.Mentions {
		if user.ID == event.Client().ID() {
			handleLlmInteraction(event)
			return
		}
	}

	// is this a reply to our message?
	if event.Message.ReferencedMessage != nil && event.Message.ReferencedMessage.Author.ID == event.Client().ID() {
		if !isLobotomyMessage(*event.Message.ReferencedMessage) && !isCardMessage(*event.Message.ReferencedMessage) {
			slog.Debug("handling reply interaction")
			handleLlmInteraction(event)
			return
		}
	}

	// trigger command?
	if containsX3Regex.MatchString(event.Message.Content) {
		trimmed := strings.TrimSpace(event.Message.Content)
		if trimmed == "x3 quote" ||
			trimmed == "x3 quote this" ||
			strings.HasSuffix(trimmed, " x3 quote") ||
			strings.HasSuffix(trimmed, " x3 quote this") {
			if err := HandleQuoteReply(event); err != nil {
				slog.Error("HandleQuoteReply failed", slog.Any("err", err))
			}
			return
		}

		handleLlmInteraction(event)
		return
	}

	// recent interaction?
	cache := db.GetChannelCache(event.ChannelID)
	if time.Since(cache.LastInteraction) < 30*time.Second {
		handleLlmInteraction(event)
		return
	}

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
				AddFile("sigma-boy.mp4", "", bytes.NewReader(SigmaBoyMp4)).
				Build(),
		)
		return
	}
}
