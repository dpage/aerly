package maps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestNewResolverDoesNotAutoFollowRedirects(t *testing.T) {
	r := NewResolver()
	// The production client must refuse to auto-follow: we follow manually so
	// each hop can be host-validated.
	if err := r.HTTP.CheckRedirect(nil, nil); !errors.Is(err, http.ErrUseLastResponse) {
		t.Errorf("CheckRedirect = %v, want http.ErrUseLastResponse", err)
	}
}

func TestResolveURL_ParseError(t *testing.T) {
	r := NewResolver()
	_, _, _, err := r.ResolveURL(context.Background(), "https://goo.gl/%zz")
	if err == nil || !strings.Contains(err.Error(), "parse url") {
		t.Fatalf("err = %v, want a parse-url error", err)
	}
}

func TestResolveURL_TransportError(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	url := srv.URL
	srv.Close() // close before the request so the GET fails to connect

	_, _, ok, err := r.ResolveURL(context.Background(), url)
	if err == nil || ok {
		t.Fatalf("ok=%v err=%v, want a transport error", ok, err)
	}
}

func TestResolveURL_BadRedirectLocation(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		// A Location that fails to parse as a URL reference.
		w.Header().Set("Location", "https://%zz")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()
	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, _, ok, err := r.ResolveURL(context.Background(), srv.URL)
	if err == nil || ok {
		t.Fatalf("ok=%v err=%v, want a redirect-parse error", ok, err)
	}
}

func TestResolveURL_TooManyRedirects(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		// Always redirect onwards (to a coord-free path on the same allowlisted
		// host) so the hop limit is what stops the loop.
		http.Redirect(w, req, "/loop", http.StatusFound)
	}))
	defer srv.Close()
	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, _, _, err := r.ResolveURL(context.Background(), srv.URL)
	if err == nil || !strings.Contains(err.Error(), "too many redirects") {
		t.Fatalf("err = %v, want a too-many-redirects error", err)
	}
}
