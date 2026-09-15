import type { GeoJSONSource, Map as MlMap } from 'maplibre-gl';
import type { FeatureCollection, RouteAlternative } from '../lib/types';
import { DEM_MAXZOOM, type Tokens } from './style';
import { makeIcon } from './icons';

export const SRC = {
  dem: 'trp-dem',
  demTerrain: 'trp-dem-terrain',
  contours: 'trp-contours',
  region: 'trp-region',
  sat: 'trp-sat',
  pois: 'trp-pois',
  lifts: 'trp-lifts',
  stations: 'trp-stations',
  alt: 'trp-alt',
  route: 'trp-route',
  avoided: 'trp-avoided',
  drag: 'trp-drag',
  hover: 'trp-hover',
} as const;

export const LYR = {
  hillshade: 'trp-hillshade',
  contour: 'trp-contour',
  contourLabel: 'trp-contour-label',
  regionMask: 'trp-region-mask',
  regionLine: 'trp-region-line',
  sat: 'trp-sat-line',
  satLabel: 'trp-sat-label',
  lifts: 'trp-lifts-line',
  liftsLabel: 'trp-lifts-label',
  poi: 'trp-poi',
  poiPlace: 'trp-poi-place',
  altHit: 'trp-alt-hit',
  alt: 'trp-alt-line',
  routeCasing: 'trp-route-casing',
  route: 'trp-route-line',
  routeDash: 'trp-route-dash',
  routeLift: 'trp-route-lift',
  routeStations: 'trp-route-stations',
  routeHit: 'trp-route-hit',
  avoided: 'trp-avoided-line',
  avoidedHatch: 'trp-avoided-hatch',
  drag: 'trp-drag-dot',
  hover: 'trp-hover-dot',
} as const;

const EMPTY: FeatureCollection = { type: 'FeatureCollection', features: [] };

/**
 * Terrain, hillshade and contours all read the same tiles through one cached
 * protocol. They are declared as two sources only because MapLibre warns when
 * a hillshade layer and 3D terrain name the same one; the bytes are fetched
 * and decoded once either way.
 */
export function addDem(map: MlMap, demUrl: string, attribution: string): void {
  const spec = {
    type: 'raster-dem',
    tiles: [demUrl],
    tileSize: 256,
    encoding: 'terrarium',
    maxzoom: DEM_MAXZOOM,
    attribution,
  };
  if (!map.getSource(SRC.dem)) map.addSource(SRC.dem, spec as never);
  if (!map.getSource(SRC.demTerrain)) map.addSource(SRC.demTerrain, spec as never);
}

/**
 * The overlays are fetched after the map is built, so they would otherwise be
 * added on top of the answer — drawing trails and lift lines over the route,
 * and taking the clicks meant for it. They belong under the route layers.
 */
function beforeRoutes(map: MlMap): string | undefined {
  return map.getLayer(LYR.altHit) ? LYR.altHit : undefined;
}

function firstSymbol(map: MlMap): string | undefined {
  for (const l of map.getStyle()?.layers ?? []) if (l.type === 'symbol') return l.id;
  return undefined;
}

export function addHillshade(map: MlMap, dark: boolean): void {
  if (map.getLayer(LYR.hillshade)) return;
  map.addLayer(
    {
      id: LYR.hillshade,
      type: 'hillshade',
      source: SRC.dem,
      paint: {
        'hillshade-exaggeration': dark ? 0.34 : 0.26,
        'hillshade-shadow-color': dark ? '#000000' : '#6b6d66',
        'hillshade-highlight-color': dark ? '#5d6570' : '#ffffff',
        'hillshade-accent-color': dark ? '#000000' : '#8a8c84',
      },
    } as never,
    firstSymbol(map),
  );
}

