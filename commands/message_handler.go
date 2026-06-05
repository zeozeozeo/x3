package commands

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/events"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/media"
)

var (
	containsX3Regex       = regexp.MustCompile(`(?i)(^|\P{L})(?:x3|х3|clanker|clanky|кланкер)(\P{L}|$)`)
	containsProtogenRegex = regexp.MustCompile(`(?i)(^|\P{L})(?:protogen|протоген)(\P{L}|$)`)
	containsSigmaRegex    = regexp.MustCompile(`(?i)(^|\P{L})(?:sigma|сигма)(\P{L}|$)`)
	antiscamPurgeJobs     sync.Map // guildID:userID -> struct{}
)

func handleCharacterCardImage(event *events.MessageCreate) bool {
	if event.GuildID != nil {
		return false
	}
	for _, attachment := range event.Message.Attachments {
		if !strings.HasSuffix(strings.ToLower(attachment.Filename), ".card.png") {
			continue
		}
		if attachment.ContentType == nil || !strings.HasPrefix(*attachment.ContentType, "image/") {
			continue
		}

		slog.Debug("fetching character card from attachment", "url", attachment.URL)
		resp, err := http.Get(attachment.URL)
		if err != nil {
			slog.Error("failed to fetch character card image", "err", err)
			continue
		}
		defer resp.Body.Close()

		const maxLimit = 10 * 1024 * 1024
		if resp.ContentLength > maxLimit {
			sendPrettyEmbed(event.Client(), event.ChannelID, "Character card too large", "The character card image exceeds 10MB limit.")
			continue
		}

		lr := io.LimitReader(resp.Body, maxLimit+1)
		body, err := io.ReadAll(lr)
		if err != nil {
			slog.Error("failed to read character card image", "err", err)
			continue
		}
		if len(body) > maxLimit {
			sendPrettyEmbed(event.Client(), event.ChannelID, "Character card too large", "The character card image exceeds 10MB limit.")
			continue
		}

		cache := db.GetChannelCache(event.ChannelID)
		card, err := cache.PersonaMeta.ApplyChara(body, event.Message.Author.EffectiveName())
		if err != nil {
			slog.Error("failed to apply character card", "err", err)
			sendPrettyEmbed(event.Client(), event.ChannelID, "Failed to apply character card", err.Error())
			continue
		}

		if err := cache.Write(event.ChannelID); err != nil {
			slog.Error("failed to write cache after applying chara card", "err", err)
			continue
		}

		charName := "<character card>"
		if card.Data != nil && card.Data.Name != "" {
			charName = card.Data.Name
		}
		sendPrettyEmbedReply(event.Client(), event.ChannelID, event.MessageID, "Character card applied", fmt.Sprintf("Applied character card: **%s**\n\n**Note:** you likely want to run `/lobotomy` and/or `/context clear` to clear previous context", charName))
		return true
	}
	return false
}

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
	includeNSFW := false
	if channel, ok := event.Channel(); ok {
		includeNSFW = channel.NSFW()
	}

	_, _, err := handleLlmInteraction2(
		event.Client(),
		event.ChannelID,
		event.MessageID,
		content,
		event.Message.Author.EffectiveName(),
		event.Message.Author.ID,
		event.Message.Attachments,
		event.Message.Embeds,
		false, // not a time interaction
		false, // not a regenerate
		"",    // no regenerate prepend
		&wg,
		event.Message.ReferencedMessage,
		nil, // no event
		nil,
		false,
		event.GuildID == nil, // if no guild this is a dm
		event.GuildID,
		includeNSFW,
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
	recordChannelMessageHistory(event.ChannelID, event.Message)
	if event.Message.Author.Bot {
		return
	}
	if event.Message.Content == "" && len(event.Message.Attachments) == 0 {
		return // might be a poll/pin message etc
	}

	// anti-scam
	if event.GuildID != nil && db.IsAntiscamEnabled(*event.GuildID) && db.IsAntiscamChannel(event.ChannelID) {
		if err := handleAntiscamTrigger(event); err != nil {
			slog.Error("failed to handle antiscam trigger", "err", err, slog.String("guild_id", event.GuildID.String()), slog.String("user_id", event.Message.Author.ID.String()))
		}
		return
	}

	// auto-apply character cards from .card.png images in DMs
	if handleCharacterCardImage(event) {
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

	shouldTriggerLlm := false

	// is this a DM?
	if !shouldTriggerLlm && event.GuildID == nil {
		if maybeHandlePersonaNewFlowMessage(event) {
			return
		}
		shouldTriggerLlm = true
	}

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
		if shouldTriggerContinuation(cache, getMessageContent(event.Message)) {
			shouldTriggerLlm = true
		}
	}

	if shouldTriggerLlm {
		handleLlmInteraction(event)
		return
	}

	// real?
	if containsProtogenRegex.MatchString(event.Message.Content) {
		event.Client().Rest.CreateMessage(
			event.ChannelID,
			discord.NewMessageCreate().
				WithContent("https://tenor.com/view/protogen-vrchat-hello-hi-jumping-gif-18406743932972249866").
				WithMessageReferenceByID(event.MessageID).
				WithAllowedMentions(&discord.AllowedMentions{RepliedUser: false}),
		)
		return
	}
	if containsSigmaRegex.MatchString(event.Message.Content) {
		event.Client().Rest.CreateMessage(
			event.ChannelID,
			discord.NewMessageCreate().
				WithMessageReferenceByID(event.MessageID).
				WithAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				AddFile("sigma-boy.mp4", "", bytes.NewReader(media.SigmaBoyMp4)),
		)
		return
	}
}

