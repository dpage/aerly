import { useEffect, useState } from 'react';
import { Box, Button, Dialog, DialogActions, DialogContent, DialogTitle, Tab, Tabs } from '@mui/material';

import { useStore } from '../state/store';
import AlertPrefsSection from './AlertPrefsSection';
import AutoShareSection from './AutoShareSection';
import HomeAddressSection from './HomeAddressSection';
import EmailsSection from './EmailsSection';

interface Props {
  open: boolean;
  onClose: () => void;
}

/** Unifies the per-user settings — alert preferences, sharing defaults, home
 * address, and (when email ingest is enabled) email addresses — into one tabbed
 * dialog. Every section auto-saves, so the only footer action is Close. Only the
 * active tab's section is mounted, so each section loads its data when first
 * shown. */
export default function PreferencesDialog({ open, onClose }: Props) {
  const emailEnabled = useStore((s) => s.capabilities.email_ingest_enabled);
  const [tab, setTab] = useState(0);

  // The dialog stays mounted (only `open` toggles), so reset to the first tab
  // each time it opens rather than reopening on whichever tab was last viewed.
  useEffect(() => {
    if (open) setTab(0);
  }, [open]);

  // Built dynamically so the gated Emails tab doesn't leave a hole in the index
  // space when ingest is disabled.
  const tabs = [
    { label: 'Alerts', render: () => <AlertPrefsSection /> },
    { label: 'Sharing', render: () => <AutoShareSection /> },
    { label: 'Home', render: () => <HomeAddressSection /> },
    ...(emailEnabled ? [{ label: 'Emails', render: () => <EmailsSection /> }] : []),
  ];
  const current = Math.min(tab, tabs.length - 1);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>Preferences</DialogTitle>
      <DialogContent dividers>
        <Box sx={{ borderBottom: 1, borderColor: 'divider', mb: 2 }}>
          <Tabs value={current} onChange={(_, v) => setTab(v as number)} variant="fullWidth">
            {tabs.map((t) => (
              <Tab key={t.label} label={t.label} />
            ))}
          </Tabs>
        </Box>
        {tabs[current].render()}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}
