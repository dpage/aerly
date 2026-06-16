import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import {
  Box,
  Button,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  IconButton,
  List,
  ListItem,
  ListItemButton,
  ListItemText,
  Tooltip,
  Typography,
} from '@mui/material';
import DeleteOutlineIcon from '@mui/icons-material/DeleteOutline';

import { useStore } from '../state/store';
import { fmtAgo } from '../lib/format';
import type { NotificationItem } from '../api/types';

interface Props {
  open: boolean;
  onClose: () => void;
}

/** The alert history inbox. Replaces the inline alert list that used to live in
 * the account menu: it shows every recent alert (newest first), flags the
 * unread ones, deep-links each to the relevant flight or trip, and lets the
 * user delete items individually or clear the lot — so alerts no longer pile up
 * once the trips they refer to have been seen.
 *
 * Unread items are marked read when the dialog closes (not on open), so the
 * highlight survives long enough to actually read. */
export default function AlertsDialog({ open, onClose }: Props) {
  const alerts = useStore((s) => s.alerts);
  const loadAlerts = useStore((s) => s.loadAlerts);
  const markAlertsRead = useStore((s) => s.markAlertsRead);
  const deleteAlert = useStore((s) => s.deleteAlert);
  const clearAlerts = useStore((s) => s.clearAlerts);
  const unreadAlerts = useStore((s) => s.notifications.unread_alerts);
  const unreadShares = useStore((s) => s.notifications.unread_shares);
  const navigate = useNavigate();

  // Pull fresh history each time the dialog opens; the inbox may have grown via
  // SSE or on another device since it was last loaded.
  useEffect(() => {
    if (!open) return;
    void loadAlerts();
  }, [open, loadAlerts]);

  const hasUnread = unreadAlerts + unreadShares > 0;

  // Closing the dialog counts as "seen" — clear the unread badge then dismiss.
  const handleClose = () => {
    if (hasUnread) void markAlertsRead();
    onClose();
  };

  const openItem = (al: NotificationItem) => {
    // Flight alerts carry plan_part_id — deep-link to the tracker so the user
    // lands on the right flight tile. Other inbox items (shares, reminders)
    // open the trip timeline instead.
    if (al.plan_part_id) {
      navigate(`/tracker?part=${al.plan_part_id}`);
    } else if (al.trip_id) {
      navigate(`/trips/${al.trip_id}`);
    } else {
      return;
    }
    handleClose();
  };

  return (
    <Dialog open={open} onClose={handleClose} maxWidth="xs" fullWidth>
      <DialogTitle>Alerts</DialogTitle>
      <DialogContent dividers sx={{ p: 0 }}>
        {alerts.length === 0 ? (
          <Box sx={{ p: 3, textAlign: 'center' }}>
            <Typography variant="body2" color="text.secondary">
              No alerts. Flight changes and trip shares will show up here.
            </Typography>
          </Box>
        ) : (
          <List disablePadding>
            {alerts.map((al) => {
              const unread = !al.read_at;
              return (
                <ListItem
                  key={`${al.source}-${al.id}`}
                  disablePadding
                  secondaryAction={
                    <Tooltip title="Delete">
                      <IconButton
                        edge="end"
                        size="small"
                        aria-label={`Delete alert: ${al.message}`}
                        onClick={() => void deleteAlert(al)}
                      >
                        <DeleteOutlineIcon fontSize="small" />
                      </IconButton>
                    </Tooltip>
                  }
                >
                  <ListItemButton
                    onClick={() => openItem(al)}
                    disabled={!al.plan_part_id && !al.trip_id}
                  >
                    {/* Unread marker: a small primary dot, mirrored by bolder text. */}
                    <Box
                      sx={{
                        width: 8,
                        height: 8,
                        mr: 1.5,
                        flexShrink: 0,
                        borderRadius: '50%',
                        bgcolor: unread ? 'primary.main' : 'transparent',
                      }}
                    />
                    <ListItemText
                      primary={al.message}
                      secondary={fmtAgo(al.created_at)}
                      slotProps={{
                        primary: {
                          variant: 'body2',
                          fontWeight: unread ? 600 : 400,
                        },
                        secondary: { variant: 'caption' },
                      }}
                    />
                  </ListItemButton>
                </ListItem>
              );
            })}
          </List>
        )}
      </DialogContent>
      <DialogActions>
        <Button color="error" disabled={alerts.length === 0} onClick={() => void clearAlerts()}>
          Clear all
        </Button>
        <Box sx={{ flexGrow: 1 }} />
        <Button onClick={handleClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
