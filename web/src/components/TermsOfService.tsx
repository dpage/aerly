import type { ReactNode } from 'react';
import { Box, Divider, Link, Paper, Stack, Typography } from '@mui/material';
import FlightTakeoffIcon from '@mui/icons-material/FlightTakeoff';

export default function TermsOfService() {
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
      <Paper sx={{ p: 4, maxWidth: 640, width: '100%' }} elevation={3}>
        <Stack spacing={3}>
          <Stack spacing={1} alignItems="center" textAlign="center">
            <FlightTakeoffIcon color="primary" sx={{ fontSize: 56 }} />
            <Typography variant="h4" component="h1">Terms of Service</Typography>
            <Typography variant="body2" color="text.secondary">
              Aerly
            </Typography>
          </Stack>

          <Divider />

          <TermsSection title="Acceptance">
            By accessing or using Aerly you agree to be bound by these terms. If you do not agree,
            do not use the service.
          </TermsSection>

          <TermsSection title="Service availability">
            Aerly is provided on a best-effort basis. We make no guarantee of uptime, availability,
            or continuity of service. The service may be modified, suspended, or discontinued at any
            time and without notice.
          </TermsSection>

          <TermsSection title="No warranty">
            The service is provided "as is" and "as available" without warranty of any kind, express
            or implied, including but not limited to warranties of merchantability, fitness for a
            particular purpose, or non-infringement.
          </TermsSection>

          <TermsSection title="Limitation of liability">
            To the fullest extent permitted by law, Aerly and its operator shall not be liable for
            any direct, indirect, incidental, special, consequential, or punitive damages arising
            from your use of, or inability to use, the service.
          </TermsSection>

          <TermsSection title="Use at your own risk">
            You are solely responsible for any decisions made based on information displayed by this
            service. Flight data may be inaccurate, delayed, or missing.
          </TermsSection>

          <TermsSection title="Changes to these terms">
            These terms may be updated at any time. Continued use of the service after changes are
            posted constitutes acceptance of the revised terms.
          </TermsSection>

          <Divider />

          <Stack direction="row" spacing={1} justifyContent="center">
            <Link href="/" variant="caption" color="text.secondary">
              Back to Aerly
            </Link>
            <Typography variant="caption" color="text.secondary">
              ·
            </Typography>
            <Link href="/privacy" variant="caption" color="text.secondary">
              Privacy Policy
            </Link>
          </Stack>
        </Stack>
      </Paper>
    </Box>
  );
}

function TermsSection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Stack spacing={0.5}>
      <Typography variant="subtitle1" fontWeight={600} component="h2">
        {title}
      </Typography>
      <Typography variant="body2">{children}</Typography>
    </Stack>
  );
}
