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
  Divider,
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
  Switch,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';

import type { Plan, PlanVisibilityMode, TripMember, User } from '../api/types';
import { userInitial, userName } from '../lib/format';
import { useFriendCandidates } from '../state/friendUsers';
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
  const setPlanShareAllFriends = useStore((s) => s.setPlanShareAllFriends);
  const sharePlanByEmail = useStore((s) => s.sharePlanByEmail);
  const notifyPlanShares = useStore((s) => s.notifyPlanShares);
  const setError = useStore((s) => s.setError);
  const openHelp = useStore((s) => s.openHelp);
  const friendCandidates = useFriendCandidates();

  const [mode, setMode] = useState<PlanVisibilityMode>(plan.visibility.mode);
  const [scopeIds, setScopeIds] = useState<number[]>(plan.visibility.user_ids);
  const [pax, setPax] = useState<number | ''>('');
  const [busy, setBusy] = useState(false);
  // Local mirror of the all-friends flag so the Switch reflects a toggle
  // immediately (the plan prop only updates after the store refetch lands).
  const [shareAll, setShareAll] = useState(plan.share_all_friends);
  const [email, setEmail] = useState('');

  // People who newly gained access during this dialog session; on close we
  // offer to notify them. Reset whenever the dialog is (re)opened.
  const [addedUserIds, setAddedUserIds] = useState<Set<number>>(() => new Set());
  const [invitedEmails, setInvitedEmails] = useState<Set<string>>(() => new Set());
  const [notify, setNotify] = useState(true);

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
    setShareAll(plan.share_all_friends);
    setEmail('');
    setAddedUserIds(new Set());
    setInvitedEmails(new Set());
    setNotify(true);
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

  // Passengers are picked from friends (accepted + pending, so you can
  // pre-share to an invited person) not already aboard.
  const paxCandidates = useMemo(
    () => friendCandidates.filter((c) => !plan.passenger_ids.includes(c.user.id)),
    [friendCandidates, plan.passenger_ids],
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
      await handleClose();
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleAddPax = async () => {
    if (pax === '') return;
    const addedId = pax;
    setBusy(true);
    try {
      await addPlanPassenger(plan.id, addedId);
      setAddedUserIds((prev) => new Set(prev).add(addedId));
      setPax('');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleShareAllChange = async (checked: boolean) => {
    setShareAll(checked);
    setBusy(true);
    try {
      await setPlanShareAllFriends(plan.id, checked);
      // Turning it on newly grants access to every accepted friend who isn't
      // already a passenger; queue them for the close-time notify.
      if (checked) {
        setAddedUserIds((prev) => {
          const next = new Set(prev);
          for (const c of friendCandidates) {
            if (!c.pending && !plan.passenger_ids.includes(c.user.id)) next.add(c.user.id);
          }
          return next;
        });
      }
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleInviteEmail = async () => {
    const addr = email.trim();
    if (!addr) return;
    setBusy(true);
    try {
      await sharePlanByEmail(plan.id, addr);
      setInvitedEmails((prev) => new Set(prev).add(addr));
      setEmail('');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleClose = async () => {
    if (notify && addedUserIds.size + invitedEmails.size > 0) {
      try {
        await notifyPlanShares(plan.id, {
          user_ids: [...addedUserIds],
          emails: [...invitedEmails],
        });
      } catch {
        // Best-effort: never block closing the dialog on a notify failure.
      }
    }
    onClose();
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
    <Dialog open={open} onClose={() => void handleClose()} maxWidth="sm" fullWidth>
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
            <FormControlLabel
              control={
                <Switch
                  checked={shareAll}
                  onChange={(e) => void handleShareAllChange(e.target.checked)}
                  disabled={busy}
                />
              }
              label="Share with all friends"
            />
            <Typography variant="caption" color="text.secondary" component="div">
              Everyone you&apos;re friends with can see this plan automatically.
            </Typography>
          </Box>

          <Box>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              Invite by email
            </Typography>
            <Stack direction="row" spacing={1} alignItems="flex-start">
              <TextField
                size="small"
                type="email"
                label="Email"
                fullWidth
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                helperText="Pre-share to someone who isn't on Aerly yet."
              />
              <Button
                variant="outlined"
                onClick={() => void handleInviteEmail()}
                disabled={busy || email.trim() === ''}
              >
                Invite
              </Button>
            </Stack>
          </Box>

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
                {paxCandidates.map((c) => (
                  <MenuItem key={c.user.id} value={String(c.user.id)}>
                    {userName(c.user)}
                    {c.pending ? ' (invited)' : ''}
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
      {addedUserIds.size + invitedEmails.size > 0 && (
        <>
          <Divider />
          <Box sx={{ px: 3, py: 1 }}>
            <FormControlLabel
              control={
                <Checkbox
                  size="small"
                  checked={notify}
                  onChange={(e) => setNotify(e.target.checked)}
                />
              }
              label="Notify the people I just added"
            />
          </Box>
        </>
      )}
      <DialogActions sx={{ justifyContent: 'space-between' }}>
        <Button size="small" color="inherit" onClick={() => openHelp('sharing')}>
          How sharing works
        </Button>
        <Box>
          <Button onClick={() => void handleClose()}>Close</Button>
          <Button variant="contained" onClick={() => void handleSave()} disabled={busy}>
            Save visibility
          </Button>
        </Box>
      </DialogActions>
    </Dialog>
  );
}
