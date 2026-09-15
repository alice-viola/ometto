<script setup lang="ts">
import { computed } from 'vue';
import { cairnInner, cairnViewBox, specFor } from '../brand/cairn.mjs';

const props = withDefaults(
  defineProps<{ size?: number; variant?: 'filled' | 'outline'; label?: string }>(),
  { size: 24, variant: 'filled', label: '' },
);

/** Under 24 px the four-stone stack loses its gaps; three hold. */
const spec = computed(() => specFor(props.size));
const box = computed(() => cairnViewBox({ spec: spec.value }));
const body = computed(() => cairnInner({ variant: props.variant, spec: spec.value }));
</script>

<template>
  <svg
    :width="size"
    :height="size"
    :viewBox="`${box.x} ${box.y} ${box.w} ${box.h}`"
    :role="label ? 'img' : undefined"
    :aria-label="label || undefined"
    :aria-hidden="label ? undefined : 'true'"
    focusable="false"
    class="shrink-0"
    v-html="body"
  />
</template>
