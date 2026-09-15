<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref } from 'vue';
import Icon from '../Icon.vue';
import { GRADES, GRADE_NAMES, MODE_LABELS } from '../../lib/format';
import type { Mode } from '../../lib/types';
import {
  MAX_POINTS,
  addStop,
  busy,
  canCompute,
  compute,
  grade,
  isCombinedMode,
  lifts,
  mode,
  parkingIndex,
  placeIndices,
  removeSlot,
  slots,
  swapEnds,
} from '../../composables/usePlanner';
import { topInset } from '../../composables/useMedia';

/**
 * The question, floating over the top of the map: where from, where to, how.
 * A row is a place; tapping it opens the search screen for that place. Under
 * the rows, the mode as a strip of chips, and the trail grade as a second
 * strip whenever the mode walks. Everything a thumb presses is 44 px tall.
 *
 * The page asks by itself whenever the question changes, and still there is a
 * Search button, in the card's right column: the one thing to press when you
 * want it asked, now, and would rather not wonder whether it was.
 */
const emit = defineEmits<{ edit: [index: number]; menu: [] }>();

const MODES: Mode[] = ['car', 'bike', 'hike', 'car+hike', 'bike+hike'];
const walks = computed(() => mode.value.includes('hike'));

const places = computed(() =>
  placeIndices.value.map((index, at) => ({ index, at, slot: slots.value[index] })),
);
const canAdd = computed(() => places.value.length < MAX_POINTS);
/** A car+hike or bike+hike trip wants to know where the wheels stop. */
const wantsParking = computed(() => isCombinedMode.value && places.value.length < 3 && canAdd.value);

function label(at: number, index: number): string {
  if (at === 0) return 'From';
  if (at === places.value.length - 1) return 'To';
  return index === parkingIndex.value ? 'Where you park' : `Stop ${at}`;
}

async function addParking() {
  addStop();
  await nextTick();
  const p = placeIndices.value;
  emit('edit', p[p.length - 2]);
}

/** The map needs to know how much of its top this card covers. */
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
    class="fixed inset-x-0 top-0 z-20 px-3"
    :style="{ paddingTop: 'calc(env(safe-area-inset-top, 0px) + 10px)' }"
  >
    <div class="card overflow-hidden" :style="{ boxShadow: 'var(--shadow-2)' }">
      <div class="flex">
        <div class="flex min-w-0 flex-1 flex-col justify-center py-1">
          <div v-for="p in places" :key="p.slot.key" class="flex items-center">
            <button
              type="button"
              class="place-row"
              :aria-label="p.slot.point ? `${label(p.at, p.index)}: ${p.slot.point.name}` : label(p.at, p.index)"
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
                {{ p.slot.point?.name || label(p.at, p.index) }}
              </span>
            </button>
            <button
              v-if="p.at > 0 && p.at < places.length - 1"
              type="button"
              class="ctl mr-1"
              aria-label="Remove this stop"
              @click="removeSlot(p.index)"
            >
              <Icon name="x" :size="15" />
            </button>
          </div>

          <button v-if="wantsParking" type="button" class="place-row text-muted" @click="addParking">
            <span class="grid size-6 shrink-0 place-items-center" aria-hidden="true">
              <Icon name="plus" :size="15" />
            </span>
            <span class="min-w-0 flex-1 truncate text-[13.5px]">Add a stop where you park</span>
          </button>
        </div>

        <div class="flex shrink-0 flex-col items-center justify-center gap-0.5 border-l border-line px-1 py-1">
          <button
            type="button"
            class="ctl ctl-primary"
            :disabled="!canCompute || busy"
            aria-label="Search"
            title="Search"
            @click="compute()"
          >
            <Icon name="search" :size="17" />
          </button>
          <button type="button" class="ctl" aria-label="Swap start and destination" @click="swapEnds">
            <Icon name="swap" :size="17" />
          </button>
          <button type="button" class="ctl" aria-label="Menu" @click="emit('menu')">
            <Icon name="dots" :size="17" />
          </button>
        </div>
      </div>

      <div class="chips border-t border-line" role="radiogroup" aria-label="Travel mode">
        <button
          v-for="m in MODES"
          :key="m"
          type="button"
          role="radio"
          class="chip"
          :class="mode === m ? 'is-on' : ''"
          :aria-checked="mode === m"
          @click="mode = m"
        >
          <span class="flex items-center gap-0.5">
            <Icon v-for="(ic, i) in m.split('+')" :key="i" :name="ic" :size="15" />
          </span>
          <span>{{ MODE_LABELS[m] }}</span>
        </button>
      </div>

      <div v-if="walks" class="chips border-t border-line" role="radiogroup" aria-label="Trail grade for the walking part">
        <button
          v-for="g in GRADES"
          :key="g"
          type="button"
          role="radio"
          class="chip chip-grade"
          :class="[grade === g ? 'is-on' : '', g === 'A' ? 'is-alpine' : '']"
          :style="{ '--g': `var(--grade-${g.toLowerCase()})` }"
          :aria-checked="grade === g"
          :aria-label="`${g} — ${GRADE_NAMES[g]}`"
          @click="grade = g"
        >
          <Icon v-if="g === 'A'" name="warning" :size="12" :width="2" />
          <span class="font-semibold">{{ g }}</span>
          <span class="bar" aria-hidden="true" />
        </button>
        <span class="chip-gap" aria-hidden="true" />
        <!-- A switch, not one more grade, and drawn as one. -->
        <button
          type="button"
          role="switch"
          class="chip chip-switch"
          :class="lifts ? 'is-on' : ''"
          :aria-checked="lifts"
          @click="lifts = !lifts"
        >
          <Icon name="lift" :size="14" />
          <span>Lifts</span>
          <span class="track" aria-hidden="true"><span class="knob" /></span>
        </button>
      </div>
    </div>
  </div>
