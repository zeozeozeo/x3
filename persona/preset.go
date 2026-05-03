package persona

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

type STChatPrompt struct {
	Name              string   `json:"name,omitempty"`
	Identifier        string   `json:"identifier,omitempty"`
	Role              string   `json:"role,omitempty"`
	Content           string   `json:"content,omitempty"`
	SystemPrompt      bool     `json:"system_prompt,omitempty"`
	Marker            bool     `json:"marker,omitempty"`
	Position          string   `json:"position,omitempty"`
	Depth             int      `json:"depth,omitempty"`
	Order             int      `json:"order,omitempty"`
	InjectionPosition int      `json:"injection_position,omitempty"`
	ForbidOverrides   bool     `json:"forbid_overrides,omitempty"`
	InjectionDepth    int      `json:"injection_depth,omitempty"`
	InjectionOrder    int      `json:"injection_order,omitempty"`
	Triggers          []string `json:"triggers,omitempty"`
}

type STPromptOrderItem struct {
	Identifier string `json:"identifier,omitempty"`
	Enabled    bool   `json:"enabled"`
}

type STPromptOrder struct {
	CharacterID int                 `json:"character_id,omitempty"`
	Order       []STPromptOrderItem `json:"order,omitempty"`
}

type STChatPreset struct {
	Name                 string          `json:"name,omitempty"`
	Temperature          float32         `json:"temperature,omitempty"`
	TopP                 float32         `json:"top_p,omitempty"`
	FrequencyPenalty     float32         `json:"frequency_penalty,omitempty"`
	PresencePenalty      float32         `json:"presence_penalty,omitempty"`
	Seed                 int             `json:"seed,omitempty"`
	Prompts              []STChatPrompt  `json:"prompts,omitempty"`
	PromptOrder          []STPromptOrder `json:"prompt_order,omitempty"`
	WIFormat             string          `json:"wi_format,omitempty"`
	ScenarioFormat       string          `json:"scenario_format,omitempty"`
	PersonalityFormat    string          `json:"personality_format,omitempty"`
	NewChatPrompt        string          `json:"new_chat_prompt,omitempty"`
	NewExampleChatPrompt string          `json:"new_example_chat_prompt,omitempty"`
	ContinueNudgePrompt  string          `json:"continue_nudge_prompt,omitempty"`
	GroupNudgePrompt     string          `json:"group_nudge_prompt,omitempty"`
	AssistantPrefill     string          `json:"assistant_prefill,omitempty"`
}

func ParseSTChatPreset(data []byte) (*STChatPreset, error) {
	var preset STChatPreset
	if err := json.Unmarshal(data, &preset); err != nil {
		return nil, err
	}
	if len(preset.Prompts) == 0 {
		return nil, fmt.Errorf("preset has no chat-completion prompts")
	}
	if preset.WIFormat == "" {
		preset.WIFormat = "{0}"
	}
	if preset.ScenarioFormat == "" {
		preset.ScenarioFormat = "{{scenario}}"
	}
	if preset.PersonalityFormat == "" {
		preset.PersonalityFormat = "{{personality}}"
	}
	return &preset, nil
}

func (p *STChatPreset) DeepCopy() *STChatPreset {
	if p == nil {
		return nil
	}
	copied := *p
	copied.Prompts = append([]STChatPrompt(nil), p.Prompts...)
	if p.PromptOrder != nil {
		copied.PromptOrder = make([]STPromptOrder, len(p.PromptOrder))
		for i := range p.PromptOrder {
			copied.PromptOrder[i] = p.PromptOrder[i]
			copied.PromptOrder[i].Order = append([]STPromptOrderItem(nil), p.PromptOrder[i].Order...)
		}
	}
	return &copied
}

func (p STChatPreset) promptByIdentifier() map[string]STChatPrompt {
	out := make(map[string]STChatPrompt, len(p.Prompts))
	for _, prompt := range p.Prompts {
		if prompt.Identifier == "" {
			continue
		}
		out[prompt.Identifier] = prompt
	}
	return out
}

func (p STChatPreset) selectedOrder() []STPromptOrderItem {
	if len(p.PromptOrder) == 0 {
		prompts := append([]STChatPrompt(nil), p.Prompts...)
		sort.SliceStable(prompts, func(i, j int) bool {
			return prompts[i].Order < prompts[j].Order
		})
		items := make([]STPromptOrderItem, 0, len(prompts))
		for _, prompt := range prompts {
			if prompt.Identifier != "" {
				items = append(items, STPromptOrderItem{Identifier: prompt.Identifier, Enabled: true})
			}
		}
		return items
	}

	selected := p.PromptOrder[0]
	for _, order := range p.PromptOrder {
		if order.CharacterID == 100001 {
			selected = order
			break
		}
		if order.CharacterID == 100000 {
			selected = order
		}
	}
	return selected.Order
}

type stMacroData struct {
	Char          string
	User          string
	Description   string
	Personality   string
	Scenario      string
	Examples      string
	PromptContext string
}

