<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, shallowRef, watch } from 'vue';
import maplibregl, { type LngLatLike, type Map as MlMap, type Marker, type MapOptions } from 'maplibre-gl';
import mlcontour from 'maplibre-contour';
import { DEM_MAXZOOM, REGION_BOUNDS, loadStyleSpec, readTokens, type Tokens } from '../map/style';
import { appConfig, loadConfig } from '../lib/config';
import {
  LYR,
  SRC,
  addContours,
  addDem,
  addHillshade,
  addCrags,
  addLifts,
  addPois,
  addRegion,
  addRouteLayers,
  addSat,
  avoidedData,
  routeData,
  hardenStyleFilters,
  setLayerVisible,
  tuneDarkStyle,
} from '../map/layers';
import { markerElement } from '../map/markers';
import { makeFallbackImage } from '../map/icons';
import { api } from '../lib/api';
import { cragDetail } from '../lib/format';
import { available, layers } from '../composables/useLayers';
import { isDark } from '../composables/useTheme';
import {
  frameRequest,
  hasHover,
  isCompact,
  panelHidden,
  sheetDragging,
  sheetHeight,
  sheetSettled,
  topInset,
} from '../composables/useMedia';
import { hoverPoint } from '../composables/useHover';
import {
  avoidedWays,
  insertVia,
  parkingIndex,
  placeIndices,
  answeredToken,
  resultToken,
  routes,
  selectRoute,
  selected,
  selectedId,
  slots,
} from '../composables/usePlanner';
import {
  boundsOf,
  cumulative,
  extendBounds,
  nearestOnLine,
  type Bounds,
  type Coord,
} from '../lib/geo';
import type { FeatureCollection, Waypoint } from '../lib/types';
import MapPopover from './MapPopover.vue';
import type { PopoverState } from '../map/popover';
import FirstVisitHint from './FirstVisitHint.vue';

function closePopover() {
  popover.value = null;
  // The phone layout marks the pressed point with the ghost dot; take it away with the sheet.
  if (map.value) setGhost(map.value, null);
}

/**
 * Reshaping the answer by hand. A press that lands on the drawn route — and
 * only there — becomes a drag that carries a via dot under the pointer; the
 * map itself must not pan while it does, or the gesture fights the map.
 * Where the press landed decides which pair of request points the via falls
 * between, so the drop has a position in the sequence and not merely a place.
 */
let routeDrag: { along: number; from: maplibregl.Point; moved: boolean } | null = null;
let suppressClick = false;
/** Below this the gesture was a tap on the line, not a reshape of it. */
const DRAG_SLOP = 6;

function setGhost(m: MlMap, lngLat: [number, number] | null) {
  const src = m.getSource(SRC.drag) as maplibregl.GeoJSONSource | undefined;
  src?.setData({
    type: 'FeatureCollection',
    features: lngLat
      ? [{ type: 'Feature', properties: {}, geometry: { type: 'Point', coordinates: lngLat } }]
      : [],
  } as never);
}

function grabAlong(lngLat: [number, number]): number | null {
  const coords = (selected.value?.geometry?.coordinates ?? []) as Coord[];
  if (coords.length < 2) return null;
  return nearestOnLine(coords, cumulative(coords), lngLat)?.along ?? null;
}

function beginRouteDrag(m: MlMap, e: maplibregl.MapMouseEvent | maplibregl.MapTouchEvent) {
  // A finger never reshapes the route. On a phone the first finger of a
  // pinch, or a pan that happens to start on the line, landed here, took the
  // gesture away from the map and dropped a via where the fingers parted — so
  // zooming in on a route moved it. Touch bends a route through the tap
  // popover ("Route via here"); the drag is a mouse gesture.
  if ('touches' in e.originalEvent) return false;
  if (!selected.value || !m.getLayer(LYR.routeHit)) return false;
  const hits = m.queryRenderedFeatures(e.point, { layers: [LYR.routeHit] });
  if (!hits.length) return false;
  const along = grabAlong([e.lngLat.lng, e.lngLat.lat]);
  if (along === null) return false;
  e.preventDefault(); // the map must not pan under the gesture
  routeDrag = { along, from: e.point, moved: false };
  m.getCanvas().style.cursor = 'grabbing';
  return true;
}

