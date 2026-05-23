import { useEffect, useState } from 'react';
import {
  Box,
  Button,
  Divider,
  Paper,
  Stack,
  TextField,
  Typography,
} from '@mui/material';
import GitHubIcon from '@mui/icons-material/GitHub';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';

import { api } from '../api/client';

export default function Login() {
  const [devBypass, setDevBypass] = useState(false);

  useEffect(() => {
    let cancelled = false;
    void api.getDevAuthBypassEnabled().then((enabled) => {
      if (!cancelled) setDevBypass(enabled);
    });
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <Box
      sx={{
        minHeight: '100vh',
        display: 'grid',
        placeItems: 'center',
        bgcolor: 'background.default',
        p: 2,
      }}
    >
      <Paper sx={{ p: 4, maxWidth: 420, width: '100%' }} elevation={3}>
        <Stack spacing={3} alignItems="center" textAlign="center">
          <FlightTakeoffIcon color="primary" sx={{ fontSize: 56 }} />
          <Typography variant="h4">Aerly</Typography>
          <Typography variant="body1" color="text.secondary">
            Track your friends&rsquo; flights to PostgreSQL conferences.
          </Typography>
          <Button
            variant="contained"
            size="large"
            startIcon={<GitHubIcon />}
            href="/auth/github/login"
            sx={{ alignSelf: 'stretch' }}
          >
            Sign in with GitHub
          </Button>
          <Typography variant="caption" color="text.secondary">
            Access is restricted to invited users.
          </Typography>
          {devBypass && (
            <>
              <Divider flexItem>DEV</Divider>
              {/* Plain GET form: the browser navigates to
                  /auth/dev-login?login=<value>, the server sets the session
                  cookie and 302s back to /. */}
              <Stack
                component="form"
                action="/auth/dev-login"
                method="GET"
                spacing={1.5}
                sx={{ alignSelf: 'stretch' }}
              >
                <TextField
                  name="login"
                  label="GitHub login"
                  size="small"
                  required
                  autoComplete="off"
                  inputProps={{ 'aria-label': 'dev login github handle' }}
                />
                <Button type="submit" variant="outlined">
                  Sign in as dev user
                </Button>
                <Typography variant="caption" color="text.secondary">
                  DEV_AUTH_BYPASS is enabled. Do not use in production.
                </Typography>
              </Stack>
            </>
          )}
        </Stack>
      </Paper>
    </Box>
  );
}
