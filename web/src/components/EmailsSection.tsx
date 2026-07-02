import { useEffect, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Box,
  Button,
  Chip,
  IconButton,
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
import DeleteIcon from '@mui/icons-material/DeleteOutline';
import RefreshIcon from '@mui/icons-material/Refresh';
import StarIcon from '@mui/icons-material/Star';
import StarBorderIcon from '@mui/icons-material/StarBorder';

import { api } from '../api/client';
import type { UserEmail } from '../api/types';
import { useStore } from '../state/store';

export default function EmailsSection() {
  const setError = useStore((s) => s.setError);
  const [emails, setEmails] = useState<UserEmail[]>([]);
  const [address, setAddress] = useState('');
  const [busy, setBusy] = useState(false);

  const reportError = (err: unknown) => setError(errorMessage(err));

  useEffect(() => {
    // Clear last session's rows so a reopen doesn't flash stale data, and guard
    // against a late response landing after the component has unmounted.
    setEmails([]);
    let cancelled = false;
    void api
      .listMyEmails()
      .then((rows) => {
        if (!cancelled) setEmails(rows);
      })
      .catch((err) => {
        if (!cancelled) reportError(err);
      });
    return () => {
      cancelled = true;
    };
    // reportError closes over setError, which is stable; intentional dep list.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const handleAdd = async () => {
    const trimmed = address.trim();
    if (!trimmed) return;
    setBusy(true);
    try {
      const created = await api.addMyEmail(trimmed);
      setEmails((rows) => [created, ...rows]);
      setAddress('');
    } catch (err) {
      reportError(err);
    } finally {
      setBusy(false);
    }
  };

  const handleDelete = async (row: UserEmail) => {
    if (!window.confirm(`Delete ${row.address}?`)) return;
    try {
      await api.deleteMyEmail(row.id);
      setEmails((rows) => rows.filter((r) => r.id !== row.id));
    } catch (err) {
      reportError(err);
    }
  };

  const handleResend = async (row: UserEmail) => {
    try {
      const updated = await api.resendMyEmail(row.id);
      setEmails((rows) => rows.map((r) => (r.id === row.id ? updated : r)));
    } catch (err) {
      reportError(err);
    }
  };

  const handleSetPrimary = async (row: UserEmail) => {
    try {
      // The endpoint returns the whole list so the address that lost primary
      // updates alongside the one that gained it.
      const rows = await api.setPrimaryMyEmail(row.id);
      setEmails(rows);
    } catch (err) {
      reportError(err);
    }
  };

  return (
    <Stack spacing={3}>
      <Box>
        <Stack direction="row" spacing={1} alignItems="center">
          <TextField
            label="Email address"
            size="small"
            fullWidth
            value={address}
            onChange={(e) => setAddress(e.target.value)}
          />
          <Button
            variant="contained"
            onClick={() => void handleAdd()}
            disabled={busy || address.trim() === ''}
          >
            Add
          </Button>
        </Stack>
        <Typography
          variant="caption"
          color="text.secondary"
          sx={{ display: 'block', mt: 0.5, ml: 1.75 }}
        >
          We'll send a verification link to confirm you own this address.
        </Typography>
      </Box>

      {emails.length === 0 ? (
        <Typography variant="body2" color="text.secondary">
          No addresses registered yet.
        </Typography>
      ) : (
        <Box>
          <Typography variant="caption" color="text.secondary" sx={{ display: 'block', mb: 1 }}>
            Your primary address (starred) is where we send share and friend notifications. Pick any
            verified address to change it.
          </Typography>
          <Table size="small">
            <TableHead>
              <TableRow>
                <TableCell>Address</TableCell>
                <TableCell align="center">Status</TableCell>
                <TableCell align="right" />
              </TableRow>
            </TableHead>
            <TableBody>
              {emails.map((row) => (
                <TableRow key={row.id} hover>
                  <TableCell>{row.address}</TableCell>
                  <TableCell align="center">
                    {row.verified ? (
                      <Chip label="verified" size="small" color="success" variant="outlined" />
                    ) : (
                      <Chip label="pending" size="small" color="warning" variant="outlined" />
                    )}
                  </TableCell>
                  <TableCell align="right">
                    <Box sx={{ display: 'inline-flex', gap: 0.5 }}>
                      {!row.verified && (
                        <Tooltip title="Resend verification">
                          <IconButton
                            size="small"
                            aria-label={`Resend ${row.address}`}
                            onClick={() => void handleResend(row)}
                          >
                            <RefreshIcon fontSize="small" />
                          </IconButton>
                        </Tooltip>
                      )}
                      {row.verified &&
                        (row.is_primary ? (
                          <Tooltip title="Primary address for notifications">
                            <span>
                              <IconButton
                                size="small"
                                disabled
                                aria-label={`${row.address} is your primary address`}
                              >
                                <StarIcon fontSize="small" color="primary" />
                              </IconButton>
                            </span>
                          </Tooltip>
                        ) : (
                          <Tooltip title="Set as primary">
                            <IconButton
                              size="small"
                              aria-label={`Set ${row.address} as primary`}
                              onClick={() => void handleSetPrimary(row)}
                            >
                              <StarBorderIcon fontSize="small" />
                            </IconButton>
                          </Tooltip>
                        ))}
                      <Tooltip title="Delete">
                        <IconButton
                          size="small"
                          aria-label={`Delete ${row.address}`}
                          onClick={() => void handleDelete(row)}
                        >
                          <DeleteIcon fontSize="small" />
                        </IconButton>
                      </Tooltip>
                    </Box>
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        </Box>
      )}
    </Stack>
  );
}
