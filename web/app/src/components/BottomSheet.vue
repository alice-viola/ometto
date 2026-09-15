<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Icon from './Icon.vue';
import { answeredToken, hasResult } from '../composables/usePlanner';
import { sheetDragging, sheetHeight, sheetSettled, topInset } from '../composables/useMedia';

/**
 * The stops, from the bottom up. `peek` shows whatever the sheet is for — the
 * headline of an answer, or a pair of fields — and no more of the map than
 * that. `closed`, when the parent gives it a height, is a strip no taller than
 * the handle: the whole map, with one line of answer along its bottom edge,
 * for anyone who came for the map.
 */
const props = withDefaults(defineProps<{ peek?: number; closed?: number }>(), { peek: 232, closed: 0 });

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
/** The home indicator's band: every stop keeps its content above it. */
const sab = ref(0);

const height = ref(props.peek);
const dragging = ref(false);
/**
 * The stop the sheet last settled on, so a keyboard or a rotation puts it back
 * there. Kept as a name, not a pixel height or an index: the stops themselves
 * move with the viewport, and on a phone the viewport is still settling while
 * the first answer lands.
 */
type Stop = 'closed' | 'peek' | 'rest' | 'top';
const restAt = ref<Stop | null>('peek');
/** The drag under way began on the closed strip: the strip stays until it ends. */
const fromClosed = ref(false);
const root = ref<HTMLElement | null>(null);

type Snap = { name: Stop; h: number };
const snaps = computed<Snap[]>(() => {
  const h = vh.value;
  const all: Snap[] = [];
  if (props.closed > 0) all.push({ name: 'closed', h: props.closed + sab.value });
  all.push({ name: 'peek', h: Math.min(props.peek + sab.value, Math.round(h * 0.42)) });
  all.push({ name: 'rest', h: Math.round(h * 0.52) });
  // The top stop leaves the floating search card readable above the sheet.
  all.push({ name: 'top', h: Math.round(h - Math.max(72, topInset.value + 8)) });
  // A phone on its side has no room for four stops: keep the ones that are
  // apart, and of two that are not, the one the sheet cannot do without.
  const out: Snap[] = [];
  for (const s of all) {
    const last = out[out.length - 1];
    if (last && s.h - last.h < 40) {
      if (s.name === 'rest') continue;
      out.pop();
    }
    out.push(s);
  }
  return out;
});
const top = computed(() => snaps.value.length - 1);

function indexOf(stop: Stop): number {
  const i = snaps.value.findIndex((s) => s.name === stop);
  if (i >= 0) return i;
  // A stop the viewport had no room for: the nearest one that is there.
  return stop === 'top' ? top.value : stop === 'rest' ? indexOf('peek') : 0;
}

const stopAt = (i: number): Stop => snaps.value[Math.max(0, Math.min(top.value, i))].name;
const peekH = computed(() => snaps.value[indexOf('peek')].h);
/** At the peek or folded below it: nothing under the finger can scroll. */
const atPeek = computed(() => height.value <= peekH.value + 1);
const closedNow = computed(() => restAt.value === 'closed' || (dragging.value && fromClosed.value));

function nearest(h: number): number {
  let best = 0;
  snaps.value.forEach((s, i) => {
    if (Math.abs(s.h - h) < Math.abs(snaps.value[best].h - h)) best = i;
  });
  return best;
}

function snapTo(stop: Stop) {
  restAt.value = stop;
  height.value = snaps.value[indexOf(stop)].h;
}

const unzoomed = () => !vv || Math.abs(vv.scale - 1) < 0.01;

function measure() {
  const follow = !!vv && unzoomed();
  vh.value = Math.round(follow ? vv!.height : window.innerHeight);
  lift.value = follow ? Math.max(0, Math.round(window.innerHeight - vv!.height - vv!.offsetTop)) : 0;
  if (root.value) sab.value = parseFloat(getComputedStyle(root.value).paddingBottom) || 0;
}