function moveRouteDrag(m: MlMap, e: maplibregl.MapMouseEvent | maplibregl.MapTouchEvent) {
  if (!routeDrag) return;
  e.preventDefault();
  if (!routeDrag.moved) {
    const dx = e.point.x - routeDrag.from.x;
    const dy = e.point.y - routeDrag.from.y;
    if (Math.hypot(dx, dy) < DRAG_SLOP) return;
    routeDrag.moved = true;
    suppressClick = true;
    // The ghost now belongs to the drag, not to an open popover.
    closePopover();
  }
  setGhost(m, [e.lngLat.lng, e.lngLat.lat]);
}

function endRouteDrag(m: MlMap, e: maplibregl.MapMouseEvent | maplibregl.MapTouchEvent) {
  if (!routeDrag) return;
  const { along, moved } = routeDrag;
  routeDrag = null;
  m.getCanvas().style.cursor = '';
  setGhost(m, null);
  if (!moved) {
    // A press that never travelled is a click on whatever lies under the
    // route, not a request to bend it.
    suppressClick = false;
    return;
  }
  insertVia(e.lngLat.lat, e.lngLat.lng, along);
  // The click that ends the drag must not also open the popover.
  window.setTimeout(() => (suppressClick = false), 60);
}

const host = ref<HTMLDivElement | null>(null);
const map = shallowRef<MlMap | null>(null);
const ready = ref(false);
/**
 * Why there is no map, when there is none. The map is WebGL, and a browser
 * whose GPU process is off — Chrome after a bad update, a locked-down
 * profile — cannot make a context: MapLibre throws from its constructor.
 * That is no reason for the page to stop. The panel still asks and answers;
 * the map's place says what is wrong and how to fix it.
 */
const mapUnavailable = ref('');
function createMap(options: MapOptions): MlMap | null {
  try {
    return new maplibregl.Map(options);
  } catch (e) {
    mapUnavailable.value = String((e as Error)?.message ?? e);
    console.warn('ometto: the map cannot start:', mapUnavailable.value);
    return null;
  }
}
const popover = ref<PopoverState | null>(null);
let tokens: Tokens = readTokens();
let markers: Marker[] = [];
let dragging = false;

// One DEM pipeline shared by hillshade, terrain and contours. Built after the
// config answers, because the elevation tiles' address comes from it.
let demSource: InstanceType<typeof mlcontour.DemSource> | null = null;
let contourTiles = '';

function buildDem(url: string) {
  if (demSource) return;
  demSource = new mlcontour.DemSource({
    url,
    encoding: 'terrarium',
    maxzoom: DEM_MAXZOOM,
    worker: true,
  });
  demSource.setupMaplibre(maplibregl);
  contourTiles = demSource.contourProtocolUrl({
    thresholds: { 10: [200, 1000], 11: [100, 500], 12: [100, 500], 13: [50, 100], 14: [50, 100] },
    elevationKey: 'ele',
    levelKey: 'level',
    contourLayer: 'contours',
    overzoom: 1,
  });
}

// Overlay data is fetched once and re-applied to every style.
let regionData: FeatureCollection | null = null;
let satData: FeatureCollection | null = null;
let poiData: FeatureCollection | null = null;
let liftData: FeatureCollection | null = null;
let cragData: FeatureCollection | null = null;

type Overlay = 'region' | 'sat' | 'pois' | 'lifts' | 'crags';
const pending: Partial<Record<Overlay, Promise<FeatureCollection | null>>> = {};

/** Fetched at most once each; the trail and summit sets are megabytes. */
function loadOverlay(name: Overlay): Promise<FeatureCollection | null> {
  return (pending[name] ??= api
    .layer(name)
    .then((fc) => (fc?.features ? fc : null))
    .catch(() => null));
}

async function ensureOverlay(name: 'sat' | 'pois' | 'lifts' | 'crags'): Promise<void> {
  const fc = await loadOverlay(name);
  if (name === 'sat') {
    satData = fc;
    available.sat = !!fc;
  } else if (name === 'pois') {
    poiData = fc;
    available.pois = !!fc;
  } else if (name === 'crags') {
    cragData = fc;
    available.crags = !!fc;
  } else {
    liftData = fc;
    available.lifts = !!fc;
  }
  const m = map.value;
  if (!m || !ready.value || !fc) return;
  if (name === 'sat') addSat(m, fc, tokens);
  else if (name === 'pois') addPois(m, fc, tokens);
  else if (name === 'crags') addCrags(m, fc, tokens);
  else addLifts(m, fc, tokens);
  applyVisibility(m);
}

