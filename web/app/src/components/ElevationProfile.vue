<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref } from 'vue';
import type { RouteAlternative } from '../lib/types';
import { fmtDistance, fmtElevation } from '../lib/format';
import { cumulative, pointAt, type Coord } from '../lib/geo';
import { hoverPoint } from '../composables/useHover';
import { hasHover } from '../composables/useMedia';

const props = defineProps<{ route: RouteAlternative }>();

const host = ref<HTMLDivElement | null>(null);
const width = ref(320);
const HEIGHT = 118;
const PAD = { top: 12, right: 10, bottom: 20, left: 38 };

let ro: ResizeObserver | null = null;
onMounted(() => {
  if (!host.value) return;
  width.value = host.value.clientWidth || 320;
  ro = new ResizeObserver((e) => (width.value = Math.max(160, e[0].contentRect.width)));
  ro.observe(host.value);
});
onBeforeUnmount(() => {
  ro?.disconnect();
  hoverPoint.value = null;
});

/** Distance ranges of each leg, so the line can be coloured by how you travel. */
const segments = computed(() => {
  const out: { mode: string; from: number; to: number }[] = [];
  let at = 0;
  for (const l of props.route.legs ?? []) {
    out.push({ mode: l.mode, from: at, to: at + l.meters });
    at += l.meters;
  }
  if (!out.length) out.push({ mode: 'car', from: 0, to: props.route.meters });
  return out;
});

const points = computed<[number, number][]>(() => {
  let p = props.route.profile;
  if (!p?.length) {
    p = [];
    let off = 0;
    for (const l of props.route.legs ?? []) {
      for (const [m, e] of l.profile ?? []) p.push([off + m, e]);
      off += l.meters;
    }
  }
  if (!p?.length) return [];
  // Draw at most ~400 samples: the shape is what matters, not every vertex.
  const step = Math.max(1, Math.floor(p.length / 400));
  const out = p.filter((_, i) => i % step === 0);
  if (out[out.length - 1] !== p[p.length - 1]) out.push(p[p.length - 1]);
  return out as [number, number][];
});

const domain = computed(() => {
  const pts = points.value;
  if (!pts.length) return { x0: 0, x1: 1, y0: 0, y1: 1 };
  let y0 = Infinity;
  let y1 = -Infinity;
  for (const [, e] of pts) {
    if (e < y0) y0 = e;
    if (e > y1) y1 = e;
  }
  // A flat valley ride should look flat, so the scale never zooms past 120 m.
  const span = Math.max(120, y1 - y0);
  const mid = (y0 + y1) / 2;
  const pad = span * 0.12;
  return { x0: 0, x1: pts[pts.length - 1][0] || 1, y0: mid - span / 2 - pad, y1: mid + span / 2 + pad };
});

const plotW = computed(() => Math.max(20, width.value - PAD.left - PAD.right));
const plotH = HEIGHT - PAD.top - PAD.bottom;

const sx = (m: number) => PAD.left + ((m - domain.value.x0) / (domain.value.x1 - domain.value.x0 || 1)) * plotW.value;
const sy = (e: number) =>
  PAD.top + plotH - ((e - domain.value.y0) / (domain.value.y1 - domain.value.y0 || 1)) * plotH;

function slice(from: number, to: number): [number, number][] {
  const pts = points.value;
  const out = pts.filter(([m]) => m >= from && m <= to);
  // Keep the joints continuous between legs.
  const before = [...pts].reverse().find(([m]) => m < from);
  const after = pts.find(([m]) => m > to);
  if (before && (!out.length || out[0][0] > from)) out.unshift(before);
  if (after) out.push(after);
  return out;
}

const shapes = computed(() =>
  segments.value.map((s) => {
    const pts = slice(s.from, s.to);
    if (pts.length < 2) return { mode: s.mode, line: '', area: '' };
    const line = pts.map(([m, e], i) => `${i ? 'L' : 'M'}${sx(m).toFixed(1)} ${sy(e).toFixed(1)}`).join(' ');
    const base = PAD.top + plotH;
    const area = `${line} L${sx(pts[pts.length - 1][0]).toFixed(1)} ${base} L${sx(pts[0][0]).toFixed(1)} ${base} Z`;
    return { mode: s.mode, line, area };
  }),
);

const LADDER = [10, 20, 25, 50, 100, 200, 250, 500, 1000, 2000];

/** Three or four honest gridlines, whatever the range. */
const gridLines = computed(() => {
  const { y0, y1 } = domain.value;
  const step = LADDER.find((s) => s >= (y1 - y0) / 4.5) ?? 2000;
  const out: { y: number; label: string }[] = [];
  for (let e = Math.ceil(y0 / step) * step; e <= y1; e += step) {
    out.push({ y: sy(e), label: String(Math.round(e)) });
  }
  return out;
});

