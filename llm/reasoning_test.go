package llm

import (
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
	if req.Reasoning.Exclude != nil {
		t.Fatalf("expected Vercel disabled reasoning to omit exclude, got %#v", *req.Reasoning.Exclude)
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

func TestApplyReasoningSettingsNonVercelDisabledExcludesReasoning(t *testing.T) {
	var req openai.ChatCompletionRequest

	applyReasoningSettings(&req, model.ProviderOpenRouter, false)

	if req.Reasoning == nil || req.Reasoning.Exclude == nil || !*req.Reasoning.Exclude {
		t.Fatalf("expected non-Vercel disabled reasoning to keep exclude=true, got %#v", req.Reasoning)
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
