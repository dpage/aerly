import { useState } from 'react';
import { Alert, Button, Snackbar } from '@mui/material';
import IosShareIcon from '@mui/icons-material/IosShare';

import { useInstallPrompt } from '../pwa';

// In-app "install this app" affordance. On Chromium (Android/desktop) it shows
// an Install button wired to the captured native prompt; on iOS — which has no
// install API — it shows a one-time hint pointing at Safari's Share → "Add to
// Home Screen". Renders nothing when already installed or on an unsupported
// browser. Anchored bottom-right so it doesn't fight the centred update/error
// snackbars.
export default function InstallPrompt() {
  const { canInstall, iosHint, promptInstall } = useInstallPrompt();
  const [dismissed, setDismissed] = useState(false);

  if (dismissed) return null;

  if (canInstall) {
    return (
      <Snackbar open anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}>
        <Alert
          severity="info"
          variant="filled"
          action={
            <>
              <Button
                color="inherit"
                size="small"
                onClick={() => {
                  promptInstall();
                  setDismissed(true);
                }}
              >
                Install
              </Button>
              <Button color="inherit" size="small" onClick={() => setDismissed(true)}>
                Later
              </Button>
            </>
          }
        >
          Install Aerly on your device.
        </Alert>
      </Snackbar>
    );
  }

  if (iosHint) {
    return (
      <Snackbar open anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}>
        <Alert
          severity="info"
          variant="filled"
          icon={<IosShareIcon fontSize="inherit" />}
          onClose={() => setDismissed(true)}
        >
          Install Aerly: tap Share, then “Add to Home Screen”.
        </Alert>
      </Snackbar>
    );
  }

  return null;
}
