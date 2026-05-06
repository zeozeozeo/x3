//go:build matrix || goolm

package commands

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/persona"
	"maunium.net/go/mautrix"
	"maunium.net/go/mautrix/crypto/attachment"
	"maunium.net/go/mautrix/crypto/cryptohelper"
	"maunium.net/go/mautrix/event"
	"maunium.net/go/mautrix/id"
)

const (
	matrixCachePrefix = "matrix:"
	matrixMaxTextLen  = 4000
)

type MatrixBot struct {
	client   *mautrix.Client
	crypto   *cryptohelper.CryptoHelper
	prefix   string
	autoJoin bool

	inProgress sync.Map // id.RoomID -> context.CancelFunc
}

type MatrixRuntime struct {
	Cancel context.CancelFunc
	Done   <-chan struct{}
	Close  func() error
}

type matrixAttachment struct {
	Filename    string
	ContentType string
	Size        int
	Data        []byte
	DataURI     string
	IsImage     bool
}

type matrixMessage struct {
	RoomID      id.RoomID
	EventID     id.EventID
	Sender      id.UserID
	Author      string
	Content     string
	Attachments []matrixAttachment
	Timestamp   time.Time
	ReplyTo     *matrixMessage
	IsBot       bool
}

type matrixCommandContext struct {
	Display string
	Offset  int
}

func (c matrixCommandContext) Raw(local string) string {
	if c.Display != "" {
		return c.Display
	}
	return local
}

func (c matrixCommandContext) Token(token matrixCommandArg) matrixCommandArg {
	token.Start += c.Offset
	token.End += c.Offset
	return token
}

func (b *MatrixBot) matrixCommandContext(command, rest string, isDM bool) matrixCommandContext {
	prefix := b.commandUsage(command, isDM)
	display := prefix
	if strings.TrimSpace(rest) != "" {
		display += " " + rest
	}
	return matrixCommandContext{
		Display: display,
		Offset:  len(prefix) + 1,
	}
}

type matrixOutFile struct {
	Name        string
	ContentType string
	Data        []byte
	IsImage     bool
}

type matrixCommandArg struct {
	Text  string
	Start int
	End   int
}

type matrixCommandArgs struct {
	Raw  string
	Args []matrixCommandArg
}

type matrixDiagnosticSpan struct {
	Token   matrixCommandArg
	Issue   string
	Primary bool
}

func parseMatrixCommandArgs(raw string) matrixCommandArgs {
	var args []matrixCommandArg
	for i := 0; i < len(raw); {
		for i < len(raw) && (raw[i] == ' ' || raw[i] == '\t' || raw[i] == '\n' || raw[i] == '\r') {
			i++
		}
		if i >= len(raw) {
			break
		}
		start := i
		var sb strings.Builder
		quote := byte(0)
		for i < len(raw) {
			ch := raw[i]
			if quote == 0 && (ch == ' ' || ch == '\t' || ch == '\n' || ch == '\r') {
				break
			}
			if ch == '"' || ch == '\'' {
				if quote == 0 {
					quote = ch
					i++
					continue
				}
				if quote == ch {
					quote = 0
					i++
					continue
				}
			}
			if ch == '\\' && i+1 < len(raw) {
				i++
				sb.WriteByte(raw[i])
				i++
				continue
			}
			sb.WriteByte(ch)
			i++
		}
		args = append(args, matrixCommandArg{Text: sb.String(), Start: start, End: i})
	}
	return matrixCommandArgs{Raw: raw, Args: args}
}

func (a matrixCommandArgs) Empty() bool {
	return len(a.Args) == 0
}

func (a matrixCommandArgs) At(i int) matrixCommandArg {
	if i < 0 || i >= len(a.Args) {
		return matrixCommandArg{}
	}
	return a.Args[i]
}

func (a matrixCommandArgs) RestAfter(i int) string {
	if i < 0 || i >= len(a.Args) {
		return ""
	}
	return strings.TrimSpace(a.Raw[a.Args[i].End:])
}

func matrixCommandDiagnostic(raw string, token matrixCommandArg, issue, hint string) string {
	return matrixCommandDiagnosticMulti(raw, []matrixDiagnosticSpan{{Token: token, Issue: issue, Primary: true}}, hint)
}

func matrixCommandDiagnosticMulti(raw string, spans []matrixDiagnosticSpan, hint string) string {
	if raw == "" {
		if len(spans) > 0 {
			raw = spans[0].Token.Text
			spans[0].Token.Start = 0
			spans[0].Token.End = len(raw)
		}
	}
	if len(spans) == 0 {
		spans = []matrixDiagnosticSpan{{Token: matrixCommandEndToken(raw), Issue: "Invalid command.", Primary: true}}
	}
	var b strings.Builder
	b.WriteString("\n```text\n")
	for _, span := range spans {
		if span.Primary {
			b.WriteString("error: ")
		} else {
			b.WriteString("note: ")
		}
		b.WriteString(span.Issue)
		b.WriteString("\n")
	}
	if hint != "" {
		b.WriteString("help: ")
		b.WriteString(hint)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	b.WriteString(raw)
	b.WriteString("\n")
	for _, span := range spans {
		token := span.Token
		start := max(token.Start, 0)
		end := token.End
		if end <= start {
			end = start + max(len(token.Text), 1)
		}
		if start > len(raw) {
			start = len(raw)
		}
		if end > len(raw) {
			end = len(raw)
		}
		width := max(end-start, 1)
		marker := "^"
		if !span.Primary {
			marker = "-"
		}
		b.WriteString(strings.Repeat(" ", start))
		b.WriteString(strings.Repeat(marker, width))
		if span.Issue != "" {
			b.WriteString(" ")
			b.WriteString(span.Issue)
		}
		b.WriteString("\n")
	}
	b.WriteString("```")
	return b.String()
}

func matrixCommandEndToken(raw string) matrixCommandArg {
	pos := len(raw)
	return matrixCommandArg{Text: "<end>", Start: pos, End: pos + 1}
}

func matrixCommandAction(args matrixCommandArgs, commandName string) (matrixCommandArg, string, *string) {
	if args.Empty() {
		msg := matrixCommandDiagnostic(args.Raw, matrixCommandEndToken(args.Raw), "Missing action for `"+commandName+"` command.", "Run `"+commandName+" help` for available actions.")
		return matrixCommandArg{}, "", &msg
	}
	token := args.At(0)
	if strings.Contains(token.Text, "=") {
		msg := matrixCommandDiagnostic(args.Raw, token, "Invalid `"+commandName+"` command syntax.", "Use spaces between tokens. Matrix commands do not use key=value syntax.")
		return matrixCommandArg{}, "", &msg
	}
	action := strings.ToLower(strings.TrimSpace(token.Text))
	return token, action, nil
}

func matrixMissingValueDiagnostic(raw string, actionToken matrixCommandArg, commandName, action, expected string) string {
	return matrixCommandDiagnostic(raw, actionToken, "Missing value for `"+commandName+" "+action+"`.", "Expected: `"+expected+"`")
}

func matrixUnexpectedTokenDiagnostic(raw string, token matrixCommandArg, commandName string) string {
	return matrixCommandDiagnostic(raw, token, "Unexpected token for `"+commandName+"` command.", "This command does not accept `"+token.Text+"` here. Run `"+commandName+" help` for valid syntax.")
}

func matrixInvalidIntDiagnostic(raw string, token matrixCommandArg, name string) string {
	return matrixCommandDiagnostic(raw, token, "Invalid integer for `"+name+"`.", "`"+token.Text+"` is not a base-10 integer.")
}

func matrixInvalidFloatDiagnostic(raw string, token matrixCommandArg, name string) string {
	return matrixCommandDiagnostic(raw, token, "Invalid number for `"+name+"`.", "`"+token.Text+"` must be a decimal number.")
}

func parseMatrixBool(value string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enable", "enabled":
		return true, true
	case "0", "false", "no", "n", "off", "disable", "disabled":
		return false, true
	default:
		return false, false
	}
}

