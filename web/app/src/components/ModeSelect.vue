<script setup lang="ts">
import Icon from './Icon.vue';
import { MODE_LABELS } from '../lib/format';
import type { Mode } from '../lib/types';
import { mode } from '../composables/usePlanner';

const ROW1: Mode[] = ['car', 'bike', 'hike'];
const ROW2: Mode[] = ['car+hike', 'bike+hike'];
const ALL = [...ROW1, ...ROW2];

function icons(m: Mode): string[] {
  return m.split('+');
}

function onKey(e: KeyboardEvent, m: Mode) {
  const keys = ['ArrowRight', 'ArrowDown', 'ArrowLeft', 'ArrowUp'];
  if (!keys.includes(e.key)) return;
  e.preventDefault();
  const delta = e.key === 'ArrowRight' || e.key === 'ArrowDown' ? 1 : -1;
  const i = ALL.indexOf(m);
  mode.value = ALL[(i + delta + ALL.length) % ALL.length];
  const next = document.getElementById(`mode-${ALL[(i + delta + ALL.length) % ALL.length]}`);
  next?.focus();
}
</script>

<template>
  <div role="radiogroup" aria-label="Travel mode" class="grid gap-1.5">
    <div class="grid grid-cols-3 gap-1.5">
      <button
        v-for="m in ROW1"
        :id="`mode-${m}`"
        :key="m"
        role="radio"
        :aria-label="MODE_LABELS[m]"
        :aria-checked="mode === m"
        :tabindex="mode === m ? 0 : -1"
        class="mode-chip"
        :class="mode === m ? 'is-on' : ''"
        @click="mode = m"
        @keydown="onKey($event, m)"
      >
        <Icon :name="m" :size="19" />
        <span>{{ MODE_LABELS[m] }}</span>
      </button>
    </div>
    <div class="grid grid-cols-2 gap-1.5">
      <button
        v-for="m in ROW2"
        :id="`mode-${m}`"
        :key="m"
        role="radio"
        :aria-label="MODE_LABELS[m]"
        :aria-checked="mode === m"
        :tabindex="mode === m ? 0 : -1"
        class="mode-chip"
        :class="mode === m ? 'is-on' : ''"
        @click="mode = m"
        @keydown="onKey($event, m)"
      >
        <span class="flex items-center gap-1">
          <Icon v-for="(ic, i) in icons(m)" :key="i" :name="ic" :size="18" />
        </span>
        <span>{{ MODE_LABELS[m] }}</span>
      </button>
    </div>
  </div>
</template>

<style scoped>
.mode-chip {
  display: flex;
  flex-direction: column;
  align-items: center;
  justify-content: center;
  gap: 4px;
  padding: 9px 4px 8px;
  border: 1px solid var(--line);
  border-radius: var(--radius-control);
  background: var(--surface-2);
  color: var(--muted);
  font-size: 11.5px;
  font-weight: 500;
  line-height: 1.1;
  white-space: nowrap;
  transition:
    background 0.14s ease,
    color 0.14s ease,
    border-color 0.14s ease;
}
.mode-chip:hover:not(.is-on) {
  color: var(--ink);
  border-color: var(--line-strong);
}
.mode-chip.is-on {
  background: var(--ink);
  border-color: var(--ink);
  color: var(--ink-invert);
}
</style>
