import MuseumIcon from '@mui/icons-material/Museum';
import PhotoCameraIcon from '@mui/icons-material/PhotoCamera';
import AccountBalanceIcon from '@mui/icons-material/AccountBalance';
import ParkIcon from '@mui/icons-material/Park';
import RestaurantIcon from '@mui/icons-material/Restaurant';
import LocalBarIcon from '@mui/icons-material/LocalBar';
import LocalCafeIcon from '@mui/icons-material/LocalCafe';
import SportsBarIcon from '@mui/icons-material/SportsBar';
import FastfoodIcon from '@mui/icons-material/Fastfood';
import NightlifeIcon from '@mui/icons-material/Nightlife';
import TheaterComedyIcon from '@mui/icons-material/TheaterComedy';
import MovieIcon from '@mui/icons-material/Movie';
import PaletteIcon from '@mui/icons-material/Palette';
import ForestIcon from '@mui/icons-material/Forest';
import BeachAccessIcon from '@mui/icons-material/BeachAccess';
import StorefrontIcon from '@mui/icons-material/Storefront';
import LocalMallIcon from '@mui/icons-material/LocalMall';
import ShoppingBagIcon from '@mui/icons-material/ShoppingBag';
import FitnessCenterIcon from '@mui/icons-material/FitnessCenter';
import PoolIcon from '@mui/icons-material/Pool';
import StadiumIcon from '@mui/icons-material/Stadium';
import PetsIcon from '@mui/icons-material/Pets';
import AttractionsIcon from '@mui/icons-material/Attractions';
import ChildFriendlyIcon from '@mui/icons-material/ChildFriendly';
import VisibilityIcon from '@mui/icons-material/Visibility';
import ChurchIcon from '@mui/icons-material/Church';
import type { PoiCategory } from '../api/types';

export interface Theme {
  key: string;
  label: string;
  children: PoiCategory[];
}

// Order and grouping mirror the backend themeSubcategories map. Sub-category
// keys MUST match the backend byte-for-byte (they cross the wire in `cats`).
export const THEMES: Theme[] = [
  { key: 'sights', label: 'Sights & landmarks', children: ['attractions', 'viewpoints', 'monuments_heritage'] },
  { key: 'food_drink', label: 'Food & drink', children: ['restaurants', 'cafes', 'bars', 'pubs', 'street_food'] },
  { key: 'nightlife', label: 'Live music & nightlife', children: ['nightclubs', 'live_venues', 'cinemas'] },
  { key: 'culture', label: 'Culture', children: ['museums', 'galleries', 'theatres'] },
  { key: 'outdoors', label: 'Outdoors & nature', children: ['parks_gardens', 'natural_features', 'beaches'] },
  { key: 'shopping', label: 'Shopping', children: ['markets', 'malls', 'speciality_shops'] },
  { key: 'sport', label: 'Sport & leisure', children: ['sports_centres', 'swimming', 'stadiums'] },
  { key: 'family', label: 'Family', children: ['zoos_aquariums', 'theme_parks', 'playgrounds'] },
  { key: 'worship', label: 'Worship', children: ['worship'] },
];

export const SUBCATEGORY_LABELS: Record<PoiCategory, string> = {
  attractions: 'Attractions', viewpoints: 'Viewpoints', monuments_heritage: 'Monuments & heritage',
  restaurants: 'Restaurants', cafes: 'Cafés', bars: 'Bars', pubs: 'Pubs', street_food: 'Street food',
  nightclubs: 'Nightclubs', live_venues: 'Live venues', cinemas: 'Cinemas',
  museums: 'Museums', galleries: 'Galleries', theatres: 'Theatres',
  parks_gardens: 'Parks & gardens', natural_features: 'Natural features', beaches: 'Beaches',
  markets: 'Markets', malls: 'Malls', speciality_shops: 'Speciality shops',
  sports_centres: 'Sports centres', swimming: 'Swimming', stadiums: 'Stadiums',
  zoos_aquariums: 'Zoos & aquariums', theme_parks: 'Theme parks', playgrounds: 'Playgrounds',
  worship: 'Places of worship',
};

export const SUBCATEGORY_ICONS: Record<PoiCategory, typeof MuseumIcon> = {
  attractions: PhotoCameraIcon, viewpoints: VisibilityIcon, monuments_heritage: AccountBalanceIcon,
  restaurants: RestaurantIcon, cafes: LocalCafeIcon, bars: LocalBarIcon, pubs: SportsBarIcon, street_food: FastfoodIcon,
  nightclubs: NightlifeIcon, live_venues: TheaterComedyIcon, cinemas: MovieIcon,
  museums: MuseumIcon, galleries: PaletteIcon, theatres: TheaterComedyIcon,
  parks_gardens: ParkIcon, natural_features: ForestIcon, beaches: BeachAccessIcon,
  markets: StorefrontIcon, malls: LocalMallIcon, speciality_shops: ShoppingBagIcon,
  sports_centres: FitnessCenterIcon, swimming: PoolIcon, stadiums: StadiumIcon,
  zoos_aquariums: PetsIcon, theme_parks: AttractionsIcon, playgrounds: ChildFriendlyIcon,
  worship: ChurchIcon,
};

// Sightseeing-leaning defaults, matching the pre-expansion behaviour.
export const DEFAULT_CATS: PoiCategory[] = ['attractions', 'monuments_heritage', 'museums', 'parks_gardens'];

const VALID_CATS = new Set<PoiCategory>(
  THEMES.flatMap((t) => t.children),
);

// Versioned key: bumping the suffix retires the old six-key vocabulary cleanly,
// so an upgraded client falls back to DEFAULT_CATS rather than an empty set.
const CATS_STORAGE_KEY = 'aerly.explore.subcats';

export function expandThemeChildren(theme: Theme): PoiCategory[] {
  return theme.children;
}

export function loadCats(): PoiCategory[] {
  try {
    const raw = window.localStorage.getItem(CATS_STORAGE_KEY);
    if (raw == null) return DEFAULT_CATS;
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return DEFAULT_CATS;
    return parsed.filter((v): v is PoiCategory => VALID_CATS.has(v as PoiCategory));
  } catch {
    return DEFAULT_CATS;
  }
}

export function saveCats(cats: PoiCategory[]): void {
  try {
    window.localStorage.setItem(CATS_STORAGE_KEY, JSON.stringify(cats));
  } catch {
    // Best-effort: private-mode / quota errors just mean no persistence.
  }
}
