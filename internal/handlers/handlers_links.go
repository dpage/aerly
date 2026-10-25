package handlers

import (
	"fmt"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/dpage/aerly/internal/store"
)

// planLinkReq is one title/URL pair on a plan (issue #109), as the plan create
// and edit requests carry it.
type planLinkReq struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

// Caps on per-plan links. They are generous for real use (a handful of tickets
// and boarding passes) whilst stopping one plan from carrying unbounded rows.
const (
	maxPlanLinks        = 50
	maxPlanLinkTitleLen = 200
	maxPlanLinkURLLen   = 2048
)

// validatePlanLinks trims and checks the links a client sent, returning them in
// store form. Only absolute http(s) URLs are accepted, so a stored link can never
// be a javascript: or data: URL that runs when another trip member taps it. An
// empty title falls back to the URL's host, which is what the user would
// otherwise have typed.
func validatePlanLinks(in []planLinkReq) ([]store.PlanLinkInput, error) {
	if len(in) > maxPlanLinks {
		return nil, fmt.Errorf("A plan can have at most %d links.", maxPlanLinks)
	}
	out := make([]store.PlanLinkInput, 0, len(in))
	for _, l := range in {
		raw := strings.TrimSpace(l.URL)
		if utf8.RuneCountInString(raw) > maxPlanLinkURLLen {
			return nil, fmt.Errorf("Link URLs can be at most %d characters.", maxPlanLinkURLLen)
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("Links must be http:// or https:// URLs.")
		}
		title := strings.TrimSpace(l.Title)
		if title == "" {
			title = u.Hostname()
		}
		if utf8.RuneCountInString(title) > maxPlanLinkTitleLen {
			return nil, fmt.Errorf("Link titles can be at most %d characters.", maxPlanLinkTitleLen)
		}
		out = append(out, store.PlanLinkInput{Title: title, URL: raw})
	}
	return out, nil
}
