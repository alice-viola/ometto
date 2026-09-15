import { ref } from 'vue';

/** Where the pointer is on the elevation profile, mirrored on the map. */
export const hoverPoint = ref<[number, number] | null>(null);
export const hoverLabel = ref<string>('');
