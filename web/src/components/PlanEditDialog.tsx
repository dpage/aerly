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
  Rating,
  Stack,
  TextField,
  Typography,
} from '@mui/material';

import type { Plan, PlanPart, UpdatePlanInput, UpdatePlanPartInput } from '../api/types';
import { api } from '../api/client';
import PlanAttachments from './PlanAttachments';
import TimezoneSelect from './TimezoneSelect';
import { useStore } from '../state/store';
import { useOnlineStatus } from '../pwa';
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

/** An ice cream stop's editable rating (0–5 stars) and what-was-ordered note. */
interface IceCreamForm {
  rating: number;
  whatOrdered: string;
}

/** The editable per-type detail for the remaining plan types. Numbers are held
 * as strings so the field can be cleared while editing; parsed on save. */
interface HotelForm {
  phone: string;
  roomType: string;
  guests: string;
}

interface TrainForm {
  operator: string;
  serviceNo: string;
  coach: string;
  seat: string;
  cls: string;
  platform: string;
}

interface GroundForm {
  provider: string;
  phone: string;
  vehicle: string;
  driver: string;
  pax: string;
}

interface DiningForm {
  reservationName: string;
  partySize: string;
  phone: string;
}

interface ExcursionForm {
  provider: string;
  ticketCount: string;
}

