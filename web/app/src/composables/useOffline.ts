import { computed, ref, shallowRef } from 'vue';
import { api } from '../lib/api';
import { appConfig } from '../lib/config';
import { boundsOf, extendBounds, type Bounds, type Coord } from '../lib/geo';
import {
  areaBytes,
  clearAll as clearStore,
  deleteSaved,
  downloadArea,
  estimate,
  listSaved,
  persist,
  planArea,
  putSaved,
  queryKey,
  removeArea,
  type SavedRoute,
} from '../lib/offline';
import { REGION_BOUNDS } from '../map/style';
import type { RouteResponse } from '../lib/types';
import type { SharedQuery } from '../lib/url';
import { layers } from './useLayers';
import { toast } from './useToast';
import { decimal, t } from '../i18n';

/**
 * Whether this browser is on the network, whether the service behind it is
 * answering, and what has been kept for the times it is neither.
 *
 * `online` is the browser's own word for it, which is optimistic — a phone on
 * a hotel wifi that goes nowhere still calls itself online. That is why the
 * saved answer is also tried when a request fails outright, in api.route().
 */
export const online = ref(navigator.onLine !== false);
/** On the network, but the routing service says it cannot answer. */
export const serviceDown = ref(false);

window.addEventListener('online', () => {
  online.value = true;
  void checkService();
});
window.addEventListener('offline', () => (online.value = false));

export async function checkService(): Promise<void> {
  try {
    const h = await api.health();
    serviceDown.value = h?.ok === false;
  } catch {
    // With no network at all it is not the service that is down, and the
    // offline line already says the true thing.
    serviceDown.value = online.value;
  }
}

// --- a new version of the app ----------------------------------------------

/** A worker is installed and waiting; the page offers the reload, never takes it. */
export const needRefresh = ref(false);
let updater: ((reload?: boolean) => Promise<void>) | null = null;

export function offerUpdate(fn: (reload?: boolean) => Promise<void>): void {
  updater = fn;
  needRefresh.value = true;
}

export function applyUpdate(): void {
  needRefresh.value = false;
  void updater?.(true);
}

// --- the saved routes -------------------------------------------------------

/**
 * Shallow on purpose. A record carries a whole answer — three alternatives,
 * their legs, their profiles — and making all of that deeply reactive would
 * cost far more than it buys: the list is replaced whole on every change.
 */
export const savedRoutes = shallowRef<SavedRoute[]>([]);
export const savedLoaded = ref(false);

export async function loadSaved(): Promise<void> {
  savedRoutes.value = await listSaved();
  savedLoaded.value = true;
}

/**
 * Bumped when a card asks for the saved list to be shown. Whoever is holding
 * the frame answers it: the panel on a desktop, the app itself on a phone,
 * where the card is in a sheet that has no navigation of its own.
 */
export const savedViewToken = ref(0);
export function openSavedView(): void {
  savedViewToken.value++;
}

/** One at a time, and visible from wherever it was started. */
export interface Downloading {
  key: string;
  done: number;
  total: number;
  bytes: number;
  /** What the area was reckoned to weigh before a byte of it arrived. */
  estimated: number;
}
export const downloading = ref<Downloading | null>(null);
let ctrl: AbortController | null = null;

export function cancelDownload(): void {
  ctrl?.abort();
}

export const downloadPercent = computed(() => {
  const d = downloading.value;
  if (!d?.total) return 0;
  return Math.min(99, Math.floor((d.done / d.total) * 100));
});

/** Every box the trip touches: each alternative's line, and the points asked for. */
function boundsFor(q: SharedQuery, res: RouteResponse): Bounds {
  let b: Bounds | null = null;
  for (const r of res.routes ?? []) {
    b = extendBounds(b, boundsOf((r.geometry?.coordinates ?? []) as Coord[]));
  }
  b = extendBounds(
    b,
    boundsOf(q.points.map((p) => [p.lon, p.lat] as Coord)),
  );
  return b ?? REGION_BOUNDS;
}

/** The overlays the map is actually drawing; the region outline is always one. */
function overlayNames(): string[] {
  const names = ['region'];
  if (layers.sat) names.push('sat');
  if (layers.pois) names.push('pois');
  if (layers.lifts) names.push('lifts');
  if (layers.crags) names.push('crags');
  return names;
}

/**
 * What the map of this answer would weigh, before a byte of it is fetched.
 * A trip across the province is a far bigger download than a walk above a
 * village, and the number is the only way to tell before starting.
 */
export function estimateFor(q: SharedQuery, res: RouteResponse): number {
  return planArea(boundsFor(q, res), appConfig().terrain.url, overlayNames()).estimatedBytes;
}

