package api_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/dpage/aerly/internal/api"
	"github.com/dpage/aerly/internal/store"
)

func TestToPlanPartDTOProjectsVehicleHire(t *testing.T) {
	excess := 1400.0
	part := &store.PlanPart{ID: 7, StartsAt: time.Date(2026, 9, 9, 15, 30, 0, 0, time.UTC)}
	dto := api.ToPlanPartDTO(part, nil, nil, nil, nil, nil, nil, nil, nil, nil,
		&store.VehicleHireDetail{
			Category: "Standard SUV", Transmission: "Automatic",
			ExcessAmount: &excess, ExcessCurrency: "EUR",
		}, nil, nil)
	if dto.VehicleHire == nil {
		t.Fatal("VehicleHire DTO not projected")
	}
	if dto.VehicleHire.Category != "Standard SUV" {
		t.Fatalf("category = %q", dto.VehicleHire.Category)
	}
	if dto.VehicleHire.ExcessAmount == nil || *dto.VehicleHire.ExcessAmount != 1400 {
		t.Fatalf("excess = %v, want 1400", dto.VehicleHire.ExcessAmount)
	}
}

func TestVehicleHireDTOJSONFieldName(t *testing.T) {
	b, err := json.Marshal(api.PlanPartDTO{VehicleHire: &api.VehicleHireDetailDTO{Category: "Compact"}})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"vehicle_hire"`) {
		t.Fatalf("JSON must use the vehicle_hire wire key, got %s", b)
	}
}

// TestToVehicleHireDetailDTO_NilDeposit checks that a nil deposit amount stays
// nil in the DTO (distinct from a genuine zero deposit), whilst the excess
// amount and every string field are carried across unchanged.
func TestToVehicleHireDetailDTO_NilDeposit(t *testing.T) {
	excess := 0.0
	d := &store.VehicleHireDetail{
		PlanPartID:      42,
		Category:        "Economy",
		Vehicle:         "Fiat 500",
		Transmission:    "Manual",
		FuelPolicy:      "Full to full",
		Mileage:         "Unlimited",
		ExcessAmount:    &excess,
		ExcessCurrency:  "GBP",
		DepositAmount:   nil,
		DepositCurrency: "",
	}
	dto := api.ToVehicleHireDetailDTO(d)
	if dto.Category != "Economy" || dto.Vehicle != "Fiat 500" || dto.Transmission != "Manual" ||
		dto.FuelPolicy != "Full to full" || dto.Mileage != "Unlimited" {
		t.Fatalf("string fields not projected: %+v", dto)
	}
	if dto.ExcessAmount == nil || *dto.ExcessAmount != 0 {
		t.Fatalf("excess amount = %v, want a pointer to 0 (not nil)", dto.ExcessAmount)
	}
	if dto.ExcessCurrency != "GBP" {
		t.Fatalf("excess currency = %q, want GBP", dto.ExcessCurrency)
	}
	if dto.DepositAmount != nil {
		t.Fatalf("deposit amount = %v, want nil (not stated)", dto.DepositAmount)
	}
	if dto.DepositCurrency != "" {
		t.Fatalf("deposit currency = %q, want empty", dto.DepositCurrency)
	}
}
