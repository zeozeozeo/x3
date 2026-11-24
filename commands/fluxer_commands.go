package commands

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/disgoorg/disgo/discord"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
	"github.com/zeozeozeo/x3/site"
)

func (b *FluxerBot) parseCommand(content string, isDM bool) (string, bool) {
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

func (b *FluxerBot) commandUsage(name string, isDM bool) string {
	if isDM {
		return "!" + name
	}
	return b.prefix + " " + name
}

func (b *FluxerBot) fluxerCommandContext(command, rest string, isDM bool) textCommandContext {
	return textCommandContextFor(b.commandUsage(command, isDM), rest)
}

// sendText sends command output as a reply; every part carries the
// command-result marker so history backfill skips it.
func (b *FluxerBot) sendText(ctx context.Context, channelID, replyToID, text string) error {
	_, err := b.sendCommandText(ctx, channelID, replyToID, text, nil)
	return err
}

func (b *FluxerBot) sendCommandText(ctx context.Context, channelID, replyToID, text string, files []fluxerOutFile) (string, error) {
	var ids []string
	if strings.TrimSpace(text) != "" || len(files) == 0 {
		for _, part := range splitFluxerText(strings.TrimSpace(text)) {
			sent, err := b.sendFluxerMessage(ctx, channelID, replyToID, fluxerCommandResultMarker+part, nil)
			if sent != nil {
				ids = append(ids, sent.ID)
			}
			if err != nil {
				return firstSentFluxerID(ids), err
			}
			replyToID = ""
		}
	}
	for _, group := range chunkFluxerFiles(files, 10) {
		sent, err := b.sendFluxerMessage(ctx, channelID, replyToID, fluxerCommandResultMarker, group)
		if sent != nil {
			ids = append(ids, sent.ID)
		}
		if err != nil {
			return firstSentFluxerID(ids), err
		}
		replyToID = ""
	}
	return firstSentFluxerID(ids), nil
}

func firstSentFluxerID(ids []string) string {
	if len(ids) > 0 {
		return ids[0]
	}
	return ""
}

func (b *FluxerBot) handleCommand(ctx context.Context, msg *fluxerMessage, raw string, isDM bool) error {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "help" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.helpText(isDM))
	}
	parsed := parseTextCommandArgs(raw)
	nameToken := parsed.At(0)
	name := strings.ToLower(strings.TrimSpace(nameToken.Text))
	rest := parsed.RestAfter(0)
	if strings.Contains(nameToken.Text, "=") {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(raw, nameToken, "Invalid top-level command syntax.", "Command names cannot contain `=`. Run `"+b.commandUsage("help", isDM)+"` for valid commands."))
	}

	switch name {
	case "chat":
		if rest == "" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(raw, nameToken, "Missing prompt for `chat` command.", "Expected: `"+b.commandUsage("chat", isDM)+" <prompt>`"))
		}
		copyMsg := *msg
		copyMsg.Content = rest
		return b.handleLlm(ctx, &copyMsg, false, "")
	case "persona":
		return b.handlePersonaCommand(ctx, msg, rest, b.fluxerCommandContext("persona", rest, isDM))
	case "context":
		return b.handleContextCommand(ctx, msg, rest, isDM)
	case "lobotomy":
		return b.handleLobotomyCommand(ctx, msg, rest)
	case "regenerate":
		if rest == "help" || rest == "-h" || rest == "--help" || rest == "?" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Regenerate command:\n"+b.commandUsage("regenerate", isDM)+" [prepend]\n\nRegenerates the last assistant response. Optional text is used as assistant prefill.")
		}
		return b.handleLlm(ctx, msg, true, rest)
	case "stats":
		return b.handleStatsCommand(ctx, msg)
	case "models":
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.fluxerModelListText(isDM))
	case "chatlog":
		return b.handleChatlogCommand(ctx, msg, rest, isDM)
	case "worldmem":
		return b.handleWorldmemCommand(ctx, msg, rest, isDM)
	case "site":
		return b.handleSiteCommand(ctx, msg, rest, isDM)
	case "blacklist":
		return b.handleBlacklistCommand(ctx, msg, rest, false)
	case "imageblacklist":
		return b.handleBlacklistCommand(ctx, msg, rest, true)
	default:
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(raw, nameToken, "Unknown command `"+nameToken.Text+"`.", "Run `"+b.commandUsage("help", isDM)+"` to see available commands."))
	}
}

