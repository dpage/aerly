package handlers

import "net/http"

// CapabilitiesDTO exposes server-side feature flags the frontend uses to
// decide which UI affordances to show — most notably whether a Resolver is
// wired up, which lets the Add Flight dialog drop to its minimal "ident +
// date" form. PollIntervalSec is the configured poll cadence in seconds,
// used by the UI to show a "next update in N seconds" countdown.
type CapabilitiesDTO struct {
	ResolverAvailable bool `json:"resolver_available"`
	PollIntervalSec   int  `json:"poll_interval_sec"`
}

func (a *API) getConfig(w http.ResponseWriter, r *http.Request) {
	_ = r
	out := CapabilitiesDTO{}
	if a.Config != nil {
		out.ResolverAvailable = a.Config.ResolverAvailable()
		out.PollIntervalSec = int(a.Config.PollInterval.Seconds())
	}
	writeJSON(w, http.StatusOK, out)
}
