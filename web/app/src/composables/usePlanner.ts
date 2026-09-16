import { computed, nextTick, ref, shallowRef, watch } from 'vue';
import { api, ApiError } from '../lib/api';
import { t } from '../i18n';
import type {
  AvoidPoint,
  AvoidedWay,
  Grade,
  HistoryEntry,
  Mode,
  RouteAlternative,
  RouteResponse,
  Waypoint,
} from '../lib/types';
import { decodeQuery, replaceUrl, type SharedQuery } from '../lib/url';
import { MAX_AVOIDS, MAX_POINTS, MAX_SEQUENCE } from '../lib/limits';
import { cumulative, nearestOnLine, round6, type Coord } from '../lib/geo';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';
import { isCompact } from './useMedia';

export interface Slot {
  key: string;
  point: Waypoint | null;
}

export type Status = 'idle' | 'loading' | 'recomputing' | 'error' | 'noroute';

let seq = 0;
const newSlot = (point: Waypoint | null = null): Slot => ({ key: `s${++seq}`, point });

export const slots = ref<Slot[]>([newSlot(), newSlot()]);
// Car + hike is what the region is for: drive to the foot of it, walk to the top.
export const mode = ref<Mode>('car+hike');
// Alpine by default: the mountain is the point of the region, and a summit
// refused at E was the first thing most people saw.
export const grade = ref<Grade>('A');
/**
 * One answer on a desktop, where the panel invites comparing on request; the
 * three of them on a phone, where the choice is the point of the sheet and
 * asking for it would be one more chip to find.
 */
export const alternatives = ref<1 | 3>(isCompact.value ? 3 : 1);
/**
 * Whether the answer may ride a cable car. Off unless asked for: a lift turns
 * a day out into a different day out, and it only runs in season.
 */
export const lifts = ref<boolean>(readLocalRaw('lifts') === '1');
watch(lifts, (v) => writeLocalRaw('lifts', v ? '1' : '0'));

export const routes = ref<RouteAlternative[]>([]);
export const selectedId = ref<string | null>(null);
export const status = ref<Status>('idle');
/** Said when it is read, so a language switch reaches an error already on screen. */
const errorSay = shallowRef<(() => string) | null>(null);
export const errorText = computed(() => errorSay.value?.() ?? '');
export const noRouteReason = ref('');
/** The grade this exact question would need, when the service knows one. */
export const neededGrade = ref<Grade | null>(null);

/**
 * Something the service had to say about one point: it had to walk a long way
 * to reach the network, or there is no network near it at all. Keyed by the
 * point's position in `slots`.
 */
export interface PointNote {
  kind: 'moved' | 'far';
  metres: number;
  /** What it landed on: "trail 318A", "Via Dolomiti". */
  name?: string;
  lat?: number;
  lon?: number;
}
export const pointNotes = ref<Record<number, PointNote>>({});

/** Ways the person has ruled out, and what the service made of them. */
export const avoids = ref<AvoidPoint[]>([]);
export const avoidedWays = ref<AvoidedWay[]>([]);
/** The service answered from a reduced state; the answer is still an answer. */
export const degraded = ref(false);
/**
 * When the answer on screen was saved, if it came out of this browser's
 * offline store because the service could not be reached. Null the rest of
 * the time, which is nearly always.
 */
export const savedCopyAt = ref<number | null>(null);
/**
 * The whole answer as it arrived, which is more than the alternatives on
 * screen: what the service made of each point, what it had to leave out. The
 * offline store keeps this one, so a saved route reopens as it first landed.
 */
export const lastResponse = shallowRef<RouteResponse | null>(null);
/** Bumped after every successful result so the map knows to frame it. */
export const resultToken = ref(0);
/** Bumped after every successful result so the history list knows to refresh. */
export const historyToken = ref(0);
/**
 * Bumped when the answer to a question the person asked has landed — a result,
 * a refusal or an error, never an automatic re-run. The phone sheet moves on
 * these and only these: a grade change must not yank it about.
 */
export const answeredToken = ref(0);

/** Every point the request carries, vias included, in order. */
export const filledPoints = computed(() =>
  slots.value.map((s) => s.point).filter((p): p is Waypoint => !!p),
);

/**
 * A via is a shape the person dragged into the route, not somewhere they are
 * going. The panel lists only the places: start, stops, destination.
 */
