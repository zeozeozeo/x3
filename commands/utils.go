package commands

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
)

const (
	x3Icon              = "https://i.imgur.com/ckpztZY.png"
	x3ErrorIcon         = "https://i.imgur.com/hCF06SC.png"
	interactionReminder = "\n-# if you wish to disable this, use `/random_dms enable: false`"
)

type AutocompleteItemFormatter func(item any, index int) (name string, value string)

func HandleGenericAutocomplete(
	event *handler.AutocompleteEvent,
	optionName string,
	items any,
	formatter AutocompleteItemFormatter,
) error {
	query := strings.TrimSpace(event.Data.String(optionName))

	var searchStrings []string
	var itemCount int

	switch v := items.(type) {
	case []string:
		searchStrings = v
		itemCount = len(v)
		if formatter == nil {
			formatter = func(item any, index int) (string, string) {
				return fmt.Sprintf("#%d %s", index+1, item.(string)), fmt.Sprintf("%d", index+1)
			}
		}
	default:
		if formatter == nil {
			return event.AutocompleteResult(nil)
		}
		val := reflect.ValueOf(items)
		if val.Kind() != reflect.Slice {
			return event.AutocompleteResult(nil)
		}
		itemCount = val.Len()
		searchStrings = make([]string, itemCount)
		for i := range itemCount {
			name, _ := formatter(val.Index(i).Interface(), i)
			searchStrings[i] = name
		}
	}

	if itemCount == 0 {
		return event.AutocompleteResult(nil)
	}

	var choices []discord.AutocompleteChoice

	if query == "" {
		for i := range min(itemCount, 25) {
			var name, value string
			switch v := items.(type) {
			case []string:
				name, value = formatter(v[i], i)
			default:
				val := reflect.ValueOf(items)
				name, value = formatter(val.Index(i).Interface(), i)
			}

			choices = append(choices, discord.AutocompleteChoiceString{
				Name:  ellipsisTrim(name, 100),
				Value: value,
			})
		}
	} else {
		indices := rankItems(query, searchStrings)

		for _, idx := range indices {
			if len(choices) >= 25 {
				break
			}

			var name, value string
			switch v := items.(type) {
			case []string:
				name, value = formatter(v[idx], idx)
			default:
				val := reflect.ValueOf(items)
				name, value = formatter(val.Index(idx).Interface(), idx)
			}

			choices = append(choices, discord.AutocompleteChoiceString{
				Name:  ellipsisTrim(name, 100),
				Value: value,
			})
		}
	}

	return event.AutocompleteResult(choices)
}

// rankItems scores each search string against the query and returns indices sorted best-first.
func rankItems(query string, searchStrings []string) []int {
	q := strings.ToLower(strings.TrimSpace(query))

	type scoredIdx struct {
		idx   int
		score int
	}
	scored := make([]scoredIdx, 0, len(searchStrings))

	for i, s := range searchStrings {
		score := scoreItem(q, strings.ToLower(s))
		if score > 0 {
			scored = append(scored, scoredIdx{i, score})
		}
	}

	sort.Slice(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].idx < scored[j].idx
	})

	indices := make([]int, len(scored))
	for i, s := range scored {
		indices[i] = s.idx
	}
	return indices
}

// scoreItem ranks how well query q matches target t (both lowercase).
// Higher score = better match. Returns 0 for no match.
func scoreItem(q, t string) int {
	// 1. exact match
	if q == t {
		return 100000
	}

	// 2. substring match
	if pos := strings.Index(t, q); pos >= 0 {
		return 90000 - pos
	}

	qWords := strings.Fields(q)
	tWords := strings.Fields(t)

	// 3. query matches a whole word in target
	for _, tw := range tWords {
		if q == tw {
			return 80000
		}
	}

	// 4. query is prefix of word
	for _, tw := range tWords {
		if strings.HasPrefix(tw, q) {
			return 70000
		}
	}

	// 5. query word matches a target word
	if len(qWords) > 1 {
		matchCount := 0
		for _, qw := range qWords {
			for _, tw := range tWords {
				if qw == tw || strings.HasPrefix(tw, qw) {
					matchCount++
					break
				}
			}
		}
		if matchCount > 0 {
			return 60000 + (matchCount * 5000 / len(qWords))
		}
	}

	// 6. each query word appears as substring in a target word
	matchCount := 0
	for _, qw := range qWords {
		for _, tw := range tWords {
			if strings.Contains(tw, qw) {
				matchCount++
				break
			}
		}
	}
	if matchCount > 0 {
		return 50000 + (matchCount * 5000 / len(qWords))
	}

	// 7. fuzzy match
	qi := 0
	matchLen := 0
	for ti := 0; ti < len(t) && qi < len(q); ti++ {
		if q[qi] == t[ti] {
			matchLen++
			qi++
		}
	}

	if matchLen == 0 {
		return 0
	}

	return matchLen * 40000 / len(q)
}