export function addContours(map: MlMap, tilesUrl: string, t: Tokens, visible: boolean): void {
  if (!map.getSource(SRC.contours)) {
    map.addSource(SRC.contours, { type: 'vector', tiles: [tilesUrl], maxzoom: DEM_MAXZOOM } as never);
  }
  const before = firstSymbol(map);
  if (!map.getLayer(LYR.contour)) {
    map.addLayer(
      {
        id: LYR.contour,
        type: 'line',
        source: SRC.contours,
        'source-layer': 'contours',
        minzoom: 10,
        layout: { visibility: visible ? 'visible' : 'none' },
        paint: {
          'line-color': t.muted,
          'line-opacity': ['match', ['get', 'level'], 1, 0.42, 0.24],
          'line-width': ['match', ['get', 'level'], 1, 1, 0.55],
        },
      } as never,
      before,
    );
  }
  if (!map.getLayer(LYR.contourLabel)) {
    map.addLayer({
      id: LYR.contourLabel,
      type: 'symbol',
      source: SRC.contours,
      'source-layer': 'contours',
      minzoom: 12.5,
      filter: ['==', ['get', 'level'], 1],
      layout: {
        visibility: visible ? 'visible' : 'none',
        'symbol-placement': 'line',
        'text-field': [
          'concat',
          ['to-string', ['round', ['to-number', ['coalesce', ['get', 'ele'], 0]]]],
          ' m',
        ],
        'text-font': ['Noto Sans Regular'],
        'text-size': 10,
        'text-max-angle': 25,
        'symbol-spacing': 320,
      },
      paint: {
        'text-color': t.muted,
        'text-halo-color': t.surface,
        'text-halo-width': 1.4,
      },
    } as never);
  }
}

/** The world outside the region, dimmed; the border, a hairline. */
export function addRegion(map: MlMap, fc: FeatureCollection, t: Tokens, dark: boolean): void {
  const rings: number[][][] = [];
  for (const f of fc.features ?? []) {
    const g = f.geometry as { type: string; coordinates: never };
    if (g.type === 'Polygon') rings.push((g.coordinates as unknown as number[][][])[0]);
    else if (g.type === 'MultiPolygon')
      for (const poly of g.coordinates as unknown as number[][][][]) rings.push(poly[0]);
  }
  const world = [
    [-180, -85],
    [180, -85],
    [180, 85],
    [-180, 85],
    [-180, -85],
  ];
  const mask = {
    type: 'FeatureCollection',
    features: [
      {
        type: 'Feature',
        properties: {},
        geometry: { type: 'Polygon', coordinates: [world, ...rings] },
      },
    ],
  };
  if (map.getSource(SRC.region)) {
    (map.getSource(SRC.region) as GeoJSONSource).setData(mask as never);
    (map.getSource(SRC.region + '-line') as GeoJSONSource)?.setData(fc as never);
    return;
  }
  map.addSource(SRC.region, { type: 'geojson', data: mask as never });
  map.addSource(SRC.region + '-line', { type: 'geojson', data: fc as never });
  map.addLayer({
    id: LYR.regionMask,
    type: 'fill',
    source: SRC.region,
    paint: {
      'fill-color': dark ? '#05070a' : '#ffffff',
      'fill-opacity': ['interpolate', ['linear'], ['zoom'], 5, dark ? 0.62 : 0.6, 10, dark ? 0.5 : 0.48],
    },
  } as never);
  map.addLayer({
    id: LYR.regionLine,
    type: 'line',
    source: SRC.region + '-line',
    paint: {
      'line-color': t.lineStrong,
      'line-width': ['interpolate', ['linear'], ['zoom'], 5, 0.8, 10, 1.4],
      'line-opacity': 0.9,
    },
  } as never);
}

