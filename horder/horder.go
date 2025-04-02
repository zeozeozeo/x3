package horder

import (
	"log/slog"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeozeozeo/aihorde-go"
)

func getenv(key, fallback string) string {
	if value, ok := os.LookupEnv(key); ok {
		return value
	}
	return fallback
}

var (
	apiKey     = getenv("X3_AIHORDE_API_KEY", "0000000000")
	goodStyles = []string{
		"anime", "artistic", "furry",
	}
	// https://raw.githubusercontent.com/Haidra-Org/AI-Horde-image-model-reference/refs/heads/main/stable_diffusion.json
	featuredModels = []string{
		"Pony Diffusion XL",
		"WAI-ANI-NSFW-PONYXL",
		"NTR MIX IL-Noob XL",
		"Cetus-Mix",
		//"Anything v5",
		"Nova Anime XL",
		"MeinaMix",
		"WAI-CUTE Pony",
		"Lawlas's yiff mix",
		"Flux.1-Schnell fp8 (Compact)",
		"Dreamshaper",
	}

	horder *Horder = NewHorder(MustFetchModelDetails())
)

func GetHorder() *Horder {
	return horder
}

type Horder struct {
	horde         *aihorde.AIHorde
	cachedModels  []aihorde.ActiveModel
	modelCacheAge time.Time
	modelsData    ModelsData
	mu            sync.Mutex
}

func NewHorder(modelsData ModelsData) *Horder {
	return &Horder{
		horde: aihorde.NewAIHorde(
			aihorde.WithDefaultToken(apiKey),
			aihorde.WithClientAgent("x3:1.0:github.com/zeozeozeo/aihorde-go"),
		),
		modelsData: modelsData,
	}
}

func (h *Horder) fetchImageModels() ([]aihorde.ActiveModel, error) {
	if len(h.cachedModels) > 0 && time.Since(h.modelCacheAge) < 2*time.Minute {
		return h.cachedModels, nil
	}
	models, err := h.horde.GetModels(
		aihorde.WithModelsType(aihorde.ModelTypeImage),
	)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	h.modelCacheAge = time.Now()
	h.cachedModels = models
	h.mu.Unlock()
	return models, nil
}

type ScoredModel struct {
	Model         aihorde.ActiveModel
	Detail        ModelDetail
	IsFeatured    bool
	score         int
	originalIndex int
}

func (s ScoredModel) String() string {
	var sb strings.Builder
	if s.IsFeatured {
		sb.WriteString("⭐ ")
	}
	sb.WriteString(s.Model.Name)
	sb.WriteString(" (")
	sb.WriteString(strconv.Itoa(s.Model.Count))
	sb.WriteString("w, ")
	realETA := s.Model.ETA
	if realETA == 0 && s.Model.Performance > 0.0 {
		realETA = int(s.Model.Queued / s.Model.Performance)
	}
	sb.WriteString(strconv.Itoa(realETA))
	sb.WriteString("s")
	tags := map[string]struct{}{
		s.Detail.Style: {},
	}
	if s.Detail.NSFW {
		tags["nsfw"] = struct{}{}
	}
	if s.Detail.Inpainting {
		tags["inpaint"] = struct{}{}
	}
	for _, tag := range s.Detail.Tags {
		tags[tag] = struct{}{}
	}
	tagSlice := make([]string, 0, len(tags))
	for tag := range tags {
		tagSlice = append(tagSlice, tag)
	}
	sort.Strings(tagSlice)
	for _, tag := range tagSlice {
		sb.WriteString(", ")
		sb.WriteString(tag)
	}
	sb.WriteString(")")
	return sb.String()
}

