import type { Plan, PlanPart } from '../api/types';
import { fmtPartPlaces, fmtPartTimeRange, planTypeLabel } from './trip-format';
import { formatCost } from './format';
import { fmtGate } from './gate';

/** Build the plain-text summary of a plan that gets copied to the clipboard or
 * handed to the native share sheet. It mirrors what a tile shows at a glance —
 * title, place(s), when, and the booking details someone would actually want to
 * paste into a message — one field per line so it reads cleanly in any app. */
export function buildPlanShareText(plan: Plan, part: PlanPart): string {
  const title = plan.title || planTypeLabel(part.type);
  const lines: string[] = [title];

  const places = fmtPartPlaces(part.type, part.start_label, part.end_label);
  if (places && places !== title) lines.push(places);

  const when = fmtPartTimeRange(part);
  if (when) lines.push(when);

  const addr = fmtPartPlaces(part.type, part.start_address, part.end_address);
  if (addr && addr !== places) lines.push(addr);

  if (part.type === 'flight' && part.flight?.ident) lines.push(`Flight: ${part.flight.ident}`);
  if (part.type === 'flight' && part.flight) {
    lines.push(`Departure gate: ${fmtGate(part.flight.origin_terminal, part.flight.origin_gate)}`);
  }
  if (plan.ticket_number) lines.push(`Ticket: ${plan.ticket_number}`);
  if (plan.confirmation_ref) lines.push(`Ref: ${plan.confirmation_ref}`);
  if (plan.supplier_name) lines.push(`Supplier: ${plan.supplier_name}`);

  const cost = formatCost(plan.cost_amount, plan.cost_currency);
  if (cost) lines.push(`Cost: ${cost}`);

  if (plan.notes) lines.push('', plan.notes.trim());

  return lines.join('\n');
}

/** True when running as an installed PWA (standalone display mode, or iOS's
 * legacy navigator.standalone). Mirrors the check in pwa.ts / push.ts — kept
 * local so this module stays free of pwa.ts's side effects. */
function isStandalone(): boolean {
  return (
    window.matchMedia('(display-mode: standalone)').matches ||
    (navigator as { standalone?: boolean }).standalone === true
  );
}

/** True when we should offer the native share sheet rather than a plain
 * clipboard copy: only when installed as a PWA (per the product intent that the
 * browser tab copies and the installed app shares) and the platform actually
 * provides navigator.share. Drives both the button icon and sharePlan's route. */
export function canShareNatively(): boolean {
  return isStandalone() && typeof navigator.share === 'function';
}

export type ShareOutcome = 'shared' | 'copied' | 'cancelled';

/** Share a plan: open the native share sheet when installed as a PWA, otherwise
 * copy the text to the clipboard. Returns which path ran so the caller can give
 * the right feedback ('cancelled' when the user dismisses the share sheet). */
export async function sharePlan(text: string, title: string): Promise<ShareOutcome> {
  if (canShareNatively()) {
    try {
      await navigator.share({ title, text });
      return 'shared';
    } catch (err) {
      // The user dismissing the sheet rejects with an AbortError — that's not a
      // failure, so swallow it and report the cancellation rather than falling
      // back to a surprise clipboard write.
      if (err instanceof DOMException && err.name === 'AbortError') return 'cancelled';
      throw err;
    }
  }
  await navigator.clipboard.writeText(text);
  return 'copied';
}
