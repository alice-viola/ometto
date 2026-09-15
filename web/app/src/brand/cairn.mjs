/**
 * The Ometto mark: a cairn, rocks above rocks, the stone man that marks a
 * trail. It is generated from parameters rather than drawn, so every size and
 * every export comes from the same numbers and can be re-tuned in one place.
 *
 * Plain ESM on purpose: the Vue app and the Node export script import this
 * same file, so there is exactly one definition of the geometry.
 */

/** @typedef {{ widths:number[], heightRatio:number, gap:number, sway:number, tilt:number, strokeRatio:number, squircle:number[], pad:number }} CairnSpec */

/**
 * The spec. Every number is a fraction of the base (bottom) stone's width.
 * Chosen by rendering the mark at 16, 20, 24 and 32 px magnified eight times
 * and keeping the set whose gaps survive: at 0.08 the top two stones merged at
 * 16 px, at 0.10 they hold from 24 px up.
 */
export const CAIRN = {
  /** Stone widths, bottom to top. */
  widths: [1, 0.78, 0.6, 0.42],
  /** Each stone's height as a fraction of its own width. */
  heightRatio: 0.48,
  /** Air between stones. */
  gap: 0.1,
  /** Alternating horizontal offset, so the stack is balanced by hand. */
  sway: 0.06,
  /** Tilt of the top stone, in degrees. */
  tilt: 8,
  /** Outline weight as a fraction of the base width. */
  strokeRatio: 1 / 12,
  /**
   * Superellipse exponent per quadrant, going clockwise from the top right.
   * Unequal on purpose: equal corners read as a UI rounded rectangle, uneven
   * ones read as a stone.
   */
  squircle: [2.5, 3.1, 2.7, 3.4],
  /** Breathing room around the stack, as a fraction of the base width. */
  pad: 0.1,
};

/**
 * Under 24 px four stones cannot keep their gaps, so the same geometry is
 * built from three chunkier ones. Same code, same shapes, fewer of them.
 */
export const CAIRN_SMALL = {
  ...CAIRN,
  widths: [1, 0.72, 0.46],
  heightRatio: 0.62,
  gap: 0.13,
  sway: 0.05,
};

/** The spec a given rendered size should use. */
export function specFor(size) {
  return size < 24 ? CAIRN_SMALL : CAIRN;
}

/** A superellipse outline, sampled; the exponent varies by quadrant. */
function stonePath(cx, cy, w, h, spec, samples = 72) {
  const a = w / 2;
  const b = h / 2;
  const pts = [];
  for (let i = 0; i < samples; i++) {
    const t = (i / samples) * Math.PI * 2;
    const c = Math.cos(t);
    const s = Math.sin(t);
    // Quadrant 0 = top right, then clockwise.
    const q = c >= 0 ? (s <= 0 ? 0 : 3) : s <= 0 ? 1 : 2;
    const n = spec.squircle[q];
    const x = cx + a * Math.sign(c) * Math.abs(c) ** (2 / n);
    const y = cy + b * Math.sign(s) * Math.abs(s) ** (2 / n);
    pts.push([x, y]);
  }
  return pts;
}

function rotate(pts, cx, cy, deg) {
  const r = (deg * Math.PI) / 180;
  const co = Math.cos(r);
  const si = Math.sin(r);
  return pts.map(([x, y]) => {
    const dx = x - cx;
    const dy = y - cy;
    return [cx + dx * co - dy * si, cy + dx * si + dy * co];
  });
}

const fmt = (n) => (Math.round(n * 100) / 100).toString();
const toPath = (pts) => `M${pts.map(([x, y]) => `${fmt(x)} ${fmt(y)}`).join('L')}Z`;

/**
 * The stones, bottom to top, in a coordinate system where the base stone is
 * `base` wide. Returns the paths and the box that contains them.
 *
 * `stones` caps how many are drawn: at very small sizes four stones close up,
 * and three of the same geometry stay separable.
 */
