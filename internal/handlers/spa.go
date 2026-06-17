package handlers

import (
	"io/fs"
	"mime"
	"net/http"
	"strings"
)

func init() {
	// Go's stdlib MIME table doesn't know .webmanifest; without this the PWA
	// manifest is served as octet-stream and browsers ignore it. Best-effort:
	// a registration failure leaves the default, which is no worse than today.
	_ = mime.AddExtensionType(".webmanifest", "application/manifest+json")
}

// SPAHandler serves the Vite-built SPA. Requests for existing files (hashed
// asset bundles, favicon, etc.) are served directly; everything else falls
// back to index.html so the client-side router can take over.
func SPAHandler(spa fs.FS) http.Handler {
	fileServer := http.FileServer(http.FS(spa))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		clean := strings.TrimPrefix(r.URL.Path, "/")
		if clean == "" {
			serveIndex(w, r, spa)
			return
		}
		if _, err := fs.Stat(spa, clean); err != nil {
			serveIndex(w, r, spa)
			return
		}
		// Long-cache hashed asset bundles; everything else short-cache. The
		// service worker must never be long-cached or the browser won't notice
		// new builds — serve it no-cache so each load can revalidate it (the
		// same contract index.html gets above).
		switch {
		case strings.HasPrefix(r.URL.Path, "/assets/"):
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		case r.URL.Path == "/sw.js" || r.URL.Path == "/registerSW.js":
			w.Header().Set("Cache-Control", "no-cache")
		}
		fileServer.ServeHTTP(w, r)
	})
}

func serveIndex(w http.ResponseWriter, r *http.Request, spa fs.FS) {
	b, err := fs.ReadFile(spa, "index.html")
	if err != nil {
		http.Error(w, "SPA not built — run `npm run build` in web/", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
	_ = r // silence unused-param lint
}
