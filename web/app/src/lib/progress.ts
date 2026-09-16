import { cumulative, haversine, nearestOnLine, type Coord } from './geo';
import type { LegMode, ProfilePoint, RouteAlternative } from './types';

/**
 * What is left of a route from where you actually are.
 *
 * The answer the service gave is a line with legs on it; a GPS fix is a point
 * somewhere near that line. Everything here is the arithmetic between the two,
 * and nothing in it touches the page: the card, the strip and the map all read
 * the same numbers, and a node check can read them too.
 */
export interface Progress {
  /** Metres travelled, measured as the nearest point on the route's own line. */
  along: number;
  /** How far the fix is from that line. */
  distanceToRoute: number;
  offRoute: boolean;
  remainingMeters: number;
  remainingSeconds: number;
  remainingAscent: number;
  /** How you are travelling right here: the mode of the leg you are on. */
  legMode: LegMode;
  arrived: boolean;
}

/** Within this of the end the trip is over, not nearly over. */
const ARRIVED = 30;
/** Further than the fix's own error plus this, and you are not on the route. */
const OFF_ROUTE = 50;

interface Prepared {
  coords: Coord[];
  cum: number[];
  /**
   * The route's own length, and the factor that carries a distance along the
   * drawn line into it. The line is the legs joined and the two lengths agree
   * to a few metres; the factor only keeps rounding from pushing `along` past
   * the last leg.
   */
  total: number;
  scale: number;
  /** Metres, in the route's metric, at which each leg ends. */
  legEnds: number[];
  /** [metres along the route, elevation], joined from the legs when needed. */
  profile: ProfilePoint[];
}

/**
 * Keyed by the route object: a route is replaced, never edited, so a stale
 * entry cannot outlive what it describes. It saves re-walking a few thousand
 * coordinates every second for a line that has not changed.
 */
const prepared = new WeakMap<RouteAlternative, Prepared>();

function prepare(route: RouteAlternative): Prepared {
  const cached = prepared.get(route);
  if (cached) return cached;

  const coords = (route.geometry?.coordinates ?? []) as Coord[];
  const cum = coords.length > 1 ? cumulative(coords) : [0];
  const lineLength = cum[cum.length - 1] ?? 0;

  const legs = route.legs ?? [];
  let legsTotal = 0;
  for (const l of legs) legsTotal += l.meters || 0;
  const total = route.meters > 0 ? route.meters : legsTotal || lineLength;
  const k = legsTotal > 0 ? total / legsTotal : 1;

  const legEnds: number[] = [];
  let acc = 0;
  for (const l of legs) {
    acc += (l.meters || 0) * k;
    legEnds.push(acc);
  }

  // The route carries its profile when it has one; otherwise the legs do, each
  // measured from its own start.
  let profile = (route.profile ?? []) as ProfilePoint[];
  if (profile.length < 2 && legs.length) {
    profile = [];
    let off = 0;
    for (let i = 0; i < legs.length; i++) {
      for (const [m, e] of legs[i].profile ?? []) profile.push([off + m * k, e]);
      off = legEnds[i];
    }
  }

  const out: Prepared = {
    coords,
    cum,
    total,
    scale: lineLength > 0 ? total / lineLength : 0,
    legEnds,
    profile,
  };
  prepared.set(route, out);
  return out;
}

/**
 * The climb still ahead, read off the samples past `along`. The first delta is
 * measured from the elevation interpolated at `along` itself, so standing
 * halfway up a ramp does not count the whole ramp again.
 */
function profileAscent(profile: ProfilePoint[], along: number): number | null {
  if (profile.length < 2) return null;
  let i = 0;
  while (i < profile.length && profile[i][0] <= along) i++;
  if (i >= profile.length) return 0;
  let prev: number;
  if (i === 0) prev = profile[0][1];
  else {
    const a = profile[i - 1];
    const b = profile[i];
    const span = b[0] - a[0];
    prev = span > 0 ? a[1] + (b[1] - a[1]) * ((along - a[0]) / span) : a[1];
  }
  let up = 0;
  for (; i < profile.length; i++) {
    const e = profile[i][1];
    if (e > prev) up += e - prev;
    prev = e;
  }
  return up;
}

export function progress(
  route: RouteAlternative,
  pos: { lat: number; lon: number; accuracy: number },
): Progress {
  const p = prepare(route);
  const legs = route.legs ?? [];
  const target: Coord = [pos.lon, pos.lat];

  const near = p.coords.length > 1 ? nearestOnLine(p.coords, p.cum, target) : null;
  const along = near ? Math.min(p.total, Math.max(0, near.along * p.scale)) : 0;
  const distanceToRoute = near
    ? near.metres
    : p.coords.length
      ? haversine(p.coords[0], target)
      : 0;
  const accuracy = isFinite(pos.accuracy) && pos.accuracy > 0 ? pos.accuracy : 0;

  const end = p.coords.length ? p.coords[p.coords.length - 1] : null;
  const toEnd = end ? haversine(end, target) : Infinity;
  const remainingMeters = Math.max(0, p.total - along);

  // The legs are walked once: the one you are on gives the fraction of itself
  // that is left, and everything after it gives all of itself.
  let remainingSeconds = 0;
  let legAscent = 0;
  let legMode: LegMode = legs.length ? legs[legs.length - 1].mode : 'car';
  for (let i = 0; i < legs.length; i++) {
    const start = i ? p.legEnds[i - 1] : 0;
    const finish = p.legEnds[i];
    if (along >= finish) continue;
    const span = finish - start;
    const left = span > 0 ? (finish - Math.max(along, start)) / span : 1;
    if (along >= start) legMode = legs[i].mode;
    remainingSeconds += left * (legs[i].seconds || 0);
    legAscent += left * (legs[i].ascent || 0);
  }
  if (!legs.length) {
    const share = p.total > 0 ? remainingMeters / p.total : 0;
    remainingSeconds = (route.seconds || 0) * share;
    legAscent = (route.ascent || 0) * share;
  }

  const fromProfile = profileAscent(p.profile, along);

  return {
    along,
    distanceToRoute,
    offRoute: distanceToRoute > accuracy + OFF_ROUTE,
    remainingMeters,
    remainingSeconds,
    remainingAscent: fromProfile ?? legAscent,
    legMode,
    arrived: toEnd <= ARRIVED || along >= p.total - ARRIVED,
  };
}
