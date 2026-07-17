package config

import "testing"

func TestScalarStringRendersScalars(t *testing.T) {
	cases := []struct {
		name string
		val  any
		want string
	}{
		{"string", "hello", "hello"},
		{"bool true", true, "1"},
		{"bool false", false, "0"},
		{"int", 42, "42"},
		{"int64", int64(9000000000), "9000000000"},
		{"uint64", uint64(18000000000), "18000000000"},
		{"float64 whole", float64(3), "3"},
		{"float64 frac", 1.5, "1.5"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := scalarString("KEY", c.val)
			if err != nil {
				t.Fatalf("scalarString(%v) error: %v", c.val, err)
			}
			if got != c.want {
				t.Errorf("scalarString(%v) = %q, want %q", c.val, got, c.want)
			}
		})
	}
}

func TestScalarStringRejectsNonScalar(t *testing.T) {
	if _, err := scalarString("KEY", []int{1, 2}); err == nil {
		t.Error("expected an error for a non-scalar value")
	}
}

func TestHTTPSFromPublicURLScheme(t *testing.T) {
	if !(&Config{PublicURL: "https://aerly.example.com"}).HTTPS() {
		t.Error("https:// PublicURL should report HTTPS")
	}
	if (&Config{PublicURL: "http://localhost:8080"}).HTTPS() {
		t.Error("http:// PublicURL should not report HTTPS")
	}
}

func TestParseBool01(t *testing.T) {
	cases := []struct {
		env     string
		set     bool
		dflt    bool
		want    bool
		wantErr bool
	}{
		{set: false, dflt: true, want: true},           // unset -> default
		{env: "", set: true, dflt: false, want: false}, // empty -> default
		{env: "0", set: true, dflt: true, want: false},
		{env: "1", set: true, dflt: false, want: true},
		{env: " 1 ", set: true, dflt: false, want: true}, // trimmed
		{env: "true", set: true, dflt: false, wantErr: true},
	}
	// The unset case is first, so the variable is genuinely absent when it runs
	// (nothing has called Setenv yet); later cases set it as needed.
	for _, c := range cases {
		if c.set {
			t.Setenv("AERLY_TEST_BOOL", c.env)
		}
		got, err := parseBool01("AERLY_TEST_BOOL", c.dflt)
		if c.wantErr {
			if err == nil {
				t.Errorf("env=%q: expected error", c.env)
			}
			continue
		}
		if err != nil {
			t.Errorf("env=%q: unexpected error %v", c.env, err)
		}
		if got != c.want {
			t.Errorf("env=%q: got %v, want %v", c.env, got, c.want)
		}
	}
}

func TestGetenvFloat(t *testing.T) {
	cases := []struct {
		name string
		env  string
		set  bool
		def  float64
		want float64
	}{
		{name: "unset returns default", set: false, def: 0.5, want: 0.5},
		{name: "empty returns default", env: "", set: true, def: 0.5, want: 0.5},
		{name: "blank returns default", env: "   ", set: true, def: 0.5, want: 0.5},
		{name: "valid float parses", env: "0.72", set: true, def: 0.5, want: 0.72},
		{name: "surrounding space trimmed", env: " 0.3 ", set: true, def: 0.5, want: 0.3},
		{name: "malformed falls back to default", env: "not-a-number", set: true, def: 0.15, want: 0.15},
	}
	// The unset case runs first, whilst the variable is genuinely absent.
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.set {
				t.Setenv("AERLY_TEST_FLOAT", c.env)
			}
			if got := getenvFloat("AERLY_TEST_FLOAT", c.def); got != c.want {
				t.Errorf("env=%q: got %v, want %v", c.env, got, c.want)
			}
		})
	}
}