// sendInteractionError sends a formatted error message as a response to a command event
func sendInteractionError(event *handler.CommandEvent, msg string, ephemeral bool) error {
	return event.CreateMessage(
		discord.NewMessageCreate().
			WithAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			WithEphemeral(ephemeral).
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0xf54242).
					WithTitle("❌ Error").
					WithFooter("x3", x3ErrorIcon).
					WithTimestamp(time.Now()).
					WithDescription(toTitle(msg)),
			),
	)
}

func sendInteractionErrorComponent(event *handler.ComponentEvent, msg string, ephemeral bool) error {
	return event.CreateMessage(
		discord.NewMessageCreate().
			WithAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			WithEphemeral(ephemeral).
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0xf54242).
					WithTitle("❌ Error").
					WithFooter("x3", x3ErrorIcon).
					WithTimestamp(time.Now()).
					WithDescription(toTitle(msg)),
			),
	)
}

func sendInteractionOk(event *handler.CommandEvent, title, msg string, ephemeral bool) error {
	return event.CreateMessage(
		discord.NewMessageCreate().
			WithAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			WithEphemeral(ephemeral).
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0x0085ff).
					WithTitle(title).
					WithFooter("x3", x3Icon).
					WithDescription(ellipsisTrim(msg, 1024)),
			),
	)
}

// updateInteractionError updates an interaction response with a formatted error message.
func updateInteractionError(event *handler.CommandEvent, msg string) error {
	_, err := event.UpdateInteractionResponse(
		discord.NewMessageUpdate().
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0xf54242).
					WithTitle("❌ Error").
					WithFooter("x3", x3ErrorIcon).
					WithTimestamp(time.Now()).
					WithDescription(toTitle(msg)),
			),
	)
	return err
}

func sendPrettyEmbed(client *bot.Client, channelID snowflake.ID, title, text string) error {
	_, err := client.Rest.CreateMessage(
		channelID,
		discord.NewMessageCreate().
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0xFFD700).
					WithTitle(title).
					WithDescription(ellipsisTrim(text, 1024)).
					WithFooter("x3", x3Icon).
					WithTimestamp(time.Now()),
			),
	)
	return err
}

func sendPrettyEmbedReply(client *bot.Client, channelID snowflake.ID, replyMessageID snowflake.ID, title, text string) error {
	_, err := client.Rest.CreateMessage(
		channelID,
		discord.NewMessageCreate().
			WithMessageReferenceByID(replyMessageID).
			WithAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0xFFD700).
					WithTitle(title).
					WithDescription(ellipsisTrim(text, 1024)).
					WithFooter("x3", x3Icon).
					WithTimestamp(time.Now()),
			),
	)
	return err
}

// sendPrettyError sends a formatted error message as a reply to a regular message.
func sendPrettyError(client *bot.Client, msg string, channelID, messageID snowflake.ID) error {
	_, err := client.Rest.CreateMessage(
		channelID,
		discord.NewMessageCreate().
			WithMessageReferenceByID(messageID).
			WithAllowedMentions(&discord.AllowedMentions{
				RepliedUser: false,
			}).
			AddEmbeds(
				discord.NewEmbed().
					WithColor(0xf54242).
					WithTitle("❌ Error").
					WithFooter("x3", x3ErrorIcon).
					WithTimestamp(time.Now()).
					WithDescription(toTitle(msg)),
			),
	)
	return err
}

