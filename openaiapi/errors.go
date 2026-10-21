package openaiapi

import (
	"encoding/json"
	"net/http"
)

// errorEnvelope is the OpenAI error response shape.
type errorEnvelope struct {
	Error errorBody `json:"error"`
}

type errorBody struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Param   string `json:"param,omitempty"`
	Code    any    `json:"code,omitempty"`
}

const (
	errTypeInvalidRequest = "invalid_request_error"
	errTypeAuthentication = "authentication_error"
	errTypeAPI            = "api_error"
	errTypeNotFound       = "not_found_error"
	errTypeRateLimit      = "rate_limit_error"
)

// writeError writes an OpenAI-compatible error response.
func writeError(w http.ResponseWriter, status int, errType, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(errorEnvelope{
		Error: errorBody{
			Message: message,
			Type:    errType,
			Code:    code,
		},
	})
}