export const isVia = (s: Slot) => !!s.point?.via;
export const placeIndices = computed(() =>
  slots.value.map((s, i) => (isVia(s) ? -1 : i)).filter((i) => i >= 0),
);
export const viaCount = computed(() => slots.value.filter(isVia).length);

export const canCompute = computed(
  () => filledPoints.value.filter((p) => !p.via).length >= 2,
);
export const hasResult = computed(() => routes.value.length > 0);
export const selected = computed<RouteAlternative | null>(
  () => routes.value.find((r) => r.id === selectedId.value) ?? routes.value[0] ?? null,
);
export const busy = computed(() => status.value === 'loading' || status.value === 'recomputing');

export { MAX_AVOIDS, MAX_POINTS, MAX_SEQUENCE };

export const isCombinedMode = computed(() => mode.value.includes('+'));

/**
 * Which slot wears the P badge. The service decides: a stop no road reaches is
 * a walking waypoint, not a car park, so `parking.point` names the last stop a
 * road actually reaches — and is null when the switch happens mid-leg, in
 * which case no stop is marked at all.
 *
 * Until the field arrives, the old rule stands: the first stop of a two-mode
 * plan, which is right often enough and wrong quietly.
 */
export const parkingIndex = computed(() => {
  const filled = slots.value.map((s, i) => (s.point ? i : -1)).filter((i) => i >= 0);
  const route = selected.value;
  const wheeled = !!route?.legs.some((l) => l.mode === 'car' || l.mode === 'bike');
  const parking = wheeled ? route?.parking : undefined;

  if (parking !== undefined && parking !== null) {
    if (parking.point === null || parking.point === undefined) return -1;
    const slot = filled[parking.point];
    const places = placeIndices.value;
    // Start and destination carry their own marks; only a stop takes the P,
    // and a via is never a car park.
    if (slot === undefined || slot === places[0] || slot === places[places.length - 1]) return -1;
    if (slots.value[slot]?.point?.via) return -1;
    return slot;
  }

  if (!isCombinedMode.value) return -1;
  const places = placeIndices.value.filter((i) => slots.value[i].point);
  return places.length >= 3 ? places[1] : -1;
});

let inflight: AbortController | null = null;
let debounceTimer: number | undefined;
let suspended = false;

/**
 * Whether the page asks by itself. A desktop has a Compute button and asks
 * when it is pressed; a phone has none, so there any change to a question
 * that has no answer yet is asked the moment it is made. Set by the layout.
 */
export const autoAsk = ref(false);
/** The question last sent, so a repeat of it is told from a change. */
let asked = '';

function currentQuery(): SharedQuery {
  return {
    points: filledPoints.value,
    mode: mode.value,
    grade: grade.value,
    alternatives: alternatives.value,
    lifts: lifts.value,
    avoid: avoids.value.slice(),
  };
}

const queryKey = (): string => JSON.stringify(currentQuery());

/**
 * `silent` marks an automatic re-run: the map or the mode changed under an
 * answer that already exists. Those recompute, but they are not new questions,
 * so they are not remembered.
 */
export async function compute(silent = false): Promise<void> {
  if (!canCompute.value) return;
  asked = queryKey();
  inflight?.abort();
  const ctrl = new AbortController();
  inflight = ctrl;
  clearTimeout(debounceTimer);

  status.value = silent && hasResult.value ? 'recomputing' : 'loading';
  errorSay.value = null;
  noRouteReason.value = '';
  neededGrade.value = null;
  pointNotes.value = {};
  degraded.value = false;
  savedCopyAt.value = null;

  const request = {
    // The name rides along with the coordinate: the service stores it with the
    // query, so the history list can say "Trento" instead of the street the
    // point happened to snap to.
    points: filledPoints.value.map((p) => ({
      lat: round6(p.lat),
      lon: round6(p.lon),
      ...(p.name ? { name: p.name } : {}),
      ...(p.via ? { via: true } : {}),
    })),
    mode: mode.value,
    grade: grade.value,
    alternatives: alternatives.value,
    lifts: lifts.value,
    ...(avoids.value.length
      ? { avoid: avoids.value.map((a) => ({ lat: round6(a.lat), lon: round6(a.lon) })) }
      : {}),
  };

  try {
    const res = await api.route(request, ctrl.signal, !silent);
    if (ctrl.signal.aborted) return;
    avoidedWays.value = res.avoided ?? [];
    degraded.value = res.degraded === true;
    savedCopyAt.value = res.fromSaved?.at ?? null;
    lastResponse.value = res;
    readSnapped(res.snapped, res.reason);
    if (!res.routes?.length) {
      routes.value = [];
      selectedId.value = null;
      noRouteReason.value = res.reason || '';
      neededGrade.value = res.neededGrade ?? null;
      status.value = 'noroute';
      if (!silent) answeredToken.value++;
      return;
    }
    routes.value = res.routes;
    selectedId.value = res.routes[0].id;
    status.value = 'idle';
    resultToken.value++;
    replaceUrl(currentQuery());
    if (!silent) {
      noteHistory(res.routes[0]);
      answeredToken.value++;
    }
  } catch (e) {
    if ((e as Error)?.name === 'AbortError') return;
    routes.value = [];
    selectedId.value = null;
    errorSay.value = () => (e instanceof ApiError ? e.human : t('error.somethingWrong'));
    status.value = 'error';
    if (!silent) answeredToken.value++;
  } finally {
    if (inflight === ctrl) inflight = null;
  }
}

