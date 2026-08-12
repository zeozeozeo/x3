package commands

import (
	"fmt"
	"log/slog"
	"math/rand/v2"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/disgoorg/disgo/bot"
	"github.com/disgoorg/disgo/discord"
	"github.com/disgoorg/disgo/handler"
	"github.com/disgoorg/snowflake/v2"
	"github.com/zeozeozeo/x3/db"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/model"
)

const (
	abSamplingChance  = 0.30
	abMessagesPerTry  = 10
	abComparisonTTL   = 10 * time.Minute
	abComparisonIntro = "Btw, which response do you like more?"
	forceABTestTag    = "x3!forceabtest"
)

type abCompletionResult struct {
	response string
	usage    llm.Usage
	err      error
}

type abComparison struct {
	mu sync.Mutex

	RequesterID  snowflake.ID
	ChannelID    snowflake.ID
	messageID    snowflake.ID
	DefaultModel string
	ABModel      string
	ResponseA    string
	ResponseB    string
	Selected     rune
	Completed    bool
}

var abComparisons sync.Map // message ID string -> *abComparison

func isABComparisonMessage(message discord.Message) bool {
	return strings.HasPrefix(message.Content, abComparisonIntro)
}

func shouldTryABTest(cache *db.ChannelCache) bool {
	if len(model.ABPool) == 0 {
		return false
	}

	next := cache.AdvanceABMessageCount(abMessagesPerTry)

	return next == abMessagesPerTry && rand.Float64() < abSamplingChance
}

func stripForceABTestTag(content string) (string, bool) {
	if !strings.Contains(content, forceABTestTag) {
		return content, false
	}
	return strings.TrimSpace(strings.ReplaceAll(content, forceABTestTag, "")), true
}

func chooseABModel(active []model.Model, shouldTry bool) (model.Model, bool) {
	if !shouldTry {
		return model.Model{}, false
	}

	activeNames := make(map[string]struct{}, len(active))
	for _, current := range active {
		activeNames[current.Name] = struct{}{}
	}

	configuredModels := make(map[string]model.Model, len(model.AllModels))
	for _, configured := range model.AllModels {
		configuredModels[configured.Name] = configured
	}

	candidates := make([]model.Model, 0, len(model.ABPool))
	seen := make(map[string]struct{}, len(model.ABPool))
	for _, candidateName := range model.ABPool {
		candidate, exists := configuredModels[candidateName]
		if !exists {
			slog.Warn("skipping unknown A/B pool model", "model", candidateName)
			continue
		}
		if _, exists := activeNames[candidate.Name]; exists {
			continue
		}
		if _, exists := seen[candidate.Name]; exists {
			continue
		}
		seen[candidate.Name] = struct{}{}
		candidates = append(candidates, candidate)
	}
	if len(candidates) == 0 {
		return model.Model{}, false
	}

	picked := candidates[rand.IntN(len(candidates))]
	candidateNames := make([]string, len(candidates))
	for i, candidate := range candidates {
		candidateNames[i] = candidate.Name
	}
	slog.Debug("selected A/B model", "candidates", candidateNames, "selected", picked.Name)
	return picked, true
}

func comparisonContent(responseLabel, response string) string {
	if responseLabel == "" {
		return abComparisonIntro
	}

	response = replaceLlmTagsWithNewlines(response, nil)
	response = strings.ReplaceAll(response, "\r\n", "\n")
	response = strings.ReplaceAll(response, "\r", "\n")
	response = strings.ReplaceAll(response, "\n\n", "\n")
	// Prevent a model-supplied fence from ending the comparison codeblock.
	response = strings.ReplaceAll(response, "```", "``\u200b`")

	const maxContentRunes = 1940
	if utf8.RuneCountInString(response) > maxContentRunes {
		response = string([]rune(response)[:maxContentRunes]) + "…"
	}
	return abComparisonIntro + "\n\nResponse " + responseLabel + ":\n```\n" + response + "\n```"
}

func cleanABResponse(response string) string {
	if _, answer := llm.ExtractThinking(response); answer != "" {
		response = answer
	}
	if display, _ := extractMemoryTags(response); display != response {
		response = display
	}
	return response
}

func abComparisonComponents(comparison *abComparison) discord.LayoutComponent {
	selected := comparison.Selected
	return discord.NewActionRow(
		discord.ButtonComponent{
			Style:    buttonStyleForSelection(selected, 'A'),
			Label:    "Response A",
			CustomID: "/abtest/" + comparisonID(comparison) + "/a",
		},
		discord.ButtonComponent{
			Style:    buttonStyleForSelection(selected, 'B'),
			Label:    "Response B",
			CustomID: "/abtest/" + comparisonID(comparison) + "/b",
		},
		discord.ButtonComponent{
			Style:    discord.ButtonStyleSecondary,
			Label:    "This one",
			Emoji:    &discord.ComponentEmoji{Name: "✅"},
			Disabled: selected == 0,
			CustomID: "/abtest/" + comparisonID(comparison) + "/confirm",
		},
		discord.ButtonComponent{
			Style:    discord.ButtonStyleSecondary,
			Emoji:    &discord.ComponentEmoji{Name: "❌"},
			CustomID: "/abtest/" + comparisonID(comparison) + "/close",
		},
	)
}

