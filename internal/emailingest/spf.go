package emailingest

import "strings"

// SPFPass reports whether a TRUSTED Authentication-Results header asserts
// `spf=pass` for an envelope sender that ALIGNS with `domain` (the From
// domain) — i.e. the SPF-authenticated identity (smtp.mailfrom, falling back
// to smtp.helo) has the same domain, exact and case-insensitive, with no
// subdomain allowance.
//
// As with DKIMPass, only the leading run of Authentication-Results headers
// stamped by our own boundary MTA (authserv-id == trustedAuthServID) is trusted
// — see boundaryAuthResults. Plain SPF only proves a server may send for the
// *envelope* domain, which a spoofer can set freely, so we additionally require
// that domain to match the From header: otherwise an SPF "pass" for the
// attacker's own envelope domain would vouch for a forged From and let their
// bookings land in the victim's account.
//
// An empty trustedAuthServID or domain fails closed.
func SPFPass(headers []string, trustedAuthServID, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	want := strings.ToLower(strings.TrimSpace(trustedAuthServID))
	if domain == "" || want == "" {
		return false
	}
	for _, header := range boundaryAuthResults(headers, want) {
		for _, line := range strings.Split(header, "\n") {
			if spfLineMatches(line, domain) {
				return true
			}
		}
	}
	return false
}

// spfLineMatches reports whether a single Authentication-Results line carries
// `spf=pass` for an identity (smtp.mailfrom, else smtp.helo) whose domain
// equals domain.
func spfLineMatches(line, domain string) bool {
	line = strings.ToLower(line)
	if !hasAuthResultToken(line, "spf=pass") {
		return false
	}
	id := spfIdentityDomain(line, "smtp.mailfrom=")
	if id == "" {
		id = spfIdentityDomain(line, "smtp.helo=")
	}
	return id != "" && id == domain
}

// hasAuthResultToken reports whether want (e.g. "spf=pass" / "dkim=pass")
// appears in line as a complete token — delimited by whitespace or ';' per
// RFC 8601 — rather than as a substring of another field's value. Without this,
// a real spf=fail result could be spoofed by an attacker-controlled identity
// such as smtp.mailfrom=foo+spf=pass@example.com, whose local-part contains the
// literal "spf=pass". Callers must lower-case line first.
func hasAuthResultToken(line, want string) bool {
	for _, tok := range strings.FieldsFunc(line, func(r rune) bool {
		return r == ';' || r == ' ' || r == '\t' || r == '\r' || r == '\n'
	}) {
		if tok == want {
			return true
		}
	}
	return false
}

// spfIdentityDomain extracts the domain of the value following key (e.g.
// "smtp.mailfrom=") on an Authentication-Results line. The value may be a bare
// domain or a full address (user@domain); either way the domain part is
// returned, surrounding quotes/whitespace trimmed. Returns "" when key is
// absent or the value is empty.
func spfIdentityDomain(line, key string) string {
	idx := strings.Index(line, key)
	if idx < 0 {
		return ""
	}
	rest := line[idx+len(key):]
	rest = strings.TrimLeft(rest, `"' `)
	if end := strings.IndexAny(rest, " \t,;\"'"); end >= 0 {
		rest = rest[:end]
	}
	if at := strings.LastIndex(rest, "@"); at >= 0 {
		rest = rest[at+1:]
	}
	return rest
}