/** Metres a point may be nudged onto the network without anyone being told. */
const QUIET_MOVE = 150;

/**
 * Reconcile what the service did to each point. A small nudge onto the nearest
 * road is applied silently; a long walk to the network is reported instead of
 * performed, so the marker stays where the person put it until they say
 * otherwise. A point with no network near it at all is named, not shrugged at.
 */
function readSnapped(snapped: RouteResponse['snapped'], reason?: string): void {
  if (!snapped?.length) return;
  const indices = slots.value.map((s, i) => (s.point ? i : -1)).filter((i) => i >= 0);
  const notes: Record<number, PointNote> = {};
  const farAt = farCoordinate(reason);
  let quiet = false;

  snapped.forEach((s, n) => {
    const index = indices[n];
    const slot = slots.value[index];
    if (!slot?.point) return;
    const metres = s.distance ?? 0;
    const isFar =
      !!farAt &&
      Math.abs(slot.point.lat - farAt[0]) < 1e-4 &&
      Math.abs(slot.point.lon - farAt[1]) < 1e-4;

    if (isFar) {
      notes[index] = { kind: 'far', metres, name: s.name };
      return;
    }
    if (metres > QUIET_MOVE) {
      notes[index] = { kind: 'moved', metres, name: s.name, lat: s.lat, lon: s.lon };
      return;
    }
    const moved = Math.abs(slot.point.lat - s.lat) > 1e-6 || Math.abs(slot.point.lon - s.lon) > 1e-6;
    if (!moved) return;
    // Applying the nudge must not read as a new question, or every answer
    // would ask itself again.
    if (!quiet) {
      quiet = true;
      suspended = true;
    }
    slot.point = { ...slot.point, lat: s.lat, lon: s.lon, name: slot.point.name ?? s.name };
  });

  pointNotes.value = notes;
  if (quiet) void nextTick(() => (suspended = false));
}

/**
 * "no road within 1 km of 46.45000,10.55000" -> [46.45, 10.55]. The wording is
 * the service's and may change; the coordinate pair is what identifies the
 * point, so that is what this looks for.
 */
function farCoordinate(reason?: string): [number, number] | null {
  const text = reason ?? '';
  const m =
    /\bof\s+(-?\d+(?:\.\d+)?)\s*,\s*(-?\d+(?:\.\d+)?)/.exec(text) ??
    /(-?\d+\.\d+)\s*,\s*(-?\d+\.\d+)\s*$/.exec(text);
  if (!m) return null;
  const lat = Number(m[1]);
  const lon = Number(m[2]);
  return isFinite(lat) && isFinite(lon) ? [lat, lon] : null;
}

/** Take the service up on its offer and put the marker where the route starts. */
export function acceptMove(index: number): void {
  const note = pointNotes.value[index];
  const slot = slots.value[index];
  if (!note || note.lat === undefined || note.lon === undefined || !slot?.point) return;
  // The route already runs from there: moving the pin changes nothing to ask again.
  suspended = true;
  slot.point = { ...slot.point, lat: note.lat, lon: note.lon };
  void nextTick(() => (suspended = false));
  dismissNote(index);
}

export function dismissNote(index: number): void {
  const next = { ...pointNotes.value };
  delete next[index];
  pointNotes.value = next;
}

