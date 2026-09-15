/**
 * Every brand file, generated from src/brand/cairn.mjs. The rasteriser fills
 * the same superellipses the SVG describes, so a PNG and an SVG of the mark
 * are the same geometry, not a trace of one another.
 */
import { writeFileSync, mkdirSync } from 'node:fs';
import { deflateSync } from 'node:zlib';
import { cairnGeometry, cairnSvg, specFor } from '../src/brand/cairn.mjs';

const OUT = new URL('../public/', import.meta.url);
mkdirSync(OUT, { recursive: true });
const INK = '#15181c';
const PAPER = '#f4f4f2';

// ---------------------------------------------------------------- rasteriser

/** Point-in-superellipse for one stone, in the stone's own frame. */
function stoneTest(cx, cy, w, h, squircle, tiltDeg) {
  const a = w / 2;
  const b = h / 2;
  const r = (-tiltDeg * Math.PI) / 180;
  const co = Math.cos(r);
  const si = Math.sin(r);
  return (px, py) => {
    // Undo the tilt about the stone's centre.
    const dx0 = px - cx;
    const dy0 = py - cy;
    const dx = dx0 * co + dy0 * si;
    const dy = -dx0 * si + dy0 * co;
    const u = dx / a;
    const v = dy / b;
    const q = u >= 0 ? (v <= 0 ? 0 : 3) : v <= 0 ? 1 : 2;
    const n = squircle[q];
    return Math.abs(u) ** n + Math.abs(v) ** n <= 1;
  };
}

function stoneTests(spec, stones) {
  const base = 100;
  const widths = spec.widths.slice(0, stones ?? spec.widths.length).map((w) => w * base);
  const heights = widths.map((w) => w * spec.heightRatio);
  const gap = spec.gap * base;
  const sway = spec.sway * base;
  const total = heights.reduce((a, b) => a + b, 0) + gap * (widths.length - 1);
  let y = total;
  const tests = [];
  widths.forEach((w, i) => {
    const h = heights[i];
    const cy = y - h / 2;
    const cx = (i % 2 === 0 ? -1 : 1) * sway * (i === 0 ? 0 : 1);
    const tilt = i === widths.length - 1 ? spec.tilt : 0;
    tests.push(stoneTest(cx, cy, w, h, spec.squircle, tilt));
    y -= h + gap;
  });
  return tests;
}

/** RGBA pixels for the mark at `size`, 4x4 supersampled. */
function renderMark(size, { ink = INK, background = null } = {}) {
  const spec = specFor(size);
  const g = cairnGeometry({ base: 100, spec });
  const side = Math.max(g.box.w, g.box.h);
  const vx = g.box.x - (side - g.box.w) / 2;
  const vy = g.box.y - (side - g.box.h) / 2;
  const tests = stoneTests(spec);

  const hex = (c) => [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16));
  const [ir, ig, ib] = hex(ink);
  const bg = background ? hex(background) : null;

  const S = 4;
  const px = Buffer.alloc(size * size * 4);
  for (let y = 0; y < size; y++) {
    for (let x = 0; x < size; x++) {
      let hits = 0;
      for (let sy = 0; sy < S; sy++) {
        for (let sx = 0; sx < S; sx++) {
          const ux = vx + ((x + (sx + 0.5) / S) / size) * side;
          const uy = vy + ((y + (sy + 0.5) / S) / size) * side;
          if (tests.some((t) => t(ux, uy))) hits++;
        }
      }
      const a = hits / (S * S);
      const o = (y * size + x) * 4;
      if (bg) {
        px[o] = Math.round(bg[0] + (ir - bg[0]) * a);
        px[o + 1] = Math.round(bg[1] + (ig - bg[1]) * a);
        px[o + 2] = Math.round(bg[2] + (ib - bg[2]) * a);
        px[o + 3] = 255;
      } else {
        px[o] = ir;
        px[o + 1] = ig;
        px[o + 2] = ib;
        px[o + 3] = Math.round(a * 255);
      }
    }
  }
  return px;
}

// ---------------------------------------------------------------- PNG writer

function crc32(buf) {
  let c;
  const table = crc32.table ??= (() => {
    const t = new Int32Array(256);
    for (let n = 0; n < 256; n++) {
      c = n;
      for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1;
      t[n] = c;
    }
    return t;
  })();
  let crc = -1;
  for (let i = 0; i < buf.length; i++) crc = (crc >>> 8) ^ table[(crc ^ buf[i]) & 0xff];
  return (crc ^ -1) >>> 0;
}

function chunk(type, data) {
  const len = Buffer.alloc(4);
  len.writeUInt32BE(data.length);
  const body = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4);
  crc.writeUInt32BE(crc32(body));
  return Buffer.concat([len, body, crc]);
}

function png(size, rgba) {
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(size, 0);
  ihdr.writeUInt32BE(size, 4);
  ihdr[8] = 8; // bit depth
  ihdr[9] = 6; // RGBA
  const raw = Buffer.alloc((size * 4 + 1) * size);
  for (let y = 0; y < size; y++) {
    raw[y * (size * 4 + 1)] = 0; // filter: none
    rgba.copy(raw, y * (size * 4 + 1) + 1, y * size * 4, (y + 1) * size * 4);
  }
  return Buffer.concat([
    Buffer.from([0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a]),
    chunk('IHDR', ihdr),
    chunk('IDAT', deflateSync(raw, { level: 9 })),
    chunk('IEND', Buffer.alloc(0)),
  ]);
}

/** An .ico is a directory of PNGs. */
function ico(entries) {
  const header = Buffer.alloc(6);
  header.writeUInt16LE(0, 0);
  header.writeUInt16LE(1, 2);
  header.writeUInt16LE(entries.length, 4);
  let offset = 6 + entries.length * 16;
  const dir = [];
  for (const { size, data } of entries) {
    const e = Buffer.alloc(16);
    e[0] = size >= 256 ? 0 : size;
    e[1] = size >= 256 ? 0 : size;
    e[4] = 1;
    e.writeUInt16LE(32, 6);
    e.writeUInt32LE(data.length, 8);
    e.writeUInt32LE(offset, 12);
    dir.push(e);
    offset += data.length;
  }
  return Buffer.concat([header, ...dir, ...entries.map((e) => e.data)]);
}

// ---------------------------------------------------------------- the files

const write = (name, buf) => {
  writeFileSync(new URL(name, OUT), buf);
  console.log('  ', name, typeof buf === 'string' ? buf.length + ' chars' : buf.length + ' bytes');
};

console.log('brand files:');
write('favicon.svg', cairnSvg({ size: 64, color: INK, title: 'Ometto' }));
write('mark-dark.svg', cairnSvg({ size: 64, color: '#e9ebee', title: 'Ometto' }));
write('mark-outline.svg', cairnSvg({ size: 64, color: INK, variant: 'outline', title: 'Ometto' }));
write('mark-24.svg', cairnSvg({ size: 24, color: INK }));
write('mark-32.svg', cairnSvg({ size: 32, color: INK }));

for (const s of [192, 512]) write(`icon-${s}.png`, png(s, renderMark(s)));
write('apple-touch-icon.png', png(180, renderMark(180, { background: PAPER })));
write(
  'favicon.ico',
  ico([16, 32, 48].map((s) => ({ size: s, data: png(s, renderMark(s)) }))),
);
