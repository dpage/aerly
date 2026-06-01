import { useMemo, useState } from 'react';
import { Link as RouterLink } from 'react-router-dom';
import {
  Box,
  Button,
  Card,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
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

  // Track the open plan by id (not a captured snapshot) so the detail dialog
  // reflects live updates after a privacy/passenger/alert mutation reloads the
  // trip, rather than showing a stale copy.
  const [planDetailId, setPlanDetailId] = useState<number | null>(null);
  const planDetail = useMemo(
    () => plans.find((p) => p.id === planDetailId) ?? null,
    [plans, planDetailId],
  );
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
            {day.parts.map(({ part, plan, edge }) => (
              <PartCard
                key={`${part.id}${edge ? `-${edge}` : ''}`}
                part={part}
                plan={plan}
                edge={edge}
                accent={accentFor(planIds, plan.id)}
                multiPart={multiPartPlanIds.has(plan.id)}
                onOpenPlan={() => setPlanDetailId(plan.id)}
              />
            ))}
          </Stack>
        </Box>
      ))}

      <PlanDetailDialog plan={planDetail} trip={currentTrip} onClose={() => setPlanDetailId(null)} />
    </Box>
  );
}

interface PartCardProps {
  part: PlanPart;
  plan: Plan;
  /** Set for the two tiles of a multi-night hotel stay (see buildTimeline). */
  edge?: 'check-in' | 'check-out';
  accent: string;
  multiPart: boolean;
  onOpenPlan: () => void;
}

function PartCard({ part, plan, edge, accent, multiPart, onOpenPlan }: PartCardProps) {
  // A cancelled part stays on the timeline, greyed out, until it's tidied
  // away (PRD §6.2/§6.9). On a rebooking the OLD part is the one stamped
  // `status='cancelled'` and marked superseded — the NEW part carries
  // `supersedes_id` and stays full-colour. So we key the greying purely on
  // `status === 'cancelled'`, which also correctly greys a plain cancellation.
  // Dismissed parts are already dropped by buildTimeline().
  const greyed = part.status === 'cancelled';

  return (
    <Card
      variant="outlined"
      onClick={onOpenPlan}
      sx={{
        position: 'relative',
        cursor: 'pointer',
        opacity: greyed ? 0.55 : 1,
        borderLeft: `4px solid ${accent}`,
        '&:hover': { boxShadow: 1 },
      }}
      data-testid={`part-card-${part.id}${edge ? `-${edge}` : ''}`}
    >
      <Stack direction="row" spacing={1.5} sx={{ p: 1.5 }} alignItems="flex-start">
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

          <Typography variant="body2" color="text.secondary" noWrap>
            {part.start_label}
            {part.end_label ? ` → ${part.end_label}` : ''}
          </Typography>

          <Typography variant="caption" color="text.secondary">
            {edge === 'check-in'
              ? `Check in · ${fmtTimeOfDay(part.starts_at, part.start_tz)}`
              : edge === 'check-out'
                ? `Check out · ${fmtTimeOfDay(part.ends_at ?? part.starts_at, part.end_tz || part.start_tz)}`
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
              onClick={(e) => e.stopPropagation()}
              variant="caption"
              sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, mt: 0.25 }}
            >
              Track {part.flight.ident || plan.title} <OpenInNewIcon sx={{ fontSize: 12 }} />
            </Link>
          )}
        </Box>
      </Stack>
    </Card>
  );
}

/** Tap-through detail: lists the whole plan and all its parts so a user who
 * tapped any one part sees the entire booking (PRD §6.2). Owners/editors get
 * the per-plan "Who can see this?" + passengers control and a delete action
 * (PRD §6.4); viewers get the per-plan "Notify me of changes" opt-in (§6.8). */
function PlanDetailDialog({ plan, trip, onClose }: { plan: Plan | null; trip: Trip; onClose: () => void }) {
  const deletePlan = useStore((s) => s.deletePlan);
  const setError = useStore((s) => s.setError);
  const [privacyOpen, setPrivacyOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [busy, setBusy] = useState(false);

  const canEdit = trip.my_role === 'owner' || trip.my_role === 'editor';
  const isViewer = trip.my_role === 'viewer';

  if (!plan) return null;
  const parts = [...plan.parts].sort(
    (a, b) => new Date(a.effective_at ?? a.starts_at).getTime() - new Date(b.effective_at ?? b.starts_at).getTime(),
  );

  const handleDelete = async () => {
    if (!window.confirm(`Delete "${plan.title || planTypeLabel(plan.type)}" from this trip?`)) return;
    setBusy(true);
    try {
      await deletePlan(plan.id);
      onClose();
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    } finally {
      setBusy(false);
    }
  };

  return (
    <>
      <Dialog open={plan !== null} onClose={onClose} fullWidth maxWidth="xs">
        <DialogTitle>
          <Stack direction="row" alignItems="center" spacing={1}>
            <PlanTypeIcon type={plan.type} fontSize="small" />
            <span>{plan.title || planTypeLabel(plan.type)}</span>
          </Stack>
        </DialogTitle>
        <DialogContent>
          {plan.confirmation_ref && (
            <Typography variant="body2" color="text.secondary" gutterBottom>
              Confirmation: {plan.confirmation_ref}
            </Typography>
          )}
          <Stack spacing={1.5} sx={{ mt: 1 }}>
            {parts.map((part) => (
              <Box key={part.id} sx={{ opacity: part.dismissed_at ? 0.5 : 1 }}>
                <Typography variant="subtitle2">
                  {part.start_label}
                  {part.end_label ? ` → ${part.end_label}` : ''}
                </Typography>
                <Typography variant="caption" color="text.secondary">
                  {fmtPartTimeRange(part)}
                </Typography>
                {(part.start_address || part.end_address) && (
                  <Typography
                    variant="caption"
                    color="text.secondary"
                    sx={{ display: 'block', mt: 0.25 }}
                  >
                    {[part.start_address, part.end_address].filter(Boolean).join(' → ')}
                  </Typography>
                )}
              </Box>
            ))}
          </Stack>
          {plan.notes && (
            <Typography variant="body2" color="text.secondary" sx={{ mt: 2, whiteSpace: 'pre-wrap' }}>
              {plan.notes}
            </Typography>
          )}
          {isViewer && (
            <Box sx={{ mt: 2, pt: 1, borderTop: 1, borderColor: 'divider' }}>
              <PlanAlertToggle plan={plan} />
            </Box>
          )}
        </DialogContent>
        <DialogActions>
          {canEdit && (
            <Button color="error" onClick={() => void handleDelete()} disabled={busy} sx={{ mr: 'auto' }}>
              Delete
            </Button>
          )}
          {canEdit && <Button onClick={() => setEditOpen(true)}>Edit</Button>}
          {canEdit && <Button onClick={() => setPrivacyOpen(true)}>Privacy &amp; passengers</Button>}
          <Button onClick={onClose}>Close</Button>
        </DialogActions>
      </Dialog>
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
    </>
  );
}
