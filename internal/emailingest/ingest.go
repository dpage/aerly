package emailingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/dpage/aerly/internal/geocode"
	"github.com/dpage/aerly/internal/notify"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/providers"
	"github.com/dpage/aerly/internal/sse"
	"github.com/dpage/aerly/internal/store"
)

// Config controls the ingest service. All fields are required.
type Config struct {
	MaildirPath  string
	PollInterval time.Duration
	RequireDKIM  bool
	// DKIMAuthServID is the authserv-id our boundary MTA stamps onto the
	// Authentication-Results header it adds (e.g. the mail host). Only headers
	// bearing this id are trusted for DKIM evaluation; sender-injected ones are
	// ignored. Empty means trust no header (DKIM never passes).
	DKIMAuthServID string
	// RateLimitPerDay caps how many messages a single verified user may have
	// ingested in the trailing 24h. A sender over the cap is rejected before the
	// LLM runs, so a prompt-injection (or a compromised account) can't drive
	// unbounded paid-LLM spend or fan plans into the database. 0 means unlimited.
	RateLimitPerDay int
	MaxBodyBytes    int
	// MaxAttachments and MaxAttachBytes bound the PDF attachments forwarded to
	// the LLM per message (count and cumulative bytes). 0 means unlimited.
	MaxAttachments int
	MaxAttachBytes int64
	IngestAddress  string // e.g. "flights@flights.example" — also the reply From
	SendmailPath   string
	PublicURL      string
}

// Service is the long-running ingest goroutine.
type Service struct {
	Cfg       Config
	Store     *store.Store
	Extractor *Extractor
	// PlanDeps wires the planops capture path (multi-type plans incl. flights
	// + date-proximity trip selection). Its Store/Extractor/Resolver must be
	// set for ingest to do anything useful.
	PlanDeps planops.Deps
	// GeoResolver fills missing part coordinates from their addresses (hotels,
	// transfers, …) so an ingested plan plots on the map, mirroring the HTTP
	// path. Optional — nil disables geocoding.
	GeoResolver *geocode.Resolver
	// Hub publishes trip.updated / plan.updated so open clients pick up an
	// ingested booking live (the trips list + the open trip), without a manual
	// refresh. Optional — nil disables live updates.
	Hub *sse.Hub
}

type outcomeKind int

const (
	outcomeOK outcomeKind = iota
	outcomeTransient
	outcomePoison
)

type outcome struct {
	kind outcomeKind
}

// Run loops until ctx is done, draining the Maildir on each tick.
func (s *Service) Run(ctx context.Context) error {
	if err := s.EnsureDirs(); err != nil {
		return err
	}
	interval := s.Cfg.PollInterval
	if interval <= 0 {
		interval = 10 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	// Drain once on startup so we don't wait for the first tick.
	s.drainNew(ctx)
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-t.C:
			s.drainNew(ctx)
		}
	}
}

// EnsureDirs creates the Maildir sub-directories if they don't already exist.
// Exposed so tests can prep a temp Maildir before dropping fixtures into new/.
func (s *Service) EnsureDirs() error {
	for _, sub := range []string{"new", "cur", "tmp", ".failed"} {
		if err := os.MkdirAll(filepath.Join(s.Cfg.MaildirPath, sub), 0o755); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) drainNew(ctx context.Context) {
	newDir := filepath.Join(s.Cfg.MaildirPath, "new")
	entries, err := os.ReadDir(newDir)
	if err != nil {
		slog.Warn("emailingest: read maildir", "err", err)
		return
	}
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		path := filepath.Join(newDir, e.Name())
		out := s.processOneSafely(ctx, path)
		switch out.kind {
		case outcomeOK:
			if err := os.Remove(path); err != nil {
				slog.Warn("emailingest: remove processed file", "err", err, "path", path)
			}
		case outcomeTransient:
			// leave it; retry next tick
		case outcomePoison:
			dst := filepath.Join(s.Cfg.MaildirPath, ".failed", e.Name())
			if err := os.Rename(path, dst); err != nil {
				slog.Warn("emailingest: move poison", "err", err, "path", path)
			}
		}
	}
}

