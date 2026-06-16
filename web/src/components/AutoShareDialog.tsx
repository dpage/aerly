import { useMemo, useState } from 'react';
import {
  Avatar,
  Box,
  Button,
  Chip,
  Dialog,
  DialogActions,
  DialogContent,
  DialogContentText,
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

import type { AutoShareRole, User } from '../api/types';
import { userInitial, userName } from '../lib/format';
import { errorMessage } from '../state/helpers';
import { useFriendUsers } from '../state/friendUsers';
import { useStore } from '../state/store';

const ROLE_LABEL: Record<AutoShareRole, string> = {
  viewer: 'Viewer',
  editor: 'Editor',
  passenger: 'Passenger',
};

/** Manage the signed-in user's "always share with" defaults: a list of friends
 * who are automatically added to every new trip the user creates, each at a
 * chosen role. Mirrors the per-trip share roles (viewer/editor/passenger).
 * Changing the list only affects future trips — trips already shared keep their
 * access. */
export default function AutoShareDialog({
  open,
  onClose,
}: {
  open: boolean;
  onClose: () => void;
}) {
  const autoShares = useStore((s) => s.autoShares);
  const users = useStore((s) => s.users);
  const setAutoShare = useStore((s) => s.setAutoShare);
  const removeAutoShare = useStore((s) => s.removeAutoShare);
  const setError = useStore((s) => s.setError);
  const friends = useFriendUsers();

  const [pick, setPick] = useState<number | ''>('');
  const [role, setRole] = useState<AutoShareRole>('viewer');
  const [busy, setBusy] = useState(false);

  const userIndex = useMemo(() => {
    const m = new Map<number, User>();
    for (const u of users) m.set(u.id, u);
    return m;
  }, [users]);

  // Friends not already in the list are eligible to be added.
  const sharedIds = useMemo(() => new Set(autoShares.map((a) => a.user_id)), [autoShares]);
  const candidates = useMemo(
    () => friends.filter((u) => !sharedIds.has(u.id)),
    [friends, sharedIds],
  );

  const reportError = (err: unknown) => setError(errorMessage(err));
  const label = (id: number): string => {
    const u = userIndex.get(id);
    return u ? userName(u) : `User #${id}`;
  };

  const handleAdd = async () => {
    if (pick === '') return;
    setBusy(true);
    try {
      await setAutoShare(pick, role);
      setPick('');
      setRole('viewer');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleRole = async (userId: number, next: AutoShareRole) => {
    try {
      await setAutoShare(userId, next);
    } catch (err) {
      reportError(err);
    }
  };

  const handleRemove = async (userId: number) => {
    try {
      await removeAutoShare(userId);
    } catch (err) {
      reportError(err);
    }
  };

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Always share with</DialogTitle>
      <DialogContent dividers>
        <DialogContentText sx={{ mb: 2 }}>
          People here are automatically added to every new trip you create — handy if you always
          want your partner to see where you are, or your assistant to manage your trips. This only
          applies to trips you create from now on; trips you&apos;ve already shared are left as they
          are.
        </DialogContentText>
        <Stack spacing={3}>
          <Box>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              Add a friend
            </Typography>
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
                onChange={(e) => setRole(e.target.value as AutoShareRole)}
                sx={{ minWidth: 130 }}
              >
                <MenuItem value="editor">Editor</MenuItem>
                <MenuItem value="viewer">Viewer</MenuItem>
                <MenuItem value="passenger">Passenger</MenuItem>
              </TextField>
              <Button variant="contained" onClick={() => void handleAdd()} disabled={busy || pick === ''}>
                Add
              </Button>
            </Stack>
          </Box>

          <Box>
            <Typography variant="subtitle2" sx={{ mb: 1 }}>
              Shared automatically with
            </Typography>
            {autoShares.length === 0 ? (
              <Typography variant="body2" color="text.secondary">
                No one yet. Add a friend above to start sharing new trips with them automatically.
              </Typography>
            ) : (
              <Table size="small">
                <TableHead>
                  <TableRow>
                    <TableCell>Friend</TableCell>
                    <TableCell>Role</TableCell>
                    <TableCell align="right" />
                  </TableRow>
                </TableHead>
                <TableBody>
                  {autoShares.map((a) => {
                    const u = userIndex.get(a.user_id);
                    const name = label(a.user_id);
                    return (
                      <TableRow key={a.user_id} hover>
                        <TableCell>
                          <Box sx={{ display: 'flex', alignItems: 'center', gap: 1 }}>
                            <Avatar src={u?.avatar_url} sx={{ width: 24, height: 24 }}>
                              {u ? userInitial(u) : name.charAt(0).toUpperCase()}
                            </Avatar>
                            <span>{name}</span>
                          </Box>
                        </TableCell>
                        <TableCell>
                          {u ? (
                            <TextField
                              select
                              size="small"
                              label={`Role for ${name}`}
                              value={a.role}
                              onChange={(e) => void handleRole(a.user_id, e.target.value as AutoShareRole)}
                              sx={{ minWidth: 140 }}
                            >
                              <MenuItem value="editor">Editor</MenuItem>
                              <MenuItem value="viewer">Viewer</MenuItem>
                              <MenuItem value="passenger">Passenger</MenuItem>
                            </TextField>
                          ) : (
                            <Chip label={ROLE_LABEL[a.role]} size="small" variant="outlined" />
                          )}
                        </TableCell>
                        <TableCell align="right">
                          <Tooltip title="Remove">
                            <IconButton
                              size="small"
                              aria-label={`Remove ${name}`}
                              onClick={() => void handleRemove(a.user_id)}
                            >
                              <DeleteOutlineIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        </TableCell>
                      </TableRow>
                    );
                  })}
                </TableBody>
              </Table>
            )}
          </Box>
        </Stack>
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
