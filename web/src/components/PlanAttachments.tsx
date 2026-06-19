import { useEffect, useRef, useState } from 'react';
import { Box, Button, Divider, Link, Stack, Typography } from '@mui/material';

import type { Attachment } from '../api/types';
import { api } from '../api/client';
import { useStore } from '../state/store';
import { errorMessage } from '../state/helpers';
import { formatBytes } from '../lib/format';

interface Props {
  planId: number;
  /** The plan's current attachments; the component keeps a local copy so an
   * upload/remove reflects immediately without waiting for an SSE refresh. */
  attachments: Attachment[];
  /** Read-only (offline): download stays available, upload/remove are hidden. */
  readOnly: boolean;
}

/** Attachments section of the plan editor (issue #91). Renders nothing unless
 * the server has an attachment store configured (capabilities.attachments_enabled),
 * so the affordance is invisible when the feature is off. */
export default function PlanAttachments({ planId, attachments, readOnly }: Props) {
  const enabled = useStore((s) => s.capabilities.attachments_enabled);
  const maxBytes = useStore((s) => s.capabilities.attachments_max_bytes);
  const setError = useStore((s) => s.setError);
  const setNotice = useStore((s) => s.setNotice);

  const [items, setItems] = useState<Attachment[]>(attachments);
  const [busy, setBusy] = useState(false);
  const inputRef = useRef<HTMLInputElement>(null);

  // Re-sync when the dialog switches to a different plan.
  useEffect(() => {
    setItems(attachments);
    // eslint-disable-next-line react-hooks/exhaustive-deps -- sync only on plan switch
  }, [planId]);

  if (!enabled) return null;

  const upload = async (file: File | undefined) => {
    if (!file) return;
    if (maxBytes && file.size > maxBytes) {
      setError(`That file is too large — the limit is ${formatBytes(maxBytes)}.`);
      return;
    }
    setBusy(true);
    try {
      const att = await api.uploadPlanAttachment(planId, file);
      setItems((prev) => [att, ...prev]);
      setNotice({ message: `Attached ${att.filename}.`, severity: 'success' });
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
      // Clear the input so re-choosing the same file fires onChange again.
      if (inputRef.current) inputRef.current.value = '';
    }
  };

  const remove = async (att: Attachment) => {
    setBusy(true);
    try {
      await api.deleteAttachment(att.id);
      setItems((prev) => prev.filter((a) => a.id !== att.id));
    } catch (err) {
      setError(errorMessage(err));
    } finally {
      setBusy(false);
    }
  };

  const download = async (att: Attachment) => {
    try {
      await api.downloadAttachment(att);
    } catch (err) {
      setError(errorMessage(err));
    }
  };

  return (
    <Box>
      <Divider sx={{ mb: 1.5 }}>
        <Typography variant="caption" color="text.secondary">
          Attachments
        </Typography>
      </Divider>
      <Stack spacing={1}>
        {items.length === 0 && (
          <Typography variant="body2" color="text.secondary">
            No attachments yet.
          </Typography>
        )}
        {items.map((att) => (
          <Stack key={att.id} direction="row" alignItems="center" spacing={1}>
            <Link
              component="button"
              type="button"
              underline="hover"
              onClick={() => void download(att)}
              sx={{ textAlign: 'left', flexGrow: 1, wordBreak: 'break-all' }}
            >
              {att.filename}
            </Link>
            <Typography variant="caption" color="text.secondary" sx={{ whiteSpace: 'nowrap' }}>
              {formatBytes(att.size_bytes)}
            </Typography>
            {!readOnly && (
              <Button
                size="small"
                color="inherit"
                disabled={busy}
                onClick={() => void remove(att)}
              >
                Remove
              </Button>
            )}
          </Stack>
        ))}
        {!readOnly && (
          <Box>
            <Button variant="outlined" size="small" component="label" disabled={busy}>
              Add file
              <input
                ref={inputRef}
                type="file"
                hidden
                onChange={(e) => void upload(e.target.files?.[0])}
              />
            </Button>
          </Box>
        )}
      </Stack>
    </Box>
  );
}
