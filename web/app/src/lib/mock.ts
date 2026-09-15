/**
 * In-app fixtures for `?mock=1`.
 *
 * They exist so the result surfaces (cards, profile, legs, steps, warnings) can
 * be designed and verified before the routing service answers. Nothing here is
 * reachable without the flag, and the real transport is never routed through it.
 */
import type {
  Favourite,
  FeatureCollection,
  GeocodeResult,
  Health,
  HistoryEntry,
  Leg,
  LegMode,
  Mode,
  ReverseResult,
  RouteAlternative,
  RouteRequest,
  RouteResponse,
} from './types';
import { readLocal, writeLocal } from './storage';
import { cumulative, haversine, type Coord } from './geo';

const delay = <T>(v: T, ms = 240): Promise<T> => new Promise((r) => setTimeout(() => r(v), ms));

const PLACES: GeocodeResult[] = [
  { id: 'p1', name: 'Trento', kind: 'place', locality: 'Trentino', lat: 46.0704, lon: 11.1207 },
  { id: 'p2', name: 'Bolzano', kind: 'place', locality: 'Alto Adige', lat: 46.4983, lon: 11.3548 },
  { id: 'p3', name: 'Rovereto', kind: 'place', locality: 'Vallagarina', lat: 45.8906, lon: 11.04 },
  { id: 'p4', name: 'Riva del Garda', kind: 'place', locality: 'Alto Garda', lat: 45.8856, lon: 10.8412 },
  { id: 'p5', name: 'Molveno', kind: 'place', locality: 'Altopiano della Paganella', lat: 46.143, lon: 10.964 },
  { id: 'p6', name: 'Madonna di Campiglio', kind: 'place', locality: 'Val Rendena', lat: 46.229, lon: 10.827 },
  { id: 'p7', name: 'Merano', kind: 'place', locality: 'Burgraviato', lat: 46.6713, lon: 11.1596 },
  { id: 'p8', name: 'Pergine Valsugana', kind: 'place', locality: 'Valsugana', lat: 46.0637, lon: 11.2372 },
  { id: 'p9', name: 'Levico Terme', kind: 'place', locality: 'Valsugana', lat: 46.0126, lon: 11.3021 },
  { id: 'p10', name: 'Cavalese', kind: 'place', locality: 'Val di Fiemme', lat: 46.2894, lon: 11.4605 },
  { id: 'k1', name: 'Monte Calisio', kind: 'peak', locality: 'Trento', lat: 46.112, lon: 11.15, ele: 1096 },
  { id: 'k2', name: 'Monte Bondone — Cima Verde', kind: 'peak', locality: 'Trento', lat: 46.0189, lon: 11.0424, ele: 2102 },
  { id: 'k3', name: 'Paganella', kind: 'peak', locality: 'Andalo', lat: 46.1447, lon: 11.0333, ele: 2125 },
  { id: 'k4', name: 'Cima Tosa', kind: 'peak', locality: 'Dolomiti di Brenta', lat: 46.1614, lon: 10.8746, ele: 3136 },
  { id: 'k5', name: 'Monte Baldo — Cima Valdritta', kind: 'peak', locality: 'Alto Garda', lat: 45.7386, lon: 10.8497, ele: 2218 },
  { id: 'k6', name: 'Sass Pordoi', kind: 'peak', locality: 'Val di Fassa', lat: 46.4919, lon: 11.8153, ele: 2950 },
  { id: 'h1', name: 'Rifugio Pedrotti alla Tosa', kind: 'hut', locality: 'Dolomiti di Brenta', lat: 46.1633, lon: 10.889, ele: 2491 },
  { id: 'h2', name: 'Rifugio Viote', kind: 'hut', locality: 'Monte Bondone', lat: 46.0242, lon: 11.0345, ele: 1550 },
  { id: 'h3', name: 'Rifugio Croz dell Altissimo', kind: 'hut', locality: 'Molveno', lat: 46.1519, lon: 10.9224, ele: 1430 },
  { id: 'h4', name: 'Rifugio Selvata', kind: 'hut', locality: 'Molveno', lat: 46.1558, lon: 10.9083, ele: 1630 },
  { id: 'a1', name: 'Passo Rolle', kind: 'pass', locality: 'Primiero', lat: 46.297, lon: 11.787, ele: 1980 },
  { id: 'a2', name: 'Passo del Tonale', kind: 'pass', locality: 'Val di Sole', lat: 46.2588, lon: 10.5852, ele: 1883 },
  { id: 'a3', name: 'Passo Sella', kind: 'pass', locality: 'Val di Fassa', lat: 46.5122, lon: 11.7607, ele: 2244 },
  { id: 's1', name: 'Via Gocciadoro', kind: 'street', locality: 'Trento', lat: 46.0602, lon: 11.1305 },
  { id: 's2', name: 'Lungadige Monte Grappa', kind: 'street', locality: 'Trento', lat: 46.0668, lon: 11.1141 },
  { id: 't1', name: 'Sentiero SAT 401', kind: 'trail', locality: 'Monte Calisio', lat: 46.0932, lon: 11.1411 },
  { id: 't2', name: 'Sentiero SAT 340', kind: 'trail', locality: 'Dolomiti di Brenta', lat: 46.1541, lon: 10.9031 },
];

