import type { ReactNode } from 'react';
import { Box, Divider, Link, Paper, Stack, Typography } from '@mui/material';

import AerlyLogo from './AerlyLogo';

export default function PrivacyPolicy() {
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
            <AerlyLogo size={56} />
            <Typography variant="h4" component="h1">
              Privacy Policy
            </Typography>
            <Typography variant="body2" color="text.secondary">
              Aerly
            </Typography>
          </Stack>

          <Divider />

          <PolicySection title="What we collect">
            When you sign in via GitHub or Google we receive your name and email address from that
            provider. If you sign in via email link we store your email address. We also store
            flight records that you or other users create, including any flights on which you are
            listed as a passenger.
          </PolicySection>

          <PolicySection title="What we don't do">
            We do not use analytics tools, tracking pixels, or advertising networks. We do not sell
            or share your data with third parties for commercial purposes.
          </PolicySection>

          <PolicySection title="Who can see your data">
            By default a flight is visible only to the person who created it, users listed as
            passengers, users explicitly added to its share list, and service administrators. A
            creator may mark a flight "public", which makes it visible to all authenticated users of
            the service. Service administrators can see all data regardless of visibility settings.
          </PolicySection>

          <PolicySection title="Data sharing">
            Your data is not shared with third parties. We will disclose data only when required to
            do so by law, such as in response to a court order or other legal obligation.
          </PolicySection>

          <PolicySection title="Data security">
            We take reasonable steps to protect your data, but we cannot guarantee its security. Use
            this service at your own risk.
          </PolicySection>

          <PolicySection title="Contact">
            If you have questions about this policy please open an issue on our{' '}
            <Link href="https://github.com/dpage/aerly" target="_blank" rel="noopener noreferrer">
              GitHub repository
            </Link>
            .
          </PolicySection>

          <Divider />

          <Stack direction="row" spacing={1} justifyContent="center">
            <Link href="/" variant="caption" color="text.secondary">
              Back to Aerly
            </Link>
            <Typography variant="caption" color="text.secondary">
              ·
            </Typography>
            <Link href="/terms" variant="caption" color="text.secondary">
              Terms of Service
            </Link>
          </Stack>
        </Stack>
      </Paper>
    </Box>
  );
}

function PolicySection({ title, children }: { title: string; children: ReactNode }) {
  return (
    <Stack spacing={0.5}>
      <Typography variant="subtitle1" fontWeight={600} component="h2">
        {title}
      </Typography>
      <Typography variant="body2">{children}</Typography>
    </Stack>
  );
}
