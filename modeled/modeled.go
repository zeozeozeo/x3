package modeled

import (
	"database/sql"
	"embed"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"

	_ "github.com/mattn/go-sqlite3"
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
	dbPath     string
}

func NewServer(dbPath string) *Server {
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

	s := &Server{
		httpServer: server,
		dbPath:     dbPath,
	}

	mux.HandleFunc("/api/backup", s.handleBackup)

	return s
}

type backupRequest struct {
	Token string `json:"token"`
}

func (s *Server) handleBackup(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req backupRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf("Error decoding request: %v", err), http.StatusBadRequest)
		return
	}

	expectedToken := os.Getenv("X3_DOWNLOAD_TOKEN")
	if expectedToken == "" || req.Token != expectedToken {
		http.Error(w, "Invalid download token", http.StatusForbidden)
		return
	}

	// Open a temporary database connection to VACUUM into a temp file
	tmpFile := s.dbPath + ".backup.tmp"
	tmpConn, err := sql.Open("sqlite3", s.dbPath)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error opening database for backup: %v", err), http.StatusInternalServerError)
		return
	}
	defer tmpConn.Close()

	// VACUUM INTO writes a cleanly-packed copy to the target path
	_, err = tmpConn.Exec(fmt.Sprintf("VACUUM INTO '%s'", tmpFile))
	if err != nil {
		http.Error(w, fmt.Sprintf("Error vacuuming database: %v", err), http.StatusInternalServerError)
		return
	}
	defer os.Remove(tmpFile)

	f, err := os.Open(tmpFile)
	if err != nil {
		http.Error(w, fmt.Sprintf("Error opening vacuumed database: %v", err), http.StatusInternalServerError)
		return
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		http.Error(w, fmt.Sprintf("Error statting database: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Disposition", "attachment; filename=x3.db")
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))

	io.Copy(w, f)
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
	if err := model.LoadModelsFromJSONData(data); err != nil {
		http.Error(w, fmt.Sprintf("Error reloading models: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Write([]byte(`{"status":"success"}`))
}

func validateConfig(config model.ModelsConfig) error {
	// Basic validation
	if len(config.Models) == 0 {
		return fmt.Errorf("at least one model must be defined")
	}

	// Check for duplicate model names
	names := make(map[string]model.Model)
	for _, m := range config.Models {
		if _, ok := names[m.Name]; ok {
			return fmt.Errorf("duplicate model name: %s", m.Name)
		}
		names[m.Name] = m
	}

	// Validate default models exist
	for _, name := range config.DefaultModels {
		if _, ok := names[name]; !ok {
			return fmt.Errorf("default model not found: %s", name)
		}
	}

	// Validate narrator models exist
	for _, name := range config.NarratorModels {
		if _, ok := names[name]; !ok {
			return fmt.Errorf("narrator model not found: %s", name)
		}
	}

	// Validate vision models exist and have vision capability
	for _, name := range config.DefaultVisionModels {
		m, found := names[name]
		if !found {
			return fmt.Errorf("vision model not found: %s", name)
		}
		if !m.Vision {
			return fmt.Errorf("vision model %s does not have vision capability", name)
		}
	}

	for _, name := range config.SiteModels {
		if _, ok := names[name]; !ok {
			return fmt.Errorf("site model not found: %s", name)
		}
	}

	for _, m := range config.Models {
		if m.FallbackVisionModel == "" {
			continue
		}
		if m.FallbackVisionModel == "Default" {
			if len(config.DefaultVisionModels) == 0 {
				return fmt.Errorf("fallback vision model for %s is Default, but no default vision models are configured", m.Name)
			}
			continue
		}
		fallback, found := names[m.FallbackVisionModel]
		if !found {
			return fmt.Errorf("fallback vision model for %s not found: %s", m.Name, m.FallbackVisionModel)
		}
		if !fallback.Vision {
			return fmt.Errorf("fallback vision model %s for %s does not have vision capability", m.FallbackVisionModel, m.Name)
		}
	}

	return nil
}
