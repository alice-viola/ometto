/// <reference lib="webworker" />

/**
 * What answers when there is no network.
 *
 * The routing engine is a 1.1 GB graph on the server, so this can never plan
 * a new trip. It does three things: it keeps the app itself on the device, it
 * serves the map of an area somebody downloaded, and it stays out of the way
 * of everything else — `/api/route` is answered in the page, out of the saved
 * routes store, because that is where the query is known.
 */
import { cleanupOutdatedCaches, createHandlerBoundToURL, precacheAndRoute } from 'workbox-precaching';
import { NavigationRoute, registerRoute } from 'workbox-routing';
import { CacheFirst, NetworkFirst, NetworkOnly } from 'workbox-strategies';
import { ExpirationPlugin } from 'workbox-expiration';
import { clientsClaim } from 'workbox-core';
import type { RouteHandlerCallback } from 'workbox-core/types';

declare let self: ServiceWorkerGlobalScope;

/** Filled by the page, area by area. Nothing here ever evicts it. */
const OFFLINE = 'ometto-offline-v1';
/** Whatever the map happened to draw, kept in case it is drawn again. */
const MAP = 'ometto-map-v1';
/** Styles, config, overlays: small, and stale is worse than late. */
const DATA = 'ometto-data-v1';

const MONTH = 30 * 24 * 60 * 60;

precacheAndRoute(self.__WB_MANIFEST);
cleanupOutdatedCaches();

// The very first worker takes the open page over as soon as it is installed,
// so a route saved on the first visit gets its map cached there and then. A
// *later* worker still waits: it only activates once the reload pill is
// pressed, which is the SKIP_WAITING message at the bottom of this file.
clientsClaim();

// Every address this app invents is the same page. The API, the basemap and
// the metrics are not pages and must never be answered with one.
registerRoute(
  new NavigationRoute(createHandlerBoundToURL('index.html'), {
    denylist: [/^\/api\//, /^\/map\//, /^\/metrics$/],
  }),
);

const mapStrategy = new CacheFirst({
  cacheName: MAP,
  plugins: [new ExpirationPlugin({ maxEntries: 2000, maxAgeSeconds: MONTH, purgeOnQuotaError: true })],
});

/**
 * Before the first download there is no such bucket, and asking for one that
 * does not exist is allowed to fail. Nothing about the map may hang on that.
 */
async function fromArea(request: Request): Promise<Response | undefined> {
  try {
    return await caches.match(request, { cacheName: OFFLINE, ignoreSearch: true });
  } catch {
    return undefined;
  }
}

/**
 * A downloaded area is the truth about that area: it is looked at first and
 * it never expires, so the tiles someone asked to keep cannot be thrown away
 * to make room for the tiles the map drew on the way there.
 */
const fromAreaElse =
  (fallback: RouteHandlerCallback): RouteHandlerCallback =>
  async (options) => {
    const saved = await fromArea(options.request);
    if (saved) return saved;
    return fallback(options);
  };

const dataStrategy = new NetworkFirst({
  cacheName: DATA,
  networkTimeoutSeconds: 4,
  plugins: [new ExpirationPlugin({ maxEntries: 40, maxAgeSeconds: MONTH, purgeOnQuotaError: true })],
});

// The basemap, its glyphs and its sprites, and the relief. A tile is the same
// tile forever, so the fastest answer is the right one.
registerRoute(
  ({ url, sameOrigin }) =>
    sameOrigin
      ? /^\/map\/(tiles|fonts|sprites|natural_earth)\//.test(url.pathname)
      : // The only cross-origin address the page is allowed to reach is the
        // terrain host the config names, and everything it asks it for is a
        // DEM tile: hillshade, terrain and the contour worker all share them.
        /\/\d+\/\d+\/\d+\.png$/.test(url.pathname),
  fromAreaElse((o) => mapStrategy.handle(o)),
);

// The style, the config and the overlays do change — a new map build moves
// them — so here the network wins while there is one. Unlike a tile, a
// downloaded copy of these is the last resort and not the first: an old style
// over new tiles would be a worse map than a slow one.
registerRoute(
  ({ url, sameOrigin }) =>
    sameOrigin &&
    (url.pathname.startsWith('/map/styles/') ||
      url.pathname.startsWith('/api/layers/') ||
      url.pathname === '/api/config'),
  async (o) => {
    try {
      return await dataStrategy.handle(o);
    } catch (err) {
      const saved = await fromArea(o.request);
      if (saved) return saved;
      throw err;
    }
  },
);

// Everything else the service answers is about right now: a route, a search,
// a favourite. A cached one would be a lie.
registerRoute(({ url, sameOrigin }) => sameOrigin && url.pathname.startsWith('/api/'), new NetworkOnly());

/**
 * A new worker waits until the page says so — the pill at the top of the app
 * offers the reload, and this is what that button reaches.
 */
self.addEventListener('message', (event: ExtendableMessageEvent) => {
  if ((event.data as { type?: string } | null)?.type === 'SKIP_WAITING') void self.skipWaiting();
});
