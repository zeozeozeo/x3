package imagedesc

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"log/slog"
	"strings"
	"time"

	"github.com/zeozeozeo/x3/llm"
	"github.com/zeozeozeo/x3/persona"
)

func init() {
	llm.SetImageDescriptionCallback(GenerateImageDescription)
}

var DB *sql.DB

var imageDescriptionCache = make(map[string]string)

// DescriptionKey hashes the image URI so multi-megabyte base64 data URIs
// never become database keys. The db package's image_descriptions migration
// uses the same function.
func DescriptionKey(imageURL string) string {
	sum := sha256.Sum256([]byte(imageURL))
	return hex.EncodeToString(sum[:])
}

// GenerateImageDescription generates a detailed description of an image using a vision model
func GenerateImageDescription(imageURL string, ctx context.Context) (string, error) {
	key := DescriptionKey(imageURL)
	if desc, ok := imageDescriptionCache[key]; ok {
		return desc, nil
	}

	if description, found := getImageDescription(key); found {
		slog.Debug("using cached image description from DB", "url", llm.TruncateForPrompt(imageURL))
		imageDescriptionCache[key] = description
		return description, nil
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

	imageDescriptionCache[key] = description

	if err := saveImageDescription(key, description); err != nil {
		slog.Warn("failed to cache image description", "err", err, "url", imageURL)
	}

	slog.Info("successfully generated image description", "url", llm.TruncateForPrompt(imageURL), "length", len(description))
	return description, nil
}

// getImageDescription retrieves the cached description for an image key
func getImageDescription(key string) (string, bool) {
	if DB == nil {
		return "", false
	}

	var description string
	err := DB.QueryRow("SELECT description FROM image_descriptions WHERE image_hash = ?", key).Scan(&description)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Warn("failed to get image description from DB", "err", err)
		}
		return "", false
	}
	return description, true
}

// saveImageDescription stores a generated description for an image key
func saveImageDescription(key, description string) error {
	if DB == nil {
		return nil
	}

	_, err := DB.Exec("INSERT OR REPLACE INTO image_descriptions (image_hash, description) VALUES (?, ?)", key, description)
	if err != nil {
		slog.Error("failed to save image description", "err", err)
	}
	return err
}
