package imagedesc

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/cache"
	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
)

func init() {
	llm.SetImageDescriptionCallback(GenerateImageDescription)
}

// DescriptionKey hashes the image URI so multi-megabyte base64 data URIs
// never become cache keys.
func DescriptionKey(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	return hex.EncodeToString(sum[:])
}

// GenerateImageDescription generates a detailed description of an image using a vision model
func GenerateImageDescription(imageURL string, ctx context.Context) (string, error) {
	key := DescriptionKey(imageURL)
	if data, ok, err := cache.Get(cache.NamespaceImageDescription, key); err != nil {
		slog.Warn("failed to read image description from cache", "err", err, "url", llm.TruncateForPrompt(imageURL))
	} else if ok {
		return string(data), nil
	}

	slog.Info("generating image description", "url", llm.TruncateForPrompt(imageURL))

	llmer := llm.NewLlmer(0)

	meta := persona.PersonaImageDescription
	p := persona.GetPersonaByMeta(meta, "", false, persona.PromptContext{})
	llmer.SetPersona(p, nil)

	llmer.AddMessage(llm.RoleUser, "Describe this image in detail.", 0)
	llmer.AddImage(imageURL)

	models := meta.GetModels()
	if len(models) == 0 {
		slog.Error("no vision models available for image description")
		return "", llm.ErrNoModelsForCompletion()
	}

	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	description, _, err := llmer.RequestCompletion(models, meta.Settings, "", ctx)
	if err != nil {
		slog.Error("failed to generate image description", "err", err, "url", llm.TruncateForPrompt(imageURL))
		return "", err
	}

	description = strings.TrimSpace(description)
	if description == "" {
		slog.Warn("generated empty description for image", "url", llm.TruncateForPrompt(imageURL))
		return "", nil
	}

	if err := cache.Put(cache.NamespaceImageDescription, key, []byte(description)); err != nil {
		slog.Warn("failed to cache image description", "err", err, "url", imageURL)
	}

	slog.Info("successfully generated image description", "url", llm.TruncateForPrompt(imageURL), "length", len(description))
	return description, nil
}
