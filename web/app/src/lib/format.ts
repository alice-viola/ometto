import type { Grade, LegMode, Mode } from './types';

const THIN = ' '; // thin space, used as the thousands separator

export function groupDigits(n: number): string {
  return Math.round(n).toLocaleString('en-GB').replace(/,/g, THIN);
}

/** "2 h 35 min", "48 min", "< 1 min" — never seconds, never a clock. */
export function fmtDuration(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return '—';
  const total = Math.round(seconds / 60);
  if (total < 1) return '< 1 min';
  const h = Math.floor(total / 60);
  const m = total % 60;
  if (h === 0) return `${m} min`;
  if (m === 0) return `${h} h`;
  return `${h} h ${m} min`;
}

/** Compact form for a card headline: "2:35" reads as a clock, so keep h/min. */
export function fmtDistance(meters: number): string {
  if (!isFinite(meters) || meters < 0) return '—';
  if (meters < 1000) return `${Math.round(meters / 10) * 10} m`;
  const km = meters / 1000;
  if (km < 10) return `${km.toFixed(1)} km`;
  return `${groupDigits(km)} km`;
}

/** Heights stay ungrouped up to five digits: 1096 m reads as a height. */
function height(meters: number): string {
  const n = Math.round(Math.abs(meters));
  return n >= 10000 ? groupDigits(n) : String(n);
}

export function fmtElevation(meters: number): string {
  return `${height(meters)} m`;
}

export function fmtAscent(meters: number): string {
  return `+${height(meters)} m`;
}

export function fmtDescent(meters: number): string {
  return `−${height(meters)} m`;
}

export function fmtDate(at: string | number): string {
  const d = typeof at === 'number' ? new Date(at < 1e12 ? at * 1000 : at) : new Date(at);
  if (isNaN(d.getTime())) return '';
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  const yesterday = new Date(now.getTime() - 86400000).toDateString() === d.toDateString();
  const time = d.toLocaleTimeString('en-GB', { hour: '2-digit', minute: '2-digit' });
  if (sameDay) return `Today ${time}`;
  if (yesterday) return `Yesterday ${time}`;
  return d.toLocaleDateString('en-GB', { day: 'numeric', month: 'short' }) + ` ${time}`;
}

/** Road classes, spoken the way a person would say them. */
const CLASS_LABELS: Record<string, string> = {
  cycleway: 'cycle path',
  path: 'path',
  trail: 'marked trail',
  track: 'track',
  residential: 'streets',
  service: 'streets',
  tertiary: 'road',
  secondary: 'main road',
  primary: 'main road',
  motorway: 'motorway',
  trunk: 'trunk road',
  pedestrian: 'pedestrian',
  living_street: 'streets',
  unclassified: 'road',
  footway: 'path',
  steps: 'steps',
  ferry: 'ferry',
};

export function classLabel(cls: string): string {
  return CLASS_LABELS[cls] ?? cls.replace(/_/g, ' ');
}

/** "19 km cycle path · 2 km streets" — the shape of the journey in one line. */
export function composition(classes: Record<string, number> | undefined, max = 3): string {
  if (!classes) return '';
  const merged = new Map<string, number>();
  for (const [cls, meters] of Object.entries(classes)) {
    if (!isFinite(meters) || meters <= 0) continue;
    const label = classLabel(cls);
    merged.set(label, (merged.get(label) ?? 0) + meters);
  }
  const total = [...merged.values()].reduce((a, b) => a + b, 0);
  if (!total) return '';
  return [...merged.entries()]
    .sort((a, b) => b[1] - a[1])
    .filter(([, m]) => m / total > 0.03)
    .slice(0, max)
    .map(([label, m]) => `${fmtDistance(m)} ${label}`)
    .join(' · ');
}

export function mergeClasses(list: (Record<string, number> | undefined)[]): Record<string, number> {
  const out: Record<string, number> = {};
  for (const c of list) {
    if (!c) continue;
    for (const [k, v] of Object.entries(c)) out[k] = (out[k] ?? 0) + (v || 0);
  }
  return out;
}

// --- vocabulary -------------------------------------------------------------

export const MODE_LABELS: Record<Mode, string> = {
  car: 'Car',
  bike: 'Bike',
  hike: 'Hike',
  'car+hike': 'Car + hike',
  'bike+hike': 'Bike + hike',
};

export const LEG_MODE_LABELS: Record<LegMode, string> = {
  car: 'Drive',
  bike: 'Ride',
  hike: 'Walk',
  lift: 'Lift',
};

export const LIFT_TYPE_LABELS: Record<string, string> = {
  cable_car: 'cable car',
  gondola: 'gondola',
  chair_lift: 'chair lift',
  mixed_lift: 'gondola and chairs',
};

export function liftTypeLabel(t?: string): string {
  if (!t) return 'lift';
  return LIFT_TYPE_LABELS[t] ?? t.replace(/_/g, ' ');
}

export const GRADES: Grade[] = ['T', 'E', 'EE', 'EEA', 'A'];

export const GRADE_NAMES: Record<Grade, string> = {
  T: 'Tourist',
  E: 'Hiker',
  EE: 'Experienced hiker',
  EEA: 'Equipped',
  A: 'Alpine',
};

export const GRADE_HINTS: Record<Grade, string> = {
  T: 'Tourist — wide, well-marked paths, no exposure.',
  E: 'Hiker — mountain paths, sure footing, some steep ground.',
  EE: 'Experienced hiker — steep, exposed or rocky ground.',
  EEA: 'Equipped — via ferrata: harness, helmet and set.',
  A: 'Alpine — glacier, rope and crampons; not a marked path.',
};

/** Warnings the card must not bury in the ordinary list. */
export function isAlpineWarning(w: string): boolean {
  return /alpine terrain/i.test(w);
}

export function isSeasonalWarning(w: string): boolean {
  return /lifts? run in season/i.test(w);
}

export function nextGrade(g: Grade): Grade | null {
  const i = GRADES.indexOf(g);
  return i >= 0 && i < GRADES.length - 1 ? GRADES[i + 1] : null;
}

/** The service speaks in fragments; a panel speaks in sentences. */
export function sentence(text: string | undefined): string {
  const t = (text ?? '').trim();
  return t ? t[0].toUpperCase() + t.slice(1) : '';
}

export const KIND_LABELS: Record<string, string> = {
  place: 'Place',
  peak: 'Peak',
  hut: 'Hut',
  pass: 'Pass',
  crag: 'Crag',
  street: 'Street',
  trail: 'Trail',
};

/**
 * What a climber asks first about a crag, from the layer's properties:
 * "4a–7c · S · limestone · 62 routes", with whichever parts the mapping has.
 */
export function cragDetail(p: Record<string, unknown>): string {
  const parts: string[] = [];
  for (const key of ['grades', 'aspect', 'rock'] as const) {
    if (p[key]) parts.push(String(p[key]));
  }
  const routes = Number(p.routes);
  if (routes > 0) parts.push(`${routes} routes`);
  return parts.join(' · ');
}