func matrixInvalidBoolDiagnostic(raw string, token matrixCommandArg, name string) string {
	return matrixCommandDiagnostic(raw, token, "Invalid boolean for `"+name+"`.", "Expected one of: `on`, `off`, `true`, `false`, `enable`, `disable`.")
}

func StartMatrixBot(parent context.Context) (*MatrixRuntime, error) {
	if !truthy(os.Getenv("X3_MATRIX_ENABLED")) {
		return nil, nil
	}

	homeserver := strings.TrimSpace(os.Getenv("X3_MATRIX_HOMESERVER"))
	userID := strings.TrimSpace(os.Getenv("X3_MATRIX_USER_ID"))
	username := strings.TrimSpace(os.Getenv("X3_MATRIX_USERNAME"))
	password := os.Getenv("X3_MATRIX_PASSWORD")
	accessToken := strings.TrimSpace(os.Getenv("X3_MATRIX_ACCESS_TOKEN"))
	deviceID := strings.TrimSpace(os.Getenv("X3_MATRIX_DEVICE_ID"))
	deviceName := strings.TrimSpace(os.Getenv("X3_MATRIX_DEVICE_NAME"))
	pickleKey := os.Getenv("X3_MATRIX_PICKLE_KEY")
	recoveryKey := strings.TrimSpace(os.Getenv("X3_MATRIX_RECOVERY_KEY"))
	cryptoDB := strings.TrimSpace(os.Getenv("X3_MATRIX_CRYPTO_DB"))
	if cryptoDB == "" {
		cryptoDB = "x3-matrix-crypto.db"
	}
	if deviceName == "" {
		deviceName = "x3 bot"
	}
	if homeserver == "" || pickleKey == "" {
		return nil, fmt.Errorf("matrix bot enabled, but X3_MATRIX_HOMESERVER and X3_MATRIX_PICKLE_KEY are required")
	}
	if accessToken == "" && password == "" {
		return nil, fmt.Errorf("matrix bot enabled, but either X3_MATRIX_ACCESS_TOKEN or X3_MATRIX_PASSWORD is required")
	}
	if accessToken != "" && userID == "" {
		return nil, fmt.Errorf("X3_MATRIX_USER_ID is required when using X3_MATRIX_ACCESS_TOKEN")
	}
	loginUser := username
	if loginUser == "" {
		loginUser = userID
	}

	client, err := mautrix.NewClient(homeserver, id.UserID(userID), accessToken)
	if err != nil {
		return nil, err
	}
	if accessToken != "" && deviceID == "" {
		whoami, err := client.Whoami(parent)
		if err != nil {
			return nil, fmt.Errorf("failed to discover Matrix device ID from access token; set X3_MATRIX_DEVICE_ID manually: %w", err)
		}
		if whoami.UserID != "" && whoami.UserID != client.UserID {
			return nil, fmt.Errorf("Matrix access token belongs to %s, but X3_MATRIX_USER_ID is %s", whoami.UserID, client.UserID)
		}
		deviceID = whoami.DeviceID.String()
	}
	client.DeviceID = id.DeviceID(deviceID)

	helper, err := cryptohelper.NewCryptoHelper(client, []byte(pickleKey), cryptoDB)
	if err != nil {
		return nil, err
	}
	if accessToken == "" {
		if loginUser == "" {
			_ = helper.Close()
			return nil, fmt.Errorf("X3_MATRIX_USERNAME or X3_MATRIX_USER_ID is required when using X3_MATRIX_PASSWORD")
		}
		req := &mautrix.ReqLogin{
			Type:                     mautrix.AuthTypePassword,
			Identifier:               mautrix.UserIdentifier{Type: mautrix.IdentifierTypeUser, User: loginUser},
			Password:                 password,
			InitialDeviceDisplayName: deviceName,
		}
		if deviceID != "" {
			req.DeviceID = id.DeviceID(deviceID)
		}
		helper.LoginAs = req
	}
	if err := helper.Init(parent); err != nil {
		_ = helper.Close()
		return nil, fmt.Errorf("%w (Matrix crypto stores are device-specific; if you changed X3_MATRIX_DEVICE_ID or switched access tokens, either restore the previous device ID or use/delete X3_MATRIX_CRYPTO_DB=%s)", err, cryptoDB)
	}
	if recoveryKey != "" {
		if err := helper.Machine().VerifyWithRecoveryKey(parent, recoveryKey); err != nil {
			_ = helper.Close()
			return nil, fmt.Errorf("failed to verify Matrix bot device with X3_MATRIX_RECOVERY_KEY: %w", err)
		}
		slog.Info("matrix bot device verified with recovery key", "user_id", client.UserID.String(), "device_id", client.DeviceID.String())
	}
	if userID != "" && client.UserID != id.UserID(userID) {
		_ = helper.Close()
		return nil, fmt.Errorf("Matrix login returned user %s, but X3_MATRIX_USER_ID is %s", client.UserID, userID)
	}
	client.Crypto = helper

	prefix := strings.TrimSpace(os.Getenv("X3_MATRIX_COMMAND_PREFIX"))
	if prefix == "" {
		prefix = "!x3"
	}
	bot := &MatrixBot{
		client:   client,
		crypto:   helper,
		prefix:   prefix,
		autoJoin: os.Getenv("X3_MATRIX_AUTO_JOIN") != "false",
	}
	bot.registerHandlers()

	ctx, cancel := context.WithCancel(parent)
	done := make(chan struct{})
	go func() {
		defer close(done)
		if err := client.SyncWithContext(ctx); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("matrix sync stopped", "err", err)
		}
	}()

	slog.Info("matrix bot started", "user_id", userID, "homeserver", homeserver, "prefix", prefix, "crypto_db", cryptoDB)
	return &MatrixRuntime{
		Cancel: cancel,
		Done:   done,
		Close:  helper.Close,
	}, nil
}

func (b *MatrixBot) registerHandlers() {
	syncer := b.client.Syncer.(*mautrix.DefaultSyncer)
	syncer.OnEventType(event.StateMember, func(ctx context.Context, evt *event.Event) {
		if !b.autoJoin || evt.GetStateKey() != b.client.UserID.String() || evt.Content.AsMember().Membership != event.MembershipInvite {
			return
		}
		if _, err := b.client.JoinRoomByID(ctx, evt.RoomID); err != nil {
			slog.Error("matrix auto-join failed", "room_id", evt.RoomID.String(), "sender", evt.Sender.String(), "err", err)
		} else {
			slog.Info("matrix joined room", "room_id", evt.RoomID.String(), "sender", evt.Sender.String())
		}
	})
	syncer.OnEventType(event.EventMessage, func(ctx context.Context, evt *event.Event) {
		go b.onMessage(ctx, evt)
	})
}

func (b *MatrixBot) onMessage(ctx context.Context, evt *event.Event) {
	if evt.Sender == b.client.UserID {
		return
	}
	msg := b.buildMessage(ctx, evt)
	if msg == nil {
		return
	}

	trimmed := strings.TrimSpace(msg.Content)
	isDM := b.isDMRoom(ctx, msg.RoomID)
	if rawCommand, ok := b.parseCommand(trimmed, isDM); ok {
		if err := b.handleCommand(ctx, msg, rawCommand, isDM); err != nil {
			slog.Error("matrix command failed", "room_id", msg.RoomID.String(), "event_id", msg.EventID.String(), "err", err)
			_ = b.sendText(ctx, msg.RoomID, msg.EventID, "Error: "+err.Error())
		}
		return
	}
	if db.IsChannelKeyInBlacklist(b.roomKey(msg.RoomID)) {
		return
	}

	shouldTrigger := isDM || b.mentioned(trimmed) || b.replyToBot(msg) || containsX3Regex.MatchString(trimmed)
	if !shouldTrigger {
		cache := db.GetChannelCacheByKey(b.roomKey(msg.RoomID))
		shouldTrigger = time.Since(cache.LastInteraction) < 30*time.Second
	}
	if !shouldTrigger {
		return
	}
	if err := b.handleLlm(ctx, msg, false, ""); err != nil {
		slog.Error("matrix LLM interaction failed", "room_id", msg.RoomID.String(), "event_id", msg.EventID.String(), "err", err)
		_ = b.sendText(ctx, msg.RoomID, msg.EventID, "LLM request failed: "+err.Error())
	}
}