/**
 * The panel sits beside the map on desktop and over it on a phone. The phone
 * band also has to clear the zoom buttons at the top right, a notch above
 * them, and the scale bar and attribution that ride just above the sheet.
 */
function padding() {
  const el = host.value;
  const w = el?.clientWidth ?? window.innerWidth;
  const h = el?.clientHeight ?? window.innerHeight;
  // On a phone the search card floats over the top of the map and the sheet
  // over the bottom; the answer is framed in the band between them.
  const pad = isCompact.value
    ? { top: topInset.value + 24, right: 24, bottom: sheetHeight.value + 36, left: 24 }
    : { top: 56, right: 56, bottom: 56, left: 56 };
  // fitBounds rejects padding that leaves no room: keep a viewport to aim at.
  // What is limited is the pair, not each side — the sheet alone may cover
  // well over half the height, and the band above it is still the target.
  // A container that has no room yet (the map is built before a pane has
  // settled its size) gets no padding at all, not a floor: MapLibre warns and
  // gives up on any padding wider than the canvas, and the region is framed
  // again once the layout exists.
  const fit = (a: number, b: number, room: number): [number, number] => {
    const free = Math.max(0, room - 120);
    const k = a + b > free ? free / (a + b) : 1;
    return [Math.floor(a * k), Math.floor(b * k)];
  };
  const [top, bottom] = fit(pad.top, pad.bottom, h);
  const [left, right] = fit(pad.left, pad.right, w);
  return { top, bottom, left, right };
}

function applyCustom(m: MlMap) {
  tokens = readTokens();
  hardenStyleFilters(m);
  // Once the styles are served from /map/ these corrections are already baked
  // into them; patching in place then would only undo someone else's work.
  if (isDark.value && appConfig().fallback) tuneDarkStyle(m);
  addDem(m, demSource!.sharedDemProtocolUrl, appConfig().terrain.attribution);
  addHillshade(m, isDark.value);
  // Contours are off by default: adding their source anyway spun up a worker
  // and a vector source nothing was reading.
  if (layers.contours) addContours(m, contourTiles, tokens, true);
  if (regionData) addRegion(m, regionData, tokens, isDark.value);
  if (satData) addSat(m, satData, tokens);
  if (poiData) addPois(m, poiData, tokens);
  if (liftData) addLifts(m, liftData, tokens);
  if (cragData) addCrags(m, cragData, tokens);
  addRouteLayers(m, tokens);
  applyVisibility(m);
  applyTerrain(m);
  pushRoutes();
  pushAvoided();
  pushHover();
  rebuildMarkers();
}

function applyVisibility(m: MlMap) {
  setLayerVisible(m, [LYR.sat, LYR.satLabel], layers.sat && available.sat);
  setLayerVisible(m, [LYR.poi, LYR.poiPlace], layers.pois && available.pois);
  setLayerVisible(m, [LYR.crag, LYR.cragSector], layers.crags && available.crags);
  setLayerVisible(m, [LYR.lifts, LYR.liftsLabel], layers.lifts && available.lifts);
  setLayerVisible(m, [LYR.contour, LYR.contourLabel], layers.contours);
}

/**
 * Terrain is for where the relief can be seen. Below this zoom the map is a
 * flat plane again, whatever the setting says: tilted and zoomed out far
 * enough, MapLibre shows the edge of the world as a thick slab with the page's
 * ground colour behind it, which reads as a broken map. The setting stays on;
 * the tilt and the relief come back on the way in.
 */
const TERRAIN_MIN_ZOOM = 7;
/** What the map has right now, as opposed to what the setting asks for. */
let terrainOn = false;

function applyTerrain(m: MlMap) {
  // setTerrain throws "Style is not done loading" when it is called before the
  // style is up — which a zoomend during the opening fitBounds does. The style
  // load applies the terrain itself, so skipping here loses nothing.
  if (!m.isStyleLoaded()) return;
  const want = layers.terrain && m.getZoom() >= TERRAIN_MIN_ZOOM;
  if (want === terrainOn && !!m.getTerrain() === want) return;
  terrainOn = want;
  if (want) {
    m.setTerrain({ source: SRC.demTerrain, exaggeration: 1.3 });
    if (m.getPitch() < 30) m.easeTo({ pitch: 58, duration: 700 });
  } else {
    m.setTerrain(null);
    if (m.getPitch() > 1) m.easeTo({ pitch: 0, duration: 500 });
  }
}