export function addSat(map: MlMap, fc: FeatureCollection, t: Tokens): void {
  if (map.getSource(SRC.sat)) {
    (map.getSource(SRC.sat) as GeoJSONSource).setData(fc as never);
    return;
  }
  map.addSource(SRC.sat, { type: 'geojson', data: fc as never });
  const byGrade = [
    'match',
    ['get', 'grade'],
    'T',
    t.gradeT,
    'E',
    t.gradeE,
    'EE',
    t.gradeEE,
    'EEA',
    t.gradeEEA,
    'A',
    t.gradeA,
    t.gradeE,
  ];
  const under = beforeRoutes(map);
  map.addLayer({
    id: LYR.sat,
    type: 'line',
    source: SRC.sat,
    minzoom: 9,
    paint: {
      'line-color': byGrade,
      'line-width': ['interpolate', ['linear'], ['zoom'], 9, 1, 13, 1.8, 16, 2.6],
      'line-opacity': ['interpolate', ['linear'], ['zoom'], 9, 0.45, 12, 0.72],
      'line-dasharray': [2.2, 1.6],
    },
  } as never, under);
  map.addLayer({
    id: LYR.satLabel,
    type: 'symbol',
    source: SRC.sat,
    minzoom: 12.5,
    layout: {
      'symbol-placement': 'line',
      'text-field': ['coalesce', ['get', 'numero'], ['get', 'ref'], ''],
      'text-font': ['Noto Sans Regular'],
      'text-size': 10.5,
      'symbol-spacing': 260,
      'text-max-angle': 30,
    },
    paint: {
      'text-color': byGrade,
      'text-halo-color': t.surface,
      'text-halo-width': 1.6,
    },
  } as never, under);
}

/**
 * Every lift in the region, drawn faintly wherever trails are drawn, so the
 * option is visible before anyone switches it on.
 */
export function addLifts(map: MlMap, fc: FeatureCollection, t: Tokens): void {
  if (map.getSource(SRC.lifts)) {
    (map.getSource(SRC.lifts) as GeoJSONSource).setData(fc as never);
    return;
  }
  map.addSource(SRC.lifts, { type: 'geojson', data: fc as never });
  const under = beforeRoutes(map);
  map.addLayer({
    id: LYR.lifts,
    type: 'line',
    source: SRC.lifts,
    minzoom: 9,
    layout: { 'line-cap': 'round' },
    paint: {
      'line-color': t.lift,
      'line-width': ['interpolate', ['linear'], ['zoom'], 9, 0.9, 14, 1.8],
      'line-opacity': ['interpolate', ['linear'], ['zoom'], 9, 0.3, 13, 0.55],
      'line-dasharray': [4, 2.5],
    },
  } as never, under);
  map.addLayer({
    id: LYR.liftsLabel,
    type: 'symbol',
    source: SRC.lifts,
    minzoom: 13,
    layout: {
      'symbol-placement': 'line',
      'text-field': ['coalesce', ['get', 'name'], ''],
      'text-font': ['Noto Sans Regular'],
      'text-size': 10.5,
      'symbol-spacing': 300,
      'text-max-angle': 30,
    },
    paint: {
      'text-color': t.lift,
      'text-halo-color': t.surface,
      'text-halo-width': 1.6,
    },
  } as never, under);
}

export function addPois(map: MlMap, fc: FeatureCollection, t: Tokens): void {
  for (const kind of ['peak', 'hut', 'pass', 'place'] as const) {
    const id = `trp-${kind}`;
    if (map.hasImage(id)) map.removeImage(id);
    // Muted marks: hundreds of summits must not shout over the route.
    map.addImage(id, makeIcon(kind, t.muted, t.surface), { pixelRatio: 2 });
  }
  const labelled = withLabels(fc);
  if (map.getSource(SRC.pois)) {
    (map.getSource(SRC.pois) as GeoJSONSource).setData(labelled as never);
    return;
  }
  map.addSource(SRC.pois, { type: 'geojson', data: labelled as never });
  const label = ['get', 'label'];
  const common = {
    'icon-image': ['match', ['get', 'kind'], 'peak', 'trp-peak', 'hut', 'trp-hut', 'pass', 'trp-pass', 'trp-place'],
    'icon-size': ['interpolate', ['linear'], ['zoom'], 9, 0.7, 13, 1],
    'text-field': label,
    'text-font': ['Noto Sans Regular'],
    'text-size': ['interpolate', ['linear'], ['zoom'], 9, 10, 14, 12],
    'text-anchor': 'top',
    'text-offset': [0, 0.85],
    'text-optional': true,
    'text-padding': 9,
  };
  const paint = {
    'text-color': t.ink,
    'text-halo-color': t.surface,
    'text-halo-width': 1.6,
    'icon-opacity': 0.92,
  };
  const underRoutes = beforeRoutes(map);
  map.addLayer({
    id: LYR.poi,
    type: 'symbol',
    source: SRC.pois,
    minzoom: 9,
    filter: ['in', ['get', 'kind'], ['literal', ['peak', 'hut', 'pass']]],
    layout: common,
    paint,
  } as never, underRoutes);
  map.addLayer({
    id: LYR.poiPlace,
    type: 'symbol',
    source: SRC.pois,
    minzoom: 11.5,
    filter: ['==', ['get', 'kind'], 'place'],
    layout: common,
    paint,
  } as never, underRoutes);
}

