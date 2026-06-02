import { useEffect, useMemo, useState } from 'react';
import {
  Avatar,
  Box,
  Button,
  Checkbox,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  FormControl,
  FormControlLabel,
  FormLabel,
  InputLabel,
  ListItemText,
  MenuItem,
  OutlinedInput,
  Radio,
  RadioGroup,
  Select,
  Stack,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';

import type { Plan, PlanVisibilityMode, TripMember, User } from '../api/types';
import { userInitial, userName } from '../lib/format';
import { useFriendUsers } from '../state/friendUsers';
import { useStore } from '../state/store';

interface Props {
  open: boolean;
  plan: Plan;
  /** Members of the plan's trip, used to scope the "who can see this" picker. */
  members: TripMember[];
  onClose: () => void;
}

/** Per-plan privacy + passengers (spec §6.4).
 *
 * "Who can see this?" offers Everyone / Hidden from… / Only visible to…; the
 * latter two reveal a member multi-select. Passengers are managed separately —
 * adding a passenger auto-grants them trip-viewer access server-side, which the
 * copy makes explicit. */
export default function PlanPrivacyDialog({ open, plan, members, onClose }: Props) {
  const users = useStore((s) => s.users);
  const setPlanVisibility = useStore((s) => s.setPlanVisibility);
  const addPlanPassenger = useStore((s) => s.addPlanPassenger);
  const removePlanPassenger = useStore((s) => s.removePlanPassenger);
  const setError = useStore((s) => s.setError);
  const openHelp = useStore((s) => s.openHelp);
  const friends = useFriendUsers();

  const [mode, setMode] = useState<PlanVisibilityMode>(plan.visibility.mode);
  const [scopeIds, setScopeIds] = useState<number[]>(plan.visibility.user_ids);
  const [pax, setPax] = useState<number | ''>('');
  const [busy, setBusy] = useState(false);

  // Re-sync local state when the dialog (re)opens or switches to a different
  // plan. Deliberately NOT keyed on plan.visibility.* — while the dialog is
  // open, an unrelated refetch (e.g. adding a passenger, or a live SSE update)
  // hands down a fresh plan object, and re-syncing then would clobber an
  // in-progress, unsaved "Who can see this?" selection. The persisted values
  // are picked up again on the next open.
  useEffect(() => {
    if (!open) return;
    setMode(plan.visibility.mode);
    setScopeIds(plan.visibility.user_ids);
    setPax('');
    // eslint-disable-next-line react-hooks/exhaustive-deps -- intentional: sync only on (re)open / plan switch
  }, [open, plan.id]);

  const userIndex = useMemo(() => {
    const m = new Map<number, User>();
    for (const u of users) m.set(u.id, u);
    return m;
  }, [users]);

  const label = (id: number): string => {
    const u = userIndex.get(id);
    return u ? userName(u) : `User #${id}`;
  };

  // The scope picker chooses among trip members (excluding the viewer's own
  // owner row isn't necessary — the server resolves owner/passenger access).
  const memberOptions = useMemo(
    () => members.map((m) => m.user_id),
    [members],
  );

  // Passengers are picked from accepted friends not already aboard.
  const paxCandidates = useMemo(
    () => friends.filter((f) => !plan.passenger_ids.includes(f.id)),
    [friends, plan.passenger_ids],
  );

  const reportError = (err: unknown) =>
    setError(err instanceof Error ? err.message : String(err));

  const handleSave = async () => {
    setBusy(true);
    try {
      await setPlanVisibility(plan.id, {
        mode,
        // Only the scoped modes carry a user list; "everyone" sends an empty one.
        user_ids: mode === 'everyone' ? [] : scopeIds,
      });
      onClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleAddPax = async () => {
    if (pax === '') return;
    setBusy(true);
    try {
      await addPlanPassenger(plan.id, pax);
      setPax('');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleRemovePax = async (userId: number) => {
    try {
      await removePlanPassenger(plan.id, userId);
    } catch (err) {
      reportError(err);
    }
  };

  const scopeLabel =
    mode === 'hidden_from' ? 'Hidden from' : mode === 'only_visible_to' ? 'Only visible to' : '';

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Privacy &amp; passengers</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          <FormControl>
            <FormLabel id="plan-visibility-label">Who can see this?</FormLabel>
            <RadioGroup
              aria-labelledby="plan-visibility-label"
              value={mode}
              onChange={(e) => setMode(e.target.value as PlanVisibilityMode)}
            >
              <FormControlLabel
                value="everyone"
                control={<Radio />}
                label="Everyone on the trip"
              />
              <FormControlLabel value="hidden_from" control={<Radio />} label="Hidden from…" />
              <FormControlLabel
                value="only_visible_to"
                control={<Radio />}
                label="Only visible to…"
              />
            </RadioGroup>

            {mode !== 'everyone' && (
              <FormControl size="small" sx={{ mt: 1 }} fullWidth>
                <InputLabel id="plan-scope-label" shrink>
                  {scopeLabel}
                </InputLabel>
                <Select
                  multiple
                  displayEmpty
                  labelId="plan-scope-label"
                  label={scopeLabel}
                  value={scopeIds.map(String)}
                  onChange={(e) => {
                    const v = e.target.value;
                    const arr = typeof v === 'string' ? v.split(',') : v;
                    setScopeIds(arr.map(Number));
                  }}
                  input={<OutlinedInput label={scopeLabel} />}
                  renderValue={(selected) =>
                    selected.length === 0 ? (
                      <Typography component="span" color="text.secondary">
                        Choose members…
                      </Typography>
                    ) : (
                      <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5 }}>
                        {selected.map((id) => (
                          <Chip key={id} label={label(Number(id))} size="small" />
                        ))}
                      </Box>
                    )
                  }
                >
                  {memberOptions.map((id) => (
                    <MenuItem key={id} value={String(id)}>
                      <Checkbox checked={scopeIds.includes(id)} size="small" />
                      <ListItemText primary={label(id)} />
                    </MenuItem>
                  ))}
                </Select>
              </FormControl>
            )}
          </FormControl>

          <Box>
            <Typography variant="subtitle2" sx={{ mb: 0.5 }}>
              Passengers
            </Typography>
            <Typography variant="caption" color="text.secondary">
              Adding a passenger also grants them viewer access to the whole trip.
            </Typography>
            <Box sx={{ display: 'flex', flexWrap: 'wrap', gap: 0.5, mt: 1 }}>
              {plan.passenger_ids.length === 0 ? (
                <Typography variant="body2" color="text.secondary">
                  No passengers yet.
                </Typography>
              ) : (
                plan.passenger_ids.map((id) => {
                  const u = userIndex.get(id);
                  return (
                    <Chip
                      key={id}
                      avatar={
                        <Avatar src={u?.avatar_url}>
                          {u ? userInitial(u) : label(id).charAt(0).toUpperCase()}
                        </Avatar>
                      }
                      label={label(id)}
                      onDelete={() => void handleRemovePax(id)}
                      deleteIcon={
                        <Tooltip title="Remove passenger">
                          <DeleteOutlineIcon aria-label={`Remove passenger ${label(id)}`} />
                        </Tooltip>
                      }
                      size="small"
                    />
                  );
                })
              )}
            </Box>
            <Stack direction="row" spacing={1} alignItems="center" sx={{ mt: 1.5 }}>
              <TextField
                select
                size="small"
                label="Add passenger"
                fullWidth
                value={pax === '' ? '' : String(pax)}
                onChange={(e) => setPax(e.target.value === '' ? '' : Number(e.target.value))}
                disabled={paxCandidates.length === 0}
                helperText={paxCandidates.length === 0 ? 'No friends left to add.' : undefined}
                slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
              >
                <MenuItem value="" disabled>
                  Choose a friend…
                </MenuItem>
                {paxCandidates.map((u) => (
                  <MenuItem key={u.id} value={String(u.id)}>
                    {userName(u)}
                  </MenuItem>
                ))}
              </TextField>
              <Button
                variant="outlined"
                onClick={() => void handleAddPax()}
                disabled={busy || pax === ''}
              >
                Add
              </Button>
            </Stack>
          </Box>
        </Stack>
      </DialogContent>
      <DialogActions sx={{ justifyContent: 'space-between' }}>
        <Button size="small" color="inherit" onClick={() => openHelp('sharing')}>
          How sharing works
        </Button>
        <Box>
          <Button onClick={onClose}>Cancel</Button>
          <Button variant="contained" onClick={() => void handleSave()} disabled={busy}>
            Save visibility
          </Button>
        </Box>
      </DialogActions>
    </Dialog>
  );
}