// processOneSafely wraps processOne with panic recovery so one malformed
// message (or an unexpected nil/index in the parse/extract path) can't crash
// the shared server process. A panicking message is treated as poison and
// moved aside rather than retried forever.
func (s *Service) processOneSafely(ctx context.Context, path string) (out outcome) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("emailingest: recovered from panic", "path", path, "panic", r)
			out = outcome{kind: outcomePoison}
		}
	}()
	return s.processOne(ctx, path)
}

func (s *Service) processOne(ctx context.Context, path string) outcome {
	raw, err := os.ReadFile(path)
	if err != nil {
		slog.Warn("emailingest: read", "err", err, "path", path)
		return outcome{kind: outcomeTransient}
	}
	parsed, err := Parse(raw)
	if err != nil {
		slog.Info("emailingest: unparseable, poison", "err", err)
		s.logIngest(ctx, "", "", "", false, nil, "parse_error", 0, 0, err.Error())
		return outcome{kind: outcomePoison}
	}

	// An absent/unparseable/multiple From can't be tied to a verified user and
	// is a spoofing signal — reject it explicitly rather than letting it fall
	// through to a misleading "no verified user" outcome.
	if parsed.From == "" {
		slog.Info("emailingest: missing/unparseable From, poison")
		s.logIngest(ctx, parsed.MessageID, "", parsed.Subject, false, nil, "no_from", 0, 0, "")
		return outcome{kind: outcomePoison}
	}

	// Refuse mail addressed from our own ingest address — prevents reply loops.
	if strings.EqualFold(parsed.From, s.Cfg.IngestAddress) {
		slog.Info("emailingest: refusing self-addressed mail", "from", parsed.From)
		return outcome{kind: outcomePoison}
	}

	fromDomain := FromDomain(parsed.From)
	dkimOK := DKIMPass(parsed.AuthResults, s.Cfg.DKIMAuthServID, fromDomain)
	if s.Cfg.RequireDKIM && !dkimOK {
		slog.Info("emailingest: DKIM not pass, poison", "from", parsed.From)
		s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, nil, "dkim_failed", 0, 0, "")
		return outcome{kind: outcomePoison}
	}

	u, err := s.Store.UserByVerifiedEmail(ctx, parsed.From)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			slog.Info("emailingest: no verified user for sender, poison", "from", parsed.From)
			s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, nil, "no_user", 0, 0, "")
			return outcome{kind: outcomePoison}
		}
		slog.Warn("emailingest: user lookup transient", "err", err)
		return outcome{kind: outcomeTransient}
	}

	// Per-user rate limit (rolling 24h). Checked after the sender resolves to a
	// verified user but before the LLM runs, so a flood — whether abuse or a
	// compromised account — is bounded in paid-LLM spend and plans written. The
	// over-limit message is logged and dropped (poison): retrying would just
	// re-hit the cap, and replying could amplify a flood, so we stay silent like
	// the other rejection paths.
	if limit := s.Cfg.RateLimitPerDay; limit > 0 {
		n, cerr := s.Store.CountEmailIngestsSince(ctx, u.ID, time.Now().Add(-24*time.Hour))
		if cerr != nil {
			slog.Warn("emailingest: rate-limit count transient", "err", cerr)
			return outcome{kind: outcomeTransient}
		}
		if n >= limit {
			slog.Info("emailingest: per-user rate limit exceeded, poison",
				"from", parsed.From, "user", u.ID, "count", n, "limit", limit)
			s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, &u.ID, "rate_limited", 0, 0, "")
			return outcome{kind: outcomePoison}
		}
	}

	body, docs := buildPrompt(parsed, s.Cfg.MaxBodyBytes, s.Cfg.MaxAttachments, s.Cfg.MaxAttachBytes)

	// All extracted bookings — flights included — now flow through planops:
	// each proposal becomes a plan-with-parts attached to a trip chosen by
	// date proximity (auto-creating one when nothing matches). Flight parts
	// are resolver-enriched inside planops.Propose (with the same email-schedule
	// fallback the legacy flightops path used), so emailed flights land as
	// flight-typed plan parts the tracker/poller key on, not legacy flight rows.
	added, failed, multiTripHint, err := s.capturePlans(ctx, u.ID, body, docs)
	if err != nil {
		// Treat any extractor/propose failure as transient: drain loop retries.
		slog.Warn("emailingest: capture plans", "err", err)
		return outcome{kind: outcomeTransient}
	}

	status := "accepted"
	switch {
	case len(added) == 0 && len(failed) == 0:
		status = "nothing_found"
	case len(added) == 0:
		status = "all_failed"
	case len(failed) > 0:
		status = "partial"
	}
	s.logIngest(ctx, parsed.MessageID, parsed.From, parsed.Subject, dkimOK, &u.ID, status, len(added), len(failed), "")

	msg := BuildReply(ReplyInput{
		FromAddr:      s.Cfg.IngestAddress,
		ToAddr:        parsed.From,
		InReplyTo:     parsed.MessageID,
		Subject:       parsed.Subject,
		Added:         added,
		Failed:        failed,
		MultiTripHint: multiTripHint,
		PublicURL:     s.Cfg.PublicURL,
	})
	if err := Send(ctx, s.Cfg.SendmailPath, s.Cfg.IngestAddress, msg); err != nil {
		slog.Warn("emailingest: send reply", "err", err)
		// We still consider the message processed — flights were added (or
		// the audit row was written). Don't loop on send failures.
	}
	return outcome{kind: outcomeOK}
}

