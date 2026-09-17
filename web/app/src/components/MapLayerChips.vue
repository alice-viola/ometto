<script setup lang="ts">
import Icon from './Icon.vue';
import { available, layers, type LayerState } from '../composables/useLayers';
import { t, type Key } from '../i18n';

/**
 * The map's own switches, on the map: one press for the relief in 3D and for
 * each thing drawn over the basemap. Settings keeps the same switches, with
 * the contour lines beside them; these are the ones worth having without
 * opening anything. While a layer is on, its icon takes the colour the layer
 * is drawn in, so the row doubles as a key.
 */
interface Chip {
  id: keyof LayerState;
  icon: string;
  label: Key;
  name: Key;
  hint: Key;
  colour: string;
}
const CHIPS: Chip[] = [
  { id: 'terrain', icon: 'cube', label: 'map.chip.terrain', name: 'settings.terrain', hint: 'settings.terrainHint', colour: 'var(--ink)' },
  { id: 'pois', icon: 'peak', label: 'map.chip.pois', name: 'settings.pois', hint: 'settings.poisHint', colour: 'var(--ink)' },
  { id: 'sat', icon: 'trail', label: 'map.chip.sat', name: 'settings.sat', hint: 'settings.satHint', colour: 'var(--grade-e)' },
  { id: 'crags', icon: 'crag', label: 'map.chip.crags', name: 'settings.crags', hint: 'settings.cragsHint', colour: 'var(--ink)' },
  { id: 'lifts', icon: 'lift', label: 'map.chip.lifts', name: 'settings.lifts', hint: 'settings.liftsHint', colour: 'var(--lift)' },
];

/** The relief comes from tiles the page always has; the overlays wait for the service to publish them. */
const published = (id: keyof LayerState) => (id in available ? available[id as keyof typeof available] : true);
</script>

<template>
  <div role="group" :aria-label="t('map.layers')" class="pointer-events-none flex flex-wrap items-center justify-center gap-1.5">
    <button
      v-for="c in CHIPS"
      :key="c.id"
      type="button"
      class="chip"
      :class="layers[c.id] ? 'is-on' : ''"
      :style="{ '--c': c.colour }"
      :aria-pressed="layers[c.id]"
      :aria-label="t(c.name)"
      :title="`${t(c.name)} — ${t(c.hint)}`"
      :disabled="!published(c.id)"
      @click="layers[c.id] = !layers[c.id]"
    >
      <Icon :name="c.icon" :size="15" class="ic" />
      <span class="chip-label">{{ t(c.label) }}</span>
    </button>
  </div>
</template>

<style scoped>
.chip {
  pointer-events: auto;
  display: flex;
  align-items: center;
  gap: 6px;
  height: 32px;
  padding: 0 12px 0 10px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--surface);
  box-shadow: var(--shadow-2);
  color: var(--muted);
  font-size: 12.5px;
  white-space: nowrap;
  transition: color 0.14s ease, border-color 0.14s ease, background 0.14s ease;
}
.chip .ic {
  color: var(--faint);
  transition: color 0.14s ease;
}
.chip:hover:not(:disabled):not(.is-on) {
  border-color: var(--line-strong);
  color: var(--ink);
}
/* On is a tint, not a ring: the words in ink, the icon in its layer's colour. */
.chip.is-on {
  border-color: color-mix(in srgb, var(--accent) 30%, var(--surface));
  background: color-mix(in srgb, var(--accent) 11%, var(--surface));
  color: var(--ink);
}
.chip.is-on .ic {
  color: var(--c);
}
.chip:disabled {
  opacity: 0.45;
  cursor: not-allowed;
}
/* A narrow map keeps the icons; the words stay in the tooltips. */
@media (max-width: 979px) {
  .chip {
    padding: 0 9px;
  }
  .chip-label {
    display: none;
  }
}
</style>
