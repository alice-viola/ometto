<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { answeredToken, hasResult } from '../composables/usePlanner';
import { sheetDragging, sheetHeight, sheetSettled } from '../composables/useMedia';

/**
 * The sheet is sized to the *visible* viewport, not the layout one. On iOS the
 * layout viewport keeps its height while the keyboard covers a third of it;
 * following `visualViewport` instead is what keeps a focused field, and the
 * list under it, above the keys. Pinch-zoom shrinks the same viewport, and
 * following it then would drag the sheet about under the finger, so a zoomed
 * page is treated as unzoomed.
 */
const vv = typeof window !== 'undefined' ? window.visualViewport : null;
const vh = ref(800);
/** How far the visible bottom edge sits above the layout viewport's bottom. */
const lift = ref(0);

/** Enough for the start and destination fields, and no more of the map than that. */
const PEEK = 232;
const height = ref(PEEK);
const dragging = ref(false);
/**
 * The stop the sheet last settled on, so a keyboard or a rotation puts it back
 * there. Kept as a name, not a pixel height or an index: the stops themselves
 * move with the viewport, and on a phone the viewport is still settling while
 * the first answer lands.
 */
type Stop = 'peek' | 'rest' | 'top';
let restAt: Stop | null = 'peek';
const root = ref<HTMLElement | null>(null);

const snaps = computed(() => {
  const h = vh.value;
  const stops = [Math.min(PEEK, Math.round(h * 0.42)), Math.round(h * 0.52), Math.round(h - 72)];
  // A phone on its side has no room for three stops: keep the ones that are apart.
  const out: number[] = [];
  for (const s of stops) if (!out.length || s - out[out.length - 1] >= 40) out.push(s);
  return out;
});
const peek = computed(() => snaps.value[0]);
const top = computed(() => snaps.value.length - 1);
/** The stop that shows an answer beside the map: half, or the peek where there is no half. */
const rest = computed(() => (snaps.value.length === 3 ? 1 : 0));
const atPeek = computed(() => height.value <= peek.value + 1);

function indexOf(stop: Stop): number {
  return stop === 'peek' ? 0 : stop === 'top' ? top.value : rest.value;
}

function stopAt(i: number): Stop {
  return i <= 0 ? 'peek' : i >= top.value ? 'top' : 'rest';
}

function nearest(h: number): number {
  let best = 0;
  snaps.value.forEach((s, i) => {
    if (Math.abs(s - h) < Math.abs(snaps.value[best] - h)) best = i;
  });
  return best;
}

function snapTo(stop: Stop) {
  restAt = stop;
  height.value = snaps.value[indexOf(stop)];
}

const unzoomed = () => !vv || Math.abs(vv.scale - 1) < 0.01;

function measure() {
  const follow = !!vv && unzoomed();
  vh.value = Math.round(follow ? vv!.height : window.innerHeight);
  lift.value = follow ? Math.max(0, Math.round(window.innerHeight - vv!.height - vv!.offsetTop)) : 0;
  if (dragging.value) return;
  if (restAt !== null) height.value = snaps.value[indexOf(restAt)];
  else height.value = Math.min(height.value, snaps.value[top.value]);
}

/** A press is not a drag until it has travelled this far, mostly vertically. */
const SLOP = 8;
type Press = {
  x: number;
  y: number;
  h: number;
  lastY: number;
  lastT: number;
  vy: number;
  handle: boolean;
  /** Started on the content: it may pull the sheet down, never up. */
  downOnly: boolean;
};
let press: Press | null = null;

/** The scrolling box under a press, if there is one inside the sheet. */
function scrollerAt(el: Element): HTMLElement | null {
  for (let n: Element | null = el; n && n !== root.value; n = n.parentElement) {
    const box = n as HTMLElement;
    if (box.scrollHeight > box.clientHeight + 1 && /auto|scroll/.test(getComputedStyle(box).overflowY)) {
      return box;
    }
  }
  return null;
}

/**
 * Where a press lands decides whether it may move the sheet. The handle and the
 * panel header always may. The content may too, but only when there is nothing
 * under the finger to scroll first — the sheet folded to the peek, or a list
 * already at its top — and then only downwards, so a swipe up still scrolls
 * the card. Without that rule the only grip on a full sheet was a 28 px bar,
 * and a thumb that missed it did nothing at all.
 *
 * A button or a field under the finger still gets its tap: nothing is captured
 * until the press has moved, and a press that never moves is a click.
 */
function onDown(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return;
  const el = e.target instanceof Element ? e.target : null;
  if (!el) return;
  const handle = !!el.closest('[data-sheet-handle]');
  const chrome = handle || !!el.closest('header');
  if (!chrome) {
    const scroller = scrollerAt(el);
    if (!atPeek.value && scroller && scroller.scrollTop > 0) return;
  }
  press = {
    x: e.clientX,
    y: e.clientY,
    h: height.value,
    lastY: e.clientY,
    lastT: e.timeStamp,
    vy: 0,
    handle,
    downOnly: !chrome && !atPeek.value,
  };
  // The bare handle has nothing to click, and a finger can leave it in 8 px.
  if (handle) capture(e.pointerId);
}