// sendTypingWithLog sends a typing indicator and logs any errors.
func sendTypingWithLog(client *bot.Client, channelID snowflake.ID, wg *sync.WaitGroup) {
	defer wg.Done()
	// typing state lasts for 10s
	if err := client.Rest.SendTyping(channelID); err != nil {
		slog.Error("failed to SendTyping", "err", err, slog.String("channel_id", channelID.String()))
	}
}

func interactionChannelNSFW(channel discord.InteractionChannel) bool {
	if guildChannel, ok := channel.MessageChannel.(discord.GuildMessageChannel); ok {
		return guildChannel.NSFW()
	}
	return false
}

// toTitle capitalizes the first letter of a string.
func toTitle(str string) string {
	if len(str) == 0 {
		return str
	}
	runes := []rune(str)
	runes[0] = unicode.ToUpper(runes[0])
	return string(runes)
}

// endsWithWhitespace checks if a string ends with a whitespace character.
func endsWithWhitespace(s string) bool {
	if len(s) == 0 {
		return false
	}
	return unicode.IsSpace(rune(s[len(s)-1]))
}

// ftoa converts a float32 to a string.
func ftoa(f float32) string {
	return strconv.FormatFloat(float64(f), 'g', 6, 32)
}

// dtoa converts a float64 to a string.
func dtoa(f float64) string {
	return strconv.FormatFloat(f, 'g', 6, 64)
}

// zifnil returns 0 if the int pointer is nil, otherwise returns the dereferenced value.
func zifnil(val *int) int {
	if val == nil {
		return 0
	}
	return *val
}

// ellipsisTrim trims a string to a maximum length, adding an ellipsis if trimmed.
func ellipsisTrim(s string, length int) string {
	if length <= 0 {
		return "…"
	}
	if s == "" {
		return s
	}
	r := []rune(s)
	if len(r) > length {
		return string(r[:length-1]) + "…"
	}
	return s
}

// isModerator checks if the user has moderator permissions.
func isModerator(p discord.Permissions) bool {
	return p.Has(discord.PermissionManageRoles) ||
		p.Has(discord.PermissionAdministrator) ||
		p.Has(discord.PermissionModerateMembers)
}

// pluralize returns the singular or plural form of a word based on the count.
// Handles basic English pluralization rules.
func pluralize(count int, singular string) string {
	if count == 1 {
		return fmt.Sprintf("%d %s", count, singular)
	}

	plural := singular + "s"
	if strings.HasSuffix(singular, "y") && !strings.ContainsAny(string(singular[len(singular)-2]), "aeiou") {
		plural = singular[:len(singular)-1] + "ies"
	} else if strings.HasSuffix(singular, "ch") || strings.HasSuffix(singular, "sh") || strings.HasSuffix(singular, "x") || strings.HasSuffix(singular, "s") || strings.HasSuffix(singular, "z") {
		plural = singular + "es"
	}

	return fmt.Sprintf("%d %s", count, plural)
}

type messagePart struct {
	Content string
	IsLatex bool
	Display bool
}

// \fcolorbox{white}{white} for a bit of padding (hacky but works)
const latexAPI = `https://latex.codecogs.com/png.image?\huge&space;\dpi{80}\fcolorbox{white}{white}{`

// latex equation->api call
func toLatexAPI(equation string) string {
	if !strings.HasPrefix(equation, "$") {
		// \displaystyle to use block rendering in \fcolorbox
		equation = `$\displaystyle ` + equation + `$`
	}
	return latexAPI + url.PathEscape(equation) + `}`
}

// api call->latex equation
func fromLatexAPI(api string) string {
	equation, _ := url.PathUnescape(strings.TrimPrefix(api, latexAPI))
	if len(equation) > 0 && equation[len(equation)-1] == '}' {
		equation = equation[:len(equation)-1]
	}
	equation = strings.TrimPrefix(equation, `$\displaystyle `)
	if !strings.HasPrefix(equation, "$") {
		equation = "$" + equation
	}
	if !strings.HasSuffix(equation, "$") {
		equation = equation + "$"
	}
	return equation
}

