import { useEffect, useMemo, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Alert,
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  MenuItem,
  Stack,
  TextField,
  Typography,
} from '@mui/material';

import type { Plan, PlanPart, UpdatePlanInput, UpdatePlanPartInput } from '../api/types';
import { api } from '../api/client';
import { useStore } from '../state/store';
import { endUnlocated, isUnlocated, parseLatLon, startUnlocated } from '../lib/geo';
import {
  coordsFromText,
  extractLatLonFromMapsUrl,
  isMapsUrl,
  isShortMapsUrl,
} from '../lib/maps-url';
import {
  isTransferType,
  planTypeLabel,
  splitLocal,
  typeHasEnd,
  zonedTimeToUtc,
} from '../lib/trip-format';

interface Props {
  open: boolean;
  plan: Plan;
  onClose: () => void;
}

/** Editable fields for one endpoint (start or end) of a part. */
interface EndForm {
  label: string;
  address: string;
  date: string;
  time: string;
  tz: string;
  /** Manual "lat, lng" override, '' when the location is address-derived. */
  coords: string;
}

/** A flight part's editable route/identity. `resolved` mirrors the provider
 * state: the IATA fields are editable only when it's false. */
interface FlightForm {
  ident: string;
  originIata: string;
  destIata: string;
  resolved: boolean;
}

interface PartForm {
  start: EndForm;
  end: EndForm;
  flight?: FlightForm;
}

function endForm(
  label: string,
  address: string,
  iso: string | undefined,
  tz: string,
  lat: number | undefined,
  lon: number | undefined,
): EndForm {
  const { date, time } = iso ? splitLocal(iso, tz) : { date: '', time: '' };
  const coords = lat != null && lon != null ? `${lat}, ${lon}` : '';
  return { label, address, date, time, tz, coords };
}

function partForm(part: PlanPart): PartForm {
  return {
    start: endForm(
      part.start_label ?? '',
      part.start_address ?? '',
      part.starts_at,
      part.start_tz ?? '',
      part.start_lat,
      part.start_lon,
    ),
    end: endForm(
      part.end_label ?? '',
      part.end_address ?? '',
      part.ends_at,
      part.end_tz || part.start_tz || '',
      part.end_lat,
      part.end_lon,
    ),
    flight:
      part.type === 'flight' && part.flight
        ? {
            ident: part.flight.ident ?? '',
            originIata: part.flight.origin_iata ?? '',
            destIata: part.flight.dest_iata ?? '',
            resolved: part.flight.resolved,
          }
        : undefined,
  };
}

/** Does this part have a meaningful "end" endpoint to edit — a transfer's
 * arrival or a hotel's check-out — or anything that already carries an end time?
 * Single-point plans (a dining reservation) show only a start. A hotel always
 * qualifies so a check-out can be added even when none was set at creation. */
function hasEnd(part: PlanPart): boolean {
  return typeHasEnd(part.type) || part.ends_at != null;
}

/** Diff a part's form against its initial snapshot into an update payload, or
 * null when nothing changed. Time fields are only sent when the local
 * date/time/tz actually changed, so an untouched part keeps its exact instant
 * (and a flight its second-precision schedule). */
