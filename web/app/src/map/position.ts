import type { GeoJSONSource, IControl, Map as MlMap } from 'maplibre-gl';
import type { Tokens } from './style';

/**
 * Where you are, on the map: a disc for what the fix is worth, a dot for the
 * fix itself, and a wedge for where you are pointed when you are moving.
 *
 * The shape is structural on purpose — this module knows nothing about the
 * composable that produces it, and the composable knows nothing about the map.
 */
export interface PositionFix {
  lat: number;
  lon: number;
  accuracy: number;
  heading: number | null;
  speed: number | null;
}

export const POSITION_SRC = 'trp-position';
export const POSITION_LYR = {
  accuracy: 'trp-position-accuracy',
  heading: 'trp-position-heading',
  dot: 'trp-position-dot',
} as const;

const HEADING_IMAGE = 'trp-position-arrow';

/** Below this the phone is standing still and its heading is noise. */
const MOVING = 0.5;
/** Sides of the accuracy disc: round enough at any zoom, cheap to rebuild. */
const SIDES = 48;
/** The same earth radius the distances are measured on. */
const R = 6371008.8;

const EMPTY = { type: 'FeatureCollection', features: [] };

/**
 * The accuracy disc as a real polygon in lon/lat.
 *
 * A circle layer would have to be sized in pixels, which means an expression
 * that guesses metres per pixel at the current zoom — and guesses wrong the
 * moment the map is tilted over terrain. A ring of points is simply where the
 * ground is, at any zoom and any pitch.
 */
function disc(lat: number, lon: number, metres: number): [number, number][] {
  const dLat = (metres / R) * (180 / Math.PI);
  const dLon = dLat / Math.max(0.01, Math.cos((lat * Math.PI) / 180));
  const ring: [number, number][] = [];
  for (let i = 0; i <= SIDES; i++) {
    const a = (i / SIDES) * Math.PI * 2;
    ring.push([lon + dLon * Math.cos(a), lat + dLat * Math.sin(a)]);
  }
  return ring;
}

export function positionData(fix: PositionFix | null): unknown {
  if (!fix || !isFinite(fix.lat) || !isFinite(fix.lon)) return EMPTY;
  const features: unknown[] = [];
  if (isFinite(fix.accuracy) && fix.accuracy > 1) {
    features.push({
      type: 'Feature',
      properties: {},
      geometry: { type: 'Polygon', coordinates: [disc(fix.lat, fix.lon, fix.accuracy)] },
    });
  }
  // A heading is only worth drawing when the device is actually going
  // somewhere; standing still it spins, and a spinning arrow is a lie.
  const moving = typeof fix.speed === 'number' && fix.speed > MOVING;
  const heading = typeof fix.heading === 'number' && isFinite(fix.heading) && moving ? fix.heading : null;
  features.push({
    type: 'Feature',
    properties: heading === null ? {} : { heading },
    geometry: { type: 'Point', coordinates: [fix.lon, fix.lat] },
  });
  return { type: 'FeatureCollection', features };
}

/** The wedge, drawn once per style so it carries the current ink. */
function headingImage(t: Tokens) {
  const size = 48; // device pixels, added at pixelRatio 2 -> 24 css px
  const c = document.createElement('canvas');
  c.width = size;
  c.height = size;
  const g = c.getContext('2d');
  if (!g) return { width: 1, height: 1, data: new Uint8Array(4) };
  // Drawn around the centre of the image, which is where the dot sits: the
  // rotation MapLibre applies is then about the fix itself.
  g.beginPath();
  g.moveTo(24, 3);
  g.lineTo(32, 19);
  g.lineTo(24, 15.5);
  g.lineTo(16, 19);
  g.closePath();
  g.lineJoin = 'round';
  g.strokeStyle = t.surface;
  g.lineWidth = 3;
  g.stroke();
  g.fillStyle = t.position;
  g.fill();
  const img = g.getImageData(0, 0, size, size);
  return { width: size, height: size, data: new Uint8Array(img.data.buffer.slice(0)) };
}

/**
 * Added from `applyCustom`, after the route layers, so the dot is over the
 * answer rather than under it — and added again on every style load, because
 * a style load takes the sources and the images with it.
 */
export function addPosition(map: MlMap, t: Tokens): void {
  if (map.hasImage(HEADING_IMAGE)) map.removeImage(HEADING_IMAGE);
  map.addImage(HEADING_IMAGE, headingImage(t), { pixelRatio: 2 });
  if (map.getSource(POSITION_SRC)) return;

  map.addSource(POSITION_SRC, { type: 'geojson', data: EMPTY as never });
  map.addLayer({
    id: POSITION_LYR.accuracy,
    type: 'fill',
    source: POSITION_SRC,
    filter: ['==', ['geometry-type'], 'Polygon'],
    paint: { 'fill-color': t.position, 'fill-opacity': 0.12 },
  } as never);
  map.addLayer({
    id: POSITION_LYR.heading,
    type: 'symbol',
    source: POSITION_SRC,
    filter: ['all', ['==', ['geometry-type'], 'Point'], ['has', 'heading']],
    layout: {
      'icon-image': HEADING_IMAGE,
      'icon-rotate': ['get', 'heading'],
      'icon-rotation-alignment': 'map',
      'icon-allow-overlap': true,
      'icon-ignore-placement': true,
    },
  } as never);
  map.addLayer({
    id: POSITION_LYR.dot,
    type: 'circle',
    source: POSITION_SRC,
    filter: ['==', ['geometry-type'], 'Point'],
    paint: {
      'circle-radius': 7,
      'circle-color': t.position,
      'circle-stroke-color': t.surface,
      'circle-stroke-width': 2.5,
    },
  } as never);
}

export function setPosition(map: MlMap, fix: PositionFix | null): void {
  const src = map.getSource(POSITION_SRC) as GeoJSONSource | undefined;
  src?.setData(positionData(fix) as never);
}

/** What the locate button looks like right now. */
export interface LocateState {
  locating: boolean;
  on: boolean;
  following: boolean;
  label: string;
}

export interface LocateControl extends IControl {
  update(state: LocateState): void;
}

/**
 * The desktop locate button: one more control under the zoom group, dressed by
 * the same `.maplibregl-ctrl-group` rules. The icon is the panel's `locate`
 * glyph, inline, in currentColor — MapLibre builds this DOM itself, so it
 * cannot be a component, and the colour has to come from the class.
 */
export function createLocateControl(onClick: () => void): LocateControl {
  const box = document.createElement('div');
  box.className = 'maplibregl-ctrl maplibregl-ctrl-group trp-locate';
  const button = document.createElement('button');
  button.type = 'button';
  button.innerHTML = `<svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor"
      stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="3.2"/><circle cx="12" cy="12" r="7.4"/>
      <path d="M12 2.6v2.2M12 19.2v2.2M2.6 12h2.2M19.2 12h2.2"/>
    </svg>`;
  button.addEventListener('click', (e) => {
    e.preventDefault();
    onClick();
  });
  box.appendChild(button);

  return {
    onAdd: () => box,
    onRemove: () => box.remove(),
    update(state: LocateState) {
      box.classList.toggle('is-locating', state.locating);
      box.classList.toggle('is-on', state.on);
      box.classList.toggle('is-following', state.following);
      button.setAttribute('aria-label', state.label);
      button.setAttribute('title', state.label);
      button.setAttribute('aria-pressed', state.following ? 'true' : 'false');
    },
  };
}
