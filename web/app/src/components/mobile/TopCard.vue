<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Icon from '../Icon.vue';
import ModeBar from '../ModeBar.vue';
import TripOptions from '../TripOptions.vue';
import {
  MAX_POINTS,
  addStop,
  answeredToken,
  busy,
  canCompute,
  clearAll,
  compute,
  grade,
  hasResult,
  isCombinedMode,
  mode,
  parkingIndex,
  placeIndices,
  removeSlot,
  slots,
  swapEnds,
} from '../../composables/usePlanner';
import { sheetRequest, sheetStop, topInset } from '../../composables/useMedia';
import { t } from '../../i18n';
import { wayName } from '../../i18n/service';

/**
 * The question, as a sheet over the top of the map — the twin of the answer
 * sheet at the bottom: edge to edge, rounded on the side that faces the map,
 * a bar to take hold of and a chevron to press. Open, it is a row per place
 * (tapping one opens the search screen for it), the mode as five icons with
 * a line of words under them, and the grade as a badge that opens the grade
 * and the lifts. Folded, it is one line — where from, where to, how — and
 * the map has the screen. It folds at any time, question or no question:
 * someone who came for the map should not have to answer first.
 *
 * The two sheets never stand on the map together: opening the answer folds
 * the question, opening the question drops the answer to its peek.
 */
const emit = defineEmits<{ edit: [index: number]; menu: [] }>();

const walks = computed(() => mode.value.includes('hike'));

const places = computed(() =>
  placeIndices.value.map((index, at) => ({ index, at, slot: slots.value[index] })),
);
const canAdd = computed(() => places.value.length < MAX_POINTS);
/** A car+hike or bike+hike trip wants to know where the wheels stop. */
const wantsParking = computed(() => isCombinedMode.value && places.value.length < 3 && canAdd.value);
const anyPoint = computed(() => places.value.some((p) => p.slot.point));

function label(at: number, index: number): string {
  if (at === 0) return t('point.from');
  if (at === places.value.length - 1) return t('point.to');
  return index === parkingIndex.value ? t('point.park') : t('point.stop', { n: at });
}

async function addParking() {
  addStop();
  await nextTick();
  const p = placeIndices.value;
  emit('edit', p[p.length - 2]);
}

// --- folded or open ---------------------------------------------------------

/**
 * Folded to one line. A sheet that arrives over an answer — the layout
 * crossed into a phone's width after the route was found — starts folded,
 * as it would have been.
 */
const folded = ref(hasResult.value);
const options = ref(false);

function fold() {
  folded.value = true;
}

function unfold() {
  folded.value = false;
  // The answer was filling the screen: the question needs its room back.
  if (sheetStop.value === 'rest' || sheetStop.value === 'top') sheetRequest.value = 'peek';
}

/** Everything cleared, and the search open on the first field: the fast way to the next trip. */
function newRoute() {
  clearAll();
  folded.value = false;
  emit('edit', placeIndices.value[0]);
}

// An answer to a question the person asked closes the question. A quiet
// re-run — a grade changed, a marker nudged — is not that, and answeredToken
// does not move for it.
watch(answeredToken, () => {
  if (canCompute.value) folded.value = true;
});
// Opening the answer folds the question.
watch(sheetStop, (s) => {
  if (s === 'rest' || s === 'top') folded.value = true;
});

/**
 * The bar is for the thumb as much as the chevron is: a pull up on the open
 * sheet folds it, a pull down on the strip opens it, and a press that never
 * travels is a tap and does the same as the chevron beside it.
 */
const SWIPE = 36;
let press: { y: number; id: number; el: HTMLElement } | null = null;
function onDown(e: PointerEvent) {
  if (e.pointerType === 'mouse' && e.button !== 0) return;
  press = { y: e.clientY, id: e.pointerId, el: e.currentTarget as HTMLElement };
  try {
    press.el.setPointerCapture(e.pointerId);
  } catch {
    /* the press still counts */
  }
}
function onUp(e: PointerEvent) {
  const p = press;
  press = null;
  if (!p) return;
  try {
    p.el.releasePointerCapture(p.id);
  } catch {
    /* already released */
  }
  const dy = e.clientY - p.y;
  if (dy <= -SWIPE) fold();
  else if (dy >= SWIPE) unfold();
  else if (folded.value) unfold();
  else fold();
}
function onCancel() {
  press = null;
}

/** The names, in order, for the folded line. */
const names = computed(() => places.value.map((p) => wayName(p.slot.point?.name) || label(p.at, p.index)));