function buildPatch(part: PlanPart, form: PartForm, init: PartForm): UpdatePlanPartInput | null {
  const patch: UpdatePlanPartInput = {};
  const s = form.start;
  const si = init.start;
  if (s.label !== si.label) patch.start_label = s.label.trim();
  if (s.address !== si.address) patch.start_address = s.address.trim();
  if (s.date !== si.date || s.time !== si.time || s.tz !== si.tz) {
    if (s.date && s.time) patch.starts_at = zonedTimeToUtc(s.date, s.time, s.tz);
    if (s.tz !== si.tz || patch.starts_at) patch.start_tz = s.tz;
  }
  // A changed coordinate override: a valid "lat, lng" pins the location (the
  // geocoder won't touch it); clearing it unpins, reverting to the address.
  // Invalid input is left for handleSave to reject before we get here.
  if (s.coords !== si.coords) {
    const c = coordsFromText(s.coords);
    if (c) {
      patch.start_lat = c.lat;
      patch.start_lon = c.lon;
      patch.start_coords_pinned = true;
    } else if (s.coords.trim() === '' && part.start_coords_pinned) {
      patch.start_coords_pinned = false;
    }
  }

  if (hasEnd(part)) {
    const e = form.end;
    const ei = init.end;
    if (e.label !== ei.label) patch.end_label = e.label.trim();
    if (e.address !== ei.address) patch.end_address = e.address.trim();
    if (e.date !== ei.date || e.time !== ei.time || e.tz !== ei.tz) {
      if (e.date && e.time) patch.ends_at = zonedTimeToUtc(e.date, e.time, e.tz);
      if (e.tz !== ei.tz || patch.ends_at) patch.end_tz = e.tz;
    }
    if (e.coords !== ei.coords) {
      const c = coordsFromText(e.coords);
      if (c) {
        patch.end_lat = c.lat;
        patch.end_lon = c.lon;
        patch.end_coords_pinned = true;
      } else if (e.coords.trim() === '' && part.end_coords_pinned) {
        patch.end_coords_pinned = false;
      }
    }
  }

  // Flight route/identity. The ident is always editable (changing it re-resolves
  // server-side); the IATAs are sent only when the flight is unresolved, where
  // they're the user-owned route. Each is included only when actually changed.
  if (form.flight && init.flight) {
    const f = form.flight;
    const fi = init.flight;
    const flight: NonNullable<UpdatePlanPartInput['flight']> = {};
    if (f.ident.trim() !== fi.ident) flight.ident = f.ident.trim();
    if (!f.resolved) {
      if (f.originIata.trim().toUpperCase() !== fi.originIata)
        flight.origin_iata = f.originIata.trim().toUpperCase();
      if (f.destIata.trim().toUpperCase() !== fi.destIata)
        flight.dest_iata = f.destIata.trim().toUpperCase();
    }
    if (Object.keys(flight).length > 0) patch.flight = flight;
  }

  return Object.keys(patch).length > 0 ? patch : null;
}

/** Edit a plan's title / confirmation / notes plus every part's schedule and
 * places — date/time/timezone and start/end label + address for each endpoint
 * (PRD §6.4) — and move it to another trip the viewer can edit (PRD §6.3).
 * Owner/editor only, gated by the caller. */
