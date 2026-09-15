<script setup lang="ts">
import { computed } from 'vue';
import Icon from '../Icon.vue';
import {
  acceptMove,
  avoidedWays,
  clearAvoids,
  clearVias,
  dismissNote,
  pointNotes,
  slots,
  stopAvoiding,
  viaCount,
} from '../../composables/usePlanner';

/**
 * What the service had to say about the question before it answered — a
 * point it had to move, a point it could not reach — and the shape the person
 * gave the route: vias and avoided ways. On a phone these sit at the top of
 * the answer sheet, where the eye goes when the answer lands.
 */
const notes = computed(() =>
  Object.entries(pointNotes.value).map(([k, n]) => {
    const i = Number(k);
    const last = slots.value.length - 1;
    const role = i === 0 ? 'starts' : i === last ? 'ends' : 'passes here';
    const d = n.metres >= 1000 ? `${(n.metres / 1000).toFixed(1)} km` : `${Math.round(n.metres)} m`;
    const text =
      n.kind === 'far'
        ? `The nearest road or trail is ${d} away.`
        : `The route ${role} ${d} away${n.name ? ` on ${n.name}` : ''}.`;
    return { index: i, kind: n.kind, text };
  }),
);

function wayLabel(w: { name?: string; class?: string }): string {
  return w.name || w.class?.replace(/_/g, ' ') || 'that way';
}
</script>

<template>
  <div v-if="notes.length || viaCount || avoidedWays.length" class="grid gap-2 pb-2" data-sheet-stay>
    <div
      v-for="n in notes"
      :key="n.index"
      class="rounded-[9px] px-3 py-2 text-[13px] leading-snug"
      :style="{ background: 'var(--surface-2)', color: n.kind === 'far' ? 'var(--dest)' : 'var(--muted)' }"
    >
      <p>{{ n.text }}</p>
      <div v-if="n.kind === 'moved'" class="mt-2 flex gap-2">
        <button type="button" class="note-btn" @click="acceptMove(n.index)">Move marker</button>
        <button type="button" class="note-btn" @click="dismissNote(n.index)">Keep</button>
      </div>
    </div>

    <div v-if="viaCount || avoidedWays.length" class="flex flex-wrap items-center gap-1.5">
      <button v-if="viaCount" type="button" class="chip" @click="clearVias">
        <span class="inline-block size-2 rounded-full" :style="{ background: 'var(--accent)' }" aria-hidden="true" />
        Via {{ viaCount }} {{ viaCount === 1 ? 'point' : 'points' }}
        <Icon name="x" :size="12" />
      </button>
      <button
        v-for="w in avoidedWays"
        :key="w.id"
        type="button"
        class="chip chip-avoid"
        :aria-label="`Stop avoiding ${wayLabel(w)}`"
        @click="stopAvoiding(w)"
      >
        <span class="max-w-[160px] truncate">Avoiding {{ wayLabel(w) }}</span>
        <Icon name="x" :size="12" />
      </button>
      <button v-if="avoidedWays.length > 1" type="button" class="chip" @click="clearAvoids">Clear all</button>
    </div>
  </div>
</template>

<style scoped>
.note-btn {
  display: inline-flex;
  align-items: center;
  min-height: 40px;
  padding: 0 14px;
  border: 1px solid var(--line);
  border-radius: var(--radius-control);
  background: var(--surface);
  color: var(--ink);
  font-size: 13px;
}
.chip {
  display: inline-flex;
  align-items: center;
  gap: 6px;
  min-height: 36px;
  padding: 0 10px 0 12px;
  border: 1px solid var(--line);
  border-radius: 999px;
  background: var(--surface-2);
  color: var(--ink);
  font-size: 12.5px;
}
.chip-avoid {
  border-color: var(--dest);
  background: var(--alert-soft);
}
</style>
