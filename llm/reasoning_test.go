package llm

import (
	"testing"

	"github.com/zeozeozeo/x3/model"
	"github.com/zeozeozeo/x3/openai"
)

func TestApplyReasoningSettingsVercelDisabled(t *testing.T) {
	var req openai.ChatCompletionRequest

	applyReasoningSettings(&req, model.ProviderVercel, false)

	if req.ReasoningEffort != "" {
		t.Fatalf("expected Vercel reasoning_effort to be omitted, got %q", req.ReasoningEffort)
	}
	if req.Reasoning == nil {
		t.Fatal("expected reasoning config")
	}
	if req.Reasoning.Enabled != nil {
		t.Fatalf("expected Vercel reasoning.enabled to be omitted, got %#v", *req.Reasoning.Enabled)
	}
	if req.Reasoning.Effort != "none" {
		t.Fatalf("expected reasoning.effort none, got %q", req.Reasoning.Effort)
	}
	if req.Reasoning.Exclude != nil {
		t.Fatalf("expected Vercel reasoning.exclude to be omitted, got %#v", *req.Reasoning.Exclude)
	}
	if req.Thinking != nil {
		t.Fatalf("expected Vercel thinking to be omitted, got %#v", req.Thinking)
	}
	if req.ChatTemplateKwargs != nil {
		t.Fatalf("expected Vercel chat_template_kwargs to be omitted, got %#v", req.ChatTemplateKwargs)
	}
	if req.ProviderOptions != nil {
		t.Fatalf("expected Vercel providerOptions to be omitted, got %#v", req.ProviderOptions)
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

	if req.ReasoningEffort != "" {
		t.Fatalf("expected Vercel reasoning_effort to be omitted, got %q", req.ReasoningEffort)
	}
	if req.Reasoning == nil {
		t.Fatal("expected reasoning config")
	}
	if req.Reasoning.Effort != "medium" {
		t.Fatalf("expected reasoning.effort medium, got %q", req.Reasoning.Effort)
	}
	if req.Reasoning.Enabled != nil || req.Reasoning.Exclude != nil {
		t.Fatalf("expected Vercel reasoning to only include effort, got %#v", req.Reasoning)
	}
	if req.Thinking != nil || req.ProviderOptions != nil || req.ChatTemplateKwargs != nil {
		t.Fatalf("expected Vercel-specific request to omit compatibility fields: thinking=%#v providerOptions=%#v chatTemplateKwargs=%#v", req.Thinking, req.ProviderOptions, req.ChatTemplateKwargs)
	}
}