func (b *MatrixBot) parseCommand(content string, isDM bool) (string, bool) {
	if content == b.prefix {
		return "", true
	}
	if strings.HasPrefix(content, b.prefix+" ") {
		return strings.TrimSpace(strings.TrimPrefix(content, b.prefix)), true
	}
	if isDM && strings.HasPrefix(content, "!") && !strings.HasPrefix(content, b.prefix) {
		raw := strings.TrimSpace(strings.TrimPrefix(content, "!"))
		return raw, raw != ""
	}
	return "", false
}

func (b *MatrixBot) mentioned(content string) bool {
	userID := b.client.UserID.String()
	localpart := strings.TrimPrefix(strings.SplitN(userID, ":", 2)[0], "@")
	contentLower := strings.ToLower(content)
	return strings.Contains(content, userID) ||
		strings.Contains(contentLower, "@"+strings.ToLower(localpart)) ||
		strings.Contains(contentLower, strings.ToLower(localpart)+":")
}

func (b *MatrixBot) replyToBot(msg *matrixMessage) bool {
	return msg != nil && msg.ReplyTo != nil && msg.ReplyTo.Sender == b.client.UserID
}

func (b *MatrixBot) isDMRoom(ctx context.Context, roomID id.RoomID) bool {
	members, err := b.client.JoinedMembers(ctx, roomID)
	if err != nil || members == nil {
		return false
	}
	return len(members.Joined) <= 2
}

func (b *MatrixBot) roomKey(roomID id.RoomID) string {
	return matrixCachePrefix + roomID.String()
}

func (b *MatrixBot) userKey(userID id.UserID) string {
	return matrixCachePrefix + userID.String()
}

func (b *MatrixBot) eventKey(eventID id.EventID) string {
	return matrixCachePrefix + eventID.String()
}

func (b *MatrixBot) buildMessage(ctx context.Context, evt *event.Event) *matrixMessage {
	if evt == nil || evt.Type != event.EventMessage {
		return nil
	}
	content := evt.Content.AsMessage()
	if content == nil || !content.MsgType.IsText() && !content.MsgType.IsMedia() {
		return nil
	}
	if content.RelatesTo != nil && content.RelatesTo.GetReplaceID() != "" {
		return nil
	}

	body := content.Body
	if content.NewContent != nil {
		body = content.NewContent.Body
	}
	msg := &matrixMessage{
		RoomID:    evt.RoomID,
		EventID:   evt.ID,
		Sender:    evt.Sender,
		Author:    b.displayName(ctx, evt.RoomID, evt.Sender),
		Content:   strings.TrimSpace(stripMatrixReplyFallback(body)),
		Timestamp: time.UnixMilli(evt.Timestamp),
		IsBot:     evt.Sender == b.client.UserID,
	}
	if msg.Author == "" {
		msg.Author = evt.Sender.String()
	}

	if replyID := content.RelatesTo.GetReplyTo(); replyID != "" {
		if reply, err := b.getMessage(ctx, evt.RoomID, replyID); err == nil {
			msg.ReplyTo = reply
		}
	}

	if content.MsgType.IsMedia() {
		if att, err := b.attachmentFromContent(ctx, content); err != nil {
			slog.Warn("failed to read matrix attachment", "room_id", evt.RoomID.String(), "event_id", evt.ID.String(), "err", err)
		} else if att != nil {
			msg.Attachments = append(msg.Attachments, *att)
		}
	}
	return msg
}

func (b *MatrixBot) getMessage(ctx context.Context, roomID id.RoomID, eventID id.EventID) (*matrixMessage, error) {
	evt, err := b.client.GetEvent(ctx, roomID, eventID)
	if err != nil {
		return nil, err
	}
	if evt.Type == event.EventEncrypted && b.client.Crypto != nil {
		evt, err = b.client.Crypto.Decrypt(ctx, evt)
		if err != nil {
			return nil, err
		}
	}
	msg := b.buildMessage(ctx, evt)
	if msg == nil {
		return nil, fmt.Errorf("event is not a message")
	}
	return msg, nil
}

func (b *MatrixBot) displayName(ctx context.Context, roomID id.RoomID, userID id.UserID) string {
	member, err := b.client.StateStore.GetMember(ctx, roomID, userID)
	if err == nil && member != nil && strings.TrimSpace(member.Displayname) != "" {
		return strings.TrimSpace(member.Displayname)
	}
	return strings.TrimPrefix(strings.SplitN(userID.String(), ":", 2)[0], "@")
}

func (b *MatrixBot) attachmentFromContent(ctx context.Context, content *event.MessageEventContent) (*matrixAttachment, error) {
	filename := firstNonEmpty(content.FileName, content.Body, "attachment")
	contentType := ""
	if content.Info != nil {
		contentType = content.Info.MimeType
	}
	uri := content.URL
	file := content.File
	if file != nil {
		uri = file.URL
	}
	if uri == "" {
		return nil, nil
	}
	parsed, err := uri.Parse()
	if err != nil {
		return nil, err
	}
	data, err := b.client.DownloadBytes(ctx, parsed)
	if err != nil {
		return nil, err
	}
	if file != nil {
		err = file.DecryptInPlace(data)
		if err != nil {
			return nil, err
		}
	}
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	isImage := strings.HasPrefix(contentType, "image/")
	att := &matrixAttachment{
		Filename:    filename,
		ContentType: contentType,
		Size:        len(data),
		Data:        data,
		IsImage:     isImage,
	}
	if isImage {
		att.DataURI = fmt.Sprintf("data:%s;base64,%s", contentType, base64.StdEncoding.EncodeToString(data))
	}
	return att, nil
}

func (b *MatrixBot) handleCommand(ctx context.Context, msg *matrixMessage, raw string, isDM bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "help" {
		return b.sendText(ctx, msg.RoomID, msg.EventID, b.helpText(isDM))
	}
	parsed := parseMatrixCommandArgs(raw)
	nameToken := parsed.At(0)
	name := strings.ToLower(strings.TrimSpace(nameToken.Text))
	rest := parsed.RestAfter(0)
	if strings.Contains(nameToken.Text, "=") {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(raw, nameToken, "Invalid top-level command syntax.", "Command names cannot contain `=`. Run `"+b.commandUsage("help", isDM)+"` for valid commands."))
	}

	switch name {
	case "chat":
		if rest == "" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(raw, nameToken, "Missing prompt for `chat` command.", "Expected: `"+b.commandUsage("chat", isDM)+" <prompt>`"))
		}
		copyMsg := *msg
		copyMsg.Content = rest
		return b.handleLlm(ctx, &copyMsg, false, "")
	case "persona":
		return b.handlePersonaCommand(ctx, msg, rest, b.matrixCommandContext("persona", rest, isDM))
	case "context":
		return b.handleContextCommand(ctx, msg, rest, isDM)
	case "lobotomy":
		return b.handleLobotomyCommand(ctx, msg, rest)
	case "regenerate":
		if rest == "help" || rest == "-h" || rest == "--help" || rest == "?" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, "Regenerate command:\n"+b.commandUsage("regenerate", isDM)+" [prepend]\n\nRegenerates the last assistant response. Optional text is used as assistant prefill.")
		}
		return b.handleLlm(ctx, msg, true, rest)
	case "stats":
		return b.handleStatsCommand(ctx, msg)
	case "chatlog":
		return b.handleChatlogCommand(ctx, msg, rest, isDM)
	case "blacklist":
		return b.handleBlacklistCommand(ctx, msg, rest, false)
	case "imageblacklist":
		return b.handleBlacklistCommand(ctx, msg, rest, true)
	default:
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(raw, nameToken, "Unknown Matrix command `"+nameToken.Text+"`.", "Run `"+b.commandUsage("help", isDM)+"` to see available commands."))
	}
}

