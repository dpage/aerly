# PRD: Trips as the core of Aerly

**Status:** Draft for review
**Date:** 2026-05-28
**Audience:** Product / founding team review

---

## 1. Summary

Aerly today is a collaborative flight tracker: people add flights and watch
each other move across a map. This document proposes making **trip planning**
the core of the product, with the **flight tracker becoming a secondary feature**
used immediately before, during, and just after a trip.

The shape we're aiming at is a modern replacement for the planning half of
TripIt — a product that has been badly neglected — combined with the live
tracking and social "who's on their way" experience Aerly already does well.

A user creates a **trip**, optionally shares it with friends, and fills it with
their travel plans: flights, hotels, trains, buses, taxis, day trips, dinners,
and so on. Plans can be added by hand, but the headline experience is **adding
them effortlessly** — forward a confirmation email, upload a ticket or PDF, or
paste a chunk of text (e.g. the message you got back from the local taxi firm
on WhatsApp) and Aerly turns it into a structured plan for you to confirm.

Everything in a trip is shown on a single **vertical timeline**, grouped by day.

---

## 2. Goals

- Make creating and filling a trip the primary thing users do in Aerly.
- Support every common travel plan type, not just flights.
- Make adding plans nearly effortless via email, upload, and paste, in addition
  to manual entry.
- Present a trip as a clean, day-by-day vertical timeline.
- Let users share a trip with friends and plan collaboratively.
- Keep the live flight tracker as a strong secondary feature, reachable both
  from a single flight and as a "who's converging" view before/during an event.

## 3. Non-goals (for this phase)

- Booking or payments. Aerly organizes plans; it does not sell travel.
- Price comparison or trip recommendations.
- A standalone mobile app. (Mobile location sharing is noted as a future
  tracker enhancement, not part of this phase.)
- A formal "event" product with hosts, RSVPs, and attendee management.

---

## 4. Who this is for

- **The organizer / frequent traveller.** Wants one tidy place for an entire
  trip's logistics, built quickly from the confirmations already sitting in
  their inbox.
- **The travelling friend group.** Several people heading to the same place —
  a conference, a wedding, a reunion — each with their own travel, who want to
  see each other's plans and watch each other arrive.
- **The companion traveller.** Someone on a shared trip (partner, family) who
  wants to see the whole plan without having built it.

---

## 5. Core concepts (in plain terms)

- **Trip** — the central object. Has a name, a destination, rough dates, and a
  collection of plans. Everything a user adds lives inside a trip.
- **Plan** — one entry on the timeline. A flight, a hotel stay, a train, a bus,
  a taxi, a day trip, a dinner, or a free-form note. Every plan has a time (or
  time range), a title, a location, and any confirmation details.
- **Timeline** — the day-by-day vertical view of a trip's plans, in order.
- **Tracker** — the live map view. Used right before, during, and after travel
  to watch real movement.
- **Tag** — an optional shared label (e.g. `pgconf-eu-26`) that a group can put
  on their separate trips so they can find and watch each other. Purely opt-in.

---

## 6. What the user sees

### 6.1 Home: your trips

The landing screen is the user's list of trips rather than a map. Trips are
grouped into **Upcoming**, **Happening now**, and **Past**, each shown as a card
with its destination, dates, and the avatars of anyone it's shared with.

A prominent **New trip** action is the main call to action.

### 6.2 Inside a trip: the timeline

Opening a trip shows its **vertical timeline** — the heart of the product. Plans
are listed in chronological order, grouped under sticky day headers, each shown
as a card with:

- An icon for its type (plane, bed, train, bus, taxi, attraction, meal, note).
- Its time or time range, shown in the **local time of where it happens** (so a
  red-eye correctly spans two days).
- A title, location, and any confirmation reference.
- For flights, a link through to the live tracker for that flight.

From within a trip the user can also switch to a **Map** view for that trip,
which plots the trip's plans geographically — but the timeline is the default
and primary view.

### 6.3 Adding a plan

A single **Add to trip** action offers four ways in, all landing in the same
place:

1. **Manual** — pick a type and fill in the details. Adding a flight uses the
   existing flight lookup so the user can often just give a flight number and
   date.
2. **Paste text** — paste any confirmation text (the taxi firm's WhatsApp reply,
   a forwarded itinerary, a hotel email body) and Aerly extracts the plan.
