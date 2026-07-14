import { useEffect, useRef, useState } from 'react';
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
import PoiMiniMap from './PoiMiniMap';
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

const CATEGORY_LABELS: Record<PoiCategory, string> = Object.fromEntries(
  CATEGORIES.map((c) => [c.value, c.label]),
) as Record<PoiCategory, string>;

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

/** Builds a Wikipedia article URL from an OSM `wikipedia` tag value, which is
 * formatted as "<lang>:<Article Title>" (e.g. "en:Uffizi Gallery"). Falls
 * back to the English wiki when the tag has no "<lang>:" prefix. */
function wikipediaUrl(tag: string): string {
  const sep = tag.indexOf(':');
  const lang = sep > 0 ? tag.slice(0, sep) : 'en';
  const title = sep > 0 ? tag.slice(sep + 1) : tag;
  return `https://${lang}.wikipedia.org/wiki/${encodeURIComponent(title.replace(/ /g, '_'))}`;
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
  // Bumped by the "Try again" button to re-run the fetch after a transient
  // upstream failure without changing any of the real query inputs.
  const [reloadKey, setReloadKey] = useState(0);
  // The resolved search centre (from the response) for the mini-map anchor, and
  // the currently-selected POI (set by clicking a map pin) for row highlighting.
  const [center, setCenter] = useState<{ lat: number; lon: number } | undefined>(
    initialCenter ? { lat: initialCenter.lat, lon: initialCenter.lon } : undefined,
  );
  const [selectedId, setSelectedId] = useState<string | undefined>(undefined);
  const rowRefs = useRef<Record<string, HTMLLIElement | null>>({});

  // Fetches on mount and whenever the category set, radius, place, or centre
  // coordinates change. Coordinates from initialCenter win over the typed place
  // when both are present (the caller — e.g. a map pin — knows exactly where it
  // means). We key the effect on the coordinate VALUES, not the initialCenter
  // object, so a caller passing an inline `{ lat, lon, label }` literal doesn't
  // trigger a needless refetch on every unrelated parent re-render (its label
  // isn't a fetch input either).
  const centerLat = initialCenter?.lat;
  const centerLon = initialCenter?.lon;
  useEffect(() => {
    let cancelled = false;
    setLoading(true);
    setError(undefined);
    const opts =
      centerLat != null && centerLon != null
        ? { lat: centerLat, lon: centerLon, cats, radius }
        : { place: placeQuery, cats, radius };
    api
      .fetchPois(tripId, opts)
      .then((res) => {
        if (cancelled) return;
        setPois(res.pois);
        setCenter(res.center);
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
  }, [tripId, placeQuery, cats, radius, centerLat, centerLon, reloadKey]);

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

  // Bring the row for a map-selected POI into view. Guarded because jsdom
  // doesn't implement scrollIntoView.
  useEffect(() => {
    if (!selectedId) return;
    const row = rowRefs.current[selectedId];
    if (row && typeof row.scrollIntoView === 'function') {
      row.scrollIntoView({ block: 'nearest' });
    }
  }, [selectedId]);

  return (
    <Stack spacing={2} sx={{ p: { xs: 2, sm: 3 }, maxWidth: 900, mx: 'auto', width: '100%' }}>
      {/* When anchored to a fixed point (e.g. a hotel), the place search is
          irrelevant, so hide it rather than showing a disabled field. */}
      {!initialCenter && (
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
            size="small"
            fullWidth
          />
          <Button type="submit" variant="outlined" startIcon={<SearchIcon />}>
            Search
          </Button>
        </Box>
      )}

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
        <Stack direction="row" spacing={1} alignItems="center" flexWrap="wrap" useFlexGap>
          <Typography color="error" variant="body2">
            Couldn’t load nearby places: {error}
          </Typography>
          <Button size="small" onClick={() => setReloadKey((k) => k + 1)}>
            Try again
          </Button>
        </Stack>
      )}

      {!loading && !error && filtered.length === 0 && (
        <Typography variant="body2" color="text.secondary">
          No places found nearby. Try a different area, widen the radius, or turn on more
          categories.
        </Typography>
      )}

      {!error && filtered.length > 0 && (
        <PoiMiniMap
          pois={filtered}
          center={center}
          selectedId={selectedId}
          onSelectPoi={setSelectedId}
        />
      )}

      <List>
        {filtered.map((poi) => (
          <ListItem
            key={poi.id}
            divider
            ref={(el: HTMLLIElement | null) => {
              rowRefs.current[poi.id] = el;
            }}
            sx={
              poi.id === selectedId
                ? { bgcolor: 'action.selected', borderRadius: 1 }
                : undefined
            }
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
                {CATEGORY_LABELS[poi.category]} · {formatDistance(poi.distance_m)} away
              </Typography>
              <Stack direction="row" spacing={1.5}>
                <Link component="button" type="button" onClick={() => setSelectedId(poi.id)}>
                  Show on map
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
                {poi.wikipedia && (
                  <Link href={wikipediaUrl(poi.wikipedia)} target="_blank" rel="noopener">
                    Wikipedia
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
