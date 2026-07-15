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
	// The genuine trusted header, stamped on top by our boundary MTA, still
	// authenticates even with a foreign-id header injected below it.
	h = []string{
		"mail.example; dkim=pass header.d=example.com",
		"attacker.invalid; dkim=pass header.d=example.com",
	}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("the trusted MTA's topmost header should authenticate")
	}
}

func TestDKIMPass_IgnoresTrustedIDBelowForeignHeader(t *testing.T) {
	// The boundary MTA prepends its Authentication-Results at the very top,
	// above anything the sender supplied, so only the leading run of
	// trusted-authserv-id headers counts. A header bearing our authserv-id that
	// sits BELOW a foreign-id header can only have been injected by the sender
	// (our MTA would have prepended its own result above, not stranded one
	// beneath a foreign stamp), so it must not be trusted.
	h := []string{
		"attacker.invalid; dkim=pass header.d=example.com",
		"mail.example; dkim=pass header.d=example.com",
	}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("a trusted-id header below a foreign-id header must be ignored as sender-injected")
	}
}

func TestDKIMPass_IgnoresInjectedTrustedHeaderBelowGenuineFail(t *testing.T) {
	// Realistic spoof attempt: our boundary MTA stamps a genuine dkim=fail on
	// top; the sender pre-inserted a forged dkim=pass below it but separated by
	// their own foreign-id A-R header. Only the genuine leading run is honoured,
	// so the forged pass is never reached.
	h := []string{
		"mail.example; dkim=fail header.d=example.com",
		"attacker.invalid; whatever",
		"mail.example; dkim=pass header.d=example.com",
	}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("a forged trusted-id pass below a foreign-id header must not override the genuine fail")
	}
}

func TestDKIMPass_NoTokenSmuggling(t *testing.T) {
	// A real dkim=fail must not be overridden by "dkim=pass" smuggled into
	// another field's value (here the agent/identity field).
	h := []string{"mail.example; dkim=fail header.d=example.com header.i=foo+dkim=pass@example.com"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("dkim=pass smuggled into a field value must not authenticate")
	}
}

func TestDKIMPass_ClauseCorrelation(t *testing.T) {
	// A single genuine boundary-MTA header can carry several dkim results
	// (';'-separated resinfo clauses) when a message bundles multiple
	// signatures. The check must bind dkim=pass to the header.d= in its OWN
	// clause — matching the pass and the domain independently would let an
	// attacker spoof From: victim by ordering a failing victim.com signature
	// before a passing attacker-controlled one.
	h := []string{"mail.example; dkim=fail header.d=example.com; dkim=pass header.d=attacker.com"}
	if DKIMPass(h, trustedID, "example.com") {
		t.Error("a pass for attacker.com must not authenticate example.com whose own result failed")
	}
	// The attacker's own domain, which genuinely passed, still authenticates —
	// but only for itself.
	if !DKIMPass(h, trustedID, "attacker.com") {
		t.Error("the clause that genuinely passed should authenticate its own domain")
	}
	// pass + aligned header.d in the same clause still authenticates even when a
	// different, failing clause precedes it.
	h = []string{"mail.example; dkim=fail header.d=other.com; dkim=pass header.d=example.com"}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("a pass clause aligned to the domain must authenticate")
	}
}

func TestDKIMPass_PassClauseWithComment(t *testing.T) {
	// A verifier may annotate the result with a parenthesised comment before the
	// properties; the leading dkim= token is still the method-result.
	h := []string{"mail.example; dkim=pass (good signature) header.d=example.com"}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("expected pass despite a parenthesised comment after the result")
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
