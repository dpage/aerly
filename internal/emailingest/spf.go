package emailingest

import "strings"

// SPFPass reports whether a TRUSTED Authentication-Results header asserts
// `spf=pass` for an envelope sender that ALIGNS with `domain` (the From
// domain) — i.e. the SPF-authenticated identity (smtp.mailfrom, falling back
// to smtp.helo) has the same domain, exact and case-insensitive, with no
// subdomain allowance.
//
// As with DKIMPass, only Authentication-Results headers whose authserv-id
// equals trustedAuthServID — the ones stamped by our own boundary MTA — are
// trusted; any header a sender injected carries a different authserv-id and is
// ignored. Plain SPF only proves a server may send for the *envelope* domain,
// which a spoofer can set freely, so we additionally require that domain to
// match the From header: otherwise an SPF "pass" for the attacker's own
// envelope domain would vouch for a forged From and let their bookings land in
// the victim's account.
//
// An empty trustedAuthServID or domain fails closed.
func SPFPass(headers []string, trustedAuthServID, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	want := strings.ToLower(strings.TrimSpace(trustedAuthServID))
	if domain == "" || want == "" {
		return false
	}
	for _, header := range headers {
		if !authServMatches(header, want) {
			continue
		}
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
	if !strings.Contains(line, "spf=pass") {
		return false
	}
	id := spfIdentityDomain(line, "smtp.mailfrom=")
	if id == "" {
		id = spfIdentityDomain(line, "smtp.helo=")
	}
	return id != "" && id == domain
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