/** Capturing is an optimisation, never a reason to lose the press. */
function capture(id: number) {
  try {
    root.value?.setPointerCapture(id);
  } catch {
    /* the pointer is already gone: the press still stands */
  }
}

function onMove(e: PointerEvent) {
  if (!press) return;
  const dy = e.clientY - press.y;
  if (!dragging.value) {
    if (Math.abs(dy) < SLOP || Math.abs(dy) < Math.abs(e.clientX - press.x)) return;
    // A pull upwards that began on the content belongs to the content.
    if (press.downOnly && dy < 0) {
      press = null;
      return;
    }
    dragging.value = true;
    restAt = null;
    capture(e.pointerId);
  }
  const dt = e.timeStamp - press.lastT;
  if (dt > 0) press.vy = (e.clientY - press.lastY) / dt;
  press.lastY = e.clientY;
  press.lastT = e.timeStamp;
  height.value = Math.min(snaps.value[top.value], Math.max(96, press.h - dy));
}

function onUp(e: PointerEvent) {
  const p = press;
  press = null;
  try {
    if (root.value?.hasPointerCapture(e.pointerId)) root.value.releasePointerCapture(e.pointerId);
  } catch {
    /* nothing to release */
  }
  if (!p) return;
  if (!dragging.value) {
    // A tap on the handle is the whole gesture for anyone who would rather not
    // drag: one step further open each time, and back to the peek from the top.
    if (p.handle) snapTo(stopAt(nearest(height.value) >= top.value ? 0 : nearest(height.value) + 1));
    return;
  }
  dragging.value = false;
  // A flick goes where it points, even when the nearest stop is behind it.
  const h = height.value;
  let i = nearest(h);
  if (p.vy < -0.4) {
    const above = snaps.value.findIndex((s) => s > h);
    i = above === -1 ? top.value : above;
  } else if (p.vy > 0.4) {
    let below = 0;
    snaps.value.forEach((s, k) => {
      if (s < h) below = k;
    });
    i = below;
  }
  snapTo(stopAt(i));
}

function step(delta: number) {
  snapTo(stopAt(nearest(height.value) + delta));
}

/**
 * Typing wants the whole height: the list opens under the field and the
 * keyboard takes the rest. Anything else pressed in the folded peek wants the
 * sheet open far enough to show what the press did.
 */
function onFocusIn(e: FocusEvent) {
  if (dragging.value) return;
  const t = e.target;
  if (t instanceof HTMLInputElement || t instanceof HTMLTextAreaElement) snapTo('top');
  else if (atPeek.value) snapTo('rest');
}

function onBodyClick() {
  if (atPeek.value && !dragging.value) snapTo('rest');
}

// The map re-frames when the sheet lands, not while a finger is still moving it.
watch([height, lift], ([h, l]) => {
  sheetHeight.value = h + l;
  if (!dragging.value) sheetSettled.value++;
});
watch(dragging, (d) => (sheetDragging.value = d));

// An answer is worth looking at beside the map: half height, whatever the
// sheet was doing — it was full while the question was typed.
watch(answeredToken, () => {
  if (hasResult.value) snapTo('rest');
});

onMounted(() => {
  measure();
  sheetHeight.value = height.value + lift.value;
  window.addEventListener('resize', measure);
  vv?.addEventListener('resize', measure);
  vv?.addEventListener('scroll', measure);
});
onBeforeUnmount(() => {
  window.removeEventListener('resize', measure);
  vv?.removeEventListener('resize', measure);
  vv?.removeEventListener('scroll', measure);
});
</script>

<template>
  <div
    ref="root"
    class="fixed inset-x-0 z-30 flex flex-col overflow-hidden rounded-t-[18px] border-t border-line bg-surface"
    :class="dragging ? 'select-none' : ''"
    :style="{
      height: `${height}px`,
      bottom: `${lift}px`,
      boxShadow: 'var(--shadow-2)',
      transition: dragging ? 'none' : 'height 0.26s cubic-bezier(0.22, 1, 0.36, 1), bottom 0.2s ease',
    }"
    @pointerdown="onDown"
    @pointermove="onMove"
    @pointerup="onUp"
    @pointercancel="onUp"
  >
    <!-- 44 px of target for a thumb, with the bar itself unchanged. -->
    <div
      data-sheet-handle
      class="flex h-11 shrink-0 cursor-grab touch-none items-center justify-center active:cursor-grabbing"
      role="separator"
      aria-label="Drag to resize the panel, or tap to open it further"
      title="Drag, or tap to open further"
      tabindex="0"
      @keydown.up.prevent="step(1)"
      @keydown.down.prevent="step(-1)"
    >
      <span class="h-1 w-9 rounded-full" :style="{ background: 'var(--line-strong)' }" />
    </div>
    <!--
      The panel header doubles as a handle — `touch-action: none` there keeps
      the browser from turning the drag into a scroll — and so does the body,
      but only at the peek, where there is nothing to scroll.
    -->
    <div
      class="min-h-0 flex-1 [&_header]:touch-none [&_header]:select-none"
      :class="atPeek || dragging ? 'touch-none' : ''"
      @focusin="onFocusIn"
      @click="onBodyClick"
    >
      <slot />
    </div>
  </div>
</template>