func (b *FluxerBot) helpText(isDM bool) string {
	return strings.Join([]string{
		"x3 commands:",
		b.commandUsage("chat", isDM) + " <prompt>",
		b.commandUsage("site", isDM) + " [--persona] <theme>",
		b.commandUsage("persona", isDM),
		b.commandUsage("persona", isDM) + " set <name>",
		b.commandUsage("persona", isDM) + " username <name|reset>",
		b.commandUsage("persona", isDM) + " botname <name|reset>",
		b.commandUsage("persona", isDM) + " model <model name>",
		b.commandUsage("persona", isDM) + " system <prompt>",
		b.commandUsage("persona", isDM) + " card <url> | preset <url>",
		b.commandUsage("persona", isDM) + " context|temperature|top_p|frequency_penalty|seed <value>",
		b.commandUsage("persona", isDM) + " images|thinking|reasoning|html|continuations|tools on|off",
		b.commandUsage("persona", isDM) + " jailbreak off|prefill|prompt",
		b.commandUsage("models", isDM) + " list available model short names",
		b.commandUsage("context", isDM) + " add|list|clear|delete|get|edit ...",
		b.commandUsage("lobotomy", isDM) + " [turns] [reset_persona]",
		b.commandUsage("regenerate", isDM) + " [prepend]",
		b.commandUsage("chatlog", isDM) + " export|import",
		b.commandUsage("worldmem", isDM) + " on|off|clear|export|import",
	}, "\n")
}

func (b *FluxerBot) personaHelpText(isDM bool) string {
	base := b.commandUsage("persona", isDM)
	return strings.Join([]string{
		"Persona command:",
		base + "                         show current persona settings",
		base + " help                    show this help",
		base + " list                    list built-in personas",
		base + " models                  list available model short names",
		base + " set <name>              set persona",
		base + " username <name>        set your name in bot context (reset to clear)",
		base + " botname <name>         set the bot persona name (reset to clear)",
		base + " model <model>           set model by name or command",
		base + " system <prompt>         set custom system prompt",
		base + " card [url]              import character card from URL or attachment",
		base + " preset [url]            import SillyTavern preset from URL or attachment",
		base + " context <n>             set soft context message target",
		base + " temperature <value>     set temperature",
		base + " top_p <value>           set top_p",
		base + " frequency_penalty <v>   set frequency penalty",
		base + " seed <n>                set seed, 0 resets",
		base + " images on|off           toggle image generation",
		base + " thinking on|off         toggle reasoning.txt attachments",
		base + " reasoning on|off        toggle model-side reasoning",
		base + " html on|off             toggle HTML rendering",
		base + " continuations           on|off toggle smart continuation trigger",
		base + " tools on|off            toggle web and Discord search tools",
		base + " jailbreak off|prefill|prompt",
		"Use spaces, not key=value, for persona settings.",
	}, "\n")
}

func (b *FluxerBot) fluxerModelListText(isDM bool) string {
	return modelListText() + "\n\nSet one with:\n" + b.commandUsage("persona", isDM) + " model <short name>"
}

