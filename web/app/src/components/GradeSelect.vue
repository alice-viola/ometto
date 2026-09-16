<script setup lang="ts">
import { computed, ref } from 'vue';
import Icon from './Icon.vue';
import { GRADES, gradeDesc, gradeHint, gradeName } from '../lib/format';
import { t } from '../i18n';
import type { Grade } from '../lib/types';
import { grade } from '../composables/usePlanner';

const hovered = ref<Grade | null>(null);
const shown = computed(() => hovered.value ?? grade.value);

function onKey(e: KeyboardEvent, g: Grade) {
  if (!['ArrowRight', 'ArrowLeft'].includes(e.key)) return;
  e.preventDefault();
  const i = GRADES.indexOf(g);
  const next = GRADES[(i + (e.key === 'ArrowRight' ? 1 : -1) + GRADES.length) % GRADES.length];
  grade.value = next;
  document.getElementById(`grade-${next}`)?.focus();
}
</script>

<template>
  <div>
    <div role="radiogroup" :aria-label="t('top.gradeGroup')" class="grid grid-cols-5 gap-1.5">
      <button
        v-for="g in GRADES"
        :id="`grade-${g}`"
        :key="g"
        role="radio"
        :aria-label="`${g} — ${gradeName(g)}`"
        :aria-checked="grade === g"
        :tabindex="grade === g ? 0 : -1"
        :title="gradeHint(g)"
        class="grade-chip"
        :class="[grade === g ? 'is-on' : '', g === 'A' ? 'is-alpine' : '']"
        @click="grade = g"
        @mouseenter="hovered = g"
        @mouseleave="hovered = null"
        @focus="hovered = g"
        @blur="hovered = null"
        @keydown="onKey($event, g)"
      >
        <span class="flex items-center gap-0.5">
          <!-- Alpine is not the next rung of the same ladder; it is marked as
               such before it is chosen, not only after. -->
          <Icon v-if="g === 'A'" name="warning" :size="11" :width="2" />
          <span class="font-semibold">{{ g }}</span>
        </span>
        <span class="grade-bar" :style="{ background: `var(--grade-${g.toLowerCase()})` }" />
      </button>
    </div>
    <p class="mt-1.5 min-h-[30px] text-[11.5px] leading-[1.35] text-muted">
      <span class="font-medium text-ink">{{ gradeName(shown) }}</span>
      — {{ gradeDesc(shown) }}
    </p>
  </div>
</template>

<style scoped>
.grade-chip {
  position: relative;
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  padding: 7px 4px 6px;
  border: 1px solid var(--line);
  border-radius: var(--radius-control);
  background: var(--surface-2);
  color: var(--muted);
  font-size: 12.5px;
  transition:
    background 0.14s ease,
    color 0.14s ease,
    border-color 0.14s ease;
}
.grade-chip:hover:not(.is-on) {
  color: var(--ink);
  border-color: var(--line-strong);
}
.grade-chip.is-on {
  background: var(--surface);
  border-color: var(--ink);
  color: var(--ink);
}
.grade-chip.is-alpine {
  color: var(--grade-a);
}
.grade-chip.is-alpine.is-on {
  border-color: var(--grade-a);
  box-shadow: inset 0 0 0 1px var(--grade-a);
  color: var(--grade-a);
}
.grade-bar {
  display: block;
  height: 3px;
  width: 20px;
  border-radius: 2px;
  opacity: 0.55;
}
.grade-chip.is-on .grade-bar {
  opacity: 1;
}
/* Five across a phone: 63 px wide each at 375, and 45 px tall for the thumb. */
@media (max-width: 899px) {
  .grade-chip {
    padding: 10px 2px 9px;
  }
}
</style>
