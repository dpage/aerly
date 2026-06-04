package handlers

import (
	"bytes"
	"io"
	"mime"
	"net/http"
	"strings"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/auth"
	"github.com/dpage/aerly/internal/planops"
	"github.com/dpage/aerly/internal/store"
	"github.com/dpage/aerly/internal/tripitics"
)

// maxUploadBytes caps a single ingest document (PDF ticket) to keep multipart
// parsing bounded. 20 MiB comfortably covers boarding-pass / itinerary PDFs.
const maxUploadBytes = 20 << 20

// Wave 2A: the ingest pipeline. POST /api/trips/{id}/ingest proposes plans from
// pasted text / uploaded documents (the LLM seam, with a rebooking match
// against the trip's existing flights); POST /api/trips/{id}/ingest/confirm
// commits the confirmed/edited proposals. Both are editor-gated on the trip.

// ingestReq is the propose request body (matches the FE IngestInput).
type ingestReq struct {
	Text   string `json:"text"`
	Source string `json:"source"`
}

// ingestConfirmReq is the confirm request body (matches the FE
// IngestConfirmInput: {plans: ConfirmPlanInput[]}).
type ingestConfirmReq struct {
	Plans []ingestConfirmPlanReq `json:"plans"`
}

type ingestConfirmPlanReq struct {
	Type             string             `json:"type"`
	Title            string             `json:"title"`
	ConfirmationRef  string             `json:"confirmation_ref"`
	TicketNumber     string             `json:"ticket_number"`
	Notes            string             `json:"notes"`
	Source           string             `json:"source"`
	CostAmount       *float64           `json:"cost_amount,omitempty"`
	CostCurrency     string             `json:"cost_currency"`
	PassengerIDs     []int64            `json:"passenger_ids"`
	Visibility       *planVisibilityReq `json:"visibility"`
	Parts            []planPartReq      `json:"parts"`
	SupersedesPartID *int64             `json:"supersedes_part_id"`
}