/**
 * An area counts as downloaded when a whole pass finished and three quarters
 * of it is on the device. A handful of addresses the server has nothing for
 * must not leave a perfectly usable map marked as missing.
 */
const ENOUGH = 0.75;

let asked = false;

/**
 * IndexedDB clones what it is given, and it refuses a Vue proxy outright: the
 * points of a query come straight out of a reactive array. All of this is
 * plain data anyway, so it goes in as plain data.
 */
const plain = <T>(v: T): T => JSON.parse(JSON.stringify(v)) as T;

/**
 * Keep this answer, then fetch the ground it crosses. The answer is written
 * first and on its own: a download that fails halfway still leaves a route
 * that opens offline, with its map marked as missing.
 */
export async function saveRoute(q: SharedQuery, res: RouteResponse): Promise<SavedRoute> {
  const key = queryKey(q);
  const rec: SavedRoute = {
    key,
    savedAt: Date.now(),
    query: plain(q),
    response: plain(res),
    bounds: [...boundsFor(q, res)] as Bounds,
    tiles: { count: 0, bytes: 0, done: false },
    urls: [],
    version: 1,
  };
  await putSaved(rec);
  if (!asked) {
    asked = true;
    void persist();
  }
  await loadSaved();
  return rec;
}

/** Fetch — or finish fetching — the map of a saved route's area. */
export async function downloadFor(rec: SavedRoute): Promise<void> {
  if (downloading.value) return;
  const plan = planArea(rec.bounds, appConfig().terrain.url, overlayNames());
  const estimated = plan.estimatedBytes;
  ctrl = new AbortController();
  downloading.value = { key: rec.key, done: 0, total: plan.urls.length, bytes: 0, estimated };

  let aborted = false;
  try {
    const result = await downloadArea(
      plan.urls,
      (p) => {
        if (downloading.value?.key === rec.key) downloading.value = { key: rec.key, ...p, estimated };
      },
      ctrl.signal,
    );
    aborted = result.aborted;
  } catch {
    // A page served over plain http has no cache storage at all. The route is
    // still saved; the area simply stays marked as missing.
    toast(t('offline.mapIncomplete'));
    return;
  } finally {
    ctrl = null;
    downloading.value = null;
  }

  // What is really on the device, not what this pass happened to fetch: two
  // routes over the same valley share most of a map, and the second one must
  // report the size of the whole thing.
  const held = await areaBytes(plan.urls);
  const done = !aborted && held.held >= plan.urls.length * ENOUGH;
  // `rec` is plain wherever it came from — the store, or the write above —
  // because the list that holds it is shallow. That is what lets it go
  // straight back in without being copied again.
  const next: SavedRoute = {
    ...rec,
    urls: plan.urls,
    tiles: { count: held.held, bytes: held.bytes, done, at: Date.now() },
  };
  await putSaved(next);
  await loadSaved();

  if (aborted) toast(t('offline.cancelled'));
  else if (done) toast(t('offline.mapSaved', { size: fmtBytes(held.bytes) }));
  else toast(t('offline.mapIncomplete'));
}

export async function forget(rec: SavedRoute): Promise<void> {
  await removeArea(rec, savedRoutes.value);
  await deleteSaved(rec.key);
  await loadSaved();
}

export async function forgetAll(): Promise<void> {
  await clearStore();
  await loadSaved();
}

/** What the saved routes take: their maps, added up. */
export async function usedBytes(): Promise<number> {
  return savedRoutes.value.reduce((a, r) => a + r.tiles.bytes, 0);
}

/**
 * What Ometto is using on this device altogether — the app itself, and
 * whatever the map drew on the way — which is more than the saved maps and
 * is the figure a full phone is about. Null when the browser will not say.
 */
export async function deviceBytes(): Promise<number | null> {
  const e = await estimate();
  return e?.usage ? e.usage : null;
}

/** One decimal megabyte, with this language's decimal mark. */
export function fmtBytes(bytes: number): string {
  return `${decimal(Math.max(0, bytes) / 1_000_000, 1)} MB`;
}

/**
 * An iPhone that is not on the Home Screen gives a site a small budget and
 * clears it after a week or two unused. Saying so is the only thing the page
 * can do about it, and it is worth saying once.
 */
export const iosNotInstalled = (() => {
  try {
    const standalone = (navigator as unknown as { standalone?: boolean }).standalone;
    return /iP(hone|ad|od)/.test(navigator.userAgent) && !standalone;
  } catch {
    return false;
  }
})();
