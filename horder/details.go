package horder

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

const sdiffModelDetailsUrl = "https://raw.githubusercontent.com/Haidra-Org/AI-Horde-image-model-reference/refs/heads/main/stable_diffusion.json"

// ModelsData represents the top-level structure of the JSON,
// which is a map where keys are model names and values are ModelDetail.
type ModelsData map[string]ModelDetail

// ModelDetail represents the detailed information for a single AI model.
type ModelDetail struct {
	Name                 string             `json:"name"`
	Baseline             string             `json:"baseline"`
	Type                 string             `json:"type"`
	Inpainting           bool               `json:"inpainting"`
	Description          string             `json:"description"`
	Tags                 []string           `json:"tags,omitempty"` // Optional field
	Showcases            []string           `json:"showcases"`
	Version              string             `json:"version"`
	Style                string             `json:"style"`
	Trigger              []string           `json:"trigger,omitempty"`  // Optional field
	Homepage             string             `json:"homepage,omitempty"` // Optional field
	NSFW                 bool               `json:"nsfw"`
	DownloadAll          bool               `json:"download_all"`
	Config               ModelConfig        `json:"config"`
	Available            bool               `json:"available"`
	SizeOnDiskBytes      int64              `json:"size_on_disk_bytes"`               // Use int64 for large numbers
	Optimization         string             `json:"optimization,omitempty"`           // Optional field (e.g., for DreamShaper XL)
	Requirements         *ModelRequirements `json:"requirements,omitempty"`           // Optional field, pointer for nil if missing
	FeaturesNotSupported []string           `json:"features_not_supported,omitempty"` // Optional field (e.g., for SDXL)
}

// ModelConfig contains configuration details like files and download links.
type ModelConfig struct {
	Files    []ModelFile     `json:"files"`
	Download []ModelDownload `json:"download"`
}

// ModelFile represents a file associated with the model, including hashes.
type ModelFile struct {
	Path      string `json:"path"`
	MD5Sum    string `json:"md5sum,omitempty"`    // Optional field
	SHA256Sum string `json:"sha256sum,omitempty"` // Optional field
	FileType  string `json:"file_type,omitempty"` // Optional field (e.g., for Stable Cascade)
}

// ModelDownload represents a download link for a model file.
type ModelDownload struct {
	FileName string `json:"file_name"`
	FilePath string `json:"file_path"`
	FileURL  string `json:"file_url"`
}

// ModelRequirements defines specific requirements or constraints for using the model.
type ModelRequirements struct {
	ClipSkip    int      `json:"clip_skip,omitempty"`
	MinSteps    int      `json:"min_steps,omitempty"`
	MaxSteps    int      `json:"max_steps,omitempty"`
	CfgScale    float64  `json:"cfg_scale,omitempty"`
	MinCfgScale float64  `json:"min_cfg_scale,omitempty"`
	MaxCfgScale float64  `json:"max_cfg_scale,omitempty"`
	Samplers    []string `json:"samplers,omitempty"`
	Schedulers  []string `json:"schedulers,omitempty"`
}

func FetchModelDetails() (ModelsData, error) {
	slog.Info("FetchModelDetails: fetching model details...", "url", sdiffModelDetailsUrl)
	start := time.Now()
	resp, err := http.Get(sdiffModelDetailsUrl)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch URL %s: %w", sdiffModelDetailsUrl, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to fetch URL %s: status code %d", sdiffModelDetailsUrl, resp.StatusCode)
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %w", err)
	}

	var models ModelsData
	err = json.Unmarshal(bodyBytes, &models)
	if err != nil {
		return nil, fmt.Errorf("failed to unmarshal JSON: %w", err)
	}

	slog.Info("FetchModelDetails: fetched model details", slog.Int("count", len(models)), "in", time.Since(start))

	return models, nil
}

func MustFetchModelDetails() ModelsData {
	models, err := FetchModelDetails()
	if err != nil {
		panic(err)
	}
	return models
}
