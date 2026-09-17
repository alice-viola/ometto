<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref } from 'vue';
import Icon from './Icon.vue';
import Toggle from './Toggle.vue';
import { GRADES, gradeDesc, gradeName } from '../lib/format';
import { grade, lifts } from '../composables/usePlanner';
import { t } from '../i18n';

/**
 * The walking part of the question, asked when it is wanted rather than shown
 * always: the grade you will walk at, with the words that say what each one
 * asks of you — a strip of five letters never had the room for those — and
 * whether a lift may be ridden. On a phone a sheet from the bottom, where the
 * thumb is; on a desktop a popover under the badge, placed by the caller.
 */
const props = withDefaults(defineProps<{ variant?: 'sheet' | 'popover' }>(), { variant: 'sheet' });
const emit = defineEmits<{ close: [] }>();

/** The catalogue's descriptions follow a dash; on a line of their own they start a sentence. */
const cap = (s: string) => (s ? s[0].toUpperCase() + s.slice(1) : s);

/**
 * Escape closes it and is spent doing so: on a desktop the same key would
 * otherwise fold the whole panel away. Captured, so it is heard first.
 */
function onKeydown(e: KeyboardEvent) {
  if (e.key !== 'Escape' && e.key !== 'Esc') return;
  e.preventDefault();
  emit('close');
}

/** The popover takes the focus to the grade it shows, so the arrows and Tab start there. */
const group = ref<HTMLElement | null>(null);
onMounted(() => {
  window.addEventListener('keydown', onKeydown, true);
  if (props.variant === 'popover') group.value?.querySelector<HTMLElement>('[aria-checked="true"]')?.focus();
});
onBeforeUnmount(() => window.removeEventListener('keydown', onKeydown, true));
</script>

<template>
  <div
    :class="variant === 'sheet' ? 'fixed inset-0 z-40' : ''"
    role="dialog"
    :aria-modal="variant === 'sheet' ? 'true' : undefined"
    :aria-label="t('top.tripOptions')"
  >
    <!-- The sheet dims the map behind it; the popover closes, unseen, on a click anywhere else. -->
    <button
      type="button"
      :class="variant === 'sheet' ? 'absolute inset-0' : 'fixed inset-0 cursor-default'"
      :style="variant === 'sheet' ? 'background: rgb(0 0 0 / 0.45)' : undefined"
      :aria-label="t('popover.close')"
      :tabindex="variant === 'sheet' ? undefined : -1"
      @click="emit('close')"
    />
    <div
      :class="
        variant === 'sheet'
          ? 'sheet card absolute inset-x-0 bottom-0 rounded-b-none rounded-t-[18px] border-b-0'
          : 'card is-popover relative overflow-hidden'
      "
      :style="{
        boxShadow: 'var(--shadow-2)',
        paddingBottom: variant === 'sheet' ? 'env(safe-area-inset-bottom, 0px)' : undefined,
      }"
    >
      <div class="flex items-center gap-2 px-4 pt-3 pb-1">
        <h2 class="label flex-1">{{ t('panel.trailGrade') }}</h2>
        <button type="button" class="btn-quiet -my-2 px-3 text-[13px]" @click="emit('close')">{{ t('card.done') }}</button>
      </div>

      <div ref="group" role="radiogroup" :aria-label="t('top.gradeGroup')">
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
@media (hover: hover) {
  .grade-row:hover:not(.is-on) {
    background: var(--surface-2);
  }
}
/* A pointer needs less than a thumb: the popover keeps the lifts in view on a short window. */
.is-popover .grade-row {
  min-height: 48px;
  padding-block: 5px;
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