// The stops move with the viewport, with the search card and with what the
// sheet holds: it stays on the stop it was resting on, never on a stale height.
watch(snaps, () => {
  if (!dragging.value && restAt.value !== null) height.value = snaps.value[indexOf(restAt.value)].h;
});

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
    fromClosed.value = restAt.value === 'closed';
    restAt.value = null;
    capture(e.pointerId);
  }
  const dt = e.timeStamp - press.lastT;
  if (dt > 0) press.vy = (e.clientY - press.lastY) / dt;
  press.lastY = e.clientY;
  press.lastT = e.timeStamp;
  // Nothing below the strip; without a strip, a little give under the peek.
  const floor = props.closed > 0 ? snaps.value[0].h : Math.min(96, snaps.value[0].h);
  height.value = Math.min(snaps.value[top.value].h, Math.max(floor, press.h - dy));
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
    // drag: one stop further open each time, and from the top back to the
    // peek. Folding the sheet away is deliberate — a pull down, or the
    // chevron — never a tap that missed.
    if (p.handle) {
      const i = nearest(height.value);
      snapTo(i >= top.value ? 'peek' : stopAt(i + 1));
    }
    return;
  }
  dragging.value = false;
  fromClosed.value = false;
  // A flick goes where it points, even when the nearest stop is behind it.
  const h = height.value;
  let i = nearest(h);
  if (p.vy < -0.4) {
    const above = snaps.value.findIndex((s) => s.h > h);
    i = above === -1 ? top.value : above;
  } else if (p.vy > 0.4) {
    let below = 0;
    snaps.value.forEach((s, k) => {
      if (s.h < h) below = k;
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

function onBodyClick(e: MouseEvent) {
  // A press whose effect is on the map — moving a marker, dropping a via —
  // has nothing to show in the sheet, and leaves it where it is.
  if (e.target instanceof Element && e.target.closest('[data-sheet-stay]')) return;
  if (atPeek.value && !dragging.value) snapTo('rest');
}

// The map re-frames when the sheet lands, not while a finger is still moving it.
watch([height, lift], ([h, l]) => {
  sheetHeight.value = h + l;
  if (!dragging.value) sheetSettled.value++;
});
watch(dragging, (d) => (sheetDragging.value = d));

// An answer is worth looking at beside the map: the headline at the peek,
// with the route framed above it, whatever the sheet was doing.
watch(answeredToken, () => {
  if (hasResult.value) snapTo('peek');
});

onMounted(() => {
  measure();
  height.value = snaps.value[indexOf('peek')].h;
  sheetHeight.value = height.value + lift.value;
  window.addEventListener('resize', measure);
  vv?.addEventListener('resize', measure);
  vv?.addEventListener('scroll', measure);
});
onBeforeUnmount(() => {
  window.removeEventListener('resize', measure);
  vv?.removeEventListener('resize', measure);
  vv?.removeEventListener('scroll', measure);
  // A sheet that is gone covers nothing: the map gets its band back.
  sheetHeight.value = 0;
  sheetSettled.value++;
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
      paddingBottom: 'env(safe-area-inset-bottom, 0px)',
      boxShadow: 'var(--shadow-2)',
      transition: dragging ? 'none' : 'height 0.26s cubic-bezier(0.22, 1, 0.36, 1), bottom 0.2s ease',
    }"
    @pointerdown="onDown"
    @pointermove="onMove"
    @pointerup="onUp"
    @pointercancel="onUp"
  >
    <!-- 44 px of target for a thumb, with the bar itself unchanged. Folded to
         the strip, the same row carries the one line that says what the
         sheet holds. -->
    <div
      data-sheet-handle
      class="relative flex h-11 shrink-0 cursor-grab touch-none flex-col items-center active:cursor-grabbing"
      :class="closedNow ? 'justify-start pt-[7px]' : 'justify-center'"
      role="separator"
      :aria-label="closedNow ? 'Tap or drag to open the details' : 'Drag to resize the panel, or tap to open it further'"
      :title="closedNow ? 'Tap or drag to open' : 'Drag, or tap to open further'"
      tabindex="0"
      @keydown.up.prevent="step(1)"
      @keydown.down.prevent="step(-1)"
    >
      <span class="h-1 w-9 shrink-0 rounded-full" :style="{ background: 'var(--line-strong)' }" />
      <div
        v-if="closedNow"
        class="mt-1.5 flex max-w-full items-center gap-1.5 whitespace-nowrap px-4 text-[13.5px] leading-5"
      >
        <slot name="closed" />
      </div>
      <button
        v-if="closed > 0 && !closedNow"
        type="button"
        class="fold"
        aria-label="Hide the details"
        title="Hide the details"
        @pointerdown.stop
        @click.stop="snapTo('closed')"
      >
        <Icon name="chevronDown" :size="18" />
      </button>
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

<style scoped>
.fold {
  position: absolute;
  top: 0;
  right: 6px;
  display: grid;
  width: 44px;
  height: 44px;
  place-items: center;
  border-radius: var(--radius-control);
  color: var(--muted);
}
.fold:active {
  background: var(--surface-3);
  color: var(--ink);
}
</style>
