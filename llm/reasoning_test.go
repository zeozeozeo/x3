package llm

import (
	"encoding/json"
	"testing"

	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
)

func TestApplyReasoningSettingsVercelDisabled(t *testing.T) {
	var req openai.ChatCompletionRequest

	applyReasoningSettings(&req, model.ProviderVercel, false)

	if req.ReasoningEffort != "none" {
		t.Fatalf("expected reasoning_effort none, got %q", req.ReasoningEffort)
	}
	if req.Reasoning == nil {
		t.Fatal("expected reasoning config")
	}
	if req.Reasoning.Enabled == nil || *req.Reasoning.Enabled {
		t.Fatalf("expected reasoning.enabled false, got %#v", req.Reasoning.Enabled)
	}
	if req.Reasoning.Effort != "none" {
		t.Fatalf("expected reasoning.effort none, got %q", req.Reasoning.Effort)
	}
	if req.Reasoning.Exclude == nil || !*req.Reasoning.Exclude {
		t.Fatalf("expected reasoning.exclude true, got %#v", req.Reasoning.Exclude)
	}
	if req.Thinking == nil || req.Thinking.Type != "disabled" {
		t.Fatalf("expected thinking.type disabled, got %#v", req.Thinking)
	}
	if got := req.ChatTemplateKwargs["enable_thinking"]; got != false {
		t.Fatalf("expected enable_thinking false, got %#v", got)
	}

	zai, ok := req.ProviderOptions["zai"].(map[string]any)
	if !ok {
		t.Fatalf("expected zai provider options, got %#v", req.ProviderOptions["zai"])
	}
	if got := zai["thinking"]; got != req.Thinking {
		t.Fatalf("expected zai thinking to reuse request thinking config, got %#v", got)
	}
}

func TestApplyReasoningSettingsEnabled(t *testing.T) {
	var req openai.ChatCompletionRequest

	applyReasoningSettings(&req, model.ProviderVercel, true)

	if req.ReasoningEffort != "medium" {
		t.Fatalf("expected reasoning_effort medium, got %q", req.ReasoningEffort)
	}
	if req.Reasoning == nil || req.Reasoning.Enabled == nil || !*req.Reasoning.Enabled {
		t.Fatalf("expected reasoning.enabled true, got %#v", req.Reasoning)
	}
	if req.Reasoning.Exclude == nil || *req.Reasoning.Exclude {
		t.Fatalf("expected enabled reasoning to set exclude=false, got %#v", req.Reasoning.Exclude)
	}
	if req.Thinking == nil || req.Thinking.Type != "enabled" {
		t.Fatalf("expected thinking.type enabled, got %#v", req.Thinking)
	}
}

func TestApplyReasoningSettingsDisabledJSONBody(t *testing.T) {
	req := openai.ChatCompletionRequest{
		Model: "zai/glm-5",
		Messages: []openai.ChatCompletionMessage{
			{Role: openai.ChatMessageRoleUser, Content: "test"},
		},
	}

	applyReasoningSettings(&req, model.ProviderVercel, false)

	data, err := json.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}

	var body map[string]any
	if err := json.Unmarshal(data, &body); err != nil {
		t.Fatal(err)
	}

	want := map[string]any{
		"reasoning_effort": "none",
		"reasoning": map[string]any{
			"enabled": false,
			"effort":  "none",
			"exclude": true,
		},
		"thinking": map[string]any{
			"type": "disabled",
		},
		"chat_template_kwargs": map[string]any{
			"enable_thinking": false,
		},
		"providerOptions": map[string]any{
			"zai": map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
			"openai": map[string]any{
				"reasoningEffort": "none",
			},
			"deepseek": map[string]any{
				"thinking": map[string]any{"type": "disabled"},
			},
		},
	}

	for key, value := range want {
		got, ok := body[key]
		if !ok {
			t.Fatalf("expected body to include %q", key)
		}
		gotJSON, _ := json.Marshal(got)
		wantJSON, _ := json.Marshal(value)
		if string(gotJSON) != string(wantJSON) {
			t.Fatalf("unexpected %s: got %s want %s", key, gotJSON, wantJSON)
		}
	}
}
