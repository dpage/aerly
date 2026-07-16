import { useEffect, useMemo, useState } from 'react';
import { errorMessage } from '../state/helpers';
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
  FormControlLabel,
  IconButton,
  MenuItem,
  Stack,
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableRow,
  TextField,
  Tooltip,
  Typography,
} from '@mui/material';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';

import type { TripMember, TripRole, User } from '../api/types';
import { userInitial, userName } from '../lib/format';
import { useFriendCandidates } from '../state/friendUsers';
import { useStore } from '../state/store';

interface Props {
  open: boolean;
  tripId: number;
  /** The viewer's role on this trip; only owners/editors may manage members. */
  myRole: TripRole;
  members: TripMember[];
  /** User ids who are trip-level passengers (travellers on the whole trip). */
  passengerIds: number[];
  /** The trip's current share-with-all-friends role, if any (owners manage it). */
  shareAllFriendsRole?: 'viewer' | 'editor';
  onClose: () => void;
}

/** The all-friends control's value: 'off' disables it. */
type ShareAllValue = 'off' | 'viewer' | 'editor';

/** A role pickable when sharing. 'passenger' isn't a trip_members role — it adds
 * a trip-level passenger (a traveller on every plan, issue #20) — but it sits in
 * the same picker as the editor/viewer member roles. */
type AddRole = 'editor' | 'viewer' | 'passenger';

/** Trip sharing panel (spec §6.4): add accepted friends as editor, viewer, or
 * passenger, change a member's role, or remove them. The owner role is conferred
 * at trip creation and can't be assigned or revoked here. */
