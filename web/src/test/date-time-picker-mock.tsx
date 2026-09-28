// Stands in for MUI's DateTimePicker in tests: a plain text input carrying the
// picked wall-clock as "YYYY-MM-DDTHH:MM", so dialogs that use the picker can
// be driven with fireEvent.change and don't need a LocalizationProvider.
// A blank value reports null and anything unparseable an Invalid Date, as the
// real picker does while a section is half-typed.
export function DateTimePicker({
  label,
  value,
  onChange,
}: {
  label: string;
  value: Date | null;
  onChange: (d: Date | null) => void;
}) {
  const pad = (n: number) => String(n).padStart(2, '0');
  const shown = value
    ? `${value.getFullYear()}-${pad(value.getMonth() + 1)}-${pad(value.getDate())}T${pad(value.getHours())}:${pad(value.getMinutes())}`
    : '';
  return (
    <input
      aria-label={label}
      value={shown}
      onChange={(e) => {
        const v = e.target.value;
        const m = /^(\d{4})-(\d{2})-(\d{2})T(\d{2}):(\d{2})$/.exec(v);
        if (v === '') onChange(null);
        else if (m) onChange(new Date(+m[1], +m[2] - 1, +m[3], +m[4], +m[5]));
        else onChange(new Date(NaN));
      }}
    />
  );
}
