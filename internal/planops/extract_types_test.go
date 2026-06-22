package planops

import "testing"

// TestFlightFieldsHasManualDetails exercises HasManualDetails: it is true only
// when every field needed to insert a flight without provider data is present,
// and false when any one of them is blank.
func TestFlightFieldsHasManualDetails(t *testing.T) {
	full := FlightFields{
		Ident:           "ZZ100",
		Date:            "2026-06-01",
		OriginIATA:      "AAA",
		DestIATA:        "BBB",
		DepartTimeLocal: "09:00",
		ArriveDate:      "2026-06-01",
		ArriveTimeLocal: "11:00",
	}
	if !full.HasManualDetails() {
		t.Errorf("a fully-populated leg should report HasManualDetails()=true: %+v", full)
	}

	// Each of the five required fields, when blank, must flip the result false.
	// (Ident/Date are not part of the manual-fallback requirement.)
	cases := map[string]func(*FlightFields){
		"OriginIATA":      func(f *FlightFields) { f.OriginIATA = "" },
		"DestIATA":        func(f *FlightFields) { f.DestIATA = "" },
		"DepartTimeLocal": func(f *FlightFields) { f.DepartTimeLocal = "" },
		"ArriveDate":      func(f *FlightFields) { f.ArriveDate = "" },
		"ArriveTimeLocal": func(f *FlightFields) { f.ArriveTimeLocal = "" },
	}
	for name, mut := range cases {
		f := full
		mut(&f)
		if f.HasManualDetails() {
			t.Errorf("with %s blank HasManualDetails() should be false: %+v", name, f)
		}
	}

	// The zero value has nothing, so it is false.
	if (FlightFields{}).HasManualDetails() {
		t.Error("zero-value FlightFields should report HasManualDetails()=false")
	}
}
