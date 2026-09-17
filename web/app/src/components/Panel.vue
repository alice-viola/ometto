<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import Icon from './Icon.vue';
import BrandMark from './BrandMark.vue';
import SearchBlock from './SearchBlock.vue';
import ModeBar from './ModeBar.vue';
import TripOptions from './TripOptions.vue';
import ResultsBlock from './ResultsBlock.vue';
import HistoryView from './HistoryView.vue';
import FavouritesView from './FavouritesView.vue';
import OfflineView from './OfflineView.vue';
import SettingsView from './SettingsView.vue';
import AboutView from './AboutView.vue';
import Toggle from './Toggle.vue';
import {
  alternatives,
  answeredToken,
  busy,
  canCompute,
  compute,
  status,
} from '../composables/usePlanner';
import { isDark, useTheme } from '../composables/useTheme';
import { appConfig } from '../lib/config';
import { hasHover, isCompact, panelHidden } from '../composables/useMedia';
import { savedViewToken } from '../composables/useOffline';
import { t } from '../i18n';

type View = 'plan' | 'history' | 'favourites' | 'saved' | 'settings' | 'about';
const view = ref<View>('plan');
const { toggle } = useTheme();

/** The grade and the lifts, opened from the mode bar's badge; leaving the plan closes them. */
const tripOptions = ref(false);
watch(view, () => (tripOptions.value = false));

const three = computed({
  get: () => alternatives.value === 3,
  set: (v: boolean) => (alternatives.value = v ? 3 : 1),
});

const title = computed(() => t(`panel.title.${view.value}`));

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

// A card that says a route is already saved offers to show the list; on a
// desktop that list is a section of this panel.
watch(savedViewToken, () => (view.value = 'saved'));

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
        :aria-label="t('panel.back')"
        @click="view = 'plan'"
      >
        <Icon name="arrowLeft" :size="17" />
      </button>
      <span v-else class="mt-[3px] text-ink">
        <BrandMark :size="22" label="Ometto" />
      </span>

      <div class="min-w-0 flex-1">
        <h1 class="truncate text-[15px] font-medium leading-tight tracking-[-0.005em]">
          {{ title }}
        </h1>
        <p v-if="view === 'plan'" class="truncate text-[11.5px] leading-tight text-faint">
          {{ t('meta.region') }}
        </p>
      </div>

      <nav class="flex items-center gap-0.5" :aria-label="t('panel.sections')">
        <button
          class="nav-btn"
          :class="view === 'history' ? 'is-on' : ''"
          :aria-label="t('panel.title.history')"
          :title="t('panel.title.history')"
          @click="go('history')"
        >
          <Icon name="history" :size="16" />
        </button>
        <button
          class="nav-btn"
          :class="view === 'favourites' ? 'is-on' : ''"
          :aria-label="t('panel.title.favourites')"
          :title="t('panel.title.favourites')"
          @click="go('favourites')"
        >
          <Icon name="star" :size="16" />
        </button>
        <button
          class="nav-btn"
          :class="view === 'saved' ? 'is-on' : ''"
          :aria-label="t('panel.title.saved')"
          :title="t('panel.title.saved')"
          @click="go('saved')"
        >
          <Icon name="download" :size="16" />
        </button>
        <button
          class="nav-btn"
          :class="view === 'settings' ? 'is-on' : ''"
          :aria-label="t('panel.title.settings')"
          :title="t('panel.title.settings')"
          @click="go('settings')"
        >
          <Icon name="settings" :size="16" />
        </button>
        <button
          class="nav-btn"
          :aria-label="isDark ? t('panel.switchToLight') : t('panel.switchToDark')"
          :title="isDark ? t('panel.lightTheme') : t('panel.darkTheme')"
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
            :aria-label="t('panel.hide')"
            :title="t('panel.hideTitle')"
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

        <!-- The phone's row: the mode as five icons, and the grade as a badge
             whose grades and lifts open in a popover under it. -->
        <div class="relative mt-4">
          <ModeBar :options-open="tripOptions" @options="tripOptions = true" />
          <Transition name="pop">
            <TripOptions
              v-if="tripOptions"
              variant="popover"
              class="absolute inset-x-0 top-[52px] z-30"
              @close="tripOptions = false"
            />
          </Transition>
        </div>

        <div class="mt-1 border-t border-line">
          <Toggle v-model="three" :label="t('panel.showThree')" :hint="t('panel.showThreeHint')" />
        </div>

        <button
          class="btn-primary compute mt-3 flex h-9 w-full items-center justify-center gap-2 px-4 text-[13px]"
          :aria-label="status === 'loading' ? t('panel.computingAria') : t('panel.compute')"
          :disabled="!canCompute || busy"
          @click="compute()"
        >
          <span v-if="status === 'loading'">{{ t('panel.computing') }}</span>
          <span v-else>{{ t('panel.compute') }}</span>
        </button>
        <p v-if="!canCompute" class="mt-1.5 text-center text-[11.5px] text-faint">
          {{ hasHover ? t('panel.needPointsClick') : t('panel.needPointsTap') }}
        </p>

        <div ref="resultsEl" class="mt-5 scroll-mt-2">
          <ResultsBlock />
        </div>
      </template>

      <HistoryView v-else-if="view === 'history'" @done="view = 'plan'" />
      <FavouritesView v-else-if="view === 'favourites'" @done="view = 'plan'" />
      <OfflineView v-else-if="view === 'saved'" @done="view = 'plan'" />
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
        :aria-label="t('panel.aboutAria')"
        :title="t('panel.aboutTitle')"
        @click="view = 'about'"
      >
        {{ dataDate ? t('panel.footerData', { date: dataDate }) : t('panel.footer') }}
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
/* Nothing to ask yet: a quiet outline rather than a grey slab of ink. */
.compute:disabled {
  border: 1px solid var(--line);
  background: var(--surface-2);
  color: var(--faint);
  opacity: 1;
}
.pop-enter-active,
.pop-leave-active {
  transition: opacity 0.14s ease, transform 0.14s ease;
}
.pop-enter-from,
.pop-leave-to {
  opacity: 0;
  transform: translateY(-4px);
}
/* A thumb, not a cursor: the icon stays 16 px, the target grows to 44. */
@media (max-width: 899px) {
  .nav-btn {
    width: 44px;
    height: 44px;
  }
}
</style>