function noteHistory(best: RouteAlternative): void {
  const pts = filledPoints.value;
  const entry: HistoryEntry = {
    id: `h${Date.now()}`,
    at: new Date().toISOString(),
    request: {
      points: pts,
      mode: mode.value,
      grade: grade.value,
      alternatives: alternatives.value,
      lifts: lifts.value,
      ...(avoids.value.length ? { avoid: avoids.value.slice() } : {}),
    },
    summary: {
      seconds: best.seconds,
      meters: best.meters,
      ascent: best.ascent,
      descent: best.descent,
      fromName: pts[0]?.name,
      toName: pts[pts.length - 1]?.name,
    },
  };
  api.noteHistory(entry);
  historyToken.value++;
}

/**
 * Any change to the query after a result re-runs it, quietly. Without a
 * result there is nothing to keep current, and a desktop waits for Compute —
 * but a page that asks by itself asks again as soon as the question has
 * changed, so "no route" or an error is never the last word on a phone.
 */
function scheduleRecompute(): void {
  if (suspended || !canCompute.value) return;
  if (!hasResult.value && !autoAsk.value) return;
  // The question already on its way, or already answered, is not asked twice:
  // a place picked on a phone is asked outright the moment it is set, and the
  // watcher that sees the same change must not chase it with a quiet re-run.
  if (queryKey() === asked) return;
  clearTimeout(debounceTimer);
  debounceTimer = window.setTimeout(() => void compute(true), 450);
}

watch([mode, grade, alternatives, lifts], scheduleRecompute);
watch(
  () => avoids.value.map((a) => `${round6(a.lat)},${round6(a.lon)}`).join('|'),
  scheduleRecompute,
);
watch(
  () =>
    slots.value
      .map((s) => (s.point ? `${round6(s.point.lat)},${round6(s.point.lon)}` : '-'))
      .join('|'),
  scheduleRecompute,
);

// --- point editing ----------------------------------------------------------

export function setPoint(index: number, point: Waypoint | null): void {
  const slot = slots.value[index];
  if (!slot) return;
  slot.point = point;
  if (!point && !hasResult.value) return;
  if (!point) {
    // Dropping a point below two invalidates the result rather than recomputing.
    if (filledPoints.value.length < 2) clearResult();
  }
}

export function addStop(): void {
  const places = placeIndices.value;
  if (places.length >= MAX_POINTS || slots.value.length >= MAX_SEQUENCE) return;
  // A stop goes before the destination, whatever vias sit around it.
  slots.value.splice(places[places.length - 1], 0, newSlot());
}

export function removeSlot(index: number): void {
  if (placeIndices.value.length <= 2) {
    setPoint(index, null);
    return;
  }
  slots.value.splice(index, 1);
}

/** Stops move past stops; the vias between them keep their own order. */
export function moveSlot(index: number, delta: number): void {
  const places = placeIndices.value;
  const at = places.indexOf(index);
  const to = places[at + delta];
  if (at < 0 || to === undefined) return;
  const a = slots.value[index];
  const b = slots.value[to];
  const pa = a.point;
  a.point = b.point;
  b.point = pa;
}

export function swapEnds(): void {
  const places = placeIndices.value;
  if (places.length < 2) return;
  const first = slots.value[places[0]];
  const last = slots.value[places[places.length - 1]];
  const a = first.point;
  first.point = last.point;
  last.point = a;
}

// --- vias -------------------------------------------------------------------

/**
 * Where in the sequence a via dropped at `along` metres belongs: after the
 * last request point the route reaches before it. Anchors come from projecting
 * each point onto the drawn line, which is also how the grab was measured.
 */
function viaPositionFor(along: number): number {
  const route = selected.value;
  const coords = (route?.geometry?.coordinates ?? []) as Coord[];
  const filled = slots.value.map((s, i) => (s.point ? i : -1)).filter((i) => i >= 0);
  if (coords.length < 2 || filled.length < 2) return Math.max(1, slots.value.length - 1);
  const cum = cumulative(coords);
  const anchors = filled.map((i) => {
    const p = slots.value[i].point!;
    return nearestOnLine(coords, cum, [p.lon, p.lat])?.along ?? 0;
  });
  let k = anchors.findIndex((a) => a > along);
  if (k <= 0) k = anchors.length - 1;
  return filled[k];
}

