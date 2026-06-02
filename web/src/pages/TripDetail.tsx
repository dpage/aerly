import { useEffect, useState } from 'react';
import { Outlet, useLocation, useNavigate, useParams } from 'react-router-dom';
import { Alert, Box, Button, Tab, Tabs, Typography } from '@mui/material';
import AddIcon from '@mui/icons-material/Add';
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
          <Button
            size="small"
            startIcon={<PeopleIcon />}
            onClick={() => setShareOpen(true)}
          >
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
