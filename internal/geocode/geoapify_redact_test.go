package geocode

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The geocode request carries the API key as a query parameter and the address
// being looked up as another, so a transport error quoting the whole URL puts a
// live credential and somebody's address in the journal. Every timeout did.
func TestRedactQueryStripsKeyAndAddress(t *testing.T) {
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
	if !errors.Is(got, context.DeadlineExceeded) {
		t.Errorf("redaction lost the wrapped cause: %v", got)
	}
	if !strings.Contains(raw.URL, "sekrit-live-key") {
		t.Error("redactQuery mutated the error it was given")
	}
}

func TestRedactQueryPassesOtherErrorsThrough(t *testing.T) {
	sentinel := errors.New("dial tcp: no route to host")
	if got := redactQuery(sentinel); !errors.Is(got, sentinel) {
		t.Errorf("a non-URL error should be returned unchanged, got %v", got)
	}
}

// End to end over the geocoding call that actually reached the journal.
func TestGeocodeTransportErrorCarriesNoKey(t *testing.T) {
	g := NewGeoapify("sekrit-live-key")
	// Port 1 on loopback refuses immediately, so the failure is in transport
	// rather than in a response, which is the case that yields a *url.Error.
	g.BaseURL = "http://127.0.0.1:1"
	g.HTTP = &http.Client{Timeout: 2 * time.Second}

	_, _, _, err := g.Geocode(context.Background(), "12 Acacia Avenue", "")
	if err == nil {
		t.Fatal("expected a transport error against a refused port")
	}
	for _, leaked := range []string{"sekrit-live-key", "Acacia"} {
		if strings.Contains(err.Error(), leaked) {
			t.Fatalf("%q reached the error text: %s", leaked, err)
		}
	}
}
