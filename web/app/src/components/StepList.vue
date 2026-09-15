<script setup lang="ts">
import { computed, ref } from 'vue';
import Icon from './Icon.vue';
import { fmtDistance } from '../lib/format';
import type { Step } from '../lib/types';

const props = defineProps<{ steps: Step[] }>();
const expanded = ref(false);
const LIMIT = 6;

/** The graph splits a street into every stretch it is made of; a person reads
    one street. Consecutive steps with the same name and mode become one. */
const merged = computed<Step[]>(() => {
  const out: Step[] = [];
  for (const s of props.steps) {
    const last = out[out.length - 1];
    if (last && last.name === s.name && last.mode === s.mode) {
      last.meters += s.meters;
      last.seconds += s.seconds;
    } else {
      out.push({ ...s });
    }
  }
  return out;
});

const shown = computed(() => (expanded.value ? merged.value : merged.value.slice(0, LIMIT)));
const hidden = computed(() => Math.max(0, merged.value.length - LIMIT));
</script>

<template>
  <div>
    <ol class="grid">
      <li
        v-for="(s, i) in shown"
        :key="i"
        class="flex items-baseline gap-2 border-b border-line py-1.5 last:border-b-0"
      >
        <Icon :name="s.mode" :size="13" class="relative top-0.5 shrink-0" :style="{ color: `var(--${s.mode})` }" />
        <span class="min-w-0 flex-1 truncate text-[13px]">{{ s.name }}</span>
        <span class="shrink-0 text-[12px] text-muted">{{ fmtDistance(s.meters) }}</span>
      </li>
    </ol>
    <button
      v-if="hidden && !expanded"
      class="btn-quiet mt-1 px-1.5 py-1 text-[12px]"
      @click="expanded = true"
    >
      Show {{ hidden }} more
    </button>
    <button v-else-if="expanded" class="btn-quiet mt-1 px-1.5 py-1 text-[12px]" @click="expanded = false">
      Show less
    </button>
  </div>
</template>
