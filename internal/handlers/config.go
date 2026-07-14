package handlers

import "net/http"

// CapabilitiesDTO exposes server-side feature flags the frontend uses to
// decide which UI affordances to show — most notably whether a Resolver is
// wired up, which lets the Add Flight dialog drop to its minimal "ident +
// date" form. PollIntervalSec is the configured poll cadence in seconds,
// used by the UI to show a "next update in N seconds" countdown.
// EmailIngestEnabled gates the "Email addresses" menu entry.
// EmailIngestAddress is the forwarding address users can mail itineraries
// to; empty (and omitted from JSON) when email ingest is disabled.
type CapabilitiesDTO struct {
	ResolverAvailable  bool   `json:"resolver_available"`
	PollIntervalSec    int    `json:"poll_interval_sec"`
	EmailIngestEnabled bool   `json:"email_ingest_enabled"`
	EmailIngestAddress string `json:"email_ingest_address,omitempty"`
	// AttachmentsEnabled gates the per-plan attachments UI (issue #91).
	// AttachmentsMaxBytes is the per-file upload cap, so the client can reject
	// oversize files before sending; omitted when attachments are disabled.
	AttachmentsEnabled  bool  `json:"attachments_enabled"`
	AttachmentsMaxBytes int64 `json:"attachments_max_bytes,omitempty"`
	// ExploreEnabled gates the Explore feature (its trip tab, the "Explore
	// nearby" button, and the preference to hide it). False when no POI
	// resolver is configured, i.e. GEOAPIFY_API_KEY is unset.
	ExploreEnabled bool `json:"explore_enabled"`
}

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	_ = r
	out := CapabilitiesDTO{}
	if a.Config != nil {
		out.ResolverAvailable = a.Config.ResolverAvailable()
		out.PollIntervalSec = int(a.Config.PollInterval.Seconds())
		out.EmailIngestEnabled = a.Config.EmailIngestEnabled
		if a.Config.EmailIngestEnabled {
			out.EmailIngestAddress = a.Config.EmailIngestAddress
		}
		out.AttachmentsEnabled = a.Config.AttachmentsEnabled()
		if a.Config.AttachmentsEnabled() {
			out.AttachmentsMaxBytes = a.Config.AttachmentsMaxBytes
		}
	}
	// Explore is available exactly when a POI resolver is wired (Geoapify keyed).
	out.ExploreEnabled = a.POIs != nil
	writeJSON(w, http.StatusOK, out)
}
