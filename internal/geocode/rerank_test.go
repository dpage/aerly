package geocode

import (
	"context"
	"errors"
	"strings"
	"testing"
)

var testCands = []Candidate{
	{Formatted: "Test Hotel, London", Lat: 51.5, Lon: -0.14},
	{Formatted: "Test Hotel, Birmingham", Lat: 52.4, Lon: -1.9},
}

func rerankerReturning(s string, err error) *LLMReranker {
	return NewLLMReranker(func(ctx context.Context, prompt string) (string, error) { return s, err })
}

func TestRerankPicksIndex(t *testing.T) {
	r := rerankerReturning(`{"index": 1}`, nil)
	idx, ok, err := r.Pick(context.Background(), "Test Hotel, Birmingham B1", testCands)
	if err != nil || !ok || idx != 1 {
		t.Fatalf("got idx=%d ok=%v err=%v", idx, ok, err)
	}
}

func TestRerankAcceptsFencedJSON(t *testing.T) {
	r := rerankerReturning("```json\n{\"index\": 0}\n```", nil)
	if idx, ok, _ := r.Pick(context.Background(), "x", testCands); !ok || idx != 0 {
		t.Fatalf("a fenced reply must still parse: idx=%d ok=%v", idx, ok)
	}
}

// Every failure mode below must resolve to "none of these" rather than an
// arbitrary pick. A re-ranker that guesses is worse than no re-ranker.
func TestRerankDeclines(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		err  error
	}{
		{"explicit null", `{"index": null}`, nil},
		{"out of range high", `{"index": 9}`, nil},
		{"out of range negative", `{"index": -1}`, nil},
		{"malformed json", `not json at all`, nil},
		{"empty reply", ``, nil},
		{"wrong type", `{"index": "one"}`, nil},
		{"llm error", ``, errors.New("quota exhausted")},
	} {
		t.Run(tt.name, func(t *testing.T) {
			r := rerankerReturning(tt.body, tt.err)
			_, ok, err := r.Pick(context.Background(), "x", testCands)
			if ok {
				t.Fatal("must decline, not guess")
			}
			if err != nil {
				t.Fatalf("failures must degrade quietly, got err=%v", err)
			}
		})
	}
}

func TestRerankNoCandidates(t *testing.T) {
	called := false
	r := NewLLMReranker(func(ctx context.Context, p string) (string, error) {
		called = true
		return `{"index":0}`, nil
	})
	if _, ok, _ := r.Pick(context.Background(), "x", nil); ok {
		t.Fatal("no candidates cannot yield a pick")
	}
	if called {
		t.Fatal("must not call the LLM with an empty candidate list")
	}
}

func TestRerankPromptCarriesCandidatesAndText(t *testing.T) {
	var got string
	r := NewLLMReranker(func(ctx context.Context, p string) (string, error) {
		got = p
		return `{"index":0}`, nil
	})
	_, _, _ = r.Pick(context.Background(), "Test Hotel near Example Street", testCands)
	for _, want := range []string{"Test Hotel near Example Street", "Test Hotel, London", "Test Hotel, Birmingham", "0:", "1:"} {
		if !strings.Contains(got, want) {
			t.Errorf("prompt missing %q:\n%s", want, got)
		}
	}
}