const geomCum = computed(() => {
  const coords = (props.route.geometry?.coordinates ?? []) as Coord[];
  return { coords, cum: coords.length ? cumulative(coords) : [] };
});

const cursor = ref<{ m: number; e: number; x: number; y: number } | null>(null);

function onMove(ev: PointerEvent) {
  const rect = (ev.currentTarget as SVGElement).getBoundingClientRect();
  const px = ev.clientX - rect.left;
  const { x0, x1 } = domain.value;
  const m = Math.min(x1, Math.max(x0, ((px - PAD.left) / plotW.value) * (x1 - x0) + x0));
  const pts = points.value;
  if (!pts.length) return;
  let lo = 0;
  let hi = pts.length - 1;
  while (lo < hi - 1) {
    const mid = (lo + hi) >> 1;
    if (pts[mid][0] <= m) lo = mid;
    else hi = mid;
  }
  const t = (m - pts[lo][0]) / (pts[hi][0] - pts[lo][0] || 1);
  const e = pts[lo][1] + (pts[hi][1] - pts[lo][1]) * t;
  cursor.value = { m, e, x: sx(m), y: sy(e) };
  const { coords, cum } = geomCum.value;
  const p = coords.length ? pointAt(coords, cum, m) : null;
  hoverPoint.value = p;
}

function onLeave() {
  cursor.value = null;
  hoverPoint.value = null;
}
</script>

<template>
  <div ref="host" class="w-full select-none">
    <svg
      :width="width"
      :height="HEIGHT"
      :viewBox="`0 0 ${width} ${HEIGHT}`"
      class="block touch-none"
      role="img"
      :aria-label="`Elevation profile: ${fmtElevation(Math.round(domain.y0))} to ${fmtElevation(Math.round(domain.y1))} over ${fmtDistance(route.meters)}`"
      @pointermove="onMove"
      @pointerdown="onMove"
      @pointerleave="onLeave"
      @pointercancel="onLeave"
    >
      <g>
        <line
          v-for="g in gridLines"
          :key="g.label"
          :x1="PAD.left"
          :x2="width - PAD.right"
          :y1="g.y"
          :y2="g.y"
          stroke="var(--line)"
          stroke-width="1"
        />
        <text
          v-for="g in gridLines"
          :key="`t-${g.label}`"
          :x="PAD.left - 6"
          :y="g.y + 3.5"
          text-anchor="end"
          font-size="9.5"
          fill="var(--faint)"
        >
          {{ g.label }}
        </text>
      </g>

      <g v-for="(s, i) in shapes" :key="i">
        <path
          v-if="s.area"
          :d="s.area"
          :fill="`var(--${s.mode})`"
          :opacity="0.14"
        />
        <path
          v-if="s.line"
          :d="s.line"
          fill="none"
          :stroke="`var(--${s.mode})`"
          stroke-width="1.8"
          stroke-linejoin="round"
          stroke-linecap="round"
        />
      </g>

      <line
        :x1="PAD.left"
        :x2="width - PAD.right"
        :y1="PAD.top + plotH"
        :y2="PAD.top + plotH"
        stroke="var(--line-strong)"
        stroke-width="1"
      />
      <text :x="PAD.left" :y="HEIGHT - 5" font-size="9.5" fill="var(--faint)">0</text>
      <text :x="width - PAD.right" :y="HEIGHT - 5" text-anchor="end" font-size="9.5" fill="var(--faint)">
        {{ fmtDistance(route.meters) }}
      </text>

      <g v-if="cursor">
        <line
          :x1="cursor.x"
          :x2="cursor.x"
          :y1="PAD.top - 2"
          :y2="PAD.top + plotH"
          stroke="var(--ink)"
          stroke-width="1"
          stroke-dasharray="2 2"
          opacity="0.5"
        />
        <circle :cx="cursor.x" :cy="cursor.y" r="3.4" fill="var(--ink)" stroke="var(--surface)" stroke-width="1.6" />
      </g>
    </svg>

    <div class="mt-0.5 flex h-4 items-center justify-between text-[11px]">
      <span v-if="cursor" class="text-ink">
        {{ fmtElevation(Math.round(cursor.e)) }}
        <span class="text-faint">at</span>
        {{ fmtDistance(cursor.m) }}
      </span>
      <span v-else class="text-faint">{{ hasHover ? 'Hover' : 'Touch' }} the profile for height and distance</span>
    </div>
  </div>
</template>