function pushRoutes() {
  const m = map.value;
  if (!m || !m.getSource(SRC.route)) return;
  const { alt, selected: sel, stations } = routeData(routes.value, selectedId.value);
  (m.getSource(SRC.alt) as maplibregl.GeoJSONSource).setData(alt as never);
  (m.getSource(SRC.route) as maplibregl.GeoJSONSource).setData(sel as never);
  (m.getSource(SRC.stations) as maplibregl.GeoJSONSource).setData(stations as never);
}

function pushAvoided() {
  const m = map.value;
  if (!m || !m.getSource(SRC.avoided)) return;
  (m.getSource(SRC.avoided) as maplibregl.GeoJSONSource).setData(
    avoidedData(avoidedWays.value) as never,
  );
}

function pushHover() {
  const m = map.value;
  if (!m || !m.getSource(SRC.hover)) return;
  const p = hoverPoint.value;
  (m.getSource(SRC.hover) as maplibregl.GeoJSONSource).setData({
    type: 'FeatureCollection',
    features: p ? [{ type: 'Feature', properties: {}, geometry: { type: 'Point', coordinates: p } }] : [],
  } as never);
}

function rebuildMarkers() {
  const m = map.value;
  if (!m || dragging) return;
  for (const mk of markers) mk.remove();
  markers = [];
  const filled = slots.value.map((s, i) => ({ slot: s, index: i })).filter((x) => x.slot.point);
  const places = placeIndices.value.filter((i) => slots.value[i].point);
  filled.forEach((entry) => {
    const p = entry.slot.point as Waypoint;
    const placeAt = places.indexOf(entry.index);
    const role = p.via
      ? 'via'
      : placeAt === 0
        ? 'start'
        : placeAt === places.length - 1
          ? 'destination'
          : entry.index === parkingIndex.value
            ? 'parking'
            : 'stop';
    const order = placeAt >= 0 ? placeAt : 0;
    // A finger needs more to hold than a cursor does.
    const el = markerElement(role, tokens, order, !hasHover.value);
    el.setAttribute('role', 'button');
    el.setAttribute('tabindex', '0');
    const what =
      role === 'start'
        ? 'Start'
        : role === 'destination'
          ? 'Destination'
          : role === 'parking'
            ? 'Where you park'
            : role === 'via'
              ? 'Via point'
              : `Stop ${order}`;
    el.setAttribute('aria-label', `${what}: ${p.name ?? 'dropped point'}. Drag to move.`);
    if (role === 'via') {
      el.addEventListener('click', (ev) => {
        ev.stopPropagation();
        openViaPopover(entry.index, p.lon, p.lat);
      });
    }
    const marker = new maplibregl.Marker({
      element: el,
      draggable: true,
      anchor: role === 'destination' ? 'bottom' : 'center',
    })
      .setLngLat([p.lon, p.lat])
      .addTo(m);
    marker.on('dragstart', () => (dragging = true));
    marker.on('dragend', () => {
      const ll = marker.getLngLat();
      entry.slot.point = { ...p, lat: ll.lat, lon: ll.lng, name: p.name };
      dragging = false;
      // A via has no name to lose and no note to earn: it is only a shape.
      if (!p.via) void renameAfterDrag(entry.index, ll.lat, ll.lng);
    });
    markers.push(marker);
  });
}

async function renameAfterDrag(index: number, lat: number, lon: number) {
  try {
    const r = await api.reverse(lat, lon);
    const slot = slots.value[index];
    if (slot?.point && Math.abs(slot.point.lat - lat) < 1e-9) {
      slot.point = { ...slot.point, name: r.name, kind: r.kind };
    }
  } catch {
    /* a point without a name is still a point */
  }
}

/**
 * On a phone every frame is made top-down. fitBounds knows nothing of pitch:
 * with the terrain's tilt on, the ground near the bottom edge is magnified
 * and the answer spills under the sheet and the card, where the band between
 * them is the whole point. The relief stays; the tilt comes back with two
 * fingers, or the next time the terrain is turned on. The desktop keeps its
 * tilt: its padding is even, and nothing floats over the map there.
 */
