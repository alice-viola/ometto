<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import MapCanvas from './components/MapCanvas.vue';
import Panel from './components/Panel.vue';
import MobileShell from './components/mobile/MobileShell.vue';
import OfflineView from './components/OfflineView.vue';
import Toast from './components/Toast.vue';
import Icon from './components/Icon.vue';
import Disclaimer from './components/Disclaimer.vue';
import { isCompact, panelHidden } from './composables/useMedia';
import { loadFavourites } from './composables/useFavourites';
import { restoreFromUrl } from './composables/usePlanner';
import {
  applyUpdate,
  checkService,
  loadSaved,
  needRefresh,
  online,
  savedViewToken,
  serviceDown,
} from './composables/useOffline';
import { MOCK } from './lib/api';
import { loadConfig } from './lib/config';
import { disclaimerAccepted, disclaimerReady } from './composables/useDisclaimer';
import { t } from './i18n';

/**
 * On a phone the card that says a route is saved sits in a sheet with no
 * navigation of its own, so the list it offers to open is shown from here.
 */
const savedOpen = ref(false);
watch(savedViewToken, () => {
  if (isCompact.value) savedOpen.value = true;
});

/**
 * Escape folds the panel away, but only when it is not busy closing something
 * of its own: a suggestion list, the map popover and the inline forms all mark
 * the key as handled before it reaches here.
 */
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' && e.key !== 'Esc') return;
  if (e.defaultPrevented || isCompact.value) return;
  if (disclaimerReady.value && !disclaimerAccepted.value) return;
  panelHidden.value = !panelHidden.value;
}

onMounted(async () => {
  window.addEventListener('keydown', onKeydown);
  // The disclaimer version lives in the config, so nothing is decided about
  // showing the dialog until the config has answered.
  await loadConfig();
  disclaimerReady.value = true;
  void loadFavourites();
  // What is already on this device, so a card can say so without being asked.
  void loadSaved();
  // A shared link is still a route: it waits behind the dialog like any other.
  if (disclaimerAccepted.value) restoreFromUrl();
  else {
    const stop = watch(disclaimerAccepted, (ok) => {
      if (!ok) return;
      stop();
      restoreFromUrl();
    });
  }
  void checkService();
});

onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown));
</script>

<template>
  <!--
    One map, always the same instance. Rendering a second MapCanvas for the
    phone layout tore the map down and built it again on every rotation, which
    is how a computed route ended up framed over half of Europe.

    The desktop panel is hidden with v-show rather than v-if: folded away it
    keeps everything it was holding — the view it was on, a half-typed
    favourite name — and gives its width back to the map either way.
  -->
  <div class="h-full w-full bg-bg text-ink">
    <div class="flex h-full">
      <aside
        v-if="!isCompact"
        v-show="!panelHidden"
        class="relative z-10 w-[416px] shrink-0 border-r border-line"
        style="box-shadow: var(--shadow-1)"
      >
        <Panel />
      </aside>
      <main class="relative min-w-0 flex-1">
        <MapCanvas />
        <button
          v-if="!isCompact && panelHidden"
          class="absolute left-3 top-3 z-20 flex items-center gap-1.5 rounded-[9px] border border-line px-2.5 py-2 text-[12.5px] text-ink transition-colors hover:border-line-strong"
          :style="{ background: 'var(--surface)', boxShadow: 'var(--shadow-2)' }"
          :aria-label="t('app.showPanel')"
          :title="t('app.showPanelTitle')"
          @click="panelHidden = false"
        >
          <Icon name="panelShow" :size="16" />
          <span>{{ t('app.panel') }}</span>
        </button>
      </main>
    </div>

    <!-- A phone is not a narrow desktop: the map is the screen, the question
         floats over its top and the answer rises from its bottom. -->
    <MobileShell v-if="isCompact" />

    <!--
      Two things can be worth saying at the top, and they are not the same
      thing: the network is gone, or this browser is holding an older copy of
      the app than the one the server now has.
    -->
    <div
      v-if="(!online || serviceDown || needRefresh) && !MOCK"
      class="pointer-events-none fixed inset-x-0 top-0 z-40 flex flex-col items-center gap-1.5 p-2.5"
      style="padding-top: calc(10px + env(safe-area-inset-top))"
      role="status"
    >
      <p
        v-if="!online || serviceDown"
        class="flex items-center gap-1.5 rounded-full border border-line px-3 py-1.5 text-[12px] text-muted"
        :style="{ background: 'var(--surface)', boxShadow: 'var(--shadow-1)' }"
      >
        <Icon v-if="!online" name="cloudOff" :size="13" />
        {{ online ? t('app.offline') : t('offline.pill') }}
      </p>
      <p
        v-if="needRefresh"
        class="pointer-events-auto flex items-center gap-2 rounded-full border border-line py-1 pl-3 pr-1 text-[12px] text-muted"
        :style="{ background: 'var(--surface)', boxShadow: 'var(--shadow-1)' }"
      >
        {{ t('offline.newVersion') }}
        <button class="btn-primary rounded-full px-2.5 py-1 text-[12px]" @click="applyUpdate()">
          {{ t('offline.reload') }}
        </button>
      </p>
    </div>

    <!-- The saved list as a screen of its own, which is what a phone needs
         and a desktop gets from the panel's own navigation instead. -->
    <div
      v-if="savedOpen && isCompact"
      class="fixed inset-0 z-50 flex flex-col bg-surface"
      role="dialog"
      :aria-label="t('panel.title.saved')"
    >
      <header
        class="flex items-center gap-1 border-b border-line px-2 pb-2"
        style="padding-top: calc(env(safe-area-inset-top, 0px) + 8px)"
      >
        <button
          type="button"
          class="grid h-11 w-11 shrink-0 place-items-center rounded-[10px] text-muted"
          :aria-label="t('menu.back')"
          @click="savedOpen = false"
        >
          <Icon name="arrowLeft" :size="19" />
        </button>
        <h1 class="min-w-0 flex-1 truncate text-[17px] font-medium">{{ t('panel.title.saved') }}</h1>
      </header>
      <div class="scroll-quiet min-h-0 flex-1 overflow-y-auto overscroll-contain px-4 pt-3 pb-8">
        <OfflineView @done="savedOpen = false" />
      </div>
    </div>

    <Disclaimer v-if="disclaimerReady && !disclaimerAccepted" />

    <Toast />
  </div>
</template>
