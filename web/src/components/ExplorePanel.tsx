import { useEffect, useState } from 'react';
import {
  Box,
  Button,
  Chip,
  LinearProgress,
  Link,
  List,
  ListItem,
  Stack,
  TextField,
  ToggleButton,
  ToggleButtonGroup,
  Typography,
} from '@mui/material';
import SearchIcon from '@mui/icons-material/Search';
import MuseumIcon from '@mui/icons-material/Museum';
import PhotoCameraIcon from '@mui/icons-material/PhotoCamera';
import AccountBalanceIcon from '@mui/icons-material/AccountBalance';
import ParkIcon from '@mui/icons-material/Park';
import RestaurantIcon from '@mui/icons-material/Restaurant';

import { api } from '../api/client';
import { errorMessage } from '../state/helpers';
import AddToTripDialog, { type PlanPrefill } from './AddToTripDialog';
import type { Poi, PoiCategory } from '../api/types';

export interface ExplorePanelProps {
  tripId: number;
  initialPlace?: string;
  initialCenter?: { lat: number; lon: number; label?: string };
}

const CATEGORIES: { value: PoiCategory; label: string }[] = [
  { value: 'sights', label: 'Sights' },
  { value: 'museum', label: 'Museum' },
  { value: 'landmark', label: 'Landmark' },
  { value: 'park', label: 'Park' },
  { value: 'food', label: 'Food' },
];

const DEFAULT_CATS: PoiCategory[] = ['sights', 'museum', 'landmark', 'park'];

const RADII = [1000, 2000, 5000];

const CATEGORY_ICONS: Record<PoiCategory, typeof MuseumIcon> = {
  museum: MuseumIcon,
  sights: PhotoCameraIcon,
  landmark: AccountBalanceIcon,
  park: ParkIcon,
  food: RestaurantIcon,
};

/** Maps a POI category to its list-row icon. `PoiCategory` is a closed union,
 * so this covers every value with no dead default branch to test around. */
function CategoryIcon({ category }: { category: PoiCategory }) {
  const Icon = CATEGORY_ICONS[category];
  return <Icon fontSize="small" />;
}

/** Formats a distance in metres as "400 m" (under 1 km) or "1.2 km". */
function formatDistance(m: number): string {
  if (m < 1000) return `${Math.round(m)} m`;
  return `${(m / 1000).toFixed(1)} km`;
}

function radiusLabel(m: number): string {
  return `${m / 1000} km`;
}

/** Panel for browsing nearby points of interest and adding one to the trip as
 * an excursion plan. The place/category/radius controls re-query the server;
 * the name filter narrows the already-loaded results client-side, so it feels
 * instant and never spams the API on every keystroke. */
