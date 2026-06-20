package emailingest

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
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
	// Geocoder fills missing part coordinates from their addresses (hotels,
	// transfers, …) so an ingested plan plots on the map, mirroring the HTTP
	// path. Optional — nil disables geocoding.
	Geocoder geocode.Geocoder
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
	added, failed, err := s.capturePlans(ctx, u.ID, body, docs)
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
		FromAddr:  s.Cfg.IngestAddress,
		ToAddr:    parsed.From,
		InReplyTo: parsed.MessageID,
		Subject:   parsed.Subject,
		Added:     added,
		Failed:    failed,
		PublicURL: s.Cfg.PublicURL,
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
// It proposes plans (tripID 0 → no rebooking match pre-attach), picks a target
// trip by date proximity (auto-creating one when nothing overlaps), and commits
// each plan against that trip. Every committed plan (any type) yields a reply
// item so the confirmation email lists what was added; commit failures yield a
// reply failure.
func (s *Service) capturePlans(ctx context.Context, userID int64, body string, emDocs []Document) ([]ReplyItem, []ReplyFailure, error) {
	docs := make([]planops.Document, 0, len(emDocs))
	for _, d := range emDocs {
		docs = append(docs, planops.Document{Data: d.Data, MediaType: d.MediaType, Filename: d.Filename})
	}
	proposals, err := planops.Propose(ctx, s.PlanDeps, userID, 0, body, docs)
	if err != nil {
		return nil, nil, fmt.Errorf("propose: %w", err)
	}

	added := []ReplyItem{}
	failed := []ReplyFailure{}
	for _, p := range proposals {
		start, end := planops.PlanSpan(p.Parts)
		tripID, ok, err := planops.SelectTrip(ctx, s.PlanDeps, userID, start, end)
		if err != nil {
			slog.Warn("emailingest: planops select trip", "err", err)
			failed = append(failed, planReplyFailure(p, err))
			continue
		}
		if !ok {
			tripID, err = s.createTripForPlan(ctx, userID, p)
			if err != nil {
				slog.Warn("emailingest: create trip for ingested plan", "err", err)
				failed = append(failed, planReplyFailure(p, err))
				continue
			}
		}
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
			if _, gerr := geocode.PlanParts(ctx, s.Store, s.Geocoder, pl.ID); gerr != nil {
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
	return added, failed, nil
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

// createTripForPlan auto-creates a trip when no existing trip matches by date
// proximity (spec §6.3). For a flight booking it names the trip for where the
// flight lands ("Trip to <city>") rather than after the flight's ident (#21),
// falling back to the plan title and then a generic label.
//
// The trip's date hints come from PlanDateSpan, which reads each part's local
// calendar day (the arrival day in the destination tz for the end), so an
// overnight eastbound flight ends the trip on its landing day rather than the
// UTC day of the arrival instant (issue #57).
func (s *Service) createTripForPlan(ctx context.Context, userID int64, p planops.ProposedPlan) (int64, error) {
	name := planops.TripNameForProposedPlan(p)
	if name == "" {
		name = strings.TrimSpace(p.Title)
	}
	if name == "" {
		name = "Trip from email"
	}
	start, end := planops.PlanDateSpan(p.Parts)
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