const fold = (s: string) =>
  s
    .toLowerCase()
    .normalize('NFD')
    .replace(/[̀-ͯ]/g, '');

export function health(): Promise<Health> {
  return delay({ ok: true, region: 'Trentino-Alto Adige', graph: { junctions: 0, stretches: 0, withElevation: 0 } }, 80);
}

export function geocode(q: string, limit: number): Promise<GeocodeResult[]> {
  const needle = fold(q.trim());
  if (!needle) return delay([], 60);
  const hits = PLACES.filter((p) => fold(p.name).includes(needle) || fold(p.locality ?? '').includes(needle));
  hits.sort((a, b) => Number(!fold(a.name).startsWith(needle)) - Number(!fold(b.name).startsWith(needle)));
  return delay(hits.slice(0, limit), 160);
}

export function reverse(lat: number, lon: number): Promise<ReverseResult> {
  let best = PLACES[0];
  let bestD = Infinity;
  for (const p of PLACES) {
    const d = haversine([lon, lat], [p.lon, p.lat]);
    if (d < bestD) {
      bestD = d;
      best = p;
    }
  }
  const name = bestD < 1500 ? best.name : `Near ${best.name}`;
  return delay({ name, kind: bestD < 1500 ? best.kind : 'place', lat, lon }, 180);
}

// --- synthetic geometry -----------------------------------------------------

function rng(seed: number) {
  let s = seed >>> 0 || 1;
  return () => {
    s ^= s << 13;
    s ^= s >>> 17;
    s ^= s << 5;
    return ((s >>> 0) % 100000) / 100000;
  };
}

/** A believable road-ish polyline: a bent path with small wander, not a chord. */
function polyline(a: Coord, b: Coord, seed: number, bend: number, steps = 90): Coord[] {
  const r = rng(seed);
  const mx = (a[0] + b[0]) / 2;
  const my = (a[1] + b[1]) / 2;
  const dx = b[0] - a[0];
  const dy = b[1] - a[1];
  const cx = mx - dy * bend;
  const cy = my + dx * bend;
  const out: Coord[] = [];
  for (let i = 0; i <= steps; i++) {
    const t = i / steps;
    const u = 1 - t;
    const x = u * u * a[0] + 2 * u * t * cx + t * t * b[0];
    const y = u * u * a[1] + 2 * u * t * cy + t * t * b[1];
    const wander = Math.sin(t * 14 + seed) * 0.0016 + (r() - 0.5) * 0.0011;
    out.push([x + wander * (dy === 0 ? 1 : dy) * 8, y + wander * (dx === 0 ? 1 : dx) * 8]);
  }
  out[0] = a;
  out[out.length - 1] = b;
  return out;
}

