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
import {
  arrivalClock,
  following,
  live,
  position,
  status as liveStatus,
  toggle as toggleLive,
} from '../../composables/useLocation';
import { fmtAscent, fmtDistance, fmtDuration } from '../../lib/format';
import { t } from '../../i18n';

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
 * The buttons that float over the map, from the sheet upwards. The "whole
 * route" one is only there with an answer to frame; the locate button is there
 * whenever the map is, and stacks above it when both are.
 */
const routeFab = computed(() => hasResult.value && !sheetTall.value);
function fabBottom(stacked: boolean): string {
  const lift = stacked ? 70 : 14;
  // Without a sheet there is nothing between the button and the home
  // indicator, so it keeps clear of that instead.
  return sheetHeight.value > 0
    ? `${sheetHeight.value + lift}px`
    : `calc(env(safe-area-inset-bottom, 0px) + ${lift}px)`;
}

/** Off, looking, on, or on and moving with you: one button says all four. */
const locateLabel = computed(() => {
  if (following.value) return t('live.stopFollowing');
  if (liveStatus.value === 'on') return t('live.followPosition');
  return t('live.showPosition');
});

/**
 * Folded to the strip, the one line is about the journey you are on rather
 * than the one you chose: the same figures as the card, in the same order.
 */
const liveStrip = computed(() => {
  if (liveStatus.value !== 'on' || !position.value) return null;
  const p = live.value;
  if (!p) return null;
  return {
    distance: fmtDistance(p.remainingMeters),
    time: fmtDuration(p.remainingSeconds),
    ascent: fmtAscent(p.remainingAscent),
    at: arrivalClock(p.remainingSeconds),
    arrived: p.arrived,
  };
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
    :style="{ bottom: fabBottom(false) }"
    :aria-label="t('mobile.wholeRoute')"
    :title="t('mobile.wholeRoute')"
    @click="frameRequest++"
  >
    <Icon name="route" :size="18" />
  </button>

  <!-- And above that: where you are, and whether the map goes with you. -->
  <button
    v-if="searching === null && !menu"
    class="fab"
    :class="{
      'is-locating': liveStatus === 'locating',
      'is-on': liveStatus === 'on',
      'is-following': following,
    }"
    :style="{ bottom: fabBottom(routeFab) }"
    :aria-label="locateLabel"
    :title="locateLabel"
    :aria-pressed="following"
    @click="toggleLive()"
  >
    <Icon name="locate" :size="18" />
  </button>

  <BottomSheet v-if="showSheet" :peek="peek" :closed="chosen ? 44 : 0">
    <!-- Folded to a strip: the route in one line, and a grip to open it. On
         the way, the same line counts down instead. -->
    <template v-if="chosen" #closed>
      <template v-if="liveStrip">
        <span class="live-dot" role="img" :aria-label="t('live.dotAria')" />
        <template v-if="liveStrip.arrived">
          <span class="truncate font-semibold text-ink">{{ t('live.arrived') }}</span>
        </template>
        <template v-else>
          <span class="font-semibold text-ink">{{ liveStrip.distance }} · {{ liveStrip.time }}</span>
          <span class="min-w-0 truncate text-muted">
            · {{ liveStrip.ascent }} · {{ liveStrip.at }}
          </span>
        </template>
      </template>
      <template v-else>
        <Icon v-for="m in chosenModes" :key="m" :name="m" :size="15" :style="{ color: `var(--${m})` }" />
        <span class="font-semibold text-ink">{{ fmtDuration(chosen.seconds) }}</span>
        <span class="text-muted">
          {{ fmtDistance(chosen.meters) }} · {{ fmtAscent(chosenAscent) }}
        </span>
      </template>
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
/* The locate button says where it is in one glance: looking, on, or carrying
   the map with it. The reduced-motion rule in theme.css stills the pulse. */
.fab.is-on {
  color: var(--position);
}
.fab.is-following {
  color: var(--position);
  border-color: color-mix(in srgb, var(--position) 40%, var(--line));
  background: color-mix(in srgb, var(--position) 13%, var(--surface));
}
.fab.is-locating {
  color: var(--position);
  animation: fab-breathe 1.2s ease-in-out infinite;
}
@keyframes fab-breathe {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.45;
  }
}

/* The map's dot, said again in one line of text. */
.live-dot {
  flex: none;
  width: 9px;
  height: 9px;
  border-radius: 999px;
  background: var(--position);
}
</style>
