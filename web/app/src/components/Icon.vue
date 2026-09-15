<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(
  defineProps<{ name: string; size?: number | string; filled?: boolean; width?: number }>(),
  { size: 18, filled: false, width: 1.6 },
);

/** One hand-drawn line set on a 24 grid, so every glyph shares a weight. */
const PATHS: Record<string, string> = {
  car: '<path d="M4.8 15.6v-2.3l1.7-4a1.8 1.8 0 0 1 1.7-1.1h7.6a1.8 1.8 0 0 1 1.7 1.1l1.7 4v2.3"/><path d="M4.8 15.6h14.4"/><path d="M6.9 13.2h10.2"/><circle cx="8" cy="16.7" r="1.4"/><circle cx="16" cy="16.7" r="1.4"/>',
  bike: '<circle cx="6" cy="16" r="3.5"/><circle cx="18" cy="16" r="3.5"/><path d="M6 16l4.2-8h3.4"/><path d="M10.4 8.2L14.8 16H18"/><path d="M8.6 8h3.2"/><path d="M14.6 16H6.6"/>',
  hike: '<circle cx="12.6" cy="4.7" r="1.8"/><path d="M10.7 20.6l2-6.2-2.6-2.5 1.1-4.3"/><path d="M11.3 7.9L8.2 9.7l-.9 2.7"/><path d="M12.3 9.6l2.7 1.4 1.3 2.9"/><path d="M12.4 14.6l2.5 6"/><path d="M17.9 5.6l.7 15"/>',
  swap: '<path d="M7.5 4.5v14"/><path d="M4.2 15.4l3.3 3.3 3.3-3.3"/><path d="M16.5 19.5v-14"/><path d="M19.8 8.6l-3.3-3.3-3.3 3.3"/>',
  plus: '<path d="M12 5.4v13.2M5.4 12h13.2"/>',
  minus: '<path d="M5.4 12h13.2"/>',
  x: '<path d="M6.4 6.4l11.2 11.2M17.6 6.4L6.4 17.6"/>',
  chevronDown: '<path d="M6.4 9.4l5.6 5.4 5.6-5.4"/>',
  chevronUp: '<path d="M6.4 14.6l5.6-5.4 5.6 5.4"/>',
  chevronRight: '<path d="M9.4 6.4l5.4 5.6-5.4 5.6"/>',
  arrowLeft: '<path d="M19 12H5"/><path d="M10.6 6.4L5 12l5.6 5.6"/>',
  arrowRight: '<path d="M5 12h14"/><path d="M13.4 6.4L19 12l-5.6 5.6"/>',
  star: '<path d="M12 3.8l2.55 5.16 5.7.83-4.13 4.02.98 5.67L12 16.8l-5.1 2.68.98-5.67L3.75 9.79l5.7-.83z"/>',
  clock: '<circle cx="12" cy="12" r="8.4"/><path d="M12 7.2V12l3.2 1.9"/>',
  ascent: '<path d="M4 17.6l5.6-7.2 3.4 4.1L19 6.6"/><path d="M15.2 6.6H19v3.8"/>',
  descent: '<path d="M4 6.6l5.6 7.2 3.4-4.1L19 17.4"/><path d="M15.2 17.4H19v-3.8"/>',
  share: '<path d="M12 15.2V4.2"/><path d="M8.5 7.6L12 4.2l3.5 3.4"/><path d="M5 13.6V18a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-4.4"/>',
  history: '<path d="M3.9 12a8.1 8.1 0 1 0 2.5-5.9"/><path d="M3.6 4.6v5.5h5.5"/><path d="M12 8.1v4.4l3 1.8"/>',
  settings: '<path d="M4 7.4h8.4M17.2 7.4H20M4 16.6h3.4M12.2 16.6H20"/><circle cx="14.8" cy="7.4" r="2.4"/><circle cx="9.8" cy="16.6" r="2.4"/>',
  sun: '<circle cx="12" cy="12" r="4"/><path d="M12 2.8v2.2M12 19v2.2M2.8 12H5M19 12h2.2M5.5 5.5l1.6 1.6M16.9 16.9l1.6 1.6M5.5 18.5l1.6-1.6M16.9 7.1l1.6-1.6"/>',
  moon: '<path d="M20.2 14.4A8.5 8.5 0 0 1 9.6 3.8a8.5 8.5 0 1 0 10.6 10.6z"/>',
  monitor: '<rect x="3.2" y="4.6" width="17.6" height="12" rx="1.8"/><path d="M8.6 20h6.8M12 16.6V20"/>',
  locate: '<circle cx="12" cy="12" r="3.2"/><circle cx="12" cy="12" r="7.4"/><path d="M12 2.6v2.2M12 19.2v2.2M2.6 12h2.2M19.2 12h2.2"/>',
  search: '<circle cx="10.8" cy="10.8" r="6.3"/><path d="M15.4 15.4L20 20"/>',
  trash: '<path d="M4.6 6.7h14.8"/><path d="M9.5 6.7V5a1 1 0 0 1 1-1h3a1 1 0 0 1 1 1v1.7"/><path d="M6.7 6.7l.8 12.1a1.6 1.6 0 0 0 1.6 1.5h5.8a1.6 1.6 0 0 0 1.6-1.5l.8-12.1"/>',
  peak: '<path d="M3 19.2l6.2-10.8 3.5 5.9 2.2-3.5L21 19.2z"/>',
  crag: '<path d="M4.6 19.4V9.2l5.6-4 9.2 3.4v10.8z"/><path d="M12.2 9.6l-1.8 3.6 1.6 3.2"/>',
  hut: '<path d="M4.4 11.6L12 5l7.6 6.6"/><path d="M6.4 10.4V19.2h11.2V10.4"/><path d="M10.4 19.2v-4.4h3.2v4.4"/>',
  pass: '<path d="M2.8 18c3.4 0 5.2-2.2 6.7-5.5C10.9 9.4 12 7.4 12 7.4s1.1 2 2.5 5.1C16 15.8 17.8 18 21.2 18"/>',
  place: '<path d="M12 21s6.6-6.1 6.6-10.4A6.6 6.6 0 1 0 5.4 10.6C5.4 14.9 12 21 12 21z"/><circle cx="12" cy="10.4" r="2.3"/>',
  street: '<path d="M8.4 3.6L5.6 20.4M15.6 3.6l2.8 16.8M12 5v2.6M12 10.7v2.6M12 16.4V19"/>',
  trail: '<path d="M6 20.4c0-4 3-4.6 3-7.4S5.4 9.6 6.6 6.4C7.4 4.3 9.6 3.6 11 3.6"/><path d="M14.2 20.4c2-2.4 1.2-5 3.2-6.8"/>',
  info: '<circle cx="12" cy="12" r="8.6"/><path d="M12 11v5.6"/><path d="M12 7.8h.01"/>',
  cube: '<path d="M12 3.3l8 4.4v8.6l-8 4.4-8-4.4V7.7z"/><path d="M4 7.7l8 4.4 8-4.4"/><path d="M12 12.1v8.6"/>',
  contour: '<path d="M2.6 17.6c4.2-5.8 8.6-5.8 12.8 0"/><path d="M5.4 13.8c3-4.2 6.2-4.2 9.2 0"/><path d="M8.2 10.2c1.6-2.3 3.2-2.3 4.8 0"/><path d="M17.4 7.4l3.8 4.6"/>',
  check: '<path d="M5 12.6l4.6 4.4L19 7"/>',
  pencil: '<path d="M4.6 19.4l.7-3.7L15.9 5.1a1.8 1.8 0 0 1 2.5 0l.9.9a1.8 1.8 0 0 1 0 2.5L8.3 18.7z"/>',
  copy: '<rect x="8.8" y="8.8" width="11" height="11" rx="2"/><path d="M15.2 5.2H6.2a2 2 0 0 0-2 2v9"/>',
  dots: '<circle cx="12" cy="5.6" r="1.3"/><circle cx="12" cy="12" r="1.3"/><circle cx="12" cy="18.4" r="1.3"/>',
  route: '<circle cx="6.2" cy="6.2" r="2.6"/><circle cx="17.8" cy="17.8" r="2.6"/><path d="M6.2 8.8v4.4a4 4 0 0 0 4 4h5"/>',
  warning: '<path d="M12 4.4L2.8 19.6h18.4z"/><path d="M12 10v4.2"/><path d="M12 17.4h.01"/>',
  lift: '<path d="M2.6 5.4l18.8 3.5"/><path d="M12 7.2v2.1"/><rect x="7.7" y="9.3" width="8.6" height="7.8" rx="1.8"/><path d="M7.7 12.9h8.6"/>',
  panelHide: '<rect x="3.2" y="4.6" width="17.6" height="14.8" rx="2.4"/><path d="M10 4.6v14.8"/><path d="M17.2 9.9L14.6 12l2.6 2.1"/>',
  panelShow: '<rect x="3.2" y="4.6" width="17.6" height="14.8" rx="2.4"/><path d="M10 4.6v14.8"/><path d="M14.4 9.9L17 12l-2.6 2.1"/>',
};

const body = computed(() => PATHS[props.name] ?? '');
const px = computed(() => (typeof props.size === 'number' ? `${props.size}px` : props.size));
</script>

<template>
  <svg
    :width="px"
    :height="px"
    viewBox="0 0 24 24"
    :fill="filled ? 'currentColor' : 'none'"
    :stroke="filled ? 'none' : 'currentColor'"
    :stroke-width="width"
    stroke-linecap="round"
    stroke-linejoin="round"
    aria-hidden="true"
    focusable="false"
    class="shrink-0"
    v-html="body"
  />
</template>
