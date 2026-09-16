import type { Grade, LegMode, Mode } from './types';
import { decimal, localeTag, t, type Key } from '../i18n';
import { serviceText } from '../i18n/service';

const THIN = ' '; // thin space, used as the thousands separator

export function groupDigits(n: number): string {
  return Math.round(n).toLocaleString('en-GB').replace(/,/g, THIN);
}

/** "2 h 35 min", "48 min", "< 1 min" — never seconds, never a clock. */
export function fmtDuration(seconds: number): string {
  if (!isFinite(seconds) || seconds < 0) return '—';
  const total = Math.round(seconds / 60);
  if (total < 1) return t('fmt.lessThanMinute');
  const h = Math.floor(total / 60);
  const m = total % 60;
  const H = t('fmt.hour');
  const M = t('fmt.minute');
  if (h === 0) return `${m} ${M}`;
  if (m === 0) return `${h} ${H}`;
  return `${h} ${H} ${m} ${M}`;
}

/** Compact form for a card headline: "2:35" reads as a clock, so keep h/min. */
export function fmtDistance(meters: number): string {
  if (!isFinite(meters) || meters < 0) return '—';
  if (meters < 1000) return `${Math.round(meters / 10) * 10} m`;
  const km = meters / 1000;
  if (km < 10) return `${decimal(km, 1)} km`;
  return `${groupDigits(km)} km`;
}

/** How far a point had to move: "120 m", "1.4 km" — or "1,4 km" in Italian. */
export function fmtOffset(meters: number): string {
  return meters >= 1000 ? `${decimal(meters / 1000, 1)} km` : `${Math.round(meters)} m`;
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
  const tag = localeTag();
  const time = d.toLocaleTimeString(tag, { hour: '2-digit', minute: '2-digit' });
  if (sameDay) return `${t('fmt.today')} ${time}`;
  if (yesterday) return `${t('fmt.yesterday')} ${time}`;
  return d.toLocaleDateString(tag, { day: 'numeric', month: 'short' }) + ` ${time}`;
}

/** A catalogue line for a word the data hands us, or the word itself. */
function lookup(prefix: string, word: string): string {
  const key = `${prefix}.${word}` as Key;
  const said = t(key);
  return said === key ? word.replace(/_/g, ' ') : said;
}

/** Road classes, spoken the way a person would say them. */
export function classLabel(cls: string): string {
  return lookup('class', cls);
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
// Functions, not tables: each reads the current language when it is called,
// so a template that calls one re-renders when the language changes.

export function modeLabel(m: Mode): string {
  return t(`mode.${m}` as Key);
}

export function legModeLabel(m: LegMode): string {
  return t(`legMode.${m}` as Key);
}

export function liftTypeLabel(type?: string): string {
  if (!type) return t('liftType.other');
  return lookup('liftType', type);
}

export const GRADES: Grade[] = ['T', 'E', 'EE', 'EEA', 'A'];

export function gradeName(g: Grade): string {
  return t(`grade.${g}.name` as Key);
}

/** What the grade asks of you, without its name: "wide, well-marked paths…". */
export function gradeDesc(g: Grade): string {
  return t(`grade.${g}.desc` as Key);
}

/** "Tourist — wide, well-marked paths, no exposure." */
export function gradeHint(g: Grade): string {
  return `${gradeName(g)} — ${gradeDesc(g)}`;
}

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

/**
 * The service speaks in fragments; a panel speaks in sentences — and in the
 * panel's language. Checks on what the service said (isAlpineWarning and the
 * rest) must run on the raw text, never on what this returns.
 */
export function sentence(text: string | undefined): string {
  const said = serviceText(text);
  return said ? said[0].toUpperCase() + said.slice(1) : '';
}

export function kindLabel(kind: string): string {
  return lookup('kind', kind);
}

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
  if (routes > 0) parts.push(t('crag.routes', { n: routes }));
  return parts.join(' · ');
}
