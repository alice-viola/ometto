export type Coord = [number, number]; // [lon, lat]

const R = 6371008.8;
const rad = (d: number) => (d * Math.PI) / 180;

export function haversine(a: Coord, b: Coord): number {
  const dLat = rad(b[1] - a[1]);
  const dLon = rad(a[0] - b[0]);
  const la1 = rad(a[1]);
  const la2 = rad(b[1]);
  const h =
    Math.sin(dLat / 2) ** 2 + Math.cos(la1) * Math.cos(la2) * Math.sin(dLon / 2) ** 2;
  return 2 * R * Math.asin(Math.min(1, Math.sqrt(h)));
}

/** Cumulative distance in metres at every vertex of a line. */
export function cumulative(coords: Coord[]): number[] {
  const out = new Array<number>(coords.length);
  let d = 0;
  out[0] = 0;
  for (let i = 1; i < coords.length; i++) {
    d += haversine(coords[i - 1], coords[i]);
    out[i] = d;
  }
  return out;
}

/** The coordinate at `metres` along a line, interpolated between vertices. */
export function pointAt(coords: Coord[], cum: number[], metres: number): Coord | null {
  if (!coords.length) return null;
  if (metres <= 0) return coords[0];
  const last = cum[cum.length - 1];
  if (metres >= last) return coords[coords.length - 1];
  let lo = 0;
  let hi = cum.length - 1;
  while (lo < hi - 1) {
    const mid = (lo + hi) >> 1;
    if (cum[mid] <= metres) lo = mid;
    else hi = mid;
  }
  const span = cum[hi] - cum[lo] || 1;
  const t = (metres - cum[lo]) / span;
  return [
    coords[lo][0] + (coords[hi][0] - coords[lo][0]) * t,
    coords[lo][1] + (coords[hi][1] - coords[lo][1]) * t,
  ];
}

export type Bounds = [number, number, number, number]; // w, s, e, n

export function boundsOf(coords: Coord[]): Bounds | null {
  if (!coords.length) return null;
  let w = Infinity;
  let s = Infinity;
  let e = -Infinity;
  let n = -Infinity;
  for (const [lon, lat] of coords) {
    if (lon < w) w = lon;
    if (lon > e) e = lon;
    if (lat < s) s = lat;
    if (lat > n) n = lat;
  }
  return [w, s, e, n];
}

export function extendBounds(a: Bounds | null, b: Bounds | null): Bounds | null {
  if (!a) return b;
  if (!b) return a;
  return [Math.min(a[0], b[0]), Math.min(a[1], b[1]), Math.max(a[2], b[2]), Math.max(a[3], b[3])];
}

/** Round-trip safe coordinate text for the share link. */
export function round6(n: number): number {
  return Math.round(n * 1e6) / 1e6;
}

/**
 * The closest point on a polyline to a target, as a distance along that line.
 * Grabbing the drawn route needs to know *where* on the route it was grabbed,
 * which decides between which two request points a via belongs.
 */
export function nearestOnLine(
  coords: Coord[],
  cum: number[],
  target: Coord,
): { along: number; metres: number; point: Coord } | null {
  if (coords.length < 2) return null;
  let best = { along: 0, metres: Infinity, point: coords[0] };
  for (let i = 1; i < coords.length; i++) {
    const a = coords[i - 1];
    const b = coords[i];
    // Flat approximation: over one segment of a drawn route it is exact enough,
    // and the answer only has to pick the right segment.
    const kx = Math.cos((((a[1] + b[1]) / 2) * Math.PI) / 180);
    const ax = a[0] * kx;
    const bx = b[0] * kx;
    const tx = target[0] * kx;
    const dx = bx - ax;
    const dy = b[1] - a[1];
    const len2 = dx * dx + dy * dy;
    const t = len2 ? Math.max(0, Math.min(1, ((tx - ax) * dx + (target[1] - a[1]) * dy) / len2)) : 0;
    const px: Coord = [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t];
    const m = haversine(px, target);
    if (m < best.metres) {
      best = { along: cum[i - 1] + (cum[i] - cum[i - 1]) * t, metres: m, point: px };
    }
  }
  return best;
}

/**
 * The service sends each line once, cut into legs, and nothing at the top of
 * the route: a route used to carry its whole line twice, and a 94 km car+hike
 * with three alternatives no longer fitted the queue's reply. The route's own
 * line is the legs' joined, minus the point every pair of consecutive legs
 * shares. A route that already has one (an older service, the mock) is left
 * alone.
 */
export function joinLegs<R extends { legs?: { geometry?: { coordinates: unknown } }[]; geometry?: { type: string; coordinates: unknown } }>(
  r: R,
): R {
  const own = r.geometry?.coordinates as Coord[] | undefined;
  if (own?.length || !r.legs?.length) return r;
  const coords: Coord[] = [];
  for (const l of r.legs) {
    const c = (l.geometry?.coordinates ?? []) as Coord[];
    for (let i = coords.length && c.length ? 1 : 0; i < c.length; i++) coords.push(c[i]);
  }
  r.geometry = { type: 'LineString', coordinates: coords };
  return r;
}