func (b *FluxerBot) contextHelpText(isDM bool) string {
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

func (b *FluxerBot) lobotomyHelpText(isDM bool) string {
	base := b.commandUsage("lobotomy", isDM)
	return strings.Join([]string{
		"Lobotomy command:",
		base + "                  forget all cached messages",
		base + " <turns>          forget the last N turns",
		base + " reset            also reset persona",
		base + " <turns> reset    combine both options",
		"",
		"Before clearing context, x3 attaches a chatlog archive when one is available.",
	}, "\n")
}

func (b *FluxerBot) chatlogHelpText(isDM bool) string {
	base := b.commandUsage("chatlog", isDM)
	return strings.Join([]string{
		"Chatlog command:",
		base + " export           export cached context as JSON",
		"Upload an `x3-chatlog-<year>-<month>-<day>.json` file to import it automatically.",
	}, "\n")
}

func (b *FluxerBot) worldmemHelpText(isDM bool) string {
	base := b.commandUsage("worldmem", isDM)
	return strings.Join([]string{
		"Worldmem command:",
		base + " on              enable world-bible retrieval",
		base + " off             disable retrieval (facts stay stored)",
		base + " clear           delete all world facts for this channel",
		base + " export          export world facts as JSON",
		base + " list            show stored world facts",
		"Upload an `x3-worldmem-<year>-<month>-<day>.json` file to import it automatically.",
	}, "\n")
}

func (b *FluxerBot) blacklistHelpText(isDM bool, image bool) string {
	name := "blacklist"
	if image {
		name = "imageblacklist"
	}
	base := b.commandUsage(name, isDM)
	return strings.Join([]string{
		strings.ToUpper(name[:1]) + name[1:] + " command:",
		base + " on              enable",
		base + " off             disable",
	}, "\n")
}

func (b *FluxerBot) handlePersonaCommand(ctx context.Context, msg *fluxerMessage, rest string, diagCtx textCommandContext) error {
	key := b.channelKey(msg.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	if strings.TrimSpace(rest) == "" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, personaInfoText(cache, b.userName(msg), b.selfDisplayName(), msg.IsDM))
	}
	parsed := parseTextCommandArgs(rest)
	actionToken := parsed.At(0)
	action := strings.ToLower(strings.TrimSpace(actionToken.Text))
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.personaHelpText(msg.IsDM))
	}
	if strings.Contains(actionToken.Text, "=") {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "invalid persona command syntax", "use a space between the setting and value, not `=`. Example: `"+b.commandUsage("persona", msg.IsDM)+" model glm5`"))
	}
	if action == "models" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.fluxerModelListText(msg.IsDM))
	}
	if action == "list" {
		var list strings.Builder
		list.WriteString("Available personas:\n")
		for i, p := range persona.AllPersonas {
			fmt.Fprintf(&list, "%d. %s", i+1, p.String())
			if p.Name != p.String() {
				fmt.Fprintf(&list, " (`%s`)", p.Name)
			}
			list.WriteString("\n")
		}
		list.WriteString("\nSet by number or name, for example:\n")
		list.WriteString(b.commandUsage("persona", msg.IsDM) + " set 1\n")
		list.WriteString(b.commandUsage("persona", msg.IsDM) + " set protogen")
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, strings.TrimSpace(list.String()))
	}
	valueToken := parsed.At(1)
	value := strings.TrimSpace(valueToken.Text)
	attachmentValueActions := action == "card" || action == "preset"
	if value == "" && !attachmentValueActions {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "missing value for persona action `"+action+"`", "expected: `"+b.commandUsage("persona", msg.IsDM)+" "+action+" <value>`"))
	}
	if valueToken.Text != "" && strings.Contains(valueToken.Text, "=") {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "invalid value syntax for persona action `"+action+"`", "use spaces instead of key=value syntax"))
	}
	valueRest := parsed.RestAfter(0)
	if action == "system" {
		value = valueRest
	}

	prev := cache.PersonaMeta.DeepCopy()
	switch action {
	case "username":
		userCache := db.GetUserCacheByKey(b.userKey(msg.SenderID))
		if strings.EqualFold(value, "reset") {
			userCache.Username = ""
		} else {
			userCache.Username = strings.TrimSpace(valueRest)
		}
		if err := userCache.WriteKey(b.userKey(msg.SenderID)); err != nil {
			return err
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Updated username.")
	case "botname":
		if strings.EqualFold(value, "reset") {
			cache.PersonaMeta.BotName = ""
		} else {
			cache.PersonaMeta.BotName = strings.TrimSpace(valueRest)
		}
		return b.writePersonaUpdate(ctx, msg, key, cache, prev)
	case "set":
		meta, ok := findCommandPersona(value)
		if !ok {
			userCache := db.GetUserCacheByKey(b.userKey(msg.SenderID))
			for i := range userCache.Personas {
				pName := userCache.Personas[i].PersonaName
				if pName == "" {
					pName = userCache.Personas[i].Name
				}
				if pName == value {
					botName := cache.PersonaMeta.BotName
					cache.PersonaMeta = persona.PersonaMeta{
						Name:                      pName,
						BotName:                   botName,
						TavernCard:                v1ToV2(userCache.Personas[i]),
						Models:                    persona.PersonaProto.Models,
						Settings:                  persona.PersonaProto.Settings,
						NeedSummaries:             true,
						EnableMiniLMContinuations: persona.PersonaProto.EnableMiniLMContinuations,
					}
					return b.writePersonaUpdate(ctx, msg, key, cache, prev)
				}
			}
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Unknown persona: "+value)
		}
		botName := cache.PersonaMeta.BotName
		cache.PersonaMeta = meta
		cache.PersonaMeta.BotName = botName
		cache.PersonaMeta.TavernCard = nil
	case "model":
		m, ok := findCommandModel(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Unknown model: "+value)
		}
		cache.PersonaMeta.Models = []string{m.Name}
	case "system":
		cache.PersonaMeta.System = strings.ReplaceAll(value, "\\n", "\n")
		cache.PersonaMeta.TavernCard = nil
		cache.PersonaMeta.ChatPreset = nil
	case "context":
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidIntDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona context"))
		}
		if n < 0 {
			n = db.DefaultContextMessages
		}
		cache.ContextLength = db.ClampContextLength(n, false)
	case "temperature":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidFloatDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona temperature"))
		}
		cache.PersonaMeta.Settings.Temperature = float32(v)
	case "top_p":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidFloatDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona top_p"))
		}
		cache.PersonaMeta.Settings.TopP = float32(v)
	case "frequency_penalty":
		v, err := strconv.ParseFloat(value, 32)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidFloatDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona frequency_penalty"))
		}
		cache.PersonaMeta.Settings.FrequencyPenalty = float32(v)
	case "seed":
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidIntDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona seed"))
		}
		if n == 0 {
			cache.PersonaMeta.Settings.Seed = nil
		} else {
			cache.PersonaMeta.Settings.Seed = &n
		}
	case "images":
		enabled, ok := parseTextBool(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona images"))
		}
		cache.PersonaMeta.EnableImages = enabled
	case "thinking":
		enabled, ok := parseTextBool(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona thinking"))
		}
		cache.PersonaMeta.ThinkingTraces = enabled
	case "reasoning":
		enabled, ok := parseTextBool(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona reasoning"))
		}
		cache.PersonaMeta.Settings.Reasoning = enabled
	case "html":
		enabled, ok := parseTextBool(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona html"))
		}
		cache.PersonaMeta.RenderHTML = enabled
	case "minilm", "continuations":
		enabled, ok := parseTextBool(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona continuations"))
		}
		cache.PersonaMeta.EnableMiniLMContinuations = enabled
	case "tools":
		enabled, ok := parseTextBool(value)
		if !ok {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "persona tools"))
		}
		cache.PersonaMeta.Tools = &enabled
	case "jailbreak":
		switch strings.ToLower(value) {
		case persona.JailbreakOff:
			cache.PersonaMeta.Jailbreak = ""
		case persona.JailbreakPrefill, persona.JailbreakPrompt:
			cache.PersonaMeta.Jailbreak = strings.ToLower(value)
		default:
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(valueToken), "invalid value for persona action `jailbreak`", "expected: off, prefill or prompt"))
		}
	case "card":
		var body []byte
		var err error
		value = strings.TrimSpace(valueRest)
		if value != "" {
			body, _, err = fetchPersonaCardData(value, discord.Attachment{})
		} else if len(msg.Attachments) > 0 {
			attachment := &msg.Attachments[0]
			if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
				return err
			}
			body = attachment.Data
		} else {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "missing character card source", "pass a card URL or attach a card image/JSON file with `"+b.commandUsage("persona", msg.IsDM)+" card`"))
		}
		if err != nil {
			return err
		}
		if _, err := cache.PersonaMeta.ApplyChara(body, msg.Author); err != nil {
			return err
		}
	case "preset":
		var body []byte
		var err error
		value = strings.TrimSpace(valueRest)
		if value != "" {
			body, _, err = fetchJSONAttachmentData(value, discord.Attachment{}, "preset")
		} else if len(msg.Attachments) > 0 {
			attachment := &msg.Attachments[0]
			if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
				return err
			}
			body = attachment.Data
		} else {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "missing preset source", "pass a preset JSON URL or attach a preset JSON file with `"+b.commandUsage("persona", msg.IsDM)+" preset`"))
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
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(diagCtx.Raw(rest), diagCtx.Token(actionToken), "unknown persona action `"+actionToken.Text+"`", "run `"+b.commandUsage("persona", msg.IsDM)+" help` for valid actions"))
	}
	cache.PersonaMeta.Settings = cache.PersonaMeta.Settings.Fixup()
	return b.writePersonaUpdate(ctx, msg, key, cache, prev)
}

