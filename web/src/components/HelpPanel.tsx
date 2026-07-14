import { useEffect, useRef, useState } from 'react';
import {
  Box,
  Breadcrumbs,
  Drawer,
  IconButton,
  Link,
  List,
  ListItemButton,
  ListItemIcon,
  ListItemText,
  Tooltip,
  Typography,
} from '@mui/material';
import ChevronRightIcon from '@mui/icons-material/ChevronRight';
import CloseIcon from '@mui/icons-material/Close';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';

import { useStore } from '../state/store';
import { HELP_PAGES, contextToPageId } from './help/HelpContent';

const OVERVIEW = 'overview';

/** In-app help: a right-side drawer with a topic nav, breadcrumb/back
 * navigation and the selected topic's content. Opened from the top-bar help
 * button or from an inline "How sharing works" link (both via the store's
 * openHelp), optionally seeded to a topic. */
export default function HelpPanel() {
  const open = useStore((s) => s.helpOpen);
  const helpPage = useStore((s) => s.helpPage);
  const closeHelp = useStore((s) => s.closeHelp);

  const [currentId, setCurrentId] = useState(OVERVIEW);
  const contentRef = useRef<HTMLDivElement>(null);

  // When opened, jump to the page the caller asked for (via its context hint).
  useEffect(() => {
    if (open) setCurrentId(contextToPageId(helpPage));
  }, [open, helpPage]);

  // Scroll back to the top whenever the topic changes.
  useEffect(() => {
    if (contentRef.current) contentRef.current.scrollTop = 0;
  }, [currentId]);

  const page = HELP_PAGES.find((p) => p.id === currentId) ?? HELP_PAGES[0];
  const onOverview = page.id === OVERVIEW;

  return (
    <Drawer
      anchor="right"
      open={open}
      onClose={closeHelp}
      slotProps={{ paper: { sx: { width: { xs: '100%', sm: 400 }, maxWidth: '100%' } } }}
    >
      {/* Top padding clears the iOS status bar in standalone PWA mode (the
          full-height panel runs under it); a no-op in the browser/Android. */}
      <Box
        sx={{
          display: 'flex',
          flexDirection: 'column',
          height: '100%',
          pt: 'env(safe-area-inset-top)',
        }}
      >
        {/* Header: back + breadcrumbs + close */}
        <Box
          sx={{
            display: 'flex',
            alignItems: 'center',
            gap: 1,
            px: 2,
            py: 1.5,
            borderBottom: 1,
            borderColor: 'divider',
          }}
        >
          {!onOverview && (
            <Tooltip title="Back to help overview">
              <IconButton
                size="small"
                aria-label="Back to help overview"
                onClick={() => setCurrentId(OVERVIEW)}
              >
                <ArrowBackIcon fontSize="small" />
              </IconButton>
            </Tooltip>
          )}
          <Breadcrumbs separator={<ChevronRightIcon fontSize="small" />} sx={{ flexGrow: 1 }}>
            {!onOverview && (
              <Link component="button" underline="hover" onClick={() => setCurrentId(OVERVIEW)}>
                Help
              </Link>
            )}
            <Typography color="text.primary" sx={{ fontWeight: 600 }}>
              {onOverview ? 'Help & guide' : page.label}
            </Typography>
          </Breadcrumbs>
          <Tooltip title="Close help">
            <IconButton size="small" aria-label="Close help" onClick={closeHelp}>
              <CloseIcon fontSize="small" />
            </IconButton>
          </Tooltip>
        </Box>

        {/* Topic nav */}
        <List dense disablePadding sx={{ borderBottom: 1, borderColor: 'divider', py: 0.5 }}>
          {HELP_PAGES.map(({ id, label, Icon }) => (
            <ListItemButton key={id} selected={id === currentId} onClick={() => setCurrentId(id)}>
              <ListItemIcon sx={{ minWidth: 36 }}>
                <Icon fontSize="small" />
              </ListItemIcon>
              <ListItemText primary={label} />
            </ListItemButton>
          ))}
        </List>

        {/* Content */}
        <Box ref={contentRef} sx={{ flexGrow: 1, overflowY: 'auto', p: 2 }}>
          <Typography variant="h6" sx={{ mb: 1 }}>
            {page.label}
          </Typography>
          {page.body}
        </Box>

        <Box sx={{ px: 2, py: 1, borderTop: 1, borderColor: 'divider' }}>
          <Typography variant="caption" color="text.secondary">
            Aerly — trip planning
          </Typography>
        </Box>
      </Box>
    </Drawer>
  );
}
