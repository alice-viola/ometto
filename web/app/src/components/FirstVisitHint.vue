<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import Icon from './Icon.vue';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';
import { hasResult, slots } from '../composables/usePlanner';
import { hasHover, isCompact, sheetHeight } from '../composables/useMedia';

const KEY = 'hint.seen';
const shown = ref(false);

function dismiss() {
  if (!shown.value) return;
  shown.value = false;
  writeLocalRaw(KEY, '1');
}

onMounted(() => {
  if (readLocalRaw(KEY) === '1') return;
  // Let the map paint first: a hint over a grey rectangle explains nothing.
  window.setTimeout(() => {
    if (!hasResult.value && !slots.value.some((s) => s.point)) shown.value = true;
  }, 1200);
});

// It has done its job the moment the person does the thing.
watch([hasResult, () => slots.value.some((s) => s.point)], ([has, any]) => {
  if (has || any) dismiss();
});
</script>

<template>
  <Transition name="hint">
    <!-- On a phone the sheet covers the bottom of the map: the hint rides
         above it, and above the attribution, which MapLibre opens on a narrow
         map until the first touch. -->
    <div
      v-if="shown"
      class="pointer-events-none absolute inset-x-0 bottom-0 z-10 flex justify-center p-4 sm:p-5"
      :style="isCompact ? { bottom: `${sheetHeight + 44}px` } : undefined"
    >
      <p
        class="pointer-events-auto flex items-center gap-2 rounded-full border border-line py-2 pl-3.5 pr-2 text-[12.5px] text-muted"
        :style="{ background: 'var(--surface)', boxShadow: 'var(--shadow-2)' }"
        role="note"
      >
        <span>{{ hasHover ? 'Click' : 'Tap' }} anywhere on the map to set a point.</span>
        <button class="btn-quiet -my-1 p-1.5 max-[899px]:-my-2" aria-label="Got it" title="Got it" @click="dismiss">
          <Icon name="x" :size="13" />
        </button>
      </p>
    </div>
  </Transition>
</template>

<style scoped>
.hint-enter-active,
.hint-leave-active {
  transition: opacity 0.25s ease, transform 0.25s ease;
}
.hint-enter-from,
.hint-leave-to {
  opacity: 0;
  transform: translateY(6px);
}
</style>