func (b *FluxerBot) writePersonaUpdate(ctx context.Context, msg *fluxerMessage, key string, cache *db.ChannelCache, prev persona.PersonaMeta) error {
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
	if cache.PersonaMeta.EnableMiniLMContinuations != prev.EnableMiniLMContinuations {
		changes = append(changes, fmt.Sprintf("continuations=%t", cache.PersonaMeta.EnableMiniLMContinuations))
	}
	if cache.PersonaMeta.ToolsEnabled() != prev.ToolsEnabled() {
		changes = append(changes, fmt.Sprintf("tools=%t", cache.PersonaMeta.ToolsEnabled()))
	}
	if cache.PersonaMeta.JailbreakMode() != prev.JailbreakMode() {
		changes = append(changes, fmt.Sprintf("jailbreak=%s", cache.PersonaMeta.JailbreakMode()))
	}
	if len(changes) == 0 {
		changes = append(changes, "settings updated")
	}
	return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Updated persona: "+strings.Join(changes, ", "))
}

func (b *FluxerBot) handleContextCommand(ctx context.Context, msg *fluxerMessage, rest string, isDM bool) error {
	parsed := parseTextCommandArgs(rest)
	actionToken, action, diag := textCommandAction(parsed, "context")
	if diag != nil {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, *diag+"\n\n"+b.contextHelpText(isDM))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.contextHelpText(isDM))
	}
	valueToken := parsed.At(1)
	value := strings.TrimSpace(valueToken.Text)
	valueRest := parsed.RestAfter(0)
	key := b.channelKey(msg.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	switch action {
	case "add":
		if strings.TrimSpace(valueRest) == "" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" add <text>"))
		}
		cache.Context = append(cache.Context, valueRest)
	case "clear":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, valueToken, "context clear"))
		}
		cache.Context = nil
	case "list":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, valueToken, "context list"))
		}
		if len(cache.Context) == 0 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "No context set for this channel.")
		}
		var sb strings.Builder
		for i, item := range cache.Context {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, item)
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, strings.TrimSpace(sb.String()))
	case "delete":
		if value == "" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" delete <n>"))
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidIntDiagnostic(rest, valueToken, "context index"))
		}
		if n < 1 || n > len(cache.Context) {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, valueToken, "Context index out of range.", fmt.Sprintf("Expected an index from 1 to %d.", len(cache.Context))))
		}
		cache.Context = append(cache.Context[:n-1], cache.Context[n:]...)
	case "get":
		if value == "" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" get <n>"))
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidIntDiagnostic(rest, valueToken, "context index"))
		}
		if n < 1 || n > len(cache.Context) {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, valueToken, "Context index out of range.", fmt.Sprintf("Expected an index from 1 to %d.", len(cache.Context))))
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, cache.Context[n-1])
	case "edit":
		if value == "" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textMissingValueDiagnostic(rest, actionToken, "context", action, b.commandUsage("context", isDM)+" edit <n> <new text>"))
		}
		n, err := strconv.Atoi(value)
		if err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidIntDiagnostic(rest, valueToken, "context index"))
		}
		if n < 1 || n > len(cache.Context) {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, valueToken, "Context index out of range.", fmt.Sprintf("Expected an index from 1 to %d.", len(cache.Context))))
		}
		newText := parsed.RestAfter(1)
		if strings.TrimSpace(newText) == "" {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, valueToken, "Missing replacement text for `context edit`.", "Expected: `"+b.commandUsage("context", isDM)+" edit <n> <new text>`"))
		}
		cache.Context[n-1] = strings.TrimSpace(newText)
	default:
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, actionToken, "Unknown context action `"+actionToken.Text+"`.", "Valid actions: add, list, clear, delete, get, edit.")+"\n\n"+b.contextHelpText(isDM))
	}
	if err := cache.WriteKey(key); err != nil {
		return err
	}
	return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Context updated.")
}

