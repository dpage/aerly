import { useEffect, useRef, useState } from 'react';
import { errorMessage } from '../state/helpers';
import {
  Alert,
  Box,
  Button,
  Chip,
  CircularProgress,
  Dialog,
  DialogActions,
  DialogContent,
  DialogTitle,
  Divider,
  Link,
  Stack,
  Tooltip,
  Typography,
} from '@mui/material';
import ContentCopyIcon from '@mui/icons-material/ContentCopy';
import IconButton from '@mui/material/IconButton';

import { api } from '../api/client';
import type { AdminInfo } from '../api/types';

interface Props {
  open: boolean;
  onClose: () => void;
}

/** Superuser-only "About Aerly" panel. Shows the running build (commit hash,
 * build time, dirty flag) plus runtime and configuration diagnostics so an
 * operator can confirm exactly what's deployed. Fetched on open from the
 * superuser-gated GET /api/admin/info. */
export default function AboutDialog({ open, onClose }: Props) {
  const [info, setInfo] = useState<AdminInfo | null>(null);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    if (!open) {
      setInfo(null);
      setError(null);
      return;
    }
    let cancelled = false;
    setLoading(true);
    setError(null);
    api
      .getAdminInfo()
      .then((data) => {
        if (!cancelled) setInfo(data);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(errorMessage(err));
      })
      .finally(() => {
        if (!cancelled) setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  return (
    <Dialog open={open} onClose={onClose} maxWidth="sm" fullWidth>
      <DialogTitle>About Aerly</DialogTitle>
      <DialogContent dividers>
        {loading && (
          <Box sx={{ display: 'grid', placeItems: 'center', minHeight: 160 }}>
            <CircularProgress />
          </Box>
        )}
        {error && (
          <Alert severity="error" role="alert">
            {error}
          </Alert>
        )}
        {info && !loading && !error && <Sections info={info} />}
      </DialogContent>
      <DialogActions>
        <Button onClick={onClose}>Close</Button>
      </DialogActions>
    </Dialog>
  );
}

function Sections({ info }: { info: AdminInfo }) {
  const { version: v, runtime: rt, config: c } = info;
  return (
    <Stack spacing={2.5}>
      <Section title="Build">
        <Row label="Commit">
          {v.commit ? (
            <Stack direction="row" spacing={0.5} alignItems="center">
              <Box component="code" sx={{ fontFamily: 'monospace' }}>
                {v.short}
              </Box>
              {v.modified && (
                <Tooltip title="Built from a modified working tree">
                  <Chip label="dirty" size="small" color="warning" variant="outlined" />
                </Tooltip>
              )}
              <CopyButton value={v.commit} label="commit hash" />
            </Stack>
          ) : (
            'unknown'
          )}
        </Row>
        <Row label="Built">{formatDateTime(v.build_time)}</Row>
        <Row label="Go">{v.go_version}</Row>
        <Row label="Platform">{`${v.os}/${v.arch}`}</Row>
      </Section>

      <Divider />

      <Section title="Runtime">
        <Row label="Started">{formatDateTime(rt.started_at)}</Row>
        <Row label="Uptime">{formatUptime(rt.uptime_sec)}</Row>
        <Row label="Goroutines">{String(rt.goroutines)}</Row>
        <Row label="CPUs">{String(rt.num_cpu)}</Row>
      </Section>

      <Divider />

      <Section title="Configuration">
        <Row label="Public URL">
          <Link href={c.public_url} target="_blank" rel="noopener noreferrer">
            {c.public_url}
          </Link>
        </Row>
        <Row label="Tracker">{c.tracker === 'opensky' ? trackerAuthLabel(c) : 'stub'}</Row>
        <Row label="Flight data">{c.resolver_available ? 'AeroDataBox' : 'none'}</Row>
        <Row label="Poll interval">{`${c.poll_interval_sec}s`}</Row>
        <Row label="LLM">{c.llm_configured ? `${c.llm_provider} / ${c.llm_model}` : 'none'}</Row>
        <Row label="Email ingest">
          {c.email_ingest_enabled ? (c.email_ingest_address ?? 'enabled') : 'disabled'}
        </Row>
        <Row label="Outbound mail">{c.mail_configured ? 'configured' : 'disabled'}</Row>
        <Row label="Sign-in">{signInLabel(c)}</Row>
        {c.dev_auth_bypass && (
          <Row label="Dev auth bypass">
            <Chip label="ON" size="small" color="warning" variant="outlined" />
          </Row>
        )}
      </Section>
    </Stack>
  );
}

function trackerAuthLabel(c: AdminInfo['config']): string {
  return c.tracker_authed ? 'OpenSky (authenticated)' : 'OpenSky (anonymous)';
}

function signInLabel(c: AdminInfo['config']): string {
  const providers: string[] = [];
  if (c.auth_github) providers.push('GitHub');
  if (c.auth_google) providers.push('Google');
  return providers.length > 0 ? providers.join(', ') : 'none';
}

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Box>
      <Typography variant="overline" color="text.secondary" sx={{ display: 'block', mb: 0.5 }}>
        {title}
      </Typography>
      <Stack spacing={0.75}>{children}</Stack>
    </Box>
  );
}

function Row({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <Box sx={{ display: 'flex', justifyContent: 'space-between', alignItems: 'center', gap: 2 }}>
      <Typography variant="body2" color="text.secondary">
        {label}
      </Typography>
      <Box sx={{ textAlign: 'right', minWidth: 0, overflowWrap: 'anywhere' }}>
        {typeof children === 'string' ? (
          <Typography variant="body2">{children}</Typography>
        ) : (
          children
        )}
      </Box>
    </Box>
  );
}

function CopyButton({ value, label }: { value: string; label: string }) {
  const [copied, setCopied] = useState(false);
  const timer = useRef<ReturnType<typeof setTimeout>>();
  useEffect(() => () => clearTimeout(timer.current), []);
  const onCopy = () => {
    void navigator.clipboard?.writeText(value).then(
      () => {
        setCopied(true);
        clearTimeout(timer.current);
        timer.current = setTimeout(() => setCopied(false), 1500);
      },
      () => {
        // Clipboard unavailable (insecure context / denied) — ignore.
      },
    );
  };
  return (
    <Tooltip title={copied ? 'Copied' : `Copy ${label}`}>
      <IconButton size="small" aria-label={`Copy ${label}`} onClick={onCopy}>
        <ContentCopyIcon fontSize="inherit" />
      </IconButton>
    </Tooltip>
  );
}

function formatDateTime(iso: string): string {
  if (!iso) return 'unknown';
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return iso;
  return d.toLocaleString();
}

function formatUptime(seconds: number): string {
  if (seconds <= 0) return '0s';
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = seconds % 60;
  const parts: string[] = [];
  if (d > 0) parts.push(`${d}d`);
  if (h > 0) parts.push(`${h}h`);
  if (m > 0) parts.push(`${m}m`);
  if (parts.length === 0) parts.push(`${s}s`);
  return parts.join(' ');
}
