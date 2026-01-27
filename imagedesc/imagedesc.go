package imagedesc

import (
	"context"
	"database/sql"
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

// GenerateImageDescription generates a detailed description of an image using a vision model
func GenerateImageDescription(imageURL string, ctx context.Context) (string, error) {
	if desc, ok := imageDescriptionCache[imageURL]; ok {
		return desc, nil
	}

	if description, found := getImageDescription(imageURL); found {
		slog.Debug("using cached image description from DB", "url", imageURL)
		imageDescriptionCache[imageURL] = description
		return description, nil
	}

	slog.Info("generating image description", "url", imageURL)

	llmer := llm.NewLlmer(0)

	meta := persona.PersonaImageDescription
	p := persona.GetPersonaByMeta(meta, nil, "", false, time.Time{}, nil)
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
		slog.Error("failed to generate image description", "err", err, "url", imageURL)
		return "", err
	}

	description = strings.TrimSpace(description)
	if description == "" {
		slog.Warn("generated empty description for image", "url", imageURL)
		return "", nil
	}

	imageDescriptionCache[imageURL] = description

	if err := saveImageDescription(imageURL, description); err != nil {
		slog.Warn("failed to cache image description", "err", err, "url", imageURL)
	}

	slog.Info("successfully generated image description", "url", imageURL, "length", len(description))
	return description, nil
}

// getImageDescription retrieves the cached description for an image URL
func getImageDescription(imageURL string) (string, bool) {
	if DB == nil {
		return "", false
	}

	var description string
	err := DB.QueryRow("SELECT description FROM image_descriptions WHERE image_url = ?", imageURL).Scan(&description)
	if err != nil {
		if err != sql.ErrNoRows {
			slog.Warn("failed to get image description from DB", "err", err, "url", imageURL)
		}
		return "", false
	}
	return description, true
}

// saveImageDescription stores a generated description for an image URL
func saveImageDescription(imageURL, description string) error {
	if DB == nil {
		return nil
	}

	_, err := DB.Exec("INSERT OR REPLACE INTO image_descriptions (image_url, description) VALUES (?, ?)", imageURL, description)
	if err != nil {
		slog.Error("failed to save image description to DB", "err", err, "url", imageURL)
	}
	return err
}
