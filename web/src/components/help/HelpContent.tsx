/* eslint-disable react-refresh/only-export-components --
   This is the help content+data module (HELP_PAGES + contextToPageId), not a
   hot-reloaded component file; the small presentational helpers are local. */
import type { ReactNode, ComponentType } from 'react';
import { Box, Typography } from '@mui/material';
import type { SvgIconProps } from '@mui/material';
import InfoOutlinedIcon from '@mui/icons-material/InfoOutlined';
import LuggageOutlinedIcon from '@mui/icons-material/LuggageOutlined';
import EventNoteOutlinedIcon from '@mui/icons-material/EventNoteOutlined';
import MapOutlinedIcon from '@mui/icons-material/MapOutlined';
import PeopleOutlineIcon from '@mui/icons-material/PeopleOutline';

// --- content primitives -----------------------------------------------------

/** A topic-section heading inside a help page. */
function SectionTitle({ children }: { children: ReactNode }) {
  return (
    <Typography variant="subtitle2" sx={{ mt: 2.5, mb: 1, fontWeight: 700 }}>
      {children}
    </Typography>
  );
}

/** A single feature/row: a bold title and an explanatory line. */
function FeatureItem({ title, description }: { title: string; description: ReactNode }) {
  return (
    <Box sx={{ mb: 1.5 }}>
      <Typography variant="body2" sx={{ fontWeight: 600 }}>
        {title}
      </Typography>
      <Typography variant="body2" color="text.secondary">
        {description}
      </Typography>
    </Box>
  );
}

/** A highlighted "Tip" callout. */
function HelpTip({ children }: { children: ReactNode }) {
  return (
    <Box
      sx={{
        mt: 2,
        p: 1.5,
        borderRadius: 1,
        bgcolor: 'action.hover',
        borderLeft: 3,
        borderColor: 'primary.main',
      }}
    >
      <Typography variant="body2">
        <Box component="strong" sx={{ color: 'primary.main' }}>
          Tip:
        </Box>{' '}
        {children}
      </Typography>
    </Box>
  );
}

function Body({ children }: { children: ReactNode }) {
  return (
    <Typography variant="body2" color="text.secondary" sx={{ mb: 1 }}>
      {children}
    </Typography>
  );
}

// --- pages -------------------------------------------------------------------

export interface HelpPage {
  id: string;
  label: string;
  Icon: ComponentType<SvgIconProps>;
  body: ReactNode;
}