func (h *Horder) scoreModel(model aihorde.ActiveModel, originalIndex int) ScoredModel {
	scoredModel := ScoredModel{
		Model:         model,
		originalIndex: originalIndex,
	}
	if model.Count == 0 {
		return scoredModel // no workers
	}
	detail, ok := h.modelsData[model.Name]
	if !ok {
		return scoredModel // can't fetch details; unknown model
	}
	scoredModel.Detail = detail
	if detail.Inpainting {
		return scoredModel // we don't support inpainting
	}

	featuredScore := slices.Index(featuredModels, model.Name)
	if featuredScore >= 0 {
		scoredModel.score = len(featuredModels) - featuredScore + 1000000
		scoredModel.IsFeatured = true
		return scoredModel
	}

	score := 1
	if detail.NSFW {
		score += 1 // we can censor nsfw anyway
	}
	score += slices.Index(goodStyles, detail.Style) + 1 // -1 => 0; otherwise style ranking
	score += model.Count                                // more workers = better
	score *= 100                                        // scale up for accurate division
	if model.ETA > 40 {
		score /= 2 // too long
	}

	scoredModel.score = score
	return scoredModel
}

func (h *Horder) ScoreModels() []ScoredModel {
	models, err := h.fetchImageModels()
	if err != nil {
		slog.Error("failed to fetch image models", slog.Any("err", err))
		return nil
	}

	scoredModels := make([]ScoredModel, len(models))
	for i, model := range models {
		scoredModels[i] = h.scoreModel(model, i)
	}

	sort.SliceStable(scoredModels, func(i, j int) bool {
		return scoredModels[i].score > scoredModels[j].score
	})

	return scoredModels
}

func (h *Horder) isAnimeModel(name string) bool {
	if detail, ok := h.modelsData[name]; ok {
		return slices.Contains(goodStyles, detail.Style)
	}
	return false
}

func ptr[T any](value T) *T {
	return &value
}

func (h *Horder) Generate(model, prompt, negative string, steps int, nsfw bool) (string, error) {
	if steps == 0 {
		steps = 20
	}

	var postProcessing []aihorde.ModelGenerationInputPostProcessingType
	if h.isAnimeModel(model) {
		postProcessing = []aihorde.ModelGenerationInputPostProcessingType{
			aihorde.PostProcessingRealESRGANx4plusAnime6B,
		}
	}

	input := aihorde.GenerationInputStable{
		Prompt: prompt,
		Params: &aihorde.ModelGenerationInputStable{
			ModelPayloadRootStable: aihorde.ModelPayloadRootStable{
				ModelPayloadStyleStable: aihorde.ModelPayloadStyleStable{
					SamplerName:    aihorde.SamplerKEulerA,
					PostProcessing: postProcessing,
					CfgScale:       ptr(7.0),
					Width:          ptr(832),
					Height:         ptr(1216),
				},
			},
			Steps: ptr(steps),
			N:     ptr(1),
		},
		Models: []string{
			model,
		},
		NSFW:             ptr(nsfw),
		CensorNSFW:       ptr(nsfw),
		ExtraSlowWorkers: ptr(false),
	}

	req, err := h.horde.PostAsyncImageGenerate(input)
	if err != nil {
		slog.Error("horder: failed to queue image", slog.Any("err", err))
		return "", err
	}

	slog.Info(
		"horder: queued image for generation",
		slog.String("prompt", prompt),
		slog.String("model", model),
		slog.Int("steps", steps),
		slog.Bool("nsfw", nsfw),
		slog.Float64("kudos", req.Kudos),
		slog.String("id", req.ID),
		slog.Any("warnings", req.Warnings),
	)

	return req.ID, nil
}

func (h *Horder) GetStatus(id string) (*aihorde.RequestStatusCheck, error) {
	return h.horde.GetAsyncGenerationCheck(id)
}

func (h *Horder) GetFinalStatus(id string) (*aihorde.RequestStatusStable, error) {
	return h.horde.GetAsyncImageStatus(id)
}

func (h *Horder) Cancel(id string) error {
	_, err := h.horde.DeleteAsyncImageStatus(id)
	return err
}
