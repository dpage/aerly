import { useState } from 'react';
import {
  Box,
  Dialog,
  DialogContent,
  DialogTitle,
  IconButton,
  Link,
  Stack,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import CloseIcon from '@mui/icons-material/Close';
import LinkIcon from '@mui/icons-material/Link';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';

import type { PlanLink } from '../api/types';
import { isInlineImage } from '../lib/plan-links';

/** A plan's links on its expanded timeline tile (issue #109). A link to an
 * image opens in a dialog so a ticket or boarding pass can be shown at the
 * gate without leaving the app; anything else opens in a new tab. */
export default function PlanLinks({ links }: { links: PlanLink[] }) {
  const [viewing, setViewing] = useState<PlanLink | null>(null);
  if (links.length === 0) return null;
  return (
    <Box sx={{ mt: 1 }}>
      {links.map((l) => (
        <Typography key={l.id} variant="body2" sx={{ display: 'block' }}>
          {isInlineImage(l.url) ? (
            <Link
              component="button"
              type="button"
              underline="hover"
              onClick={() => setViewing(l)}
              sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5, textAlign: 'left' }}
            >
              <LinkIcon sx={{ fontSize: 16 }} />
              {l.title}
            </Link>
          ) : (
            <Link
              href={l.url}
              target="_blank"
              rel="noopener noreferrer"
              underline="hover"
              sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}
            >
              <LinkIcon sx={{ fontSize: 16 }} />
              {l.title}
              <OpenInNewIcon sx={{ fontSize: 12 }} />
            </Link>
          )}
        </Typography>
      ))}
      {viewing && <ImageViewer link={viewing} onClose={() => setViewing(null)} />}
    </Box>
  );
}

function ImageViewer({ link, onClose }: { link: PlanLink; onClose: () => void }) {
  const theme = useTheme();
  const fullScreen = useMediaQuery(theme.breakpoints.down('sm'));
  const [failed, setFailed] = useState(false);
  return (
    <Dialog open onClose={onClose} fullScreen={fullScreen} maxWidth="md" fullWidth>
      <DialogTitle sx={{ pr: 6 }}>
        {link.title}
        <IconButton
          aria-label="Close"
          onClick={onClose}
          sx={{ position: 'absolute', right: 8, top: 8 }}
        >
          <CloseIcon />
        </IconButton>
      </DialogTitle>
      <DialogContent>
        <Stack spacing={1.5} alignItems="center">
          {failed ? (
            <Typography variant="body2" color="text.secondary">
              This image couldn’t be shown here.
            </Typography>
          ) : (
            <Box
              component="img"
              src={link.url}
              alt={link.title}
              referrerPolicy="no-referrer"
              onError={() => setFailed(true)}
              sx={{ maxWidth: '100%', height: 'auto', bgcolor: '#fff' }}
            />
          )}
          <Link
            href={link.url}
            target="_blank"
            rel="noopener noreferrer"
            sx={{ display: 'inline-flex', alignItems: 'center', gap: 0.5 }}
          >
            Open in new tab <OpenInNewIcon sx={{ fontSize: 14 }} />
          </Link>
        </Stack>
      </DialogContent>
    </Dialog>
  );
}
