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
import NotificationsOutlinedIcon from '@mui/icons-material/NotificationsOutlined';
import SettingsOutlinedIcon from '@mui/icons-material/SettingsOutlined';

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
          Aerly keeps a trip&apos;s travel in one place. Add flights, hotels, trains, taxis, dinners
          and excursions to a trip, and they appear on a shared timeline and map — with live flight
          tracking where available.
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
          description="Invite friends as editors or viewers, and fine-tune who sees what per plan. See Friends & sharing."
        />
        <HelpTip>
          The Map tab and the global Tracker show every mappable plan in time order — click an item
          in the list or on the map to highlight it and see its details.
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
          A trip is the container for everything else. Create one from the Trips list with{' '}
          <strong>New trip</strong> — give it a name, and optionally a destination and dates.
          Your own trips live under <strong>My trips</strong> (along with any you&apos;re a
          passenger on); trips a friend has shared with you appear under <strong>Friends&apos;
          trips</strong>.
        </Body>
        <SectionTitle>Dates</SectionTitle>
        <FeatureItem
          title="Optional, and auto-inferred"
          description="Leave dates blank and Aerly shows an inferred span (marked with ~) from the plans inside. If a plan falls outside dates you have set, the trip flags it so you can check."
        />
        <SectionTitle>Importing</SectionTitle>
        <FeatureItem
          title="Import a TripIt or Kayak .ics"
          description="Use Import .ics on the Trips list to turn a TripIt or Kayak calendar export into trips — Aerly creates them and adds the plans for you. A Kayak account feed can hold several trips and imports them all at once. Re-importing the same export updates the existing trips rather than duplicating them."
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
          A plan is a single booking — a flight, hotel, train, taxi, meal or excursion. Open a trip
          and click <strong>New plan</strong> to add one.
        </Body>
        <SectionTitle>Four ways to capture a plan</SectionTitle>
        <FeatureItem title="Manual" description="Fill in the details yourself." />
        <FeatureItem
          title="Paste text"
          description="Paste a confirmation email or itinerary and Aerly extracts the plan, flagging anything it isn't sure about for you to confirm."
        />
        <FeatureItem
          title="Upload"
          description="Upload a booking PDF or text file to extract from."
        />
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
        <SectionTitle>Combine or split bookings</SectionTitle>
        <FeatureItem
          title="Link bookings"
          description="When two or more flights, trains or transfers are really one booking, editors can use Link bookings on the Timeline to select them and fold them into a single multi-part plan."
        />
        <FeatureItem
          title="Split out"
          description="A multi-leg booking's Edit dialog offers Split out on each leg, to pull a leg back into its own separate plan."
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
          Aerly plots every plan that has a location on a map, with a time-ordered list beside it.
          You&apos;ll find it in two places: a trip&apos;s <strong>Map</strong> tab (that trip), and
          the global <strong>Tracker</strong> (across all your trips, by date).
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
          title="Mine only"
          description="Toggle Mine only to hide everyone else's plans and show just your own."
        />
        <FeatureItem
          title="Show / hide types"
          description="Tap the coloured type chips (flights, hotels, trains, taxis, dining, excursions) to show or hide each kind of plan on the map."
        />
        <FeatureItem
          title="Live flights"
          description="When a flight is airborne and tracking data is available, its pin shows the aircraft's current position, and selecting it draws the flown track over the planned route."
        />
        <HelpTip>
          The trip Map tab and the Tracker behave identically — the Tracker just adds the date and
          tag controls for spanning multiple trips.
        </HelpTip>
      </Box>
    ),
  },
  {
    id: 'sharing',
    label: 'Friends & sharing',
    Icon: PeopleOutlineIcon,
    body: (
      <Box>
        <Body>
          You share trips with <strong>friends</strong>. Sharing then works at two levels: who is on
          the <strong>trip</strong>, and who can see each individual <strong>plan</strong>.
        </Body>
        <SectionTitle>Friends</SectionTitle>
        <FeatureItem
          title="Add a friend"
          description="Open Friends from the account menu and invite someone by email. If they're already on Aerly they get a friend request; otherwise they're emailed an invitation to join."
        />
        <FeatureItem
          title="Requests & the badge"
          description="Incoming requests appear in the same dialog to accept or decline — a badge on your avatar flags any that are pending."
        />
        <FeatureItem
          title="Unfriend"
          description="Removing a friend also revokes the flight access the two of you had shared with each other."
        />
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
        <FeatureItem
          title="Passenger"
          description="Someone travelling on the whole trip (e.g. your partner). They become a passenger on every plan — existing and future — so they see all of it (except plans hidden from them), and it shows up under their own My trips."
        />
        <Body>You can only add people who are already your friends.</Body>
        <FeatureItem
          title="Always share with"
          description="In Preferences → Sharing, set a list of friends who are added to every new trip you create automatically — e.g. your partner as a viewer and your assistant as an editor. It applies only to trips you create from then on; trips you've already shared are left as they are."
        />
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
          Trip roles control the whole trip; per-plan privacy is a finer control on top — use it to
          keep a surprise dinner hidden from one traveller while the rest of the trip stays shared.
        </HelpTip>
      </Box>
    ),
  },
  {
    id: 'alerts',
    label: 'Alerts & reminders',
    Icon: NotificationsOutlinedIcon,
    body: (
      <Box>
        <Body>
          Aerly tells you when a flight changes, and can remind you before a plan. Everything
          collects in the <strong>Alerts</strong> inbox in the account menu.
        </Body>
        <SectionTitle>Alerts inbox</SectionTitle>
        <FeatureItem
          title="What lands here"
          description="Flight changes (delays, gate or terminal changes, cancellations), reminders, and notifications that a trip has been shared with you. A badge on your avatar shows the unread count."
        />
        <FeatureItem
          title="Open, delete, clear"
          description="Open an alert to jump straight to the affected flight or trip. Delete items individually or use Clear all; opening the inbox marks everything read."
        />
        <SectionTitle>Alert preferences</SectionTitle>
        <FeatureItem
          title="How you're notified"
          description="In Preferences → Alerts choose in-app, email, or both — and set a minimum delay so short hiccups below that many minutes don't alert you."
        />
        <FeatureItem
          title="Notify me of changes"
          description="As a trip viewer you don't get a plan's flight alerts by default. Open the plan and turn on Notify me of changes for the ones you want to follow. (Owners and editors are alerted via their own preferences.)"
        />
        <SectionTitle>Reminders</SectionTitle>
        <FeatureItem
          title="Per trip"
          description="Turn on Email me reminders on a trip and set a lead time in hours to be reminded before every plan you can see on it."
        />
        <FeatureItem
          title="Per plan"
          description="A plan's Reminder control overrides the trip setting — change its lead time, or opt a single plan in or out."
        />
        <HelpTip>
          Reminders are about timing (a heads-up before you travel); the flight alerts above fire
          whenever the airline reports a change. They&apos;re independent — set each to taste.
        </HelpTip>
      </Box>
    ),
  },
  {
    id: 'account',
    label: 'Your account',
    Icon: SettingsOutlinedIcon,
    body: (
      <Box>
        <Body>
          These all live in the account menu under your avatar (top-right). Most of your settings
          are gathered together under Preferences, a tabbed dialog covering alert delivery, sharing
          defaults, your home address and forwarding emails.
        </Body>
        <SectionTitle>Statistics</SectionTitle>
        <FeatureItem
          title="Your flying, totted up"
          description="Flown and upcoming totals — flights, distance, time in the air and airports — plus highlights like your longest flight, most-visited airport and laps of the Earth."
        />
        <SectionTitle>Subscribe to calendar</SectionTitle>
        <FeatureItem
          title="Private iCal feeds"
          description="Get a private subscription link — your whole schedule, a single trip, or one plan — to add to Apple Calendar, Google Calendar or Outlook. It always shows exactly what you can see in the app. Regenerate the link to revoke the old one."
        />
        <SectionTitle>Preferences</SectionTitle>
        <FeatureItem
          title="Home address"
          description="On the Home tab, set your home address once so plans captured from text (e.g. “taxi from home to the airport”) know where home is. It's only ever visible to you."
        />
        <FeatureItem
          title="Email addresses"
          description="If your Aerly is set up to add flights from forwarded booking emails, use the Emails tab to add and verify the addresses you can forward from. (Hidden when forwarding isn't enabled.)"
        />
        <HelpTip>
          Preferences also holds alert delivery (see Flight alerts) and the “Always share with”
          sharing defaults (see Friends &amp; sharing) — each on its own tab. Everything saves as you
          go, so there's no Save button.
        </HelpTip>
        <SectionTitle>Appearance & sessions</SectionTitle>
        <FeatureItem
          title="Theme and signing out"
          description="Switch between Light, Dark and System appearance. Sign out ends this session; Sign out everywhere ends every active session on all your devices."
        />
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