/** Elevation of a named place, or a plausible valley height for anywhere else. */
function eleAt(p: Coord): number {
  let best: GeocodeResult | null = null;
  let bestD = Infinity;
  for (const q of PLACES) {
    if (!q.ele) continue;
    const d = haversine(p, [q.lon, q.lat]);
    if (d < bestD) {
      bestD = d;
      best = q;
    }
  }
  if (best && bestD < 700) return best.ele as number;
  return 200 + Math.abs(Math.sin(p[0] * 9.3) * Math.cos(p[1] * 7.1)) * 260;
}

/** A profile that climbs the way a valley-to-summit walk climbs. */
function profileFor(coords: Coord[], cum: number[], e0: number, e1: number, seed: number): [number, number][] {
  const total = cum[cum.length - 1] || 1;
  const r = rng(seed * 7 + 11);
  const rough = 18 + r() * 26;
  return coords.map((_, i) => {
    const t = cum[i] / total;
    // Ease the climb: gentle at the bottom, steeper in the middle, flat at the top.
    const s = t * t * (3 - 2 * t);
    const undulation =
      Math.sin(t * 9 + seed) * rough + Math.sin(t * 23 + seed * 0.7) * (rough * 0.4);
    return [Math.round(cum[i]), Math.round(e0 + (e1 - e0) * s + undulation)] as [number, number];
  });
}

const SPEED: Record<LegMode, number> = { car: 15.3, bike: 4.6, hike: 1.15, lift: 5.0 }; // m/s on the flat

function buildLeg(mode: LegMode, coords: Coord[], seed: number, grade?: string): Leg {
  const cum = cumulative(coords);
  const meters = cum[cum.length - 1];
  const profile = profileFor(coords, cum, eleAt(coords[0]), eleAt(coords[coords.length - 1]), seed);
  let ascent = 0;
  let descent = 0;
  for (let i = 1; i < profile.length; i++) {
    const d = profile[i][1] - profile[i - 1][1];
    if (d > 0) ascent += d;
    else descent -= d;
  }
  // Walking follows the Alpine clubs' rule: horizontal and vertical added, the
  // smaller of the two halved. Wheels just lose a little time to the climb.
  let seconds: number;
  if (mode === 'hike') {
    const flat = meters / SPEED.hike;
    const up = (ascent / 400) * 3600;
    seconds = Math.max(flat, up) + Math.min(flat, up) / 2;
  } else {
    seconds = meters / SPEED[mode] + (ascent / 100) * (mode === 'bike' ? 42 : 4);
  }
  const classes: Record<string, number> =
    mode === 'car'
      ? { secondary: meters * 0.52, tertiary: meters * 0.31, residential: meters * 0.17 }
      : mode === 'bike'
        ? { cycleway: meters * 0.68, residential: meters * 0.19, tertiary: meters * 0.13 }
        : { trail: meters * 0.71, path: meters * 0.21, track: meters * 0.08 };
  return {
    mode,
    seconds: Math.round(seconds),
    meters: Math.round(meters),
    ascent: Math.round(ascent),
    descent: Math.round(descent),
    grade: mode === 'hike' ? ((grade as Leg['grade']) ?? 'E') : undefined,
    classes,
    geometry: { type: 'LineString', coordinates: coords },
    profile,
  };
}

const STEP_NAMES: Record<LegMode, string[]> = {
  lift: ['Cabinovia Peio', 'Seggiovia Doss dei Cembri'],
  car: ['Via Gocciadoro', 'SS12 del Brennero', 'SP85 della Panarotta', 'Via Brennero', 'SS45bis Gardesana'],
  bike: ['Ciclabile della Valsugana', 'Via Sanseverino', 'Ciclabile dell Adige', 'Via Maccani', 'Ciclabile del Garda'],
  hike: ['trail 401', 'trail 340', 'Sentiero delle Vaneze', 'trail 305', 'trail SAT 618'],
};