interface PartForm {
  start: EndForm;
  end: EndForm;
  flight?: FlightForm;
  iceCream?: IceCreamForm;
  hotel?: HotelForm;
  train?: TrainForm;
  ground?: GroundForm;
  dining?: DiningForm;
  excursion?: ExcursionForm;
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
    iceCream:
      part.type === 'ice_cream'
        ? {
            rating: part.ice_cream?.rating ?? 0,
            whatOrdered: part.ice_cream?.what_ordered ?? '',
          }
        : undefined,
    hotel:
      part.type === 'hotel'
        ? {
            phone: part.hotel?.phone ?? '',
            roomType: part.hotel?.room_type ?? '',
            guests: part.hotel?.guests != null ? String(part.hotel.guests) : '',
          }
        : undefined,
    train:
      part.type === 'train'
        ? {
            operator: part.train?.operator ?? '',
            serviceNo: part.train?.service_no ?? '',
            coach: part.train?.coach ?? '',
            seat: part.train?.seat ?? '',
            cls: part.train?.class ?? '',
            platform: part.train?.platform ?? '',
          }
        : undefined,
    ground:
      part.type === 'ground'
        ? {
            provider: part.ground?.provider ?? '',
            phone: part.ground?.phone ?? '',
            vehicle: part.ground?.vehicle ?? '',
            driver: part.ground?.driver ?? '',
            pax: part.ground?.pax != null ? String(part.ground.pax) : '',
          }
        : undefined,
    dining:
      part.type === 'dining'
        ? {
            reservationName: part.dining?.reservation_name ?? '',
            partySize: part.dining?.party_size != null ? String(part.dining.party_size) : '',
            phone: part.dining?.phone ?? '',
          }
        : undefined,
    excursion:
      part.type === 'excursion'
        ? {
            provider: part.excursion?.provider ?? '',
            ticketCount:
              part.excursion?.ticket_count != null ? String(part.excursion.ticket_count) : '',
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

  // Ice cream rating / what-ordered. Each field is sent only when it changed.
  if (form.iceCream && init.iceCream) {
    const c = form.iceCream;
    const ci = init.iceCream;
    const ice: NonNullable<UpdatePlanPartInput['ice_cream']> = {};
    if (c.rating !== ci.rating) ice.rating = c.rating;
    if (c.whatOrdered.trim() !== ci.whatOrdered.trim()) ice.what_ordered = c.whatOrdered.trim();
    if (Object.keys(ice).length > 0) patch.ice_cream = ice;
  }

  // The remaining per-type details. Each text field is sent only when changed
  // (trimmed); each count only when it changed to a valid non-negative number —
  // a count can be set or corrected but, like cost, not cleared back to unknown.
  if (form.hotel && init.hotel) {
    const h = form.hotel;
    const hi = init.hotel;
    const d: NonNullable<UpdatePlanPartInput['hotel']> = {};
    // The hotel's name is the single "Place" field (start_label); mirror an edit
    // of it into property_name so the map detail's "Property" row stays in sync
    // rather than asking for the name twice.
    if (form.start.label.trim() !== init.start.label.trim())
      d.property_name = form.start.label.trim();
    if (h.phone.trim() !== hi.phone.trim()) d.phone = h.phone.trim();
    if (h.roomType.trim() !== hi.roomType.trim()) d.room_type = h.roomType.trim();
    const guests = parseCount(h.guests);
    if (guests != null && h.guests.trim() !== hi.guests.trim()) d.guests = guests;
    if (Object.keys(d).length > 0) patch.hotel = d;
  }
  if (form.train && init.train) {
    const t = form.train;
    const ti = init.train;
    const d: NonNullable<UpdatePlanPartInput['train']> = {};
    if (t.operator.trim() !== ti.operator.trim()) d.operator = t.operator.trim();
    if (t.serviceNo.trim() !== ti.serviceNo.trim()) d.service_no = t.serviceNo.trim();
    if (t.coach.trim() !== ti.coach.trim()) d.coach = t.coach.trim();
    if (t.seat.trim() !== ti.seat.trim()) d.seat = t.seat.trim();
    if (t.cls.trim() !== ti.cls.trim()) d.class = t.cls.trim();
    if (t.platform.trim() !== ti.platform.trim()) d.platform = t.platform.trim();
    if (Object.keys(d).length > 0) patch.train = d;
  }
  if (form.ground && init.ground) {
    const g = form.ground;
    const gi = init.ground;
    const d: NonNullable<UpdatePlanPartInput['ground']> = {};
    if (g.provider.trim() !== gi.provider.trim()) d.provider = g.provider.trim();
    if (g.phone.trim() !== gi.phone.trim()) d.phone = g.phone.trim();
    if (g.vehicle.trim() !== gi.vehicle.trim()) d.vehicle = g.vehicle.trim();
    if (g.driver.trim() !== gi.driver.trim()) d.driver = g.driver.trim();
    const pax = parseCount(g.pax);
    if (pax != null && g.pax.trim() !== gi.pax.trim()) d.pax = pax;
    if (Object.keys(d).length > 0) patch.ground = d;
  }
  if (form.dining && init.dining) {
    const dn = form.dining;
    const di = init.dining;
    const d: NonNullable<UpdatePlanPartInput['dining']> = {};
    if (dn.reservationName.trim() !== di.reservationName.trim())
      d.reservation_name = dn.reservationName.trim();
    if (dn.phone.trim() !== di.phone.trim()) d.phone = dn.phone.trim();
    const partySize = parseCount(dn.partySize);
    if (partySize != null && dn.partySize.trim() !== di.partySize.trim()) d.party_size = partySize;
    if (Object.keys(d).length > 0) patch.dining = d;
  }
  if (form.excursion && init.excursion) {
    const e = form.excursion;
    const ei = init.excursion;
    const d: NonNullable<UpdatePlanPartInput['excursion']> = {};
    if (e.provider.trim() !== ei.provider.trim()) d.provider = e.provider.trim();
    const ticketCount = parseCount(e.ticketCount);
    if (ticketCount != null && e.ticketCount.trim() !== ei.ticketCount.trim())
      d.ticket_count = ticketCount;
    if (Object.keys(d).length > 0) patch.excursion = d;
  }

  return Object.keys(patch).length > 0 ? patch : null;
}

/** Parse an optional count field: a finite, non-negative integer, else
 * undefined (blank or invalid — left unchanged on save). */
function parseCount(v: string): number | undefined {
  const t = v.trim();
  if (t === '') return undefined;
  const n = Number(t);
  return Number.isFinite(n) && n >= 0 ? Math.trunc(n) : undefined;
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
  // Offline: the dialog still opens so you can read a plan's full details (more
  // than the timeline tile shows), but every control is read-only and Save is
  // disabled — editing needs the server.
  const readOnly = !useOnlineStatus();
  // Ice cream is a casual stop, not a booking: a parlour has no ticket or
  // supplier, and its "confirmation" is really just the name a table was held
  // under — so those fields are dropped/relabelled for it.
  const isIceCream = plan.type === 'ice_cream';

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
    // Only needed to populate the "move to another trip" picker, which is
    // disabled offline — skip the fetch (and its avoidable error churn) then.
    if (!readOnly) void listTrips();
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

  const patchIceCream = (partId: number, field: keyof IceCreamForm, value: string | number) => {
    setForms((prev) => {
      const f = prev[partId].iceCream;
      if (!f) return prev;
      return { ...prev, [partId]: { ...prev[partId], iceCream: { ...f, [field]: value } } };
    });
  };

  // One updater for the remaining per-type detail sub-forms — each is a flat
  // record of string fields, so a single keyed merge serves all of them.
  type DetailKey = 'hotel' | 'train' | 'ground' | 'dining' | 'excursion';
  const patchDetail = (partId: number, key: DetailKey, field: string, value: string) => {
    setForms((prev) => {
      const sub = prev[partId][key];
      if (!sub) return prev;
      return {
        ...prev,
        [partId]: { ...prev[partId], [key]: { ...sub, [field]: value } as typeof sub },
      };
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
        {readOnly && (
          <Alert severity="info" sx={{ mb: 2 }}>
            You&apos;re offline — viewing only. Reconnect to edit.
          </Alert>
        )}
        {/* A disabled fieldset makes every nested control read-only in one go,
            so an offline transition can't slip an edit through. Reset the
            element's native border/spacing so layout is unchanged. */}
        <Box
          component="fieldset"
          disabled={readOnly}
          sx={{ border: 0, m: 0, p: 0, minInlineSize: 0 }}
        >
          <Stack spacing={2} sx={{ mt: 0.5 }}>
            <TextField
              label="Title"
              value={title}
              onChange={(e) => setTitle(e.target.value)}
              required
              fullWidth
            />
            <TextField
              label={isIceCream ? 'Reservation name' : 'Confirmation ref'}
              value={confRef}
              onChange={(e) => setConfRef(e.target.value)}
              fullWidth
            />
            {!isIceCream && (
              <TextField
                label="Ticket number"
                value={ticketNumber}
                onChange={(e) => setTicketNumber(e.target.value)}
                fullWidth
              />
            )}
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
            {!isIceCream && (
              <TextField
                label="Supplier"
                value={supplierName}
                onChange={(e) => setSupplierName(e.target.value)}
                placeholder="Who the booking is with, e.g. British Airways"
                fullWidth
              />
            )}
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

            <PlanAttachments planId={plan.id} attachments={plan.attachments} readOnly={readOnly} />

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
                  {form.iceCream && (
                    <Box sx={{ mt: 1.5 }}>
                      <IceCreamFields
                        form={form.iceCream}
                        onChange={(f, v) => patchIceCream(part.id, f, v)}
                      />
                    </Box>
                  )}
                  {form.hotel && (
                    <Box sx={{ mt: 1.5 }}>
                      <HotelFields
                        form={form.hotel}
                        onChange={(f, v) => patchDetail(part.id, 'hotel', f, v)}
                      />
                    </Box>
                  )}
                  {form.train && (
                    <Box sx={{ mt: 1.5 }}>
                      <TrainFields
                        form={form.train}
                        onChange={(f, v) => patchDetail(part.id, 'train', f, v)}
                      />
                    </Box>
                  )}
                  {form.ground && (
                    <Box sx={{ mt: 1.5 }}>
                      <GroundFields
                        form={form.ground}
                        onChange={(f, v) => patchDetail(part.id, 'ground', f, v)}
                      />
                    </Box>
                  )}
                  {form.dining && (
                    <Box sx={{ mt: 1.5 }}>
                      <DiningFields
                        form={form.dining}
                        onChange={(f, v) => patchDetail(part.id, 'dining', f, v)}
                      />
                    </Box>
                  )}
                  {form.excursion && (
                    <Box sx={{ mt: 1.5 }}>
                      <ExcursionFields
                        form={form.excursion}
                        onChange={(f, v) => patchDetail(part.id, 'excursion', f, v)}
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
        </Box>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>{readOnly ? 'Close' : 'Cancel'}</Button>
        <Button
          variant="contained"
          onClick={() => void handleSave()}
          disabled={busy || !title.trim() || readOnly}
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

/** Ice cream inputs: a 0–5 star rating to score the find and a free-text note
 * of what was ordered. Both are saved on the part's ice-cream satellite. */
function IceCreamFields({
  form,
  onChange,
}: {
  form: IceCreamForm;
  onChange: (field: keyof IceCreamForm, value: string | number) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Ice cream
      </Typography>
      <Stack direction="row" spacing={1} alignItems="center">
        <Typography variant="body2" color="text.secondary">
          Rating
        </Typography>
        <Rating
          name="ice-cream-rating"
          value={form.rating}
          onChange={(_, value) => onChange('rating', value ?? 0)}
        />
      </Stack>
      <TextField
        label="What was ordered"
        size="small"
        value={form.whatOrdered}
        onChange={(e) => onChange('whatOrdered', e.target.value)}
        placeholder="e.g. Pistachio &amp; stracciatella cone"
        fullWidth
        multiline
        minRows={2}
      />
    </Stack>
  );
}

/** Hotel detail inputs: property name, phone, room type and guest count — the
 * fields the map list shows for a stay (PartDetailBlock). */
function HotelFields({
  form,
  onChange,
}: {
  form: HotelForm;
  onChange: (field: keyof HotelForm, value: string) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Hotel
      </Typography>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Room type"
          size="small"
          value={form.roomType}
          onChange={(e) => onChange('roomType', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Guests"
          type="number"
          size="small"
          value={form.guests}
          onChange={(e) => onChange('guests', e.target.value)}
          slotProps={{ htmlInput: { min: 0, step: 1 } }}
          sx={{ flex: 1 }}
        />
      </Stack>
      <TextField
        label="Phone"
        type="tel"
        size="small"
        value={form.phone}
        onChange={(e) => onChange('phone', e.target.value)}
        fullWidth
      />
    </Stack>
  );
}

/** Train detail inputs: operator, service, class and coach/seat/platform. */
function TrainFields({
  form,
  onChange,
}: {
  form: TrainForm;
  onChange: (field: keyof TrainForm, value: string) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Train
      </Typography>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Operator"
          size="small"
          value={form.operator}
          onChange={(e) => onChange('operator', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Service no."
          size="small"
          value={form.serviceNo}
          onChange={(e) => onChange('serviceNo', e.target.value)}
          sx={{ flex: 1 }}
        />
      </Stack>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Class"
          size="small"
          value={form.cls}
          onChange={(e) => onChange('cls', e.target.value)}
          sx={{ flex: 1 }}
        />
        <TextField
          label="Coach"
          size="small"
          value={form.coach}
          onChange={(e) => onChange('coach', e.target.value)}
          sx={{ flex: 1 }}
        />
        <TextField
          label="Seat"
          size="small"
          value={form.seat}
          onChange={(e) => onChange('seat', e.target.value)}
          sx={{ flex: 1 }}
        />
        <TextField
          label="Platform"
          size="small"
          value={form.platform}
          onChange={(e) => onChange('platform', e.target.value)}
          sx={{ flex: 1 }}
        />
      </Stack>
    </Stack>
  );
}

/** Ground-transport detail inputs: provider, phone, vehicle, driver, pax. */
function GroundFields({
  form,
  onChange,
}: {
  form: GroundForm;
  onChange: (field: keyof GroundForm, value: string) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Ground transport
      </Typography>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Provider"
          size="small"
          value={form.provider}
          onChange={(e) => onChange('provider', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Phone"
          type="tel"
          size="small"
          value={form.phone}
          onChange={(e) => onChange('phone', e.target.value)}
          sx={{ flex: 1 }}
        />
      </Stack>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Vehicle"
          size="small"
          value={form.vehicle}
          onChange={(e) => onChange('vehicle', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Driver"
          size="small"
          value={form.driver}
          onChange={(e) => onChange('driver', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Passengers"
          type="number"
          size="small"
          value={form.pax}
          onChange={(e) => onChange('pax', e.target.value)}
          slotProps={{ htmlInput: { min: 0, step: 1 } }}
          sx={{ flex: 1 }}
        />
      </Stack>
    </Stack>
  );
}

/** Dining detail inputs: reservation name, party size, phone. */
function DiningFields({
  form,
  onChange,
}: {
  form: DiningForm;
  onChange: (field: keyof DiningForm, value: string) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Dining
      </Typography>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Reservation name"
          size="small"
          value={form.reservationName}
          onChange={(e) => onChange('reservationName', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Party size"
          type="number"
          size="small"
          value={form.partySize}
          onChange={(e) => onChange('partySize', e.target.value)}
          slotProps={{ htmlInput: { min: 0, step: 1 } }}
          sx={{ flex: 1 }}
        />
      </Stack>
      <TextField
        label="Phone"
        type="tel"
        size="small"
        value={form.phone}
        onChange={(e) => onChange('phone', e.target.value)}
        fullWidth
      />
    </Stack>
  );
}

/** Excursion detail inputs: provider and ticket count. */
function ExcursionFields({
  form,
  onChange,
}: {
  form: ExcursionForm;
  onChange: (field: keyof ExcursionForm, value: string) => void;
}) {
  return (
    <Stack spacing={1.5}>
      <Typography variant="overline" color="text.secondary" sx={{ lineHeight: 1 }}>
        Excursion
      </Typography>
      <Stack direction="row" spacing={1}>
        <TextField
          label="Provider"
          size="small"
          value={form.provider}
          onChange={(e) => onChange('provider', e.target.value)}
          sx={{ flex: 2 }}
        />
        <TextField
          label="Tickets"
          type="number"
          size="small"
          value={form.ticketCount}
          onChange={(e) => onChange('ticketCount', e.target.value)}
          slotProps={{ htmlInput: { min: 0, step: 1 } }}
          sx={{ flex: 1 }}
        />
      </Stack>
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
          placeholder="optional: e.g. 48.2105, 4.0823 or a Google Maps link"
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
      <TimezoneSelect
        value={form.tz}
        onChange={(tz) => onChange('tz', tz)}
        placeholder="UTC"
        helperText="IANA name, e.g. Europe/London. Blank = UTC."
      />
    </Stack>
  );
}