/** The help topics, in nav order. Page bodies describe current behaviour. */
export const HELP_PAGES: HelpPage[] = [
  {
    id: 'overview',
    label: 'Overview',
    Icon: InfoOutlinedIcon,
    body: (
      <Box>
        <Body>
          Aerly keeps a trip&apos;s travel in one place. Add flights, hotels, trains,
          taxis, dinners and excursions to a trip, and they appear on a shared
          timeline and map — with live flight tracking where available.
        </Body>
        <SectionTitle>Getting started</SectionTitle>
        <FeatureItem
          title="1. Create a trip"
          description="From the Trips list, click New trip and give it a name (dates are optional — Aerly can infer them from the plans you add)."
        />
        <FeatureItem
          title="2. Add plans"
          description="Open the trip and click New plan. Enter it by hand, or paste / upload / forward a booking email and Aerly extracts the details."
        />
        <FeatureItem
          title="3. Share it"
          description="Invite friends as editors or viewers, and fine-tune who sees what per plan. See Sharing & privacy."
        />
        <HelpTip>
          The Map tab and the global Tracker show every mappable plan in time
          order — click an item in the list or on the map to highlight it and see
          its details.
        </HelpTip>
      </Box>
    ),
  },
  {
    id: 'trips',
    label: 'Trips',
    Icon: LuggageOutlinedIcon,
    body: (
      <Box>
        <Body>
          A trip is the container for everything else. Create one from the Trips
          list with <strong>New trip</strong>.
        </Body>
        <SectionTitle>Dates</SectionTitle>
        <FeatureItem
          title="Optional, and auto-inferred"
          description="Leave dates blank and Aerly shows an inferred span (marked with ~) from the plans inside. If a plan falls outside dates you have set, the trip flags it so you can check."
        />
        <SectionTitle>Tags & editing</SectionTitle>
        <FeatureItem
          title="Tags"
          description="Add shared tags (e.g. pgconf-eu-26) on the trip's Edit dialog to group related trips — the Tracker can scope to a tag."
        />
        <FeatureItem
          title="Edit / Delete"
          description="Use Edit on the trip page to change its name, dates and tags. The owner can also delete the trip there."
        />
      </Box>
    ),
  },
  {
    id: 'plans',
    label: 'Plans',
    Icon: EventNoteOutlinedIcon,
    body: (
      <Box>
        <Body>
          A plan is a single booking — a flight, hotel, train, taxi, meal or
          excursion. Open a trip and click <strong>New plan</strong> to add one.
        </Body>
        <SectionTitle>Four ways to capture a plan</SectionTitle>
        <FeatureItem title="Manual" description="Fill in the details yourself." />
        <FeatureItem
          title="Paste text"
          description="Paste a confirmation email or itinerary and Aerly extracts the plan, flagging anything it isn't sure about for you to confirm."
        />
        <FeatureItem title="Upload" description="Upload a booking PDF or text file to extract from." />
        <FeatureItem
          title="From email"
          description="Forward booking emails to your personal Aerly address (when enabled) and they're added automatically."
        />
        <SectionTitle>Editing & viewing</SectionTitle>
        <FeatureItem
          title="Edit everything"
          description="A plan's Edit dialog lets you change each part's date, time, timezone and start/end places — editing an address re-locates it on the map."
        />
        <FeatureItem
          title="Timeline & map"
          description="Plans show on the trip's Timeline (grouped by local day) and on the Map tab, which lists every mappable plan in time order beside the map."
        />
      </Box>
    ),
  },
  {
    id: 'tracker',
    label: 'Map & tracker',
    Icon: MapOutlinedIcon,
    body: (
      <Box>
        <Body>
          Aerly plots every plan that has a location on a map, with a
          time-ordered list beside it. You&apos;ll find it in two places: a
          trip&apos;s <strong>Map</strong> tab (that trip), and the global{' '}
          <strong>Tracker</strong> (across all your trips, by date).
        </Body>
        <SectionTitle>Reading the map</SectionTitle>
        <FeatureItem
          title="Coloured pins"
          description="Each plan type has its own pin colour. Click a pin for a quick popover with the type, place and local time."
        />
        <FeatureItem
          title="Paths"
          description="Journeys (flights, trains, taxis) are drawn as a line between their two ends; single venues (hotels, dining) are a single pin."
        />
        <SectionTitle>Selecting an item</SectionTitle>
        <FeatureItem
          title="List ↔ map, both ways"
          description="Click a row in the list or an item on the map and it highlights in both. A journey zooms to its whole path; a venue centres on its point. The row expands to its details — the full flight card for flights, the address / operator / reservation for everything else."
        />
        <SectionTitle>The global Tracker</SectionTitle>
        <FeatureItem
          title="From / To dates"
          description="The Tracker shows plans whose timing falls in the From–To window. Adjust the date pickers to look further back or ahead."
        />
        <FeatureItem
          title="Tag filter"
          description="Scope the Tracker to a tag to see just that group of trips (e.g. one conference)."
        />
        <FeatureItem
          title="Live flights"
          description="When a flight is airborne and tracking data is available, its pin shows the aircraft's current position, and selecting it draws the flown track over the planned route."
        />
        <HelpTip>
          The trip Map tab and the Tracker behave identically — the Tracker just
          adds the date and tag controls for spanning multiple trips.
        </HelpTip>
      </Box>
    ),
  },
  {
    id: 'sharing',
    label: 'Sharing & privacy',
    Icon: PeopleOutlineIcon,
    body: (
      <Box>
        <Body>
          Sharing works at two levels: who is on the <strong>trip</strong>, and who can
          see each individual <strong>plan</strong>.
        </Body>
        <SectionTitle>Trip roles (Share trip)</SectionTitle>
        <FeatureItem
          title="Owner"
          description="The person who created the trip. Can do everything, including delete it. The owner role can't be reassigned or removed."
        />
        <FeatureItem
          title="Editor"
          description="Can add and edit plans and manage who else is on the trip."
        />
        <FeatureItem
          title="Viewer"
          description="Can see the trip and its plans, but can't change anything."
        />
        <Body>You can only add people who are already your friends.</Body>
        <SectionTitle>Per-plan privacy</SectionTitle>
        <FeatureItem
          title="Who can see this plan?"
          description="On a plan's Privacy & passengers dialog, choose Everyone on the trip (default), Hidden from… (everyone except the people you pick), or Only visible to… (just the people you pick)."
        />
        <FeatureItem
          title="Passengers"
          description="People on a plan (e.g. fellow flyers). Adding a passenger also grants them viewer access to the whole trip, and they can always see that plan."
        />
        <HelpTip>
          Trip roles control the whole trip; per-plan privacy is a finer control on
          top — use it to keep a surprise dinner hidden from one traveller while the
          rest of the trip stays shared.
        </HelpTip>
      </Box>
    ),
  },
];

const PAGE_IDS = new Set(HELP_PAGES.map((p) => p.id));

/** Map a context hint (from the help button's current screen, or a dialog's
 * deep link) to a topic page id, defaulting to the overview. */
export function contextToPageId(context: string | null | undefined): string {
  switch (context) {
    case 'trip':
    case 'plans':
      return 'plans';
    case 'sharing':
    case 'privacy':
      return 'sharing';
    case 'tracker':
    case 'map':
      return 'tracker';
    case 'trips':
      return 'trips';
    default:
      // An exact page id passes through; anything else falls to the overview.
      return context && PAGE_IDS.has(context) ? context : 'overview';
  }
}