export default function PlanEditDialog({ open, plan, onClose }: Props) {
  const trips = useStore((s) => s.trips);
  const listTrips = useStore((s) => s.listTrips);
  const updatePlan = useStore((s) => s.updatePlan);
  const updatePlanPart = useStore((s) => s.updatePlanPart);
  const movePlan = useStore((s) => s.movePlan);
  const splitPlanPart = useStore((s) => s.splitPlanPart);
  const setError = useStore((s) => s.setError);
  const setNotice = useStore((s) => s.setNotice);

  const [title, setTitle] = useState(plan.title);
  const [confRef, setConfRef] = useState(plan.confirmation_ref);
  const [ticketNumber, setTicketNumber] = useState(plan.ticket_number ?? '');
  const [notes, setNotes] = useState(plan.notes);
  const [cost, setCost] = useState(plan.cost_amount != null ? String(plan.cost_amount) : '');
  const [currency, setCurrency] = useState(plan.cost_currency ?? '');
  const [supplierName, setSupplierName] = useState(plan.supplier_name);
  const [contactEmail, setContactEmail] = useState(plan.contact_email);
  const [contactPhone, setContactPhone] = useState(plan.contact_phone);
  const [website, setWebsite] = useState(plan.website);
  const [moveTarget, setMoveTarget] = useState<number | ''>('');
  const [busy, setBusy] = useState(false);

  // The editable parts (dismissed ones are hidden) and their initial snapshot.
  const editableParts = useMemo(() => plan.parts.filter((p) => !p.dismissed_at), [plan.parts]);
  // A multi-leg flight/train/ground booking can have a leg split out into its
  // own plan when it wasn't really part of the same booking (#12).
  const canSplit =
    editableParts.length > 1 &&
    (plan.type === 'flight' || plan.type === 'train' || plan.type === 'ground');
  const [forms, setForms] = useState<Record<number, PartForm>>({});
  const [initial, setInitial] = useState<Record<number, PartForm>>({});

  // Re-sync the form when the dialog (re)opens or switches plans, and refresh
  // the trip list so the move targets reflect what the viewer can edit now.
  // Not keyed on plan.* fields so an in-flight refetch can't clobber edits.
  useEffect(() => {
    if (!open) return;
    setTitle(plan.title);
    setConfRef(plan.confirmation_ref);
    setTicketNumber(plan.ticket_number ?? '');
    setNotes(plan.notes);
    setCost(plan.cost_amount != null ? String(plan.cost_amount) : '');
    setCurrency(plan.cost_currency ?? '');
    setSupplierName(plan.supplier_name);
    setContactEmail(plan.contact_email);
    setContactPhone(plan.contact_phone);
    setWebsite(plan.website);
    setMoveTarget('');
    const snap: Record<number, PartForm> = {};
    for (const p of editableParts) snap[p.id] = partForm(p);
    setForms(snap);
    setInitial(snap);
    setCoordsErr({});
    setCoordsBusy({});
    void listTrips();
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync only on (re)open / plan switch
  }, [open, plan.id]);

  // A plan can only move to another trip the viewer can edit (spec §5.2).
  const moveTargets = useMemo(
    () =>
      trips.filter(
        (t) => t.id !== plan.trip_id && (t.my_role === 'owner' || t.my_role === 'editor'),
      ),
    [trips, plan.trip_id],
  );

  const reportError = (err: unknown) => setError(errorMessage(err));

  const patchEnd = (
    partId: number,
    which: 'start' | 'end',
    field: keyof EndForm,
    value: string,
  ) => {
    setForms((prev) => ({
      ...prev,
      [partId]: { ...prev[partId], [which]: { ...prev[partId][which], [field]: value } },
    }));
  };

  // Per-endpoint blur-resolution of a pasted Google Maps URL. A full URL is
  // decoded client-side; a short link is resolved by the backend (which follows
  // its redirect). Busy/error are keyed by "partId:which" so each field is
  // independent.
  const [coordsBusy, setCoordsBusy] = useState<Record<string, boolean>>({});
  const [coordsErr, setCoordsErr] = useState<Record<string, string>>({});
  const coordsKey = (partId: number, which: 'start' | 'end') => `${partId}:${which}`;
  const COORDS_FAIL = "Couldn't read a location from that link; paste the coordinates instead.";

  const resolveCoords = async (partId: number, which: 'start' | 'end') => {
    const key = coordsKey(partId, which);
    // The field only renders once forms[partId] exists, so it's always present
    // by the time a blur fires; read its current value directly.
    const value = forms[partId][which].coords.trim();
    setCoordsErr((p) => ({ ...p, [key]: '' }));
    if (value === '' || parseLatLon(value) || !isMapsUrl(value)) return;
    const local = extractLatLonFromMapsUrl(value);
    if (local) {
      patchEnd(partId, which, 'coords', `${local.lat}, ${local.lon}`);
      return;
    }
    if (!isShortMapsUrl(value)) {
      setCoordsErr((p) => ({ ...p, [key]: COORDS_FAIL }));
      return;
    }
    setCoordsBusy((p) => ({ ...p, [key]: true }));
    try {
      const c = await api.resolveMapsUrl(value);
      patchEnd(partId, which, 'coords', `${c.lat}, ${c.lon}`);
    } catch {
      setCoordsErr((p) => ({ ...p, [key]: COORDS_FAIL }));
    } finally {
      setCoordsBusy((p) => ({ ...p, [key]: false }));
    }
  };

  const patchFlight = (partId: number, field: keyof FlightForm, value: string) => {
    setForms((prev) => {
      const f = prev[partId].flight;
      if (!f) return prev;
      return { ...prev, [partId]: { ...prev[partId], flight: { ...f, [field]: value } } };
    });
  };

  const handleSave = async () => {
    // Reject an unparseable coordinate override before writing anything.
    for (const part of editableParts) {
      const f = forms[part.id];
      for (const end of [f?.start, f?.end]) {
        if (end && end.coords.trim() !== '' && !coordsFromText(end.coords)) {
          setError('Enter coordinates as "lat, lng", or paste a Google Maps link.');
          return;
        }
      }
    }
    setBusy(true);
    try {
      // The plan-level metadata is sent as one snapshot when any of it changed;
      // the backend COALESCEs each field, so re-sending unchanged values is a
      // no-op. A blank cost parses to undefined and is omitted, which the
      // backend leaves unchanged (cost can be set or corrected but not cleared
      // back to "unknown", mirroring how the part editor treats times).
      const costNum = cost.trim() === '' ? undefined : Number(cost);
      const curr = currency.trim().toUpperCase();
      const costChanged = costNum != null && !Number.isNaN(costNum) && costNum !== plan.cost_amount;
      const metaChanged =
        title.trim() !== plan.title ||
        confRef.trim() !== plan.confirmation_ref ||
        ticketNumber.trim() !== (plan.ticket_number ?? '') ||
        notes !== plan.notes ||
        curr !== (plan.cost_currency ?? '') ||
        supplierName.trim() !== plan.supplier_name ||
        contactEmail.trim() !== plan.contact_email ||
        contactPhone.trim() !== plan.contact_phone ||
        website.trim() !== plan.website ||
        costChanged;
      if (metaChanged) {
        const payload: UpdatePlanInput = {
          title: title.trim(),
          confirmation_ref: confRef.trim(),
          ticket_number: ticketNumber.trim(),
          notes,
          cost_currency: curr,
          supplier_name: supplierName.trim(),
          contact_email: contactEmail.trim(),
          contact_phone: contactPhone.trim(),
          website: website.trim(),
        };
        if (costChanged) payload.cost_amount = costNum;
        await updatePlan(plan.id, payload);
      }
      const stranded: string[] = [];
      for (const part of editableParts) {
        const patch = buildPatch(part, forms[part.id], initial[part.id]);
        if (!patch) continue;
        const addrChanged = patch.start_address !== undefined || patch.end_address !== undefined;
        const updated = await updatePlanPart(part.id, patch);
        if (addrChanged && isUnlocated(updated)) {
          stranded.push(patch.start_address || patch.end_address || '');
        }
      }
      onClose();
      if (stranded.length > 0) {
        // Surface a single notice even if several parts failed to geocode.
        setNotice({
          severity: 'info',
          message: `Saved — couldn't place "${stranded[0]}" on the map.`,
        });
      }
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleSplit = async (partId: number) => {
    setBusy(true);
    try {
      // The leg moves to a new plan; close so the refreshed timeline shows it.
      await splitPlanPart(partId);
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleMove = async () => {
    if (moveTarget === '') return;
    setBusy(true);
    try {
      await movePlan(plan.id, moveTarget);
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Edit plan</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={2} sx={{ mt: 0.5 }}>
          <TextField
            label="Title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            fullWidth
          />
          <TextField
            label="Confirmation ref"
            value={confRef}
            onChange={(e) => setConfRef(e.target.value)}
            fullWidth
          />
          <TextField
            label="Ticket number"
            value={ticketNumber}
            onChange={(e) => setTicketNumber(e.target.value)}
            fullWidth
          />
          <Stack direction="row" spacing={1}>
            <TextField
              label="Cost"
              type="number"
              value={cost}
              onChange={(e) => setCost(e.target.value)}
              slotProps={{ htmlInput: { min: 0, step: '0.01' } }}
              sx={{ flex: 2 }}
            />
            <TextField
              label="Currency"
              value={currency}
              onChange={(e) => setCurrency(e.target.value)}
              placeholder="GBP"
              slotProps={{ htmlInput: { maxLength: 3, style: { textTransform: 'uppercase' } } }}
              sx={{ flex: 1 }}
            />
          </Stack>
          <TextField
            label="Supplier"
            value={supplierName}
            onChange={(e) => setSupplierName(e.target.value)}
            placeholder="Who the booking is with, e.g. British Airways"
            fullWidth
          />
          <TextField
            label="Contact email"
            type="email"
            value={contactEmail}
            onChange={(e) => setContactEmail(e.target.value)}
            fullWidth
          />
          <TextField
            label="Contact phone"
            type="tel"
            value={contactPhone}
            onChange={(e) => setContactPhone(e.target.value)}
            fullWidth
          />
          <TextField
            label="Website"
            type="url"
            value={website}
            onChange={(e) => setWebsite(e.target.value)}
            placeholder="https://…"
            fullWidth
          />
          <TextField
            label="Notes"
            value={notes}
            onChange={(e) => setNotes(e.target.value)}
            fullWidth
            multiline
            minRows={2}
          />

          {editableParts.map((part, i) => {
            const form = forms[part.id];
            if (!form) return null;
            const withEnd = hasEnd(part);
            return (
              <Box key={part.id}>
                <Divider sx={{ mb: 1.5 }}>
                  <Typography variant="caption" color="text.secondary">
                    {planTypeLabel(part.type)}
                    {editableParts.length > 1 ? ` ${i + 1}` : ''}
                  </Typography>
                </Divider>
                {canSplit && (
                  <Box sx={{ display: 'flex', justifyContent: 'flex-end', mb: 1 }}>
                    <Button
                      size="small"
                      color="inherit"
                      onClick={() => void handleSplit(part.id)}
                      disabled={busy}
                    >
                      Split out
                    </Button>
                  </Box>
                )}
                <EndFields
                  heading={withEnd && isTransferType(part.type) ? 'From' : 'Where'}
                  form={form.start}
                  onChange={(f, v) => patchEnd(part.id, 'start', f, v)}
                  unlocated={startUnlocated(part)}
                  onResolveCoords={() => void resolveCoords(part.id, 'start')}
                  coordsResolving={!!coordsBusy[coordsKey(part.id, 'start')]}
                  coordsError={coordsErr[coordsKey(part.id, 'start')] ?? ''}
                />
                {withEnd && (
                  <Box sx={{ mt: 1.5 }}>
                    <EndFields
                      heading={isTransferType(part.type) ? 'To' : 'Until'}
                      form={form.end}
                      onChange={(f, v) => patchEnd(part.id, 'end', f, v)}
                      // A non-transfer's "end" is the same place (a hotel's
                      // check-out), so only its time is editable — no second
                      // Place/Address.
                      timeOnly={!isTransferType(part.type)}
                      unlocated={endUnlocated(part)}
                      onResolveCoords={() => void resolveCoords(part.id, 'end')}
                      coordsResolving={!!coordsBusy[coordsKey(part.id, 'end')]}
                      coordsError={coordsErr[coordsKey(part.id, 'end')] ?? ''}
                    />
                  </Box>
                )}
                {form.flight && (
                  <Box sx={{ mt: 1.5 }}>
                    <FlightFields
                      form={form.flight}
                      onChange={(f, v) => patchFlight(part.id, f, v)}
                    />
                  </Box>
                )}
              </Box>
            );
          })}

          {moveTargets.length > 0 && (
            <Box>
              <Divider sx={{ mb: 1.5 }} />
              <Typography variant="subtitle2" sx={{ mb: 1 }}>
                Move to another trip
              </Typography>
              <Stack direction="row" spacing={1} alignItems="flex-start">
                <TextField
                  select
                  size="small"
                  label="Move to another trip"
                  fullWidth
                  value={moveTarget === '' ? '' : String(moveTarget)}
                  onChange={(e) =>
                    setMoveTarget(e.target.value === '' ? '' : Number(e.target.value))
                  }
                  helperText="Takes the plan and its parts to another trip you can edit."
                  slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
                >
                  <MenuItem value="" disabled>
                    Choose a trip…
                  </MenuItem>
                  {moveTargets.map((t) => (
                    <MenuItem key={t.id} value={String(t.id)}>
                      {t.name}
                    </MenuItem>
                  ))}
                </TextField>
                <Button
                  variant="outlined"
                  onClick={() => void handleMove()}
                  disabled={busy || moveTarget === ''}
                  sx={{ mt: 0.5 }}
                >
                  Move
                </Button>
              </Stack>
            </Box>
          )}
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Cancel</Button>
        <Button
          variant="contained"
          onClick={() => void handleSave()}
          disabled={busy || !title.trim()}
        >
          Save
        </Button>
      </DialogActions>
    </Dialog>
  );
}

/** Flight route/identity inputs. The flight number is always editable —
 * changing it re-resolves the flight server-side, re-deriving route, schedule
 * and tracking. The origin/dest IATA are editable only for an unresolved flight
 * (one the provider can't track); for a resolved flight they're read-only and
 * provider-owned, since editing them would just be overwritten on the next poll. */
function FlightFields({
  form,
  onChange,
}: {
  form: FlightForm;
  onChange: (field: keyof FlightForm, value: string) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Flight
      </Typography>
      <TextField
        label="Flight number"
        size="small"
        value={form.ident}
        onChange={(e) => onChange('ident', e.target.value)}
        helperText="Changing this re-looks-up the flight and its route."
        fullWidth
      />
      <Stack direction="row" spacing={1}>
        <TextField
          label="From (IATA)"
          size="small"
          value={form.originIata}
          onChange={(e) => onChange('originIata', e.target.value)}
          disabled={form.resolved}
          slotProps={{ htmlInput: { maxLength: 3, style: { textTransform: 'uppercase' } } }}
          sx={{ flex: 1 }}
        />
        <TextField
          label="To (IATA)"
          size="small"
          value={form.destIata}
          onChange={(e) => onChange('destIata', e.target.value)}
          disabled={form.resolved}
          slotProps={{ htmlInput: { maxLength: 3, style: { textTransform: 'uppercase' } } }}
          sx={{ flex: 1 }}
        />
      </Stack>
      <Typography variant="caption" color="text.secondary">
        {form.resolved
          ? 'Route is set from live flight data. Change the flight number to re-look it up.'
          : "We couldn't match this flight number, so you can set its route by hand."}
      </Typography>
    </Stack>
  );
}

/** The label / address / date / time / timezone inputs for one endpoint. When
 * timeOnly is set the Place/Address inputs are hidden — used for the "Until"
 * edge of a single-location part (a hotel's check-out shares the check-in
 * place), leaving only its date/time/timezone editable. */
function EndFields({
  heading,
  form,
  onChange,
  timeOnly = false,
  unlocated = false,
  onResolveCoords,
  coordsResolving = false,
  coordsError = '',
}: {
  heading: string;
  form: EndForm;
  onChange: (field: keyof EndForm, value: string) => void;
  timeOnly?: boolean;
  unlocated?: boolean;
  onResolveCoords?: () => void;
  coordsResolving?: boolean;
  coordsError?: string;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        {heading}
      </Typography>
      {!timeOnly && (
        <TextField
          label="Place"
          size="small"
          value={form.label}
          onChange={(e) => onChange('label', e.target.value)}
          fullWidth
        />
      )}
      {!timeOnly && (
        <TextField
          label="Address"
          size="small"
          value={form.address}
          onChange={(e) => onChange('address', e.target.value)}
          helperText="Editing the address re-locates it on the map."
          fullWidth
        />
      )}
      {!timeOnly && unlocated && (
        <Alert severity="warning" sx={{ py: 0 }}>
          This address couldn&apos;t be located on the map. Try a simpler form — e.g. the property
          name and town.
        </Alert>
      )}
      {!timeOnly && (
        <TextField
          label="Coordinates (lat, lng)"
          size="small"
          value={form.coords}
          onChange={(e) => onChange('coords', e.target.value)}
          onBlur={() => onResolveCoords?.()}
          placeholder="optional — e.g. 48.2105, 4.0823 or a Google Maps link"
          error={
            coordsError !== '' ||
            (form.coords.trim() !== '' &&
              parseLatLon(form.coords) === null &&
              !isMapsUrl(form.coords))
          }
          helperText={
            coordsResolving
              ? 'Resolving link…'
              : coordsError !== ''
                ? coordsError
                : form.coords.trim() !== '' &&
                    parseLatLon(form.coords) === null &&
                    !isMapsUrl(form.coords)
                  ? 'Enter as "lat, lng", or paste a Google Maps pin or link.'
                  : 'Paste a Google Maps pin or link to override the geocoded location.'
          }
          fullWidth
        />
      )}
      <Stack direction="row" spacing={1}>
        <TextField
          label="Date"
          type="date"
          size="small"
          value={form.date}
          onChange={(e) => onChange('date', e.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
          sx={{ flex: 1 }}
        />
        <TextField
          label="Time"
          type="time"
          size="small"
          value={form.time}
          onChange={(e) => onChange('time', e.target.value)}
          slotProps={{ inputLabel: { shrink: true } }}
          sx={{ flex: 1 }}
        />
      </Stack>
      <TextField
        label="Timezone"
        size="small"
        value={form.tz}
        onChange={(e) => onChange('tz', e.target.value)}
        placeholder="UTC"
        helperText="IANA name, e.g. Europe/London. Blank = UTC."
        fullWidth
      />
    </Stack>
  );
}
