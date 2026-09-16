/**
 * What a browser keeps of a route so it can be opened again with no network:
 * the question, the answer the service gave, and the map of the ground it
 * crosses.
 *
 * The answer lives in IndexedDB — a route with three alternatives, their legs
 * and their profiles is far past what localStorage will hold. The map lives
 * in a Cache Storage bucket the service worker knows by name and never
 * evicts; this file is the only thing that puts anything in it.
 */
import type { Bounds } from './geo';
import type { RouteResponse, Waypoint } from './types';
import type { SharedQuery } from './url';
import { round6 } from './geo';

/** The bucket the service worker looks in before it looks anywhere else. */
export const AREA_CACHE = 'ometto-offline-v1';

export interface SavedTiles {
  count: number;
  bytes: number;
  /** False while the area is half downloaded, or was cancelled. */
  done: boolean;
  at?: number;
}

export interface SavedRoute {
  key: string;
  savedAt: number;
  query: SharedQuery;
  /** Exactly what the page was handed: joined legs and all. */
  response: RouteResponse;
  bounds: Bounds;
  tiles: SavedTiles;
  /** Every address this area put in the cache, so removing one area can tell
      its own tiles from the ones another saved route still needs. */
  urls: string[];
  version: 1;
}

// --- the store --------------------------------------------------------------

const DB = 'ometto';
const STORE = 'routes';

let opening: Promise<IDBDatabase> | null = null;

function open(): Promise<IDBDatabase> {
  return (opening ??= new Promise<IDBDatabase>((resolve, reject) => {
    const req = indexedDB.open(DB, 1);
    req.onupgradeneeded = () => {
      const db = req.result;
      if (!db.objectStoreNames.contains(STORE)) db.createObjectStore(STORE, { keyPath: 'key' });
    };
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
    req.onblocked = () => reject(new Error('indexeddb blocked'));
  }));
}

/** One request in one transaction, which is every question this store is asked. */
function run<T>(mode: IDBTransactionMode, make: (s: IDBObjectStore) => IDBRequest): Promise<T> {
  return open().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const req = make(db.transaction(STORE, mode).objectStore(STORE));
        req.onsuccess = () => resolve(req.result as T);
        req.onerror = () => reject(req.error);
      }),
  );
}

/**
 * What identifies a saved answer. `alternatives` is left out on purpose: one
 * route or three is a way of showing the same trip, and a link that asks for
 * one must still find the copy that was saved with three.
 */
export function queryKey(q: SharedQuery): string {
  const point = (p: Waypoint) =>
    `${round6(p.lat)},${round6(p.lon)},${p.name ?? ''}${p.via ? ',v' : ''}`;
  return [
    q.points.map(point).join(';'),
    q.mode,
    q.grade,
    q.lifts ? 'l' : '',
    (q.avoid ?? [])
      .map((a) => `${round6(a.lat)},${round6(a.lon)}`)
      .sort()
      .join(';'),
  ].join('|');
}

/** Newest first, and never a rejection: a broken store is an empty list. */
export async function listSaved(): Promise<SavedRoute[]> {
  try {
    const all = await run<SavedRoute[]>('readonly', (s) => s.getAll());
    return all.sort((a, b) => b.savedAt - a.savedAt);
  } catch {
    return [];
  }
}

export async function getSaved(key: string): Promise<SavedRoute | undefined> {
  try {
    return await run<SavedRoute | undefined>('readonly', (s) => s.get(key));
  } catch {
    return undefined;
  }
}

export async function putSaved(rec: SavedRoute): Promise<void> {
  await run('readwrite', (s) => s.put(rec));
}

export async function deleteSaved(key: string): Promise<void> {
  try {
    await run('readwrite', (s) => s.delete(key));
  } catch {
    /* nothing to delete is not a failure */
  }
}

/** How much this browser is using and how much it will allow. */
export async function estimate(): Promise<{ usage: number; quota: number } | null> {
  try {
    const e = await navigator.storage?.estimate?.();
    return e ? { usage: e.usage ?? 0, quota: e.quota ?? 0 } : null;
  } catch {
    return null;
  }
}

/**
 * Ask the browser not to throw this away when it is short of room. Asked once,
 * when the first route is saved; a refusal changes nothing we can do about it.
 */
