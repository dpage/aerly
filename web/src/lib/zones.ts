/** All IANA time zone names the runtime knows about, with 'UTC' guaranteed to
 * be present. Computed once, as the list is static for the life of the page. */
function loadZones(): string[] {
  let zones: string[] = [];
  try {
    const supported = (
      Intl as unknown as { supportedValuesOf?: (k: string) => string[] }
    ).supportedValuesOf?.('timeZone');
    if (Array.isArray(supported)) zones = supported;
  } catch {
    zones = [];
  }
  if (!zones.includes('UTC')) zones = ['UTC', ...zones];
  return zones;
}

export const ZONES = loadZones();

/** The IANA spelling of a typed zone, or null when it is not one. The browser
 * accepts any case ('europe/london') but the server's zone database does not,
 * so a zone is saved as listed: from the zone list first, else as the browser
 * resolves it, which covers names the list leaves out. */
export function canonicalZone(text: string): string | null {
  const t = text.trim();
  if (!t) return null;
  const lower = t.toLowerCase();
  const listed = ZONES.find((z) => z.toLowerCase() === lower);
  if (listed) return listed;
  try {
    return new Intl.DateTimeFormat('en-GB', { timeZone: t }).resolvedOptions().timeZone;
  } catch {
    return null;
  }
}
