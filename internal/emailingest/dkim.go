package emailingest

import "strings"

// DKIMPass reports whether a TRUSTED Authentication-Results header asserts
// `dkim=pass` for `domain` (exact, case-insensitive header.d= match; no
// subdomain allowance).
//
// headers is the list of raw Authentication-Results header values from the
// message, in the order they appear (topmost first). Only the leading run of
// headers whose authserv-id (the leading token before the first ';' or
// whitespace, RFC 8601) equals trustedAuthServID is considered — see
// boundaryAuthResults for why only that leading run is trustworthy. Without
// this, a spoofer could forge `From: victim@…` plus their own
// `Authentication-Results: x; dkim=pass header.d=…` to vouch for the victim's
// domain and have their bookings written into the victim's account.
//
// An empty trustedAuthServID trusts no header (fail closed): the caller must
// configure the local authserv-id for DKIM enforcement to mean anything.
func DKIMPass(headers []string, trustedAuthServID, domain string) bool {
	domain = strings.ToLower(strings.TrimSpace(domain))
	want := strings.ToLower(strings.TrimSpace(trustedAuthServID))
	if domain == "" || want == "" {
		return false
	}
	for _, header := range boundaryAuthResults(headers, want) {
		for _, line := range strings.Split(header, "\n") {
			if dkimLineMatches(line, domain) {
				return true
			}
		}
	}
	return false
}

// boundaryAuthResults returns the leading contiguous run of Authentication-
// Results header values stamped by our own boundary MTA (authserv-id == want;
// want must already be lower-cased). It returns the prefix of headers up to —
// but not including — the first header whose authserv-id does not match.
//
// The boundary MTA prepends its Authentication-Results header(s) at the very
// top of the message, above anything the sender supplied, so the trustworthy
// results are exactly this leading run. The first header whose authserv-id does
// not match marks the start of sender-supplied (untrusted) territory, and
// everything from there down is ignored. This is deliberately stricter than
// trusting any matching header found anywhere in the stack: a forged
// `<our-id>; dkim=pass` header smuggled in below a foreign-authserv-id header
// is no longer honoured, since our MTA would have prepended its own header
// above any genuine result rather than leaving one stranded beneath a foreign
// stamp.
//
// This still relies on the boundary MTA stripping inbound Authentication-
// Results headers that bear its own authserv-id (an RFC 8601 §5 requirement,
// documented in .env.example): a forged header carrying our authserv-id and
// sitting contiguously within the leading run is textually indistinguishable
// from a genuine one and can only be removed at the boundary. The leading-run
// rule closes the gap for everything else.
func boundaryAuthResults(headers []string, want string) []string {
	n := 0
	for _, header := range headers {
		if !authServMatches(header, want) {
			break
		}
		n++
	}
	return headers[:n]
}

// authServMatches reports whether the Authentication-Results header value was
// stamped by want — i.e. its authserv-id (the first token, terminated by the
// first ';' or whitespace) matches, case-insensitively.
func authServMatches(header, want string) bool {
	h := strings.TrimSpace(header)
	if end := strings.IndexAny(h, "; \t\r\n"); end >= 0 {
		h = h[:end]
	}
	return strings.ToLower(h) == want
}

func dkimLineMatches(line, domain string) bool {
	line = strings.ToLower(line)
	// Match dkim=pass as a complete RFC 8601 token, not a substring: an
	// attacker-controlled field value (e.g. header.i=foo+dkim=pass@example.com)
	// must not be able to smuggle the result past a real dkim=fail.
	if !hasAuthResultToken(line, "dkim=pass") {
		return false
	}
	idx := strings.Index(line, "header.d=")
	if idx < 0 {
		return false
	}
	rest := line[idx+len("header.d="):]
	rest = strings.TrimLeft(rest, `"' `)
	end := strings.IndexAny(rest, " \t,;\"'")
	if end >= 0 {
		rest = rest[:end]
	}
	return rest == domain
}

// FromDomain extracts the domain part of an email address, lowercased.
// Returns "" if the input doesn't look like an address.
func FromDomain(addr string) string {
	at := strings.LastIndex(addr, "@")
	if at <= 0 || at == len(addr)-1 {
		return ""
	}
	return strings.ToLower(addr[at+1:])
}
