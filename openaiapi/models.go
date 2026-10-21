package openaiapi

import (
	"net/http"
	"strings"

	"github.com/zeozeozeo/x3/model"
)

type modelList struct {
	Object string       `json:"object"`
	Data   []modelEntry `json:"data"`
}

type modelEntry struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	OwnedBy string `json:"owned_by"`
}

func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errTypeInvalidRequest, "method_not_allowed",
			"Only GET is supported on this endpoint.")
		return
	}

	models := model.ModelsByCommand()
	data := make([]modelEntry, 0, len(models))
	for _, m := range models {
		if !isLLM(m) {
			continue
		}
		data = append(data, s.modelEntry(m))
	}

	writeJSON(w, http.StatusOK, modelList{
		Object: "list",
		Data:   data,
	})
}

func (s *Server) handleModel(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, errTypeInvalidRequest, "method_not_allowed",
			"Only GET is supported on this endpoint.")
		return
	}

	id := strings.Trim(strings.TrimPrefix(r.URL.Path, "/v1/models/"), "/")
	if id == "" {
		s.handleModels(w, r)
		return
	}

	m, ok := model.GetModelByCommand(id)
	if !ok {
		writeError(w, http.StatusNotFound, errTypeNotFound, "model_not_found",
			"The model '"+id+"' does not exist. Use GET /v1/models for the list of available models.")
		return
	}

	writeJSON(w, http.StatusOK, s.modelEntry(m))
}

func (s *Server) modelEntry(m model.Model) modelEntry {
	return modelEntry{
		ID:      m.Command,
		Object:  "model",
		Created: s.created,
		OwnedBy: "x3",
	}
}

// isLLM reports whether the model is an actual language model. The markov,
// eliza and alice models are not served by any provider.
func isLLM(m model.Model) bool {
	return !m.IsMarkov && !m.IsEliza && !m.IsAlice
}
