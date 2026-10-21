package openaiapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/zeozeozeo/x3/openai"
)

// writeJSON writes a JSON response.
func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}

// writeUpstreamError translates an upstream failure into an OpenAI error
// response.
func writeUpstreamError(w http.ResponseWriter, err error) {
	if err == nil {
		writeError(w, http.StatusBadGateway, errTypeAPI, "upstream_error", "The upstream provider failed.")
		return
	}

	var apiErr *openai.APIError
	if errors.As(err, &apiErr) {
		status := apiErr.HTTPStatusCode
		if status < 400 || status > 599 {
			status = http.StatusBadGateway
		}
		errType := apiErr.Type
		if errType == "" {
			errType = errTypeAPI
		}
		code := apiErr.Code
		if code == nil {
			code = "upstream_error"
		}
		writeError(w, status, errType, codeString(code), apiErr.Message)
		return
	}

	switch {
	case errors.Is(err, context.DeadlineExceeded):
		writeError(w, http.StatusGatewayTimeout, errTypeAPI, "timeout",
			"The upstream provider did not answer in time.")
	case errors.Is(err, context.Canceled):
		writeError(w, http.StatusRequestTimeout, errTypeAPI, "canceled",
			"The request was canceled before the upstream provider answered.")
	default:
		writeError(w, http.StatusBadGateway, errTypeAPI, "upstream_error", err.Error())
	}
}

// codeString converts an error code of any type into a string.
func codeString(code any) string {
	switch v := code.(type) {
	case nil:
		return ""
	case string:
		return v
	default:
		return "upstream_error"
	}
}