func (b *MatrixBot) commandUsage(name string, isDM bool) string {
	if isDM {
		return "!" + name
	}
	return b.prefix + " " + name
}

func (b *MatrixBot) helpText(isDM bool) string {
	return strings.Join([]string{
		"x3 commands:",
		b.commandUsage("chat", isDM) + " <prompt>",
		b.commandUsage("persona", isDM),
		b.commandUsage("persona", isDM) + " set <name>",
		b.commandUsage("persona", isDM) + " model <model name>",
		b.commandUsage("persona", isDM) + " system <prompt>",
		b.commandUsage("persona", isDM) + " card <url> | preset <url>",
		b.commandUsage("persona", isDM) + " context|temperature|top_p|frequency_penalty|seed <value>",
		b.commandUsage("persona", isDM) + " images|thinking|reasoning|html on|off",
		b.commandUsage("context", isDM) + " add|list|clear|delete|get|edit ...",
		b.commandUsage("lobotomy", isDM) + " [amount] [reset_persona]",
		b.commandUsage("regenerate", isDM) + " [prepend]",
		b.commandUsage("chatlog", isDM) + " export|import",
	}, "\n")
}

func (b *MatrixBot) personaHelpText(isDM bool) string {
	base := b.commandUsage("persona", isDM)
	return strings.Join([]string{
		"Persona command:",
		base + "                         show current persona settings",
		base + " help                    show this help",
		base + " list                    list built-in personas",
		base + " set <name>              set persona",
		base + " model <model>           set model by name or command",
		base + " system <prompt>         set custom system prompt",
		base + " card [url]              import character card from URL or attachment",
		base + " preset [url]            import SillyTavern preset from URL or attachment",
		base + " context <n>             set context message count",
		base + " temperature <value>     set temperature",
		base + " top_p <value>           set top_p",
		base + " frequency_penalty <v>   set frequency penalty",
		base + " seed <n>                set seed, 0 resets",
		base + " images on|off           toggle image generation",
		base + " thinking on|off         toggle reasoning.txt attachments",
		base + " reasoning on|off        toggle model-side reasoning",
		base + " html on|off             toggle HTML rendering",
	}, "\n")
}

func (b *MatrixBot) contextHelpText(isDM bool) string {
	base := b.commandUsage("context", isDM)
	return strings.Join([]string{
		"Context command:",
		base + " add <text>       add persistent context",
		base + " list             list context entries",
		base + " clear            remove all context entries",
		base + " get <n>          show one context entry",
		base + " delete <n>       delete one context entry",
		base + " edit <n> <text>  replace one context entry",
	}, "\n")
}

func (b *MatrixBot) lobotomyHelpText(isDM bool) string {
	base := b.commandUsage("lobotomy", isDM)
	return strings.Join([]string{
		"Lobotomy command:",
		base + "                  forget all cached messages",
		base + " <amount>         forget the last N cached messages",
		base + " reset            also reset persona",
		base + " <amount> reset   combine both options",
		"",
		"Before clearing context, x3 attaches a chatlog archive when one is available.",
	}, "\n")
}

func (b *MatrixBot) chatlogHelpText(isDM bool) string {
	base := b.commandUsage("chatlog", isDM)
	return strings.Join([]string{
		"Chatlog command:",
		base + " export           export cached context as JSON",
		base + " import           import an attached x3 chatlog JSON file",
	}, "\n")
}

func (b *MatrixBot) blacklistHelpText(isDM bool, image bool) string {
	name := "blacklist"
	if image {
		name = "imageblacklist"
	}
	base := b.commandUsage(name, isDM)
	return strings.Join([]string{
		strings.Title(name) + " command:",
		base + " on              enable",
		base + " off             disable",
	}, "\n")
}

func (b *MatrixBot) handlePersonaCommand(ctx context.Context, msg *matrixMessage, rest string, diagCtx matrixCommandContext) error {
	key := b.roomKey(msg.RoomID)
	cache := db.GetChannelCacheByKey(key)
	if strings.TrimSpace(rest) == "" {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixPersonaInfo(cache, msg.Author, b.isDMRoom(ctx, msg.RoomID)))
	}
	parsed := parseMatrixCommandArgs(rest)
	actionToken := parsed.At(0)
	action := strings.ToLower(strings.TrimSpace(actionToken.Text))
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.RoomID, msg.EventID, b.personaHelpText(b.isDMRoom(ctx, msg.RoomID)))
	}
	if strings.Contains(actionToken.Text, "=") {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "invalid persona command syntax", "use a space between the setting and value, not `=`. Example: `"+b.commandUsage("persona", b.isDMRoom(ctx, msg.RoomID))+" model glm5`"))
	}
	if action == "list" {
		var names []string
		for _, p := range persona.AllPersonas {
			names = append(names, p.Name)
		}
		return b.sendText(ctx, msg.RoomID, msg.EventID, "Available personas:\n"+strings.Join(names, ", "))
	}
	valueToken := parsed.At(1)
	value := strings.TrimSpace(valueToken.Text)
	attachmentValueActions := action == "card" || action == "preset"
	if value == "" && !attachmentValueActions {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "missing value for persona action `"+action+"`", "expected: `"+b.commandUsage("persona", b.isDMRoom(ctx, msg.RoomID))+" "+action+" <value>`"))
	}
	if valueToken.Text != "" && strings.Contains(valueToken.Text, "=") {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "invalid value syntax for persona action `"+action+"`", "use spaces instead of key=value syntax"))
	}
	valueRest := parsed.RestAfter(0)
	if action == "system" {
		value = valueRest
	}

	prev := cache.PersonaMeta.DeepCopy()
	switch action {
	case "set":
		meta, err := persona.GetMetaByName(value)
		if err != nil {
			userCache := db.GetUserCacheByKey(b.userKey(msg.Sender))
			for i := range userCache.Personas {
				pName := userCache.Personas[i].PersonaName
				if pName == "" {
					pName = userCache.Personas[i].Name
				}
				if pName == value {
					cache.PersonaMeta = persona.PersonaMeta{
						Name:          pName,
						TavernCard:    v1ToV2(userCache.Personas[i]),
						Models:        persona.PersonaProto.Models,
						Settings:      persona.PersonaProto.Settings,
						NeedSummaries: true,
					}
					return b.writePersonaUpdate(ctx, msg, key, cache, prev)
				}
			}
			return b.sendText(ctx, msg.RoomID, msg.EventID, "Unknown persona: "+value)
		}
		cache.PersonaMeta = meta
		cache.PersonaMeta.TavernCard = nil
	case "model":
		m, ok := findMatrixModel(value)
		if !ok {
			return b.sendText(ctx, msg.RoomID, msg.EventID, "Unknown model: "+value)
		}
		cache.PersonaMeta.Models = []string{m.Name}
	case "system":
		cache.PersonaMeta.System = strings.ReplaceAll(value, "\\n", "\n")
		cache.PersonaMeta.TavernCard = nil
		cache.PersonaMeta.ChatPreset = nil
	case "context":
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidIntDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona context"))
		}
		if n < 0 {
			n = db.DefaultContextMessages
		}
		cache.ContextLength = min(n, 500)
	case "temperature":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidFloatDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona temperature"))
		}
		cache.PersonaMeta.Settings.Temperature = float32(v)
	case "top_p":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidFloatDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona top_p"))
		}
		cache.PersonaMeta.Settings.TopP = float32(v)
	case "frequency_penalty":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidFloatDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona frequency_penalty"))
		}
		cache.PersonaMeta.Settings.FrequencyPenalty = float32(v)
	case "seed":
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidIntDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona seed"))
		}
		if n == 0 {
			cache.PersonaMeta.Settings.Seed = nil
		} else {
			cache.PersonaMeta.Settings.Seed = &n
		}
	case "images":
		enabled, ok := parseMatrixBool(value)
		if !ok {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona images"))
		}
		cache.PersonaMeta.EnableImages = enabled
	case "thinking":
		enabled, ok := parseMatrixBool(value)
		if !ok {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona thinking"))
		}
		cache.PersonaMeta.ThinkingTraces = enabled
	case "reasoning":
		enabled, ok := parseMatrixBool(value)
		if !ok {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona reasoning"))
		}
		cache.PersonaMeta.Settings.Reasoning = enabled
	case "html":
		enabled, ok := parseMatrixBool(value)
		if !ok {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona html"))
		}
		cache.PersonaMeta.RenderHTML = enabled
	case "card":
		var body []byte
		var filename string
		var err error
		value = strings.TrimSpace(valueRest)
		if value != "" {
			body, filename, err = fetchPersonaCardData(value, discord.Attachment{})
		} else if len(msg.Attachments) > 0 {
			body = msg.Attachments[0].Data
			filename = msg.Attachments[0].Filename
		} else {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "missing character card source", "pass a card URL or attach a card image/JSON file with `"+b.commandUsage("persona", b.isDMRoom(ctx, msg.RoomID))+" card`"))
		}
		if err != nil {
			return err
		}
		if _, err := cache.PersonaMeta.ApplyChara(body, msg.Author); err != nil {
			return err
		}
		_ = filename
	case "preset":
		var body []byte
		var err error
		value = strings.TrimSpace(valueRest)
		if value != "" {
			body, _, err = fetchJSONAttachmentData(value, discord.Attachment{}, "preset")
		} else if len(msg.Attachments) > 0 {
			body = msg.Attachments[0].Data
		} else {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "missing preset source", "pass a preset JSON URL or attach a preset JSON file with `"+b.commandUsage("persona", b.isDMRoom(ctx, msg.RoomID))+" preset`"))
		}
		if err != nil {
			return err
		}
		preset, err := persona.ParseSTChatPreset(body)
		if err != nil {
			return err
		}
		cache.PersonaMeta.ChatPreset = preset
		cache.PersonaMeta.Settings = preset.ImportedSettings()
		if preset.AssistantPrefill != "" {
			cache.PersonaMeta.Prepend = strings.ReplaceAll(preset.AssistantPrefill, "\\n", "\n")
		}
	default:
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "unknown persona action `"+actionToken.Text+"`", "run `"+b.commandUsage("persona", b.isDMRoom(ctx, msg.RoomID))+" help` for valid actions"))
	}
	cache.PersonaMeta.Settings = cache.PersonaMeta.Settings.Fixup()
	return b.writePersonaUpdate(ctx, msg, key, cache, prev)
}

