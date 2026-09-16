<script setup lang="ts">
import { computed } from 'vue';
import Icon from './Icon.vue';
import PointField from './PointField.vue';
import {
  MAX_POINTS,
  acceptMove,
  addStop,
  avoidedWays,
  avoids,
  clearAll,
  clearAvoids,
  clearVias,
  dismissNote,
  isCombinedMode,
  moveSlot,
  parkingIndex,
  placeIndices,
  pointNotes,
  removeSlot,
  setPoint,
  slots,
  stopAvoiding,
  swapEnds,
  viaCount,
} from '../composables/usePlanner';
import { t } from '../i18n';
import { wayName } from '../i18n/service';

/** Only the places the person named; vias are route shape, not destinations. */
const places = computed(() => placeIndices.value.map((i) => ({ index: i, slot: slots.value[i] })));
const canAdd = computed(() => places.value.length < MAX_POINTS);
const anyFilled = computed(() => slots.value.some((s) => s.point) || avoids.value.length > 0);

function roleOf(i: number, at: number): 'start' | 'stop' | 'destination' | 'parking' {
  if (at === 0) return 'start';
  if (at === places.value.length - 1) return 'destination';
  return i === parkingIndex.value ? 'parking' : 'stop';
}

/** A way with no name still has to be nameable in a chip. */
function wayLabel(w: { name?: string; class?: string }): string {
  return wayName(w.name) || w.class?.replace(/_/g, ' ') || t('search.thatWay');
}
</script>

<template>
  <section :aria-label="t('search.points')">
    <div class="grid gap-1.5">
      <PointField
        v-for="(p, at) in places"
        :key="p.slot.key"
        :model-value="p.slot.point"
        :role="roleOf(p.index, at)"
        :index="p.index"
        :stop-number="at"
        :note="pointNotes[p.index] ?? null"
        :can-remove="at > 0 && at < places.length - 1"
        :can-move-up="at > 1 && at < places.length - 1"
        :can-move-down="at > 0 && at < places.length - 2"
        @update:model-value="setPoint(p.index, $event)"
        @remove="removeSlot(p.index)"
        @move="moveSlot(p.index, $event)"
        @accept-move="acceptMove(p.index)"
        @dismiss-note="dismissNote(p.index)"
      />
    </div>

    <!-- Shape the person dragged into the route, summarised rather than listed. -->
    <p v-if="viaCount" class="mt-1.5 flex items-center gap-2 pl-0.5 text-[11.5px] text-muted">
      <span class="inline-block size-2 rounded-full" :style="{ background: 'var(--accent)' }" aria-hidden="true" />
      <span>{{ t('search.via', { n: viaCount }) }}</span>
      <button class="note-action" @click="clearVias">{{ t('search.clear') }}</button>
    </p>

    <div v-if="avoidedWays.length" class="mt-1.5 flex flex-wrap items-center gap-1.5">
      <span class="text-[11.5px] text-muted">{{ t('search.avoiding') }}</span>
      <button
        v-for="w in avoidedWays"
        :key="w.id"
        class="avoid-chip"
        :aria-label="t('search.stopAvoiding', { name: wayLabel(w) })"
        :title="t('search.stopAvoiding', { name: wayLabel(w) })"
        @click="stopAvoiding(w)"
      >
        <span class="max-w-[150px] truncate">{{ wayLabel(w) }}</span>
        <Icon name="x" :size="11" />
      </button>
      <button v-if="avoidedWays.length > 1" class="note-action text-[11.5px]" @click="clearAvoids">
        {{ t('search.clearAll') }}
      </button>
    </div>

    <div class="mt-1.5 flex items-center gap-1">
      <button
        class="btn-quiet flex items-center gap-1 px-2 py-1.5 text-[12.5px]"
        :disabled="!canAdd"
        @click="addStop"
      >
        <Icon name="plus" :size="14" /> {{ t('search.addStop') }}
      </button>
      <span class="flex-1" />
      <button
        class="btn-quiet p-1.5"
        :title="t('search.swap')"
        :aria-label="t('search.swap')"
        @click="swapEnds"
      >
        <Icon name="swap" :size="15" />
      </button>
      <button
        class="btn-quiet p-1.5"
        :title="t('search.clearPoints')"
        :aria-label="t('search.clearPoints')"
        :disabled="!anyFilled"
        @click="clearAll"
      >
        <Icon name="trash" :size="15" />
      </button>
    </div>

    <p
      v-if="isCombinedMode && places.length < 3 && anyFilled"
      class="mt-1 pl-0.5 text-[11.5px] leading-snug text-muted"
    >
      {{ t('search.parkHint') }}
      <button class="note-action" @click="addStop">{{ t('search.addOne') }}</button>
    </p>
  </section>
</template>

<style scoped>
.note-action {
  color: var(--ink);
  font-size: 11.5px;
  text-decoration: underline;
  text-decoration-color: var(--line-strong);
  text-underline-offset: 2px;
}
.note-action:hover {
  text-decoration-color: var(--ink);
}
.avoid-chip {
  display: inline-flex;
  align-items: center;
  gap: 4px;
  max-width: 100%;
  padding: 3px 6px 3px 8px;
  border: 1px solid var(--dest);
  border-radius: 999px;
  background: var(--alert-soft);
  color: var(--ink);
  font-size: 11.5px;
  transition: background 0.14s ease;
}
.avoid-chip:hover {
  background: var(--surface-3);
}
/* Phones: the links keep their look, the hit areas grow to a thumb's; the
   padding is paid back by the margin so the lines stay where they are. */
@media (max-width: 899px) {
  .note-action {
    padding: 12px 6px;
    margin: -12px -6px;
  }
  .avoid-chip {
    min-height: 40px;
    padding: 0 8px 0 12px;
  }
}
</style>
