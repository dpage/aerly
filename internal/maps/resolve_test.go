package maps

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newTestResolver returns a resolver whose allowlist also trusts the given
// httptest host, so a fake short link can be exercised without real network.
func newTestResolver(extraHost string) *Resolver {
	r := NewResolver()
	r.AllowedHosts = append(r.AllowedHosts, extraHost)
	return r
}

func TestResolveURL_FollowsRedirectToCoords(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		http.Redirect(w, req, "https://www.google.com/maps/place/X/@1,2,17z/data=!3d40.5!4d-70.25", http.StatusFound)
	}))
	defer srv.Close()

	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	lat, lon, ok, err := r.ResolveURL(context.Background(), srv.URL)
	if err != nil || !ok {
		t.Fatalf("ResolveURL: ok=%v err=%v", ok, err)
	}
	if lat != 40.5 || lon != -70.25 {
		t.Fatalf("got (%v,%v), want (40.5,-70.25)", lat, lon)
	}
}

func TestResolveURL_RejectsNonAllowlistedHost(t *testing.T) {
	r := NewResolver()
	_, _, _, err := r.ResolveURL(context.Background(), "https://evil.example.com/maps")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
}

func TestResolveURL_RejectsNonHTTPS(t *testing.T) {
	r := NewResolver()
	_, _, _, err := r.ResolveURL(context.Background(), "http://maps.app.goo.gl/x")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
}

func TestResolveURL_FullURLNeedsNoNetwork(t *testing.T) {
	r := NewResolver()
	r.HTTP = nil
	lat, lon, ok, err := r.ResolveURL(context.Background(),
		"https://www.google.com/maps/place/X/data=!3d12.5!4d-3.5")
	if err != nil || !ok || lat != 12.5 || lon != -3.5 {
		t.Fatalf("got (%v,%v) ok=%v err=%v", lat, lon, ok, err)
	}
}

// TestResolveURL_AcceptsGoogleCoUK checks the UK Google domain (the address bar
// a UK-based traveller pastes from) is on the allowlist and resolves exactly
// like google.com, needing no network call since the URL already carries a
// pinned coordinate.
func TestResolveURL_AcceptsGoogleCoUK(t *testing.T) {
	r := NewResolver()
	r.HTTP = nil
	lat, lon, ok, err := r.ResolveURL(context.Background(),
		"https://www.google.co.uk/maps/place/X/data=!3d51.5!4d-0.14")
	if err != nil || !ok || lat != 51.5 || lon != -0.14 {
		t.Fatalf("got (%v,%v) ok=%v err=%v, want (51.5,-0.14) ok=true err=nil", lat, lon, ok, err)
	}
}

func TestResolveURL_NoCoordsIsNotOK(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, _, ok, err := r.ResolveURL(context.Background(), srv.URL)
	if err != nil || ok {
		t.Fatalf("ok=%v err=%v, want ok=false err=nil", ok, err)
	}
}

// roundTripFunc lets a test stub http.Client.Do without a real network call,
// regardless of the request's target host.
type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TestResolveURLPrefersExactCoords checks that a URL carrying an exact pin
// (the !3d!4d data segment) never falls through to a hint, and needs no
// network round trip to say so.
func TestResolveURLPrefersExactCoords(t *testing.T) {
	r := NewResolver()
	lat, lon, ok, hint, err := r.ResolveURLOrHint(context.Background(),
		"https://www.google.com/maps/place/Test/@51.5,-0.14,15z/data=!3m1!4b1!4m2!3d51.51!4d-0.13")
	if err != nil || !ok {
		t.Fatalf("want exact coords: ok=%v err=%v", ok, err)
	}
	if lat != 51.51 || lon != -0.13 {
		t.Errorf("want the pinned place, got %v,%v", lat, lon)
	}
	if hint != "" {
		t.Errorf("exact coords need no hint, got %q", hint)
	}
}

// TestResolveURLReturnsHintWhenNoCoords checks the iOS Share-link shape: a
// terminal URL (no redirect) whose q= carries a place name and no coordinate.
// The transport is stubbed so the "no redirects" terminal response never makes
// a real network call.
func TestResolveURLReturnsHintWhenNoCoords(t *testing.T) {
	r := NewResolver()
	r.HTTP = &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody, Header: make(http.Header)}, nil
	})}
	lat, lon, ok, hint, err := r.ResolveURLOrHint(context.Background(),
		"https://www.google.com/maps/search/?api=1&q=Test+Hotel%2C+London&ftid=0x1:0x2")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("must not invent coordinates: got %v,%v", lat, lon)
	}
	if hint != "Test Hotel, London" {
		t.Errorf("want the q text as a hint, got %q", hint)
	}
}

// TestResolveURLOrHint_FollowsRedirectToHint checks that the hop loop still
// follows a redirect (as ResolveURL does) and only extracts a hint from the
// terminal URL, not an intermediate one.
func TestResolveURLOrHint_FollowsRedirectToHint(t *testing.T) {
	var hits int
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		hits++
		if hits == 1 {
			http.Redirect(w, req, "/maps/place/Test+Hotel", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	lat, lon, ok, hint, err := r.ResolveURLOrHint(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ok {
		t.Fatalf("must not invent coordinates: got %v,%v", lat, lon)
	}
	if hint != "Test Hotel" {
		t.Errorf("want the hint from the terminal URL, got %q", hint)
	}
}

// TestResolveURLOrHint_NoHintNoCoords checks the plain no-lead case: a
// terminal URL with neither coordinates nor a q=/place name degrades to
// ok=false, hint="", exactly like ResolveURL's own miss case.
func TestResolveURLOrHint_NoHintNoCoords(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	_, _, ok, hint, err := r.ResolveURLOrHint(context.Background(), srv.URL)
	if err != nil || ok || hint != "" {
		t.Fatalf("ok=%v hint=%q err=%v, want ok=false hint=\"\" err=nil", ok, hint, err)
	}
}

// TestResolveURLOrHint_RejectsNonAllowlistedHost checks the SSRF guard still
// applies to the hint-aware entry point.
func TestResolveURLOrHint_RejectsNonAllowlistedHost(t *testing.T) {
	r := NewResolver()
	_, _, _, _, err := r.ResolveURLOrHint(context.Background(), "https://evil.example.com/maps")
	if !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
}
