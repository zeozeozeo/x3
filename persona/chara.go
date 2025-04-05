package persona

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"text/template"

	"github.com/barasher/go-exiftool"
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
	).Replace(field)
	return field
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
	systemPromptTemplate = template.Must(template.New("").Parse(`You are roleplaying as {{ .Char }}, a character with the following attributes:
{{ if .Description }}- Description: {{ .Description }}{{ end }}
{{ if .Personality }}- Personality: {{ .Personality }}{{ end }}
{{ if .Scenario }}- Scenario: {{ .Scenario }}{{ end }}
{{ if .Examples }}
- The following examples are unrelated to the context of the roleplay and represent the desired output formatting and dynamics of {{ .Char }}'s output in a roleplay session:
"""
{{ .Examples }}
"""{{ end }}

Write character dialogue in quotation marks. Write {{ .Char }}'s thoughts in asterisks.
Write {{ .Char }}'s next replies in a fictional chat between {{ .Char }} and {{ .User }}.`))

	errCharaExifNotFound = errors.New("character card not found in image exif")
)

// Apply json character card
func (meta *PersonaMeta) ApplyJsonChara(data []byte, user string) (TavernCardV2, error) {
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

	// execute template
	var b bytes.Buffer
	err = systemPromptTemplate.Execute(&b, struct {
		Char        string
		User        string
		Description string
		Personality string
		Scenario    string
		Examples    string
	}{
		Char:        card.Data.Name,
		User:        user,
		Description: card.formatField(card.Data.Description, user),
		Personality: card.formatField(card.Data.Personality, user),
		Scenario:    card.formatField(card.Data.Scenario, user),
		Examples:    card.formatExamples(user),
	})
	if err != nil {
		return card, err
	}

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
	meta.System = b.String()
	meta.FirstMes = firstMessagesArr
	meta.IsFirstMes = len(firstMessagesArr) > 0
	meta.DisableImages = false
	slog.Info("ApplyChara: generated system prompt", slog.String("system", meta.System))
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

func (meta *PersonaMeta) ApplyChara(data []byte, user string) (TavernCardV2, error) {
	// try json first
	card, err := meta.ApplyJsonChara(data, user)
	if err == nil {
		return card, nil
	}
	slog.Warn("ApplyChara: failed to parse json chara, trying exif", slog.Any("err", err))

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

			card, err = meta.ApplyJsonChara(decodedData, user)
			return card, err
		}
	}

	return card, errCharaExifNotFound
}