func parseMessageForLatex(content string) []messagePart {
	rawParts := parseLatexSegments(content, latexParseOptions{})
	parts := make([]messagePart, 0, len(rawParts))
	for _, part := range rawParts {
		if part.IsLatex {
			part.Content = toLatexAPI(part.Content)
		}
		parts = append(parts, part)
	}
	return parts
}

type latexParseOptions struct {
	AllowSlashDelimiters   bool
	AllowUnclosedDisplay   bool
	AllowUnclosedBracketed bool
}

func parseLatexSegments(content string, opts latexParseOptions) []messagePart {
	var parts []messagePart
	scanner := 0
	textStart := 0

	for scanner < len(content) {
		if scanner+1 < len(content) && content[scanner:scanner+2] == "$$" {
			end := strings.Index(content[scanner+2:], "$$")
			if end == -1 {
				if opts.AllowUnclosedDisplay && isLikelyUnclosedDisplayStart(content, scanner) && validDisplayLatexContent(content[scanner+2:]) {
					if scanner > textStart {
						parts = append(parts, messagePart{Content: content[textStart:scanner]})
					}
					parts = append(parts, messagePart{Content: strings.TrimSpace(content[scanner+2:]), IsLatex: true, Display: true})
					scanner = len(content)
					textStart = scanner
					break
				}
				scanner += 2
				break
			}

			latexContent := content[scanner+2 : scanner+2+end]
			if !validDisplayLatexContent(latexContent) {
				scanner += 2
				continue
			}
			if scanner > textStart {
				parts = append(parts, messagePart{Content: content[textStart:scanner]})
			}
			parts = append(parts, messagePart{Content: strings.TrimSpace(latexContent), IsLatex: true, Display: true})

			scanner += 2 + end + 2
			textStart = scanner
		} else if opts.AllowSlashDelimiters && scanner+1 < len(content) && content[scanner] == '\\' && (content[scanner+1] == '[' || content[scanner+1] == '(') {
			open := content[scanner+1]
			closeDelim := `\)`
			display := false
			if open == '[' {
				closeDelim = `\]`
				display = true
			}
			end := strings.Index(content[scanner+2:], closeDelim)
			if end == -1 {
				if opts.AllowUnclosedBracketed && display && isLikelyUnclosedDisplayStart(content, scanner) && validDisplayLatexContent(content[scanner+2:]) {
					if scanner > textStart {
						parts = append(parts, messagePart{Content: content[textStart:scanner]})
					}
					parts = append(parts, messagePart{Content: strings.TrimSpace(content[scanner+2:]), IsLatex: true, Display: true})
					scanner = len(content)
					textStart = scanner
					break
				}
				scanner += 2
				continue
			}
			latexContent := content[scanner+2 : scanner+2+end]
			if display {
				if !validDisplayLatexContent(latexContent) {
					scanner += 2
					continue
				}
			} else if !validLatexContent(latexContent) {
				scanner += 2
				continue
			}
			if scanner > textStart {
				parts = append(parts, messagePart{Content: content[textStart:scanner]})
			}
			parts = append(parts, messagePart{Content: strings.TrimSpace(latexContent), IsLatex: true, Display: display})
			scanner += 2 + end + len(closeDelim)
			textStart = scanner
			continue
		} else if content[scanner] == '$' {
			end := -1
			searchStart := scanner + 1
			for {
				next := strings.Index(content[searchStart:], "$")
				if next == -1 {
					break
				}
				if searchStart+next > 0 && content[searchStart+next-1] == '\\' {
					searchStart += next + 1
					continue
				}
				end = searchStart + next
				break
			}
			if end != -1 {
				latexContent := content[scanner+1 : end]
				if validLatexContent(latexContent) {
					if scanner > textStart {
						parts = append(parts, messagePart{Content: content[textStart:scanner]})
					}
					parts = append(parts, messagePart{Content: strings.TrimSpace(latexContent), IsLatex: true})
					scanner = end + 1
					textStart = scanner
					continue
				}
			}
			scanner++
		} else {
			scanner++
		}
	}
	if textStart < len(content) {
		parts = append(parts, messagePart{Content: content[textStart:]})
	}
	return parts
}

