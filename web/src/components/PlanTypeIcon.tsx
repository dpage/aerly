import type { SvgIconProps } from '@mui/material';
import FlightIcon from '@mui/icons-material/Flight';
import TrainIcon from '@mui/icons-material/Train';
import HotelIcon from '@mui/icons-material/Hotel';
import DirectionsCarIcon from '@mui/icons-material/DirectionsCar';
import RestaurantIcon from '@mui/icons-material/Restaurant';
import LocalActivityIcon from '@mui/icons-material/LocalActivity';
import IcecreamIcon from '@mui/icons-material/Icecream';
import GroupsIcon from '@mui/icons-material/Groups';
import EventIcon from '@mui/icons-material/Event';
import PlaceIcon from '@mui/icons-material/Place';

import type { PlanType } from '../api/types';

/** Maps a plan/part type to its MUI icon. */
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
    case 'meeting':
      return <GroupsIcon {...props} />;
    case 'event':
      return <EventIcon {...props} />;
    default:
      return <PlaceIcon {...props} />;
  }
}
