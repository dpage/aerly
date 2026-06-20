import { useMemo } from 'react';
import { Autocomplete, TextField } from '@mui/material';

/** All IANA timezone names the runtime knows about, with 'UTC' guaranteed to be
 * present. Computed once — the list is static for the life of the page. */
function loadZones(): string[] {
  let zones: string[] = [];
  try {
    const supported = (
      Intl as unknown as { supportedValuesOf?: (k: string) => string[] }
    ).supportedValuesOf?.('timeZone');
    if (Array.isArray(supported)) zones = supported;
  } catch {
    zones = [];
  }
  if (!zones.includes('UTC')) zones = ['UTC', ...zones];
  return zones;
}

const ZONES = loadZones();

/** Fold a zone name (or query) to a comparable form: lower-cased with the IANA
 * separators flattened to spaces, so "New York" matches "America/New_York" and
 * "van" matches "America/Vancouver". */
function fold(s: string): string {
  return s.toLowerCase().replace(/[_/]+/g, ' ');
}

interface Props {
  value: string;
  onChange: (tz: string) => void;
  label?: string;
  helperText?: string;
  placeholder?: string;
  size?: 'small' | 'medium';
  fullWidth?: boolean;
}

/** Timezone picker: an autocomplete over the IANA zone list with unanchored,
 * case-insensitive substring matching (type "Van" to find America/Vancouver).
 *
 * `freeSolo` keeps it tolerant — the typed text is always the value, so existing
 * data or a zone the runtime doesn't enumerate still round-trips, and clearing
 * the field yields '' (the caller treats blank as UTC). */
export default function TimezoneSelect({
  value,
  onChange,
  label = 'Timezone',
  helperText,
  placeholder,
  size = 'small',
  fullWidth = true,
}: Props) {
  // Substring match on the folded text; an empty query lists everything.
  const filterOptions = useMemo(
    () => (options: string[], state: { inputValue: string }) => {
      const q = fold(state.inputValue.trim());
      if (!q) return options;
      return options.filter((o) => fold(o).includes(q));
    },
    [],
  );

  return (
    <Autocomplete
      freeSolo
      autoHighlight
      options={ZONES}
      value={value}
      inputValue={value}
      onInputChange={(_, v) => onChange(v)}
      onChange={(_, v) => onChange(v ?? '')}
      filterOptions={filterOptions}
      fullWidth={fullWidth}
      renderInput={(params) => (
        <TextField
          {...params}
          label={label}
          size={size}
          placeholder={placeholder}
          helperText={helperText}
        />
      )}
    />
  );
}
