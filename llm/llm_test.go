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