func stCardData(card *TavernCardV2, user string, promptContext PromptContext, fallbackChar string) stMacroData {
	data := stMacroData{
		Char:          firstNonEmptyString(fallbackChar, "Assistant"),
		User:          firstNonEmptyString(user, "User"),
		PromptContext: promptContext.BuildBlock(),
	}
	if card == nil || card.Data == nil {
		return data
	}
	data.Char = firstNonEmptyString(card.Data.Name, data.Char)
	data.Description = card.FormatField(card.Data.Description, user)
	data.Personality = card.FormatField(card.Data.Personality, user)
	data.Scenario = card.FormatField(card.Data.Scenario, user)
	data.Examples = card.formatExamples(user)
	return data
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

var stRandomMacroRegexp = regexp.MustCompile(`(?is)\{\{random::([^{}]+)\}\}`)

func replaceSTMacros(s string, data stMacroData) string {
	now := time.Now()
	replacements := map[string]string{
		"char":            data.Char,
		"bot":             data.Char,
		"user":            data.User,
		"description":     data.Description,
		"charDescription": data.Description,
		"personality":     data.Personality,
		"charPersonality": data.Personality,
		"scenario":        data.Scenario,
		"mesExamples":     data.Examples,
		"mesExamplesRaw":  data.Examples,
		"date":            now.Format("January 2, 2006"),
		"isodate":         now.Format("2006-01-02"),
		"time":            now.Format("15:04"),
		"isotime":         now.Format("15:04:05"),
		"weekday":         now.Weekday().String(),
		"datetime":        now.Format(time.RFC1123),
	}
	for key, value := range replacements {
		for _, variant := range []string{
			"{{" + key + "}}",
			"{{ " + key + " }}",
			"{{" + strings.ToLower(key) + "}}",
			"{{ " + strings.ToLower(key) + " }}",
		} {
			s = strings.ReplaceAll(s, variant, value)
		}
	}
	s = stRandomMacroRegexp.ReplaceAllStringFunc(s, func(match string) string {
		inner := strings.TrimSuffix(strings.TrimPrefix(match, "{{random::"), "}}")
		parts := strings.Split(inner, "::")
		for _, part := range parts {
			if trimmed := strings.TrimSpace(part); trimmed != "" {
				return trimmed
			}
		}
		return ""
	})
	s = strings.ReplaceAll(s, "{{trim}}", "")
	return strings.TrimSpace(html.UnescapeString(s))
}

func formatSTTemplate(tmpl, value string, data stMacroData) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if tmpl == "" {
		return value
	}
	out := strings.ReplaceAll(tmpl, "{0}", value)
	out = strings.ReplaceAll(out, "{{scenario}}", value)
	out = strings.ReplaceAll(out, "{{personality}}", value)
	return replaceSTMacros(out, data)
}

func (p STChatPreset) markerContent(identifier string, card *TavernCardV2, data stMacroData) string {
	switch identifier {
	case "chatHistory":
		return ""
	case "worldInfoBefore", "worldInfoAfter":
		return formatSTTemplate(p.WIFormat, data.PromptContext, data)
	case "charDescription":
		return data.Description
	case "charPersonality":
		return formatSTTemplate(p.PersonalityFormat, data.Personality, data)
	case "scenario":
		return formatSTTemplate(p.ScenarioFormat, data.Scenario, data)
	case "dialogueExamples":
		if data.Examples == "" {
			return ""
		}
		prefix := replaceSTMacros(p.NewExampleChatPrompt, data)
		if prefix == "" {
			return data.Examples
		}
		return strings.TrimSpace(prefix + "\n" + data.Examples)
	case "personaDescription":
		return ""
	default:
		if card != nil && card.Data != nil && identifier == "systemPrompt" {
			return card.FormatField(card.Data.SystemPrompt, data.User)
		}
		return ""
	}
}

func (p STChatPreset) BuildSystemPrompt(card *TavernCardV2, user string, promptContext PromptContext, fallbackChar string) string {
	data := stCardData(card, user, promptContext, fallbackChar)
	prompts := p.promptByIdentifier()
	var sections []string

	for _, item := range p.selectedOrder() {
		if !item.Enabled || item.Identifier == "" || item.Identifier == "chatHistory" {
			continue
		}
		prompt, ok := prompts[item.Identifier]
		var content string
		if ok && prompt.Marker {
			content = p.markerContent(item.Identifier, card, data)
		} else if ok {
			content = replaceSTMacros(prompt.Content, data)
		} else {
			content = p.markerContent(item.Identifier, card, data)
		}
		if strings.TrimSpace(content) != "" {
			sections = append(sections, content)
		}
	}

	return strings.TrimSpace(strings.Join(sections, "\n\n"))
}

func (p STChatPreset) ImportedSettings() InferenceSettings {
	settings := InferenceSettings{
		Temperature:      p.Temperature,
		TopP:             p.TopP,
		FrequencyPenalty: p.FrequencyPenalty,
	}
	if p.Seed >= 0 {
		seed := p.Seed
		settings.Seed = &seed
	}
	return settings.Fixup()
}

func (p STChatPreset) DisplayName() string {
	if strings.TrimSpace(p.Name) != "" {
		return p.Name
	}
	if len(p.Prompts) > 0 {
		return "SillyTavern preset (" + strconv.Itoa(len(p.Prompts)) + " prompts)"
	}
	return "SillyTavern preset"
}
