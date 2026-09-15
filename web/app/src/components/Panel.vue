<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import Icon from './Icon.vue';
import BrandMark from './BrandMark.vue';
import SearchBlock from './SearchBlock.vue';
import ModeSelect from './ModeSelect.vue';
import GradeSelect from './GradeSelect.vue';
import ResultsBlock from './ResultsBlock.vue';
import HistoryView from './HistoryView.vue';
import FavouritesView from './FavouritesView.vue';
import SettingsView from './SettingsView.vue';
import AboutView from './AboutView.vue';
import Toggle from './Toggle.vue';
import {
  alternatives,
  answeredToken,
  busy,
  canCompute,
  compute,
  lifts,
  mode,
  status,
} from '../composables/usePlanner';
import { isDark, useTheme } from '../composables/useTheme';
import { appConfig } from '../lib/config';
import { hasHover, isCompact, panelHidden } from '../composables/useMedia';

type View = 'plan' | 'history' | 'favourites' | 'settings' | 'about';
const view = ref<View>('plan');
const { toggle } = useTheme();

const walks = computed(() => mode.value.includes('hike'));
const three = computed({
  get: () => alternatives.value === 3,
  set: (v: boolean) => (alternatives.value = v ? 3 : 1),
});

const TITLES: Record<View, string> = {
  plan: 'Ometto',
  history: 'Recent routes',
  favourites: 'Favourites',
  settings: 'Settings',
  about: 'About',
};

/** One date for the footer: how old the map data is, which is the age of the
    OpenStreetMap extract — not the build, which says nothing about the roads
    and paths. The other dates keep their labels in About. The service sends
    timestamps; nobody needs the seconds. */
const dataDate = computed(() => {
  const d = appConfig().dataDates;
  const osm = d.osm ?? d.osmExtract ?? '';
  return String(osm).slice(0, 10);
});

function go(v: View) {
  view.value = view.value === v ? 'plan' : v;
}

/**
 * On a phone the answer lands below the question, outside the sheet's window.
 * Bring it to the top of the scroll: the peek then shows the headline, half
 * height shows the card, and the fields are one swipe up. Desktop has the
 * height to show both and is left alone.
 */
const resultsEl = ref<HTMLElement | null>(null);
watch(answeredToken, async () => {
  if (!isCompact.value) return;
  await nextTick();
  // Instant, not smooth: the sheet is resizing under it at the same moment,
  // and a viewport still settling after a load can cancel an animated scroll.
  resultsEl.value?.scrollIntoView({ block: 'start' });
});
</script>