</template>

<style scoped>
.place-row {
  display: flex;
  min-width: 0;
  flex: 1;
  align-items: center;
  gap: 10px;
  min-height: 44px;
  padding: 0 8px 0 12px;
  text-align: left;
}
.place-row:active {
  background: var(--surface-2);
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

/* One strip of chips that scrolls sideways; the scrollbar stays out of sight. */
.chips {
  display: flex;
  align-items: center;
  gap: 6px;
  padding: 7px 10px;
  overflow-x: auto;
  overscroll-behavior-x: contain;
  -webkit-overflow-scrolling: touch;
  scrollbar-width: none;
}
.chips::-webkit-scrollbar {
  display: none;
}
.chip-gap {
  flex: 0 0 4px;
}
.chip {
  display: inline-flex;
  flex: 0 0 auto;
  align-items: center;
  gap: 5px;
  min-height: 38px;
  padding: 0 12px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--surface-2);
  color: var(--muted);
  font-size: 13px;
  font-weight: 500;
  white-space: nowrap;
  transition:
    background 0.14s ease,
    color 0.14s ease,
    border-color 0.14s ease;
}
.chip.is-on {
  background: var(--ink);
  border-color: var(--ink);
  color: var(--ink-invert);
}
/* The grade keeps its colour bar under the letter, on and off. */
.chip-grade {
  position: relative;
  padding-bottom: 3px;
}
.chip-grade .bar {
  position: absolute;
  left: 12px;
  right: 12px;
  bottom: 5px;
  height: 3px;
  border-radius: 2px;
  background: var(--g);
  opacity: 0.55;
}
.chip-grade.is-on {
  background: var(--surface);
  border-color: var(--ink);
  color: var(--ink);
}
.chip-grade.is-on .bar {
  opacity: 1;
}
.chip-grade.is-alpine {
  color: var(--grade-a);
}
.chip-grade.is-alpine.is-on {
  border-color: var(--grade-a);
  box-shadow: inset 0 0 0 1px var(--grade-a);
}

/* A switch among the chips — something on or off, not one of a set — carries
   a small toggle and never inverts the way a chosen mode does. */
.chip-switch,
.chip-switch.is-on {
  background: var(--surface);
  border-color: var(--line);
  color: var(--muted);
}
.chip-switch.is-on {
  border-color: var(--line-strong);
  color: var(--ink);
}
.chip-switch .track {
  position: relative;
  width: 26px;
  height: 16px;
  margin-left: 2px;
  border: 1px solid var(--line);
  border-radius: 8px;
  background: var(--surface-3);
  transition:
    background 0.14s ease,
    border-color 0.14s ease;
}
.chip-switch.is-on .track {
  border-color: var(--ink);
  background: var(--ink);
}
.chip-switch .knob {
  position: absolute;
  top: 1px;
  left: 1px;
  width: 12px;
  height: 12px;
  border-radius: 50%;
  background: var(--surface);
  transition: transform 0.14s ease;
}
.chip-switch.is-on .knob {
  background: var(--ink-invert);
  transform: translateX(10px);
}
</style>
