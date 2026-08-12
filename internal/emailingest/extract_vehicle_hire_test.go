package emailingest

import (
	"context"
	"testing"
)

func TestExtractPlansVehicleHire(t *testing.T) {
	raw := `{"plans":[{"type":"vehicle_hire","title":"Test Rentals Ltd",
	  "confirmation_ref":"TEST123","supplier_name":"Test Rentals Ltd",
	  "parts":[{"type":"vehicle_hire","confidence":"high",
	    "start_date":"2026-09-09","start_time":"15:30",
	    "end_date":"2026-09-11","end_time":"14:00",
	    "start_label":"Test Depot North","end_label":"Test Depot South",
	    "vehicle_hire":{"category":"Standard SUV","vehicle":"Test Hatchback or similar",
	      "transmission":"Automatic","fuel_policy":"Same to same","mileage":"Unlimited",
	      "excess_amount":1400,"excess_currency":"EUR",
	      "deposit_amount":300,"deposit_currency":"EUR"}}]}]}`
	x, _ := newExtractor(raw)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Parts) != 1 {
		t.Fatalf("got %d plans", len(plans))
	}
	p := plans[0].Parts[0]
	if p.Type != "vehicle_hire" {
		t.Fatalf("type = %q", p.Type)
	}
	if p.EndDate != "2026-09-11" || p.EndTime != "14:00" {
		t.Fatalf("end = %q %q, want the return instant", p.EndDate, p.EndTime)
	}
	if p.HireCategory != "Standard SUV" || p.HireTransmission != "Automatic" {
		t.Fatalf("hire fields = %+v", p)
	}
	if p.HireExcessAmount == nil || *p.HireExcessAmount != 1400 || p.HireExcessCurrency != "EUR" {
		t.Fatalf("excess = %v %q", p.HireExcessAmount, p.HireExcessCurrency)
	}
	if p.HireDepositAmount == nil || *p.HireDepositAmount != 300 || p.HireDepositCurrency != "EUR" {
		t.Fatalf("deposit = %v %q", p.HireDepositAmount, p.HireDepositCurrency)
	}
	if p.HireVehicle != "Test Hatchback or similar" || p.HireFuelPolicy != "Same to same" || p.HireMileage != "Unlimited" {
		t.Fatalf("hire text fields = %+v", p)
	}
}

func TestExtractPlansVehicleHireDropsNonISOCurrency(t *testing.T) {
	// excess_currency "euros" is not ISO 4217; the amount survives, the code does not.
	// Mirrors the plan-cost precedent at extract.go:411-417.
	raw := `{"plans":[{"type":"vehicle_hire","title":"Test Rentals Ltd",
	  "confirmation_ref":"TEST456","supplier_name":"Test Rentals Ltd",
	  "parts":[{"type":"vehicle_hire","confidence":"high",
	    "start_date":"2026-09-09","end_date":"2026-09-11",
	    "vehicle_hire":{"category":"Economy","vehicle":"Test Compact",
	      "excess_amount":900,"excess_currency":"euros",
	      "deposit_amount":200,"deposit_currency":"dollars"}}]}]}`
	x, _ := newExtractor(raw)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	if len(plans) != 1 || len(plans[0].Parts) != 1 {
		t.Fatalf("got %d plans", len(plans))
	}
	p := plans[0].Parts[0]
	if p.HireExcessAmount == nil || *p.HireExcessAmount != 900 {
		t.Fatalf("excess amount = %v, want 900 (kept despite bad currency)", p.HireExcessAmount)
	}
	if p.HireExcessCurrency != "" {
		t.Fatalf("excess currency = %q, want empty (non-ISO code dropped)", p.HireExcessCurrency)
	}
	if p.HireDepositAmount == nil || *p.HireDepositAmount != 200 {
		t.Fatalf("deposit amount = %v, want 200 (kept despite bad currency)", p.HireDepositAmount)
	}
	if p.HireDepositCurrency != "" {
		t.Fatalf("deposit currency = %q, want empty (non-ISO code dropped)", p.HireDepositCurrency)
	}
}

func TestExtractPlansVehicleHireRejectsMissingStartDate(t *testing.T) {
	// A vehicle_hire part with no well-formed start_date is dropped, like every
	// other non-flight type (extract.go:462).
	raw := `{"plans":[{"type":"vehicle_hire","title":"Test Rentals Ltd",
	  "confirmation_ref":"TEST789","supplier_name":"Test Rentals Ltd",
	  "parts":[{"type":"vehicle_hire","confidence":"high",
	    "end_date":"2026-09-11",
	    "vehicle_hire":{"category":"Economy","vehicle":"Test Compact"}}]}]}`
	x, _ := newExtractor(raw)
	plans, err := x.ExtractPlans(context.Background(), "body", nil)
	if err != nil {
		t.Fatalf("ExtractPlans: %v", err)
	}
	if len(plans) != 0 {
		t.Fatalf("plans = %+v, want none (missing start_date drops the only part)", plans)
	}
}
