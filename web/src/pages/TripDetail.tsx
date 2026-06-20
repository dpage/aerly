import { useEffect, useState } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import {
  Alert,
  Box,
  Button,
  CircularProgress,
  IconButton,
  ListItemIcon,
  Menu,
  MenuItem,
  Snackbar,
  Tab,
  Tabs,
  Tooltip,
  Typography,
  useMediaQuery,
  useTheme,
} from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
import ArrowBackIcon from '@mui/icons-material/ArrowBack';
import MoreVertIcon from '@mui/icons-material/MoreVert';
import PeopleIcon from '@mui/icons-material/PeopleOutline';
import EditIcon from '@mui/icons-material/EditOutlined';
import CalendarMonthIcon from '@mui/icons-material/CalendarMonth';
import FileDownloadIcon from '@mui/icons-material/FileDownloadOutlined';
import PictureAsPdfIcon from '@mui/icons-material/PictureAsPdfOutlined';

import { api } from '../api/client';
import { showExternalPlansEnabled } from '../lib/showExternalPlans';
import { useStore } from '../state/store';
import { useOnlineStatus } from '../pwa';
import { fmtTripDates, plansOutsideTripDates } from '../lib/trip-format';
import AddToTripDialog from '../components/AddToTripDialog';
import TripMembersDialog from '../components/TripMembersDialog';
import TripEditDialog from '../components/TripEditDialog';
import CalendarSubscribeDialog from '../components/CalendarSubscribeDialog';
import TripReminderToggle from '../components/TripReminderToggle';

/** Trip detail layout (spec §11). Holds the Timeline / Map sub-tabs and loads
 * the trip into the store on mount; the active tab renders via the nested
 * route `<Outlet>`. Wave 0b wires loading + tab navigation; the tab bodies are
 * placeholders fleshed out in Wave 1F. */