/** The map needs to know how much of its top this covers, folded or not. */
const root = ref<HTMLElement | null>(null);
let ro: ResizeObserver | null = null;
function measure() {
  const el = root.value;
  if (el) topInset.value = Math.round(el.getBoundingClientRect().bottom);
}
onMounted(() => {
  measure();
  if (typeof ResizeObserver !== 'undefined' && root.value) {
    ro = new ResizeObserver(measure);
    ro.observe(root.value);
  }
});
onBeforeUnmount(() => {
  ro?.disconnect();
  topInset.value = 0;
});
</script>

<template>
  <div
    ref="root"
    class="fixed inset-x-0 top-0 z-20 overflow-hidden rounded-b-[18px] border-b border-line bg-surface"
    :style="{ paddingTop: 'env(safe-area-inset-top, 0px)', boxShadow: 'var(--shadow-2)' }"
  >
    <Transition name="top" mode="out-in">
      <!-- Folded: the question in one line, the bar under it, and the chevron
           that opens it. The cross starts over, once there is a trip to leave. -->
      <div
        v-if="folded"
        key="strip"
        class="strip"
        role="separator"
        :aria-label="t('top.unfold')"
        @pointerdown="onDown"
        @pointerup="onUp"
        @pointercancel="onCancel"
      >
        <div class="flex h-[38px] items-center pl-4 pr-1">
          <button type="button" class="strip-body" :aria-label="t('top.editRoute')" @pointerdown.stop @click="unfold">
            <span class="flex shrink-0 items-center gap-0.5" aria-hidden="true">
              <Icon v-if="!anyPoint" name="search" :size="15" class="text-muted" />
              <Icon v-for="(ic, i) in anyPoint ? mode.split('+') : []" :key="i" :name="ic" :size="15" :style="{ color: `var(--${ic})` }" />
            </span>
            <span v-if="!anyPoint" class="min-w-0 flex-1 truncate text-[14px] text-muted">{{ t('top.prompt') }}</span>
            <span v-else class="min-w-0 flex-1 truncate text-[14px] text-ink">
              <template v-for="(n, i) in names" :key="i"><span v-if="i" class="text-faint"> → </span>{{ n }}</template>
            </span>
            <span
              v-if="anyPoint && walks"
              class="shrink-0 text-[12px] font-semibold"
              :style="{ color: `var(--grade-${grade.toLowerCase()})` }"
              >{{ grade }}</span
            >
          </button>
          <button v-if="canCompute" type="button" class="ctl" :aria-label="t('top.newRoute')" :title="t('top.newRoute')" @pointerdown.stop @click="newRoute">
            <Icon name="x" :size="17" />
          </button>
          <button type="button" class="ctl" :aria-label="t('top.unfold')" :title="t('top.unfold')" @pointerdown.stop @click="unfold">
            <Icon name="chevronDown" :size="18" />
          </button>
        </div>
        <span class="bar" aria-hidden="true" />
      </div>

      <div v-else key="card">
        <div class="flex">
          <div class="flex min-w-0 flex-1 flex-col justify-center py-1">
            <div v-for="p in places" :key="p.slot.key" class="flex items-center">
              <button
                type="button"
                class="place-row"
                :aria-label="p.slot.point ? `${label(p.at, p.index)}: ${wayName(p.slot.point.name)}` : label(p.at, p.index)"
                @click="emit('edit', p.index)"
              >
                <span class="grid size-6 shrink-0 place-items-center" aria-hidden="true">
                  <svg v-if="p.at === 0" width="14" height="14" viewBox="0 0 14 14">
                    <circle cx="7" cy="7" r="5" fill="none" stroke="var(--ink)" stroke-width="2.4" />
                  </svg>
                  <svg v-else-if="p.at === places.length - 1" width="13" height="16" viewBox="0 0 13 16">
                    <path
                      d="M6.5 15.5S12 9.4 12 6.1A5.5 5.5 0 1 0 1 6.1C1 9.4 6.5 15.5 6.5 15.5z"
                      fill="var(--dest)"
                    />
                    <circle cx="6.5" cy="6.1" r="2" fill="var(--surface)" />
                  </svg>
                  <svg v-else-if="p.index === parkingIndex" width="14" height="14" viewBox="0 0 14 14">
                    <rect x="1.2" y="1.2" width="11.6" height="11.6" rx="3" fill="none" stroke="var(--muted)" stroke-width="1.6" />
                    <text x="7" y="10" text-anchor="middle" font-size="8" font-weight="700" fill="var(--muted)">P</text>
                  </svg>
                  <svg v-else width="14" height="14" viewBox="0 0 14 14">
                    <circle cx="7" cy="7" r="5.2" fill="none" stroke="var(--muted)" stroke-width="1.6" />
                    <text x="7" y="9.6" text-anchor="middle" font-size="7" font-weight="600" fill="var(--muted)">
                      {{ p.at }}
                    </text>
                  </svg>
                </span>
                <span class="min-w-0 flex-1 truncate text-[15px]" :class="p.slot.point ? 'text-ink' : 'text-faint'">
                  {{ wayName(p.slot.point?.name) || label(p.at, p.index) }}
                </span>
              </button>
              <button
                v-if="p.at > 0 && p.at < places.length - 1"
                type="button"
                class="ctl mr-1"
                :aria-label="t('point.remove')"
                @click="removeSlot(p.index)"
              >
                <Icon name="x" :size="15" />
              </button>
            </div>

            <button v-if="wantsParking" type="button" class="place-row park-row text-muted" @click="addParking">
              <span class="grid size-6 shrink-0 place-items-center" aria-hidden="true">
                <Icon name="plus" :size="15" />
              </span>
              <span class="min-w-0 flex-1 truncate text-[13px]">{{ t('top.addParkStop') }}</span>
            </button>
          </div>

          <div class="flex shrink-0 flex-col items-center justify-center gap-0.5 border-l border-line px-1.5 py-1">
            <button
              type="button"
              class="ctl ctl-primary"
              :disabled="!canCompute || busy"
              :aria-label="t('top.search')"
              :title="t('top.search')"
              @click="compute()"
            >
              <Icon name="search" :size="17" />
            </button>
            <button type="button" class="ctl" :aria-label="t('search.swap')" @click="swapEnds">
              <Icon name="swap" :size="17" />
            </button>
            <button type="button" class="ctl" :aria-label="t('top.menu')" @click="emit('menu')">
              <Icon name="dots" :size="17" />
            </button>
          </div>
        </div>

        <div class="border-t border-line px-3 pt-2">
          <ModeBar :options-open="options" @options="options = true" />
        </div>

        <!-- The bar and the chevron, as the answer sheet has them: pull up or
             press to fold the question away. -->
        <div
          class="handle"
          role="separator"
          :aria-label="t('top.fold')"
          @pointerdown="onDown"
          @pointerup="onUp"
          @pointercancel="onCancel"
        >
          <span class="bar" aria-hidden="true" />
          <button type="button" class="fold" :aria-label="t('top.fold')" :title="t('top.fold')" @pointerdown.stop @click="fold">
            <Icon name="chevronUp" :size="18" />
          </button>
        </div>
      </div>
    </Transition>

    <Teleport to="body">
      <Transition name="rise">
        <TripOptions v-if="options" @close="options = false" />
      </Transition>
    </Teleport>
  </div>