const flat = () => (isCompact.value ? { pitch: 0 } : {});

function fitToResult() {
  const m = map.value;
  const route = selected.value;
  if (!m || !route) return;
  let b: Bounds | null = null;
  for (const r of routes.value) b = extendBounds(b, boundsOf(r.geometry.coordinates as Coord[]));
  // The pins too: a destination kept 700 m from where the road ends is still
  // part of the answer, and must not end up under the search card.
  for (const s of slots.value) {
    if (s.point) b = extendBounds(b, [s.point.lon, s.point.lat, s.point.lon, s.point.lat]);
  }
  if (!b) return;
  m.fitBounds(
    [
      [b[0], b[1]],
      [b[2], b[3]],
    ],
    { padding: padding(), duration: 700, maxZoom: 15, ...flat() },
  );
}

/** With no route to frame, still keep every point the user set in view. */
function fitToPoints(force = false) {
  const m = map.value;
  if (!m || (routes.value.length && !force)) return;
  const pts = slots.value.map((s) => s.point).filter(Boolean);
  if (pts.length < 1) return;
  const b = boundsOf(pts.map((p) => [p!.lon, p!.lat] as Coord));
  if (!b) return;
  const view = m.getBounds();
  const inside =
    b[0] >= view.getWest() && b[2] <= view.getEast() && b[1] >= view.getSouth() && b[3] <= view.getNorth();
  // Leave the view alone only when the points are both visible and filling it:
  // two pins lost in a region-wide map should be framed, not merely contained.
  const fills =
    (b[2] - b[0]) / Math.max(1e-9, view.getEast() - view.getWest()) > 0.28 ||
    (b[3] - b[1]) / Math.max(1e-9, view.getNorth() - view.getSouth()) > 0.28;
  if (!force && inside && fills) return;
  if (b[0] === b[2] && b[1] === b[3]) {
    m.easeTo({ center: [b[0], b[1]], zoom: Math.max(m.getZoom(), 12), duration: 600, ...flat() });
    return;
  }
  m.fitBounds(
    [
      [b[0], b[1]],
      [b[2], b[3]],
    ],
    { padding: padding(), duration: 600, maxZoom: 14, ...flat() },
  );
}

/**
 * The constructor's fitBounds runs before the phone layout has settled, which
 * left the opening view somewhere over Bavaria. Frame the region again once
 * the style is up and nothing else has claimed the view.
 */
function fitRegion() {
  const m = map.value;
  if (!m || routes.value.length || slots.value.some((s) => s.point)) return;
  m.fitBounds(
    [
      [REGION_BOUNDS[0], REGION_BOUNDS[1]],
      [REGION_BOUNDS[2], REGION_BOUNDS[3]],
    ],
    { padding: padding(), duration: 0, ...flat() },
  );
}

function syncPopover() {
  const m = map.value;
  if (!m || !popover.value) return;
  const pt = m.project(popover.value.lngLat as LngLatLike);
  popover.value.x = pt.x;
  popover.value.y = pt.y;
}

function openViaPopover(index: number, lng: number, lat: number) {
  const m = map.value;
  if (!m) return;
  const pt = m.project([lng, lat]);
  popover.value = { lngLat: [lng, lat], x: pt.x, y: pt.y, name: 'Via point', loading: false, viaIndex: index };
}

/**
 * What is under the click, if anything worth acting on: one of our own layers
 * first, then the base map's roads, paths and summits. The tiles are the only
 * place a road's name exists, so they are asked rather than guessed at.
 */
const OUR_FEATURE_LAYERS = [
  LYR.avoided,
  LYR.avoidedHatch,
  LYR.lifts,
  LYR.sat,
  LYR.poi,
  LYR.poiPlace,
  LYR.crag,
  LYR.cragSector,
];
const BASE_SOURCE_LAYERS = ['transportation', 'transportation_name', 'poi', 'mountain_peak'];

