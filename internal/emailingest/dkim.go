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
	// Strip CFWS comments up front so neither the authserv-id check nor the
	// per-clause correlation below can be derailed by punctuation inside a
	// comment — see stripComments.
	stripped := make([]string, len(headers))
	for i, header := range headers {
		stripped[i] = stripComments(header)
	}
	for _, header := range boundaryAuthResults(stripped, want) {
		if headerAuthenticates(header, domain) {
			return true
		}
	}
	return false
}

// stripComments removes RFC 5322 CFWS comments from an Authentication-Results
// header value, replacing each with a single space (a comment is semantically
// whitespace, so eliding it outright could weld two tokens together).
//
// This is required for correctness, not tidiness: comment text is arbitrary and
// routinely contains the very delimiters the parsing relies on. OpenDKIM stamps
// `dkim=pass (2048-bit key; unprotected) header.d=example.com`, and that
// SEMICOLON inside the comment would otherwise split the resinfo in two, leaving
// dkim=pass in one clause and its header.d= in the next, so a perfectly good
// signature fails to align and legitimate mail is rejected.
//
// Comments nest, and a quoted-pair (`\(`) escapes a parenthesis rather than
// opening or closing one; parentheses inside a quoted string are literal data
// and not comment delimiters (RFC 5322 §3.2.1–3.2.4). Semicolons that separate
// resinfo clauses are never inside a comment by definition, so removing comments
// only ever discards non-delimiter punctuation and cannot merge two clauses.
// An unterminated comment swallows the rest of the value, which fails closed:
// the properties it would have carried disappear and nothing authenticates.
func stripComments(header string) string {
	var sb strings.Builder
	sb.Grow(len(header))
	depth, inQuotes := 0, false
	for i := 0; i < len(header); i++ {
		c := header[i]
		switch {
		case c == '\\' && (inQuotes || depth > 0):
			// Quoted-pair: consume the escaped octet with its backslash so an
			// escaped delimiter can't open or close a comment or quoted string.
			if depth == 0 {
				sb.WriteByte(c)
				if i+1 < len(header) {
					sb.WriteByte(header[i+1])
				}
			}
			i++
		case c == '"' && depth == 0:
			inQuotes = !inQuotes
			sb.WriteByte(c)
		case c == '(' && !inQuotes:
			if depth == 0 {
				sb.WriteByte(' ')
			}
			depth++
		case c == ')' && !inQuotes && depth > 0:
			depth--
		default:
			if depth == 0 {
				sb.WriteByte(c)
			}
		}
	}
	return sb.String()
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
// The value must have had its comments removed by stripComments already, or a
// semicolon inside a comment will split a resinfo clause in two.
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
