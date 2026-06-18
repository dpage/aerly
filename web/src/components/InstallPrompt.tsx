import { useState } from 'react';
import { Alert, Button, Snackbar } from '@mui/material';
import IosShareIcon from '@mui/icons-material/IosShare';

import { useInstallPrompt } from '../pwa';

// Remember a dismissal across visits: neither the iOS hint nor the Chromium
// prompt has a reliable "user added the app" signal in-page, so a manual
// dismissal is our only cue to stop showing it. Persisted (not session) so it
// survives reloads and revisits.
const DISMISSED_KEY = 'aerly.install_prompt_dismissed';

function loadDismissed(): boolean {
  try {
    return window.localStorage.getItem(DISMISSED_KEY) === '1';
  } catch {
    // SSR / privacy modes that throw on localStorage access — treat as not dismissed.
    return false;
  }
}

function persistDismissed(): void {
  try {
    window.localStorage.setItem(DISMISSED_KEY, '1');
  } catch {
    // ignore — best effort
  }
}

// In-app "install this app" affordance. On Chromium (Android/desktop) it shows
// an Install button wired to the captured native prompt; on iOS — which has no
// install API — it shows a one-time hint pointing at Safari's Share → "Add to
// Home Screen". Renders nothing when already installed, on an unsupported
// browser, or once the user has dismissed it before. Anchored bottom-right so
// it doesn't fight the centred update/error snackbars.
export default function InstallPrompt() {
  const { canInstall, iosHint, promptInstall } = useInstallPrompt();
  const [dismissed, setDismissed] = useState(loadDismissed);

  // Record the dismissal so the prompt doesn't reappear on every visit.
  const dismiss = () => {
    persistDismissed();
    setDismissed(true);
  };

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
                  dismiss();
                }}
              >
                Install
              </Button>
              <Button color="inherit" size="small" onClick={dismiss}>
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
          onClose={dismiss}
        >
          Install Aerly: tap Share, then “Add to Home Screen”.
        </Alert>
      </Snackbar>
    );
  }

  return null;
}