function featureAt(m: MlMap, point: maplibregl.Point) {
  const box: [maplibregl.PointLike, maplibregl.PointLike] = [
    [point.x - 6, point.y - 6],
    [point.x + 6, point.y + 6],
  ];
  const ours = m.queryRenderedFeatures(box, {
    layers: OUR_FEATURE_LAYERS.filter((id) => m.getLayer(id)),
  });
  const first = ours[0];
  if (first) {
    const p = (first.properties ?? {}) as Record<string, unknown>;
    const layer = first.layer.id;
    if (layer === LYR.avoided || layer === LYR.avoidedHatch) {
      return { name: String(p.name || 'that way'), kind: 'avoided', avoidedId: String(p.id ?? '') };
    }
    if (layer === LYR.crag || layer === LYR.cragSector) {
      // A sector is named after its wall, as it is in the search results.
      const own = String(p.name || 'Crag');
      return { name: p.parent ? `${own} (${String(p.parent)})` : own, kind: 'crag', detail: cragDetail(p) };
    }
    const kind =
      layer === LYR.lifts ? 'lift' : layer === LYR.sat ? 'trail' : String(p.kind ?? 'place');
    const name = String(p.name || p.numero || kind);
    return { name, kind };
  }

  for (const f of m.queryRenderedFeatures(box)) {
    const sl = (f as { sourceLayer?: string }).sourceLayer;
    if (!sl || !BASE_SOURCE_LAYERS.includes(sl)) continue;
    const p = (f.properties ?? {}) as Record<string, unknown>;
    const cls = String(p.class ?? p.subclass ?? sl);
    const name = String(p.name || cls).replace(/_/g, ' ');
    return { name, kind: cls };
  }
  return null;
}

async function openPopover(lng: number, lat: number) {
  const m = map.value;
  if (!m) return;
  const pt = m.project([lng, lat]);
  const hit = featureAt(m, pt);
  // The phone popover is a sheet at the bottom, with no arrow to point: the
  // ghost dot says where the press landed instead.
  if (isCompact.value) setGhost(m, [lng, lat]);
  popover.value = {
    lngLat: [lng, lat],
    x: pt.x,
    y: pt.y,
    name: hit?.name ?? '',
    loading: !hit,
    ...(hit && hit.kind !== 'avoided'
      ? { feature: { name: hit.name, kind: hit.kind, ...(hit.detail ? { detail: hit.detail } : {}) } }
      : {}),
    ...(hit?.kind === 'avoided' ? { avoidedId: hit.avoidedId } : {}),
  };
  if (hit) return;
  try {
    const r = await api.reverse(lat, lng);
    if (popover.value?.lngLat[0] === lng) popover.value = { ...popover.value, name: r.name, loading: false };
  } catch {
    if (popover.value?.lngLat[0] === lng)
      popover.value = { ...popover.value, name: `${lat.toFixed(4)}, ${lng.toFixed(4)}`, loading: false };
  }
}