export default function ExplorePanel({ tripId, initialPlace, initialCenter }: ExplorePanelProps) {
  const [place, setPlace] = useState(initialPlace ?? '');
  const [placeQuery, setPlaceQuery] = useState(initialPlace ?? '');
  const [cats, setCats] = useState<PoiCategory[]>(DEFAULT_CATS);
  const [radius, setRadius] = useState(2000);
  const [nameQ, setNameQ] = useState('');
  const [pois, setPois] = useState<Poi[]>([]);
  const [loading, setLoading] = useState(false);
  const [error, setError] = useState<string | undefined>(undefined);
  const [prefill, setPrefill] = useState<PlanPrefill | undefined>(undefined);
  const [addOpen, setAddOpen] = useState(false);

  // Fetches on mount and whenever the category set, radius, place, or
  // initialCenter change. Coordinates from initialCenter win over the typed
  // place when both are present (the caller — e.g. a map pin — knows exactly
  // where it means).
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(undefined);
    const opts = initialCenter
      ? { lat: initialCenter.lat, lon: initialCenter.lon, cats, radius }
      : { place: placeQuery, cats, radius };
    api
      .fetchPois(tripId, opts)
      .then((res) => {
        if (cancelled) return;
        setPois(res.pois);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(errorMessage(err));
        setPois([]);
      })
      .finally(() => {
        if (cancelled) return;
        setLoading(false);
      });
    return () => {
      cancelled = true;
    };
  }, [tripId, placeQuery, cats, radius, initialCenter]);

  const toggleCategory = (cat: PoiCategory) => {
    setCats((cs) => (cs.includes(cat) ? cs.filter((c) => c !== cat) : [...cs, cat]));
  };

  const openAdd = (poi: Poi) => {
    setPrefill({
      type: 'excursion',
      title: poi.name,
      startLabel: poi.name,
      startAddress: poi.address,
      startLat: poi.lat,
      startLon: poi.lon,
    });
    setAddOpen(true);
  };

  const filtered = nameQ.trim()
    ? pois.filter((p) => p.name.toLowerCase().includes(nameQ.trim().toLowerCase()))
    : pois;

  return (
    <Stack spacing={2}>
      <Box
        component="form"
        onSubmit={(e) => {
          e.preventDefault();
          setPlaceQuery(place);
        }}
        sx={{ display: 'flex', gap: 1 }}
      >
        <TextField
          label="Place"
          placeholder="e.g. Lisbon, or an address"
          value={place}
          onChange={(e) => setPlace(e.target.value)}
          disabled={!!initialCenter}
          size="small"
          fullWidth
        />
        <Button type="submit" variant="outlined" startIcon={<SearchIcon />} disabled={!!initialCenter}>
          Search
        </Button>
      </Box>

      <Stack direction="row" spacing={1} flexWrap="wrap" useFlexGap>
        {CATEGORIES.map((c) => (
          <Chip
            key={c.value}
            label={c.label}
            color={cats.includes(c.value) ? 'primary' : 'default'}
            variant={cats.includes(c.value) ? 'filled' : 'outlined'}
            onClick={() => toggleCategory(c.value)}
          />
        ))}
      </Stack>

      <ToggleButtonGroup
        value={radius}
        exclusive
        size="small"
        onChange={(_, v: number | null) => {
          if (v != null) setRadius(v);
        }}
      >
        {RADII.map((r) => (
          <ToggleButton key={r} value={r}>
            {radiusLabel(r)}
          </ToggleButton>
        ))}
      </ToggleButtonGroup>

      <TextField
        label="Filter by name"
        placeholder="Narrow the results shown below"
        value={nameQ}
        onChange={(e) => setNameQ(e.target.value)}
        size="small"
        fullWidth
      />

      {loading && <LinearProgress />}
      {error && (
        <Typography color="error" variant="body2">
          Couldn’t load nearby places: {error}
        </Typography>
      )}

      {!loading && !error && filtered.length === 0 && (
        <Typography variant="body2" color="text.secondary">
          No places found nearby. Try a different area, widen the radius, or turn on more
          categories.
        </Typography>
      )}

      <List>
        {filtered.map((poi) => (
          <ListItem
            key={poi.id}
            divider
            secondaryAction={
              <Button size="small" variant="outlined" onClick={() => openAdd(poi)}>
                Add to trip
              </Button>
            }
          >
            <Stack spacing={0.5} sx={{ pr: 14, width: '100%' }}>
              <Stack direction="row" spacing={1} alignItems="center">
                <CategoryIcon category={poi.category} />
                <Typography variant="body1">{poi.name}</Typography>
              </Stack>
              <Typography variant="caption" color="text.secondary">
                {poi.category} · {formatDistance(poi.distance_m)} away
              </Typography>
              <Stack direction="row" spacing={1.5}>
                <Link href={`https://www.openstreetmap.org/${poi.id}`} target="_blank" rel="noopener">
                  Map
                </Link>
                {poi.wikidata && (
                  <Link
                    href={`https://www.wikidata.org/wiki/${poi.wikidata}`}
                    target="_blank"
                    rel="noopener"
                  >
                    Wikidata
                  </Link>
                )}
                {poi.website && (
                  <Link href={poi.website} target="_blank" rel="noopener">
                    Website
                  </Link>
                )}
              </Stack>
            </Stack>
          </ListItem>
        ))}
      </List>

      <Typography variant="caption" color="text.secondary">
        Data © OpenStreetMap contributors
      </Typography>

      <AddToTripDialog
        open={addOpen}
        tripId={tripId}
        prefill={prefill}
        onClose={() => setAddOpen(false)}
      />
    </Stack>
  );
}
