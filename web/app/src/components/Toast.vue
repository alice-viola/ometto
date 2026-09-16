<script setup lang="ts">
import { dismissToast, toastAction, toastMessage } from '../composables/useToast';

/** The action runs after the toast is gone, so what it does is seen, not covered. */
function act() {
  const a = toastAction.value;
  dismissToast();
  a?.run();
}
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
        class="pointer-events-auto flex items-center gap-3 rounded-full py-2 pl-3.5 text-[12.5px] font-medium"
        :class="toastAction ? 'pr-1.5' : 'pr-3.5'"
        :style="{ background: 'var(--ink)', color: 'var(--ink-invert)', boxShadow: 'var(--shadow-2)' }"
      >
        <span>{{ toastMessage }}</span>
        <button v-if="toastAction" type="button" class="toast-action" @click="act">{{ toastAction.label }}</button>
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
/* Inverted like the pill it sits in, with a lighter well so it reads as a button. */
.toast-action {
  min-height: 30px;
  padding: 0 11px;
  border-radius: 999px;
  background: color-mix(in srgb, var(--ink-invert) 18%, transparent);
  color: var(--ink-invert);
  font-weight: 600;
}
.toast-action:active {
  background: color-mix(in srgb, var(--ink-invert) 30%, transparent);
}
</style>
