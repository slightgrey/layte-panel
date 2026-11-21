package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
)

// errorBody is the JSON error shape for every API error:
//
//	{ "error": { "code": "...", "message": "..." } }
type errorBody struct {
	Error apiError `json:"error"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, errorBody{Error: apiError{Code: code, Message: message}})
}

// decodeJSON reads a small JSON body into dst and rejects unknown fields.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		msg := "invalid JSON body"
		var mbe *http.MaxBytesError
		if errors.As(err, &mbe) {
			msg = "request body too large"
		} else if errors.Is(err, io.EOF) {
			msg = "request body is empty"
		}
		writeError(w, http.StatusBadRequest, "bad_request", msg)
		return false
	}
	return true
}
