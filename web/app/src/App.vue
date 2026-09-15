<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import MapCanvas from './components/MapCanvas.vue';
import Panel from './components/Panel.vue';
import MobileShell from './components/mobile/MobileShell.vue';
import Toast from './components/Toast.vue';
import Icon from './components/Icon.vue';
import Disclaimer from './components/Disclaimer.vue';
import { isCompact, panelHidden } from './composables/useMedia';
import { loadFavourites } from './composables/useFavourites';
import { restoreFromUrl } from './composables/usePlanner';
import { api, MOCK } from './lib/api';
import { loadConfig } from './lib/config';
import { disclaimerAccepted, disclaimerReady } from './composables/useDisclaimer';

const offline = ref(false);

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
  // A shared link is still a route: it waits behind the dialog like any other.
  if (disclaimerAccepted.value) restoreFromUrl();
  else {
    const stop = watch(disclaimerAccepted, (ok) => {
      if (!ok) return;
      stop();
      restoreFromUrl();
    });
  }
  api
    .health()
    .then((h) => (offline.value = h?.ok === false))
    .catch(() => (offline.value = true));
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
          aria-label="Show the panel"
          title="Show the panel (Esc)"
          @click="panelHidden = false"
        >
          <Icon name="panelShow" :size="16" />
          <span>Panel</span>
        </button>
      </main>
    </div>

    <!-- A phone is not a narrow desktop: the map is the screen, the question
         floats over its top and the answer rises from its bottom. -->
    <MobileShell v-if="isCompact" />

    <div
      v-if="offline && !MOCK"
      class="pointer-events-none fixed inset-x-0 top-0 z-40 flex justify-center p-2.5"
      style="padding-top: calc(10px + env(safe-area-inset-top))"
      role="status"
    >
      <p
        class="rounded-full border border-line px-3 py-1.5 text-[12px] text-muted"
        :style="{ background: 'var(--surface)', boxShadow: 'var(--shadow-1)' }"
      >
        The routing service is not reachable yet.
      </p>
    </div>

    <Disclaimer v-if="disclaimerReady && !disclaimerAccepted" />

    <Toast />
  </div>
</template>
