<script setup lang="ts">
import Icon from '../Icon.vue';
import Toggle from '../Toggle.vue';
import { GRADES, gradeDesc, gradeName } from '../../lib/format';
import { grade, lifts } from '../../composables/usePlanner';
import { t } from '../../i18n';

/**
 * The walking part of the question, asked when it is wanted rather than shown
 * always: the grade you will walk at, with the words that say what each one
 * asks of you — a strip of five letters never had the room for those — and
 * whether a lift may be ridden. A sheet from the bottom, where the thumb is.
 */
const emit = defineEmits<{ close: [] }>();

/** The catalogue's descriptions follow a dash; on a line of their own they start a sentence. */
const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);
</script>

<template>
  <div class="fixed inset-0 z-40" role="dialog" aria-modal="true" :aria-label="t('top.tripOptions')">
    <button type="button" class="absolute inset-0" style="background: rgb(0 0 0 / 0.45)" :aria-label="t('popover.close')" @click="emit('close')" />
    <div
      class="sheet card absolute inset-x-0 bottom-0 rounded-b-none rounded-t-[18px] border-b-0"
      :style="{ boxShadow: 'var(--shadow-2)', paddingBottom: 'env(safe-area-inset-bottom, 0px)' }"
    >
      <div class="flex items-center gap-2 px-4 pt-3 pb-1">
        <h2 class="label flex-1">{{ t('panel.trailGrade') }}</h2>
        <button type="button" class="btn-quiet -my-2 px-3 text-[13px]" @click="emit('close')">{{ t('card.done') }}</button>
      </div>

      <div role="radiogroup" :aria-label="t('top.gradeGroup')">
        <button
          v-for="g in GRADES"
          :key="g"
          type="button"
          role="radio"
          class="grade-row"
          :class="{ 'is-on': grade === g, 'is-alpine': g === 'A' }"
          :aria-checked="grade === g"
          @click="grade = g"
        >
          <span class="grade-mark" :style="{ '--g': `var(--grade-${g.toLowerCase()})` }">
            <span class="flex items-center gap-0.5">
              <Icon v-if="g === 'A'" name="warning" :size="12" :width="2" />
              <span class="font-semibold">{{ g }}</span>
            </span>
            <span class="bar" aria-hidden="true" />
          </span>
          <span class="min-w-0 flex-1">
            <span class="block text-[14px] text-ink" :class="grade === g ? 'font-semibold' : ''">{{ gradeName(g) }}</span>
            <span class="block text-[12px] leading-snug text-muted">{{ cap(gradeDesc(g)) }}</span>
          </span>
          <Icon
            v-if="grade === g"
            name="check"
            :size="16"
            class="shrink-0"
            :style="{ color: g === 'A' ? 'var(--grade-a)' : 'var(--ink)' }"
          />
        </button>
      </div>

      <!-- A switch, not one more grade: it sits under its own line. -->
      <div class="border-t border-line px-4">
        <Toggle v-model="lifts" :label="t('panel.useLifts')" :hint="t('panel.useLiftsHint')" />
      </div>
    </div>
  </div>
</template>

<style scoped>
.grade-row {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 14px;
  min-height: 56px;
  padding: 7px 16px;
  text-align: left;
}
.grade-row + .grade-row {
  border-top: 1px solid var(--line);
}
.grade-row:active {
  background: var(--surface-2);
}
.grade-mark {
  display: flex;
  width: 40px;
  flex: none;
  flex-direction: column;
  align-items: center;
  gap: 4px;
  color: var(--muted);
  font-size: 13px;
}
.is-on .grade-mark {
  color: var(--ink);
}
.is-alpine .grade-mark {
  color: var(--grade-a);
}
.bar {
  width: 22px;
  height: 3px;
  border-radius: 2px;
  background: var(--g);
  opacity: 0.55;
}
.is-on .bar {
  opacity: 1;
}
</style>