function buildAlternative(id: string, points: Coord[], mode: Mode, grade: string, variant: number): RouteAlternative {
  const seed = 17 + variant * 131;
  const wheels: LegMode = mode.startsWith('bike') ? 'bike' : 'car';
  const legs: Leg[] = [];
  for (let i = 0; i < points.length - 1; i++) {
    const a = points[i];
    const b = points[i + 1];
    const bend = (variant - 1) * 0.11 + 0.06;
    if (mode === 'car+hike' || mode === 'bike+hike') {
      // One switch at a trailhead: the wheels stop where the walking starts.
      const whole = polyline(a, b, seed + i, bend, 120);
      const cut = Math.floor(whole.length * 0.68);
      legs.push(buildLeg(wheels, whole.slice(0, cut + 1), seed + i));
      legs.push(buildLeg('hike', whole.slice(cut), seed + i + 7, grade));
    } else {
      legs.push(buildLeg(mode as LegMode, polyline(a, b, seed + i, bend, 120), seed + i, grade));
    }
  }
  const coords: Coord[] = [];
  for (const l of legs) for (const c of l.geometry.coordinates) coords.push(c as Coord);
  const cum = cumulative(coords);
  const profile: [number, number][] = [];
  let off = 0;
  for (const l of legs) {
    for (const [m, e] of l.profile ?? []) profile.push([Math.round(off + m), e]);
    off += l.meters;
  }
  const steps = legs.flatMap((l, li) => {
    const names = STEP_NAMES[l.mode];
    const n = Math.min(4, Math.max(2, Math.round(l.meters / 3500)));
    return Array.from({ length: n }, (_, i) => ({
      name: names[(li + i) % names.length],
      mode: l.mode,
      meters: Math.round(l.meters / n),
      seconds: Math.round(l.seconds / n),
    }));
  });
  const hikeLegs = legs.filter((l) => l.mode === 'hike');
  const warnings: string[] = [];
  if (hikeLegs.some((l) => (l.ascent ?? 0) > 900)) warnings.push('Over 900 m of climbing on foot: start early.');
  if (variant === 2) warnings.push('One section follows a road without a shoulder for 1.4 km.');
  if (grade === 'EEA') warnings.push('A via ferrata section needs harness, helmet and set.');
  return {
    id,
    seconds: legs.reduce((a, l) => a + l.seconds, 0),
    meters: Math.round(cum[cum.length - 1]),
    ascent: legs.reduce((a, l) => a + l.ascent, 0),
    descent: legs.reduce((a, l) => a + l.descent, 0),
    grade: hikeLegs.length ? (grade as RouteAlternative['grade']) : undefined,
    legs,
    geometry: { type: 'LineString', coordinates: coords },
    profile,
    steps,
    warnings,
  };
}

/** The same shape the service returns, distance included. */
function snapOf(req: RouteRequest) {
  return req.points.map((p) => {
    let best = PLACES[0];
    let bestD = Infinity;
    for (const q of PLACES) {
      const d = haversine([p.lon, p.lat], [q.lon, q.lat]);
      if (d < bestD) {
        bestD = d;
        best = q;
      }
    }
    // Near a known place it lands on its doorstep; elsewhere it has to walk.
    const distance = bestD < 2000 ? Math.round(6 + (bestD % 30)) : Math.round(Math.min(bestD, 900));
    return { lat: p.lat, lon: p.lon, name: bestD < 2000 ? best.name : 'the nearest road', distance };
  });
}

export function route(req: RouteRequest): Promise<RouteResponse> {
  const points: Coord[] = req.points.map((p) => [p.lon, p.lat]);
  if (points.length < 2) return delay({ routes: [], reason: 'Two points are needed.' }, 200);
  // A deliberate dead end to exercise the no-route state: T up to a 3,000 m peak.
  const tooHard =
    req.grade === 'T' &&
    req.points.some((p) => PLACES.some((q) => q.ele && q.ele > 2400 && Math.abs(q.lat - p.lat) < 0.01 && Math.abs(q.lon - p.lon) < 0.01));
  if (tooHard) {
    return delay(
      {
        routes: [],
        reason: `no route at ${req.grade}: the trail to the summit is EEA`,
        neededGrade: 'EEA',
        snapped: snapOf(req),
        computedMs: 210,
      },
      500,
    );
  }
  const n = req.alternatives === 3 ? 3 : 1;
  const routes = Array.from({ length: n }, (_, i) => buildAlternative(`r${i + 1}`, points, req.mode, req.grade, i + 1));
  routes.sort((a, b) => a.seconds - b.seconds);
  const snapped = snapOf(req);
  return delay({ routes, snapped, computedMs: 180 + Math.round(Math.random() * 240) }, 620);
}

