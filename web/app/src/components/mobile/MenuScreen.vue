<script setup lang="ts">
import { ref } from 'vue';
import Icon from '../Icon.vue';
import HistoryView from '../HistoryView.vue';
import FavouritesView from '../FavouritesView.vue';
import SettingsView from '../SettingsView.vue';
import AboutView from '../AboutView.vue';
import { MAX_POINTS, addStop, clearAll, placeIndices } from '../../composables/usePlanner';
import { isDark, useTheme } from '../../composables/useTheme';
import { appConfig } from '../../lib/config';

/**
 * Everything that is not the question or the answer, one screen at a time:
 * the list, then the section it opens. The sections are the same components
 * the desktop panel shows; only the frame around them is a phone's.
 */
export type MenuView = 'menu' | 'history' | 'favourites' | 'settings' | 'about';

const props = defineProps<{ view: MenuView }>();
const emit = defineEmits<{ close: [] }>();

const current = ref<MenuView>(props.view);
const { toggle } = useTheme();

const TITLES: Record<MenuView, string> = {
  menu: 'Ometto',
  history: 'Recent routes',
  favourites: 'Favourites',
  settings: 'Settings',
  about: 'About',
};

function back() {
  if (current.value === 'menu') emit('close');
  else current.value = 'menu';
}

function addAStop() {
  if (placeIndices.value.length < MAX_POINTS) addStop();
  emit('close');
}

function startOver() {
  clearAll();
  emit('close');
}

const dataDate = String(appConfig().dataDates.osm ?? appConfig().dataDates.osmExtract ?? '').slice(0, 10);
</script>

<template>
  <div class="fixed inset-0 z-50 flex flex-col bg-surface" role="dialog" :aria-label="TITLES[current]">
    <header
      class="flex items-center gap-1 border-b border-line px-2 pb-2"
      :style="{ paddingTop: 'calc(env(safe-area-inset-top, 0px) + 8px)' }"
    >
      <button type="button" class="ctl" :aria-label="current === 'menu' ? 'Back to the map' : 'Back'" @click="back">
        <Icon name="arrowLeft" :size="19" />
      </button>
      <h1 class="min-w-0 flex-1 truncate text-[17px] font-medium">{{ TITLES[current] }}</h1>
      <button
        v-if="current === 'menu'"
        type="button"
        class="ctl"
        :aria-label="isDark ? 'Switch to the light theme' : 'Switch to the dark theme'"
        @click="toggle"
      >
        <Icon :name="isDark ? 'sun' : 'moon'" :size="18" />
      </button>
    </header>

    <div class="scroll-quiet min-h-0 flex-1 overflow-y-auto overscroll-contain" :class="current === 'menu' ? '' : 'px-4 pt-3 pb-8'">
      <ul v-if="current === 'menu'" class="pb-8">
        <li><button type="button" class="row" @click="current = 'history'"><Icon name="history" :size="18" /> Recent routes</button></li>
        <li><button type="button" class="row" @click="current = 'favourites'"><Icon name="star" :size="18" /> Favourites</button></li>
        <li><button type="button" class="row" @click="current = 'settings'"><Icon name="settings" :size="18" /> Settings</button></li>
        <li><button type="button" class="row" @click="current = 'about'"><Icon name="info" :size="18" /> About</button></li>
        <li class="mt-2 border-t border-line pt-2">
          <button type="button" class="row" :disabled="placeIndices.length >= MAX_POINTS" @click="addAStop">
            <Icon name="plus" :size="18" /> Add a stop
          </button>
        </li>
        <li><button type="button" class="row" @click="startOver"><Icon name="trash" :size="18" /> Start over</button></li>
        <li class="px-4 pt-4 text-[12px] text-faint">
          Planning aid, not a guide<template v-if="dataDate"> · data {{ dataDate }}</template>
        </li>
      </ul>
      <HistoryView v-else-if="current === 'history'" @done="emit('close')" />
      <FavouritesView v-else-if="current === 'favourites'" @done="emit('close')" />
      <SettingsView v-else-if="current === 'settings'" @about="current = 'about'" />
      <AboutView v-else />
    </div>
  </div>
</template>

<style scoped>
.ctl {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-control);
  color: var(--muted);
}
.ctl:active {
  background: var(--surface-3);
  color: var(--ink);
}
.row {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 14px;
  min-height: 52px;
  padding: 0 18px;
  font-size: 15px;
  text-align: left;
  color: var(--ink);
}
.row:active {
  background: var(--surface-2);
}
.row:disabled {
  opacity: 0.4;
}
</style>