// maxDocBytes caps each document we forward to the LLM. Anthropic accepts
// PDFs up to ~32 MiB; this leaves headroom and prevents an oversized
// attachment from causing the provider to reject the whole request — which
// would otherwise loop in `new/` as a transient extractor failure.
const maxDocBytes = 25 * 1024 * 1024

// buildPrompt returns the text body to put in the LLM prompt and the list
// of document attachments (PDFs) to pass alongside it. Plain text + HTML
// are concatenated into the prompt with section dividers; PDFs are
// passed natively as Document blocks rather than text-extracted. PDFs
// larger than maxDocBytes are dropped with a warning.
//
// maxBody truncates only the text portion. Attachments are bounded by both a
// count cap (maxAttachments) and a cumulative-byte cap (maxAttachBytes) so a
// sender can't drive unbounded paid-LLM spend with many/large PDFs; each is
// also individually capped at maxDocBytes. A cap of 0 means unlimited.
func buildPrompt(p *Parsed, maxBody, maxAttachments int, maxAttachBytes int64) (string, []Document) {
	var sb strings.Builder
	if p.TextBody != "" {
		sb.WriteString("--- text/plain ---\n")
		sb.WriteString(p.TextBody)
		sb.WriteString("\n")
	}
	if p.HTMLBody != "" {
		sb.WriteString("--- text/html ---\n")
		// Strip the byte-heavy non-content (CSS, scripts, comments, base64
		// data: images) so a large marketing-heavy HTML email — common for
		// hotel confirmations — doesn't bury or truncate the reservation text.
		sb.WriteString(sanitizeHTML(p.HTMLBody))
		sb.WriteString("\n")
	}
	body := sb.String()
	if maxBody > 0 && len(body) > maxBody {
		body = body[:maxBody]
	}
	docs := make([]Document, 0, len(p.PDFs))
	var totalBytes int64
	for i, pdfBytes := range p.PDFs {
		if maxAttachments > 0 && len(docs) >= maxAttachments {
			slog.Warn("emailingest: attachment count cap reached; dropping remaining PDFs",
				"cap", maxAttachments, "attachments", len(p.PDFs))
			break
		}
		if len(pdfBytes) > maxDocBytes {
			slog.Warn("emailingest: dropping oversized PDF attachment",
				"index", i+1, "bytes", len(pdfBytes), "cap", maxDocBytes)
			continue
		}
		if maxAttachBytes > 0 && totalBytes+int64(len(pdfBytes)) > maxAttachBytes {
			slog.Warn("emailingest: cumulative attachment byte cap reached; dropping remaining PDFs",
				"cap", maxAttachBytes)
			break
		}
		totalBytes += int64(len(pdfBytes))
		docs = append(docs, Document{
			Data:      pdfBytes,
			MediaType: "application/pdf",
			Filename:  fmt.Sprintf("attachment-%d.pdf", i+1),
		})
	}
	return body, docs
}