/**
 * Name and height joined once, here, rather than by an expression per frame —
 * and collision priority expressed as draw order rather than as a data-driven
 * `symbol-sort-key`, so no style expression ever reads a property that the
 * source may leave null.
 */
function withLabels(fc: FeatureCollection): FeatureCollection {
  const ranked = (fc.features ?? []).map((f) => {
    const name = String(f.properties?.name ?? '');
    const raw = f.properties?.ele;
    const ele = typeof raw === 'number' ? raw : Number(raw);
    const height = isFinite(ele) && ele > 0 ? Math.round(ele) : 0;
    const label = height ? `${name}  ${height} m` : name;
    // Huts first, then the highest summits: drawn earlier, so they win.
    const rank = f.properties?.kind === 'hut' ? -1e6 : -height;
    return { feature: { ...f, properties: { ...f.properties, label } }, rank };
  });
  ranked.sort((a, b) => a.rank - b.rank);
  return { type: 'FeatureCollection', features: ranked.map((r) => r.feature) };
}

export function addRouteLayers(map: MlMap, t: Tokens): void {
  if (map.getSource(SRC.alt)) return;
  map.addSource(SRC.alt, { type: 'geojson', data: EMPTY as never });
  map.addSource(SRC.route, { type: 'geojson', data: EMPTY as never });
  map.addSource(SRC.stations, { type: 'geojson', data: EMPTY as never });
  map.addSource(SRC.avoided, { type: 'geojson', data: EMPTY as never });
  map.addSource(SRC.drag, { type: 'geojson', data: EMPTY as never });
  map.addSource(SRC.hover, { type: 'geojson', data: EMPTY as never });

  const byMode = ['match', ['get', 'mode'], 'bike', t.bike, 'hike', t.hike, t.car];

  map.addLayer({
    id: LYR.altHit,
    type: 'line',
    source: SRC.alt,
    layout: { 'line-cap': 'round' },
    paint: { 'line-color': '#000000', 'line-opacity': 0, 'line-width': 18 },
  } as never);
  map.addLayer({
    id: LYR.alt,
    type: 'line',
    source: SRC.alt,
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': t.faint,
      'line-width': ['interpolate', ['linear'], ['zoom'], 8, 2.4, 14, 3.4],
      'line-opacity': 0.75,
    },
  } as never);
  map.addLayer({
    id: LYR.routeCasing,
    type: 'line',
    source: SRC.route,
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': t.surface,
      'line-width': ['interpolate', ['linear'], ['zoom'], 8, 7.5, 14, 10.5],
      'line-opacity': 0.85,
    },
  } as never);
  map.addLayer({
    id: LYR.route,
    type: 'line',
    source: SRC.route,
    filter: ['in', ['get', 'mode'], ['literal', ['car', 'bike']]],
    layout: { 'line-cap': 'round', 'line-join': 'round' },
    paint: {
      'line-color': byMode,
      'line-width': ['interpolate', ['linear'], ['zoom'], 8, 3.6, 14, 5.6],
    },
  } as never);
  map.addLayer({
    id: LYR.routeDash,
    type: 'line',
    source: SRC.route,
    filter: ['==', ['get', 'mode'], 'hike'],
    layout: { 'line-cap': 'butt', 'line-join': 'round' },
    paint: {
      'line-color': t.hike,
      'line-width': ['interpolate', ['linear'], ['zoom'], 8, 3.6, 14, 5.6],
      'line-dasharray': [1.5, 1.1],
    },
  } as never);
  // A ride is a cable between two stations: thin, straight and unmistakably
  // not a way you walked.
  map.addLayer({
    id: LYR.routeLift,
    type: 'line',
    source: SRC.route,
    filter: ['==', ['get', 'mode'], 'lift'],
    layout: { 'line-cap': 'butt', 'line-join': 'round' },
    paint: {
      'line-color': t.lift,
      'line-width': ['interpolate', ['linear'], ['zoom'], 8, 1.8, 14, 2.6],
    },
  } as never);
  map.addLayer({
    id: LYR.routeStations,
    type: 'circle',
    source: SRC.stations,
    paint: {
      'circle-radius': ['interpolate', ['linear'], ['zoom'], 8, 3, 14, 4.5],
      'circle-color': t.surface,
      'circle-stroke-color': t.lift,
      'circle-stroke-width': 2,
    },
  } as never);
  // Ruled out: struck through in red, and wide enough to click back.
  map.addLayer({
    id: LYR.avoidedHatch,
    type: 'line',
    source: SRC.avoided,
    layout: { 'line-cap': 'butt' },
    paint: {
      'line-color': t.dest,
      'line-width': ['interpolate', ['linear'], ['zoom'], 10, 7, 15, 12],
      'line-opacity': 0.28,
      'line-dasharray': [0.5, 0.5],
    },
  } as never);
  map.addLayer({
    id: LYR.avoided,
    type: 'line',
    source: SRC.avoided,
    paint: {
      'line-color': t.dest,
      'line-width': ['interpolate', ['linear'], ['zoom'], 10, 1.4, 15, 2.2],
      'line-opacity': 0.9,
    },
  } as never);

  // A wide invisible ribbon over the answer: what a finger has to hit to
  // reshape it. Added last so it is what the pointer finds first.
  map.addLayer({
    id: LYR.routeHit,
    type: 'line',
    source: SRC.route,
    layout: { 'line-cap': 'round' },
    paint: { 'line-color': '#000000', 'line-opacity': 0, 'line-width': 24 },
  } as never);

  map.addLayer({
    id: LYR.drag,
    type: 'circle',
    source: SRC.drag,
    paint: {
      'circle-radius': 6.5,
      'circle-color': t.accent,
      'circle-stroke-color': t.surface,
      'circle-stroke-width': 2.5,
    },
  } as never);

  map.addLayer({
    id: LYR.hover,
    type: 'circle',
    source: SRC.hover,
    paint: {
      'circle-radius': 5.5,
      'circle-color': t.ink,
      'circle-stroke-color': t.surface,
      'circle-stroke-width': 2.5,
    },
  } as never);
}