<template>
  <div class="flex h-full min-h-0 flex-col bg-surface">
    <header
      class="flex items-start gap-2 border-b border-line px-4"
      :class="isCompact ? 'pt-1.5 pb-1.5' : 'pt-3.5 pb-3'"
    >
      <button
        v-if="view !== 'plan'"
        class="btn-quiet -ml-1.5 mt-0.5 p-1.5"
        aria-label="Back to the planner"
        @click="view = 'plan'"
      >
        <Icon name="arrowLeft" :size="17" />
      </button>
      <span v-else class="mt-[3px] text-ink">
        <BrandMark :size="22" label="Ometto" />
      </span>

      <div class="min-w-0 flex-1">
        <h1 class="truncate text-[15px] font-medium leading-tight tracking-[-0.005em]">
          {{ TITLES[view] }}
        </h1>
        <p v-if="view === 'plan'" class="truncate text-[11.5px] leading-tight text-faint">
          Trentino-Alto Adige
        </p>
      </div>

      <nav class="flex items-center gap-0.5" aria-label="Sections">
        <button
          class="nav-btn"
          :class="view === 'history' ? 'is-on' : ''"
          aria-label="Recent routes"
          title="Recent routes"
          @click="go('history')"
        >
          <Icon name="history" :size="16" />
        </button>
        <button
          class="nav-btn"
          :class="view === 'favourites' ? 'is-on' : ''"
          aria-label="Favourites"
          title="Favourites"
          @click="go('favourites')"
        >
          <Icon name="star" :size="16" />
        </button>
        <button
          class="nav-btn"
          :class="view === 'settings' ? 'is-on' : ''"
          aria-label="Settings"
          title="Settings"
          @click="go('settings')"
        >
          <Icon name="settings" :size="16" />
        </button>
        <button
          class="nav-btn"
          :aria-label="isDark ? 'Switch to the light theme' : 'Switch to the dark theme'"
          :title="isDark ? 'Light theme' : 'Dark theme'"
          @click="toggle"
        >
          <Icon :name="isDark ? 'sun' : 'moon'" :size="16" />
        </button>
        <!-- Folding the panel away is a desktop affordance: on a phone the
             sheet already collapses, and this control would do nothing. -->
        <template v-if="!isCompact">
          <span class="mx-0.5 h-4 w-px" :style="{ background: 'var(--line)' }" aria-hidden="true" />
          <button
            class="nav-btn"
            aria-label="Hide the panel"
            title="Hide the panel (Esc)"
            @click="panelHidden = true"
          >
            <Icon name="panelHide" :size="16" />
          </button>
        </template>
      </nav>
    </header>

    <!-- `relative` so that anything positioned inside the body — the sr-only
         labels in the leg list — belongs to this scroller and not to the fixed
         sheet above it, whose own box scrollIntoView would otherwise scroll,
         hiding the header behind the handle. -->
    <div
      class="scroll-quiet relative min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pb-6"
      :class="isCompact ? 'pt-3' : 'pt-3.5'"
    >
      <template v-if="view === 'plan'">
        <SearchBlock />

        <h2 class="label mt-5 mb-2">Mode</h2>
        <ModeSelect />

        <template v-if="walks">
          <h2 class="label mt-4 mb-2">Trail grade</h2>
          <GradeSelect />
        </template>

        <div class="mt-2 border-t border-line">
          <Toggle
            v-if="walks"
            v-model="lifts"
            label="Use lifts"
            hint="Cable cars and chair lifts, where they run."
          />
          <Toggle v-model="three" label="Show 3 routes" hint="Compare alternatives side by side." />
        </div>

        <button
          class="btn-primary mt-3 flex w-full items-center justify-center gap-2 px-4 py-3 text-[14px]"
          :aria-label="status === 'loading' ? 'Computing the route' : 'Compute route'"
          :disabled="!canCompute || busy"
          @click="compute()"
        >
          <span v-if="status === 'loading'">Computing…</span>
          <span v-else>Compute route</span>
        </button>
        <p v-if="!canCompute" class="mt-1.5 text-center text-[11.5px] text-faint">
          Set a start and a destination — or {{ hasHover ? 'click' : 'tap' }} the map.
        </p>

        <div ref="resultsEl" class="mt-5 scroll-mt-2">
          <ResultsBlock />
        </div>
      </template>

      <HistoryView v-else-if="view === 'history'" @done="view = 'plan'" />
      <FavouritesView v-else-if="view === 'favourites'" @done="view = 'plan'" />
      <AboutView v-else-if="view === 'about'" />
      <SettingsView v-else @about="view = 'about'" />
    </div>

    <!-- Always in view, wherever the panel has got to. The bottom padding
         clears a phone's home indicator; on anything else the inset is zero. -->
    <footer
      class="shrink-0 border-t border-line px-4 pt-2"
      style="padding-bottom: max(8px, env(safe-area-inset-bottom))"
    >
      <button
        class="w-full truncate text-left text-[11px] text-faint transition-colors hover:text-muted"
        :aria-label="'About Ometto, its data and its limits'"
        title="What this is, where the data comes from, and what it cannot tell you"
        @click="view = 'about'"
      >
        Planning aid, not a guide<template v-if="dataDate"> · data {{ dataDate }}</template>
      </button>
    </footer>
  </div>
</template>

<style scoped>
.nav-btn {
  display: grid;
  place-items: center;
  width: 30px;
  height: 30px;
  border-radius: 8px;
  color: var(--muted);
  transition: background 0.14s ease, color 0.14s ease;
}
.nav-btn:hover {
  background: var(--surface-3);
  color: var(--ink);
}
.nav-btn.is-on {
  background: var(--surface-3);
  color: var(--ink);
}
/* A thumb, not a cursor: the icon stays 16 px, the target grows to 44. */
@media (max-width: 899px) {
  .nav-btn {
    width: 44px;
    height: 44px;
  }
}
</style>