func (b *FluxerBot) handleLobotomyCommand(ctx context.Context, msg *fluxerMessage, rest string) error {
	parsed := parseTextCommandArgs(rest)
	amount := 0
	resetPersona := false
	for i, token := range parsed.Args {
		part := strings.ToLower(token.Text)
		if i == 0 && (part == "help" || part == "-h" || part == "--help" || part == "?") {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.lobotomyHelpText(msg.IsDM))
		}
		if strings.Contains(token.Text, "=") {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, token, "Invalid lobotomy command syntax.", "Use positional arguments, not key=value syntax. Example: `"+b.commandUsage("lobotomy", msg.IsDM)+" 5 reset`"))
		}
		if part == "reset_persona" || part == "reset" {
			resetPersona = true
			continue
		}
		if i == 0 {
			n, err := strconv.Atoi(token.Text)
			if err != nil {
				return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidIntDiagnostic(rest, token, "lobotomy amount"))
			}
			amount = n
			continue
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, token, "lobotomy"))
	}
	key := b.channelKey(msg.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	archive := buildFluxerChatArchive(msg, cache)
	archiveData, err := marshalChatArchive(archive)
	if err != nil {
		return err
	}
	attachArchive := !chatArchiveIsEmpty(archive)

	if resetPersona {
		cache.PersonaMeta = db.NewChannelCache().PersonaMeta
	}
	messagesToRemove := amount * 2
	lobotomizeCachedHistory(cache, messagesToRemove, func() *llm.Llmer {
		return llm.NewLlmerForKey(key)
	})
	cache.Summaries = nil
	cache.Memories = nil
	if err := cache.WriteKey(key); err != nil {
		return err
	}
	text := "Lobotomized for this channel."
	if amount > 0 {
		text = fmt.Sprintf("Removed last %d turns from the context", amount)
	}
	if attachArchive {
		_, err = b.sendCommandText(ctx, msg.ChannelID, msg.MessageID, text, []fluxerOutFile{fluxerChatArchiveFile(archiveData, archive.ExportedAt)})
		return err
	}
	return b.sendText(ctx, msg.ChannelID, msg.MessageID, text)
}