/** Selected route as one feature per leg; the rest as thin muted lines. */
export function routeData(routes: RouteAlternative[], selectedId: string | null) {
  const sel = routes.find((r) => r.id === selectedId) ?? routes[0];
  const alt = {
    type: 'FeatureCollection',
    features: routes
      .filter((r) => r !== sel)
      .map((r) => ({ type: 'Feature', properties: { id: r.id }, geometry: r.geometry })),
  };
  const selected = {
    type: 'FeatureCollection',
    features: sel
      ? (sel.legs?.length
          ? sel.legs.map((l) => ({ type: 'Feature', properties: { mode: l.mode }, geometry: l.geometry }))
          : [{ type: 'Feature', properties: { mode: 'car' }, geometry: sel.geometry }])
      : [],
  };
  const stations = {
    type: 'FeatureCollection',
    features: (sel?.legs ?? [])
      .filter((l) => l.mode === 'lift')
      .flatMap((l) => {
        const c = l.geometry?.coordinates ?? [];
        if (c.length < 2) return [];
        return [c[0], c[c.length - 1]].map((coord) => ({
          type: 'Feature',
          properties: { name: l.name ?? '' },
          geometry: { type: 'Point', coordinates: coord },
        }));
      }),
  };

  return { alt, selected, stations };
}