// ingestTrip proposes plans from pasted text against the target trip. Nothing
// is written — the response is a set of proposals for the user to confirm.
func (a *API) ingestTrip(w http.ResponseWriter, r *http.Request) {
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	text, docs, err := parseIngestBody(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// An .ics from a recognised source (e.g. a TripIt export) is structured, so
	// map it deterministically rather than via the LLM — no token cost and, for
	// TripIt, no ±2-year date guard that would drop historical trips. An
	// unrecognised calendar falls through to the LLM with its content as text.
	if data, ok := icalUpload(text, docs); ok {
		if res, recognised := icalProposals(data); recognised {
			writeJSON(w, http.StatusOK, res)
			return
		}
		text, docs = string(data), nil
	}
	if a.Extractor == nil {
		writeError(w, http.StatusServiceUnavailable, "ingest is not configured (no LLM provider)")
		return
	}
	deps := planops.Deps{Store: a.Store, Extractor: a.Extractor, Resolver: a.Resolver}
	proposals, err := planops.Propose(r.Context(), deps, me.ID, tripID, text, docs)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	out := api.IngestResultDTO{Proposals: make([]api.ProposedPlanDTO, 0, len(proposals))}
	for _, p := range proposals {
		out.Proposals = append(out.Proposals, toProposedPlanDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// parseIngestBody reads the propose request as either JSON ({text, source}) or
// multipart/form-data (a "file" part plus optional "text"/"source" fields). The
// uploaded file is forwarded to planops as a Document so the extractor's PDF
// path runs end-to-end; text-only requests keep the JSON/text path unchanged.
func parseIngestBody(r *http.Request) (text string, docs []planops.Document, err error) {
	ct := r.Header.Get("Content-Type")
	mediaType, _, _ := mime.ParseMediaType(ct)
	if mediaType != "multipart/form-data" {
		var in ingestReq
		if derr := decode(r, &in); derr != nil {
			return "", nil, derr
		}
		return in.Text, nil, nil
	}

	// +1 so a file at exactly the cap isn't silently truncated past the limit.
	if perr := r.ParseMultipartForm(maxUploadBytes + 1); perr != nil {
		return "", nil, perr
	}
	text = r.FormValue("text")

	file, header, ferr := r.FormFile("file")
	if ferr == http.ErrMissingFile {
		// Multipart with no file — treat as a text-only request.
		return text, nil, nil
	}
	if ferr != nil {
		return "", nil, ferr
	}
	defer file.Close()

	data, rerr := io.ReadAll(io.LimitReader(file, maxUploadBytes))
	if rerr != nil {
		return "", nil, rerr
	}
	doc := planops.Document{
		Data:      data,
		MediaType: documentMediaType(header.Header.Get("Content-Type"), header.Filename),
		Filename:  header.Filename,
	}
	return text, []planops.Document{doc}, nil
}

// documentMediaType resolves the document's media type from the part's declared
// Content-Type, falling back to a filename-extension guess (PDFs are the common
// case) and finally application/octet-stream.
func documentMediaType(declared, filename string) string {
	if mt, _, err := mime.ParseMediaType(declared); err == nil && mt != "" && mt != "application/octet-stream" {
		return mt
	}
	if strings.HasSuffix(strings.ToLower(filename), ".pdf") {
		return "application/pdf"
	}
	if declared != "" {
		return declared
	}
	return "application/octet-stream"
}

// icalUpload returns the raw iCalendar bytes from an ingest request — whether
// the .ics arrived as an uploaded file or pasted into the text field — and
// whether it looked like iCal at all.
func icalUpload(text string, docs []planops.Document) ([]byte, bool) {
	for _, d := range docs {
		if looksICal(d.Data) {
			return d.Data, true
		}
	}
	if looksICal([]byte(text)) {
		return []byte(text), true
	}
	return nil, false
}

// looksICal sniffs for the iCalendar BEGIN:VCALENDAR marker near the start of
// the content (so a stray mention deeper in a pasted email doesn't trigger it).
func looksICal(b []byte) bool {
	n := len(b)
	if n > 512 {
		n = 512
	}
	return bytes.Contains(b[:n], []byte("BEGIN:VCALENDAR"))
}

// icalProposals parses an .ics and, if it's from a recognised source (e.g.
// TripIt), maps it deterministically into propose-step proposals. recognised is
// false for a calendar no source-specific mapper handles, so the caller can
// fall back to the LLM.
func icalProposals(data []byte) (out api.IngestResultDTO, recognised bool) {
	cal, err := tripitics.Parse(bytes.NewReader(data))
	if err != nil {
		return api.IngestResultDTO{}, false
	}
	mt, _, ok := tripitics.Map(cal)
	if !ok {
		return api.IngestResultDTO{}, false
	}
	out = api.IngestResultDTO{Proposals: make([]api.ProposedPlanDTO, 0, len(mt.Plans))}
	for _, p := range mt.Plans {
		out.Proposals = append(out.Proposals, icalProposalDTO(p))
	}
	return out, true
}

// icalProposalDTO renders a mapped plan as a propose-step DTO. Unlike
// toProposedPlanDTO (the LLM path, which carries no coordinates), it preserves
// the start/end coordinates and timezones the mapper resolved, so the
// great-circle arc and local-time rendering survive the confirm round-trip.
func icalProposalDTO(in planops.ConfirmPlanInput) api.ProposedPlanDTO {
	dto := api.ProposedPlanDTO{
		Type:            in.Type,
		Title:           in.Title,
		ConfirmationRef: in.ConfirmationRef,
		TicketNumber:    in.TicketNumber,
		Notes:           in.Notes,
		CostAmount:      in.CostAmount,
		CostCurrency:    in.CostCurrency,
		Confidence:      1,
		Parts:           make([]api.PlanPartDTO, 0, len(in.Parts)),
	}
	for i, part := range in.Parts {
		sp := &store.PlanPart{
			Type:         part.Type,
			Seq:          i,
			StartsAt:     part.StartsAt,
			EndsAt:       part.EndsAt,
			StartTZ:      part.StartTZ,
			EndTZ:        part.EndTZ,
			StartLabel:   part.StartLabel,
			StartLat:     part.StartLat,
			StartLon:     part.StartLon,
			StartAddress: part.StartAddress,
			EndLabel:     part.EndLabel,
			EndLat:       part.EndLat,
			EndLon:       part.EndLon,
			EndAddress:   part.EndAddress,
			Status:       part.Status,
		}
		dto.Parts = append(dto.Parts, api.ToPlanPartDTO(sp,
			part.Flight, part.Hotel, part.Train, part.Ground, part.Dining, part.Excursion, nil, nil))
	}
	return dto
}

// ingestTripConfirm commits the confirmed/edited proposals against the trip,
// applying any rebooking supersessions (the new part links supersedes_id; the
// old part is stamped status='cancelled'). Returns the created plans.
func (a *API) ingestTripConfirm(w http.ResponseWriter, r *http.Request) {
	tripID, err := pathID(r, "id")
	if err != nil {
		writeError(w, http.StatusBadRequest, "bad id")
		return
	}
	me := auth.UserFrom(r.Context())
	if err := a.requireTripEdit(r.Context(), tripID, me, w); err != nil {
		return
	}
	var in ingestConfirmReq
	if err := decode(r, &in); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	plans := make([]planops.ConfirmPlanInput, 0, len(in.Plans))
	for _, p := range in.Plans {
		if !validPlanTypes[p.Type] {
			writeError(w, http.StatusBadRequest, "invalid plan type")
			return
		}
		plans = append(plans, toConfirmPlanInput(p))
	}
	deps := planops.Deps{Store: a.Store, Extractor: a.Extractor, Resolver: a.Resolver}
	created, err := planops.Commit(r.Context(), deps, tripID, me.ID, plans)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	out := make([]api.PlanDTO, 0, len(created))
	for _, pl := range created {
		dto, err := a.planDTO(r.Context(), pl.ID, me.ID)
		if err != nil {
			handleStoreErr(w, err)
			return
		}
		out = append(out, dto)
		a.publishPlanUpdated(r.Context(), dto.TripID, dto.ID)
		a.geocodePlanAsync(dto.TripID, dto.ID)
	}
	writeJSON(w, http.StatusOK, out)
}

// toConfirmPlanInput maps the request shape onto the planops commit input,
// reusing toCreatePartPayload (handlers_plans.go) to build per-type satellites.
func toConfirmPlanInput(p ingestConfirmPlanReq) planops.ConfirmPlanInput {
	out := planops.ConfirmPlanInput{
		Type:             p.Type,
		Title:            p.Title,
		ConfirmationRef:  p.ConfirmationRef,
		TicketNumber:     p.TicketNumber,
		Notes:            p.Notes,
		Source:           p.Source,
		CostAmount:       p.CostAmount,
		CostCurrency:     p.CostCurrency,
		PassengerIDs:     p.PassengerIDs,
		SupersedesPartID: p.SupersedesPartID,
	}
	if p.Visibility != nil {
		out.Visibility = &planops.ConfirmVisibility{Mode: p.Visibility.Mode, UserIDs: p.Visibility.UserIDs}
	}
	for _, part := range p.Parts {
		cp := toCreatePartPayload(p.Type, part)
		out.Parts = append(out.Parts, planops.ConfirmPartInput{
			Type:         p.Type,
			Seq:          cp.Seq,
			StartsAt:     cp.StartsAt,
			EndsAt:       cp.EndsAt,
			StartTZ:      cp.StartTZ,
			EndTZ:        cp.EndTZ,
			StartLabel:   cp.StartLabel,
			StartLat:     cp.StartLat,
			StartLon:     cp.StartLon,
			StartAddress: cp.StartAddress,
			EndLabel:     cp.EndLabel,
			EndLat:       cp.EndLat,
			EndLon:       cp.EndLon,
			EndAddress:   cp.EndAddress,
			Status:       cp.Status,
			Flight:       cp.Flight,
			Hotel:        cp.Hotel,
			Train:        cp.Train,
			Ground:       cp.Ground,
			Dining:       cp.Dining,
			Excursion:    cp.Excursion,
		})
	}
	return out
}

// toProposedPlanDTO renders a planops.ProposedPlan as the FE ProposedPlan
// shape, projecting each part through ToPlanPartDTO (ids are 0 — these are not
// yet persisted).
func toProposedPlanDTO(p planops.ProposedPlan) api.ProposedPlanDTO {
	dto := api.ProposedPlanDTO{
		Type:             p.Type,
		Title:            p.Title,
		ConfirmationRef:  p.ConfirmationRef,
		TicketNumber:     p.TicketNumber,
		Notes:            p.Notes,
		CostAmount:       p.CostAmount,
		CostCurrency:     p.CostCurrency,
		Confidence:       p.Confidence,
		Parts:            make([]api.PlanPartDTO, 0, len(p.Parts)),
		SupersedesPartID: p.SupersedesPartID,
	}
	for i, part := range p.Parts {
		sp := &store.PlanPart{
			Type:         part.Type,
			Seq:          i,
			StartsAt:     part.StartsAt,
			EndsAt:       part.EndsAt,
			StartTZ:      part.StartTZ,
			EndTZ:        part.EndTZ,
			StartLabel:   part.StartLabel,
			EndLabel:     part.EndLabel,
			StartAddress: part.StartAddress,
			EndAddress:   part.EndAddress,
			Status:       part.Status,
		}
		dto.Parts = append(dto.Parts, api.ToPlanPartDTO(sp,
			part.Flight, part.Hotel, part.Train, part.Ground, part.Dining, part.Excursion, nil, nil))
	}
	return dto
}
