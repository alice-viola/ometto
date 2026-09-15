<script setup lang="ts">
defineProps<{ modelValue: boolean; label: string; hint?: string; disabled?: boolean }>();
const emit = defineEmits<{ 'update:modelValue': [boolean] }>();
</script>

<template>
  <label
    class="flex items-center gap-3 py-2"
    :class="disabled ? 'opacity-45' : 'cursor-pointer'"
  >
    <span class="min-w-0 flex-1">
      <span class="block text-[13.5px]">{{ label }}</span>
      <span v-if="hint" class="block text-[11.5px] leading-snug text-faint">{{ hint }}</span>
    </span>
    <button
      type="button"
      role="switch"
      :aria-checked="modelValue"
      :aria-label="label"
      :disabled="disabled"
      class="relative h-[22px] w-[38px] shrink-0 rounded-full border transition-colors"
      :style="{
        background: modelValue ? 'var(--ink)' : 'var(--surface-3)',
        borderColor: modelValue ? 'var(--ink)' : 'var(--line)',
      }"
      @click="!disabled && emit('update:modelValue', !modelValue)"
    >
      <span
        class="absolute top-[2px] size-[16px] rounded-full transition-[left]"
        :style="{ left: modelValue ? '18px' : '2px', background: modelValue ? 'var(--ink-invert)' : 'var(--surface)' }"
      />
    </button>
  </label>
</template>