func (b *MatrixBot) writePersonaUpdate(ctx context.Context, msg *matrixMessage, key string, cache *db.ChannelCache, prev persona.PersonaMeta) error {
	if err := cache.WriteKey(key); err != nil {
		return err
	}
	changes := []string{}
	if cache.PersonaMeta.Name != prev.Name {
		changes = append(changes, "persona="+cache.PersonaMeta.Name)
	}
	if !stringsEqual(cache.PersonaMeta.Models, prev.Models) && len(cache.PersonaMeta.Models) > 0 {
		changes = append(changes, "model="+strings.Join(cache.PersonaMeta.Models, ", "))
	}
	if cache.PersonaMeta.RenderHTML != prev.RenderHTML {
		changes = append(changes, fmt.Sprintf("html=%t", cache.PersonaMeta.RenderHTML))
	}
	if cache.PersonaMeta.EnableImages != prev.EnableImages {
		changes = append(changes, fmt.Sprintf("images=%t", cache.PersonaMeta.EnableImages))
	}
	if cache.PersonaMeta.Settings.Reasoning != prev.Settings.Reasoning {
		changes = append(changes, fmt.Sprintf("reasoning=%t", cache.PersonaMeta.Settings.Reasoning))
	}
	if len(changes) == 0 {
		changes = append(changes, "settings updated")
	}
	return b.sendText(ctx, msg.RoomID, msg.EventID, "Updated persona: "+strings.Join(changes, ", "))
}

func matrixPersonaInfo(cache *db.ChannelCache, username string, isDM bool) string {
	settings := cache.PersonaMeta.Settings.Fixup()
	remapped := settings
	remapped.Remap()
	promptContext := persona.PromptContext{
		Memories: append([]string(nil), cache.Memories...),
		Context:  append([]string(nil), cache.Context...),
	}
	if summariesEnabled() {
		promptContext.Summaries = append([]persona.Summary(nil), cache.Summaries...)
	}
	system := persona.GetPersonaByMeta(cache.PersonaMeta, username, isDM, promptContext).System
	models := cache.PersonaMeta.Models
	if len(models) == 0 {
		models = persona.PersonaProto.Models
	}
	var b strings.Builder
	fmt.Fprintf(&b, "Persona: %s\n", cache.PersonaMeta.Name)
	fmt.Fprintf(&b, "Model: %s\n", strings.Join(models, ", "))
	fmt.Fprintf(&b, "Temperature: %s (remapped to %s)\n", ftoa(settings.Temperature), ftoa(remapped.Temperature))
	fmt.Fprintf(&b, "Top P: %s (remapped to %s)\n", ftoa(settings.TopP), ftoa(remapped.TopP))
	fmt.Fprintf(&b, "Frequency penalty: %s\n", ftoa(settings.FrequencyPenalty))
	fmt.Fprintf(&b, "Context length: %d\n", cache.ContextLength)
	fmt.Fprintf(&b, "Images: %t\nReasoning: %t\nThinking traces: %t\nHTML rendering: %t\n", cache.PersonaMeta.EnableImages, settings.Reasoning, cache.PersonaMeta.ThinkingTraces, cache.PersonaMeta.RenderHTML)
	if cache.PersonaMeta.ChatPreset != nil {
		fmt.Fprintf(&b, "SillyTavern preset: %s\n", cache.PersonaMeta.ChatPreset.DisplayName())
	}
	if system != "" {
		fmt.Fprintf(&b, "\nSystem prompt:\n%s", ellipsisTrim(system, 1600))
	}
	return strings.TrimSpace(b.String())
}

func (b *MatrixBot) handleContextCommand(ctx context.Context, msg *matrixMessage, rest string, isDM bool) error {
	parsed := parseMatrixCommandArgs(rest)
	actionToken, action, diag := matrixCommandAction(parsed, "context")
	if diag != nil {
		return b.sendText(ctx, msg.RoomID, msg.EventID, *diag+"\n\n"+b.contextHelpText(isDM))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.RoomID, msg.EventID, b.contextHelpText(isDM))
	}
	valueToken := parsed.At(1)
	value := strings.TrimSpace(valueToken.Text)
	valueRest := parsed.RestAfter(0)
	key := b.roomKey(msg.RoomID)
	cache := db.GetChannelCacheByKey(key)
	switch action {
	case "add":
		if strings.TrimSpace(valueRest) == "" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" add <text>"))
		}
		cache.Context = append(cache.Context, valueRest)
	case "clear":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixUnexpectedTokenDiagnostic(rest, valueToken, "context clear"))
		}
		cache.Context = nil
	case "list":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixUnexpectedTokenDiagnostic(rest, valueToken, "context list"))
		}
		if len(cache.Context) == 0 {
			return b.sendText(ctx, msg.RoomID, msg.EventID, "No context set for this room.")
		}
		var sb strings.Builder
		for i, item := range cache.Context {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, item)
		}
		return b.sendText(ctx, msg.RoomID, msg.EventID, strings.TrimSpace(sb.String()))
	case "delete":
		if value == "" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" delete <n>"))
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidIntDiagnostic(rest, valueToken, "context index"))
		}
		if n < 1 || n > len(cache.Context) {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, valueToken, "Context index out of range.", fmt.Sprintf("Expected an index from 1 to %d.", len(cache.Context))))
		}
		cache.Context = append(cache.Context[:n-1], cache.Context[n:]...)
	case "get":
		if value == "" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" get <n>"))
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidIntDiagnostic(rest, valueToken, "context index"))
		}
		if n < 1 || n > len(cache.Context) {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, valueToken, "Context index out of range.", fmt.Sprintf("Expected an index from 1 to %d.", len(cache.Context))))
		}
		return b.sendText(ctx, msg.RoomID, msg.EventID, cache.Context[n-1])
	case "edit":
		if value == "" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" edit <n> <new text>"))
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidIntDiagnostic(rest, valueToken, "context index"))
		}
		if n < 1 || n > len(cache.Context) {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, valueToken, "Context index out of range.", fmt.Sprintf("Expected an index from 1 to %d.", len(cache.Context))))
		}
		newText := parsed.RestAfter(1)
		if strings.TrimSpace(newText) == "" {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, valueToken, "Missing replacement text for `context edit`.", "Expected: `"+b.commandUsage("context", isDM)+" edit <n> <new text>`"))
		}
		cache.Context[n-1] = strings.TrimSpace(newText)
	default:
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, actionToken, "Unknown context action `"+actionToken.Text+"`.", "Valid actions: add, list, clear, delete, get, edit.")+"\n\n"+b.contextHelpText(isDM))
	}
	if err := cache.WriteKey(key); err != nil {
		return err
	}
	return b.sendText(ctx, msg.RoomID, msg.EventID, "Context updated.")
}