export async function persist(): Promise<void> {
  try {
    await navigator.storage?.persist?.();
  } catch {
    /* ignored */
  }
}

// --- the map of an area -----------------------------------------------------

/** Metres of ground kept around the route, so a wrong turn still has a map. */
const PAD_METRES = 1500;
/**
 * What a tile weighs, near enough to put a number on a download before it
 * starts: a mountain valley at zoom 14 and a relief tile both run to 80 KB and
 * more, and the overlays ride along uncounted. The real total is counted as it
 * arrives and is what gets stored.
 */
const TILE_BYTES = 80_000;

/** The basemap the page draws from. Below 8 is what the region view asks for. */
const BASEMAP_ZOOMS = [6, 7, 8, 9, 10, 11, 12, 13, 14];
/** The DEM, for hillshade, terrain and the contour lines. */
const DEM_ZOOMS = [9, 10, 11, 12, 13];
/** The shaded relief under the vector map, which stops at 6. */
const RELIEF_ZOOMS = [4, 5, 6];
/** What the two styles name; a stack the page never draws is never fetched. */
const FONT_STACKS = ['Noto Sans Regular', 'Noto Sans Bold', 'Noto Sans Italic'];
const FONT_RANGES = ['0-255', '256-511', '512-767', '768-1023'];
/** The style says `"sprite": "/map/sprites/ofm"`; these are the four files. */
const SPRITES = ['ofm.json', 'ofm.png', 'ofm@2x.json', 'ofm@2x.png'];

const lonToX = (lon: number, z: number) => Math.floor(((lon + 180) / 360) * 2 ** z);

const latToY = (lat: number, z: number) => {
  const r = (Math.max(-85.05, Math.min(85.05, lat)) * Math.PI) / 180;
  return Math.floor(((1 - Math.log(Math.tan(r) + 1 / Math.cos(r)) / Math.PI) / 2) * 2 ** z);
};

/** Ground around the line, in degrees, which is not the same in both axes. */
function padBounds(b: Bounds, metres = PAD_METRES): Bounds {
  const dLat = metres / 111_320;
  const mid = ((b[1] + b[3]) / 2) * (Math.PI / 180);
  const dLon = metres / (111_320 * Math.max(0.2, Math.cos(mid)));
  return [b[0] - dLon, b[1] - dLat, b[2] + dLon, b[3] + dLat];
}

/** Every {z}/{x}/{y} the standard web-mercator grid puts under a box. */
function tilesIn(b: Bounds, z: number): [number, number][] {
  const n = 2 ** z;
  const clamp = (v: number) => Math.max(0, Math.min(n - 1, v));
  const x0 = clamp(lonToX(b[0], z));
  const x1 = clamp(lonToX(b[2], z));
  const y0 = clamp(latToY(b[3], z));
  const y1 = clamp(latToY(b[1], z));
  const out: [number, number][] = [];
  for (let x = x0; x <= x1; x++) for (let y = y0; y <= y1; y++) out.push([x, y]);
  return out;
}

/** The same address the map will ask for: absolute, as transformRequest makes it. */
const abs = (path: string) => new URL(path, location.origin).toString();

const fill = (template: string, z: number, x: number, y: number) =>
  template.replace('{z}', String(z)).replace('{x}', String(x)).replace('{y}', String(y));

export interface AreaPlan {
  /** Tiles first: they are the bulk, and the estimate is made of them. */
  urls: string[];
  tiles: number;
  /** What to say before anything has been downloaded. */
  estimatedBytes: number;
}

/**
 * Everything the map needs to draw this box with no network: the tiles, the
 * relief, the DEM, both styles, the sprite, the glyph ranges the styles name,
 * the runtime config and the overlays that are switched on.
 */
