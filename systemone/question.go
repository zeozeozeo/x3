// Package systemone implements the System One decision model API
package systemone

import (
	"encoding/json"
	"fmt"
)

// QuestionType identifies which answer shape a question expects.
type QuestionType string

const (
	// TypeNoul asks a yes/no question and answers with P(yes).
	TypeNoul QuestionType = "noul"
	// TypeChoice picks one option from a set of named criteria.
	TypeChoice QuestionType = "choice"
	// TypeScore rates the state against an ordered rubric.
	TypeScore QuestionType = "score"
)

// Model names understood by the Cloudflare clef endpoints. The provider path
// carries the bare name (no @cf/cloudflare/ prefix) as does the request body.
const (
	ModelFlash = "clef-flash"
	ModelBase  = "clef"
)

// NoulCriteria optionally spells out what counts as a yes and a no.
type NoulCriteria struct {
	True  string `json:"true,omitempty"`
	False string `json:"false,omitempty"`
}

// Question is one named question in a request. The wire shape of criteria
// depends on the type: an object keyed by option name for choice, an ordered
// array of level descriptions for score, and an optional true/false object for
// noul. Use the Noul, Choice, and Score constructors rather than filling the
// fields directly so the type and criteria stay consistent.
type Question struct {
	Type         QuestionType
	Instructions string
	Criteria     map[string]string
	Levels       []string
	NoulTrue     string
	NoulFalse    string
}

// Noul builds a yes/no question. Criteria is optional; when both entries are
// empty it is omitted.
func Noul(instructions string, criteria NoulCriteria) Question {
	return Question{
		Type:         TypeNoul,
		Instructions: instructions,
		NoulTrue:     criteria.True,
		NoulFalse:    criteria.False,
	}
}

// Choice builds a single-choice question from option name to description.
func Choice(instructions string, criteria map[string]string) Question {
	return Question{
		Type:         TypeChoice,
		Instructions: instructions,
		Criteria:     criteria,
	}
}

// Score builds a rating question from ordered level descriptions, lowest
// first. Two to ten levels are accepted by the spec.
func Score(instructions string, levels []string) Question {
	return Question{
		Type:         TypeScore,
		Instructions: instructions,
		Levels:       levels,
	}
}

// MarshalJSON emits the type-specific criteria representation.
func (q Question) MarshalJSON() ([]byte, error) {
	out := map[string]any{
		"type":         string(q.Type),
		"instructions": q.Instructions,
	}
	switch q.Type {
	case TypeChoice:
		out["criteria"] = q.Criteria
	case TypeScore:
		out["criteria"] = q.Levels
	case TypeNoul:
		if q.NoulTrue != "" || q.NoulFalse != "" {
			out["criteria"] = NoulCriteria{True: q.NoulTrue, False: q.NoulFalse}
		}
	default:
		return nil, fmt.Errorf("unknown systemone question type %q", q.Type)
	}
	return json.Marshal(out)
}
