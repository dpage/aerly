import { useEffect, useMemo, useState } from 'react';
import {
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

import type { Plan, PlanPart, UpdatePlanPartInput } from '../api/types';
import { useStore } from '../state/store';
import { isTransferType, planTypeLabel, splitLocal, zonedTimeToUtc } from '../lib/trip-format';

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
}

interface PartForm {
  start: EndForm;
  end: EndForm;
}

function endForm(label: string, address: string, iso: string | undefined, tz: string): EndForm {
  const { date, time } = iso ? splitLocal(iso, tz) : { date: '', time: '' };
  return { label, address, date, time, tz };
}

function partForm(part: PlanPart): PartForm {
  return {
    start: endForm(part.start_label ?? '', part.start_address ?? '', part.starts_at, part.start_tz ?? ''),
    end: endForm(
      part.end_label ?? '',
      part.end_address ?? '',
      part.ends_at,
      part.end_tz || part.start_tz || '',
    ),
  };
}

/** Does this part have a meaningful "end" endpoint to edit — a transfer's
 * arrival, or anything that already carries an end time (e.g. a hotel's
 * check-out)? Single-point plans (a dining reservation) show only a start. */
function hasEnd(part: PlanPart): boolean {
  return isTransferType(part.type) || part.ends_at != null;
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

  if (hasEnd(part)) {
    const e = form.end;
    const ei = init.end;
    if (e.label !== ei.label) patch.end_label = e.label.trim();
    if (e.address !== ei.address) patch.end_address = e.address.trim();
    if (e.date !== ei.date || e.time !== ei.time || e.tz !== ei.tz) {
      if (e.date && e.time) patch.ends_at = zonedTimeToUtc(e.date, e.time, e.tz);
      if (e.tz !== ei.tz || patch.ends_at) patch.end_tz = e.tz;
    }
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

  const [title, setTitle] = useState(plan.title);
  const [confRef, setConfRef] = useState(plan.confirmation_ref);
  const [notes, setNotes] = useState(plan.notes);
  const [moveTarget, setMoveTarget] = useState<number | ''>('');
  const [busy, setBusy] = useState(false);

  // The editable parts (dismissed ones are hidden) and their initial snapshot.
  const editableParts = useMemo(() => plan.parts.filter((p) => !p.dismissed_at), [plan.parts]);
  // A multi-leg flight/train booking can have a leg split out into its own plan
  // when it wasn't really part of the same booking (#12).
  const canSplit = editableParts.length > 1 && (plan.type === 'flight' || plan.type === 'train');
  const [forms, setForms] = useState<Record<number, PartForm>>({});
  const [initial, setInitial] = useState<Record<number, PartForm>>({});

  // Re-sync the form when the dialog (re)opens or switches plans, and refresh
  // the trip list so the move targets reflect what the viewer can edit now.
  // Not keyed on plan.* fields so an in-flight refetch can't clobber edits.
  useEffect(() => {
    if (!open) return;
    setTitle(plan.title);
    setConfRef(plan.confirmation_ref);
    setNotes(plan.notes);
    setMoveTarget('');
    const snap: Record<number, PartForm> = {};
    for (const p of editableParts) snap[p.id] = partForm(p);
    setForms(snap);
    setInitial(snap);
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

  const reportError = (err: unknown) =>
    setError(err instanceof Error ? err.message : String(err));

  const patchEnd = (partId: number, which: 'start' | 'end', field: keyof EndForm, value: string) => {
    setForms((prev) => ({
      ...prev,
      [partId]: { ...prev[partId], [which]: { ...prev[partId][which], [field]: value } },
    }));
  };

  const handleSave = async () => {
    setBusy(true);
    try {
      if (title.trim() !== plan.title || confRef.trim() !== plan.confirmation_ref || notes !== plan.notes) {
        await updatePlan(plan.id, {
          title: title.trim(),
          confirmation_ref: confRef.trim(),
          notes,
        });
      }
      for (const part of editableParts) {
        const patch = buildPatch(part, forms[part.id], initial[part.id]);
        if (patch) await updatePlanPart(part.id, patch);
      }
      onClose();
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

/** The label / address / date / time / timezone inputs for one endpoint. When
 * timeOnly is set the Place/Address inputs are hidden — used for the "Until"
 * edge of a single-location part (a hotel's check-out shares the check-in
 * place), leaving only its date/time/timezone editable. */
function EndFields({
  heading,
  form,
  onChange,
  timeOnly = false,
}: {
  heading: string;
  form: EndForm;
  onChange: (field: keyof EndForm, value: string) => void;
  timeOnly?: boolean;
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
