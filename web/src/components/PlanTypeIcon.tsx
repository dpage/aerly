import type { SvgIconProps } from '@mui/material';
import FlightIcon from '@mui/icons-material/Flight';
import TrainIcon from '@mui/icons-material/Train';
import HotelIcon from '@mui/icons-material/Hotel';
import DirectionsCarIcon from '@mui/icons-material/DirectionsCar';
import RestaurantIcon from '@mui/icons-material/Restaurant';
import LocalActivityIcon from '@mui/icons-material/LocalActivity';
import IcecreamIcon from '@mui/icons-material/Icecream';
import PlaceIcon from '@mui/icons-material/Place';

import type { PlanType } from '../api/types';

/** Maps a plan/part type to its MUI icon (spec §11 / PRD §6.2 icon set:
 * plane, train, bed, ground transport, meal, excursion). */
export default function PlanTypeIcon({ type, ...props }: { type: PlanType } & SvgIconProps) {
  switch (type) {
    case 'flight':
      return <FlightIcon {...props} />;
    case 'train':
      return <TrainIcon {...props} />;
    case 'hotel':
      return <HotelIcon {...props} />;
    case 'ground':
      return <DirectionsCarIcon {...props} />;
    case 'dining':
      return <RestaurantIcon {...props} />;
    case 'excursion':
      return <LocalActivityIcon {...props} />;
    case 'ice_cream':
      return <IcecreamIcon {...props} />;
    default:
      return <PlaceIcon {...props} />;
  }
}
