<script setup lang="ts">
import { computed } from 'vue';
import Icon from './Icon.vue';
import { gradeName, modeLabel } from '../lib/format';
import type { Mode } from '../lib/types';
import { grade, lifts, mode } from '../composables/usePlanner';
import { t } from '../i18n';

/**
 * How the trip travels, the same on a phone and on a desktop: the mode as five
 * icons in one well, the words for what they chose on the line under them,
 * and, when the trip walks, the grade as a badge that opens the grade and the
 * lifts. Where that choice opens is the caller's: a sheet from the bottom on a
 * phone, a popover under the badge on a desktop.
 */
defineProps<{ optionsOpen?: boolean }>();
const emit = defineEmits<{ options: [] }>();

const MODES: Mode[] = ['car', 'bike', 'hike', 'car+hike', 'bike+hike'];
const walks = computed(() => mode.value.includes('hike'));

/**
 * The line under the icons says what the icons chose, in words — and, when
 * the trip walks, at what grade and whether a lift may be taken.
 */
const caption = computed(() => {
  const parts = [modeLabel(mode.value)];
  if (walks.value) {
    parts.push(`${gradeName(grade.value)} (${grade.value})`);
    parts.push(lifts.value ? t('top.liftsOn') : t('top.liftsOff'));
  }
  return parts.join(' · ');
});

/** Arrow keys move the choice along the well, as in any radio group. */
const STEP: Record<string, number> = { ArrowRight: 1, ArrowDown: 1, ArrowLeft: -1, ArrowUp: -1 };
function onKey(e: KeyboardEvent, m: Mode) {
  const step = STEP[e.key];
  if (!step) return;
  e.preventDefault();
  const next = MODES[(MODES.indexOf(m) + step + MODES.length) % MODES.length];
  mode.value = next;
  const well = (e.currentTarget as HTMLElement).parentElement;
  well?.querySelector<HTMLElement>(`[data-mode="${next}"]`)?.focus();
}
</script>

<template>
  <div>
    <div class="flex items-center gap-1.5">
      <div class="seg" role="radiogroup" :aria-label="t('top.travelMode')">
        <button
          v-for="m in MODES"
          :key="m"
          type="button"
          role="radio"
          class="seg-item"
          :class="mode === m ? 'is-on' : ''"
          :data-mode="m"
          :aria-checked="mode === m"
          :aria-label="modeLabel(m)"
          :title="modeLabel(m)"
          :tabindex="mode === m ? 0 : -1"
          @click="mode = m"
          @keydown="onKey($event, m)"
        >
          <Icon v-for="(ic, i) in m.split('+')" :key="i" :name="ic" :size="17" />
        </button>
      </div>
      <button
        v-if="walks"
        type="button"
        class="grade-badge"
        :class="optionsOpen ? 'is-open' : ''"
        :style="{ '--g': `var(--grade-${grade.toLowerCase()})` }"
        :aria-label="t('top.optionsAria', { mode: modeLabel(mode), grade })"
        :aria-expanded="optionsOpen"
        :title="t('top.tripOptions')"
        @click="emit('options')"
      >
        <span class="flex items-center gap-0.5">
          <Icon v-if="grade === 'A'" name="warning" :size="11" :width="2" />
          <span class="font-semibold">{{ grade }}</span>
        </span>
        <span class="bar-g" aria-hidden="true" />
      </button>
    </div>
    <p class="truncate px-1 pt-1.5 pb-1 text-[11.5px] leading-snug text-muted">{{ caption }}</p>
  </div>
</template>

<style scoped>
/* Five icons in one well; the chosen one is the inked one. */
.seg {
  display: flex;
  min-width: 0;
  flex: 1;
  gap: 3px;
  padding: 3px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface-2);
}
.seg-item {
  display: flex;
  flex: 1 1 0;
  align-items: center;
  justify-content: center;
  gap: 2px;
  min-width: 0;
  height: 38px;
  border-radius: 9px;
  color: var(--muted);
  transition: background 0.14s ease, color 0.14s ease;
}
.seg-item.is-on {
  background: var(--ink);
  color: var(--ink-invert);
}
/* The grade, as the strip drew it, now one chip that opens the choice. */
.grade-badge {
  display: flex;
  flex: none;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  width: 46px;
  height: 46px;
  border: 1px solid var(--line);
  border-radius: 12px;
  background: var(--surface);
  color: var(--ink);
  font-size: 12.5px;
  transition: border-color 0.14s ease, background 0.14s ease;
}
.grade-badge:active {
  background: var(--surface-3);
}
.grade-badge.is-open {
  border-color: color-mix(in srgb, var(--accent) 30%, var(--surface));
  background: color-mix(in srgb, var(--accent) 11%, var(--surface));
}
.bar-g {
  width: 20px;
  height: 3px;
  border-radius: 2px;
  background: var(--g);
}
/* A mouse gets a hover; a thumb keeps the press states above. */
@media (hover: hover) {
  .seg-item:hover:not(.is-on) {
    background: var(--surface-3);
    color: var(--ink);
  }
  .grade-badge:hover:not(.is-open) {
    border-color: var(--line-strong);
  }
}
</style>