func (b *FluxerBot) handleStatsCommand(ctx context.Context, msg *fluxerMessage) error {
	stats, err := db.GetGlobalStats()
	if err != nil {
		return err
	}
	cache := db.GetChannelCacheByKey(b.channelKey(msg.ChannelID))
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
	return b.sendText(ctx, msg.ChannelID, msg.MessageID, text)
}

func (b *FluxerBot) handleBlacklistCommand(ctx context.Context, msg *fluxerMessage, rest string, image bool) error {
	commandName := "blacklist"
	if image {
		commandName = "imageblacklist"
	}
	parsed := parseTextCommandArgs(rest)
	actionToken, action, diag := textCommandAction(parsed, commandName)
	if diag != nil {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, *diag+"\n\n"+b.blacklistHelpText(msg.IsDM, image))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.blacklistHelpText(msg.IsDM, image))
	}
	enabled, ok := parseTextBool(action)
	if !ok {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textInvalidBoolDiagnostic(rest, actionToken, commandName)+"\n\n"+b.blacklistHelpText(msg.IsDM, image))
	}
	if len(parsed.Args) > 1 {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), commandName))
	}
	key := b.channelKey(msg.ChannelID)
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
	return b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Blacklist updated: %t", enabled))
}

func (b *FluxerBot) handleChatlogCommand(ctx context.Context, msg *fluxerMessage, rest string, isDM bool) error {
	parsed := parseTextCommandArgs(rest)
	actionToken, action, diag := textCommandAction(parsed, "chatlog")
	if diag != nil {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, *diag+"\n\n"+b.chatlogHelpText(isDM))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.chatlogHelpText(isDM))
	}
	key := b.channelKey(msg.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	switch action {
	case "export":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "chatlog export"))
		}
		archive := buildFluxerChatArchive(msg, cache)
		data, err := marshalChatArchive(archive)
		if err != nil {
			return err
		}
		_, err = b.sendCommandText(ctx, msg.ChannelID, msg.MessageID, "Exported chatlog.", []fluxerOutFile{fluxerChatArchiveFile(data, archive.ExportedAt)})
		return err
	case "import":
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Fluxer imports chatlog archives automatically. Upload an `x3-chatlog-<year>-<month>-<day>.json` file in the channel.")
	default:
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, actionToken, "Unknown chatlog action `"+actionToken.Text+"`.", "Valid actions: export.")+"\n\n"+b.chatlogHelpText(isDM))
	}
}

