import { type MouseEvent, useMemo, useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import {
  Box,
  Button,
  Card,
  Chip,
  Collapse,
  Divider,
  Link,
  Stack,
  Typography,
} from '@mui/material';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';

import { useStore } from '../state/store';
import type { Plan, PlanPart, Trip } from '../api/types';
import PlanTypeIcon from '../components/PlanTypeIcon';
import PlanPrivacyDialog from '../components/PlanPrivacyDialog';
import PlanEditDialog from '../components/PlanEditDialog';
import PlanAlertToggle from '../components/PlanAlertToggle';
import AddToTripDialog from '../components/AddToTripDialog';
import {
  buildTimeline,
  fmtPartPlaces,
  fmtPartTimeRange,
  fmtTimeOfDay,
  planTypeLabel,
} from '../lib/trip-format';

// Accent palette used to visually tie a plan's parts together (PRD §6.2). A
// plan's parts all share the same accent stripe and connector, so a return
// flight's two legs read as one booking even days apart. Colours are assigned
// by stable order of plan id so the same plan keeps its colour across renders.
const ACCENTS = ['#1f5fa8', '#d97706', '#2e7d32', '#7b1fa2', '#c2185b', '#00838f', '#5d4037'];

function accentFor(planIds: number[], planId: number): string {
  const idx = planIds.indexOf(planId);
  return ACCENTS[(idx < 0 ? 0 : idx) % ACCENTS.length];
}

/** Default trip detail view (spec §11, PRD §6.2): a day-grouped vertical list
 * of plan parts sorted by `effective_at`, with sticky local-day headers, the
 * right MUI icon per type, local-time ranges, parts of one plan visually tied
 * together, multi-night hotels as a band, and superseded parts greyed. */
export default function TripTimeline() {
  const currentTrip = useStore((s) => s.currentTrip);
  const plans = useMemo(() => currentTrip?.plans ?? [], [currentTrip]);

  const days = useMemo(() => buildTimeline(plans), [plans]);
  // Stable plan ordering for accent assignment (first appearance on timeline).
  const planIds = useMemo(() => {
    const seen: number[] = [];
    for (const d of days) {
      for (const { plan } of d.parts) if (!seen.includes(plan.id)) seen.push(plan.id);
    }
    return seen;
  }, [days]);

  // Which plan ids have more than one distinct part — only those get the
  // "part of a multi-part booking" connector treatment. Count distinct part
  // ids (not timeline tiles), so a single hotel stay shown as two check-in /
  // check-out tiles isn't mistaken for a multi-part booking.
  const multiPartPlanIds = useMemo(() => {
    const partsByPlan = new Map<number, Set<number>>();
    for (const d of days)
      for (const { plan, part } of d.parts) {
        let set = partsByPlan.get(plan.id);
        if (!set) partsByPlan.set(plan.id, (set = new Set()));
        set.add(part.id);
      }
    return new Set([...partsByPlan].filter(([, s]) => s.size > 1).map(([id]) => id));
  }, [days]);

  // Tiles expand in place (multiple at once) rather than opening a modal, so a
  // whole day can be unfolded side by side. Keyed by the tile key.
  const [expanded, setExpanded] = useState<ReadonlySet<string>>(() => new Set());
  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  const [addOpen, setAddOpen] = useState(false);

  if (!currentTrip) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">Loading…</Typography>
      </Box>
    );
  }

  if (days.length === 0) {
    return (
      <Box sx={{ p: 3 }}>
        <Typography color="text.secondary">
          Nothing on this trip yet. Use{' '}
          <Link
            component="button"
            type="button"
            onClick={() => setAddOpen(true)}
            sx={{ verticalAlign: 'baseline', fontWeight: 600 }}
          >
            Add to trip
          </Link>{' '}
          to add a flight, hotel, or other plan.
        </Typography>
        {addOpen && (
          <AddToTripDialog open={addOpen} tripId={currentTrip.id} onClose={() => setAddOpen(false)} />
        )}
      </Box>
    );
  }

  return (
    <Box sx={{ p: 3, maxWidth: 760, mx: 'auto' }}>
      {days.map((day) => (
        <Box key={day.dayKey} sx={{ mb: 2 }}>
          <Typography
            variant="subtitle2"
            color="text.secondary"
            sx={{
              position: 'sticky',
              top: 0,
              zIndex: 1,
              py: 0.75,
              bgcolor: 'background.default',
              borderBottom: 1,
              borderColor: 'divider',
            }}
          >
            {day.label}
          </Typography>
          <Stack spacing={1.5} sx={{ mt: 1.5 }}>
            {day.parts.map(({ part, plan, edge }) => {
              const key = `${part.id}${edge ? `-${edge}` : ''}`;
              return (
                <PartCard
                  key={key}
                  part={part}
                  plan={plan}
                  trip={currentTrip}
                  edge={edge}
                  accent={accentFor(planIds, plan.id)}
                  multiPart={multiPartPlanIds.has(plan.id)}
                  expanded={expanded.has(key)}
                  onToggle={() => toggle(key)}
                />
              );
            })}
          </Stack>
        </Box>
      ))}
    </Box>
  );
}

