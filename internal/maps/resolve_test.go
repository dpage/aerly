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

func TestResolveURL_ReadsCoordsFromTerminalPageBody(t *testing.T) {
	// The place URL carries no coordinates (a feature-ID-only link), so the
	// resolver fetches it and must read the coordinates from the rendered page's
	// canonical link rather than giving up.
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(
			`<link rel="canonical" href="https://www.google.com/maps/place/Caf%C3%A9/@48.8584,2.2945,17z/data=!3d48.8584!4d2.2945">`,
		))
	}))
	defer srv.Close()

	r := newTestResolver(strings.TrimPrefix(srv.URL, "https://"))
	r.HTTP = srv.Client()
	r.HTTP.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }

	lat, lon, ok, err := r.ResolveURL(context.Background(), srv.URL+"/maps/place/Cafe/data=!3m1!1s0x0:0x0")
	if err != nil || !ok {
		t.Fatalf("ResolveURL: ok=%v err=%v", ok, err)
	}
	if lat != 48.8584 || lon != 2.2945 {
		t.Fatalf("got (%v,%v), want (48.8584,2.2945)", lat, lon)
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
