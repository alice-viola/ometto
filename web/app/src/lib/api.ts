import type {
  Favourite,
  FeatureCollection,
  GeocodeResult,
  Health,
  HistoryEntry,
  Mode,
  ReverseResult,
  RouteRequest,
  RouteResponse,
} from './types';
import { joinLegs } from './geo';
import { userId } from './storage';

/** Fixtures are loaded on demand so they never weigh on the real bundle. */
const mock = () => import('./mock');

/** `?mock=1` swaps the transport for in-app fixtures. The real path is untouched. */
export const MOCK = (() => {
  try {
    return new URLSearchParams(location.search).get('mock') === '1';
  } catch {
    return false;
  }
})();

export class ApiError extends Error {
  constructor(
    readonly status: number,
    message: string,
    /** Seconds the service asked us to wait, from Retry-After. */
    readonly retryAfter?: number,
  ) {
    super(message);
    this.name = 'ApiError';
  }
  /** A sentence for a person, never a status code. */
  get human(): string {
    if (this.status === 0) return 'The routing service is not reachable.';
    if (this.status === 429) {
      const s = this.retryAfter;
      return s && s > 1
        ? `Too many requests, wait a moment — about ${s} ${s === 1 ? 'second' : 'seconds'}.`
        : 'Too many requests, wait a moment.';
    }
    if (this.status === 504) return 'The route took too long to compute. Try again.';
    if (this.status === 503) return 'The service is busy. Try again in a moment.';
    if (this.status === 404) return 'Not available yet.';
    if (this.status >= 500) return 'The routing service had a problem. Try again.';
    return this.message || 'That request could not be completed.';
  }
}

async function request<T>(path: string, init?: RequestInit): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { headers: { accept: 'application/json' }, ...init });
  } catch (e) {
    if ((e as Error)?.name === 'AbortError') throw e;
    throw new ApiError(0, 'network');
  }
  if (!res.ok) {
    let message = '';
    try {
      const body = (await res.json()) as { error?: string; reason?: string };
      message = body?.error || body?.reason || '';
    } catch {
      /* body was not JSON */
    }
    const retry = Number(res.headers.get('retry-after') ?? '');
    throw new ApiError(res.status, message, isFinite(retry) && retry > 0 ? Math.ceil(retry) : undefined);
  }
  if (res.status === 204) return undefined as T;
  return (await res.json()) as T;
}

const qs = (params: Record<string, string | number | undefined>) => {
  const u = new URLSearchParams();
  for (const [k, v] of Object.entries(params)) if (v !== undefined && v !== '') u.set(k, String(v));
  const s = u.toString();
  return s ? `?${s}` : '';
};

export const api = {
  health(): Promise<Health> {
    if (MOCK) return mock().then((k) => k.health());
    return request<Health>('/api/health');
  },

  geocode(q: string, signal?: AbortSignal, limit = 10): Promise<GeocodeResult[]> {
    if (MOCK) return mock().then((k) => k.geocode(q, limit));
    return request<{ results?: GeocodeResult[] }>(`/api/geocode${qs({ q, limit })}`, { signal }).then(
      (r) => r.results ?? [],
    );
  },

  reverse(lat: number, lon: number, mode?: Mode, signal?: AbortSignal): Promise<ReverseResult> {
    if (MOCK) return mock().then((k) => k.reverse(lat, lon));
    return request<ReverseResult>(`/api/reverse${qs({ lat, lon, mode })}`, { signal });
  },

  /**
   * `record` decides whether this question joins the person's history. The
   * service appends whenever a user id is present, so an automatic recompute
   * simply does not send one — otherwise nudging a marker would fill the list
   * with near-duplicates of the same trip.
   */
  route(req: Omit<RouteRequest, 'user'>, signal?: AbortSignal, record = true): Promise<RouteResponse> {
    const body = (record ? { ...req, user: userId() } : { ...req }) as RouteRequest;
    // The wire carries each line once, on the legs; the route's own line is
    // joined here, once, so nothing downstream has to know.
    const joined = (res: RouteResponse) => {
      for (const r of res.routes ?? []) joinLegs(r);
      return res;
    };
    if (MOCK) return mock().then((k) => k.route(body)).then(joined);
    return request<RouteResponse>('/api/route', {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json' },
      body: JSON.stringify(body),
      signal,
    }).then(joined);
  },

  favourites(): Promise<Favourite[]> {
    if (MOCK) return mock().then((k) => k.favourites());
    return request<{ favorites?: Favourite[] }>(`/api/favorites${qs({ user: userId() })}`).then(
      (r) => r.favorites ?? [],
    );
  },

  addFavourite(f: { name: string; lat: number; lon: number; kind?: string }): Promise<Favourite> {
    if (MOCK) return mock().then((k) => k.addFavourite(f));
    return request<Favourite>('/api/favorites', {
      method: 'POST',
      headers: { 'content-type': 'application/json', accept: 'application/json' },
      body: JSON.stringify({ ...f, user: userId() }),
    });
  },

  removeFavourite(id: string): Promise<void> {
    if (MOCK) return mock().then((k) => k.removeFavourite(id));
    return request<void>(`/api/favorites/${encodeURIComponent(id)}${qs({ user: userId() })}`, {
      method: 'DELETE',
    });
  },

  history(limit = 50): Promise<HistoryEntry[]> {
    if (MOCK) return mock().then((k) => k.history());
    return request<{ history?: HistoryEntry[] }>(`/api/history${qs({ user: userId(), limit })}`).then(
      (r) => r.history ?? [],
    );
  },

  removeHistory(id: string): Promise<void> {
    if (MOCK) return mock().then((k) => k.removeHistory(id));
    return request<void>(`/api/history/${encodeURIComponent(id)}${qs({ user: userId() })}`, {
      method: 'DELETE',
    });
  },

  clearHistory(): Promise<void> {
    if (MOCK) return mock().then((k) => k.clearHistory());
    return request<void>(`/api/history${qs({ user: userId() })}`, { method: 'DELETE' });
  },

  /** In fixture mode the browser keeps the history the service would keep. */
  noteHistory(entry: HistoryEntry): void {
    if (!MOCK) return;
    void mock().then((k) => k.recordHistory(entry));
  },

  layer(name: 'region' | 'sat' | 'pois' | 'huts' | 'lifts'): Promise<FeatureCollection> {
    if (MOCK) return mock().then((k) => k.layer(name));
    return request<FeatureCollection>(`/api/layers/${name}`);
  },
};
