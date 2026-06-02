import { useMemo, useState } from 'react';
import {
  Avatar,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
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
import { useFriendUsers } from '../state/friendUsers';
import { useStore } from '../state/store';

interface Props {
  open: boolean;
  tripId: number;
  /** The viewer's role on this trip; only owners/editors may manage members. */
  myRole: TripRole;
  members: TripMember[];
  onClose: () => void;
}

/** Trip sharing panel (spec §6.4): add accepted friends as editor or viewer,
 * change a member's role, or remove them. The owner role is conferred at trip
 * creation and can't be assigned or revoked here. */
export default function TripMembersDialog({ open, tripId, myRole, members, onClose }: Props) {
  const users = useStore((s) => s.users);
  const addTripMember = useStore((s) => s.addTripMember);
  const removeTripMember = useStore((s) => s.removeTripMember);
  const setError = useStore((s) => s.setError);
  const openHelp = useStore((s) => s.openHelp);
  const friends = useFriendUsers();

  const [pick, setPick] = useState<number | ''>('');
  const [role, setRole] = useState<Exclude<TripRole, 'owner'>>('viewer');
  const [busy, setBusy] = useState(false);

  const canManage = myRole === 'owner' || myRole === 'editor';

  const userIndex = useMemo(() => {
    const m = new Map<number, User>();
    for (const u of users) m.set(u.id, u);
    return m;
  }, [users]);

  // Friends not already on the trip are eligible to be added.
  const memberIds = useMemo(() => new Set(members.map((m) => m.user_id)), [members]);
  const candidates = useMemo(
    () => friends.filter((f) => !memberIds.has(f.id)),
    [friends, memberIds],
  );

  const reportError = (err: unknown) =>
    setError(err instanceof Error ? err.message : String(err));

  const memberLabel = (id: number): string => {
    const u = userIndex.get(id);
    return u ? userName(u) : `User #${id}`;
  };

  const handleAdd = async () => {
    if (pick === '') return;
    setBusy(true);
    try {
      await addTripMember(tripId, { user_id: pick, role });
      setPick('');
      setRole('viewer');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleRole = async (userId: number, next: TripRole) => {
    try {
      // The members endpoint upserts, so changing a role is a re-add.
      await addTripMember(tripId, { user_id: userId, role: next });
    } catch (err) {
      reportError(err);
    }
  };

  const handleRemove = async (userId: number) => {
    const label = memberLabel(userId);
    if (!window.confirm(`Remove ${label} from this trip?`)) return;
    try {
      await removeTripMember(tripId, userId);
    } catch (err) {
      reportError(err);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Share trip</DialogTitle>
      <DialogContent dividers>
        <Stack spacing={3}>
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
                  {candidates.map((u) => (
                    <MenuItem key={u.id} value={String(u.id)}>
                      {userName(u)}
                    </MenuItem>
                  ))}
                </TextField>
                <TextField
                  select
                  size="small"
                  label="Role"
                  value={role}
                  onChange={(e) => setRole(e.target.value as Exclude<TripRole, 'owner'>)}
                  sx={{ minWidth: 120 }}
                >
                  <MenuItem value="editor">Editor</MenuItem>
                  <MenuItem value="viewer">Viewer</MenuItem>
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
                          <Chip label={m.role} size="small" variant="outlined" />
                        ) : (
                          <TextField
                            select
                            size="small"
                            label={`Role for ${label}`}
                            value={m.role}
                            onChange={(e) => void handleRole(m.user_id, e.target.value as TripRole)}
                            sx={{ minWidth: 130 }}
                          >
                            <MenuItem value="editor">Editor</MenuItem>
                            <MenuItem value="viewer">Viewer</MenuItem>
                          </TextField>
                        )}
                      </TableCell>
                      <TableCell align="right">
                        {!isOwner && canManage && (
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
      <DialogActions sx={{ justifyContent: 'space-between' }}>
        <Button size="small" color="inherit" onClick={() => openHelp('sharing')}>
          How sharing works
        </Button>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