// comparisonID is replaced by the map key when the prompt is created. The
// field is intentionally kept out of the public state object.
func comparisonID(comparison *abComparison) string {
	return comparison.messageID.String()
}

func buttonStyleForSelection(selected, button rune) discord.ButtonStyle {
	if selected == button {
		return discord.ButtonStylePrimary
	}
	return discord.ButtonStyleSecondary
}

func sendABComparison(client *bot.Client, comparison *abComparison, referenceID snowflake.ID) error {
	message := discord.NewMessageCreate().
		WithContent(comparisonContent("", "")).
		WithAllowedMentions(&discord.AllowedMentions{RepliedUser: false})
	if referenceID != 0 {
		message = message.WithMessageReferenceByID(referenceID)
	}

	sent, err := client.Rest.CreateMessage(comparison.ChannelID, message)
	if err != nil {
		return err
	}
	comparison.messageID = sent.ID
	abComparisons.Store(sent.ID.String(), comparison)
	if _, err := client.Rest.UpdateMessage(comparison.ChannelID, sent.ID,
		discord.NewMessageUpdate().WithComponents(abComparisonComponents(comparison))); err != nil {
		abComparisons.Delete(sent.ID.String())
		_ = client.Rest.DeleteMessage(comparison.ChannelID, sent.ID)
		return err
	}
	time.AfterFunc(abComparisonTTL, func() {
		abComparisons.Delete(sent.ID.String())
	})

	if err := db.RecordABComparison(comparison.DefaultModel, comparison.ABModel); err != nil {
		slog.Error("failed to record A/B comparison", "err", err)
	}
	return nil
}

func HandleABTestButton(data discord.ButtonInteractionData, event *handler.ComponentEvent) error {
	comparisonID := event.Vars["id"]
	comparisonAny, ok := abComparisons.Load(comparisonID)
	if !ok {
		return sendInteractionErrorComponent(event, "This A/B comparison has expired.", true)
	}
	comparison := comparisonAny.(*abComparison)
	if event.User().ID != comparison.RequesterID {
		return event.CreateMessage(discord.NewMessageCreate().
			WithContent("Only the person who started this comparison can vote.").
			WithEphemeral(true))
	}

	action := event.Vars["action"]
	comparison.mu.Lock()
	if comparison.Completed {
		comparison.mu.Unlock()
		return sendInteractionErrorComponent(event, "This A/B comparison is already complete.", true)
	}
	switch action {
	case "a", "b":
		comparison.Selected = rune(strings.ToUpper(action)[0])
		selected := comparison.Selected
		response := comparison.ResponseA
		label := "A"
		if selected == 'B' {
			response = comparison.ResponseB
			label = "B"
		}
		content := comparisonContent(label, response)
		components := abComparisonComponents(comparison)
		comparison.mu.Unlock()
		if err := event.DeferUpdateMessage(); err != nil {
			return err
		}
		_, err := event.UpdateInteractionResponse(discord.NewMessageUpdate().
			WithContent(content).
			WithComponents(components))
		return err
	case "confirm":
		if comparison.Selected != 'A' && comparison.Selected != 'B' {
			comparison.mu.Unlock()
			return sendInteractionErrorComponent(event, "Select Response A or Response B first.", true)
		}
		selected := comparison.Selected
		comparison.Completed = true
		comparison.mu.Unlock()

		if err := db.RecordABVote(comparison.DefaultModel, comparison.ABModel, selected); err != nil {
			return fmt.Errorf("record A/B vote: %w", err)
		}
		if err := event.DeferUpdateMessage(); err != nil {
			return err
		}
		abComparisons.Delete(comparisonID)
		if err := event.Client().Rest.DeleteMessage(event.Message.ChannelID, event.Message.ID); err != nil {
			return err
		}

		modelName := comparison.DefaultModel
		if selected == 'B' {
			modelName = comparison.ABModel
		}
		_, err := event.CreateFollowupMessage(discord.NewMessageCreate().
			WithContent(fmt.Sprintf("The model was: %s", modelName)).
			WithEphemeral(true))
		return err
	case "close":
		comparison.Completed = true
		comparison.mu.Unlock()
		if err := db.RecordABClose(comparison.DefaultModel, comparison.ABModel); err != nil {
			return fmt.Errorf("record closed A/B comparison: %w", err)
		}
		abComparisons.Delete(comparisonID)
		if err := event.DeferUpdateMessage(); err != nil {
			return err
		}
		return event.Client().Rest.DeleteMessage(event.Message.ChannelID, event.Message.ID)
	default:
		comparison.mu.Unlock()
		return fmt.Errorf("unknown A/B action %q", action)
	}
}
