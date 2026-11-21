package api

import (
	"errors"
	"io/fs"
	"net/http"
	"path"
	"strings"
)

// spaHandler serves the embedded React build. Unknown paths fall back to
// index.html so client-side routes (/login, /sites/...) work on reload.
func spaHandler(ui fs.FS) http.Handler {
	if ui == nil {
		return http.HandlerFunc(uiMissing)
	}
	if _, err := fs.Stat(ui, "index.html"); err != nil {
		return http.HandlerFunc(uiMissing)
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
			return
		}
		name := strings.TrimPrefix(path.Clean(r.URL.Path), "/")
		if name == "" {
			name = "index.html"
		}
		fi, err := fs.Stat(ui, name)
		if err != nil || fi.IsDir() {
			if err != nil && !errors.Is(err, fs.ErrNotExist) {
				http.Error(w, "internal error", http.StatusInternalServerError)
				return
			}
			name = "index.html"
		}
		if strings.HasPrefix(name, "assets/") {
			// Vite puts a content hash in asset filenames, so they never change.
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		http.ServeFileFS(w, r, ui, name)
	})
}

func uiMissing(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(http.StatusServiceUnavailable)
	w.Write([]byte("Layte Panel UI is not built into this binary.\nRun `cd web && npm run build`, then rebuild, or use the Vite dev server (npm run dev).\n"))
}
