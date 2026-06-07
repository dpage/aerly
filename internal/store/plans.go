package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// Plan is a booking: the unit of sharing, privacy, passengers, and
// confirmation identity. Its timeline entries are PlanParts; the per-type
// detail lives in a 1:1 satellite selected by Type.
type Plan struct {
	ID              int64
	TripID          int64
	Type            string // flight|train|hotel|ground|dining|excursion
	Title           string
	ConfirmationRef string
	TicketNumber    string // e-ticket / ticket number, when known (issue #22)
	Notes           string
	Source          string   // manual|paste|upload|email
	CostAmount      *float64 // booking total, nil when unknown (issue #22)
	CostCurrency    string   // ISO 4217 code for CostAmount, e.g. "GBP"
	// Supplier contact block — who the booking is with and how to reach them
	// about it. Consistent across every plan type (sits alongside
	// confirmation_ref), distinct from the per-type service detail.
	SupplierName string
	ContactEmail string
	ContactPhone string
	Website      string
	CreatedBy    *int64
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// PlanPart is the spine: one timeline entry — a time range with a start and
// end place, a status, and an optional supersession link. Type-specific data
// hangs off the matching *Detail satellite keyed on the part id.
type PlanPart struct {
	ID           int64
	PlanID       int64
	Type         string // mirror of the owning plan's type, for convenience
	Seq          int
	StartsAt     time.Time
	EndsAt       *time.Time
	StartTZ      string
	EndTZ        string
	StartLabel   string
	StartLat     *float64
	StartLon     *float64
	StartAddress string
	EndLabel     string
	EndLat       *float64
	EndLon       *float64
	EndAddress   string
	Status       string // planned|confirmed|cancelled
	SupersedesID *int64
	DismissedAt  *time.Time
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

// EffectiveAt returns the time the front end sorts/renders a part by:
// COALESCE(actual, estimated, scheduled). For non-flight parts there are no
// estimated/actual times, so it is simply StartsAt; flight parts override via
// their detail (see EffectiveAt on FlightDetail). Kept as a helper so the rule
// lives in one place (mirrors flights.go's COALESCE).
func (p *PlanPart) EffectiveAt() time.Time { return p.StartsAt }

// FlightDetail is the flight-type satellite: the tracker-specific machinery
// (the three time pairs, the rich status enum, airframe ids, poll timestamps).
type FlightDetail struct {
	PlanPartID     int64
	Ident          string
	ICAO24         *string
	Callsign       *string
	ScheduledOut   time.Time
	ScheduledIn    time.Time
	EstimatedOut   *time.Time
	EstimatedIn    *time.Time
	ActualOut      *time.Time
	ActualIn       *time.Time
	OriginIATA     string
	DestIATA       string
	FlightStatus   string
	LastPolledAt   *time.Time
	LastResolvedAt *time.Time
	OriginGate     string
	DestGate       string
	OriginTerminal string
	DestTerminal   string
	AircraftType   string
}

// EffectiveOut / EffectiveIn collapse the three time pairs the way the tracker
// does: prefer actual, then estimated, then scheduled.
func (d *FlightDetail) EffectiveOut() time.Time {
	return coalesceTime(d.ActualOut, d.EstimatedOut, &d.ScheduledOut)
}

func (d *FlightDetail) EffectiveIn() time.Time {
	return coalesceTime(d.ActualIn, d.EstimatedIn, &d.ScheduledIn)
}

// coalesceTime returns the first non-nil time in priority order. The final
// argument is expected to be the always-present scheduled fallback.
func coalesceTime(ts ...*time.Time) time.Time {
	for _, t := range ts {
		if t != nil {
			return *t
		}
	}
	return time.Time{}
}

// HotelDetail is the hotel-type satellite. The actual check-in/out instants
// are the part's StartsAt / EndsAt; StandardCheckin/Checkout are local
// time-of-day hints for the smart-times calc.
type HotelDetail struct {
	PlanPartID       int64
	PropertyName     string
	Address          string
	Phone            string
	RoomType         string
	Guests           *int
	StandardCheckin  *string // "HH:MM" local, nil → default
	StandardCheckout *string
}

// TrainDetail is the train-type satellite.
type TrainDetail struct {
	PlanPartID int64
	Operator   string
	ServiceNo  string
	Coach      string
	Seat       string
	Class      string
	Platform   string
}

// GroundDetail is the ground-transport satellite (pickup/dropoff).
type GroundDetail struct {
	PlanPartID int64
	Provider   string
	Phone      string
	Vehicle    string
	Driver     string
	Pax        *int
}

// DiningDetail is the dining-reservation satellite.
type DiningDetail struct {
	PlanPartID      int64
	PartySize       *int
	ReservationName string
	Phone           string
}

// ExcursionDetail is the excursion/activity satellite.
type ExcursionDetail struct {
	PlanPartID  int64
	Provider    string
	TicketCount *int
}

// CreatePlanPayload bundles a plan plus its parts and per-type details for an
// atomic insert. The detail slices are written according to Type.
type CreatePlanPayload struct {
	TripID          int64
	Type            string
	Title           string
	ConfirmationRef string
	TicketNumber    string
	Notes           string
	Source          string
	CostAmount      *float64
	CostCurrency    string
	SupplierName    string
	ContactEmail    string
	ContactPhone    string
	Website         string
	TripItUID       string // TripIt event UID for .ics imports (else ""); skips re-imported plans
	Parts           []CreatePlanPartPayload
}

// CreatePlanPartPayload is one part to insert under a plan, with at most one
// populated detail matching the plan's type.
type CreatePlanPartPayload struct {
	Seq          int
	StartsAt     time.Time
	EndsAt       *time.Time
	StartTZ      string
	EndTZ        string
	StartLabel   string
	StartLat     *float64
	StartLon     *float64
	StartAddress string
	EndLabel     string
	EndLat       *float64
	EndLon       *float64
	EndAddress   string
	Status       string
	SupersedesID *int64

	Flight    *FlightDetail
	Hotel     *HotelDetail
	Train     *TrainDetail
	Ground    *GroundDetail
	Dining    *DiningDetail
	Excursion *ExcursionDetail
}

// UpdatePlanPayload carries the optionally-set fields of a plan edit. A nil
// pointer leaves that field unchanged (the COALESCE idiom shared with the
// other updaters); clearing cost_amount back to NULL is not supported, mirroring
// how ends_at can't be cleared via UpdatePlanPart.
type UpdatePlanPayload struct {
	Title           *string
	ConfirmationRef *string
	TicketNumber    *string
	Notes           *string
	CostAmount      *float64
	CostCurrency    *string
	SupplierName    *string
	ContactEmail    *string
	ContactPhone    *string
	Website         *string
}

// UpdatePlanPartPayload carries the optionally-set fields of a part edit
// (time/place/status).
type UpdatePlanPartPayload struct {
	StartsAt     *time.Time
	EndsAt       *time.Time
	StartTZ      *string
	EndTZ        *string
	StartLabel   *string
	StartLat     *float64
	StartLon     *float64
	StartAddress *string
	EndLabel     *string
	EndLat       *float64
	EndLon       *float64
	EndAddress   *string
	Status       *string
}

// IsEmpty reports whether the payload sets no fields (so an update would be a
// no-op and can be skipped).
func (p UpdatePlanPartPayload) IsEmpty() bool {
	return p.StartsAt == nil && p.EndsAt == nil && p.StartTZ == nil && p.EndTZ == nil &&
		p.StartLabel == nil && p.StartLat == nil && p.StartLon == nil && p.StartAddress == nil &&
		p.EndLabel == nil && p.EndLat == nil && p.EndLon == nil && p.EndAddress == nil &&
		p.Status == nil
}

// ----- Plan CRUD -----

const planColumns = `id, trip_id, type, title, confirmation_ref, ticket_number, notes, source, cost_amount, cost_currency, supplier_name, contact_email, contact_phone, website, created_by, created_at, updated_at`

func scanPlan(row pgx.Row) (*Plan, error) {
	var p Plan
	err := row.Scan(&p.ID, &p.TripID, &p.Type, &p.Title, &p.ConfirmationRef,
		&p.TicketNumber, &p.Notes, &p.Source, &p.CostAmount, &p.CostCurrency,
		&p.SupplierName, &p.ContactEmail, &p.ContactPhone, &p.Website,
		&p.CreatedBy, &p.CreatedAt, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// CreatePlan inserts a plan, its parts, and the matching detail rows, all in
// one transaction. The detail satellite written per part is the one selected
// by the plan's Type — a CreatePlanPartPayload's other detail pointers are
// ignored.
func (s *Store) CreatePlan(ctx context.Context, in CreatePlanPayload, createdBy int64) (*Plan, error) {
	source := in.Source
	if source == "" {
		source = "manual"
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)

	// Serialize with trip-passenger materialisation (AddTripPassenger): take the
	// trip row lock before inserting so a new plan and a new trip passenger
	// can't cross and miss each other (issue #20).
	if _, err := tx.Exec(ctx, `SELECT 1 FROM trips WHERE id = $1 FOR UPDATE`, in.TripID); err != nil {
		return nil, err
	}

	p, err := scanPlan(tx.QueryRow(ctx, `
		INSERT INTO plans (trip_id, type, title, confirmation_ref, ticket_number, notes, source, cost_amount, cost_currency, supplier_name, contact_email, contact_phone, website, created_by, tripit_uid)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
		RETURNING `+planColumns,
		in.TripID, in.Type, in.Title, in.ConfirmationRef, in.TicketNumber, in.Notes,
		source, in.CostAmount, in.CostCurrency, in.SupplierName, in.ContactEmail,
		in.ContactPhone, in.Website, createdBy, in.TripItUID))
	if err != nil {
		return nil, err
	}
	for _, part := range in.Parts {
		if err := insertPartTx(ctx, tx, p.ID, in.Type, part); err != nil {
			return nil, err
		}
	}
	// A trip's trip-level passengers (issue #20) travel on every plan they may
	// see, so a new plan inherits them as (via_trip) passengers — skipping any
	// passenger the plan is already hidden from. A brand-new plan has no
	// visibility row, so all trip passengers qualify; SetPlanVisibility later
	// reconciles if the creator restricts it.
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id, via_trip)
		 SELECT $1, tp.user_id, true FROM trip_passengers tp
		  WHERE tp.trip_id = $2 AND plan_visible_to_member($1, tp.user_id)
		 ON CONFLICT (plan_id, user_id) DO NOTHING`, p.ID, in.TripID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return p, nil
}

// PlanExistsByTripItUID reports whether the trip already has a plan imported
// from the given TripIt event UID — the dedupe check the .ics importer runs
// before committing each plan. uid must be non-empty.
func (s *Store) PlanExistsByTripItUID(ctx context.Context, tripID int64, uid string) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx,
		`SELECT EXISTS (SELECT 1 FROM plans WHERE trip_id = $1 AND tripit_uid = $2)`,
		tripID, uid).Scan(&exists)
	return exists, err
}

// insertPartTx writes one plan_parts row and the single satellite row dictated
// by planType.
func insertPartTx(ctx context.Context, tx pgx.Tx, planID int64, planType string, in CreatePlanPartPayload) error {
	status := in.Status
	if status == "" {
		status = "planned"
	}
	var partID int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO plan_parts (plan_id, seq, starts_at, ends_at, start_tz, end_tz,
			start_label, start_lat, start_lon, end_label, end_lat, end_lon,
			status, supersedes_id, start_address, end_address)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)
		RETURNING id`,
		planID, in.Seq, in.StartsAt, in.EndsAt, in.StartTZ, in.EndTZ,
		in.StartLabel, in.StartLat, in.StartLon, in.EndLabel, in.EndLat, in.EndLon,
		status, in.SupersedesID, in.StartAddress, in.EndAddress).Scan(&partID); err != nil {
		return err
	}
	return insertDetailTx(ctx, tx, partID, planType, in)
}

func insertDetailTx(ctx context.Context, tx pgx.Tx, partID int64, planType string, in CreatePlanPartPayload) error {
	switch planType {
	case "flight":
		d := in.Flight
		if d == nil {
			d = &FlightDetail{}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO flight_details (plan_part_id, ident, icao24, callsign,
				scheduled_out, scheduled_in, estimated_out, estimated_in,
				actual_out, actual_in, origin_iata, dest_iata, flight_status,
				last_polled_at, last_resolved_at)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,COALESCE(NULLIF($13,''),'Scheduled'),$14,$15)`,
			partID, d.Ident, d.ICAO24, d.Callsign, d.ScheduledOut, d.ScheduledIn,
			d.EstimatedOut, d.EstimatedIn, d.ActualOut, d.ActualIn,
			d.OriginIATA, d.DestIATA, d.FlightStatus, d.LastPolledAt, d.LastResolvedAt)
		return err
	case "hotel":
		d := in.Hotel
		if d == nil {
			d = &HotelDetail{}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO hotel_details (plan_part_id, property_name, address, phone,
				room_type, guests, standard_checkin, standard_checkout)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8)`,
			partID, d.PropertyName, d.Address, d.Phone, d.RoomType, d.Guests,
			d.StandardCheckin, d.StandardCheckout)
		return err
	case "train":
		d := in.Train
		if d == nil {
			d = &TrainDetail{}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO train_details (plan_part_id, operator, service_no, coach, seat, class, platform)
			VALUES ($1,$2,$3,$4,$5,$6,$7)`,
			partID, d.Operator, d.ServiceNo, d.Coach, d.Seat, d.Class, d.Platform)
		return err
	case "ground":
		d := in.Ground
		if d == nil {
			d = &GroundDetail{}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO ground_details (plan_part_id, provider, phone, vehicle, driver, pax)
			VALUES ($1,$2,$3,$4,$5,$6)`,
			partID, d.Provider, d.Phone, d.Vehicle, d.Driver, d.Pax)
		return err
	case "dining":
		d := in.Dining
		if d == nil {
			d = &DiningDetail{}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO dining_details (plan_part_id, party_size, reservation_name, phone)
			VALUES ($1,$2,$3,$4)`,
			partID, d.PartySize, d.ReservationName, d.Phone)
		return err
	case "excursion":
		d := in.Excursion
		if d == nil {
			d = &ExcursionDetail{}
		}
		_, err := tx.Exec(ctx, `
			INSERT INTO excursion_details (plan_part_id, provider, ticket_count)
			VALUES ($1,$2,$3)`,
			partID, d.Provider, d.TicketCount)
		return err
	default:
		return errors.New("unknown plan type: " + planType)
	}
}

// PlanByID returns a single plan by id.
func (s *Store) PlanByID(ctx context.Context, id int64) (*Plan, error) {
	return scanPlan(s.pool.QueryRow(ctx,
		`SELECT `+planColumns+` FROM plans WHERE id = $1`, id))
}

// PlansByTrip returns the plans in a trip, ordered by id.
func (s *Store) PlansByTrip(ctx context.Context, tripID int64) ([]*Plan, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+planColumns+` FROM plans WHERE trip_id = $1 ORDER BY id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Plan
	for rows.Next() {
		p, err := scanPlan(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// TripPartEndpoints returns every geocoded endpoint coordinate of a trip's
// live (non-dismissed) plan parts — both start and end where present — as
// [lat, lon] pairs. Used to derive the trip's country from where its plans
// actually are, rather than from a freeform name. Order is by part then
// start-before-end so the derivation's tie-breaking is deterministic.
func (s *Store) TripPartEndpoints(ctx context.Context, tripID int64) ([][2]float64, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT part.start_lat, part.start_lon, part.end_lat, part.end_lon
		   FROM plan_parts part
		   JOIN plans pl ON pl.id = part.plan_id
		  WHERE pl.trip_id = $1 AND part.dismissed_at IS NULL
		  ORDER BY part.id`, tripID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]float64
	for rows.Next() {
		var sLat, sLon, eLat, eLon *float64
		if err := rows.Scan(&sLat, &sLon, &eLat, &eLon); err != nil {
			return nil, err
		}
		if sLat != nil && sLon != nil {
			out = append(out, [2]float64{*sLat, *sLon})
		}
		if eLat != nil && eLon != nil {
			out = append(out, [2]float64{*eLat, *eLon})
		}
	}
	return out, rows.Err()
}

// UpdatePlan applies the supplied fields to a plan.
func (s *Store) UpdatePlan(ctx context.Context, id int64, in UpdatePlanPayload) (*Plan, error) {
	return scanPlan(s.pool.QueryRow(ctx, `
		UPDATE plans SET
			title            = COALESCE($2, title),
			confirmation_ref = COALESCE($3, confirmation_ref),
			ticket_number    = COALESCE($4, ticket_number),
			notes            = COALESCE($5, notes),
			cost_amount      = COALESCE($6, cost_amount),
			cost_currency    = COALESCE($7, cost_currency),
			supplier_name    = COALESCE($8, supplier_name),
			contact_email    = COALESCE($9, contact_email),
			contact_phone    = COALESCE($10, contact_phone),
			website          = COALESCE($11, website),
			updated_at       = NOW()
		WHERE id = $1
		RETURNING `+planColumns,
		id, in.Title, in.ConfirmationRef, in.TicketNumber, in.Notes, in.CostAmount, in.CostCurrency,
		in.SupplierName, in.ContactEmail, in.ContactPhone, in.Website))
}

// DeletePlan removes a plan and its parts/details (cascade).
func (s *Store) DeletePlan(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM plans WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MovePlan reassigns a plan (and, by FK, its parts/passengers/visibility) to
// another trip. Only the plans.trip_id changes; the parts, plan_passengers and
// plan_visibility rows reference the plan, so they travel with it implicitly.
// Visibility is thereafter evaluated against the destination trip's membership
// by the §4 predicate — any passenger or visibility-member who isn't on the new
// trip simply goes inert (the predicate's trip_members gate fails first).
func (s *Store) MovePlan(ctx context.Context, planID, destTripID int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE plans SET trip_id = $2, updated_at = NOW() WHERE id = $1`,
		planID, destTripID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LinkableType reports whether a plan type may hold a multi-leg booking that can
// be linked or split (flights, trains and ground transport have
// outbound/return/connection legs). Other types are single-venue and are
// excluded from link/split.
func LinkableType(t string) bool { return t == "flight" || t == "train" || t == "ground" }

// LinkPlans folds the absorbed plans' parts into the primary plan, making one
// multi-part booking (issue #12). All plans must be in the same trip and share
// one link/split-eligible type (flight|train). The primary's title,
// confirmation_ref, notes, passengers and visibility win — the absorbed plans
// are deleted. Per-type details and live tracking (positions) travel with each
// part automatically (they key on plan_part_id); flight_alerts.plan_id is
// repointed first so deleting the absorbed plans leaves no dangling reference.
func (s *Store) LinkPlans(ctx context.Context, primaryID int64, absorbIDs []int64) error {
	if len(absorbIDs) == 0 {
		return errors.New("no plans to link")
	}
	for _, id := range absorbIDs {
		if id == primaryID {
			return errors.New("cannot link a plan to itself")
		}
	}
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock every involved plan row in one id-ordered query, so two concurrent
	// inverse link requests (A absorbs B vs B absorbs A) can't deadlock by taking
	// the row locks in opposite order.
	type planRow struct {
		tripID int64
		typ    string
	}
	ids := append([]int64{primaryID}, absorbIDs...)
	rows, err := tx.Query(ctx,
		`SELECT id, trip_id, type FROM plans WHERE id = ANY($1) ORDER BY id FOR UPDATE`, ids)
	if err != nil {
		return err
	}
	byID := map[int64]planRow{}
	for rows.Next() {
		var id, tripID int64
		var typ string
		if err := rows.Scan(&id, &tripID, &typ); err != nil {
			rows.Close()
			return err
		}
		byID[id] = planRow{tripID: tripID, typ: typ}
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	primary, ok := byID[primaryID]
	if !ok {
		return ErrNotFound
	}
	if !LinkableType(primary.typ) {
		return fmt.Errorf("plan type %q cannot be linked", primary.typ)
	}
	// Validate every absorbed plan: it exists, is in the same trip, same type.
	for _, id := range absorbIDs {
		a, ok := byID[id]
		if !ok {
			return ErrNotFound
		}
		if a.tripID != primary.tripID {
			return fmt.Errorf("plan %d is not in the same trip", id)
		}
		if a.typ != primary.typ {
			return fmt.Errorf("plan %d has type %q, not %q", id, a.typ, primary.typ)
		}
	}
	// Repoint alerts (they carry plan_id with no FK) before re-parenting parts
	// and deleting the now-empty absorbed plans.
	if _, err := tx.Exec(ctx,
		`UPDATE flight_alerts SET plan_id = $1 WHERE plan_id = ANY($2)`, primaryID, absorbIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE plan_parts SET plan_id = $1, updated_at = NOW() WHERE plan_id = ANY($2)`, primaryID, absorbIDs); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `DELETE FROM plans WHERE id = ANY($1)`, absorbIDs); err != nil {
		return err
	}
	if err := resequencePartsTx(ctx, tx, primaryID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `UPDATE plans SET updated_at = NOW() WHERE id = $1`, primaryID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// SplitPlanPart moves one part out of its plan into a brand-new plan in the same
// trip, so a mis-grouped booking can be separated (issue #12). The new plan
// copies the parent's type, source, title, confirmation_ref, ticket_number,
// cost and notes, and —
// crucially — its passengers and visibility, so the split-out leg keeps the exact
// same audience (a split must never widen privacy). Returns the new and parent
// plan ids. Returns ErrNotSplittable when the plan has one or zero live parts
// (nothing to split) or its type is not link/split-eligible.
func (s *Store) SplitPlanPart(ctx context.Context, partID int64) (newPlanID, parentPlanID int64, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, 0, err
	}
	defer tx.Rollback(ctx)

	var parent Plan
	err = tx.QueryRow(ctx, `
		SELECT pl.id, pl.trip_id, pl.type, pl.title, pl.confirmation_ref,
		       pl.ticket_number, pl.notes, pl.source, pl.cost_amount, pl.cost_currency,
		       pl.supplier_name, pl.contact_email, pl.contact_phone, pl.website,
		       pl.created_by, pl.created_at, pl.updated_at
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.id = $1
		FOR UPDATE OF pl`, partID).Scan(
		&parent.ID, &parent.TripID, &parent.Type, &parent.Title, &parent.ConfirmationRef,
		&parent.TicketNumber, &parent.Notes, &parent.Source, &parent.CostAmount, &parent.CostCurrency,
		&parent.SupplierName, &parent.ContactEmail, &parent.ContactPhone, &parent.Website,
		&parent.CreatedBy, &parent.CreatedAt, &parent.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	if err != nil {
		return 0, 0, err
	}
	if !LinkableType(parent.Type) {
		return 0, 0, ErrNotSplittable
	}
	var liveCount int
	if err := tx.QueryRow(ctx,
		`SELECT COUNT(*) FROM plan_parts WHERE plan_id = $1 AND dismissed_at IS NULL`,
		parent.ID).Scan(&liveCount); err != nil {
		return 0, 0, err
	}
	if liveCount <= 1 {
		return 0, 0, ErrNotSplittable
	}
	// New plan inherits the parent's identity fields (a copy — the user edits the
	// split-out booking afterward).
	if err := tx.QueryRow(ctx, `
		INSERT INTO plans (trip_id, type, title, confirmation_ref, ticket_number, notes, source, cost_amount, cost_currency, supplier_name, contact_email, contact_phone, website, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14) RETURNING id`,
		parent.TripID, parent.Type, parent.Title, parent.ConfirmationRef, parent.TicketNumber,
		parent.Notes, parent.Source, parent.CostAmount, parent.CostCurrency,
		parent.SupplierName, parent.ContactEmail, parent.ContactPhone, parent.Website,
		parent.CreatedBy).Scan(&newPlanID); err != nil {
		return 0, 0, err
	}
	// Copy visibility (mode + members) BEFORE passengers so the new plan's
	// audience exactly matches the parent's — never widening it.
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_visibility (plan_id, mode)
		 SELECT $1, mode FROM plan_visibility WHERE plan_id = $2`, newPlanID, parent.ID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_visibility_members (plan_id, user_id)
		 SELECT $1, user_id FROM plan_visibility_members WHERE plan_id = $2`, newPlanID, parent.ID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id, via_trip, added_at)
		 SELECT $1, user_id, via_trip, added_at FROM plan_passengers WHERE plan_id = $2`, newPlanID, parent.ID); err != nil {
		return 0, 0, err
	}
	// Move the part and its alerts onto the new plan.
	if _, err := tx.Exec(ctx,
		`UPDATE flight_alerts SET plan_id = $1 WHERE plan_part_id = $2`, newPlanID, partID); err != nil {
		return 0, 0, err
	}
	if _, err := tx.Exec(ctx,
		`UPDATE plan_parts SET plan_id = $1, updated_at = NOW() WHERE id = $2`, newPlanID, partID); err != nil {
		return 0, 0, err
	}
	if err := resequencePartsTx(ctx, tx, parent.ID); err != nil {
		return 0, 0, err
	}
	if err := resequencePartsTx(ctx, tx, newPlanID); err != nil {
		return 0, 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, 0, err
	}
	return newPlanID, parent.ID, nil
}

// resequencePartsTx renumbers a plan's parts seq=0..n by start time so a freshly
// merged or split plan reads in chronological order. Dismissed parts keep their
// rows but sort after the live ones.
func resequencePartsTx(ctx context.Context, tx pgx.Tx, planID int64) error {
	_, err := tx.Exec(ctx, `
		WITH ordered AS (
			SELECT id, ROW_NUMBER() OVER (
				ORDER BY (dismissed_at IS NOT NULL), starts_at, id) - 1 AS rn
			FROM plan_parts WHERE plan_id = $1)
		UPDATE plan_parts p SET seq = o.rn
		FROM ordered o WHERE p.id = o.id AND p.seq <> o.rn`, planID)
	return err
}

// ----- Plan parts -----

const planPartColumns = `part.id, part.plan_id, pl.type, part.seq, part.starts_at,
	part.ends_at, part.start_tz, part.end_tz, part.start_label, part.start_lat,
	part.start_lon, part.end_label, part.end_lat, part.end_lon, part.status,
	part.supersedes_id, part.dismissed_at, part.created_at, part.updated_at,
	part.start_address, part.end_address`

func scanPart(row pgx.Row) (*PlanPart, error) {
	var p PlanPart
	err := row.Scan(&p.ID, &p.PlanID, &p.Type, &p.Seq, &p.StartsAt,
		&p.EndsAt, &p.StartTZ, &p.EndTZ, &p.StartLabel, &p.StartLat,
		&p.StartLon, &p.EndLabel, &p.EndLat, &p.EndLon, &p.Status,
		&p.SupersedesID, &p.DismissedAt, &p.CreatedAt, &p.UpdatedAt,
		&p.StartAddress, &p.EndAddress)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// PartsByPlan returns the parts of a plan, ordered by seq then start.
func (s *Store) PartsByPlan(ctx context.Context, planID int64) ([]*PlanPart, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+planPartColumns+`
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.plan_id = $1
		ORDER BY part.seq, part.starts_at`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlanPart
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PartsNeedingTZ returns non-dismissed parts that have a coordinate but a
// still-empty start or end timezone — the candidates for one-off tz anchoring
// of historical rows ingested before coordinate-based tz resolution existed.
func (s *Store) PartsNeedingTZ(ctx context.Context) ([]*PlanPart, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT `+planPartColumns+`
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.dismissed_at IS NULL
		  AND ((part.start_tz = '' AND part.start_lat IS NOT NULL)
		    OR (part.end_tz = '' AND part.ends_at IS NOT NULL
		        AND (part.end_lat IS NOT NULL OR part.start_lat IS NOT NULL)))
		ORDER BY part.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlanPart
	for rows.Next() {
		p, err := scanPart(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// PlanIDsNeedingGeocode returns the distinct plan ids that have at least one
// non-dismissed, non-flight part with a free-text address OR a place label but
// no coordinates — i.e. parts that could be plotted on the map once geocoded.
// Used by the startup backfill to fill plans ingested before geocoding existed
// (or while it was unavailable). The label is included because a transfer's
// airport endpoint often arrives as a bare name ("Alicante Airport") with no
// address — without it those endpoints would never be backfilled. Flights are
// excluded: their IATA-code labels are located via the airport table / poller.
// Idempotent: once a part has coordinates it stops matching.
func (s *Store) PlanIDsNeedingGeocode(ctx context.Context) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT DISTINCT part.plan_id
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.dismissed_at IS NULL
		  AND pl.type <> 'flight'
		  AND ((part.start_lat IS NULL AND (part.start_address <> ''
		        OR part.start_label ILIKE '%airport%' OR part.start_label ILIKE '%terminal%'))
		    OR (part.end_lat IS NULL AND (part.end_address <> ''
		        OR part.end_label ILIKE '%airport%' OR part.end_label ILIKE '%terminal%')))
		ORDER BY part.plan_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

// PlanPartByID returns a single part by id (with its owning plan's type).
func (s *Store) PlanPartByID(ctx context.Context, id int64) (*PlanPart, error) {
	return scanPart(s.pool.QueryRow(ctx, `
		SELECT `+planPartColumns+`
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.id = $1`, id))
}

// PlanIDForPart returns the plan id (and trip id) owning a part, for
// authorization. Returns ErrNotFound when the part doesn't exist.
func (s *Store) PlanIDForPart(ctx context.Context, partID int64) (planID, tripID int64, err error) {
	err = s.pool.QueryRow(ctx, `
		SELECT pl.id, pl.trip_id
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		WHERE part.id = $1`, partID).Scan(&planID, &tripID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNotFound
	}
	return planID, tripID, err
}

// UpdatePlanPart applies the supplied fields to a part (time/place/status).
func (s *Store) UpdatePlanPart(ctx context.Context, id int64, in UpdatePlanPartPayload) (*PlanPart, error) {
	tag, err := s.pool.Exec(ctx, `
		UPDATE plan_parts SET
			starts_at   = COALESCE($2, starts_at),
			ends_at     = CASE WHEN $3::boolean THEN $4 ELSE ends_at END,
			start_tz    = COALESCE($5, start_tz),
			end_tz      = COALESCE($6, end_tz),
			start_label = COALESCE($7, start_label),
			start_lat   = CASE WHEN $8::boolean THEN $9 ELSE start_lat END,
			start_lon   = CASE WHEN $10::boolean THEN $11 ELSE start_lon END,
			end_label   = COALESCE($12, end_label),
			end_lat     = CASE WHEN $13::boolean THEN $14 ELSE end_lat END,
			end_lon     = CASE WHEN $15::boolean THEN $16 ELSE end_lon END,
			status        = COALESCE($17, status),
			start_address = COALESCE($18, start_address),
			end_address   = COALESCE($19, end_address),
			updated_at  = NOW()
		WHERE id = $1`,
		id, in.StartsAt,
		in.EndsAt != nil, in.EndsAt,
		in.StartTZ, in.EndTZ, in.StartLabel,
		in.StartLat != nil, in.StartLat,
		in.StartLon != nil, in.StartLon,
		in.EndLabel,
		in.EndLat != nil, in.EndLat,
		in.EndLon != nil, in.EndLon,
		in.Status, in.StartAddress, in.EndAddress)
	if err != nil {
		return nil, err
	}
	if tag.RowsAffected() == 0 {
		return nil, ErrNotFound
	}
	return s.PlanPartByID(ctx, id)
}

// DismissPlanPart stamps dismissed_at so a superseded part drops off the
// timeline.
func (s *Store) DismissPlanPart(ctx context.Context, id int64) error {
	tag, err := s.pool.Exec(ctx,
		`UPDATE plan_parts SET dismissed_at = NOW(), updated_at = NOW() WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ----- Per-type detail loaders -----

// FlightDetailFor loads the flight satellite for a part, or (nil, nil) if none.
func (s *Store) FlightDetailFor(ctx context.Context, partID int64) (*FlightDetail, error) {
	var d FlightDetail
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, ident, icao24, callsign, scheduled_out, scheduled_in,
			estimated_out, estimated_in, actual_out, actual_in, origin_iata,
			dest_iata, flight_status, last_polled_at, last_resolved_at,
			COALESCE(origin_gate,''), COALESCE(dest_gate,''),
			COALESCE(origin_terminal,''), COALESCE(dest_terminal,''),
			COALESCE(aircraft_type,'')
		FROM flight_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.Ident, &d.ICAO24, &d.Callsign, &d.ScheduledOut,
		&d.ScheduledIn, &d.EstimatedOut, &d.EstimatedIn, &d.ActualOut, &d.ActualIn,
		&d.OriginIATA, &d.DestIATA, &d.FlightStatus, &d.LastPolledAt, &d.LastResolvedAt,
		&d.OriginGate, &d.DestGate, &d.OriginTerminal, &d.DestTerminal,
		&d.AircraftType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil // genuine "no satellite"
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// HotelDetailFor loads the hotel satellite for a part, or (nil, nil) if none.
func (s *Store) HotelDetailFor(ctx context.Context, partID int64) (*HotelDetail, error) {
	var d HotelDetail
	var ci, co *time.Time
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, property_name, address, phone, room_type, guests,
			standard_checkin, standard_checkout
		FROM hotel_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.PropertyName, &d.Address, &d.Phone, &d.RoomType,
		&d.Guests, &ci, &co)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}
	if err != nil {
		return nil, err
	}
	d.StandardCheckin = formatTimeOfDay(ci)
	d.StandardCheckout = formatTimeOfDay(co)
	return &d, nil
}

// formatTimeOfDay renders a pg TIME (decoded as a time.Time on the zero date)
// as "HH:MM", or nil when absent.
func formatTimeOfDay(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := t.Format("15:04")
	return &s
}

// TrainDetailFor loads the train satellite for a part, or (nil, nil) if none.
func (s *Store) TrainDetailFor(ctx context.Context, partID int64) (*TrainDetail, error) {
	var d TrainDetail
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, operator, service_no, coach, seat, class, platform
		FROM train_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.Operator, &d.ServiceNo, &d.Coach, &d.Seat, &d.Class, &d.Platform)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// GroundDetailFor loads the ground satellite for a part, or (nil, nil) if none.
func (s *Store) GroundDetailFor(ctx context.Context, partID int64) (*GroundDetail, error) {
	var d GroundDetail
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, provider, phone, vehicle, driver, pax
		FROM ground_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.Provider, &d.Phone, &d.Vehicle, &d.Driver, &d.Pax)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// DiningDetailFor loads the dining satellite for a part, or (nil, nil) if none.
func (s *Store) DiningDetailFor(ctx context.Context, partID int64) (*DiningDetail, error) {
	var d DiningDetail
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, party_size, reservation_name, phone
		FROM dining_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.PartySize, &d.ReservationName, &d.Phone)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ExcursionDetailFor loads the excursion satellite for a part, or (nil, nil).
func (s *Store) ExcursionDetailFor(ctx context.Context, partID int64) (*ExcursionDetail, error) {
	var d ExcursionDetail
	err := s.pool.QueryRow(ctx, `
		SELECT plan_part_id, provider, ticket_count
		FROM excursion_details WHERE plan_part_id = $1`, partID).Scan(
		&d.PlanPartID, &d.Provider, &d.TicketCount)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil //nolint:nilnil
	}
	if err != nil {
		return nil, err
	}
	return &d, nil
}

// ----- Passengers (the trigger keeps trip_members in sync) -----

// AddPlanPassenger adds a passenger to a plan. The DB trigger ensures the
// matching trip_members viewer row.
func (s *Store) AddPlanPassenger(ctx context.Context, planID, userID int64) error {
	// An explicit per-plan add is "manual" (via_trip = false): it always sees
	// the plan and is never auto-removed when the plan is hidden, even if the
	// row was originally materialised from a trip-level passenger (#20).
	_, err := s.pool.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id, via_trip) VALUES ($1, $2, false)
		 ON CONFLICT (plan_id, user_id) DO UPDATE SET via_trip = false`, planID, userID)
	return err
}

// RemovePlanPassenger drops an explicit per-plan passenger assignment. If the
// user is still a trip-level passenger (#20) and the plan is visible to them,
// the row is kept and re-derived as trip-level (via_trip = true) rather than
// deleted — you can't evict a trip passenger from a non-hidden plan one plan at
// a time; remove the trip passenger (or hide the plan) for that. Otherwise the
// row is deleted. The trip membership is left intact either way (once on the
// trip, they stay a viewer). Returns ErrNotFound when no passenger row exists.
func (s *Store) RemovePlanPassenger(ctx context.Context, planID, userID int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM trips t JOIN plans p ON p.trip_id = t.id WHERE p.id = $1 FOR UPDATE OF t`,
		planID); err != nil {
		return err
	}
	// Still a trip passenger on a plan they may see → keep the row as
	// trip-derived (drops only the manual override).
	tag, err := tx.Exec(ctx, `
		UPDATE plan_passengers SET via_trip = true
		 WHERE plan_id = $1 AND user_id = $2
		   AND plan_visible_to_member($1, $2)
		   AND EXISTS (
		     SELECT 1 FROM trip_passengers tp
		     JOIN plans pl ON pl.id = $1
		     WHERE tp.trip_id = pl.trip_id AND tp.user_id = $2)`, planID, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() > 0 {
		return tx.Commit(ctx)
	}
	// Otherwise drop the assignment outright.
	del, err := tx.Exec(ctx,
		`DELETE FROM plan_passengers WHERE plan_id = $1 AND user_id = $2`, planID, userID)
	if err != nil {
		return err
	}
	if del.RowsAffected() == 0 {
		return ErrNotFound
	}
	return tx.Commit(ctx)
}

// PassengersByPlan returns a plan_id → []user_id map for the given plans. Plans
// with no passengers are absent from the map.
func (s *Store) PassengersByPlan(ctx context.Context, planIDs []int64) (map[int64][]int64, error) {
	out := map[int64][]int64{}
	if len(planIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT plan_id, user_id FROM plan_passengers WHERE plan_id = ANY($1) ORDER BY plan_id, user_id`,
		planIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var pid, uid int64
		if err := rows.Scan(&pid, &uid); err != nil {
			return nil, err
		}
		out[pid] = append(out[pid], uid)
	}
	return out, rows.Err()
}

// IsTripPassenger reports whether userID is a passenger on any plan in tripID
// (i.e. they're travelling on the trip, not merely a shared viewer). The trip
// list uses it to file passenger trips under "My trips" and badge them
// (issue #19).
func (s *Store) IsTripPassenger(ctx context.Context, tripID, userID int64) (bool, error) {
	var ok bool
	// A passenger on any of the trip's plans, OR a trip-level passenger (#20).
	// The latter covers a passenger on a trip whose plans are all hidden from
	// them, or an empty trip, so it still files under their "My trips".
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM plan_passengers pp
			JOIN plans pl ON pl.id = pp.plan_id
			WHERE pl.trip_id = $1 AND pp.user_id = $2)
		    OR EXISTS (
			SELECT 1 FROM trip_passengers tp
			WHERE tp.trip_id = $1 AND tp.user_id = $2)`, tripID, userID).Scan(&ok)
	return ok, err
}

// PlanOwners returns the creator (owner) user id for each plan id, in one
// query. Used to label tracker parts with who added them.
func (s *Store) PlanOwners(ctx context.Context, planIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(planIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT id, created_by FROM plans WHERE id = ANY($1)`, planIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var planID int64
		var ownerID *int64
		if err := rows.Scan(&planID, &ownerID); err != nil {
			return nil, err
		}
		if ownerID != nil {
			out[planID] = *ownerID
		}
	}
	return out, rows.Err()
}

// TripOwnersByPlan returns the owner (creator) user id of each plan's containing
// trip, keyed by plan id, in one query. The map hashes it to a per-person
// colour so each person's trips share a hue (issue #13). Plans whose trip has a
// NULL created_by are omitted.
func (s *Store) TripOwnersByPlan(ctx context.Context, planIDs []int64) (map[int64]int64, error) {
	out := map[int64]int64{}
	if len(planIDs) == 0 {
		return out, nil
	}
	rows, err := s.pool.Query(ctx,
		`SELECT p.id, t.created_by
		   FROM plans p
		   JOIN trips t ON t.id = p.trip_id
		  WHERE p.id = ANY($1)`, planIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var planID int64
		var ownerID *int64
		if err := rows.Scan(&planID, &ownerID); err != nil {
			return nil, err
		}
		if ownerID != nil {
			out[planID] = *ownerID
		}
	}
	return out, rows.Err()
}

// ----- Per-plan visibility -----

// PlanVisibility is the per-plan privacy override. A nil result (ErrNotFound)
// means the default "everyone on the trip".
type PlanVisibility struct {
	PlanID  int64
	Mode    string // hidden_from|only_visible_to
	UserIDs []int64
}

// PlanVisibilityFor returns the per-plan visibility row, or ErrNotFound when
// the plan uses the default everyone-on-trip rule.
func (s *Store) PlanVisibilityFor(ctx context.Context, planID int64) (*PlanVisibility, error) {
	v := PlanVisibility{PlanID: planID}
	err := s.pool.QueryRow(ctx,
		`SELECT mode FROM plan_visibility WHERE plan_id = $1`, planID).Scan(&v.Mode)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, err := s.pool.Query(ctx,
		`SELECT user_id FROM plan_visibility_members WHERE plan_id = $1 ORDER BY user_id`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		v.UserIDs = append(v.UserIDs, uid)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return &v, nil
}

// SetPlanVisibility writes the parent mode row and member list atomically. An
// empty mode clears the override (back to everyone-on-trip); the
// plan_visibility_members rows cascade away with the parent. The structure
// (one mode per plan_id parent row) makes a mixed-mode plan unrepresentable, so
// no app-side mode enforcement is needed.
func (s *Store) SetPlanVisibility(ctx context.Context, planID int64, mode string, userIDs []int64) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	// Lock the trip so the visibility change + trip-passenger reconcile below is
	// serialized against concurrent plan creation / passenger adds (issue #20).
	if _, err := tx.Exec(ctx,
		`SELECT 1 FROM trips t JOIN plans p ON p.trip_id = t.id WHERE p.id = $1 FOR UPDATE OF t`,
		planID); err != nil {
		return err
	}

	// Clearing or replacing always starts by dropping the existing parent row
	// (members cascade), so the write is idempotent and never leaves a stale
	// mode or member.
	if _, err := tx.Exec(ctx, `DELETE FROM plan_visibility WHERE plan_id = $1`, planID); err != nil {
		return err
	}
	if mode != "" {
		if _, err := tx.Exec(ctx,
			`INSERT INTO plan_visibility (plan_id, mode) VALUES ($1, $2)`, planID, mode); err != nil {
			return err
		}
		for _, uid := range userIDs {
			if _, err := tx.Exec(ctx,
				`INSERT INTO plan_visibility_members (plan_id, user_id) VALUES ($1, $2)
				 ON CONFLICT DO NOTHING`, planID, uid); err != nil {
				return err
			}
		}
	}

	// Reconcile trip-level passengers (issue #20) against the new visibility:
	// materialise the trip's passengers onto this plan if they may now see it,
	// and drop the via_trip rows of any passenger it's now hidden from. Manual
	// (via_trip = false) rows are never touched.
	if _, err := tx.Exec(ctx,
		`INSERT INTO plan_passengers (plan_id, user_id, via_trip)
		 SELECT $1, tp.user_id, true
		   FROM trip_passengers tp
		   JOIN plans p ON p.id = $1 AND p.trip_id = tp.trip_id
		  WHERE plan_visible_to_member($1, tp.user_id)
		 ON CONFLICT (plan_id, user_id) DO NOTHING`, planID); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx,
		`DELETE FROM plan_passengers
		  WHERE plan_id = $1 AND via_trip = true
		    AND NOT plan_visible_to_member($1, user_id)`, planID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// ----- Visibility predicate (implemented now — spec §4) -----

// canViewPlanPredicate is the SQL fragment of the spec §4 plan-visibility
// rule, parameterised on $1 = planID, $2 = viewerID. It is shared by
// CanViewPlan and ListVisiblePlanParts so the rule lives in exactly one place
// (replacing the three duplicated flight predicates).
//
// A viewer V can see plan P in trip T when V is on T (or owns it) AND P is not
// hidden from V — the creator, passengers, and the trip owner are granted
// before plan_visibility is consulted, so a stray hidden_from row naming one of
// them is inert.
const canViewPlanPredicate = `
	EXISTS (
		SELECT 1 FROM plans p
		JOIN trips t ON t.id = p.trip_id
		WHERE p.id = $1
		  AND (
		       t.created_by = $2
		    OR (
		         EXISTS (SELECT 1 FROM trip_members tm
		                 WHERE tm.trip_id = p.trip_id AND tm.user_id = $2)
		         AND (
		              p.created_by = $2
		           OR EXISTS (SELECT 1 FROM plan_passengers pp
		                      WHERE pp.plan_id = p.id AND pp.user_id = $2)
		           OR NOT EXISTS (SELECT 1 FROM plan_visibility pv
		                          WHERE pv.plan_id = p.id)
		           OR EXISTS (SELECT 1 FROM plan_visibility pv
		                      WHERE pv.plan_id = p.id
		                        AND pv.mode = 'hidden_from'
		                        AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                        WHERE m.plan_id = p.id AND m.user_id = $2))
		           OR EXISTS (SELECT 1 FROM plan_visibility pv
		                      JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                      WHERE pv.plan_id = p.id
		                        AND pv.mode = 'only_visible_to'
		                        AND m.user_id = $2)
		         )
		       )
		  )
	)`

// CanViewPlan reports whether viewerID may see planID under the spec §4
// predicate. showAllForSuperuser keeps the existing superuser bypass: when
// true (caller must verify the session is a superuser opting in), it is a mere
// existence check so a missing plan still returns false.
func (s *Store) CanViewPlan(ctx context.Context, planID, viewerID int64, showAllForSuperuser bool) (bool, error) {
	if showAllForSuperuser {
		var n int
		err := s.pool.QueryRow(ctx,
			`SELECT 1 FROM plans WHERE id = $1`, planID).Scan(&n)
		if err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return false, nil
			}
			return false, err
		}
		return true, nil
	}
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT `+canViewPlanPredicate, planID, viewerID).Scan(&ok)
	return ok, err
}

// ListVisiblePlanPartsOpts narrows ListVisiblePlanParts. A nil time bound is
// open-ended; TripID==0 means "any trip the viewer can see".
type ListVisiblePlanPartsOpts struct {
	TripID              int64
	ShowAllForSuperuser bool
	IncludeDismissed    bool
	// Type, when non-empty, restricts to plans of that type (e.g. "flight"
	// for the tracker).
	Type string
}

// ListVisiblePlanParts returns the parts the viewer is allowed to see (their
// plan passes the §4 predicate), newest-startable last. Bodies for the join to
// satellite details are filled in by the feature waves; the visibility gate is
// authoritative here.
func (s *Store) ListVisiblePlanParts(ctx context.Context, viewerID int64, opts ListVisiblePlanPartsOpts) ([]*PlanPart, error) {
	conds := []string{}
	args := []any{viewerID}
	// The predicate keys on $1=planID, $2=viewerID; here viewerID is $1 and we
	// correlate planID to the outer row, so we inline an adapted form rather
	// than reuse canViewPlanPredicate verbatim.
	visible := `(
		t.created_by = $1
	 OR (
		  EXISTS (SELECT 1 FROM trip_members tm
		          WHERE tm.trip_id = pl.trip_id AND tm.user_id = $1)
		  AND (
		       pl.created_by = $1
		    OR EXISTS (SELECT 1 FROM plan_passengers pp
		               WHERE pp.plan_id = pl.id AND pp.user_id = $1)
		    OR NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = pl.id)
		    OR EXISTS (SELECT 1 FROM plan_visibility pv
		               WHERE pv.plan_id = pl.id AND pv.mode = 'hidden_from'
		                 AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                 WHERE m.plan_id = pl.id AND m.user_id = $1))
		    OR EXISTS (SELECT 1 FROM plan_visibility pv
		               JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		               WHERE pv.plan_id = pl.id AND pv.mode = 'only_visible_to'
		                 AND m.user_id = $1)
		  )
		)
	)`
	if !opts.ShowAllForSuperuser {
		conds = append(conds, visible)
	}
	if opts.TripID != 0 {
		args = append(args, opts.TripID)
		conds = append(conds, `pl.trip_id = $`+strconv.Itoa(len(args)))
	}
	if opts.Type != "" {
		args = append(args, opts.Type)
		conds = append(conds, `pl.type = $`+strconv.Itoa(len(args)))
	}
	if !opts.IncludeDismissed {
		conds = append(conds, `part.dismissed_at IS NULL`)
	}
	q := `SELECT part.id, part.plan_id, pl.type, part.seq, part.starts_at,
		part.ends_at, part.start_tz, part.end_tz,
		part.start_label, part.start_lat, part.start_lon,
		part.end_label, part.end_lat, part.end_lon,
		part.status, part.supersedes_id, part.dismissed_at,
		part.created_at, part.updated_at,
		part.start_address, part.end_address
		FROM plan_parts part
		JOIN plans pl ON pl.id = part.plan_id
		JOIN trips t ON t.id = pl.trip_id`
	if len(conds) > 0 {
		q += " WHERE " + strings.Join(conds, " AND ")
	}
	q += " ORDER BY part.starts_at ASC"
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*PlanPart
	for rows.Next() {
		var p PlanPart
		if err := rows.Scan(&p.ID, &p.PlanID, &p.Type, &p.Seq, &p.StartsAt,
			&p.EndsAt, &p.StartTZ, &p.EndTZ,
			&p.StartLabel, &p.StartLat, &p.StartLon,
			&p.EndLabel, &p.EndLat, &p.EndLon,
			&p.Status, &p.SupersedesID, &p.DismissedAt,
			&p.CreatedAt, &p.UpdatedAt,
			&p.StartAddress, &p.EndAddress); err != nil {
			return nil, err
		}
		out = append(out, &p)
	}
	return out, rows.Err()
}

// VisiblePlanUserIDs returns the user IDs that can see the plan through any
// non-superuser path — used by publishers to populate the VisibleTo set on
// SSE events. It is the set form of the §4 predicate: trip owner + every trip
// member who passes the per-plan rule, unioned with passengers and the plan
// creator (who are always granted).
//
// Named VisiblePlanUserIDs (the flight-keyed VisibleUserIDs it once shared the
// concept with was retired with the legacy flight surface in Wave 3). Callers
// fanning out plan-part SSE events should call this.
func (s *Store) VisiblePlanUserIDs(ctx context.Context, planID int64) ([]int64, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT u.id FROM users u
		JOIN plans p ON p.id = $1
		WHERE
		     u.id = p.created_by
		  OR EXISTS (SELECT 1 FROM trips t WHERE t.id = p.trip_id AND t.created_by = u.id)
		  OR EXISTS (SELECT 1 FROM plan_passengers pp WHERE pp.plan_id = p.id AND pp.user_id = u.id)
		  OR (
		       EXISTS (SELECT 1 FROM trip_members tm WHERE tm.trip_id = p.trip_id AND tm.user_id = u.id)
		       AND (
		            NOT EXISTS (SELECT 1 FROM plan_visibility pv WHERE pv.plan_id = p.id)
		         OR EXISTS (SELECT 1 FROM plan_visibility pv
		                    WHERE pv.plan_id = p.id AND pv.mode = 'hidden_from'
		                      AND NOT EXISTS (SELECT 1 FROM plan_visibility_members m
		                                      WHERE m.plan_id = p.id AND m.user_id = u.id))
		         OR EXISTS (SELECT 1 FROM plan_visibility pv
		                    JOIN plan_visibility_members m ON m.plan_id = pv.plan_id
		                    WHERE pv.plan_id = p.id AND pv.mode = 'only_visible_to'
		                      AND m.user_id = u.id)
		       )
		     )`, planID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var uid int64
		if err := rows.Scan(&uid); err != nil {
			return nil, err
		}
		out = append(out, uid)
	}
	return out, rows.Err()
}
