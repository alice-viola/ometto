/**
 * The routing engine speaks English, and its tests pin every sentence it says
 * (`internal/route/route_test.go`): "parked at Molveno", "no route up to grade
 * E". Rather than teach the engine a second language, the page recognises
 * those sentences and says them again in its own.
 *
 * Two rules keep this honest:
 *  - In English nothing is rewritten. The engine's text is the text, so the
 *    English page cannot drift from what the engine and its tests say.
 *  - Anything not recognised is shown as it came. A sentence the engine learns
 *    tomorrow reads in English until it is added here, never as a blank.
 *
 * The frontend's own checks on these strings — `isAlpineWarning`,
 * `isSeasonalWarning`, the parking badge, the far-point coordinate — run on the
 * raw text, before any of this. Translate for display only.
 */
import { locale, t, type Key } from './index';

const GRADE = '(T|E|EE|EEA|A)';

/** A mode as the engine names it, said as a way of travelling. */
function byMode(mode: string): string {
  const key = `byMode.${mode}` as Key;
  const said = t(key);
  return said === key ? mode : said;
}

/** "the destination" is the engine's own word for a point it cannot name. */
function place(name: string): string {
  return name === 'the destination' ? t('svc.theDestination') : wayName(name);
}

const SENTENCES: [RegExp, (m: RegExpMatchArray) => string][] = [
  // warnings
  [/^via ferrata excluded$/i, () => t('svc.ferrataExcluded')],
  [/^route uses a via ferrata$/i, () => t('svc.usesFerrata')],
  [/^alpine terrain beyond EEA\b/i, () => t('svc.alpine')],
  [new RegExp(`^route uses an? ${GRADE} path$`, 'i'), (m) => t('svc.usesGradePath', { grade: m[1] })],
  [/^lifts? run in season\b/i, () => t('svc.liftsSeason')],
  [/^no elevation data$/i, () => t('svc.noElevation')],
  [new RegExp(`^paths above grade ${GRADE} were excluded$`, 'i'), (m) => t('svc.gradeExcluded', { grade: m[1] })],
  [/^parked at the trailhead$/i, () => t('svc.parkedTrailhead')],
  [/^parked at the stop$/i, () => t('svc.parkedStop')],
  [/^parked at (.+)$/i, (m) => t('svc.parkedAt', { name: wayName(m[1]) })],
  // reasons there is no route
  [/^a route needs at least two points\.?$/i, () => t('svc.twoPoints')],
  [/^two points are needed\.?$/i, () => t('svc.twoPoints')],
  [/^no road within 1 km of (.+)$/i, (m) => t('svc.noRoadNear', { coord: m[1] })],
  [/^no route without (.+)$/i, (m) => t('svc.noRouteWithout', { name: wayName(m[1]) })],
  [new RegExp(`^no route up to grade ${GRADE}$`, 'i'), (m) => t('svc.noRouteGrade', { grade: m[1] })],
  [
    new RegExp(`^no route at ${GRADE}: the trail to (.+) is ${GRADE}$`, 'i'),
    (m) => t('svc.noRouteAt', { grade: m[1], name: place(m[2]), needed: m[3] }),
  ],
  [
    /^no (car|bike|hike|car\+hike|bike\+hike) route between these points$/i,
    (m) => t('svc.noModeRoute', { mode: byMode(m[1].toLowerCase()) }),
  ],
];

/** A warning or a no-route reason, in the page's language. */
export function serviceText(text: string | undefined): string {
  const raw = (text ?? '').trim();
  if (!raw || locale.value === 'en') return raw;
  for (const [re, say] of SENTENCES) {
    const m = raw.match(re);
    if (m) return say(m);
  }
  return raw;
}

const WAYS: Record<string, Key> = {
  road: 'way.road',
  track: 'way.track',
  path: 'way.path',
  lane: 'way.lane',
  street: 'way.street',
  'unnamed road': 'way.unnamed',
};

/**
 * A way the engine had to name for itself. Real names — "Via Belenzani",
 * "SAT 102", "SS 12" — pass through: only the engine's own words are
 * translated, and only when they are the whole name.
 */
export function wayName(name: string | undefined): string {
  const raw = name ?? '';
  if (!raw || locale.value === 'en') return raw;
  const key = WAYS[raw.toLowerCase()];
  if (key) return t(key);
  let m = raw.match(/^trail (\S+)$/i);
  if (m) return t('way.trail', { ref: m[1] });
  m = raw.match(/^cycle route (\S+)$/i);
  if (m) return t('way.cycleRoute', { ref: m[1] });
  // The last resort of a reverse lookup: "service road", "residential road".
  if (/^[a-z_]+ road$/.test(raw)) return t('way.road');
  return raw;
}

/** The geocoder's line under a crag — "4a–7c · S · 62 routes". */
export function detailText(detail: string | undefined): string {
  const raw = detail ?? '';
  if (!raw || locale.value === 'en') return raw;
  return raw.replace(/\b(\d+) routes?\b/, (_, n: string) => t('crag.routes', { n: Number(n) }));
}

/**
 * The small word under a clicked feature's name: a kind from our own layers,
 * or a class from the basemap. Known words are translated, lower-case, as the
 * English page shows them; a basemap class we have no word for stays as is.
 */
export function featureKind(kind: string): string {
  const raw = kind.replace(/_/g, ' ');
  if (locale.value === 'en') return raw;
  const candidates = [`kind.${kind}`, `class.${kind}`, `liftType.${kind}`] as Key[];
  if (kind === 'lift') candidates.unshift('liftType.other');
  for (const key of candidates) {
    const said = t(key);
    if (said !== key) return said.toLowerCase();
  }
  return raw;
}
