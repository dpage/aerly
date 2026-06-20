package planops

import "context"

// Document is one binary attachment forwarded to the LLM alongside the prompt
// (typically a PDF ticket). It mirrors emailingest.Document; planops owns its
// own copy so the capture path has no dependency cycle with the email service
// (emailingest depends on planops, never the reverse).
type Document struct {
	Data      []byte
	MediaType string
	Filename  string
}

// FlightFields are the resolver/manual-fallback inputs for a flight part —
// the same surface the email flights path uses.
type FlightFields struct {
	Ident           string
	Date            string // YYYY-MM-DD (departure)
	OriginIATA      string
	DestIATA        string
	DepartTimeLocal string // HH:MM
	ArriveDate      string // YYYY-MM-DD
	ArriveTimeLocal string // HH:MM
}

// HasManualDetails reports whether every field needed to insert the flight
// without provider data is present.
func (f FlightFields) HasManualDetails() bool {
	return f.OriginIATA != "" && f.DestIATA != "" &&
		f.DepartTimeLocal != "" && f.ArriveDate != "" && f.ArriveTimeLocal != ""
}

// ExtractedPart is one timeline entry the extractor pulled from an
// email/paste/upload, of any plan type. Type selects which fields carry
// meaning. StartDate/EndDate are YYYY-MM-DD local; StartTime/EndTime are HH:MM
// 24h local. Confidence is "high"|"medium"|"low".
type ExtractedPart struct {
	Type       string // flight|train|hotel|ground|dining|excursion|meeting|event
	Confidence string

	StartDate  string
	StartTime  string
	EndDate    string
	EndTime    string
	StartLabel string
	EndLabel   string
	// Free-text postal addresses for the start/end of the part, when the
	// source states or implies them (e.g. an airport terminal). Geocoded into
	// coordinates downstream for map markers.
	StartAddress string
	EndAddress   string

	Flight FlightFields // Type=="flight"

	// Hotel (Type=="hotel"). StartDate/EndDate are check-in/out days.
	HotelName string
	Address   string
	Phone     string
	RoomType  string

	// Train (Type=="train").
	Operator  string
	ServiceNo string
	Class     string

	// Ground (Type=="ground").
	Provider string
	Vehicle  string

	// Dining (Type=="dining").
	ReservationName string

	// Excursion (Type=="excursion").
	ExcursionTitle string

	// Meeting (Type=="meeting").
	MeetingLocation  string
	MeetingOrganiser string
	MeetingPlatform  string

	// Event (Type=="event").
	EventPerformer string
	EventCategory  string
	EventVenueArea string
	EventURL       string
}

// ExtractedPlan groups the parts of one booking into a single plan (PRD §6.3:
// one round-trip email → one plan with several parts).
type ExtractedPlan struct {
	Type            string
	Title           string
	ConfirmationRef string
	// TicketNumber is the e-ticket / ticket number when the source states one
	// (issue #22); CostAmount + CostCurrency are the booking total and its ISO
	// 4217 currency. All empty/nil when the source doesn't say.
	TicketNumber string
	CostAmount   *float64
	CostCurrency string
	// Supplier contact block — who the booking is with and how to reach them.
	// Empty when the source doesn't state them.
	SupplierName string
	ContactEmail string
	ContactPhone string
	Website      string
	Parts        []ExtractedPart
}

// Extractor is the LLM seam Propose calls — implemented by
// *emailingest.Extractor. Narrowed to an interface so planops doesn't pull the
// whole ingest service in and so tests can stub it.
type Extractor interface {
	ExtractPlans(ctx context.Context, body string, docs []Document) ([]ExtractedPlan, error)
}
