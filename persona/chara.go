// SillyTavern character card parsing (not fully spec compliant since it will attempt to fix wrongly formatted cards)
package persona

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/barasher/go-exiftool"
	"github.com/cloudflare/ahocorasick"
	"github.com/dustin/go-humanize"
	"github.com/google/uuid"
)

type TavernCardV1 struct {
	Name               string   `json:"name,omitempty"`
	Description        string   `json:"description,omitempty"`
	Personality        string   `json:"personality,omitempty"`
	FirstMes           string   `json:"first_mes,omitempty"`
	Avatar             string   `json:"avatar,omitempty"`
	MesExample         string   `json:"mes_example,omitempty"`
	Scenario           string   `json:"scenario,omitempty"`
	CreatorNotes       string   `json:"creator_notes,omitempty"`
	SystemPrompt       string   `json:"system_prompt,omitempty"`
	Tags               []string `json:"tags,omitempty"`
	AlternateGreetings []string `json:"alternate_greetings,omitempty"`
}

func (c TavernCardV2) DeepCopy() *TavernCardV2 {
	if c.Data == nil {
		return &TavernCardV2{Spec: c.Spec, SpecVersion: c.SpecVersion}
	}
	copied := TavernCardV2{
		Spec:        c.Spec,
		SpecVersion: c.SpecVersion,
		Data: &TavernCardV1{
			Name:               c.Data.Name,
			Description:        c.Data.Description,
			Personality:        c.Data.Personality,
			FirstMes:           c.Data.FirstMes,
			Avatar:             c.Data.Avatar,
			MesExample:         c.Data.MesExample,
			Scenario:           c.Data.Scenario,
			CreatorNotes:       c.Data.CreatorNotes,
			SystemPrompt:       c.Data.SystemPrompt,
			Tags:               clone(c.Data.Tags),
			AlternateGreetings: clone(c.Data.AlternateGreetings),
		},
	}
	return &copied
}

func (card TavernCardV1) AllGreetings() []string {
	var greetings []string
	if card.FirstMes != "" {
		greetings = append(greetings, card.FirstMes)
	}
	return append(greetings, card.AlternateGreetings...)
}

// https://github.com/malfoyslastname/character-card-spec-v2
type TavernCardV2 struct {
	Spec        string        `json:"spec,omitempty"`
	SpecVersion string        `json:"spec_version,omitempty"`
	Data        *TavernCardV1 `json:"data"`
}

func (c TavernCardV2) formatField(field string, user string) string {
	field = strings.NewReplacer(
		"\r\n", "\n",
		"{{char}}", c.Data.Name,
		"{{bot}}", c.Data.Name, // bad
		"{{user}}", user,
		"(char)", c.Data.Name, // bad
		"(bot)", c.Data.Name, // bad
		"(user)", user, // bad
		"<char>", c.Data.Name, // bad
		"<bot>", c.Data.Name, // bad
		"<user>", user, // bad
		"[char]", c.Data.Name, // bad
		"[bot]", c.Data.Name, // bad
		"[user]", user, // bad
		"{char}", c.Data.Name, // bad
		"{bot}", c.Data.Name, // bad
		"{user}", user, // bad
		// all bad
		"{{Char}}", c.Data.Name,
		"{{Bot}}", c.Data.Name,
		"{{User}}", user,
		"(Char)", c.Data.Name,
		"(Bot)", c.Data.Name,
		"(User)", user,
		"<Char>", c.Data.Name,
		"<Bot>", c.Data.Name,
		"<User>", user,
		"[Char]", c.Data.Name,
		"[Bot]", c.Data.Name,
		"[User]", user,
		"{Char}", c.Data.Name,
		"{Bot}", c.Data.Name,
		"{User}", user,
	).Replace(field)
	return field
}

var weirdPersonalityStringsMatcher = ahocorasick.NewStringMatcher([]string{
	"https://",
	"http://",
	"janitor",
	"jannyai",
	"openai",
	"deepseek",
	" bot ",
	" bot.",
	"llm",
	" ai ",
})

func (c *TavernCardV2) maybeSwapWeirdPersonalityFieldWithCreatorNotesIfTheCardAuthorDoesntKnowHowToUseThePersonalityFieldCorrectly() {
	if c.Data == nil {
		return
	}
	if ContainsEmoji(c.Data.Personality) || weirdPersonalityStringsMatcher.Contains([]byte(strings.ToLower(c.Data.Personality))) {
		if c.Data.CreatorNotes != "" {
			c.Data.CreatorNotes += "\n"
		}
		c.Data.CreatorNotes = c.Data.Personality
		c.Data.Personality = ""
	}
}

func (c TavernCardV2) formatExamples(user string) string {
	if c.Data.MesExample == "" {
		return ""
	}

	var sb strings.Builder
	i := 0
	for example := range strings.SplitSeq(c.Data.MesExample, "<START>") {
		example = strings.TrimSpace(c.formatField(example, user))
		if example == "" {
			continue
		}
		i++
		sb.WriteString("### Example ")
		sb.WriteString(strconv.Itoa(i))
		sb.WriteString(":\n")
		sb.WriteString(example)
		sb.WriteRune('\n')
	}

	return sb.String()
}

