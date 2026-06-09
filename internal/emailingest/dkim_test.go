package emailingest

import "testing"

const trustedID = "mail.example"

func TestDKIMPass_Simple(t *testing.T) {
	h := []string{"mail.example; dkim=pass header.d=example.com"}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("expected pass for matching domain")
	}
}

func TestDKIMPass_WrongDomain(t *testing.T) {
	h := []string{"mail.example; dkim=pass header.d=other.com"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("expected fail when domain doesn't align")
	}
}

func TestDKIMPass_Fail(t *testing.T) {
	h := []string{"mail.example; dkim=fail header.d=example.com"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("expected fail")
	}
}

func TestDKIMPass_MultipleResults(t *testing.T) {
	// Two A-R headers from the trusted MTA; one passes for the right domain.
	h := []string{
		"mail.example; dkim=fail header.d=other.com",
		"mail.example; dkim=pass header.d=example.com",
	}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("expected pass — at least one header authenticated the right domain")
	}
}

func TestDKIMPass_NoHeaderD(t *testing.T) {
	h := []string{"mail.example; dkim=pass spf=pass"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("expected fail when no header.d is present")
	}
}

func TestDKIMPass_StrictDomainMatch(t *testing.T) {
	// Many forwarders sign with bounces.gmail.com etc. — exact-match only.
	h := []string{"mail.example; dkim=pass header.d=bounces.example.com"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("expected strict match — no subdomain match")
	}
}

func TestDKIMPass_Empty(t *testing.T) {
	if DKIMPass(nil, trustedID, "example.com") {
		t.Error("no A-R headers must not pass")
	}
	if DKIMPass([]string{"mail.example; dkim=pass header.d=example.com"}, trustedID, "") {
		t.Error("empty domain must not pass")
	}
	if DKIMPass([]string{"mail.example; dkim=pass header.d=example.com"}, "", "example.com") {
		t.Error("empty trusted authserv-id must fail closed (no header trusted)")
	}
}

func TestDKIMPass_QuotedDomain(t *testing.T) {
	h := []string{`mail.example; dkim=pass header.d="example.com"`}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("expected pass with quoted header.d value")
	}
}

func TestDKIMPass_IgnoresUntrustedAuthServID(t *testing.T) {
	// A sender-injected header with a different authserv-id must be ignored
	// even though it claims dkim=pass for the sender's own domain.
	h := []string{"attacker.invalid; dkim=pass header.d=example.com"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("a header not stamped by the trusted authserv-id must not be trusted")
	}
	// But the genuine trusted header alongside it still counts.
	h = append(h, "mail.example; dkim=pass header.d=example.com")
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("the trusted MTA's header should still authenticate")
	}
}

func TestFromDomain(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"alice@Example.COM", "example.com"},
		{"a@b.co.uk", "b.co.uk"},
		{"not-an-email", ""},
		{"@nohost", ""},
		{"trailing@", ""},
	}
	for _, c := range cases {
		if got := FromDomain(c.in); got != c.want {
			t.Errorf("FromDomain(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