/** The ways the service struck out, as lines it can draw. */
export function avoidedData(ways: { id: string; name?: string; points?: [number, number][] }[]) {
  return {
    type: 'FeatureCollection',
    features: (ways ?? [])
      .filter((w) => (w.points?.length ?? 0) > 1)
      .map((w) => ({
        type: 'Feature',
        properties: { id: w.id, name: w.name ?? '' },
        geometry: { type: 'LineString', coordinates: w.points },
      })),
  };
}

export function setLayerVisible(map: MlMap, ids: string[], visible: boolean): void {
  for (const id of ids) {
    if (map.getLayer(id)) map.setLayoutProperty(id, 'visibility', visible ? 'visible' : 'none');
  }
}

/**
 * The shipped dark style is low on contrast and asks its sprite for a
 * `wood-pattern` that is not in it, so every forest renders as flat grey.
 * These are small, surgical corrections to that style, not a new one.
 */
export function tuneDarkStyle(map: MlMap): void {
  const set = (layer: string, prop: string, value: unknown) => {
    if (!map.getLayer(layer)) return;
    try {
      map.setPaintProperty(layer, prop, value as never);
    } catch {
      /* the style moved on; the map is still usable without the tweak */
    }
  };

  // Woodland: drop the missing pattern and give it a colour that reads as trees.
  set('landcover_wood', 'fill-pattern', undefined);
  set('landcover_wood', 'fill-color', '#18271c');
  set('landcover_wood', 'fill-opacity', [
    'interpolate',
    ['exponential', 0.4],
    ['zoom'],
    7,
    0.35,
    10,
    0.7,
    14,
    0.55,
  ]);

  // Water that is a shade of water, not a shade of ground.
  set('water', 'fill-color', '#16222e');
  set('waterway', 'line-color', '#1b2b3a');
  set('water_name', 'text-color', '#8199ad');
  set('water_name', 'text-halo-color', 'rgba(0,0,0,0.75)');

  // Ground and buildings, lifted just enough to separate.
  set('background', 'background-color', '#0e1013');
  set('building', 'fill-color', '#131619');
  set('building', 'fill-outline-color', '#1d2126');
  set('landuse_residential', 'fill-color', '#15171a');

  // Place names at rgb(101,101,101) on near-black are barely there.
  for (const id of [
    'place_other',
    'place_suburb',
    'place_village',
    'place_town',
    'place_city',
    'place_city_large',
    'place_state',
    'place_country_other',
    'place_country_minor',
    'place_country_major',
  ]) {
    set(id, 'text-color', '#a9aeb6');
  }
  set('highway_name_other', 'text-color', '#7d8189');
  set('highway_name_motorway', 'text-color', '#8d9097');
}

/**
 * The shipped styles filter on numbers that the vector tiles often leave null
 * — `["<=", ["get","ref_length"], 6]` on the road-shield layers, `rank` on the
 * place and POI layers. MapLibre evaluates those, finds null where it wants a
 * number, and warns once per tile batch; the console fills up with something
 * no one can act on.
 *
 * Each such comparison is rewritten to read through a sentinel that keeps the
 * result exactly as it is today — a missing value already compared false — so
 * nothing renders differently and the warning has nothing left to report.
 */
export function hardenStyleFilters(map: MlMap): void {
  const LOW = -1e9;
  const HIGH = 1e9;

  const harden = (node: unknown): unknown => {
    if (!Array.isArray(node)) return node;
    const [op, left, right] = node as [unknown, unknown, unknown];
    if (
      (op === '<' || op === '<=' || op === '>' || op === '>=') &&
      Array.isArray(left) &&
      left[0] === 'get' &&
      typeof left[1] === 'string' &&
      typeof right === 'number'
    ) {
      const sentinel = op === '<' || op === '<=' ? HIGH : LOW;
      return [op, ['coalesce', left, sentinel], right];
    }
    return (node as unknown[]).map(harden);
  };

  for (const layer of map.getStyle()?.layers ?? []) {
    const filter = (layer as { filter?: unknown }).filter;
    if (!filter) continue;
    const hardened = harden(filter);
    if (JSON.stringify(hardened) === JSON.stringify(filter)) continue;
    try {
      map.setFilter(layer.id, hardened as never);
    } catch {
      /* a filter shape we do not understand is left exactly as it was */
    }
  }
}