</template>

<style scoped>
.strip {
  position: relative;
  touch-action: none;
  cursor: grab;
}
.strip-body {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  gap: 8px;
  height: 100%;
  text-align: left;
}
.strip-body:active {
  opacity: 0.7;
}
.bar {
  display: block;
  width: 36px;
  height: 4px;
  margin: 0 auto 7px;
  border-radius: 999px;
  background: var(--line-strong);
}
.handle {
  position: relative;
  display: flex;
  height: 30px;
  align-items: flex-end;
  justify-content: center;
  touch-action: none;
  cursor: grab;
}
.handle:active,
.strip:active {
  cursor: grabbing;
}
.handle:active .bar {
  background: var(--muted);
}
.fold {
  position: absolute;
  top: -8px;
  right: 6px;
  display: grid;
  width: 44px;
  height: 38px;
  place-items: center;
  border-radius: var(--radius-control);
  color: var(--muted);
}
.fold:active {
  background: var(--surface-3);
  color: var(--ink);
}
.place-row {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  padding: 0 8px 0 16px;
  text-align: left;
}
.place-row:active {
  background: var(--surface-2);
}
.park-row {
  min-height: 36px;
}
.ctl {
  display: grid;
  place-items: center;
  width: 40px;
  height: 40px;
  border-radius: var(--radius-control);
  color: var(--muted);
}
.ctl:active {
  background: var(--surface-3);
  color: var(--ink);
}
/* The one filled button on the card: press it and the question is asked. */
.ctl-primary,
.ctl-primary:active {
  border-radius: 999px;
  background: var(--ink);
  color: var(--ink-invert);
  transition: opacity 0.14s ease;
}
.ctl-primary:disabled {
  opacity: 0.3;
}

.top-enter-active,
.top-leave-active {
  transition: opacity 0.15s ease, transform 0.15s ease;
}
.top-enter-from,
.top-leave-to {
  opacity: 0;
  transform: translateY(-6px);
}
.rise-enter-active,
.rise-leave-active {
  transition: opacity 0.2s ease;
}
.rise-enter-active :deep(.sheet),
.rise-leave-active :deep(.sheet) {
  transition: transform 0.22s cubic-bezier(0.22, 1, 0.36, 1);
}
.rise-enter-from,
.rise-leave-to {
  opacity: 0;
}
.rise-enter-from :deep(.sheet),
.rise-leave-to :deep(.sheet) {
  transform: translateY(100%);
}
</style>