// reStyleScript removes <style>/<script>/<head> blocks (case-insensitive,
// across newlines); reComment removes HTML comments; reDataURI collapses
// base64 data: URIs (embedded images) to a short placeholder. Together they
// strip the bulk of a marketing email's bytes while leaving the textual
// content + remaining tags the LLM reads.
var (
	reStyleScript = regexp.MustCompile(`(?is)<style\b[^>]*>.*?</style>|<script\b[^>]*>.*?</script>|<head\b[^>]*>.*?</head>`)
	reComment     = regexp.MustCompile(`(?s)<!--.*?-->`)
	reDataURI     = regexp.MustCompile(`(?i)data:[a-z0-9.+/-]+;base64,[A-Za-z0-9+/=\s]+`)
	reBlankLines  = regexp.MustCompile(`\n[ \t]*\n([ \t]*\n)+`)
)

// sanitizeHTML trims an HTML email body down to its content for the LLM prompt.
func sanitizeHTML(html string) string {
	html = reStyleScript.ReplaceAllString(html, "")
	html = reComment.ReplaceAllString(html, "")
	html = reDataURI.ReplaceAllString(html, "data:[stripped]")
	html = reBlankLines.ReplaceAllString(html, "\n\n")
	return strings.TrimSpace(html)
}

// failureReason renders a per-leg ReplyFailure.Reason string, recognising
// the well-known sentinel errors from the resolver so the user sees a
// terse, actionable message instead of a stack of wrapped errors.
func failureReason(err error) string {
	switch {
	case errors.Is(err, providers.ErrFlightUnscheduled):
		return "the airline hasn't published a schedule for that date yet — try again closer to the departure date"
	case errors.Is(err, providers.ErrFlightNotFound):
		return "no matching flight found for that ident on that date"
	}
	s := err.Error()
	if len(s) > 200 {
		s = s[:200] + "…"
	}
	return s
}

// capturePlans runs the planops capture path for every booking in an email.
// It proposes plans (tripID 0 → no rebooking match pre-attach), picks a single
// target trip for the whole email by date proximity (auto-creating one when
// nothing overlaps), and commits every plan against that trip. Every committed
// plan (any type) yields a reply item so the confirmation email lists what was
// added; commit failures yield a reply failure.
//
// It also returns multiTripHint: the target trip's name when the batch looks
// like it might really be more than one trip (see looksLikeSeparateTrips), so
// the reply can invite the user to split them. Empty means no hint.
func (s *Service) capturePlans(ctx context.Context, userID int64, body string, emDocs []Document) (added []ReplyItem, failed []ReplyFailure, multiTripHint string, err error) {
	docs := make([]planops.Document, 0, len(emDocs))
	for _, d := range emDocs {
		docs = append(docs, planops.Document{Data: d.Data, MediaType: d.MediaType, Filename: d.Filename})
	}
	proposals, err := planops.Propose(ctx, s.PlanDeps, userID, 0, body, docs)
	if err != nil {
		return nil, nil, "", fmt.Errorf("propose: %w", err)
	}

	added = []ReplyItem{}
	failed = []ReplyFailure{}

	// A single booking confirmation is one trip's worth of bookings: a round
	// trip ticketed as two one-way PNRs, or a flight and its hotel, arrives in
	// one email yet its legs can be days apart and carry different confirmation
	// refs. Placing each plan independently by date proximity would scatter them
	// across separate trips — a Vienna⇄Sofia conference booked as two one-ways
	// landed as "Trip to Sofia" plus "Trip to Vienna". So resolve ONE target
	// trip for the whole email from the union of every proposal's span and
	// commit all plans against it; the user can still split a genuinely
	// unrelated batch by hand afterwards.
	tripID, err := s.selectTripForBatch(ctx, userID, proposals)
	if err != nil {
		slog.Warn("emailingest: select trip for ingested batch", "err", err)
		for _, p := range proposals {
			failed = append(failed, planReplyFailure(p, err))
		}
		return added, failed, "", nil
	}

	for _, p := range proposals {
		// The sender confirms as-extracted with themselves as the sole
		// passenger; the user can correct or re-share afterwards in the UI.
		in := toConfirmInput(p)
		in.PassengerIDs = []int64{userID}
		committed, err := planops.Commit(ctx, s.PlanDeps, tripID, userID, []planops.ConfirmPlanInput{in})
		if err != nil {
			slog.Warn("emailingest: planops commit", "err", err, "trip", tripID)
			failed = append(failed, planReplyFailure(p, err))
			continue
		}
		// Geocode addressed parts (hotels, transfers, …) so they plot on the
		// map, then publish so open clients pick the booking up live — the same
		// follow-up the HTTP confirm path does, which the email path previously
		// skipped (committed plans had no coordinates and never refreshed).
		for _, pl := range committed {
			// Best-effort: geocoding runs synchronously here (the ingest loop
			// is already off the request path) and a failure is non-fatal.
			if _, gerr := geocode.PlanParts(ctx, s.Store, s.GeoResolver, pl.ID); gerr != nil {
				slog.Warn("emailingest: geocode plan", "err", gerr, "plan", pl.ID)
			}
			// plan.updated drives the tracker + open-trip refresh (App.tsx
			// onPlan); published whether or not geocoding changed anything so a
			// freshly ingested flight appears in the tracker live.
			notify.PlanUpdated(ctx, s.Store, s.Hub, tripID, pl.ID)
		}
		// trip.updated drives the trips-list refresh (App.tsx onTrip) so a
		// brand-new auto-created trip appears without a manual reload.
		notify.TripUpdated(ctx, s.Store, s.Hub, tripID)
		added = append(added, planReplyItem(p))
	}

	// When at least two bookings landed and their flight legs don't form one
	// connected journey, the batch may really be several trips we've merged into
	// one. Surface the trip's name so the reply can offer to split it. A failed
	// name lookup just drops the hint (non-fatal, best-effort).
	if len(added) >= 2 && looksLikeSeparateTrips(proposals) {
		if t, terr := s.Store.TripByID(ctx, tripID); terr != nil {
			slog.Warn("emailingest: trip name for multi-trip hint", "err", terr, "trip", tripID)
		} else if t != nil {
			multiTripHint = t.Name
		}
	}
	return added, failed, multiTripHint, nil
}

