package modeled

import (
	"embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"

	"github.com/zeozeozeo/x3/model"
)

const (
	modelsFilePath = "models.json"
	Port           = ":6741"
)

//go:embed static/*
var staticFiles embed.FS

//go:embed templates/*
var templateFiles embed.FS

type Server struct {
	httpServer *http.Server
}

func NewServer() *Server {
	mux := http.NewServeMux()

	// Serve static files
	mux.Handle("/static/", http.FileServer(http.FS(staticFiles)))

	// API endpoints
	mux.HandleFunc("/api/models", handleModels)
	mux.HandleFunc("/api/models/save", handleSaveModels)
	mux.HandleFunc("/", handleIndex)

	server := &http.Server{
		Addr:    Port,
		Handler: mux,
	}

	return &Server{
		httpServer: server,
	}
}

func (s *Server) Start() error {
	slog.Info("Starting modeled server", "port", Port)
	return s.httpServer.ListenAndServe()
}

func (s *Server) Stop() error {
	return s.httpServer.Close()
}

func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	htmlContent, err := templateFiles.ReadFile("templates/index.html")
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading template: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html")
	w.Write(htmlContent)
}

func handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	data, err := os.ReadFile(modelsFilePath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error reading models file: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write(data)
}

func handleSaveModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var config model.ModelsConfig
	if err := json.NewDecoder(r.Body).Decode(&config); err != nil {
		http.Error(w, fmt.Sprintf("Error decoding JSON: %v", err), http.StatusBadRequest)
		return
	}

	// Validate the configuration
	if err := validateConfig(config); err != nil {
		http.Error(w, fmt.Sprintf("Invalid configuration: %v", err), http.StatusBadRequest)
		return
	}

	// Write to file
	data, err := json.MarshalIndent(config, "", "  ")
	if err != nil {
		http.Error(w, fmt.Sprintf("Error encoding JSON: %v", err), http.StatusInternalServerError)
		return
	}

	if err := os.WriteFile(modelsFilePath, data, 0644); err != nil {
		http.Error(w, fmt.Sprintf("Error writing file: %v", err), http.StatusInternalServerError)
		return
	}

	// Reload models in the application
	if err := model.LoadModelsFromJSON(); err != nil {
		http.Error(w, fmt.Sprintf("Error reloading models: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status": "success"}`))
}

func validateConfig(config model.ModelsConfig) error {
	// Basic validation
	if len(config.Models) == 0 {
		return fmt.Errorf("at least one model must be defined")
	}

	// Check for duplicate model names
	names := make(map[string]bool)
	for _, m := range config.Models {
		if names[m.Name] {
			return fmt.Errorf("duplicate model name: %s", m.Name)
		}
		names[m.Name] = true
	}

	// Validate default models exist
	for _, name := range config.DefaultModels {
		if !names[name] {
			return fmt.Errorf("default model not found: %s", name)
		}
	}

	// Validate narrator models exist
	for _, name := range config.NarratorModels {
		if !names[name] {
			return fmt.Errorf("narrator model not found: %s", name)
		}
	}

	// Validate vision models exist and have vision capability
	for _, name := range config.DefaultVisionModels {
		found := false
		for _, m := range config.Models {
			if m.Name == name {
				found = true
				if !m.Vision {
					return fmt.Errorf("vision model %s does not have vision capability", name)
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("vision model not found: %s", name)
		}
	}

	return nil
}