func (b *MatrixBot) handleLobotomyCommand(ctx context.Context, msg *matrixMessage, rest string) error {
	parsed := parseMatrixCommandArgs(rest)
	amount := 0
	resetPersona := false
	for i, token := range parsed.Args {
		part := strings.ToLower(token.Text)
		if i == 0 && (part == "help" || part == "-h" || part == "--help" || part == "?") {
			return b.sendText(ctx, msg.RoomID, msg.EventID, b.lobotomyHelpText(b.isDMRoom(ctx, msg.RoomID)))
		}
		if strings.Contains(token.Text, "=") {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, token, "Invalid lobotomy command syntax.", "Use positional arguments, not key=value syntax. Example: `"+b.commandUsage("lobotomy", b.isDMRoom(ctx, msg.RoomID))+" 5 reset`"))
		}
		if part == "reset_persona" || part == "reset" {
			resetPersona = true
			continue
		}
		if i == 0 {
			n, err := strconv.Atoi(token.Text)
			if err != nil {
				return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidIntDiagnostic(rest, token, "lobotomy amount"))
			}
			amount = n
			continue
		}
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixUnexpectedTokenDiagnostic(rest, token, "lobotomy"))
	}
	key := b.roomKey(msg.RoomID)
	cache := db.GetChannelCacheByKey(key)
	archive := buildMatrixChatArchive(msg, cache)
	archiveData, err := marshalChatArchive(archive)
	if err != nil {
		return err
	}
	attachArchive := !chatArchiveIsEmpty(archive)

	if resetPersona {
		cache.PersonaMeta = db.NewChannelCache().PersonaMeta
	}
	if cache.Llmer != nil {
		cache.Llmer.Lobotomize(amount)
	}
	if cache.ImportedHistory != nil {
		if amount > 0 {
			if cache.Llmer == nil {
				cache.Llmer = llm.NewLlmerForKey(key)
				cache.Llmer.Messages = append([]llm.Message(nil), cache.ImportedHistory.Messages...)
			}
			cache.Llmer.Lobotomize(amount)
			cache.ImportedHistory.Messages = nonSystemMessages(cache.Llmer.Messages)
		} else {
			cache.ImportedHistory = nil
			cache.Llmer = nil
		}
	}
	cache.Summaries = nil
	cache.Memories = nil
	if err := cache.WriteKey(key); err != nil {
		return err
	}
	text := "Lobotomized for this room."
	if amount > 0 {
		text = fmt.Sprintf("Removed last %d messages from the context", amount)
	}
	if attachArchive {
		_, err = b.sendFiles(ctx, msg.RoomID, msg.EventID, text, []matrixOutFile{matrixChatArchiveFile(archiveData, archive.ExportedAt)})
		return err
	}
	return b.sendText(ctx, msg.RoomID, msg.EventID, text)
}

func (b *MatrixBot) handleStatsCommand(ctx context.Context, msg *matrixMessage) error {
	stats, err := db.GetGlobalStats()
	if err != nil {
		return err
	}
	cache := db.GetChannelCacheByKey(b.roomKey(msg.RoomID))
	prompt, response, total := formatUsageStrings(cache.Usage)
	promptLast, responseLast, totalLast := formatUsageStrings(cache.LastUsage)
	promptTotal, responseTotal, totalTotal := formatUsageStrings(stats.Usage)
	text := fmt.Sprintf("Stats\nChannel tokens: prompt %s, response %s, total %s\nLast response: prompt %s, response %s, total %s\nGlobal tokens: prompt %s, response %s, total %s\nUptime: %s\nMessages processed: %d\nImages generated: %d",
		prompt, response, total,
		promptLast, responseLast, totalLast,
		promptTotal, responseTotal, totalTotal,
		time.Since(StartTime).Round(time.Second),
		stats.MessageCount,
		stats.ImagesGenerated,
	)
	return b.sendText(ctx, msg.RoomID, msg.EventID, text)
}

func (b *MatrixBot) handleBlacklistCommand(ctx context.Context, msg *matrixMessage, rest string, image bool) error {
	commandName := "blacklist"
	if image {
		commandName = "imageblacklist"
	}
	parsed := parseMatrixCommandArgs(rest)
	actionToken, action, diag := matrixCommandAction(parsed, commandName)
	if diag != nil {
		return b.sendText(ctx, msg.RoomID, msg.EventID, *diag+"\n\n"+b.blacklistHelpText(b.isDMRoom(ctx, msg.RoomID), image))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.RoomID, msg.EventID, b.blacklistHelpText(b.isDMRoom(ctx, msg.RoomID), image))
	}
	enabled, ok := parseMatrixBool(action)
	if !ok {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixInvalidBoolDiagnostic(rest, actionToken, commandName)+"\n\n"+b.blacklistHelpText(b.isDMRoom(ctx, msg.RoomID), image))
	}
	if len(parsed.Args) > 1 {
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixUnexpectedTokenDiagnostic(rest, parsed.At(1), commandName))
	}
	key := b.roomKey(msg.RoomID)
	var err error
	if image {
		if enabled {
			err = db.AddChannelKeyToImageBlacklist(key)
		} else {
			err = db.RemoveChannelKeyFromImageBlacklist(key)
		}
	} else {
		if enabled {
			err = db.AddChannelKeyToBlacklist(key)
		} else {
			err = db.RemoveChannelKeyFromBlacklist(key)
		}
	}
	if err != nil {
		return err
	}
	return b.sendText(ctx, msg.RoomID, msg.EventID, fmt.Sprintf("Blacklist updated: %t", enabled))
}

func (b *MatrixBot) handleChatlogCommand(ctx context.Context, msg *matrixMessage, rest string, isDM bool) error {
	parsed := parseMatrixCommandArgs(rest)
	actionToken, action, diag := matrixCommandAction(parsed, "chatlog")
	if diag != nil {
		return b.sendText(ctx, msg.RoomID, msg.EventID, *diag+"\n\n"+b.chatlogHelpText(isDM))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.RoomID, msg.EventID, b.chatlogHelpText(isDM))
	}
	key := b.roomKey(msg.RoomID)
	cache := db.GetChannelCacheByKey(key)
	switch action {
	case "export":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixUnexpectedTokenDiagnostic(rest, parsed.At(1), "chatlog export"))
		}
		archive := buildMatrixChatArchive(msg, cache)
		data, err := marshalChatArchive(archive)
		if err != nil {
			return err
		}
		_, err = b.sendFiles(ctx, msg.RoomID, msg.EventID, "Exported chatlog.", []matrixOutFile{matrixChatArchiveFile(data, archive.ExportedAt)})
		return err
	case "import":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixUnexpectedTokenDiagnostic(rest, parsed.At(1), "chatlog import"))
		}
		if len(msg.Attachments) == 0 {
			return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, actionToken, "Missing chatlog archive attachment.", "Attach an `x3-chatlog-<year>-<month>-<day>.json` file and run `"+b.commandUsage("chatlog", isDM)+" import`."))
		}
		var archive chatArchive
		if err := json.Unmarshal(msg.Attachments[0].Data, &archive); err != nil {
			return err
		}
		if archive.Version != chatArchiveVersion {
			return fmt.Errorf("unsupported archive version %d", archive.Version)
		}
		importedMessages := matrixArchiveToLLMMessages(archive.Messages)
		cache.Llmer = llm.NewLlmerForKey(key)
		cache.Llmer.Messages = importedMessages
		cache.ImportedHistory = &db.ImportedChatHistory{Messages: append([]llm.Message(nil), importedMessages...)}
		cache.Summaries = append([]persona.Summary(nil), archive.Summaries...)
		cache.Memories = nil
		cache.AddMemories(archive.Memories)
		cache.Context = append([]string(nil), archive.Context...)
		cache.UpdateInteractionTime()
		if err := cache.WriteKey(key); err != nil {
			return err
		}
		return b.sendText(ctx, msg.RoomID, msg.EventID, fmt.Sprintf("Imported %s into cached context.", pluralize(len(importedMessages), "message")))
	default:
		return b.sendText(ctx, msg.RoomID, msg.EventID, matrixCommandDiagnostic(rest, actionToken, "Unknown chatlog action `"+actionToken.Text+"`.", "Valid actions: export, import.")+"\n\n"+b.chatlogHelpText(isDM))
	}
}

