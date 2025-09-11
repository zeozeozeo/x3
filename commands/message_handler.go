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
		if err := HandleSay(event); err != nil {
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
			handleLlmInteraction(event)
			return
		}
	}

	// "clanker"
	if containsX3Regex.MatchString(event.Message.Content) {
		handleLlmInteraction(event)
		return
	}

	// recent interaction?
	cache := db.GetChannelCache(event.ChannelID)
	if time.Since(cache.LastInteraction) < 30*time.Second {
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