func handleAntiscamTrigger(event *events.MessageCreate) error {
	if event.GuildID == nil {
		return nil
	}

	guildID := *event.GuildID
	userID := event.Message.Author.ID
	triggeredAt := event.Message.CreatedAt
	if triggeredAt.IsZero() {
		triggeredAt = time.Now()
	}
	start := triggeredAt.Add(-5 * time.Minute)
	end := triggeredAt.Add(5 * time.Minute)

	if err := purgeGuildUserMessages(event.Client(), guildID, userID, start, time.Now().Add(time.Second)); err != nil {
		return err
	}

	jobKey := guildID.String() + ":" + userID.String()
	if _, loaded := antiscamPurgeJobs.LoadOrStore(jobKey, struct{}{}); loaded {
		return nil
	}
	go func() {
		defer antiscamPurgeJobs.Delete(jobKey)
		delay := time.Until(end)
		if delay > 0 {
			time.Sleep(delay)
		}
		if err := purgeGuildUserMessages(event.Client(), guildID, userID, start, end); err != nil {
			slog.Error("failed delayed antiscam purge", "err", err, slog.String("guild_id", guildID.String()), slog.String("user_id", userID.String()))
		}
	}()

	return nil
}

func purgeGuildUserMessages(client *bot.Client, guildID, userID snowflake.ID, start, end time.Time) error {
	channels, err := client.Rest.GetGuildChannels(guildID)
	if err != nil {
		return err
	}
	trapChannels, err := db.GetAntiscamChannels(guildID)
	if err != nil {
		return err
	}
	trapChannelIDs := make(map[snowflake.ID]struct{}, len(trapChannels))
	for _, channel := range trapChannels {
		trapChannelIDs[channel.ChannelID] = struct{}{}
	}

	for _, channel := range channels {
		if !isAntiscamSupportedChannel(channel) {
			continue
		}
		if _, isTrap := trapChannelIDs[channel.ID()]; !isTrap && db.IsChannelInBlacklist(channel.ID()) {
			continue
		}
		if err := purgeChannelUserMessages(client, channel.ID(), userID, start, end); err != nil {
			slog.Warn("failed to purge antiscam channel messages", "err", err, slog.String("channel_id", channel.ID().String()))
		}
	}
	return nil
}

func purgeChannelUserMessages(client *bot.Client, channelID, userID snowflake.ID, start, end time.Time) error {
	afterID := snowflake.New(start.Add(-time.Millisecond))
	beforeID := snowflake.ID(0)
	var ids []snowflake.ID

	for {
		messages, err := client.Rest.GetMessages(channelID, 0, beforeID, afterID, 100)
		if err != nil {
			return err
		}
		if len(messages) == 0 {
			break
		}

		oldestID := messages[len(messages)-1].ID
		for _, msg := range messages {
			if msg.CreatedAt.Before(start) || msg.CreatedAt.After(end) {
				continue
			}
			if msg.Author.ID == userID {
				ids = append(ids, msg.ID)
			}
		}

		oldestTime := messages[len(messages)-1].CreatedAt
		if len(messages) < 100 || !oldestTime.After(start) || oldestID == beforeID {
			break
		}
		beforeID = oldestID
	}

	return deleteMessages(client, channelID, ids)
}

func deleteMessages(client *bot.Client, channelID snowflake.ID, ids []snowflake.ID) error {
	for len(ids) > 0 {
		n := min(len(ids), 100)
		batch := ids[:n]
		if len(batch) == 1 {
			if err := client.Rest.DeleteMessage(channelID, batch[0]); err != nil {
				return err
			}
		} else if err := client.Rest.BulkDeleteMessages(channelID, batch); err != nil {
			for _, id := range batch {
				if err := client.Rest.DeleteMessage(channelID, id); err != nil {
					return err
				}
			}
		}
		ids = ids[n:]
	}
	return nil
}

func handleReactionAdd(client *bot.Client, messageAuthorID *snowflake.ID, channelID, messageID, userID snowflake.ID, emoji discord.PartialEmoji, isDM bool) {
	if messageAuthorID == nil || *messageAuthorID != client.ID() {
		return
	}
	msg, err := client.Rest.GetMessage(channelID, messageID)
	if err != nil || msg == nil {
		slog.Warn("OnReactionAdd failed to get message", "err", err)
		return
	}
	user, err := client.Rest.GetUser(userID)
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
		msg.Embeds,
		false, // not a time interaction
		false, // not a regenerate
		"",    // no regenerate prepend
		&wg,
		msg.ReferencedMessage,
		nil, // no event
		nil,
		false,
		isDM, // dm event
		msg.GuildID,
		false,
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
	recordChannelMessageHistory(event.ChannelID, event.Message)
	if event.Message.Author.Bot {
		return
	}

	// only handle DMs
	if event.GuildID != nil {
		return
	}

	// determine if this is the last message sent by the user
	msgs, err := event.Client().Rest.GetMessages(event.ChannelID, 0, 0, 0, 1)
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
	afterMsgs, err := event.Client().Rest.GetMessages(event.ChannelID, 0, event.MessageID, 0, 10)
	if err == nil {
		for _, m := range afterMsgs {
			if m.Author.ID == event.Client().ID() {
				_ = event.Client().Rest.DeleteMessage(event.ChannelID, m.ID)
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