func validLatexContent(content string) bool {
	return len(content) > 0 &&
		!strings.HasPrefix(content, " ") && !strings.HasSuffix(content, " ") &&
		!strings.HasPrefix(content, "\n") && !strings.HasSuffix(content, "\n")
}

func validDisplayLatexContent(content string) bool {
	return strings.TrimSpace(content) != ""
}

func isLikelyUnclosedDisplayStart(content string, scanner int) bool {
	before := strings.TrimSpace(content[:scanner])
	return before == "" || strings.HasSuffix(before, ":")
}

func sendMessageSplits(
	client *bot.Client,
	messageID snowflake.ID, // message to reply to (0 if not replying or using interaction event)
	event *handler.CommandEvent, // interaction event (nil if sending regular message)
	flags discord.MessageFlags, // message flags (e.g., ephemeral)
	channelID snowflake.ID, // channel to send to
	content string, // content runes
	files []*discord.File, // files to attach (only sent with the last split)
	sepFlag bool, // add an invisible character to the last split to indicate joining needed
	usernames map[string]struct{},
) (*discord.Message, error) {
	parts := parseMessageForLatex(content)

	if len(parts) == 0 && len(files) == 0 {
		return nil, nil
	}

	var firstBotMessage *discord.Message
	isFirstMessage := true

	for i, part := range parts {
		currentFiles := []*discord.File{}
		if i == len(parts)-1 {
			currentFiles = files
		}

		// add separator flag only to the very last part
		currentSepFlag := sepFlag && (i == len(parts)-1)

		// remove prepended usernames (some models like gpt-5 are dumb enough for this)
		content2 := part.Content
		for username := range usernames {
			prefix := username + ": "
			if len(content2) >= len(prefix) && strings.EqualFold(content2[:len(prefix)], prefix) {
				content2 = content2[len(prefix):]
			}
		}

		msg, err := sendTextPart(client, &messageID, &event, flags, channelID, []rune(content2), currentFiles, currentSepFlag, &isFirstMessage, part.IsLatex)
		if err != nil {
			return firstBotMessage, err
		}
		if isFirstMessage && msg != nil {
			firstBotMessage = msg
			isFirstMessage = false
		}
	}

	// no content, only files
	if len(parts) == 0 && len(files) > 0 {
		return sendTextPart(client, &messageID, &event, flags, channelID, []rune{}, files, sepFlag, &isFirstMessage, false)
	}

	return firstBotMessage, nil
}

func sendTextPart(
	client *bot.Client,
	messageID *snowflake.ID,
	event **handler.CommandEvent,
	flags discord.MessageFlags,
	channelID snowflake.ID,
	runes []rune,
	files []*discord.File,
	sepFlag bool,
	isFirst *bool,
	latex bool,
) (*discord.Message, error) {
	maxLen := 2000
	if sepFlag {
		maxLen-- // reserve one char for the separator
	}
	messageLen := len(runes)
	numMessages := (messageLen + maxLen - 1) / maxLen
	if numMessages == 0 && len(files) > 0 {
		numMessages = 1
	} else if numMessages == 0 {
		return nil, nil
	}

	var firstSentMessage *discord.Message
	var lastErr error

	for i := 0; i < numMessages; i++ {
		start := i * maxLen
		end := min(start+maxLen, messageLen)
		var segment, rawSegment string
		if start < end {
			rawSegment = string(runes[start:end])
			segment = rawSegment
		}

		// add separator to the last message if needed
		if !latex && sepFlag && i == numMessages-1 {
			segment += "\u200B"
		}

		// attach files only to the last message
		currentFiles := []*discord.File{}
		if i == numMessages-1 {
			currentFiles = files
		}

		var message *discord.Message
		var err error

		if i == 0 && *isFirst && event != nil && *event != nil {
			message, err = (*event).UpdateInteractionResponse(discord.MessageUpdate{
				Content: &segment,
				Flags:   &flags,
				Files:   currentFiles,
			})
		} else {
			builder := discord.NewMessageCreate().
				WithContent(segment).
				WithFlags(flags).
				WithAllowedMentions(&discord.AllowedMentions{RepliedUser: false}).
				AddFiles(currentFiles...)
			if i == 0 && *isFirst && *messageID != 0 {
				builder = builder.WithMessageReferenceByID(*messageID)
			}

			if len(currentFiles) > 0 || strings.TrimSpace(rawSegment) != "" {
				message, err = client.Rest.CreateMessage(channelID, builder)
			}
		}

		if err != nil {
			slog.Warn("failed to send message split", "i", i+1, "err", err)
			lastErr = fmt.Errorf("failed to send message split %d: %w", i+1, err)
			continue
		}

		if i == 0 && *isFirst {
			firstSentMessage = message
		}
	}

	if numMessages > 0 {
		*isFirst = false
		if event != nil {
			*event = nil
		}
		*messageID = 0
	}

	if firstSentMessage == nil && lastErr != nil {
		return nil, lastErr
	}

	return firstSentMessage, nil
}

