import { useMemo } from 'react';

import { useStore } from '../state/store';
import PlanMapView from '../components/PlanMapView';

/** Trip detail Map tab (spec §11): the trip's plans on a shared map+list view.
 * The same component backs the global Tracker — here it simply runs over the
 * current trip's parts, with no time controls. */
export default function TripMap() {
  const currentTrip = useStore((s) => s.currentTrip);
  const parts = useMemo(() => currentTrip?.plans.flatMap((p) => p.parts) ?? [], [currentTrip]);
  return <PlanMapView parts={parts} />;
}
