package decide

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/minilm"
	"github.com/zeozeozeo/x3/systemone"
)

// Question ids. They are chosen by us and echoed back as the answer keys.
const (
	QuestionRespond    = "respond"
	QuestionAddressed  = "addressed"
	QuestionKind       = "kind"
	QuestionInvitation = "invitation"
)

// Options for the addressed question.
const (
	AddressedDirect       = "direct"
	AddressedContinuation = "continuation"
	AddressedAmbient      = "ambient"
)

// RoleBot and RoleUser label transcript turns so the model can tell its own
// messages apart from everyone else's.
const (
	RoleBot  = "bot"
	RoleUser = "user"
)

// Turn is one message in the state handed to the model.
type Turn struct {
	Role    string `json:"role"`
	Name    string `json:"name,omitempty"`
	Content string `json:"content"`
}

// State is the context every question is evaluated against.
type State struct {
	Note        string `json:"note"`
	Bot         string `json:"bot"`
	Channel     string `json:"channel,omitempty"`
	Persona     string `json:"persona,omitempty"`
	IdleSeconds int    `json:"seconds_since_last_bot_message"`
	Transcript  []Turn `json:"transcript"`
	Candidate   Turn   `json:"candidate"`
}

// addressedCriteria separates a message meant for the bot from room talk.
var addressedCriteria = map[string]string{
	AddressedDirect:       "The message names or addresses the bot, quotes the bot, replies to the bot, or asks the bot a question directly.",
	AddressedContinuation: "The message continues the conversation with the bot without naming it, such as answering it, correcting it, or asking for more of what it just said.",
	AddressedAmbient:      "The message is part of the room's general conversation between other people, or is the room talking, and the bot is not part of it.",
}

// kindCriteria classifies what the message is doing.
var kindCriteria = map[string]string{
	"question":    "The bot is being asked something and an answer would help.",
	"follow_up":   "The message reacts to or continues something the bot just said.",
	"instruction": "The message tells the bot to do something, or to stop, go away, or be quiet.",
	"banter":      "Jokes, teasing, sarcasm, or playful nonsense.",
	"statement":   "A remark about anything at all that does not need an answer.",
	"other":       "Anything that does not fit the categories above, including links, pasted output, and reactions.",
}

var invitationLevels = []string{
	"The message wants nothing from the bot and is only talking to the room.",
	"The message mentions the bot or its topic, but does not ask it for anything.",
	"The message could reasonably be answered by the bot, and a reply would not be unwelcome.",
	"The message explicitly asks the bot to respond, or is a follow-up the bot is expected to answer.",
}

func Questions(botNames []string) map[string]systemone.Question {
	bot := describeBot(botNames)
	return map[string]systemone.Question{
		QuestionRespond: systemone.Noul(
			fmt.Sprintf("Should the bot reply to the candidate message? Judge only the candidate message, using the transcript for context. Answer no when the bot should stay silent: the candidate tells the bot to stop, shut up, go away, or leave it alone, answers the bot to end the conversation, is clearly talking to someone else, or is an insult meant to wound rather than to talk. %s", bot),
			systemone.NoulCriteria{
				True:  "The bot is wanted in the conversation and replying is the right move.",
				False: "The bot is not wanted here, or has nothing useful to add, and replying would be intrusive.",
			},
		),
		QuestionAddressed: systemone.Choice(
			"Is the candidate message meant for the bot, a follow-up to the bot, or just the room talking?",
			addressedCriteria),
		QuestionKind: systemone.Choice(
			"What is the candidate message doing?",
			kindCriteria),
		QuestionInvitation: systemone.Score(
			"How strongly does the candidate message invite a reply from the bot, from not at all to explicitly asking for one?",
			invitationLevels),
	}
}

func describeBot(botNames []string) string {
	names := make([]string, 0, len(botNames))
	seen := map[string]bool{}
	for _, name := range botNames {
		name = strings.TrimSpace(name)
		if name == "" || seen[strings.ToLower(name)] {
			continue
		}
		seen[strings.ToLower(name)] = true
		names = append(names, name)
	}
	if len(names) == 0 {
		names = []string{"the bot"}
	}
	return fmt.Sprintf("The bot is called %s, and may also be summoned by saying any of %s.",
		names[0], strings.Join(names[1:], ", "))
}

// BuildState renders the decision state from cached history and the candidate
// message. History is trimmed to the newest turns and then to a character
// budget, dropping whole turns rather than truncating mid-message so the
// transcript never ends in half a sentence.
func BuildState(history []llm.Message, candidate Turn, botNames []string, channelName, personaName string, idleSeconds int, cfg Config) State {
	turns := make([]Turn, 0, cfg.HistoryTurns+1)
	for _, message := range history {
		if len(turns) >= cfg.HistoryTurns {
			turns = turns[1:]
		}
		turns = append(turns, Turn{
			Role:    messageRole(message.Role),
			Name:    minilm.Clean(message.Author),
			Content: cleanContent(message.Content),
		})
	}

	state := State{
		Note:        "Decide whether the bot should send a message in this channel in response to the candidate message. Earlier transcript turns give context; only the candidate message is up for a decision.",
		Bot:         describeBot(botNames),
		Channel:     minilm.Clean(channelName),
		Persona:     minilm.Clean(personaName),
		IdleSeconds: idleSeconds,
		Transcript:  turns,
		Candidate:   Turn{Role: RoleUser, Name: minilm.Clean(candidate.Name), Content: candidate.Content},
	}
	state.Transcript = trimTranscript(state.Transcript, cfg.MaxStateChars)
	return state
}

// trimTranscript drops the oldest turns until the transcript fits the budget.
func trimTranscript(turns []Turn, maxChars int) []Turn {
	total := 0
	for _, turn := range turns {
		total += len(turn.Name) + len(turn.Content) + len(turn.Role)
	}
	for len(turns) > 1 && total > maxChars {
		oldest := turns[0]
		total -= len(oldest.Name) + len(oldest.Content) + len(oldest.Role)
		turns = turns[1:]
	}
	return turns
}

func messageRole(role string) string {
	if role == llm.RoleAssistant {
		return RoleBot
	}
	return RoleUser
}

// cleanContent strips the decorations the bot adds to cached history so the
// model reads plain text: split continuations carry a zero-width marker and
// every turn is prefixed with its author.
func cleanContent(content string) string {
	content = strings.ReplaceAll(content, "\u200B", "")
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		lines[i] = stripAuthorPrefix(line)
	}
	return minilm.Clean(strings.Join(lines, "\n"))
}

// authorPrefix matches the "name: " attribution formatMsg puts on cached
// messages. It is deliberately narrow: anything containing a slash, colon, or
// other URL punctuation is treated as prose and left alone, so a message like
// "see https://example.com/x: y" keeps its text.
var authorPrefix = regexp.MustCompile(`^[\p{L}\p{N} _.'\-]{1,32}: `)

// stripAuthorPrefix removes a leading "name: " attribution, and the
// "<in reply to name: ">" wrapper formatMsg adds to replies.
func stripAuthorPrefix(line string) string {
	line = strings.TrimSpace(line)
	if strings.HasPrefix(line, "<in reply to ") {
		if end := strings.Index(line, ">\n"); end != -1 {
			line = strings.TrimSpace(line[end+2:])
		} else if end := strings.Index(line, ">"); end != -1 {
			line = strings.TrimSpace(line[end+1:])
		}
	}
	if !authorPrefix.MatchString(line) {
		return line
	}
	_, rest, _ := strings.Cut(line, ": ")
	return strings.TrimSpace(rest)
}