func buildMatrixChatArchive(msg *matrixMessage, cache *db.ChannelCache) chatArchive {
	return chatArchive{
		Version:    chatArchiveVersion,
		ExportedAt: time.Now().UTC(),
		ChannelID:  msg.RoomID.String(),
		Messages:   archiveMessagesFromLLM(cacheHistoryMessages(cache)),
		Summaries:  activeSummaries(cache),
		Memories:   append([]string(nil), cache.Memories...),
		Context:    append([]string(nil), cache.Context...),
	}
}

func matrixChatArchiveFile(data []byte, exportedAt time.Time) matrixOutFile {
	return matrixOutFile{
		Name:        chatArchiveFilename(exportedAt),
		ContentType: "application/json",
		Data:        data,
	}
}

func matrixArchiveToLLMMessages(messages []chatArchiveMessage) []llm.Message {
	out := make([]llm.Message, 0, len(messages))
	for _, msg := range messages {
		if strings.TrimSpace(msg.Content) == "" && len(msg.Images) == 0 {
			continue
		}
		out = append(out, llm.Message{
			Role:      msg.Role,
			Content:   msg.Content,
			Images:    append([]string(nil), msg.Images...),
			Author:    msg.Author,
			Timestamp: msg.Timestamp,
			MessageID: msg.MessageID,
		})
	}
	return out
}

func (b *MatrixBot) handleLlm(ctx context.Context, msg *matrixMessage, isRegenerate bool, regeneratePrepend string) error {
	key := b.roomKey(msg.RoomID)
	ctx, cancel := context.WithCancel(ctx)
	if oldCancel, loaded := b.inProgress.Swap(msg.RoomID, cancel); loaded {
		if c, ok := oldCancel.(context.CancelFunc); ok {
			c()
		}
	}
	defer func() {
		b.inProgress.Delete(msg.RoomID)
		cancel()
	}()

	_, _ = b.client.UserTyping(ctx, msg.RoomID, true, 15*time.Second)
	cache := db.GetChannelCacheByKey(key)
	models := cache.PersonaMeta.GetModels()
	if len(models) == 0 {
		return llm.ErrNoModelsForCompletion()
	}

	var llmer *llm.Llmer
	if cache.Llmer != nil {
		llmer = cache.Llmer
		llmer.ConversationID = key
	} else {
		llmer = llm.NewLlmerForKey(key)
		if cache.ImportedHistory != nil {
			llmer.Messages = append([]llm.Message(nil), cache.ImportedHistory.Messages...)
		}
	}

	if isRegenerate {
		lastID := lastAssistantMatrixMessageID(llmer)
		if lastID == "" {
			return errRegenerateNoMessage
		}
		llmer.LobotomizeUntilMessageID(lastID)
	} else {
		promptContext := matrixPromptContext(cache)
		p := persona.GetPersonaByMeta(cache.PersonaMeta, msg.Author, b.isDMRoom(ctx, msg.RoomID), promptContext)
		llmer.SetPersona(p, &cache.PersonaMeta.ExcessiveSplit)
		content := matrixFormatMsg(msg.Content, msg.Author, msg.ReplyTo)
		llmer.AddMessageWithID(llm.RoleUser, content, 0, msg.EventID.String())
		if len(llmer.Messages) > 0 {
			added := &llmer.Messages[len(llmer.Messages)-1]
			added.Author = msg.Author
			added.Timestamp = msg.Timestamp
			added.MessageID = msg.EventID.String()
		}
		for _, attachment := range msg.Attachments {
			if attachment.IsImage && attachment.DataURI != "" {
				llmer.AddImage(attachment.DataURI)
			}
		}
	}

	prepend := cache.PersonaMeta.Prepend
	if regeneratePrepend != "" {
		prepend = regeneratePrepend
		if !endsWithWhitespace(prepend) {
			prepend += " "
		}
	}

	slog.Info("requesting Matrix LLM completion",
		"room_id", msg.RoomID.String(),
		"num_models", len(models),
		"num_messages", llmer.NumMessages(),
		"is_regenerate", isRegenerate,
	)
	response, usage, err := llmer.RequestCompletion(models, cache.PersonaMeta.Settings, prepend, ctx)
	if err != nil {
		return err
	}
	cache.Usage = cache.Usage.Add(usage)
	cache.LastUsage = usage
	_ = db.UpdateGlobalStats(usage)

	var thinking string
	thinking, response = extractThinkingForDisplay(response)
	if displayResponse, memories := extractMemoryTags(response); displayResponse != response || len(memories) > 0 {
		response = displayResponse
		setLatestAssistantMessageContent(llmer, response)
		cache.AddMemories(memories)
	}

	files := []*discord.File{}
	if thinking != "" && cache.PersonaMeta.ThinkingTraces {
		files = append(files, &discord.File{Name: "reasoning.txt", Reader: strings.NewReader(thinking)})
	}
	rawResponse := response
	response, files, htmlRendered := prepareHTMLRenderedResponse(ctx, cache.PersonaMeta, response, files)
	response = strings.TrimSpace(response)

	outFiles, err := matrixFilesFromDiscordFiles(files)
	if err != nil {
		return err
	}
	var sentID id.EventID
	if isRegenerate {
		lastID := id.EventID(lastAssistantMatrixMessageID(llmer))
		sentID, err = b.editText(ctx, msg.RoomID, lastID, replaceLlmTagsWithNewlines(response, &cache.PersonaMeta), outFiles)
	} else {
		sentID, err = b.sendLLMResponse(ctx, msg.RoomID, msg.EventID, response, outFiles, &cache.PersonaMeta)
	}
	if err != nil {
		return err
	}
	if sentID != "" {
		setLatestMatrixAssistantMessageMetadata(llmer, b.client.UserID.String(), sentID.String(), time.Now())
		if htmlRendered {
			_ = db.WriteMessageRenderedContentKey(b.eventKey(sentID), rawResponse)
		}
	}

	if summariesEnabled() {
		for i := range cache.Summaries {
			cache.Summaries[i].Age++
		}
	}
	cache.Llmer = llmer
	if cache.ImportedHistory != nil {
		cache.ImportedHistory.Messages = append([]llm.Message(nil), llmer.Messages...)
	}
	cache.UpdateInteractionTime()
	return cache.WriteKey(key)
}

func matrixPromptContext(cache *db.ChannelCache) persona.PromptContext {
	ctx := persona.PromptContext{
		Memories: append([]string(nil), cache.Memories...),
		Context:  append([]string(nil), cache.Context...),
	}
	if summariesEnabled() {
		ctx.Summaries = append([]persona.Summary(nil), cache.Summaries...)
	}
	return ctx
}

