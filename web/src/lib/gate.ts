/** Human-readable "Terminal X · Gate Y" for a flight endpoint, with graceful
 * fallbacks: gate-only, terminal-only, or "Unknown" when neither is known. */
export function fmtGate(terminal?: string, gate?: string): string {
  const t = terminal?.trim();
  const g = gate?.trim();
  const parts: string[] = [];
  if (t) parts.push(`Terminal ${t}`);
  if (g) parts.push(`Gate ${g}`);
  return parts.length ? parts.join(' · ') : 'Unknown';
}
