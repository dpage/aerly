import type { MouseEvent } from 'react';
import { IconButton, Tooltip } from '@mui/material';
import OpenInNewIcon from '@mui/icons-material/OpenInNew';

import type { FlightDetail } from '../api/types';
import { flightradar24Url } from '../lib/flightradar24';

/** A small "View on Flightradar24" link for a flight, or nothing when the
 * flight has no usable ident. It is a real anchor rather than a click handler
 * that calls window.open, because iOS universal links and Android app links
 * only hand off to the installed app on a user's tap of an ordinary link. */
export default function Flightradar24Link({
  flight,
  onClick,
}: {
  flight: Pick<FlightDetail, 'ident' | 'callsign'>;
  /** Called on click, e.g. to stop the tap reaching a clickable tile. */
  onClick?: (e: MouseEvent) => void;
}) {
  const href = flightradar24Url(flight);
  if (!href) return null;
  return (
    <Tooltip title="View on Flightradar24">
      <IconButton
        component="a"
        href={href}
        target="_blank"
        rel="noopener noreferrer"
        aria-label="View on Flightradar24"
        size="small"
        onClick={onClick}
        sx={{ p: 0.25, ml: 0.5, verticalAlign: 'middle' }}
      >
        <OpenInNewIcon sx={{ fontSize: 14 }} />
      </IconButton>
    </Tooltip>
  );
}