func extractThinkingForDisplay(response string) (thinking, display string) {
	thinking, answer := llm.ExtractThinking(response)
	if thinking != "" && answer != "" {
		return thinking, answer
	}
	return thinking, response
}

func lastAssistantMatrixMessageID(llmer *llm.Llmer) string {
	if llmer == nil {
		return ""
	}
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleAssistant && llmer.Messages[i].MessageID != "" && llmer.Messages[i].MessageID != "0" {
			return llmer.Messages[i].MessageID
		}
	}
	return ""
}

func setLatestMatrixAssistantMessageMetadata(llmer *llm.Llmer, author, eventID string, timestamp time.Time) {
	if llmer == nil {
		return
	}
	for i := len(llmer.Messages) - 1; i >= 0; i-- {
		if llmer.Messages[i].Role == llm.RoleAssistant {
			llmer.Messages[i].Author = author
			llmer.Messages[i].Timestamp = timestamp
			llmer.Messages[i].MessageID = eventID
			return
		}
	}
}

func (b *MatrixBot) sendLLMResponse(ctx context.Context, roomID id.RoomID, replyTo id.EventID, response string, files []matrixOutFile, meta *persona.PersonaMeta) (id.EventID, error) {
	messages := splitLlmTags(response, meta)
	if len(messages) == 0 && len(files) > 0 {
		messages = []string{""}
	}
	var firstID id.EventID
	for i, content := range messages {
		content = strings.TrimSpace(content)
		currentFiles := []matrixOutFile(nil)
		if i == len(messages)-1 {
			currentFiles = files
		}
		sentID, err := b.sendFiles(ctx, roomID, replyTo, content, currentFiles)
		if err != nil {
			return firstID, err
		}
		if firstID == "" {
			firstID = sentID
		}
		replyTo = ""
	}
	return firstID, nil
}

func (b *MatrixBot) sendText(ctx context.Context, roomID id.RoomID, replyTo id.EventID, text string) error {
	_, err := b.sendFiles(ctx, roomID, replyTo, text, nil)
	return err
}

func (b *MatrixBot) sendFiles(ctx context.Context, roomID id.RoomID, replyTo id.EventID, text string, files []matrixOutFile) (id.EventID, error) {
	var firstID id.EventID
	if strings.TrimSpace(text) != "" || len(files) == 0 {
		for _, chunk := range splitMatrixText(text) {
			content := matrixTextContent(chunk)
			if replyTo != "" {
				content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(replyTo)
			}
			resp, err := b.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
			if err != nil {
				return firstID, err
			}
			if firstID == "" {
				firstID = resp.EventID
			}
			replyTo = ""
		}
	}
	for _, file := range files {
		eventID, err := b.sendFile(ctx, roomID, replyTo, file)
		if err != nil {
			return firstID, err
		}
		if firstID == "" {
			firstID = eventID
		}
		replyTo = ""
	}
	return firstID, nil
}

func (b *MatrixBot) editText(ctx context.Context, roomID id.RoomID, target id.EventID, text string, files []matrixOutFile) (id.EventID, error) {
	if target == "" {
		return "", errRegenerateNoMessage
	}
	text = firstNonEmpty(strings.TrimSpace(text), "<empty response>")
	content := matrixTextContent(ellipsisTrim(text, matrixMaxTextLen))
	content.SetEdit(target)
	resp, err := b.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		return "", err
	}
	if len(files) > 0 {
		_, err = b.sendFiles(ctx, roomID, target, "", files)
	}
	return resp.EventID, err
}

func (b *MatrixBot) sendFile(ctx context.Context, roomID id.RoomID, replyTo id.EventID, file matrixOutFile) (id.EventID, error) {
	data := append([]byte(nil), file.Data...)
	contentType := file.ContentType
	if contentType == "" {
		contentType = http.DetectContentType(data)
	}
	url, encryptedFile, err := b.uploadMatrixMedia(ctx, roomID, data, file.Name, contentType)
	if err != nil {
		return "", err
	}
	msgType := event.MsgFile
	if file.IsImage || strings.HasPrefix(contentType, "image/") {
		msgType = event.MsgImage
	}
	content := &event.MessageEventContent{
		MsgType:  msgType,
		Body:     file.Name,
		FileName: file.Name,
		Info: &event.FileInfo{
			MimeType: contentType,
			Size:     len(file.Data),
		},
		URL:  url,
		File: encryptedFile,
	}
	if replyTo != "" {
		content.RelatesTo = (&event.RelatesTo{}).SetReplyTo(replyTo)
	}
	resp, err := b.client.SendMessageEvent(ctx, roomID, event.EventMessage, content)
	if err != nil {
		return "", err
	}
	return resp.EventID, nil
}

func (b *MatrixBot) uploadMatrixMedia(ctx context.Context, roomID id.RoomID, data []byte, filename, contentType string) (id.ContentURIString, *event.EncryptedFileInfo, error) {
	var file *event.EncryptedFileInfo
	encrypted, err := b.client.StateStore.IsEncrypted(ctx, roomID)
	if err != nil {
		return "", nil, err
	}
	if encrypted {
		file = &event.EncryptedFileInfo{EncryptedFile: *attachment.NewEncryptedFile()}
		file.EncryptInPlace(data)
		contentType = "application/octet-stream"
		filename = ""
	}
	resp, err := b.client.UploadMedia(ctx, mautrix.ReqUploadMedia{
		ContentBytes: data,
		ContentType:  contentType,
		FileName:     filename,
	})
	if err != nil {
		return "", nil, err
	}
	url := resp.ContentURI.CUString()
	if file != nil {
		file.URL = url
		return "", file, nil
	}
	return url, nil, nil
}

func splitMatrixText(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return []string{""}
	}
	runes := []rune(text)
	var out []string
	for len(runes) > matrixMaxTextLen {
		cut := matrixMaxTextLen
		for cut > matrixMaxTextLen/2 && runes[cut-1] != '\n' && runes[cut-1] != ' ' {
			cut--
		}
		if cut <= matrixMaxTextLen/2 {
			cut = matrixMaxTextLen
		}
		out = append(out, strings.TrimSpace(string(runes[:cut])))
		runes = runes[cut:]
	}
	if len(runes) > 0 {
		out = append(out, strings.TrimSpace(string(runes)))
	}
	return out
}

func matrixFilesFromDiscordFiles(files []*discord.File) ([]matrixOutFile, error) {
	out := make([]matrixOutFile, 0, len(files))
	for _, file := range files {
		if file == nil || file.Reader == nil {
			continue
		}
		data, err := io.ReadAll(file.Reader)
		if err != nil {
			return nil, err
		}
		contentType := mime.TypeByExtension(path.Ext(file.Name))
		if contentType == "" {
			contentType = http.DetectContentType(data)
		}
		out = append(out, matrixOutFile{
			Name:        file.Name,
			ContentType: contentType,
			Data:        data,
			IsImage:     strings.HasPrefix(contentType, "image/"),
		})
	}
	return out, nil
}

func matrixFormatMsg(content, username string, reference *matrixMessage) string {
	if reference != nil && strings.TrimSpace(reference.Content) != "" {
		return fmt.Sprintf("<in reply to %s: \"%s\">\n%s: %s", reference.Author, strings.TrimSpace(reference.Content), username, content)
	}
	if username != "" {
		return fmt.Sprintf("%s: %s", username, content)
	}
	return content
}

func stripMatrixReplyFallback(body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	lines := strings.Split(body, "\n")
	for len(lines) > 0 && strings.HasPrefix(strings.TrimSpace(lines[0]), ">") {
		lines = lines[1:]
	}
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "" {
		lines = lines[1:]
	}
	return strings.Join(lines, "\n")
}

func parseOnOff(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enable", "enabled":
		return true
	default:
		return false
	}
}

func truthy(value string) bool {
	return parseOnOff(value)
}

func stringsEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func findMatrixModel(value string) (model.Model, bool) {
	query := strings.TrimSpace(value)
	for _, m := range model.AllModels {
		if strings.EqualFold(m.Name, query) || strings.EqualFold(m.Command, query) {
			return m, true
		}
	}
	return model.Model{}, false
}