export function insertVia(lat: number, lon: number, along: number): void {
  if (slots.value.length >= MAX_SEQUENCE) return;
  slots.value.splice(viaPositionFor(along), 0, newSlot({ lat, lon, via: true }));
}

export function removeVia(index: number): void {
  if (!slots.value[index]?.point?.via) return;
  slots.value.splice(index, 1);
}

export function clearVias(): void {
  slots.value = slots.value.filter((s) => !isVia(s));
}

// --- avoids -----------------------------------------------------------------

export function addAvoid(lat: number, lon: number): void {
  if (avoids.value.length >= MAX_AVOIDS) return;
  avoids.value = [...avoids.value, { lat, lon }];
}

/**
 * Drop every avoid coordinate that resolved to this way. One way may have been
 * clicked more than once, and the service tells us only the way, so the match
 * is geometric: within the radius it resolves within.
 */
export function stopAvoiding(way: AvoidedWay): void {
  const pts = way.points ?? [];
  if (!pts.length) {
    avoids.value = [];
    return;
  }
  const near = (a: AvoidPoint) =>
    pts.some((c) => Math.abs(c[1] - a.lat) < 0.0012 && Math.abs(c[0] - a.lon) < 0.0018);
  const kept = avoids.value.filter((a) => !near(a));
  avoids.value = kept.length === avoids.value.length ? [] : kept;
  avoidedWays.value = avoidedWays.value.filter((w) => w.id !== way.id);
}

export function clearAvoids(): void {
  avoids.value = [];
  avoidedWays.value = [];
}

export function clearResult(): void {
  inflight?.abort();
  clearTimeout(debounceTimer);
  routes.value = [];
  selectedId.value = null;
  status.value = 'idle';
  errorSay.value = null;
  noRouteReason.value = '';
  neededGrade.value = null;
  pointNotes.value = {};
  degraded.value = false;
  savedCopyAt.value = null;
  lastResponse.value = null;
}

export function clearAll(): void {
  suspended = true;
  slots.value = [newSlot(), newSlot()];
  avoids.value = [];
  avoidedWays.value = [];
  clearResult();
  try {
    const keep = new URLSearchParams(location.search).get('mock');
    history.replaceState(null, '', location.pathname + (keep ? `?mock=${keep}` : ''));
  } catch {
    /* ignored */
  }
  suspended = false;
}

/** The first empty place, preferring start then destination. Vias are skipped. */
export function nextEmptyIndex(): number {
  const places = placeIndices.value;
  const i = places.find((idx) => !slots.value[idx].point);
  return i === undefined ? places[places.length - 1] : i;
}

export function placeFromMap(point: Waypoint, where: 'start' | 'stop' | 'destination' | 'next'): void {
  const places = () => placeIndices.value;
  if (where === 'start') setPoint(places()[0], point);
  else if (where === 'destination') setPoint(places()[places().length - 1], point);
  else if (where === 'stop') {
    addStop();
    const p = places();
    setPoint(p[p.length - 2], point);
  } else setPoint(nextEmptyIndex(), point);

  // A place picked off the map is a new question. A desktop asks it only
  // while there is no answer yet, and leaves the rest to Compute; a phone
  // asks it every time.
  if (canCompute.value && (!hasResult.value || autoAsk.value)) void compute();
}

export function selectRoute(id: string): void {
  selectedId.value = id;
}

/**
 * Replay a stored query as it was asked — except that a phone always shows the
 * alternatives, whatever the link or the history row said.
 */
export function applyQuery(q: SharedQuery, run = true): void {
  suspended = true;
  slots.value = q.points.map((p) => newSlot({ ...p }));
  while (slots.value.length < 2) slots.value.push(newSlot());
  mode.value = q.mode;
  grade.value = q.grade;
  alternatives.value = isCompact.value ? 3 : q.alternatives;
  lifts.value = !!q.lifts;
  avoids.value = (q.avoid ?? []).slice(0, MAX_AVOIDS);
  avoidedWays.value = [];
  clearResult();
  suspended = false;
  if (run) void compute();
}

/** A shared link reopens the same question and answers it. */
export function restoreFromUrl(): boolean {
  const q = decodeQuery(location.search);
  if (!q) return false;
  applyQuery(q, true);
  return true;
}

export function shareQuery(): SharedQuery {
  return currentQuery();
}