export default function TripMembersDialog({
  open,
  tripId,
  myRole,
  members,
  passengerIds,
  shareAllFriendsRole,
  onClose,
}: Props) {
  const users = useStore((s) => s.users);
  const me = useStore((s) => s.me);
  const addTripMember = useStore((s) => s.addTripMember);
  const removeTripMember = useStore((s) => s.removeTripMember);
  const addTripPassenger = useStore((s) => s.addTripPassenger);
  const removeTripPassenger = useStore((s) => s.removeTripPassenger);
  const setTripShareAllFriends = useStore((s) => s.setTripShareAllFriends);
  const shareTripByEmail = useStore((s) => s.shareTripByEmail);
  const notifyTripShares = useStore((s) => s.notifyTripShares);
  const setError = useStore((s) => s.setError);
  const openHelp = useStore((s) => s.openHelp);
  const friendCandidates = useFriendCandidates();

  const passengerSet = useMemo(() => new Set(passengerIds), [passengerIds]);

  const [pick, setPick] = useState<number | ''>('');
  const [role, setRole] = useState<AddRole>('viewer');
  const [email, setEmail] = useState('');
  const [emailRole, setEmailRole] = useState<'viewer' | 'editor'>('viewer');
  const [busy, setBusy] = useState(false);
  // Local mirror of the all-friends role so the Select reflects a change
  // immediately (the trip prop only updates after the store refetch lands).
  const [shareAll, setShareAll] = useState<ShareAllValue>(shareAllFriendsRole ?? 'off');

  useEffect(() => {
    setShareAll(shareAllFriendsRole ?? 'off');
  }, [shareAllFriendsRole]);

  // People who newly gained access during this dialog session; on close we
  // offer to notify them. Reset whenever the dialog is (re)opened.
  const [addedUserIds, setAddedUserIds] = useState<Set<number>>(() => new Set());
  const [invitedEmails, setInvitedEmails] = useState<Set<string>>(() => new Set());
  const [notify, setNotify] = useState(true);

  useEffect(() => {
    if (open) {
      setAddedUserIds(new Set());
      setInvitedEmails(new Set());
      setNotify(true);
    }
  }, [open]);

  const canManage = myRole === 'owner' || myRole === 'editor';
  // The all-friends and invite-by-email controls map to owner-only endpoints
  // (setTripShareAllFriends / shareTripByEmail both require trip ownership), so
  // gate them on ownership rather than canManage to avoid showing editors a
  // control that would 403.
  const isOwner = myRole === 'owner';

  const userIndex = useMemo(() => {
    const m = new Map<number, User>();
    for (const u of users) m.set(u.id, u);
    return m;
  }, [users]);

  // Friends not already on the trip are eligible to be added; pending friends
  // are offered too (pre-share to someone who hasn't accepted yet).
  const memberIds = useMemo(() => new Set(members.map((m) => m.user_id)), [members]);
  const candidates = useMemo(
    () => friendCandidates.filter((c) => !memberIds.has(c.user.id)),
    [friendCandidates, memberIds],
  );

  const reportError = (err: unknown) => setError(errorMessage(err));

  const memberLabel = (id: number): string => {
    const u = userIndex.get(id);
    return u ? userName(u) : `User #${id}`;
  };

  const handleAdd = async () => {
    if (pick === '') return;
    const addedId = pick;
    setBusy(true);
    try {
      if (role === 'passenger') {
        await addTripPassenger(tripId, addedId);
      } else {
        await addTripMember(tripId, { user_id: addedId, role });
      }
      setAddedUserIds((prev) => new Set(prev).add(addedId));
      setPick('');
      setRole('viewer');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleShareAllChange = async (value: ShareAllValue) => {
    setShareAll(value);
    setBusy(true);
    try {
      await setTripShareAllFriends(tripId, value === 'off' ? null : value);
      // Turning it on newly grants access to every accepted friend who isn't
      // already an explicit member; queue them for the close-time notify.
      if (value !== 'off') {
        setAddedUserIds((prev) => {
          const next = new Set(prev);
          for (const c of friendCandidates) {
            if (!c.pending && !memberIds.has(c.user.id)) next.add(c.user.id);
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
      await shareTripByEmail(tripId, { email: addr, role: emailRole });
      setInvitedEmails((prev) => new Set(prev).add(addr));
      setEmail('');
      setEmailRole('viewer');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleClose = async () => {
    if (notify && addedUserIds.size + invitedEmails.size > 0) {
      try {
        await notifyTripShares(tripId, {
          user_ids: [...addedUserIds],
          emails: [...invitedEmails],
        });
      } catch {
        // Best-effort: never block closing the dialog on a notify failure.
      }
    }
    onClose();
  };

  /** Change a member's effective role from the per-row picker. 'passenger' isn't
   * a trip_members role — it's a trip-level traveller status (issue #20) layered
   * on a viewer membership — so converting to/from it routes through the
   * passenger endpoints instead of the members upsert:
   * - editor/viewer → passenger: add them as a trip passenger (they travel on
   *   every plan; their membership is kept);
   * - passenger → editor/viewer: drop the passenger status first (this also
   *   clears the auto-created viewer membership), then set the plain role;
   * - editor ↔ viewer: a plain members upsert.
   * This lets an owner/editor retag an auto-shared member (e.g. a friend who got
   * viewer access automatically) as a fellow traveller without remove-and-re-add. */
  const changeMemberRole = async (userId: number, currentlyPassenger: boolean, next: AddRole) => {
    try {
      if (next === 'passenger') {
        if (currentlyPassenger) return;
        await addTripPassenger(tripId, userId);
      } else {
        if (currentlyPassenger) {
          await removeTripPassenger(tripId, userId);
        }
        // The members endpoint upserts, so setting a role is a re-add.
        await addTripMember(tripId, { user_id: userId, role: next });
      }
    } catch (err) {
      reportError(err);
    }
  };

  const handleRemove = async (userId: number) => {
    const label = memberLabel(userId);
    if (!window.confirm(`Remove ${label} from this trip?`)) return;
    try {
      // Trip passengers are removed via the passenger endpoint (which also
      // takes them off every plan); plain members via the member endpoint.
      if (passengerSet.has(userId)) {
        await removeTripPassenger(tripId, userId);
      } else {
        await removeTripMember(tripId, userId);
      }
    } catch (err) {
      reportError(err);
    }
  };

  return (
    <Dialog open={open} onClose={() => void handleClose()} maxWidth="sm" fullWidth>
      <DialogTitle>Share trip</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
          {isOwner && (
            <Box>
              <Typography variant="subtitle2" sx={{ mb: 1 }}>
                Share with all friends
              </Typography>
              <Stack direction="row" spacing={1} alignItems="center">
                <TextField
                  select
                  size="small"
                  label="All friends"
                  value={shareAll}
                  onChange={(e) => void handleShareAllChange(e.target.value as ShareAllValue)}
                  disabled={busy}
                  sx={{ minWidth: 160 }}
                >
                  <MenuItem value="off">Off</MenuItem>
                  <MenuItem value="viewer">Viewer</MenuItem>
                  <MenuItem value="editor">Editor</MenuItem>
                </TextField>
                <Typography variant="body2" color="text.secondary">
                  Everyone you&apos;re friends with gets this access automatically.
                </Typography>
              </Stack>
            </Box>
          )}

          {canManage && (
            <Box>
              <Typography variant="subtitle2" sx={{ mb: 1 }}>
                Add a friend to this trip
              </Typography>
              {/* Align to the top of the inputs: the Friend field's helper
                  text ("No friends left to add.") makes it taller, and centring
                  would drag Role + Add down out of line with it. */}
              <Stack direction="row" spacing={1} alignItems="flex-start">
                <TextField
                  select
                  size="small"
                  label="Friend"
                  fullWidth
                  value={pick === '' ? '' : String(pick)}
                  onChange={(e) => setPick(e.target.value === '' ? '' : Number(e.target.value))}
                  disabled={candidates.length === 0}
                  helperText={candidates.length === 0 ? 'No friends left to add.' : undefined}
                  slotProps={{ select: { displayEmpty: true }, inputLabel: { shrink: true } }}
                >
                  <MenuItem value="" disabled>
                    Choose a friend…
                  </MenuItem>
                  {candidates.map((c) => (
                    <MenuItem key={c.user.id} value={String(c.user.id)}>
                      {userName(c.user)}
                      {c.pending ? ' (invited)' : ''}
                    </MenuItem>
                  ))}
                </TextField>
                <TextField
                  select
                  size="small"
                  label="Role"
                  value={role}
                  onChange={(e) => setRole(e.target.value as AddRole)}
                  sx={{ minWidth: 120 }}
                >
                  <MenuItem value="editor">Editor</MenuItem>
                  <MenuItem value="viewer">Viewer</MenuItem>
                  <MenuItem value="passenger">Passenger</MenuItem>
                </TextField>
                <Button
                  variant="contained"
                  onClick={() => void handleAdd()}
                  disabled={busy || pick === ''}
                >
                  Add
                </Button>
              </Stack>
            </Box>
          )}

          {isOwner && (
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
                <TextField
                  select
                  size="small"
                  label="Email role"
                  value={emailRole}
                  onChange={(e) => setEmailRole(e.target.value as 'viewer' | 'editor')}
                  sx={{ minWidth: 130 }}
                >
                  <MenuItem value="editor">Editor</MenuItem>
                  <MenuItem value="viewer">Viewer</MenuItem>
                </TextField>
                <Button
                  variant="outlined"
                  onClick={() => void handleInviteEmail()}
                  disabled={busy || email.trim() === ''}
                >
                  Invite
                </Button>
              </Stack>
            </Box>
          )}

          <Box>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              Members
            </Typography>
            <Table size="small">
              <TableHead>
                <TableRow>
                  <TableCell>Member</TableCell>
                  <TableCell>Role</TableCell>
                  <TableCell align="right" />
                </TableRow>
              </TableHead>
              <TableBody>
                {members.map((m) => {
                  const label = memberLabel(m.user_id);
                  const u = userIndex.get(m.user_id);
                  const isOwner = m.role === 'owner';
                  // A trip passenger travels on every plan; the picker shows
                  // 'passenger' for them (rather than their underlying 'viewer'
                  // membership) and offers switching between editor/viewer/passenger.
                  const isPassenger = passengerSet.has(m.user_id);
                  return (
                    <TableRow key={m.user_id} hover>
                      <TableCell>
                        <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                          <Avatar src={u?.avatar_url} sx={{ width: 24, height: 24 }}>
                            {u ? userInitial(u) : label.charAt(0).toUpperCase()}
                          </Avatar>
                          <span>{label}</span>
                        </Box>
                      </TableCell>
                      <TableCell>
                        {isOwner || !canManage ? (
                          <Chip
                            label={isPassenger ? 'passenger' : m.role}
                            size="small"
                            variant="outlined"
                          />
                        ) : (
                          <TextField
                            select
                            size="small"
                            label={`Role for ${label}`}
                            value={isPassenger ? 'passenger' : m.role}
                            onChange={(e) =>
                              void changeMemberRole(
                                m.user_id,
                                isPassenger,
                                e.target.value as AddRole,
                              )
                            }
                            sx={{ minWidth: 130 }}
                          >
                            <MenuItem value="editor">Editor</MenuItem>
                            <MenuItem value="viewer">Viewer</MenuItem>
                            <MenuItem value="passenger">Passenger</MenuItem>
                          </TextField>
                        )}
                      </TableCell>
                      <TableCell align="right">
                        {/* Managers can remove anyone; a passenger may always
                            remove themselves (matches the API permission). */}
                        {!isOwner && (canManage || (isPassenger && me?.id === m.user_id)) && (
                          <Tooltip title="Remove">
                            <IconButton
                              size="small"
                              aria-label={`Remove ${label}`}
                              onClick={() => void handleRemove(m.user_id)}
                            >
                              <DeleteOutlineIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        )}
                      </TableCell>
                    </TableRow>
                  );
                })}
              </TableBody>
            </Table>
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
        <Button onClick={() => void handleClose()}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
