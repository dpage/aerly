package handlers

import "net/http"

// contentSecurityPolicy is the CSP sent with every response. It pins the app to
// its own origin plus the specific third-party hosts the SPA genuinely needs,
// so an injected <script> or framed clickjacking attempt has nowhere to load
// from:
//
//   - The map (PlanMapView's STYLE) pulls OpenStreetMap raster tiles and
//     MapLibre demo glyph fonts. MapLibre fetches both from inside a web worker,
//     as images and via fetch, so the tile/glyph hosts appear in img-src AND
//     connect-src, and the worker itself is built from a blob: URL.
//   - Trip cards show a country flag image, but those SVGs are now served
//     same-origin from /flags (bundled out of flag-icons at build time), so no
//     third-party flag host is needed; 'self' in img-src covers them.
//   - User avatars come from the GitHub and Google CDNs. dev-login (a dev-only
//     affordance) uses https://github.com/<login>.png, which 302s to the
//     githubusercontent CDN; the redirect's origin (github.com) must therefore
//     also be allowed for that avatar to load.
//
// MUI/emotion injects <style> elements at runtime, so style-src must allow
// 'unsafe-inline'; there is, however, no inline script (index.html loads a
// module and the service worker registers from src/pwa.ts), so script-src
// stays the strict 'self'. frame-ancestors 'none' (plus the legacy
// X-Frame-Options) blocks framing; object-src 'none' kills plugin vectors.
const contentSecurityPolicy = "default-src 'self'; " +
	"base-uri 'self'; " +
	"object-src 'none'; " +
	"frame-ancestors 'none'; " +
	"form-action 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"img-src 'self' data: blob: https://tile.openstreetmap.org https://github.com https://*.githubusercontent.com https://*.googleusercontent.com; " +
	"font-src 'self' data:; " +
	"worker-src 'self' blob:; " +
	"manifest-src 'self'; " +
	"connect-src 'self' https://tile.openstreetmap.org https://demotiles.maplibre.org"

// hstsValue is two years with subdomains — long enough to be meaningful, and we
// intentionally omit `preload` so operators opt into the HSTS preload list
// deliberately rather than having a default commit them to it.
const hstsValue = "max-age=63072000; includeSubDomains"

// SecurityHeaders wraps h so every response carries a baseline set of hardening
// headers (CSP, nosniff, anti-framing, referrer policy). When https is true it
// also emits Strict-Transport-Security; callers should pass true only for an
// HTTPS deployment (see config.Config.HTTPS), since HSTS over plain HTTP is
// ignored by browsers and risks pinning a later-reused localhost host.
//
// Headers are set before the wrapped handler runs, so they survive even when a
// handler streams (SSE) or writes its own status, while still letting handlers
// add response-specific headers of their own.
func SecurityHeaders(h http.Handler, https bool) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		head := w.Header()
		head.Set("Content-Security-Policy", contentSecurityPolicy)
		head.Set("X-Content-Type-Options", "nosniff")
		head.Set("X-Frame-Options", "DENY")
		head.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		if https {
			head.Set("Strict-Transport-Security", hstsValue)
		}
		h.ServeHTTP(w, r)
	})
}
