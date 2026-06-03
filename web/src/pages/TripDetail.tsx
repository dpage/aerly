import { useEffect, useState } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import {
  Alert,
  Box,
  Button,
  IconButton,
  ListItemIcon,
  Menu,
  MenuItem,
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

import { useStore } from '../state/store';
import { fmtTripDates, plansOutsideTripDates } from '../lib/trip-format';
import AddToTripDialog from '../components/AddToTripDialog';
import TripMembersDialog from '../components/TripMembersDialog';
import TripEditDialog from '../components/TripEditDialog';
import CalendarSubscribeDialog from '../components/CalendarSubscribeDialog';

/** Trip detail layout (spec §11). Holds the Timeline / Map sub-tabs and loads
 * the trip into the store on mount; the active tab renders via the nested
 * route `<Outlet>`. Wave 0b wires loading + tab navigation; the tab bodies are
 * placeholders fleshed out in Wave 1F. */
export default function TripDetail() {
  const params = useParams();
  const navigate = useNavigate();
  const location = useLocation();
  const tripId = Number(params.id);

  const currentTrip = useStore((s) => s.currentTrip);
  const loadTrip = useStore((s) => s.loadTrip);
  const clearCurrentTrip = useStore((s) => s.clearCurrentTrip);
  const [shareOpen, setShareOpen] = useState(false);
  const [subscribeOpen, setSubscribeOpen] = useState(false);
  const [editOpen, setEditOpen] = useState(false);
  const [newPlanOpen, setNewPlanOpen] = useState(false);
  // On phones the four toolbar buttons crowd the trip name off-screen, so the
  // secondary actions collapse into an overflow (⋮) menu; New plan stays primary.
  const [actionsAnchor, setActionsAnchor] = useState<HTMLElement | null>(null);
  const theme = useTheme();
  const isNarrow = useMediaQuery(theme.breakpoints.down('sm'));
  const closeActions = () => setActionsAnchor(null);

  useEffect(() => {
    if (!Number.isFinite(tripId)) return;
    void loadTrip(tripId);
    return () => clearCurrentTrip();
  }, [tripId, loadTrip, clearCurrentTrip]);

  const onMap = location.pathname.endsWith('/map');
  const tab = onMap ? 'map' : 'timeline';
  const loaded = currentTrip?.id === tripId ? currentTrip : null;
  const title = loaded ? loaded.name : `Trip #${tripId}`;
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
              onClick={() => navigate('/')}
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
              {loaded && canEdit && (
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
              {loaded && (
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
          <Button size="small" onClick={() => navigate('/')}>
            ← Trips
          </Button>
          <Box
            sx={{ flexGrow: 1, minWidth: 0, display: 'flex', alignItems: 'baseline', gap: 1.5 }}
          >
            <Typography variant="h5" noWrap>
              {title}
            </Typography>
            {meta && (
              <Typography variant="body2" color="text.secondary" noWrap>
                {meta}
              </Typography>
            )}
          </Box>
          {loaded && canEdit && (
            <Button
              variant="contained"
              size="small"
              startIcon={<AddIcon />}
              onClick={() => setNewPlanOpen(true)}
            >
              New plan
            </Button>
          )}
          {loaded && canEdit && (
            <Button size="small" startIcon={<EditIcon />} onClick={() => setEditOpen(true)}>
              Edit
            </Button>
          )}
          {loaded && (
            <Button size="small" startIcon={<PeopleIcon />} onClick={() => setShareOpen(true)}>
              Share
            </Button>
          )}
          <Button
            size="small"
            startIcon={<CalendarMonthIcon />}
            onClick={() => setSubscribeOpen(true)}
          >
            Subscribe
          </Button>
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
      <Tabs
        value={tab}
        onChange={(_e, v) => navigate(v === 'map' ? `/trips/${tripId}/map` : `/trips/${tripId}`)}
        sx={{ px: 3, borderBottom: 1, borderColor: 'divider' }}
      >
        <Tab label="Timeline" value="timeline" />
        <Tab label="Map" value="map" />
      </Tabs>
      <Box sx={{ position: 'relative', flexGrow: 1, minHeight: 0, overflowY: 'auto' }}>
        <Outlet />
      </Box>
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
          onDeleted={() => navigate('/')}
        />
      )}
      {loaded && (
        <TripMembersDialog
          open={shareOpen}
          tripId={loaded.id}
          myRole={loaded.my_role}
          members={loaded.members}
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
    </Box>
  );
}
