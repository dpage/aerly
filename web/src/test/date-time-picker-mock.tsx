// Stands in for MUI's DateTimePicker in tests: a plain text input carrying the
// picked wall-clock as "YYYY-MM-DDTHH:MM", so dialogs that use the picker can
// be driven with fireEvent.change without the real picker's section editing.
// Values are Luxon DateTimes in the picker's timezone, as under AdapterLuxon.
// A blank value reports null and anything unparseable an invalid DateTime, as
// the real picker does while a section is half-typed.
import { DateTime } from 'luxon';

export function DateTimePicker({
  label,
  value,
  timezone = 'UTC',
  onChange,
}: {
  label: string;
  value: DateTime | null;
  timezone?: string;
  onChange: (d: DateTime | null) => void;
}) {
  return (
    <input
      aria-label={label}
      value={value ? value.toFormat("yyyy-MM-dd'T'HH:mm") : ''}
      onChange={(e) => {
        const v = e.target.value;
        if (v === '') onChange(null);
        else onChange(DateTime.fromFormat(v, "yyyy-MM-dd'T'HH:mm", { zone: timezone }));
      }}
    />
  );
}