// --- stored collections -----------------------------------------------------

const FAV_KEY = 'mock.favourites';
const HIST_KEY = 'mock.history';

export function favourites(): Promise<Favourite[]> {
  return delay(readLocal<Favourite[]>(FAV_KEY, []), 80);
}

export function addFavourite(f: { name: string; lat: number; lon: number; kind?: string }): Promise<Favourite> {
  const list = readLocal<Favourite[]>(FAV_KEY, []);
  const item: Favourite = {
    id: `f${Date.now()}`,
    name: f.name,
    lat: f.lat,
    lon: f.lon,
    kind: f.kind as Favourite['kind'],
    createdAt: new Date().toISOString(),
  };
  writeLocal(FAV_KEY, [item, ...list]);
  return delay(item, 80);
}

export function removeFavourite(id: string): Promise<void> {
  writeLocal(
    FAV_KEY,
    readLocal<Favourite[]>(FAV_KEY, []).filter((f) => f.id !== id),
  );
  return delay(undefined, 60);
}

export function history(): Promise<HistoryEntry[]> {
  return delay(readLocal<HistoryEntry[]>(HIST_KEY, []), 80);
}

export function recordHistory(entry: HistoryEntry): void {
  const list = readLocal<HistoryEntry[]>(HIST_KEY, []);
  writeLocal(HIST_KEY, [entry, ...list].slice(0, 50));
}

export function removeHistory(id: string): Promise<void> {
  writeLocal(
    HIST_KEY,
    readLocal<HistoryEntry[]>(HIST_KEY, []).filter((h) => h.id !== id),
  );
  return delay(undefined, 60);
}

export function clearHistory(): Promise<void> {
  writeLocal(HIST_KEY, []);
  return delay(undefined, 60);
}

export function layer(name: 'region' | 'sat' | 'pois' | 'huts' | 'lifts' | 'crags'): Promise<FeatureCollection> {
  if (name === 'region') {
    const ring = [
      [10.38, 45.67], [11.0, 45.68], [11.62, 45.78], [12.05, 46.02], [12.45, 46.35],
      [12.38, 46.62], [12.18, 46.85], [11.72, 47.01], [11.1, 47.09], [10.68, 46.98],
      [10.38, 46.72], [10.42, 46.4], [10.5, 46.05], [10.38, 45.67],
    ];
    return delay(
      {
        type: 'FeatureCollection',
        features: [
          { type: 'Feature', geometry: { type: 'Polygon', coordinates: [ring] }, properties: { name: 'Trentino-Alto Adige' } },
        ],
      } as FeatureCollection,
      120,
    );
  }
  if (name === 'sat') {
    const grades = ['T', 'E', 'EE', 'EEA'];
    const features = Array.from({ length: 26 }, (_, i) => {
      const r = rng(i * 37 + 3);
      const a: Coord = [10.75 + r() * 1.1, 45.95 + r() * 0.7];
      const b: Coord = [a[0] + (r() - 0.5) * 0.24, a[1] + (r() - 0.4) * 0.2];
      return {
        type: 'Feature' as const,
        geometry: { type: 'LineString', coordinates: polyline(a, b, i * 13 + 5, 0.18, 26) },
        properties: { numero: String(300 + i * 7), grade: grades[i % 4], name: `Sentiero SAT ${300 + i * 7}` },
      };
    });
    return delay({ type: 'FeatureCollection', features } as FeatureCollection, 160);
  }
  const features = PLACES.filter((p) => p.kind !== 'street' && p.kind !== 'trail').map((p) => ({
    type: 'Feature' as const,
    geometry: { type: 'Point', coordinates: [p.lon, p.lat] },
    properties: { name: p.name, kind: p.kind, ele: p.ele ?? null },
  }));
  return delay({ type: 'FeatureCollection', features } as FeatureCollection, 140);
}