// looksLikeSeparateTrips reports whether a batch of proposals from one email
// resembles more than one distinct trip rather than a single coherent journey.
// We keep every booking from one email in one trip (see capturePlans), but a
// return home followed by another departure, or a disconnected hop, suggests the
// merge lumped together what are really separate trips — worth flagging so the
// reply can offer to split them.
//
// The signal is flight-leg connectivity, walked in departure order: two legs
// belong to the same trip when the traveller stays put between them (the first
// leg's arrival airport is the next leg's departure airport) and that airport is
// not home (the first leg's origin). A leg that returns home before a later
// departure, or a hop whose departure airport isn't where the previous leg
// landed, is a trip boundary. This deliberately treats an ordinary round trip
// (out and back, arrival == next departure away from home) and a connected
// multi-city itinerary as one trip, so it doesn't nag on normal travel.
//
// Only flight legs are considered: their IATA endpoints compare cleanly, whereas
// mixing free-text ground/hotel labels in would risk false positives on ordinary
// trips (a taxi labelled differently from the airport it leaves). A batch with
// fewer than two flight legs is never flagged.
func looksLikeSeparateTrips(proposals []planops.ProposedPlan) bool {
	type leg struct {
		start        time.Time
		origin, dest string
	}
	var legs []leg
	for _, p := range proposals {
		if p.Type != "flight" {
			continue
		}
		for _, part := range p.Parts {
			f := part.Flight
			if f == nil || part.StartsAt.IsZero() {
				continue
			}
			origin := strings.ToUpper(strings.TrimSpace(f.OriginIATA))
			dest := strings.ToUpper(strings.TrimSpace(f.DestIATA))
			if origin == "" || dest == "" {
				continue
			}
			legs = append(legs, leg{start: part.StartsAt, origin: origin, dest: dest})
		}
	}
	if len(legs) < 2 {
		return false
	}
	sort.Slice(legs, func(i, j int) bool { return legs[i].start.Before(legs[j].start) })
	home := legs[0].origin
	for i := 0; i+1 < len(legs); i++ {
		if legs[i].dest != legs[i+1].origin || legs[i].dest == home {
			return true
		}
	}
	return false
}