export function planArea(bounds: Bounds, terrainUrl: string, layerNames: string[]): AreaPlan {
  const b = padBounds(bounds);
  const urls: string[] = [];

  for (const z of BASEMAP_ZOOMS) {
    for (const [x, y] of tilesIn(b, z)) urls.push(abs(`/map/tiles/${z}/${x}/${y}.pbf`));
  }
  for (const z of RELIEF_ZOOMS) {
    for (const [x, y] of tilesIn(b, z)) urls.push(abs(`/map/natural_earth/${z}/${x}/${y}.png`));
  }
  if (terrainUrl) {
    // Somebody else's host, so `abs` only normalises it here — which is what
    // matters: the cache keys these against the same normalised form.
    for (const z of DEM_ZOOMS) {
      for (const [x, y] of tilesIn(b, z)) urls.push(abs(fill(terrainUrl, z, x, y)));
    }
  }
  const tiles = urls.length;

  urls.push(abs('/map/styles/light.json'), abs('/map/styles/dark.json'));
  for (const s of SPRITES) urls.push(abs(`/map/sprites/${s}`));
  for (const stack of FONT_STACKS) {
    // MapLibre puts the stack in the path unencoded and lets the URL
    // constructor escape it; `new URL` here does exactly the same.
    for (const range of FONT_RANGES) urls.push(abs(`/map/fonts/${stack}/${range}.pbf`));
  }
  urls.push(abs('/api/config'));
  for (const name of layerNames) urls.push(abs(`/api/layers/${name}`));

  return { urls, tiles, estimatedBytes: tiles * TILE_BYTES };
}

export interface AreaProgress {
  done: number;
  total: number;
  bytes: number;
}

export interface AreaResult {
  stored: number;
  failed: number;
  bytes: number;
  /** What is in the cache for this area, including what was already there. */
  urls: string[];
  aborted: boolean;
}

/** Six at a time: enough to fill a home line, few enough not to starve the map. */
const CONCURRENCY = 6;

/**
 * Fetch a plan into the area cache. Individual failures are counted, not
 * thrown: one missing tile is a blank square, and stopping over it would cost
 * the person the other nine hundred.
 */
export async function downloadArea(
  urls: string[],
  onProgress: (p: AreaProgress) => void,
  signal?: AbortSignal,
): Promise<AreaResult> {
  const cache = await caches.open(AREA_CACHE);
  const already = new Set((await cache.keys()).map((r) => r.url));
  const todo = urls.filter((u) => !already.has(u));

  const result: AreaResult = { stored: 0, failed: 0, bytes: 0, urls, aborted: false };
  let done = urls.length - todo.length;
  let next = 0;
  onProgress({ done, total: urls.length, bytes: 0 });

  const worker = async () => {
    for (;;) {
      if (signal?.aborted) return;
      const i = next++;
      if (i >= todo.length) return;
      const url = todo[i];
      try {
        const res = await fetch(url, { signal, credentials: 'same-origin' });
        // 204 is what the basemap answers where it holds nothing; keeping it
        // is what stops the map asking the network for it again offline.
        if (res.ok) {
          const body = await res.clone().arrayBuffer();
          await cache.put(url, res);
          result.stored++;
          result.bytes += body.byteLength;
        } else {
          result.failed++;
        }
      } catch {
        if (signal?.aborted) return;
        result.failed++;
      }
      done++;
      onProgress({ done, total: urls.length, bytes: result.bytes });
    }
  };

  await Promise.all(Array.from({ length: CONCURRENCY }, worker));
  result.aborted = !!signal?.aborted;
  return result;
}

/** What is in the cache already, so a half-finished area can be measured. */
export async function areaBytes(urls: string[]): Promise<{ held: number; bytes: number }> {
  try {
    const cache = await caches.open(AREA_CACHE);
    let held = 0;
    let bytes = 0;
    for (const url of urls) {
      const res = await cache.match(url);
      if (!res) continue;
      held++;
      bytes += (await res.arrayBuffer()).byteLength;
    }
    return { held, bytes };
  } catch {
    return { held: 0, bytes: 0 };
  }
}

/**
 * Drop one area's tiles. Two saved routes over the same valley share most of
 * their map, so only what nothing else still needs is actually deleted.
 */
export async function removeArea(rec: SavedRoute, others: SavedRoute[]): Promise<void> {
  try {
    const cache = await caches.open(AREA_CACHE);
    const keep = new Set<string>();
    for (const o of others) if (o.key !== rec.key) for (const u of o.urls) keep.add(u);
    for (const url of rec.urls) if (!keep.has(url)) await cache.delete(url);
  } catch {
    /* no cache storage: there was nothing to remove */
  }
}

/** Everything, tiles and answers alike. */
export async function clearAll(): Promise<void> {
  try {
    await caches.delete(AREA_CACHE);
  } catch {
    /* ignored */
  }
  try {
    await run('readwrite', (s) => s.clear());
  } catch {
    /* ignored */
  }
}
