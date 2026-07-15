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
		if headerAuthenticates(header, domain) {
			return true
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

// headerAuthenticates reports whether a single TRUSTED Authentication-Results
// header value carries a dkim=pass result whose OWN header.d= equals domain.
//
// The pass result and the header.d= must belong to the SAME resinfo clause. An
// Authentication-Results value is `authserv-id *(";" resinfo)`, and each resinfo
// is `method "=" result *(CFWS ptype "." property "=" value)` (RFC 8601 §2.2).
// A single header stamped by our own MTA can therefore legitimately carry
// several dkim results — e.g. `mta; dkim=fail header.d=victim.com; dkim=pass
// header.d=attacker.com` when the message bundles a failing victim.com signature
// with a passing attacker.com one. Matching dkim=pass and header.d= independently
// across the whole value would then vouch for victim.com off the back of the
// unrelated attacker.com pass, letting an attacker spoof From: victim@victim.com
// into the victim's account. Correlating them per clause closes that.
func headerAuthenticates(header, domain string) bool {
	// Clauses are ';'-separated; the leading authserv-id clause carries no
	// dkim= token and is simply skipped by dkimClauseMatches.
	for _, clause := range strings.Split(header, ";") {
		if dkimClauseMatches(clause, domain) {
			return true
		}
	}
	return false
}

// dkimClauseMatches reports whether a single resinfo clause is a dkim=pass
// result aligned to domain. The method-result is the FIRST `dkim=` token in the
// clause, so a `dkim=pass` smuggled into a later property value (e.g.
// header.i=foo+dkim=pass@example.com) can't promote a real `dkim=fail`; the
// header.d= property must then appear in this SAME clause and equal domain.
func dkimClauseMatches(clause, domain string) bool {
	fields := strings.Fields(strings.ToLower(clause))
	result := ""
	for _, f := range fields {
		if strings.HasPrefix(f, "dkim=") {
			result = strings.TrimPrefix(f, "dkim=")
			break
		}
	}
	if result != "pass" {
		return false
	}
	// Bind to the FIRST header.d= in the clause, symmetric with taking the first
	// dkim= as the result: a legitimate resinfo carries exactly one signing
	// domain, so a later stray header.d= token can't be used to vouch for a
	// second domain off one pass.
	for _, f := range fields {
		if strings.HasPrefix(f, "header.d=") {
			d := strings.Trim(strings.TrimPrefix(f, "header.d="), `"'`)
			return d == domain
		}
	}
	return false
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
