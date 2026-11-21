package api

import (
	_ "embed"
	"net/http"
)

// The OpenAPI description of the endpoints API keys can use. It's embedded
// so every binary serves the spec that matches its own code.
//
//go:embed openapi.json
var openapiSpec []byte

// GET /api/v1/openapi.json (no auth: it's documentation).
func (s *server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(openapiSpec)
}
