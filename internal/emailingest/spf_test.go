package emailingest

import "testing"

func TestSPFPass_Simple(t *testing.T) {
	h := []string{"mail.example; spf=pass smtp.mailfrom=example.com"}
	if !SPFPass(h, trustedID, "example.com") {
		t.Error("expected pass for aligned envelope domain")
	}
}

func TestSPFPass_MailfromAddressForm(t *testing.T) {
	// smtp.mailfrom may carry a full address; the domain part must align.
	h := []string{"mail.example; spf=pass smtp.mailfrom=bounce@example.com"}
	if !SPFPass(h, trustedID, "example.com") {
		t.Error("expected pass extracting domain from a full mailfrom address")
	}
}

func TestSPFPass_HeloFallback(t *testing.T) {
	h := []string{"mail.example; spf=pass smtp.helo=example.com"}
	if !SPFPass(h, trustedID, "example.com") {
		t.Error("expected pass via smtp.helo when smtp.mailfrom is absent")
	}
}

func TestSPFPass_Unaligned(t *testing.T) {
	// A real SPF pass for the attacker's own envelope domain must not vouch for
	// a forged From in a different domain.
	h := []string{"mail.example; spf=pass smtp.mailfrom=attacker.invalid"}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("expected fail when the SPF identity doesn't align with From")
	}
}

func TestSPFPass_NotPass(t *testing.T) {
	for _, res := range []string{"spf=fail", "spf=softfail", "spf=neutral", "spf=none"} {
		h := []string{"mail.example; " + res + " smtp.mailfrom=example.com"}
		if SPFPass(h, trustedID, "example.com") {
			t.Errorf("expected fail for %q", res)
		}
	}
}

func TestSPFPass_NoIdentity(t *testing.T) {
	// spf=pass with no smtp.mailfrom/helo identity can't be aligned — fail.
	h := []string{"mail.example; dkim=pass spf=pass"}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("expected fail when no SPF identity is present to align")
	}
}

func TestSPFPass_NoTokenSmuggling(t *testing.T) {
	// A real spf=fail must not be overridden by "spf=pass" smuggled into the
	// (attacker-controlled) mailfrom local-part.
	h := []string{"mail.example; spf=fail smtp.mailfrom=foo+spf=pass@example.com"}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("spf=pass smuggled in the mailfrom local-part must not authenticate")
	}
}

func TestSPFPass_StrictDomainMatch(t *testing.T) {
	h := []string{"mail.example; spf=pass smtp.mailfrom=bounces.example.com"}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("expected strict match — no subdomain alignment")
	}
}

func TestSPFPass_IgnoresUntrustedAuthServID(t *testing.T) {
	h := []string{"attacker.invalid; spf=pass smtp.mailfrom=example.com"}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("a header not stamped by the trusted authserv-id must not be trusted")
	}
	// The genuine trusted header, stamped on top by our boundary MTA, still
	// authenticates even with a foreign-id header injected below it.
	h = []string{
		"mail.example; spf=pass smtp.mailfrom=example.com",
		"attacker.invalid; spf=pass smtp.mailfrom=example.com",
	}
	if !SPFPass(h, trustedID, "example.com") {
		t.Error("the trusted MTA's topmost header should authenticate")
	}
}

func TestSPFPass_IgnoresTrustedIDBelowForeignHeader(t *testing.T) {
	// Only the leading run of boundary-stamped headers is trusted: a header
	// bearing our authserv-id below a foreign-id header is sender-injected.
	h := []string{
		"attacker.invalid; spf=pass smtp.mailfrom=example.com",
		"mail.example; spf=pass smtp.mailfrom=example.com",
	}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("a trusted-id header below a foreign-id header must be ignored as sender-injected")
	}
}

func TestSPFPass_IgnoresInjectedTrustedHeaderBelowGenuineFail(t *testing.T) {
	// Genuine spf=fail on top; forged spf=pass below a foreign-id header must
	// not override it.
	h := []string{
		"mail.example; spf=fail smtp.mailfrom=example.com",
		"attacker.invalid; whatever",
		"mail.example; spf=pass smtp.mailfrom=example.com",
	}
	if SPFPass(h, trustedID, "example.com") {
		t.Error("a forged trusted-id pass below a foreign-id header must not override the genuine fail")
	}
}

func TestSPFPass_Empty(t *testing.T) {
	good := "mail.example; spf=pass smtp.mailfrom=example.com"
	if SPFPass(nil, trustedID, "example.com") {
		t.Error("no A-R headers must not pass")
	}
	if SPFPass([]string{good}, trustedID, "") {
		t.Error("empty domain must not pass")
	}
	if SPFPass([]string{good}, "", "example.com") {
		t.Error("empty trusted authserv-id must fail closed")
	}
}

func TestSPFPass_CombinedWithDKIMHeader(t *testing.T) {
	// A single Authentication-Results header carrying both results: each check
	// reads its own token + identity.
	h := []string{"mail.example; dkim=pass header.d=example.com; spf=pass smtp.mailfrom=example.com"}
	if !DKIMPass(h, trustedID, "example.com") {
		t.Error("expected DKIM pass")
	}
	if !SPFPass(h, trustedID, "example.com") {
		t.Error("expected SPF pass")
	}
}