3. **Upload** — drop in a PDF ticket or confirmation and Aerly extracts the plan.
4. **From email** — forward a confirmation to Aerly (the existing email-ingest
   path), and it lands in a trip.

For the paste / upload / email paths, Aerly shows the **extracted plan(s) for
the user to confirm or edit before they're added**. Anything it's unsure about
is flagged rather than silently guessed. This effortless capture is the feature
we expect users to fall in love with.

### 6.4 Sharing a trip

A user can share a trip with friends and choose whether each can view or edit.
Shared trips update live for everyone viewing them — if one person adds the
dinner reservation, it appears on everyone's timeline. Sharing is always
explicit; nothing a user adds is visible to others unless they share it.

### 6.5 The tracker

The live tracker is reachable two ways, and adapts to how it was opened:

- **From a single flight** in a trip → a focused view of that one flight: its
  position, its track, its status.
- **As a "who's on their way" view** → a map showing the live movement of all
  the trackable travel (today, flights) across the trips currently visible to
  the user, within an adjustable time window. When several friends are heading
  to the same place around the same time, they naturally cluster here — this is
  the "watch the race" experience, and the spiritual successor to Aerly's
  original "who's already in the air?" feature.

The time window for the tracker is **user-adjustable** (e.g. from a week before
to a week after now), so people who travel early or stay late to sightsee are
still included. The window is a simple control the user can widen or narrow.

### 6.6 Tags: finding each other without the ceremony

Rather than a formal "event" anyone has to create and own, a group coordinates
through an **optional shared tag**:

- Anyone can put a tag on their own trip — creating a tag is just typing it the
  first time.
- Others in the group add the **same tag** to their own trips to opt in.
- A tag **groups, it never grants access**: tagging your trip never exposes it
  to anyone who couldn't already see it. You only ever see tagged trips that are
  already shared with you. Two unrelated groups can use the same word with no
  overlap.
- When the tracker is viewed for a tag, the default time window automatically
  spans all the tagged trips the viewer can see (with the slider still available
  to widen or narrow). Users who don't use tags simply use the window control
  directly.

Tags are entirely optional. They exist to give a group a lightweight rallying
point — "tag it `pgconf-eu-26`" — without anyone having to host or manage an
event.

---

## 7. Key journeys

**A. Build a trip from my inbox.**
Create "Lisbon, October". Forward the flight confirmation, the hotel email, and
the airport-transfer email. Paste the reply from the local guide about the day
trip. Confirm each extracted plan. The timeline now shows the whole trip,
day by day, without typing it out.

**B. Travel with friends to the same event.**
Each person builds their own trip and tags it `pgconf-eu-26`. Each shares with
the others. In the run-up they can see each other's plans; on travel day they
open the tracker for the tag and watch everyone converge on the city.

**C. Day-of logistics.**
On the morning of departure, open today's plans, see the flight is on time via
the tracker, and have the taxi pickup time and hotel address one scroll away.

---

## 8. Impact on existing users

- Aerly already contains people's flights. On the switch to trips, each user's
  existing flights are gathered into a simple **"Imported flights" trip** per
  user. From there users can move them into real trips they create. We keep this
  migration deliberately simple rather than guessing how past flights should be
  grouped.
- The original social "who's in the air" experience is preserved, reframed as
  the tracker's "who's on their way" view (Section 6.5), scoped by the trips a
  user shares and, optionally, by tags.

---

## 9. Open questions

- **Inbound-leg detection for the tracker.** When watching a tag, should Aerly
  automatically highlight each person's arriving leg, or is the clustered map
  enough? Leaning toward "clustered map is enough" first, with auto-highlight as
  a later refinement.
- **Edit vs. view roles.** Is a simple view/edit split enough for shared trips,
  or do we need finer control (e.g. per-plan privacy within a shared trip)?
- **Tag discovery.** Beyond typing an agreed tag, do we want any in-product
  suggestion of tags already on trips you can see?
- **Plan types at launch.** Which set ships first (flights + hotels + ground +
  activities + dining + notes), and which can wait?

## 10. Possible future directions (not in this phase)

- Mobile app with live location sharing, so the tracker can follow people on the
  ground (e.g. walking from the station), not just while a flight is in the air.
- Tracking for other transport types (train, etc.) feeding the same tracker.
- Richer tags (descriptions, colours) if the lightweight label proves popular.
