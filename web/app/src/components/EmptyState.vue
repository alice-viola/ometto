<script setup lang="ts">
import { computed } from 'vue';
import { alternatives, applyQuery, slots } from '../composables/usePlanner';
import type { Grade, Mode } from '../lib/types';

interface Example {
  label: string;
  points: { lat: number; lon: number; name: string }[];
  mode: Mode;
  grade: Grade;
}

/**
 * One example, so the first screen shows what the app answers rather than
 * describing it.
 *
 * Only one: at 380 px the row has 348 px to spend and this chip measures
 * 220 px, so a second ("Molveno → Rifugio Pedrotti · hike", also 220 px)
 * would wrap to a line of its own. A single chip that always sits on one
 * line beats three that reflow. Both other examples route correctly and can
 * come back the moment the row has the width for them.
 *
 * Coordinates are the geocoder's own — Trento the city, Monte Stivo the
 * peak — and the trip is re-checked against the live graph.
 */
const EXAMPLES: Example[] = [
  {
    label: 'Trento → Monte Stivo · car + hike',
    points: [
      { lat: 46.066423, lon: 11.12576, name: 'Trento' },
      { lat: 45.920127, lon: 10.963691, name: 'Monte Stivo' },
    ],
    mode: 'car+hike',
    grade: 'E',
  },
];

/** Gone the moment the person has a question of their own; back when they clear. */
const untouched = computed(() => !slots.value.some((s) => s.point));

function run(e: Example) {
  // The same door a shared link comes through, so an example and a link can
  // never drift apart. `true` computes it as if Compute had been pressed:
  // the trip lands in history and in the address bar.
  applyQuery(
    {
      points: e.points,
      mode: e.mode,
      grade: e.grade,
      alternatives: alternatives.value, // left as the person has it
      lifts: false,
    },
    true,
  );
}
</script>

<template>
  <div class="pt-1">
    <p class="text-[13.5px] leading-relaxed text-muted">
      Pick two points and see how long it takes — by car, by bike or on foot, with the climb and the
      trail grade along the way.
    </p>

    <div v-if="untouched" class="mt-4">
      <h3 class="label mb-2">Try an example</h3>
      <div class="flex flex-wrap gap-1.5">
        <button
          v-for="e in EXAMPLES"
          :key="e.label"
          type="button"
          class="rounded-full border border-line bg-surface-2 px-2.5 py-1.5 text-[12.5px] font-medium whitespace-nowrap transition-colors hover:border-line-strong hover:bg-surface max-[899px]:min-h-11 max-[899px]:px-3.5"
          @click="run(e)"
        >
          {{ e.label }}
        </button>
      </div>
    </div>
  </div>
</template>
