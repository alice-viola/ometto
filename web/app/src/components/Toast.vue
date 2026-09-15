<script setup lang="ts">
import { toastMessage } from '../composables/useToast';
</script>

<template>
  <Transition name="toast">
    <div
      v-if="toastMessage"
      class="pointer-events-none fixed inset-x-0 z-50 flex justify-center px-4"
      style="bottom: calc(24px + env(safe-area-inset-bottom))"
      role="status"
      aria-live="polite"
    >
      <div
        class="rounded-full px-3.5 py-2 text-[12.5px] font-medium"
        :style="{ background: 'var(--ink)', color: 'var(--ink-invert)', boxShadow: 'var(--shadow-2)' }"
      >
        {{ toastMessage }}
      </div>
    </div>
  </Transition>
</template>

<style scoped>
.toast-enter-active,
.toast-leave-active {
  transition: opacity 0.18s ease, transform 0.18s ease;
}
.toast-enter-from,
.toast-leave-to {
  opacity: 0;
  transform: translateY(6px);
}
</style>
