package providers

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The API key travels as a query parameter and the search text is somebody's
// address, so a transport error quoting the whole URL puts both in the log.
// Every timeout used to do exactly that.
func TestRedactQueryStripsKeyAndSearchText(t *testing.T) {
	raw := &url.Error{
		Op:  "Get",
		URL: "https://api.geoapify.com/v1/geocode/search?apiKey=sekrit-live-key&text=12+Acacia+Avenue",
		Err: context.DeadlineExceeded,
	}
	got := redactQuery(raw)
	msg := got.Error()
	for _, leaked := range []string{"sekrit-live-key", "apiKey", "Acacia"} {
		if strings.Contains(msg, leaked) {
			t.Errorf("redacted error still contains %q: %s", leaked, msg)
		}
	}
	if !strings.Contains(msg, "api.geoapify.com/v1/geocode/search") {
		t.Errorf("redaction threw away the useful part of the URL: %s", msg)
	}
	// The wrapped cause has to survive, or every timeout check downstream stops
	// recognising a timeout.
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("redaction lost the wrapped cause: %v", got)
	}
	var ue *url.Error
	if !errors.As(got, &ue) || ue.Op != "Get" {
		t.Errorf("redaction lost the *url.Error shape or its Op: %v", got)
	}
	// The original is left alone rather than mutated under the caller.
	if !strings.Contains(raw.URL, "sekrit-live-key") {
		t.Error("redactQuery mutated the error it was given")
	}
}

// An error that isn't a *url.Error carries no URL and is passed straight back.
func TestRedactQueryPassesOtherErrorsThrough(t *testing.T) {
	sentinel := errors.New("dial tcp: no route to host")
	if got := redactQuery(sentinel); !errors.Is(got, sentinel) {
		t.Errorf("a non-URL error should be returned unchanged, got %v", got)
	}
}

// End to end: a request that fails in transport must not surface the key, which
// is the path that actually reached the journal.
func TestGeoapifyTransportErrorCarriesNoKey(t *testing.T) {
	g := NewGeoapify("sekrit-live-key")
	// Port 1 on loopback refuses immediately, so the failure is in transport
	// rather than in a response, which is the case that produces a *url.Error.
	g.BaseURL = "http://127.0.0.1:1/v2/places"
	g.HTTP = &http.Client{Timeout: 2 * time.Second}
	g.Limiter = nil

	_, err := g.Nearby(context.Background(), 48.2, 16.37, 500, []string{"restaurants"})
	if err == nil {
		t.Fatal("expected a transport error against a refused port")
	}
	if strings.Contains(err.Error(), "sekrit-live-key") {
		t.Fatalf("the API key reached the error text: %s", err)
	}
}
