package llm

import (
	"testing"

	"github.com/zeozeozeo/x3/model"
)

func TestApplyFallbackVisionModelsUsesRecentUserImage(t *testing.T) {
	if err := model.LoadModelsFromJSONData([]byte(`{
		"models": [
			{"name": "Text", "command": "text", "fallback_vision_model": "Vision"},
			{"name": "Vision", "command": "vision", "vision": true}
		],
		"default_models": ["Text"],
		"default_vision_models": ["Vision"],
		"current_version": 1
	}`)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = model.LoadModelsFromJSON() }()

	l := Llmer{}
	l.AddMessage(RoleUser, "look", 0)
	l.AddImage("https://example.com/image.png")

	got := l.applyFallbackVisionModels([]model.Model{model.GetModelByName("Text")})
	if len(got) != 1 || got[0].Name != "Vision" {
		t.Fatalf("fallback model = %#v, want Vision", got)
	}
}

func TestApplyFallbackVisionModelsIgnoresOlderImages(t *testing.T) {
	if err := model.LoadModelsFromJSONData([]byte(`{
		"models": [
			{"name": "Text", "command": "text", "fallback_vision_model": "Vision"},
			{"name": "Vision", "command": "vision", "vision": true}
		],
		"default_models": ["Text"],
		"default_vision_models": ["Vision"],
		"current_version": 1
	}`)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = model.LoadModelsFromJSON() }()

	l := Llmer{}
	l.AddMessage(RoleUser, "look", 0)
	l.AddImage("https://example.com/image.png")
	for i := 0; i < 4; i++ {
		l.AddMessage(RoleUser, "later", 0)
	}

	got := l.applyFallbackVisionModels([]model.Model{model.GetModelByName("Text")})
	if len(got) != 1 || got[0].Name != "Text" {
		t.Fatalf("fallback model = %#v, want Text", got)
	}
}

func TestApplyFallbackVisionModelsUsesDefaultVisionModels(t *testing.T) {
	if err := model.LoadModelsFromJSONData([]byte(`{
		"models": [
			{"name": "Text", "command": "text", "fallback_vision_model": "Default"},
			{"name": "Vision 1", "command": "vision1", "vision": true},
			{"name": "Vision 2", "command": "vision2", "vision": true}
		],
		"default_models": ["Text"],
		"default_vision_models": ["Vision 1", "Vision 2"],
		"current_version": 1
	}`)); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = model.LoadModelsFromJSON() }()

	l := Llmer{}
	l.AddMessage(RoleUser, "look", 0)
	l.AddImage("https://example.com/image.png")

	got := l.applyFallbackVisionModels([]model.Model{model.GetModelByName("Text")})
	if len(got) != 2 || got[0].Name != "Vision 1" || got[1].Name != "Vision 2" {
		t.Fatalf("fallback models = %#v, want Vision 1 then Vision 2", got)
	}
}

func TestContextHardMessageLimitOvershootsSoftLimit(t *testing.T) {
	if got := ContextHardMessageLimit(150); got != 300 {
		t.Fatalf("hard limit = %d, want 300", got)
	}
	if got := ContextHardMessageLimit(10); got != 74 {
		t.Fatalf("hard limit = %d, want 74", got)
	}
	if got := ContextHardMessageLimit(300); got != 500 {
		t.Fatalf("hard limit = %d, want 500", got)
	}
	if got := ContextHardMessageLimit(450); got != 500 {
		t.Fatalf("hard limit = %d, want 500", got)
	}
}

func TestTrimCacheFriendlyContextKeepsOvershootBand(t *testing.T) {
	l := Llmer{}
	for i := 0; i < ContextHardMessageLimit(100); i++ {
		l.AddMessage(RoleUser, "msg", 0)
	}

	if l.TrimCacheFriendlyContext(100) {
		t.Fatal("expected no trim while at hard limit")
	}

	l.AddMessage(RoleUser, "one more", 0)
	if !l.TrimCacheFriendlyContext(100) {
		t.Fatal("expected trim after exceeding hard limit")
	}
	if got := l.NonSystemMessageCount(); got != 100 {
		t.Fatalf("non-system messages = %d, want 100", got)
	}
}

func TestTrimCacheFriendlyContextPreservesSystemPrompt(t *testing.T) {
	l := Llmer{Messages: []Message{{Role: RoleSystem, Content: "system"}}}
	for i := 0; i < ContextHardMessageLimit(10)+1; i++ {
		l.AddMessage(RoleUser, "msg", 0)
	}

	if !l.TrimCacheFriendlyContext(10) {
		t.Fatal("expected trim")
	}
	if len(l.Messages) != 11 {
		t.Fatalf("messages = %d, want 11", len(l.Messages))
	}
	if l.Messages[0].Role != RoleSystem || l.Messages[0].Content != "system" {
		t.Fatalf("first message = %#v, want system prompt", l.Messages[0])
	}
	if got := l.NonSystemMessageCount(); got != 10 {
		t.Fatalf("non-system messages = %d, want 10", got)
	}
}
