export const STYLE_LIGHT = 'https://tiles.openfreemap.org/styles/liberty';
export const STYLE_DARK = 'https://tiles.openfreemap.org/styles/dark';
export const DEM_TILES = 'https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png';
export const DEM_MAXZOOM = 15;

export const REGION_CENTER: [number, number] = [11.32, 46.28];
export const REGION_BOUNDS: [number, number, number, number] = [10.38, 45.67, 12.48, 47.1];

export interface Tokens {
  ink: string;
  inkInvert: string;
  muted: string;
  faint: string;
  line: string;
  lineStrong: string;
  surface: string;
  bg: string;
  accent: string;
  car: string;
  bike: string;
  hike: string;
  lift: string;
  dest: string;
  gradeT: string;
  gradeE: string;
  gradeEE: string;
  gradeEEA: string;
  gradeA: string;
  mapGround: string;
}

const VARS: Record<keyof Tokens, string> = {
  ink: '--ink',
  inkInvert: '--ink-invert',
  muted: '--muted',
  faint: '--faint',
  line: '--line',
  lineStrong: '--line-strong',
  surface: '--surface',
  bg: '--bg',
  accent: '--accent',
  car: '--car',
  bike: '--bike',
  hike: '--hike',
  lift: '--lift',
  dest: '--dest',
  gradeT: '--grade-t',
  gradeE: '--grade-e',
  gradeEE: '--grade-ee',
  gradeEEA: '--grade-eea',
  gradeA: '--grade-a',
  mapGround: '--map-ground',
};

/** The map reads its colours from the same tokens as the panel: one palette. */
export function readTokens(): Tokens {
  const cs = getComputedStyle(document.documentElement);
  const out = {} as Tokens;
  for (const [key, cssVar] of Object.entries(VARS) as [keyof Tokens, string][]) {
    out[key] = cs.getPropertyValue(cssVar).trim() || '#888888';
  }
  return out;
}

export function modeColor(t: Tokens, mode: string): string {
  return mode === 'bike' ? t.bike : mode === 'hike' ? t.hike : t.car;
}

export function gradeColor(t: Tokens, grade: string): string {
  if (grade === 'T') return t.gradeT;
  if (grade === 'EE') return t.gradeEE;
  if (grade === 'EEA') return t.gradeEEA;
  if (grade === 'A') return t.gradeA;
  return t.gradeE;
}

/**
 * Fetch a style and resolve its paths against the origin it is served from.
 *
 * The self-hosted style names its tiles, glyphs and sprite by path — which is
 * what makes the same file work on localhost, on a staging box and behind
 * Caddy at the real domain. MapLibre will not take a relative sprite URL: it
 * rejects the style outright before `transformRequest` is ever consulted. So
 * the paths are made absolute here, against wherever this app is being served
 * from, and MapLibre is handed the finished object.
 *
 * Nothing about the style's content is touched.
 */
export async function loadStyleSpec(url: string): Promise<unknown> {
  if (!url.startsWith('/')) return url; // a full URL: hand it over untouched
  const res = await fetch(url, { headers: { accept: 'application/json' } });
  if (!res.ok) throw new Error(`style ${url}: ${res.status}`);
  const style = (await res.json()) as Record<string, unknown>;
  // Plain concatenation, not `new URL`: the glyph path carries {fontstack}
  // and {range} placeholders that URL parsing would percent-encode.
  const abs = (p: string) => (p.startsWith('/') ? location.origin + p : p);

  if (typeof style.sprite === 'string') style.sprite = abs(style.sprite);
  else if (Array.isArray(style.sprite)) {
    style.sprite = (style.sprite as { id: string; url: string }[]).map((s) => ({ ...s, url: abs(s.url) }));
  }
  if (typeof style.glyphs === 'string') style.glyphs = abs(style.glyphs);

  for (const src of Object.values((style.sources ?? {}) as Record<string, Record<string, unknown>>)) {
    if (typeof src.url === 'string') src.url = abs(src.url);
    if (Array.isArray(src.tiles)) src.tiles = (src.tiles as string[]).map(abs);
  }
  return style;
}
