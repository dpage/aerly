import { useStore } from '../state/store';
import ExplorePanel from '../components/ExplorePanel';

/** Trip detail Explore tab: nearby points of interest anchored to the trip's
 * destination, so travellers can plan sightseeing without leaving the trip. */
export default function TripExplore() {
  const trip = useStore((s) => s.currentTrip);
  if (!trip) return null;
  return <ExplorePanel tripId={trip.id} initialPlace={trip.destination} />;
}