var (
	charaTemplate = template.Must(template.New("").Parse(`You are roleplaying as {{ .Char }}, a character with the following attributes:
{{ if .Description }}- Description: {{ .Description }}{{ end }}
{{ if .Personality }}- Personality: {{ .Personality }}{{ end }}
{{ if .Scenario }}- Scenario: {{ .Scenario }}{{ end }}
{{ if .Examples }}
- The following examples are unrelated to the context of the roleplay and represent the desired output formatting and dynamics of {{ .Char }}'s output in a roleplay session:
"""
{{ .Examples }}
"""{{ end }}
{{ if .Summaries }}
**Past chat summaries:**
{{ range .Summaries }}
- {{ .Str }} (updated {{ .Age }} messages ago)
{{ end }}
{{ end }}
{{ if .Context }}
**IMPORTANT:** Additional instructions for {{ .Char }}'s behavior:
{{ range .Context }}
- {{ . }}
{{ end }}
{{ end }}

Write {{ .Char }}'s next replies in a fictional chat between {{ .Char }} and {{ .User }}.`))

	errCharaExifNotFound = errors.New("character card not found in image exif")
)

type charaTemplateData struct {
	Char               string
	User               string
	Description        string
	Personality        string
	Scenario           string
	Examples           string
	Summaries          []Summary
	Context            []string
	InteractionElapsed string
}

func newCharaTemplateData(card *TavernCardV2, user string, summaries []Summary, context []string, interactedAt time.Time) charaTemplateData {
	now := time.Now().UTC()
	var elapsed string
	if !interactedAt.IsZero() && now.Sub(interactedAt) >= 5*time.Minute {
		elapsed = strings.TrimSpace(humanize.RelTime(interactedAt, now, "", ""))
	}
	return charaTemplateData{
		Char:               card.Data.Name,
		User:               user,
		Description:        card.formatField(card.Data.Description, user),
		Personality:        card.formatField(card.Data.Personality, user),
		Scenario:           card.formatField(card.Data.Scenario, user),
		Examples:           card.formatExamples(user),
		Summaries:          summaries,
		Context:            context,
		InteractionElapsed: elapsed,
		//Date:               fmt.Sprint(now.Date()),
		//Time:               now.Format(time.TimeOnly),
	}
}

func BuildCharaSystemPrompt(card *TavernCardV2, user string, summaries []Summary, context []string, interactedAt time.Time) string {
	if card == nil || card.Data == nil {
		return ""
	}
	var b bytes.Buffer
	data := newCharaTemplateData(card, user, summaries, context, interactedAt)
	if err := charaTemplate.Execute(&b, data); err != nil {
		slog.Error("BuildCharaSystemPrompt: template execution failed", "err", err)
		return ""
	}
	return b.String()
}

func (meta *PersonaMeta) ApplyJsonChara(data []byte, user string, context []string) (TavernCardV2, error) {
	slog.Debug("ApplyChara: chara", slog.Int("len", len(data)))

	var card TavernCardV2
	err := json.Unmarshal(data, &card)
	if err != nil {
		return card, err
	}

	if card.Data == nil {
		// might be a v1 card
		var cardV1 TavernCardV1
		err = json.Unmarshal(data, &cardV1)
		if err != nil {
			return card, err
		}
		card.Data = &cardV1
	}

	card.maybeSwapWeirdPersonalityFieldWithCreatorNotesIfTheCardAuthorDoesntKnowHowToUseThePersonalityFieldCorrectly()

	firstMessages := map[string]struct{}{
		card.Data.FirstMes: {},
	}
	for _, greeting := range card.Data.AlternateGreetings {
		firstMessages[greeting] = struct{}{}
	}

	firstMessagesArr := make([]string, 0, len(firstMessages))
	for greeting := range firstMessages {
		firstMessagesArr = append(firstMessagesArr, card.formatField(greeting, user))
	}

	meta.Name = "<character card>"
	if card.Data.Name != "" {
		meta.Name = card.Data.Name
	}
	meta.Desc = "<none>"
	meta.FirstMes = firstMessagesArr
	meta.IsFirstMes = len(firstMessagesArr) > 0
	meta.EnableImages = true
	meta.TavernCard = &card
	return card, nil
}

func writeTempFile(data []byte) (string, error) {
	err := os.MkdirAll(filepath.Join(".", "x3-temp"), 0755)
	if err != nil {
		return "", err
	}
	filepath := filepath.Join("x3-temp", fmt.Sprintf("%s.png", uuid.NewString()))
	err = os.WriteFile(filepath, data, 0644)
	if err != nil {
		return "", err
	}
	return filepath, nil
}

func (meta *PersonaMeta) ApplyChara(data []byte, user string, context []string) (TavernCardV2, error) {
	// try json first
	card, err := meta.ApplyJsonChara(data, user, context)
	if err == nil {
		return card, nil
	}
	slog.Warn("ApplyChara: failed to parse json chara, trying exif", "err", err)

	// not json, try extracting the "Chara" field from exif
	et, err := exiftool.NewExiftool()
	if err != nil {
		return card, err
	}
	defer et.Close()

	filePath, err := writeTempFile(data)
	if err != nil {
		return card, err
	}
	defer os.Remove(filePath)

	metadata := et.ExtractMetadata(filePath)
	if len(metadata) == 0 {
		return card, errCharaExifNotFound
	}

	for _, data := range metadata {
		if charaValue, found := data.Fields["Chara"]; found {
			// decode b64
			decodedData, err := base64.StdEncoding.DecodeString(charaValue.(string))
			if err != nil {
				return card, err
			}

			card, err = meta.ApplyJsonChara(decodedData, user, context)
			return card, err
		}
	}

	return card, errCharaExifNotFound
}