func (b *FluxerBot) handleWorldmemCommand(ctx context.Context, msg *fluxerMessage, rest string, isDM bool) error {
	parsed := parseTextCommandArgs(rest)
	actionToken, action, diag := textCommandAction(parsed, "worldmem")
	if diag != nil {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, *diag+"\n\n"+b.worldmemHelpText(isDM))
	}
	if action == "help" || action == "-h" || action == "--help" || action == "?" {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, b.worldmemHelpText(isDM))
	}
	key := b.channelKey(msg.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	switch action {
	case "on":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "worldmem on"))
		}
		cache.WorldMemDisabled = false
		if err := cache.WriteKey(key); err != nil {
			return err
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "World memory on: world-bible retrieval is now enabled in this channel.")
	case "off":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "worldmem off"))
		}
		cache.WorldMemDisabled = true
		if err := cache.WriteKey(key); err != nil {
			return err
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "World memory off: retrieval disabled. Stored facts are kept and new facts are still recorded.")
	case "clear":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "worldmem clear"))
		}
		n, err := db.ClearWorldEntries(key)
		if err != nil {
			return err
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Deleted %s from this channel's world memory.", pluralize(n, "fact")))
	case "export":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "worldmem export"))
		}
		data, err := db.ExportWorldEntries(key)
		if err != nil {
			return err
		}
		n, err := db.CountWorldEntries(key)
		if err != nil {
			return err
		}
		_, err = b.sendCommandText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Exported %s from this channel's world memory.", pluralize(n, "fact")), []fluxerOutFile{{
			Name:        worldmemExportFilename(time.Now().UTC()),
			ContentType: "application/json",
			Data:        data,
		}})
		return err
	case "list":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "worldmem list"))
		}
		entries, err := db.GetWorldEntries(key)
		if err != nil {
			return err
		}
		if len(entries) == 0 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "No world facts stored for this channel yet.")
		}
		var sb strings.Builder
		sb.WriteString(fmt.Sprintf("World memory (%s):\n", pluralize(len(entries), "fact")))
		shown := entries
		if len(shown) > 10 {
			shown = shown[:10]
		}
		for i, entry := range shown {
			fmt.Fprintf(&sb, "%d. %s\n", i+1, ellipsisTrim(entry.Text, 300))
		}
		if len(entries) > len(shown) {
			fmt.Fprintf(&sb, "…and %d more.", len(entries)-len(shown))
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, strings.TrimSpace(sb.String()))
	case "import":
		if len(parsed.Args) > 1 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, textUnexpectedTokenDiagnostic(rest, parsed.At(1), "worldmem import"))
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Fluxer imports world memory archives automatically. Upload an `x3-worldmem-<year>-<month>-<day>.json` file in the channel.")
	default:
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, textCommandDiagnostic(rest, actionToken, "Unknown worldmem action `"+actionToken.Text+"`.", "Valid actions: on, off, clear, export, import, list.")+"\n\n"+b.worldmemHelpText(isDM))
	}
}

func (b *FluxerBot) tryAutoImportWorldmem(ctx context.Context, msg *fluxerMessage) (bool, error) {
	for i := range msg.Attachments {
		attachment := &msg.Attachments[i]
		if !isWorldmemExportFilename(attachment.Filename) {
			continue
		}
		if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, "World memory import failed: "+err.Error())
		}
		if attachment.Size > worldImportMaxSize || len(attachment.Data) > worldImportMaxSize {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("World memory import failed: archive is too large; max size is %d bytes", worldImportMaxSize))
		}

		entries, err := db.ParseWorldExport(attachment.Data)
		if err != nil {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, "World memory import failed: "+err.Error())
		}

		key := b.channelKey(msg.ChannelID)
		imported, err := importWorldEntries(key, entries)
		if err != nil {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, "World memory import failed: "+err.Error())
		}
		return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Imported %s into this channel's world memory.", pluralize(imported, "fact")))
	}

	return false, nil
}

func (b *FluxerBot) tryAutoImportChatArchive(ctx context.Context, msg *fluxerMessage) (bool, error) {
	for i := range msg.Attachments {
		attachment := &msg.Attachments[i]
		if !isChatArchiveFilename(attachment.Filename) {
			continue
		}
		if err := b.downloadFluxerAttachment(ctx, attachment); err != nil {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, "Chatlog import failed: "+err.Error())
		}
		if attachment.Size > chatArchiveMaxSize || len(attachment.Data) > chatArchiveMaxSize {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Chatlog import failed: archive is too large; max size is %d bytes", chatArchiveMaxSize))
		}

		var archive chatArchive
		if err := json.Unmarshal(attachment.Data, &archive); err != nil {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, "Chatlog import failed: invalid archive JSON: "+err.Error())
		}
		if archive.Version != chatArchiveVersion {
			return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Chatlog import failed: unsupported archive version %d", archive.Version))
		}

		key := b.channelKey(msg.ChannelID)
		cache := db.GetChannelCacheByKey(key)
		importedMessages := applyChatArchiveToCache(cache, llm.NewLlmerForKey(key), archive)
		if err := cache.WriteKey(key); err != nil {
			return true, err
		}
		return true, b.sendText(ctx, msg.ChannelID, msg.MessageID, fmt.Sprintf("Imported %s into cached context.", pluralize(len(importedMessages), "message")))
	}

	return false, nil
}

