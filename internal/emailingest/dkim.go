package emailingest

import "strings"

// DKIMPass reports whether a TRUSTED Authentication-Results header asserts
// `dkim=pass` for `domain` (exact, case-insensitive header.d= match; no
// subdomain allowance).
//
// headers is the list of raw Authentication-Results header values from the
// message, in order. Only headers whose authserv-id (the leading token before
// the first ';' or whitespace, RFC 8601) equals trustedAuthServID are
// considered — i.e. the ones stamped by our own boundary MTA. Any
// Authentication-Results header a sender injected into the message carries a
// different (or absent) authserv-id and is ignored. Without this, a spoofer
// could forge `From: victim@…` plus their own
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
	for _, header := range headers {
		if !authServMatches(header, want) {
			continue
		}
		for _, line := range strings.Split(header, "\n") {
			if dkimLineMatches(line, domain) {
				return true
			}
		}
	}
	return false
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
