<script setup lang="ts">
import Icon from './Icon.vue';
import Toggle from './Toggle.vue';
import { available, layers } from '../composables/useLayers';
import { theme, type ThemeChoice } from '../composables/useTheme';

const emit = defineEmits<{ about: [] }>();

const THEMES: { id: ThemeChoice; label: string; icon: string }[] = [
  { id: 'system', label: 'System', icon: 'monitor' },
  { id: 'light', label: 'Light', icon: 'sun' },
  { id: 'dark', label: 'Dark', icon: 'moon' },
];
</script>

<template>
  <section class="grid gap-5">
    <div>
      <h3 class="label mb-2">Theme</h3>
      <div role="radiogroup" aria-label="Theme" class="grid grid-cols-3 gap-1.5">
        <button
          v-for="t in THEMES"
          :key="t.id"
          role="radio"
          :aria-label="t.label"
          :aria-checked="theme === t.id"
          class="theme-chip"
          :class="theme === t.id ? 'is-on' : ''"
          @click="theme = t.id"
        >
          <Icon :name="t.icon" :size="16" />
          <span>{{ t.label }}</span>
        </button>
      </div>
    </div>

    <div>
      <h3 class="label mb-0.5">Map</h3>
      <div class="divide-y divide-line">
        <Toggle v-model="layers.terrain" label="3D terrain" hint="Tilts the view and lifts the relief, once you are close enough to see it." />
        <Toggle v-model="layers.contours" label="Contour lines" hint="Labelled every 100 m, from zoom 10." />
        <Toggle
          v-model="layers.sat"
          label="Marked trails"
          :hint="available.sat ? 'SAT paths, coloured by grade.' : 'Not published yet.'"
          :disabled="!available.sat"
        />
        <Toggle
          v-model="layers.pois"
          label="Peaks, huts and passes"
          :hint="available.pois ? 'With names and heights.' : 'Not published yet.'"
          :disabled="!available.pois"
        />
        <Toggle
          v-model="layers.crags"
          label="Climbing crags"
          :hint="
            available.crags
              ? 'Grade span and aspect where OpenStreetMap has them. Dense around Arco, thin elsewhere.'
              : 'Not published yet.'
          "
          :disabled="!available.crags"
        />
        <Toggle
          v-model="layers.lifts"
          label="Lifts"
          :hint="available.lifts ? 'Cable cars, gondolas and chair lifts.' : 'Not published yet.'"
          :disabled="!available.lifts"
        />
      </div>
    </div>

    <div>
      <h3 class="label mb-1.5">Units</h3>
      <p class="text-[13px] text-muted">Metric: kilometres, metres, hours and minutes.</p>
    </div>

    <div class="border-t border-line pt-3">
      <h3 class="label mb-1.5">About</h3>
      <p class="text-[12px] leading-relaxed text-muted">
        Map, paths and lifts from OpenStreetMap. Marked trails and place names from SAT and the
        Province of Trento. Elevation from the public Terrarium tiles. Walking times follow the Alpine clubs'
        rule: distance and climb counted together, the smaller of the two halved.
      </p>
    </div>
  </section>
</template>

<style scoped>
.note-action {
  color: var(--ink);
  font-size: 12px;
  text-decoration: underline;
  text-decoration-color: var(--line-strong);
  text-underline-offset: 2px;
}
.note-action:hover {
  text-decoration-color: var(--ink);
}
.theme-chip {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  padding: 9px 4px 8px;
  border: 1px solid var(--line);
  border-radius: var(--radius-control);
  background: var(--surface-2);
  color: var(--muted);
  font-size: 12px;
  transition: background 0.14s ease, color 0.14s ease, border-color 0.14s ease;
}
.theme-chip:hover:not(.is-on) {
  color: var(--ink);
  border-color: var(--line-strong);
}
.theme-chip.is-on {
  background: var(--ink);
  border-color: var(--ink);
  color: var(--ink-invert);
}
</style>
