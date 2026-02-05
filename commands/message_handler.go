package commands

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"log/slog"
	"regexp"
	"strings"
	"sync"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/media"
)

var (
	containsX3Regex              = regexp.MustCompile(`(?i)(^|\P{L})(?:x3|х3|clanker|clanky|кланкер)(\P{L}|$)`)
	containsProtogenRegex        = regexp.MustCompile(`(?i)(^|\P{L})(?:protogen|протоген)(\P{L}|$)`)
	containsSigmaRegex           = regexp.MustCompile(`(?i)(^|\P{L})(?:sigma|сигма)(\P{L}|$)`)
	cdnLinkRegex                 = regexp.MustCompile(`https?://(?:cdn|media)\.discordapp\.com/[^\s]+\.(?:png|jpg|jpeg|webp)(?:\?[^\s]*)?`)
	antiscamNotificationDebounce sync.Map // guildID -> time.Time
)

func handleLlmInteraction(event *events.MessageCreate) error {
	ctx, cancel := context.WithCancel(context.Background())
	if oldCancel, loaded := llmInteractionsInProgress.Swap(event.ChannelID, cancel); loaded {
		if c, ok := oldCancel.(context.CancelFunc); ok {
			c()
		}
	}
	defer func() {
		llmInteractionsInProgress.Delete(event.ChannelID)
		cancel()
	}()

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
		ctx,
	)

	if err != nil {
		slog.Error("handleLlmInteraction failed", "err", err)
		//sendPrettyError(event.Client(), "Epic fail", event.ChannelID, event.MessageID)
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
	if event.Message.Content == "" && len(event.Message.Attachments) == 0 {
		return // might be a poll/pin message etc
	}

	// anti-scam
	if event.GuildID != nil && db.IsAntiscamEnabled(*event.GuildID) {
		isScam := false
		// 4 image attachments
		if len(event.Message.Attachments) == 4 {
			allImages := true
			for _, a := range event.Message.Attachments {
				if !isImageAttachment(a) {
					allImages = false
					break
				}
			}
			if allImages {
				isScam = true
			}
		}

		// 4 discord CDN links and no other content
		if !isScam {
			content := strings.TrimSpace(event.Message.Content)
			links := cdnLinkRegex.FindAllString(content, -1)
			if len(links) == 4 {
				// check if there is any other content
				remaining := content
				for _, link := range links {
					remaining = strings.Replace(remaining, link, "", 1)
				}
				if strings.TrimSpace(remaining) == "" {
					isScam = true
				}
			}
		}

		if isScam {
			_ = event.Client().Rest().DeleteMessage(event.ChannelID, event.MessageID)

			// notify (debounced)
			if lastNotify, ok := antiscamNotificationDebounce.Load(*event.GuildID); !ok || time.Since(lastNotify.(time.Time)) > 30*time.Second {
				antiscamNotificationDebounce.Store(*event.GuildID, time.Now())
				_ = sendPrettyEmbed(event.Client(), event.ChannelID, "Anti-scam", "Deleted a message that appeared to be a scam.")
			}
			return
		}
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
	if !shouldTriggerLlm && event.Message.ReferencedMessage == nil {
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

func handleReactionAdd(client bot.Client, messageAuthorID *snowflake.ID, channelID, messageID, userID snowflake.ID, emoji discord.PartialEmoji, isDM bool) {
	if messageAuthorID == nil || *messageAuthorID != client.ID() {
		return
	}
	msg, err := client.Rest().GetMessage(channelID, messageID)
	if err != nil || msg == nil {
		slog.Warn("OnReactionAdd failed to get message", "err", err)
		return
	}
	user, err := client.Rest().GetUser(userID)
	if err != nil {
		slog.Warn("OnReactionAdd failed to get user", "err", err)
		return
	}

	var wg sync.WaitGroup
	wg.Add(1)
	go sendTypingWithLog(client, channelID, &wg)

	content := fmt.Sprintf(`<Reacted %s to %s's message "%s">`, emoji.Reaction(), msg.Author.EffectiveName(), getMessageContent(*msg))

	_, _, err = handleLlmInteraction2(
		client,
		channelID,
		messageID,
		content,
		user.EffectiveName(),
		userID,
		msg.Attachments,
		false, // not a time interaction
		false, // not a regenerate
		"",    // no regenerate prepend
		&wg,
		msg.ReferencedMessage,
		nil, // no event
		nil,
		false,
		isDM, // dm event
		context.Background(),
	)

	if err != nil {
		slog.Error("OnReactionAdd failed", "err", err)
	}
}

func OnDMMessageReactionAdd(event *events.DMMessageReactionAdd) {
	handleReactionAdd(event.Client(), event.MessageAuthorID, event.ChannelID, event.MessageID, event.UserID, event.Emoji, true)
}

func OnGuildMessageReactionAdd(event *events.GuildMessageReactionAdd) {
	handleReactionAdd(event.Client(), event.MessageAuthorID, event.ChannelID, event.MessageID, event.UserID, event.Emoji, false)
}

func OnMessageUpdate(event *events.MessageUpdate) {
	if event.Message.Author.Bot {
		return
	}

	// only handle DMs
	if event.GuildID != nil {
		return
	}

	// determine if this is the last message sent by the user
	msgs, err := event.Client().Rest().GetMessages(event.ChannelID, 0, 0, 0, 1)
	if err != nil || len(msgs) == 0 {
		return
	}

	if msgs[0].ID != event.MessageID {
		return // not the last message
	}

	// cancel existing interaction for this channel
	if cancel, loaded := llmInteractionsInProgress.Load(event.ChannelID); loaded {
		if c, ok := cancel.(context.CancelFunc); ok {
			c()
		}
	}

	// delete bot messages sent after this message
	afterMsgs, err := event.Client().Rest().GetMessages(event.ChannelID, 0, event.MessageID, 0, 10)
	if err == nil {
		for _, m := range afterMsgs {
			if m.Author.ID == event.Client().ID() {
				_ = event.Client().Rest().DeleteMessage(event.ChannelID, m.ID)
			}
		}
	}

	// trigger new interaction
	createEvent := &events.MessageCreate{
		GenericMessage: &events.GenericMessage{
			GenericEvent: event.GenericEvent,
			MessageID:    event.MessageID,
			Message:      event.Message,
			ChannelID:    event.ChannelID,
			GuildID:      event.GuildID,
		},
	}
	handleLlmInteraction(createEvent)
}
