import type { AvoidPoint, Grade, Mode, Waypoint } from './types';
import { round6 } from './geo';
import { shareOrigin } from './config';
import { MAX_AVOIDS, MAX_POINTS, MAX_SEQUENCE } from './limits';

const MODES: Mode[] = ['car', 'bike', 'hike', 'car+hike', 'bike+hike'];
const GRADES: Grade[] = ['T', 'E', 'EE', 'EEA', 'A'];

export interface SharedQuery {
  points: Waypoint[];
  mode: Mode;
  grade: Grade;
  alternatives: 1 | 3;
  lifts?: boolean;
  avoid?: AvoidPoint[];
}

/**
 * `?p=46.07,11.12,Trento;46.11,11.15,Monte%20Calisio&m=hike&g=E&a=3`
 *
 * Built by hand rather than through URLSearchParams, which would percent-encode
 * the already-encoded names a second time and put `Monte%2520Calisio` in
 * someone's chat window. encodeURIComponent escapes `,` and `;`, so the
 * separators survive a name that contains them.
 */
export function encodeQuery(q: SharedQuery): string {
  // A point is lat,lon,name and — for a via — a fourth field. Names are
  // percent-encoded, so no comma of theirs can reach the separator.
  const p = q.points
    .map((w) => {
      const base = [round6(w.lat), round6(w.lon), encodeURIComponent(w.name ?? '')];
      if (w.via) base.push('1');
      return base.join(',');
    })
    .join(';');
  const parts = [`p=${p}`, `m=${encodeURIComponent(q.mode)}`, `g=${q.grade}`];
  if (q.alternatives === 3) parts.push('a=3');
  if (q.lifts) parts.push('l=1');
  if (q.avoid?.length) {
    parts.push(`x=${q.avoid.map((a) => `${round6(a.lat)},${round6(a.lon)}`).join(';')}`);
  }
  return `?${parts.join('&')}`;
}

/**
 * A link holds what the planner does: six places, twelve entries with the
 * vias. Counting the vias as places cut a ten-point walk at its sixth entry and
 * made that the destination. A link the page wrote always fits; one edited
 * past the limits loses entries before its destination, not the destination.
 */
function fit(points: Waypoint[]): Waypoint[] {
  const last = points[points.length - 1];
  const out: Waypoint[] = [];
  let places = last.via ? 0 : 1;
  for (const p of points.slice(0, -1)) {
    if (out.length + 1 >= MAX_SEQUENCE) break;
    if (!p.via && places++ >= MAX_POINTS) break;
    out.push(p);
  }
  out.push(last);
  return out;
}

export function decodeQuery(search: string): SharedQuery | null {
  let u: URLSearchParams;
  try {
    u = new URLSearchParams(search);
  } catch {
    return null;
  }
  const raw = u.get('p');
  if (!raw) return null;
  const points: Waypoint[] = [];
  for (const chunk of raw.split(';')) {
    const parts = chunk.split(',');
    const lat = Number(parts[0]);
    const lon = Number(parts[1]);
    if (!isFinite(lat) || !isFinite(lon) || Math.abs(lat) > 90 || Math.abs(lon) > 180) continue;
    // URLSearchParams has already decoded the value once; decoding again here
    // is what made the old links need double encoding to survive.
    const name = parts[2] || undefined;
    const via = parts[3] === '1';
    points.push(via ? { lat, lon, name, via: true } : { lat, lon, name });
  }
  if (points.length < 2) return null;
  const m = u.get('m') as Mode | null;
  const g = u.get('g') as Grade | null;
  return {
    points: fit(points),
    mode: m && MODES.includes(m) ? m : 'car',
    grade: g && GRADES.includes(g) ? g : 'E',
    alternatives: u.get('a') === '3' ? 3 : 1,
    lifts: u.get('l') === '1',
    avoid: (u.get('x') ?? '')
      .split(';')
      .map((chunk) => chunk.split(','))
      .map(([a, b]) => ({ lat: Number(a), lon: Number(b) }))
      .filter((a) => isFinite(a.lat) && isFinite(a.lon) && Math.abs(a.lat) <= 90)
      .slice(0, MAX_AVOIDS),
  };
}

/** The address bar always shows the query on screen, without a navigation. */
export function replaceUrl(q: SharedQuery): void {
  try {
    const keep = new URLSearchParams(location.search).get('mock');
    const s = encodeQuery(q) + (keep ? `&mock=${keep}` : '');
    history.replaceState(null, '', location.pathname + s);
  } catch {
    /* ignored */
  }
}

export function shareUrl(q: SharedQuery): string {
  // The address someone else can open, which on a deployed box is not the
  // address this tab happens to be on.
  return shareOrigin() + location.pathname + encodeQuery(q);
}
