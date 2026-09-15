<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, ref, watch } from 'vue';
import BottomSheet from '../BottomSheet.vue';
import ResultsBlock from '../ResultsBlock.vue';
import Icon from '../Icon.vue';
import TopCard from './TopCard.vue';
import SearchScreen from './SearchScreen.vue';
import MenuScreen, { type MenuView } from './MenuScreen.vue';
import Notes from './Notes.vue';
import {
  alternatives,
  answeredToken,
  autoAsk,
  busy,
  canCompute,
  compute,
  hasResult,
  routes,
  selectedId,
  status,
} from '../../composables/usePlanner';
import { frameRequest, sheetHeight } from '../../composables/useMedia';
import { fmtAscent, fmtDistance, fmtDuration } from '../../lib/format';

/**
 * The phone layout. The map is the whole screen; the question floats over its
 * top as a card, the answer rises from its bottom as a sheet, and typing takes
 * a screen of its own. Nothing here holds state: it is all in usePlanner,
 * which is also what the desktop panel draws from.
 */

/**
 * A phone always answers with the alternatives: the choice is what the sheet
 * is for, and a chip to ask for it looked like one more travel mode.
 */
alternatives.value = 3;

/** The slot being typed into, or null when the map is showing. */
const searching = ref<number | null>(null);
const menu = ref<MenuView | null>(null);

/**
 * There is no Compute button on a phone. Both ends set IS the question, the
 * way it is in every map app people carry — and it is an explicit one, so it
 * lands in history and frames the answer. A shared link and a history row
 * compute for themselves before this can, which is what the busy check is
 * for: they are already loading by the time the slots have changed. After
 * that the planner asks again by itself whenever the question changes.
 */
autoAsk.value = true;
onBeforeUnmount(() => (autoAsk.value = false));
watch(canCompute, (ok) => {
  if (ok && !hasResult.value && !busy.value) void compute();
});

/** The sheet exists only while there is something to say. */
const showSheet = computed(() => hasResult.value || status.value !== 'idle');
/** With the sheet over most of the screen there is no map left to frame. */
const sheetTall = computed(() => sheetHeight.value > (typeof window !== 'undefined' ? window.innerHeight : 800) * 0.5);

/** The route the sheet is about: the chosen one, or the first. */
const chosen = computed(
  () => routes.value.find((r) => r.id === selectedId.value) ?? routes.value[0] ?? null,
);
const chosenModes = computed(() => {
  const seen: string[] = [];
  for (const l of chosen.value?.legs ?? []) if (!seen.includes(l.mode)) seen.push(l.mode);
  return seen.length ? seen : ['car'];
});
/** The climb the strip quotes: the walker's on a mixed trip, the route's otherwise. */
const chosenAscent = computed(() => {
  const r = chosen.value;
  if (!r) return 0;
  const legs = r.legs ?? [];
  const walk = legs.filter((l) => l.mode === 'hike');
  if (walk.length && walk.length < legs.length) return walk.reduce((a, l) => a + l.ascent, 0);
  return r.ascent;
});

/**
 * The peek is measured, not guessed: the handle, whatever the notes about the
 * points take, and the headline of the first card — so the lowest open stop
 * shows the answer whole and never cuts it off. The sheet caps it itself.
 */
const HANDLE = 44;
const content = ref<HTMLElement | null>(null);
const peek = ref(184);
let ro: ResizeObserver | null = null;

function measurePeek() {
  const box = content.value;
  if (!box) return;
  // The skeleton is not worth measuring: the answer is seconds away.
  if (!hasResult.value && status.value === 'loading') return;
  const head = box.querySelector<HTMLElement>('article > button') ?? box.querySelector<HTMLElement>('.card');
  if (!head) return;
  const bottom = head.getBoundingClientRect().bottom - box.getBoundingClientRect().top;
  peek.value = Math.round(HANDLE + bottom + 14);
}
watch(content, (el) => {
  ro?.disconnect();
  ro = null;
  if (!el) return;
  measurePeek();
  if (typeof ResizeObserver !== 'undefined') {
    ro = new ResizeObserver(measurePeek);
    ro.observe(el);
  }
});
watch(answeredToken, () => void nextTick(measurePeek));
onBeforeUnmount(() => ro?.disconnect());
</script>

<template>
  <TopCard @edit="searching = $event" @menu="menu = 'menu'" />

  <!-- Just above the sheet: one button that puts the whole answer back on
       screen after a pan or a pinch. -->
  <button
    v-if="hasResult && searching === null && !menu"
    v-show="!sheetTall"
    class="fab"
    :style="{ bottom: `${sheetHeight + 14}px` }"
    aria-label="Show the whole route"
    title="Show the whole route"
    @click="frameRequest++"
  >
    <Icon name="route" :size="18" />
  </button>

  <BottomSheet v-if="showSheet" :peek="peek" :closed="chosen ? 44 : 0">
    <!-- Folded to a strip: the route in one line, and a grip to open it. -->
    <template v-if="chosen" #closed>
      <Icon v-for="m in chosenModes" :key="m" :name="m" :size="15" :style="{ color: `var(--${m})` }" />
      <span class="font-semibold text-ink">{{ fmtDuration(chosen.seconds) }}</span>
      <span class="text-muted">
        {{ fmtDistance(chosen.meters) }} · {{ fmtAscent(chosenAscent) }}
      </span>
    </template>
    <div class="scroll-quiet h-full overflow-y-auto overscroll-contain px-3 pb-6">
      <div ref="content">
        <Notes />
        <ResultsBlock />
      </div>
    </div>
  </BottomSheet>

  <SearchScreen v-if="searching !== null" :index="searching" @close="searching = null" />
  <MenuScreen v-if="menu" :view="menu" @close="menu = null" />
</template>

<style scoped>
.fab {
  position: fixed;
  right: 12px;
  z-index: 20;
  display: grid;
  place-items: center;
  width: 46px;
  height: 46px;
  border-radius: 999px;
  border: 1px solid var(--line);
  background: var(--surface);
  color: var(--ink);
  box-shadow: var(--shadow-2);
  transition: bottom 0.26s cubic-bezier(0.22, 1, 0.36, 1);
}
.fab:active {
  background: var(--surface-3);
}
</style>
