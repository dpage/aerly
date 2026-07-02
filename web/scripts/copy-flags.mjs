// Copies the flag SVGs we serve locally out of the flag-icons package into
// public/flags/, so the built SPA carries its own flags (embedded in the Go
// binary via //go:embed) instead of hot-linking flagcdn.com at runtime. That
// third-party dependency was intermittently failing to connect, leaving cards
// flagless and spamming the console; same-origin assets share the app's own
// connection and can't blip independently.
//
// flag-icons ships public-domain flag artwork; we take the 4x3 set (the card's
// flag is rendered with object-fit: cover, so the fixed aspect is fine). Only
// two-letter ISO 3166-1 alpha-2 codes are copied, because flagUrl() only ever
// requests those — the package's extra entries (eu, un, gb-eng, …) would never
// be fetched. Generated output; see .gitignore.
import { mkdirSync, readdirSync, copyFileSync, rmSync } from 'node:fs';
import { dirname, join } from 'node:path';
import { fileURLToPath } from 'node:url';

const here = dirname(fileURLToPath(import.meta.url));
const src = join(here, '..', 'node_modules', 'flag-icons', 'flags', '4x3');
const dest = join(here, '..', 'public', 'flags');

const isoAlpha2 = /^[a-z]{2}\.svg$/;

rmSync(dest, { recursive: true, force: true });
mkdirSync(dest, { recursive: true });

let n = 0;
for (const name of readdirSync(src)) {
  if (!isoAlpha2.test(name)) continue;
  copyFileSync(join(src, name), join(dest, name));
  n++;
}
console.log(`copy-flags: ${n} flags -> public/flags/`);