interface PartCardProps {
  part: PlanPart;
  plan: Plan;
  trip: Trip;
  /** Set for the two tiles of a multi-night hotel stay (see buildTimeline). */
  edge?: 'check-in' | 'check-out';
  accent: string;
  multiPart: boolean;
  expanded: boolean;
  onToggle: () => void;
}

/** A timeline tile. Tapping the header expands it in place (PRD §6.2 tap-through
 * to the whole plan) to reveal the address, type-specific detail, notes and the
 * per-plan actions — owners/editors get Edit / "Privacy & passengers" / Delete
 * (§6.4), viewers get the "Notify me of changes" opt-in (§6.8). */
function PartCard({ part, plan, trip, edge, accent, multiPart, expanded, onToggle }: PartCardProps) {
  const deletePlan = useStore((s) => s.deletePlan);
  const setError = useStore((s) => s.setError);
  const [privacyOpen, setPrivacyOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const canEdit = trip.my_role === 'owner' || trip.my_role === 'editor';
  const isViewer = trip.my_role === 'viewer';

  // A cancelled part stays on the timeline, greyed out, until it's tidied
  // away (PRD §6.2/§6.9). On a rebooking the OLD part is the one stamped
  // `status='cancelled'` and marked superseded — the NEW part carries
  // `supersedes_id` and stays full-colour. So we key the greying purely on
  // `status === 'cancelled'`, which also correctly greys a plain cancellation.
  // Dismissed parts are already dropped by buildTimeline().
  const greyed = part.status === 'cancelled';
  const places = fmtPartPlaces(part.type, part.start_label, part.end_label);
  const addr = fmtPartPlaces(part.type, part.start_address, part.end_address);
  const details = partDetailLines(part);

  const handleDelete = async () => {
    if (!window.confirm(`Delete "${plan.title || planTypeLabel(plan.type)}" from this trip?`)) return;
    setBusy(true);
    try {
      await deletePlan(plan.id);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  // Clicks inside the expanded body shouldn't fold the card back up.
  const stop = (e: MouseEvent) => e.stopPropagation();

  return (
    <Card
      variant="outlined"
      onClick={onToggle}
      sx={{
        position: 'relative',
        opacity: greyed ? 0.55 : 1,
        borderLeft: `4px solid ${accent}`,
      }}
      data-testid={`part-card-${part.id}${edge ? `-${edge}` : ''}`}
    >
      <Stack
        direction="row"
        spacing={1.5}
        sx={{ p: 1.5, cursor: 'pointer', '&:hover': { bgcolor: 'action.hover' } }}
        alignItems="flex-start"
      >
        <PlanTypeIcon type={part.type} sx={{ color: accent, mt: 0.25 }} />
        <Box sx={{ flexGrow: 1, minWidth: 0 }}>
          <Stack direction="row" alignItems="center" spacing={1}>
            <Typography variant="subtitle2" sx={{ fontWeight: 600 }} noWrap>
              {plan.title || planTypeLabel(part.type)}
            </Typography>
            {multiPart && (
              <Chip
                label="multi-part"
                size="small"
                variant="outlined"
                sx={{ height: 18, fontSize: 10, borderColor: accent, color: accent }}
              />
            )}
            {part.status === 'confirmed' && (
              <Chip label="confirmed" size="small" color="success" variant="outlined" sx={{ height: 18, fontSize: 10 }} />
            )}
            {greyed && (
              <Chip label="cancelled" size="small" color="warning" variant="outlined" sx={{ height: 18, fontSize: 10 }} />
            )}
          </Stack>

          {places && places !== (plan.title || planTypeLabel(part.type)) && (
            <Typography variant="body2" color="text.secondary" noWrap>
              {places}
            </Typography>
          )}

          <Typography variant="caption" color="text.secondary">
            {edge === 'check-in'
              ? `Check in · ${localClock(part.starts_at, part.start_tz)}`
              : edge === 'check-out'
                ? `Check out · ${localClock(part.ends_at ?? part.starts_at, part.end_tz || part.start_tz)}`
                : fmtPartTimeRange(part)}
          </Typography>

          {plan.confirmation_ref && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              Ref: {plan.confirmation_ref}
            </Typography>
          )}

          {part.type === 'flight' && part.flight && (
            <Link
              component={RouterLink}
              to={`/tracker?part=${part.id}`}
              onClick={stop}
              variant="caption"
              sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, mt: 0.25 }}
            >
              Track {part.flight.ident || plan.title} <OpenInNewIcon sx={{ fontSize: 12 }} />
            </Link>
          )}
        </Box>
      </Stack>

      <Collapse in={expanded} unmountOnExit>
        <Box onClick={stop} sx={{ px: 1.5, pb: 1.5, pl: 5.5 }}>
          <Divider sx={{ mb: 1 }} />
          {addr && (
            <Typography variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {addr}
            </Typography>
          )}
          {details.map((line, i) => (
            <Typography key={i} variant="caption" color="text.secondary" sx={{ display: 'block' }}>
              {line}
            </Typography>
          ))}
          {plan.notes && (
            <Typography variant="body2" color="text.secondary" sx={{ mt: 1, whiteSpace: 'pre-wrap' }}>
              {plan.notes}
            </Typography>
          )}
          {isViewer && (
            <Box sx={{ mt: 0.5 }}>
              <PlanAlertToggle plan={plan} />
            </Box>
          )}
          {canEdit && (
            <Stack direction="row" spacing={1} sx={{ mt: 1 }}>
              <Button size="small" onClick={() => setEditOpen(true)}>
                Edit
              </Button>
              <Button size="small" onClick={() => setPrivacyOpen(true)}>
                Privacy &amp; passengers
              </Button>
              <Button size="small" color="error" onClick={() => void handleDelete()} disabled={busy}>
                Delete
              </Button>
            </Stack>
          )}
        </Box>
      </Collapse>

      {canEdit && privacyOpen && (
        <PlanPrivacyDialog
          open={privacyOpen}
          plan={plan}
          members={trip.members}
          onClose={() => setPrivacyOpen(false)}
        />
      )}
      {canEdit && editOpen && (
        <PlanEditDialog open={editOpen} plan={plan} onClose={() => setEditOpen(false)} />
      )}
    </Card>
  );
}

