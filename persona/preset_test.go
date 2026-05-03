package persona

import (
	"strings"
	"testing"
)

func TestParseSTChatPresetAndBuildSystemPrompt(t *testing.T) {
	raw := []byte(`{
		"temperature": 0.8,
		"top_p": 0.9,
		"frequency_penalty": 0.2,
		"seed": 42,
		"wi_format": "Context:\n{0}",
		"prompts": [
			{"identifier":"main","name":"Main Prompt","role":"system","content":"Write {{char}}'s next reply to {{user}}."},
			{"identifier":"worldInfoBefore","name":"World Info","marker":true},
			{"identifier":"charDescription","name":"Description","marker":true},
			{"identifier":"jailbreak","name":"Post-History Instructions","role":"system","content":"Use vivid in-world details."},
			{"identifier":"chatHistory","name":"Chat History","marker":true}
		],
		"prompt_order": [
			{"character_id":100001,"order":[
				{"identifier":"main","enabled":true},
				{"identifier":"worldInfoBefore","enabled":true},
				{"identifier":"charDescription","enabled":true},
				{"identifier":"chatHistory","enabled":true},
				{"identifier":"jailbreak","enabled":true}
			]}
		]
	}`)

	preset, err := ParseSTChatPreset(raw)
	if err != nil {
		t.Fatalf("ParseSTChatPreset failed: %v", err)
	}

	card := &TavernCardV2{Data: &TavernCardV1{
		Name:        "Astra",
		Description: "A pilot from {{user}}'s crew.",
	}}
	system := preset.BuildSystemPrompt(card, "Mira", PromptContext{Context: []string{"The ship is damaged."}}, "fallback")

	for _, want := range []string{
		"Write Astra's next reply to Mira.",
		"Context:",
		"The ship is damaged.",
		"A pilot from Mira's crew.",
		"Use vivid in-world details.",
	} {
		if !strings.Contains(system, want) {
			t.Fatalf("system prompt missing %q:\n%s", want, system)
		}
	}
	if strings.Contains(system, "chatHistory") {
		t.Fatalf("chatHistory marker leaked into system prompt:\n%s", system)
	}

	settings := preset.ImportedSettings()
	if settings.Temperature != 0.8 || settings.TopP != 0.9 || settings.FrequencyPenalty != 0.2 {
		t.Fatalf("settings were not imported: %+v", settings)
	}
	if settings.Seed == nil || *settings.Seed != 42 {
		t.Fatalf("seed was not imported: %+v", settings.Seed)
	}
}

func TestChatPresetDeepCopy(t *testing.T) {
	preset, err := ParseSTChatPreset([]byte(`{
		"prompts":[{"identifier":"main","content":"hello"}],
		"prompt_order":[{"character_id":100000,"order":[{"identifier":"main","enabled":true}]}]
	}`))
	if err != nil {
		t.Fatal(err)
	}
	copied := preset.DeepCopy()
	copied.Prompts[0].Content = "changed"
	copied.PromptOrder[0].Order[0].Enabled = false
	if preset.Prompts[0].Content != "hello" {
		t.Fatal("prompt copy shares backing storage")
	}
	if !preset.PromptOrder[0].Order[0].Enabled {
		t.Fatal("prompt order copy shares backing storage")
	}
}
