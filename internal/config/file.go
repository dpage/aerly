package config

import (
	"fmt"
	"os"
	"strconv"

	"go.yaml.in/yaml/v3"
)

// requiredFileMode is the only permission bitmask a config file may carry:
// read-only by its owner, inaccessible to group and other. A config file
// routinely holds secrets (DATABASE_URL, OAuth client secrets, SESSION_KEY),
// so anything more permissive is refused at startup rather than silently
// trusted.
const requiredFileMode os.FileMode = 0o400

// LoadFile reads a YAML config file and applies its values to the process
// environment, where Load (and the rest of the package) pick them up. The
// file's keys are environment-variable names — the same names documented in
// the README — so the file is a drop-in alternative to setting the variables
// directly, with no per-field plumbing here.
//
// Precedence: a file value is applied only when its variable is blank or unset
// in the environment, so real environment variables (and anything a .env has
// already loaded) override the file. This matches issue #94: "the existing
// envvars would override config file values".
//
// Scalars are stringified to match the env-var conventions: booleans become
// "1"/"0" (the 0/1 flags the loader expects), numbers their decimal form, and
// strings themselves. A null maps to "unset" (skipped). Non-scalar values
// (maps, sequences) are rejected — an env var is a flat string.
//
// The file must have 0400 permissions; LoadFile returns an error (and the
// server refuses to start) otherwise.
func LoadFile(path string) error {
	info, err := os.Stat(path)
	if err != nil {
		return fmt.Errorf("config file: %w", err)
	}
	if perm := info.Mode().Perm(); perm != requiredFileMode {
		return fmt.Errorf("config file %s has insecure permissions %#o; it must be %#o (read-only by its owner)",
			path, perm, requiredFileMode)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("config file: %w", err)
	}

	var doc map[string]any
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("config file %s: %w", path, err)
	}

	for key, val := range doc {
		if val == nil {
			continue // explicit null leaves the variable unset
		}
		s, err := scalarString(key, val)
		if err != nil {
			return fmt.Errorf("config file %s: %w", path, err)
		}
		// Only fill variables the environment hasn't already set, so env vars
		// win over the file. A blank value counts as unset here, mirroring the
		// rest of the package (getenv treats "" as "fall back to the default").
		if os.Getenv(key) != "" {
			continue
		}
		if err := os.Setenv(key, s); err != nil {
			return fmt.Errorf("config file %s: applying %s: %w", path, key, err)
		}
	}
	return nil
}

// scalarString renders a YAML scalar as the string an env var would carry.
func scalarString(key string, val any) (string, error) {
	switch v := val.(type) {
	case string:
		return v, nil
	case bool:
		if v {
			return "1", nil
		}
		return "0", nil
	case int:
		return strconv.Itoa(v), nil
	case int64:
		return strconv.FormatInt(v, 10), nil
	case uint64:
		return strconv.FormatUint(v, 10), nil
	case float64:
		return strconv.FormatFloat(v, 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("value for %q must be a string, number, or boolean (got %T)", key, val)
	}
}