func buildFluxerChatArchive(msg *fluxerMessage, cache *db.ChannelCache) chatArchive {
	return chatArchive{
		Version:    chatArchiveVersion,
		ExportedAt: time.Now().UTC(),
		ChannelID:  msg.ChannelID,
		Messages:   archiveMessagesFromLLM(cacheHistoryMessages(cache)),
		Summaries:  activeSummaries(cache),
		Memories:   append([]string(nil), cache.Memories...),
		Context:    append([]string(nil), cache.Context...),
	}
}

func fluxerChatArchiveFile(data []byte, exportedAt time.Time) fluxerOutFile {
	return fluxerOutFile{
		Name:        chatArchiveFilename(exportedAt),
		ContentType: "application/json",
		Data:        data,
	}
}

func (b *FluxerBot) handleSiteCommand(ctx context.Context, msg *fluxerMessage, rest string, isDM bool) error {
	if siteManager == nil {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Site runtime is not available.")
	}
	parsed := parseTextCommandArgs(rest)
	if len(parsed.Args) == 0 {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Usage:\n"+
			b.commandUsage("site", isDM)+" <theme>\n"+
			b.commandUsage("site", isDM)+" [--persona|-p] <theme>\n"+
			b.commandUsage("site", isDM)+" cancel <site_id>")
	}

	firstArg := strings.ToLower(parsed.Args[0].Text)
	if firstArg == "cancel" {
		if len(parsed.Args) < 2 {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Missing site ID. Usage: "+b.commandUsage("site", isDM)+" cancel <site_id>")
		}
		siteID := parsed.Args[1].Text
		if err := siteManager.CancelSite(siteID, msg.SenderID); err != nil {
			return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Failed to cancel site: "+err.Error())
		}
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Site canceled.")
	}

	var includePersona bool
	themeStartIdx := 0
	if firstArg == "--persona" || firstArg == "-p" || firstArg == "include_persona" {
		includePersona = true
		themeStartIdx = 1
	}

	if themeStartIdx >= len(parsed.Args) {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Missing theme. Usage: "+b.commandUsage("site", isDM)+" [--persona] <theme>")
	}

	var theme string
	if includePersona {
		theme = parsed.RestAfter(0)
	} else {
		theme = rest
	}
	theme = strings.TrimSpace(theme)

	key := b.channelKey(msg.ChannelID)
	cache := db.GetChannelCacheByKey(key)
	additionalContext := append([]string(nil), cache.Context...)
	var personaSystem string
	if includePersona {
		personaSystem = persona.GetPersonaByMeta(
			cache.PersonaMeta,
			msg.Author,
			msg.IsDM,
			persona.PromptContext{Context: append([]string(nil), cache.Context...)},
		).System
		additionalContext = nil
	}

	ctx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()

	result, err := siteManager.CreateSite(ctx, site.CreateOptions{
		CreatorID:         msg.SenderID,
		Theme:             theme,
		AdditionalContext: additionalContext,
		PersonaSystem:     personaSystem,
	})
	if err != nil {
		return b.sendText(ctx, msg.ChannelID, msg.MessageID, "Failed to create site: "+err.Error())
	}

	duration := time.Until(result.ExpiresAt).Round(time.Minute)
	if duration < time.Minute {
		duration = time.Until(result.ExpiresAt).Round(time.Second)
	}
	content := fmt.Sprintf(
		"Live site: %s\nDeleted automatically in %s (at %s)\nTo cancel: `%s`",
		result.URL,
		duration.String(),
		result.ExpiresAt.Local().Format("15:04:05 MST"),
		b.commandUsage("site", isDM)+" cancel "+result.SiteID,
	)

	sentID, err := b.sendCommandText(ctx, msg.ChannelID, msg.MessageID, content, nil)
	if err != nil {
		return err
	}
	if sentID != "" {
		_ = siteManager.AttachDiscordMessage(result.SiteID, sentID)
	}
	return nil
}