// planReplyItem renders a committed proposal of any type as a ReplyItem for the
// confirmation email. Flights show "IDENT" + date (ManualNote set when the
// schedule came from the email's own details rather than the resolver, i.e. the
// enrich fallback path that leaves FlightDetail.Resolved false). Every other
// type shows its title (or the type label) + "<Type> · <date>".
//
// ManualNote keys off Resolved, not the airframe: the resolver's schedule
// endpoint never returns an ICAO24/Mode-S hex for a future flight (that only
// arrives once the aircraft is live-tracked near departure), so an airframe
// check would mis-flag every resolved future flight as "from the email".
func planReplyItem(p planops.ProposedPlan) ReplyItem {
	if p.Type == "flight" && len(p.Parts) > 0 && p.Parts[0].Flight != nil {
		fd := p.Parts[0].Flight
		return ReplyItem{
			Label:      fd.Ident,
			Detail:     fd.ScheduledOut.Format("2006-01-02"),
			ManualNote: !fd.Resolved,
		}
	}
	return ReplyItem{Label: planLabel(p), Detail: planDetail(p)}
}

// planReplyFailure renders a proposal that couldn't be committed.
func planReplyFailure(p planops.ProposedPlan, err error) ReplyFailure {
	if p.Type == "flight" && len(p.Parts) > 0 && p.Parts[0].Flight != nil {
		fd := p.Parts[0].Flight
		return ReplyFailure{Label: fd.Ident, Detail: fd.ScheduledOut.Format("2006-01-02"), Reason: failureReason(err)}
	}
	return ReplyFailure{Label: planLabel(p), Detail: planDetail(p), Reason: failureReason(err)}
}

// planLabel is the headline for a non-flight booking: its title, else the type.
func planLabel(p planops.ProposedPlan) string {
	if t := strings.TrimSpace(p.Title); t != "" {
		return t
	}
	return planTypeLabel(p.Type)
}

// planDetail is the secondary line: the type label plus the start date.
func planDetail(p planops.ProposedPlan) string {
	d := planTypeLabel(p.Type)
	if start, _ := planops.PlanSpan(p.Parts); !start.IsZero() {
		d += " · " + start.Format("2 Jan 2006")
	}
	return d
}

// planTypeLabel is the human label for a plan type.
func planTypeLabel(t string) string {
	switch t {
	case "flight":
		return "Flight"
	case "hotel":
		return "Hotel"
	case "train":
		return "Train"
	case "ground":
		return "Ground transport"
	case "dining":
		return "Dining"
	case "excursion":
		return "Excursion"
	case "meeting":
		return "Meeting"
	case "event":
		return "Event"
	default:
		return "Booking"
	}
}

// selectTripForBatch resolves the single trip every plan from one email attaches
// to. It matches on the union of the proposals' UTC spans (so a booking whose
// legs are days apart still resolves as one trip rather than one per leg), and
// auto-creates a trip when nothing matches by date proximity (spec §6.3).
func (s *Service) selectTripForBatch(ctx context.Context, userID int64, proposals []planops.ProposedPlan) (int64, error) {
	var start, end time.Time
	for _, p := range proposals {
		ps, pe := planops.PlanSpan(p.Parts)
		if ps.IsZero() {
			continue
		}
		if start.IsZero() || ps.Before(start) {
			start = ps
		}
		if end.IsZero() || pe.After(end) {
			end = pe
		}
	}
	tripID, ok, err := planops.SelectTrip(ctx, s.PlanDeps, userID, start, end)
	if err != nil {
		return 0, err
	}
	if ok {
		return tripID, nil
	}
	return s.createTripForBatch(ctx, userID, proposals)
}