onMounted(async () => {
  if (!host.value) return;
  const cfg = await loadConfig();
  buildDem(cfg.terrain.url);
  const startStyle = await loadStyleSpec(isDark.value ? cfg.styles.dark : cfg.styles.light);
  if (!host.value) return;
  const m = createMap({
    container: host.value,
    style: startStyle as never,
    bounds: [
      [REGION_BOUNDS[0], REGION_BOUNDS[1]],
      [REGION_BOUNDS[2], REGION_BOUNDS[3]],
    ],
    fitBoundsOptions: { padding: padding() },
    maxZoom: 19,
    // One region: there is nothing to plan a thousand kilometres out, and a
    // map zoomed out to the world is where the terrain shows its edge.
    minZoom: 5,
    attributionControl: { compact: true },
    /**
     * The self-hosted style names its tiles, glyphs and sprite by path, which
     * is right: the same file has to work on any host. MapLibre needs absolute
     * URLs, so they are resolved here against wherever the app is being served
     * from — the one place that knows.
     */
    transformRequest: (url: string) =>
      url.startsWith('/') ? { url: new URL(url, location.origin).toString() } : { url },
    dragRotate: true,
    pitchWithRotate: true,
  });
  if (!m) return;
  map.value = m;
  // A handle for checking the map from the console; harmless in production
  // and the only way to measure a frame on a phone.
  (window as unknown as { __map?: MlMap }).__map = m;
  m.addControl(new maplibregl.NavigationControl({ visualizePitch: true, showCompass: true }), 'top-right');
  m.addControl(new maplibregl.ScaleControl({ maxWidth: 96, unit: 'metric' }), 'bottom-left');
  m.keyboard.enable();

  // A style may reference an image its sprite does not hold; supply one rather
  // than let it warn on every load.
  m.on('styleimagemissing', (e: { id: string }) => {
    if (!e?.id || m.hasImage(e.id)) return;
    m.addImage(e.id, makeFallbackImage(e.id, tokens.muted), { pixelRatio: 2 });
  });

  m.on('style.load', () => {
    applyCustom(m);
    ready.value = true;
    setTimeout(fitRegion, 50);
  });
  m.on('move', syncPopover);
  m.on('click', (e) => {
    if (suppressClick) return;
    const hits = m.queryRenderedFeatures(e.point, { layers: [LYR.altHit].filter((id) => m.getLayer(id)) });
    if (hits.length) {
      const id = hits[0].properties?.id as string | undefined;
      if (id) {
        selectRoute(id);
        return;
      }
    }
    void openPopover(e.lngLat.lng, e.lngLat.lat);
  });
  m.on('dragstart', closePopover);
  m.on('zoomstart', closePopover);
  m.on('zoomend', () => applyTerrain(m));

  m.on('mousedown', (e) => beginRouteDrag(m, e));
  m.on('touchstart', (e) => beginRouteDrag(m, e));
  m.on('mousemove', (e) => moveRouteDrag(m, e));
  m.on('touchmove', (e) => moveRouteDrag(m, e));
  m.on('mouseup', (e) => endRouteDrag(m, e));
  m.on('touchend', (e) => endRouteDrag(m, e));
  m.on('mouseenter', LYR.routeHit, () => {
    if (!routeDrag) m.getCanvas().style.cursor = 'grab';
  });
  m.on('mouseleave', LYR.routeHit, () => {
    if (!routeDrag) m.getCanvas().style.cursor = '';
  });
  m.on('mouseenter', LYR.altHit, () => (m.getCanvas().style.cursor = 'pointer'));
  m.on('mouseleave', LYR.altHit, () => (m.getCanvas().style.cursor = ''));

  const region = await loadOverlay('region');
  regionData = region;
  available.region = !!region;
  if (ready.value) applyCustom(m);
  if (layers.sat) void ensureOverlay('sat');
  if (layers.pois) void ensureOverlay('pois');
  if (layers.lifts) void ensureOverlay('lifts');
  if (layers.crags) void ensureOverlay('crags');
});

function onEscape(e: KeyboardEvent) {
  if (e.key !== 'Escape' && e.key !== 'Esc') return;
  const m = map.value;
  if (routeDrag && m) {
    // Abandon the reshape rather than drop a via nobody asked for.
    routeDrag = null;
    setGhost(m, null);
    m.getCanvas().style.cursor = '';
    suppressClick = false;
    e.preventDefault();
    return;
  }
  if (!popover.value) return;
  e.preventDefault(); // handled here; the panel toggle must not also fire
  closePopover();
}
window.addEventListener('keydown', onEscape);

onBeforeUnmount(() => {
  window.removeEventListener('keydown', onEscape);
  for (const mk of markers) mk.remove();
  map.value?.remove();
});

/**
 * The map just changed shape: the panel folded away, or the layout crossed the
 * phone breakpoint. Resize, then put the answer back into the space there now
 * is — twice, because the first pass can land before the layout has settled.
 */
function reframeAfterResize() {
  const m = map.value;
  if (!m) return;
  const settle = () => {
    m.resize();
    if (routes.value.length) fitToResult();
    else if (slots.value.some((s) => s.point)) fitToPoints(true);
    else fitRegion();
  };
  setTimeout(settle, 60);
  setTimeout(settle, 400);
}

watch(isCompact, reframeAfterResize);
watch(panelHidden, reframeAfterResize);

// The phone's "show the whole route" button, and anything else that asks.
watch(frameRequest, () => {
  const m = map.value;
  if (!m) return;
  if (routes.value.length) fitToResult();
  else if (slots.value.some((s) => s.point)) fitToPoints(true);
  else fitRegion();
});

// Dragging the sheet changes how much map there is; put the answer back in it.
watch(sheetSettled, () => {
  const m = map.value;
  if (!m) return;
  setTimeout(() => {
    if (routes.value.length) fitToResult();
    else if (slots.value.some((s) => s.point)) fitToPoints(true);
    else fitRegion();
  }, 300);
});

