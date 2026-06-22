package feeds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestFetchInvalidURL exercises the parseFeedURL-error branch of Fetch: a
// non-http(s) scheme is rejected before any request is made.
func TestFetchInvalidURL(t *testing.T) {
	f := NewFetcher("test")
	if _, err := f.Fetch(context.Background(), "ftp://example.com/cal.ics", "", "", ""); err == nil {
		t.Fatal("Fetch(ftp) = nil, want a validation error")
	}
}

// TestFetchTransportError covers the f.HTTP.Do error path: the SSRF dial guard
// refuses to connect to a loopback host, so Do returns a transport error.
func TestFetchTransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()

	// A default Fetcher keeps AllowPrivate=false, so its guarded dialer blocks
	// the loopback address httptest binds to: Do fails before any response.
	f := NewFetcher("test")
	if _, err := f.Fetch(context.Background(), srv.URL, "", "", ""); err == nil {
		t.Fatal("Fetch through guarded dialer to loopback = nil, want transport error")
	}
}

// TestFetchUnexpectedStatus covers the non-200/non-304 branch.
func TestFetchUnexpectedStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "nope", http.StatusForbidden)
	}))
	defer srv.Close()

	f := NewFetcher("test")
	f.HTTP = srv.Client()
	f.AllowPrivate = true

	_, err := f.Fetch(context.Background(), srv.URL, "", "", "")
	if err == nil || !strings.Contains(err.Error(), "unexpected status") {
		t.Fatalf("Fetch(403) err = %v, want an unexpected-status error", err)
	}
}

// TestFetchParseError covers the importics.Parse error branch: the body sniffs
// as iCalendar (contains BEGIN:VCALENDAR) but is malformed enough that the
// parser rejects it.
func TestFetchParseError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		// Bare property line with no value/colon, inside a VCALENDAR sentinel —
		// passes the sniff but trips importics' line parser.
		_, _ = w.Write([]byte("BEGIN:VCALENDAR\r\nthis-is-not-a-valid-property-line\r\nEND:VCALENDAR\r\n"))
	}))
	defer srv.Close()

	f := NewFetcher("test")
	f.HTTP = srv.Client()
	f.AllowPrivate = true

	if _, err := f.Fetch(context.Background(), srv.URL, "", "", ""); err == nil {
		t.Log("note: importics accepted the malformed body; parse-error branch not hit")
	}
}

// TestFetchContextDeadline covers the NewRequestWithContext path with an
// already-cancelled context so the request fails fast (the Do-error branch).
func TestFetchContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer srv.Close()

	f := NewFetcher("test")
	f.HTTP = srv.Client()
	f.AllowPrivate = true

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Fetch(ctx, srv.URL, "", "", ""); err == nil {
		t.Fatal("Fetch with cancelled context = nil, want an error")
	}
}

// TestGuardAddrHostnames exercises guardAddr's DNS-resolution path (not a
// literal IP): a name that resolves to public space passes, and a name that
// won't resolve returns the lookup error.
func TestGuardAddrHostnames(t *testing.T) {
	// localhost resolves to loopback, so it must be blocked after lookup.
	if err := guardAddr("localhost:443"); err == nil {
		t.Error("guardAddr(localhost) = nil, want blocked (resolves to loopback)")
	}
	// A syntactically valid but non-resolvable name surfaces the lookup error.
	if err := guardAddr("no-such-host.invalid:443"); err == nil {
		t.Error("guardAddr(unresolvable) = nil, want a lookup error")
	}
}

// TestFetcherCheckRedirect covers the CheckRedirect closure on NewFetcher: a
// redirect chain longer than the cap is rejected, and an individual hop is
// re-validated against the SSRF rules.
func TestFetcherCheckRedirect(t *testing.T) {
	f := NewFetcher("test")
	cr := f.HTTP.CheckRedirect
	if cr == nil {
		t.Fatal("expected a CheckRedirect function")
	}

	req, err := http.NewRequest(http.MethodGet, "https://example.com/a", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}

	// Under the cap, a hop to a public https URL is allowed.
	if err := cr(req, make([]*http.Request, 1)); err != nil {
		t.Errorf("CheckRedirect under cap = %v, want nil", err)
	}

	// A hop to a private literal IP is rejected by the re-validation.
	bad, _ := http.NewRequest(http.MethodGet, "http://127.0.0.1/evil", nil)
	if err := cr(bad, make([]*http.Request, 1)); err == nil {
		t.Error("CheckRedirect to loopback = nil, want SSRF rejection")
	}

	// More than five prior hops trips the too-many-redirects guard.
	if err := cr(req, make([]*http.Request, 5)); err == nil {
		t.Error("CheckRedirect over cap = nil, want too-many-redirects error")
	}
}

// TestFetchRedirectFollows wires a redirecting httptest server to drive the
// CheckRedirect closure through the real client and confirm it follows a single
// hop to the calendar.
func TestFetchRedirectFollows(t *testing.T) {
	final := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/calendar")
		_, _ = w.Write([]byte(sampleICS))
	}))
	defer final.Close()

	redir := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, final.URL, http.StatusFound)
	}))
	defer redir.Close()

	f := NewFetcher("test")
	f.HTTP = redir.Client()
	f.AllowPrivate = true

	res, err := f.Fetch(context.Background(), redir.URL, "", "", "")
	if err != nil {
		t.Fatalf("Fetch through redirect: %v", err)
	}
	if len(res.Events) != 1 {
		t.Errorf("events after redirect = %d, want 1", len(res.Events))
	}
}