func extractFilenameFromURL(rawURL string) (string, error) {
	// parse to exclude query params
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL '%s': %w", rawURL, err)
	}

	urlPath := parsedURL.Path
	if urlPath == "" || urlPath == "/" {
		return "", fmt.Errorf("URL '%s' has no path component to extract a filename from", rawURL)
	}

	filename := filepath.Base(urlPath)
	if filename == "." || filename == "/" {
		return "", fmt.Errorf("could not extract a valid filename from path '%s'", urlPath)
	}

	return filename, nil
}

// processImageData fetches or decodes image data and determines a safe filename.
func processImageData(imgSrc string, base string) ([]byte, string, error) {
	if base == "" {
		base = "image"
	}
	var body []byte
	var ext string
	var err error

	if strings.HasPrefix(imgSrc, "https://") || strings.HasPrefix(imgSrc, "http://") {
		resp, err := http.Get(imgSrc)
		if err != nil {
			return nil, "", fmt.Errorf("failed to fetch image URL %s: %w", imgSrc, err)
		}
		defer resp.Body.Close()

		if resp.StatusCode != http.StatusOK {
			return nil, "", fmt.Errorf("failed to fetch image URL %s: status %d", imgSrc, resp.StatusCode)
		}

		body, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read image body from %s: %w", imgSrc, err)
		}

		// try to get filename from URL
		urlFilename, _ := extractFilenameFromURL(imgSrc)
		if urlFilename != "" {
			ext = filepath.Ext(urlFilename)
		}
	} else {
		// base64 encoded webp
		body, err = base64.StdEncoding.DecodeString(imgSrc)
		if err != nil {
			return nil, "", fmt.Errorf("failed to decode base64 image: %w", err)
		}
	}

	if ext == "" {
		ext = ".webp"
	}

	return body, base + ext, nil
}

func makeSpoilerFlag(isSpoiler bool) discord.FileFlags {
	if isSpoiler {
		return discord.FileFlagSpoiler
	}
	return discord.FileFlagsNone
}

// ptr is a helper function to create a pointer to a value.
func ptr[T any](v T) *T {
	return &v
}

// isImageAttachment checks if a message attachment is an image.
func isImageAttachment(attachment discord.Attachment) bool {
	return attachment.ContentType != nil && strings.HasPrefix(*attachment.ContentType, "image/")
}

func purgeBotMessagesAfter(client *bot.Client, messageID, channelID snowflake.ID, inclusive, inDM bool) error {
	if messageID == 0 {
		return errors.New("message ID cannot be zero")
	}
	messages, err := client.Rest.GetMessages(channelID, 0, 0, messageID, 100)
	if err != nil {
		return err
	}
	ids := make([]snowflake.ID, 0, len(messages)/2)
	for _, msg := range messages {
		if msg.Author.Bot {
			ids = append(ids, msg.ID)
		}
	}
	if inclusive {
		ids = append(ids, messageID)
	}

	slog.Debug("purgeBotMessagesAfter:", "ids", ids)

	if !inDM {
		if len(ids) >= 2 {
			return client.Rest.BulkDeleteMessages(channelID, ids)
		} else if len(ids) == 1 {
			return client.Rest.DeleteMessage(channelID, ids[0])
		}
	} else {
		for _, id := range ids {
			err := client.Rest.DeleteMessage(channelID, id)
			if err != nil {
				return err
			}
		}
	}

	return nil
}