watch(isDark, (dark) => {
  const m = map.value;
  if (!m) return;
  ready.value = false;
  const cfg = appConfig();
  void loadStyleSpec(dark ? cfg.styles.dark : cfg.styles.light).then((spec) => {
    if (map.value === m) m.setStyle(spec as never, { diff: false });
  });
});
watch([routes, selectedId], pushRoutes, { deep: false });
/**
 * The map follows a QUESTION, not every answer.
 *
 * Asking something new — Compute, a shared link, a favourite, a history row —
 * frames the answer. An automatic re-run does not: changing the grade or the
 * mode re-computes silently, and re-framing there threw away the view the
 * person had just panned and zoomed to, so every click on a chip looked like
 * the map zooming out and back in. The one exception is an answer that would
 * otherwise be off screen: a route that moved to another valley is worth
 * showing, and is what the re-frame was for.
 */
watch(answeredToken, () => setTimeout(fitToResult, 30));
watch(resultToken, () =>
  setTimeout(() => {
    if (!routeIsVisible()) fitToResult();
  }, 30),
);

/** Is the whole selected route inside what the map is showing right now? */
function routeIsVisible(): boolean {
  const m = map.value;
  const route = selected.value;
  if (!m || !route) return false;
  let b: Bounds | null = null;
  for (const r of routes.value) b = extendBounds(b, boundsOf(r.geometry.coordinates as Coord[]));
  if (!b) return false;
  const view = m.getBounds();
  return (
    b[0] >= view.getWest() && b[1] >= view.getSouth() && b[2] <= view.getEast() && b[3] <= view.getNorth()
  );
}
watch(hoverPoint, pushHover);
watch(avoidedWays, pushAvoided, { deep: true });
watch(
  () =>
    slots.value.map((s) => (s.point ? `${s.point.lat},${s.point.lon},${s.point.name ?? ''}` : '-')).join('|') +
    `#${parkingIndex.value}`,
  () => {
    rebuildMarkers();
    if (!dragging) setTimeout(fitToPoints, 30);
  },
);
watch(
  () => [layers.sat, layers.pois, layers.lifts, layers.crags, layers.contours],
  () => {
    const m = map.value;
    if (m) applyVisibility(m);
    // A layer switched on for the first time still has to be fetched.
    if (layers.sat && !satData) void ensureOverlay('sat');
    if (layers.pois && !poiData) void ensureOverlay('pois');
    if (layers.lifts && !liftData) void ensureOverlay('lifts');
    if (layers.crags && !cragData) void ensureOverlay('crags');
    if (layers.contours && m && !m.getSource(SRC.contours)) {
      addContours(m, contourTiles, tokens, true);
      applyVisibility(m);
    }
  },
);
watch(
  () => layers.terrain,
  () => {
    const m = map.value;
    if (m) applyTerrain(m);
  },
);

defineExpose({
  flyTo(lon: number, lat: number, zoom = 13) {
    map.value?.flyTo({ center: [lon, lat], zoom, duration: 900 });
  },
  fit: fitToResult,
});
</script>

<template>
  <!-- `--sheet-h` lifts the scale bar and the attribution above the phone
       sheet (see theme.css); they stop easing while a finger holds the sheet. -->
  <div
    class="relative h-full w-full"
    :style="{
      '--sheet-h': isCompact ? `${sheetHeight}px` : '0px',
      '--sheet-transition': sheetDragging ? 'none' : undefined,
    }"
  >
    <div ref="host" class="absolute inset-0" style="background: var(--map-ground)" aria-label="Map of Trentino-Alto Adige" />
    <div v-if="mapUnavailable" class="absolute inset-0 z-10 grid place-items-center p-5" role="alert">
      <div class="card max-w-[380px] px-4 py-3.5 text-[13px] leading-snug" :style="{ boxShadow: 'var(--shadow-2)' }">
        <p class="font-medium">The map cannot be drawn in this browser.</p>
        <p class="mt-1.5 text-muted">
          It could not start WebGL, which the map needs. In Chrome, open
          <span class="font-mono text-[12px]">chrome://gpu</span>: if WebGL is listed as unavailable, turn on
          “Use graphics acceleration when available” under Settings → System, then quit and reopen the
          browser. Routes still work without the map.
        </p>
      </div>
    </div>
    <FirstVisitHint v-else />
    <MapPopover v-if="popover" :state="popover" @close="closePopover" />
  </div>
</template>
