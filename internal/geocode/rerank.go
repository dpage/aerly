package geocode

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"
)

// rerankTimeout bounds a single re-rank. Geocoding runs inside a backfill loop,
// so a stalled provider must not hold the loop open.
const rerankTimeout = 20 * time.Second

// Reranker picks the candidate that best matches the original booking text.
// ok=false means "none of these", which is always a permitted answer.
type Reranker interface {
	Pick(ctx context.Context, text string, cands []Candidate) (idx int, ok bool, err error)
}

// CompleterFunc sends a prompt to an LLM and returns its text reply. It mirrors
// the shape of emailingest.RealLLM.Complete without the document argument, so
// this package needn't import emailingest (which would be a cycle) nor care
// which provider is configured.
type CompleterFunc func(ctx context.Context, prompt string) (string, error)

// LLMReranker resolves an ambiguous candidate list by asking an LLM which one
// matches the booking text.
//
// The model chooses an index into a list of real geocoding results; it never
// produces coordinates. That is what keeps this inside the rule that we never
// guess a location: the worst case is picking the wrong real place, not
// inventing one. Every failure (a bad index, unparseable output, a timeout, an
// exhausted quota) degrades to "none of these" and lets the caller fall back to
// its confidence-only decision.
type LLMReranker struct {
	complete CompleterFunc
}

func NewLLMReranker(c CompleterFunc) *LLMReranker { return &LLMReranker{complete: c} }

const rerankPrompt = `You are matching a travel booking to a geocoding result.

The booking says this location is:
%s

Here are the candidate locations a geocoder returned:
%s

Reply with ONLY a JSON object naming the candidate that is the same place as the
booking location: {"index": <number>}

If none of the candidates is clearly the same place, reply {"index": null}.
Declining is always correct when you are unsure: a missing map pin is a small
annoyance, but a pin on the wrong building is a real problem for a traveller.
Do not explain. Do not output coordinates. Reply with the JSON object only.`

func (r *LLMReranker) Pick(ctx context.Context, text string, cands []Candidate) (int, bool, error) {
	if len(cands) == 0 || strings.TrimSpace(text) == "" {
		return 0, false, nil
	}
	list := &strings.Builder{}
	for i, c := range cands {
		fmt.Fprintf(list, "%d: %s\n", i, c.Formatted)
	}
	ctx, cancel := context.WithTimeout(ctx, rerankTimeout)
	defer cancel()

	out, err := r.complete(ctx, fmt.Sprintf(rerankPrompt, text, list.String()))
	if err != nil {
		// A dead or rate-limited LLM must never block geocoding, and must never
		// be treated as a pick. Degrade to the confidence-only decision.
		slog.Warn("geocode re-rank failed, falling back to confidence", "err", err)
		return 0, false, nil
	}
	idx, ok := parseRerankIndex(out, len(cands))
	if !ok {
		slog.Debug("geocode re-rank declined", "reply", truncate(out, 120))
	}
	return idx, ok, nil
}

// parseRerankIndex reads {"index": n} from a model reply, tolerating markdown
// fences and surrounding prose. Anything it can't read, or an index outside the
// candidate list, is a decline rather than a guess.
func parseRerankIndex(out string, n int) (int, bool) {
	s := strings.TrimSpace(out)
	if i := strings.Index(s, "{"); i >= 0 {
		if j := strings.LastIndex(s, "}"); j > i {
			s = s[i : j+1]
		}
	}
	var reply struct {
		Index *int `json:"index"`
	}
	if err := json.Unmarshal([]byte(s), &reply); err != nil || reply.Index == nil {
		return 0, false
	}
	if *reply.Index < 0 || *reply.Index >= n {
		return 0, false
	}
	return *reply.Index, true
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