// createTripForBatch auto-creates the trip for an email's plans when none match
// by date proximity. It names the trip for where the outbound flight lands
// ("Trip to <city>", #21) — the earliest-departing flight proposal's
// destination, so a Vienna→Sofia→Vienna trip ingested as two one-way PNRs is
// named "Trip to Sofia" rather than for whichever leg the LLM listed first —
// falling back to the earliest booking's title, then a generic label.
//
// The trip's date hints span the whole batch via PlanDateSpan, which reads each
// part's local calendar day (the arrival day in the destination tz for the end),
// so an overnight eastbound flight ends the trip on its landing day (#57) and a
// two-day round trip is not stored as a single day.
func (s *Service) createTripForBatch(ctx context.Context, userID int64, proposals []planops.ProposedPlan) (int64, error) {
	name := batchTripName(proposals)
	if name == "" {
		name = "Trip from email"
	}
	var start, end time.Time
	for _, p := range proposals {
		ps, pe := planops.PlanDateSpan(p.Parts)
		if ps.IsZero() {
			continue
		}
		if start.IsZero() || ps.Before(start) {
			start = ps
		}
		if end.IsZero() || pe.After(end) {
			end = pe
		}
	}
	in := store.CreateTripPayload{Name: name}
	if !start.IsZero() {
		s := start
		in.StartsOn = &s
	}
	if !end.IsZero() {
		e := end
		in.EndsOn = &e
	}
	t, err := s.Store.CreateTrip(ctx, in, userID)
	if err != nil {
		return 0, err
	}
	return t.ID, nil
}

// batchTripName derives an auto-created trip's name from a batch of proposals:
// the earliest-departing flight's destination city, else the earliest booking's
// title, else "" (the caller then uses a generic label).
func batchTripName(proposals []planops.ProposedPlan) string {
	if p, ok := earliestProposal(proposals, true); ok {
		if name := planops.TripNameForProposedPlan(p); name != "" {
			return name
		}
	}
	if p, ok := earliestProposal(proposals, false); ok {
		return strings.TrimSpace(p.Title)
	}
	return ""
}

// earliestProposal returns the dated proposal with the earliest start. When
// flightsOnly is set only flight proposals are considered (used to name a trip
// for its outbound leg); ok is false when no proposal qualifies.
func earliestProposal(proposals []planops.ProposedPlan, flightsOnly bool) (planops.ProposedPlan, bool) {
	var best planops.ProposedPlan
	var bestStart time.Time
	found := false
	for _, p := range proposals {
		if flightsOnly && p.Type != "flight" {
			continue
		}
		ps, _ := planops.PlanSpan(p.Parts)
		if ps.IsZero() {
			continue
		}
		if !found || ps.Before(bestStart) {
			best, bestStart, found = p, ps, true
		}
	}
	return best, found
}

// toConfirmInput converts a proposed plan into a confirm payload. Email ingest
// has no interactive confirm UI, so it confirms the proposal as-extracted with
// the sender as sole passenger; the user can correct or move it afterwards.
func toConfirmInput(p planops.ProposedPlan) planops.ConfirmPlanInput {
	in := planops.ConfirmPlanInput{
		Type:             p.Type,
		Title:            p.Title,
		ConfirmationRef:  p.ConfirmationRef,
		TicketNumber:     p.TicketNumber,
		Notes:            p.Notes,
		CostAmount:       p.CostAmount,
		CostCurrency:     p.CostCurrency,
		Source:           "email",
		SupersedesPartID: p.SupersedesPartID,
	}
	for _, part := range p.Parts {
		in.Parts = append(in.Parts, planops.ConfirmPartInput{
			Type:         part.Type,
			StartsAt:     part.StartsAt,
			EndsAt:       part.EndsAt,
			StartTZ:      part.StartTZ,
			EndTZ:        part.EndTZ,
			StartLabel:   part.StartLabel,
			EndLabel:     part.EndLabel,
			StartAddress: part.StartAddress,
			EndAddress:   part.EndAddress,
			Status:       part.Status,
			Flight:       part.Flight,
			Hotel:        part.Hotel,
			Train:        part.Train,
			Ground:       part.Ground,
			Dining:       part.Dining,
			Excursion:    part.Excursion,
		})
	}
	return in
}

func (s *Service) logIngest(ctx context.Context, msgID, from, subject string, dkimPass bool, userID *int64, status string, added, failed int, errMsg string) {
	var msgPtr *string
	if msgID != "" {
		msgPtr = &msgID
	}
	if _, err := s.Store.InsertEmailIngest(ctx, store.EmailIngestPayload{
		MessageID:     msgPtr,
		FromAddress:   from,
		Subject:       subject,
		DKIMPass:      dkimPass,
		UserID:        userID,
		Status:        status,
		FlightsAdded:  added,
		FlightsFailed: failed,
		Error:         errMsg,
	}); err != nil {
		slog.Warn("emailingest: insert audit row", "err", err)
	}
}