export default function TripDetail() {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const tripId = Number(params.id);

  // Where "Back" should return to: the list we arrived from (TripList stashes
  // its pathname in the navigation state), defaulting to the home trips list
  // when there's no origin (e.g. a deep link or a hard refresh). Captured once
  // on mount — switching the Timeline/Map tabs replaces location.state, so we
  // read it lazily here to keep the origin stable for the page's lifetime.
  const [backTo] = useState<string>(() => {
    const from = (location.state as { from?: string } | null)?.from;
    return typeof from === 'string' ? from : '/';
  });

  const currentTrip = useStore((s) => s.currentTrip);
  const currentTripStatus = useStore((s) => s.currentTripStatus);
  const loadTrip = useStore((s) => s.loadTrip);
  const clearCurrentTrip = useStore((s) => s.clearCurrentTrip);
  const online = useOnlineStatus();
  const [shareOpen, setShareOpen] = useState(false);
  const [subscribeOpen, setSubscribeOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [newPlanOpen, setNewPlanOpen] = useState(false);
  // On phones the four toolbar buttons crowd the trip name off-screen, so the
  // secondary actions collapse into an overflow (⋮) menu; New plan stays primary.
  const [actionsAnchor, setActionsAnchor] = useState<HTMLElement | null>(null);
  // Surfaces a failure if an export download (.ics / PDF) can't be fetched.
  const [exportError, setExportError] = useState<string | null>(null);
  const theme = useTheme();
  const isNarrow = useMediaQuery(theme.breakpoints.down('sm'));
  const closeActions = () => setActionsAnchor(null);

  const exportIcs = () => {
    void api.exportTripIcs(tripId).catch((err: unknown) => {
      setExportError(err instanceof Error ? err.message : String(err));
    });
  };

  const exportPdf = () => {
    // Match the on-screen view: include external feed events in the PDF only
    // when the viewer's "Show external plans" toggle is on.
    void api.exportTripPdf(tripId, showExternalPlansEnabled()).catch((err: unknown) => {
      setExportError(err instanceof Error ? err.message : String(err));
    });
  };

  const dismissExportError = () => setExportError(null);

  useEffect(() => {
    if (!Number.isFinite(tripId)) return;
    void loadTrip(tripId);
    return () => clearCurrentTrip();
  }, [tripId, loadTrip, clearCurrentTrip]);

  const onMap = location.pathname.endsWith('/map');
  const tab = onMap ? 'map' : 'timeline';
  const loaded = currentTrip?.id === tripId ? currentTrip : null;
  // No internal id in the placeholder: until the trip loads we show no name
  // rather than "Trip #35". The body below shows a spinner or a clean message.
  const title = loaded ? loaded.name : '';
  const unavailable = !loaded && currentTripStatus === 'error';
  // Secondary header line beside the name: destination and, when the trip has
  // any dates, its from/to span. Omitted entirely when neither is known.
  const hasDates =
    loaded != null &&
    Boolean(loaded.starts_on || loaded.ends_on || loaded.effective_start || loaded.effective_end);
  const meta = loaded
    ? [loaded.destination, hasDates ? fmtTripDates(loaded) : ''].filter(Boolean).join(' · ')
    : '';
  // Only owners/editors get the tag editor; viewers see nothing to change.
  const canEdit = loaded != null && loaded.my_role !== 'viewer';
  const datesMismatch = loaded != null && plansOutsideTripDates(loaded, loaded.plans);

  return (
    <Box sx={{ height: '100%', display: 'flex', flexDirection: 'column' }}>
      {isNarrow ? (
        <Box sx={{ px: 1.5, pt: 2, display: 'flex', alignItems: 'flex-start', gap: 0.5 }}>
          <Tooltip title="Back to trips">
            <IconButton
              size="small"
              edge="start"
              aria-label="Back to trips"
              onClick={() => navigate(backTo)}
              sx={{ flexShrink: 0 }}
            >
              <ArrowBackIcon />
            </IconButton>
          </Tooltip>
          {/* Content column: title + actions on top, dates on their own
              full-width line below so they're never squeezed off-screen. */}
          <Box sx={{ flexGrow: 1, minWidth: 0 }}>
            <Box sx={{ display: 'flex', alignItems: 'center', gap: 0.5 }}>
              <Typography variant="h5" noWrap sx={{ flexGrow: 1, minWidth: 0 }}>
                {title}
              </Typography>
              {loaded && canEdit && online && (
                <Button
                  variant="contained"
                  size="small"
                  startIcon={<AddIcon />}
                  onClick={() => setNewPlanOpen(true)}
                  sx={{ flexShrink: 0, whiteSpace: 'nowrap' }}
                >
                  New plan
                </Button>
              )}
              {loaded && online && (
                <>
                  <Tooltip title="More actions">
                    <IconButton
                      size="small"
                      aria-label="More actions"
                      onClick={(e) => setActionsAnchor(e.currentTarget)}
                      sx={{ flexShrink: 0 }}
                    >
                      <MoreVertIcon />
                    </IconButton>
                  </Tooltip>
                  <Menu
                    anchorEl={actionsAnchor}
                    open={actionsAnchor !== null}
                    onClose={closeActions}
                    anchorOrigin={{ vertical: 'bottom', horizontal: 'right' }}
                    transformOrigin={{ vertical: 'top', horizontal: 'right' }}
                  >
                    {canEdit && (
                      <MenuItem
                        onClick={() => {
                          closeActions();
                          setEditOpen(true);
                        }}
                      >
                        <ListItemIcon>
                          <EditIcon fontSize="small" />
                        </ListItemIcon>
                        Edit
                      </MenuItem>
                    )}
                    <MenuItem
                      onClick={() => {
                        closeActions();
                        setShareOpen(true);
                      }}
                    >
                      <ListItemIcon>
                        <PeopleIcon fontSize="small" />
                      </ListItemIcon>
                      Share
                    </MenuItem>
                    <MenuItem
                      onClick={() => {
                        closeActions();
                        setSubscribeOpen(true);
                      }}
                    >
                      <ListItemIcon>
                        <CalendarMonthIcon fontSize="small" />
                      </ListItemIcon>
                      Subscribe
                    </MenuItem>
                    <MenuItem
                      onClick={() => {
                        closeActions();
                        exportIcs();
                      }}
                    >
                      <ListItemIcon>
                        <FileDownloadIcon fontSize="small" />
                      </ListItemIcon>
                      Export .ics
                    </MenuItem>
                    <MenuItem
                      onClick={() => {
                        closeActions();
                        exportPdf();
                      }}
                    >
                      <ListItemIcon>
                        <PictureAsPdfIcon fontSize="small" />
                      </ListItemIcon>
                      Download PDF
                    </MenuItem>
                  </Menu>
                </>
              )}
            </Box>
            {meta && (
              <Typography variant="body2" color="text.secondary" noWrap sx={{ maxWidth: '100%' }}>
                {meta}
              </Typography>
            )}
          </Box>
        </Box>
      ) : (
        <Box sx={{ px: 3, pt: 2, display: 'flex', alignItems: 'center', gap: 2 }}>
          <Button size="small" onClick={() => navigate(backTo)}>
            ← Trips
          </Button>
          <Box sx={{ flexGrow: 1, minWidth: 0, display: 'flex', alignItems: 'baseline', gap: 1.5 }}>
            <Typography variant="h5" noWrap>
              {title}
            </Typography>
            {meta && (
              <Typography variant="body2" color="text.secondary" noWrap>
                {meta}
              </Typography>
            )}
          </Box>
          {loaded && canEdit && online && (
            <Button
              variant="contained"
              size="small"
              startIcon={<AddIcon />}
              onClick={() => setNewPlanOpen(true)}
            >
              New plan
            </Button>
          )}
          {loaded && canEdit && online && (
            <Button size="small" startIcon={<EditIcon />} onClick={() => setEditOpen(true)}>
              Edit
            </Button>
          )}
          {loaded && online && (
            <Button size="small" startIcon={<PeopleIcon />} onClick={() => setShareOpen(true)}>
              Share
            </Button>
          )}
          {loaded && online && (
            <Button
              size="small"
              startIcon={<CalendarMonthIcon />}
              onClick={() => setSubscribeOpen(true)}
            >
              Subscribe
            </Button>
          )}
          {loaded && online && (
            <Button size="small" startIcon={<FileDownloadIcon />} onClick={exportIcs}>
              Export .ics
            </Button>
          )}
          {loaded && online && (
            <Button size="small" startIcon={<PictureAsPdfIcon />} onClick={exportPdf}>
              Download PDF
            </Button>
          )}
        </Box>
      )}
      {datesMismatch && (
        <Box sx={{ px: 3, pt: 1 }}>
          <Alert severity="warning" sx={{ py: 0 }}>
            Some plans fall outside this trip&apos;s dates
            {canEdit ? ' — check the dates with Edit.' : '.'}
          </Alert>
        </Box>
      )}
      {loaded && (
        <Box sx={{ px: 3, pt: 1 }}>
          <TripReminderToggle trip={loaded} />
        </Box>
      )}
      {/* Tabs + routed body once the trip is loaded; otherwise a spinner while
          it loads, or a clean message when it can't be (offline and not cached,
          or no longer available) — never an internal id or a stuck "Loading…". */}
      {loaded ? (
        <>
          <Tabs
            value={tab}
            onChange={(_e, v) =>
              navigate(v === 'map' ? `/trips/${tripId}/map` : `/trips/${tripId}`)
            }
            sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
          >
            <Tab label="Timeline" value="timeline" />
            <Tab label="Map" value="map" />
          </Tabs>
          <Box sx={{ position: 'relative', flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
            <Outlet />
          </Box>
        </>
      ) : unavailable ? (
        <Box sx={{ px: 3, pt: 3 }}>
          <Alert severity="info">
            {online
              ? "Sorry, this trip couldn't be loaded. Please try again."
              : "This trip isn't saved for offline viewing yet. Reconnect to the internet to open it."}
          </Alert>
        </Box>
      ) : (
        <Box sx={{ display: 'grid', placeItems: 'center', flexGrow: 1, minHeight: 0 }}>
          <CircularProgress />
        </Box>
      )}
      {loaded && canEdit && (
        <AddToTripDialog
          open={newPlanOpen}
          tripId={loaded.id}
          onClose={() => setNewPlanOpen(false)}
        />
      )}
      {loaded && canEdit && (
        <TripEditDialog
          open={editOpen}
          trip={loaded}
          onClose={() => setEditOpen(false)}
          onDeleted={() => navigate(backTo)}
        />
      )}
      {loaded && (
        <TripMembersDialog
          open={shareOpen}
          tripId={loaded.id}
          myRole={loaded.my_role}
          members={loaded.members}
          passengerIds={loaded.passenger_ids}
          shareAllFriendsRole={loaded.share_all_friends_role}
          onClose={() => setShareOpen(false)}
        />
      )}
      <CalendarSubscribeDialog
        open={subscribeOpen}
        onClose={() => setSubscribeOpen(false)}
        scope="trip"
        id={tripId}
        title={title}
      />
      <Snackbar
        open={exportError !== null}
        autoHideDuration={6000}
        onClose={dismissExportError}
        anchorOrigin={{ vertical: 'bottom', horizontal: 'center' }}
      >
        <Alert severity="error" onClose={dismissExportError} sx={{ width: '100%' }}>
          Couldn&apos;t download this trip: {exportError}
        </Alert>
      </Snackbar>
    </Box>
  );
}