// A hotel check-in/out is a local wall-clock at the property. We don't store
// the property's timezone, so drop the "UTC" suffix fmtTimeOfDay adds for
// tz-less times — the digits are the local check-in/out the booking stated, and
// labelling them "UTC" is misleading.
function localClock(iso: string, tz?: string): string {
  return fmtTimeOfDay(iso, tz).replace(/\s*UTC$/, '');
}

/** Type-specific detail lines for the plan-detail dialog, so a tapped plan
 * shows what it actually is (room type, operator, reservation…) not just a
 * place and time. Each returned string renders as its own caption line. */
function partDetailLines(part: PlanPart): string[] {
  const join = (...bits: (string | undefined)[]) => bits.filter(Boolean).join(' · ');
  const out: string[] = [];
  switch (part.type) {
    case 'hotel':
      if (part.hotel) out.push(join(part.hotel.room_type, part.hotel.phone));
      break;
    case 'train':
      if (part.train) {
        out.push(join(part.train.operator, part.train.service_no, part.train.class));
        out.push(
          join(
            part.train.coach && `Coach ${part.train.coach}`,
            part.train.seat && `Seat ${part.train.seat}`,
            part.train.platform && `Platform ${part.train.platform}`,
          ),
        );
      }
      break;
    case 'ground':
      if (part.ground) out.push(join(part.ground.provider, part.ground.vehicle, part.ground.phone));
      break;
    case 'dining':
      if (part.dining)
        out.push(
          join(
            part.dining.reservation_name && `Reservation: ${part.dining.reservation_name}`,
            part.dining.phone,
          ),
        );
      break;
    case 'excursion':
      if (part.excursion) out.push(part.excursion.provider);
      break;
    case 'flight':
      if (part.flight) out.push(join(part.flight.ident, part.flight.flight_status));
      break;
  }
  return out.filter(Boolean);
}