export function cairnGeometry({ base = 100, spec = CAIRN, stones = spec.widths.length } = {}) {
  const widths = spec.widths.slice(0, stones).map((w) => w * base);
  const heights = widths.map((w) => w * spec.heightRatio);
  const gap = spec.gap * base;
  const sway = spec.sway * base;

  const total = heights.reduce((a, b) => a + b, 0) + gap * (widths.length - 1);
  const cx = 0;
  let y = total; // build from the ground up

  const paths = [];
  widths.forEach((w, i) => {
    const h = heights[i];
    const centreY = y - h / 2;
    const centreX = cx + (i % 2 === 0 ? -1 : 1) * sway * (i === 0 ? 0 : 1);
    let pts = stonePath(centreX, centreY, w, h, spec);
    if (i === widths.length - 1 && spec.tilt) pts = rotate(pts, centreX, centreY, -spec.tilt);
    paths.push(toPath(pts));
    y -= h + gap;
  });

  const pad = spec.pad * base;
  const halfWidth = base / 2 + sway + pad;
  return {
    paths,
    stroke: spec.strokeRatio * base,
    box: { x: -halfWidth, y: -pad, w: halfWidth * 2, h: total + pad * 2 },
  };
}

/**
 * The mark as an SVG string. `variant` is 'filled' or 'outline'; `color`
 * defaults to currentColor so the app can let the theme decide.
 */
export function cairnSvg({
  size = 100,
  color = 'currentColor',
  background = null,
  variant = 'filled',
  stones,
  spec = CAIRN,
  square = true,
  title = '',
} = {}) {
  const g = cairnGeometry({ base: 100, spec, stones });
  const { box } = g;
  // Square viewBox centred on the stack, so every export is the same shape.
  const side = Math.max(box.w, box.h);
  const vb = square
    ? { x: box.x - (side - box.w) / 2, y: box.y - (side - box.h) / 2, w: side, h: side }
    : box;

  const body =
    variant === 'outline'
      ? `<g fill="none" stroke="${color}" stroke-width="${fmt(g.stroke)}" stroke-linejoin="round">${g.paths
          .map((d) => `<path d="${d}"/>`)
          .join('')}</g>`
      : `<g fill="${color}">${g.paths.map((d) => `<path d="${d}"/>`).join('')}</g>`;

  const bg = background
    ? `<rect x="${fmt(vb.x)}" y="${fmt(vb.y)}" width="${fmt(vb.w)}" height="${fmt(vb.h)}" fill="${background}"/>`
    : '';

  const label = title ? `<title>${title}</title>` : '';
  const w = square ? size : Math.round((size * vb.w) / vb.h);
  return `<svg xmlns="http://www.w3.org/2000/svg" width="${w}" height="${size}" viewBox="${fmt(vb.x)} ${fmt(vb.y)} ${fmt(vb.w)} ${fmt(vb.h)}" role="img" aria-hidden="${title ? 'false' : 'true'}">${label}${bg}${body}</svg>`;
}

/** Inner markup only, for inlining in a component that owns the <svg> tag. */
export function cairnInner({ variant = 'filled', stones, spec = CAIRN } = {}) {
  const g = cairnGeometry({ base: 100, spec, stones });
  return variant === 'outline'
    ? `<g fill="none" stroke="currentColor" stroke-width="${fmt(g.stroke)}" stroke-linejoin="round">${g.paths.map((d) => `<path d="${d}"/>`).join('')}</g>`
    : `<g fill="currentColor">${g.paths.map((d) => `<path d="${d}"/>`).join('')}</g>`;
}

export function cairnViewBox({ spec = CAIRN, stones } = {}) {
  const { box } = cairnGeometry({ base: 100, spec, stones });
  const side = Math.max(box.w, box.h);
  return {
    x: box.x - (side - box.w) / 2,
    y: box.y - (side - box.h) / 2,
    w: side,
    h: side,
  };
}
