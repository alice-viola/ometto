<script setup lang="ts">
import { computed } from 'vue';
import Icon from './Icon.vue';
import { fmtAscent, fmtDistance, fmtDuration, fmtOffset, legModeLabel } from '../lib/format';
import { selected } from '../composables/usePlanner';
import {
  arrivalClock,
  live,
  position,
  stale,
  status as liveStatus,
  stop as stopLive,
} from '../composables/useLocation';
import { t } from '../i18n';

/**
 * What is left, while you are walking it. It sits above the alternatives in
 * both layouts, because the route you are on has stopped being one of three.
 *
 * The figures move every second, which is exactly what a live region must not
 * announce: the card is a `status` a screen reader can be pointed at, with the
 * whole sentence as its label, and it never interrupts.
 */
const shown = computed(
  () => (liveStatus.value === 'on' || liveStatus.value === 'locating') && !!selected.value,
);
const p = computed(() => (position.value ? live.value : null));

/** The fix's own error, when it is wide enough to be worth the doubt. */
const accuracy = computed(() => {
  const a = position.value?.accuracy ?? 0;
  return a >= 5 ? t('live.accuracy', { m: Math.round(a) }) : '';
});

/** Said in full for anyone who reads the card rather than glances at it. */
const spoken = computed(() => {
  const now = p.value;
  if (!now) return t('point.findingYou');
  if (now.arrived) return t('live.arrived');
  const parts = [
    t('live.toGo', {
      distance: fmtDistance(now.remainingMeters),
      time: fmtDuration(now.remainingSeconds),
    }),
    t('live.arriveAbout', { time: arrivalClock(now.remainingSeconds) }),
  ];
  if (now.remainingAscent >= 1) parts.push(t('live.stillToClimb', { ascent: fmtAscent(now.remainingAscent) }));
  if (now.offRoute) parts.push(t('live.offRoute', { distance: fmtOffset(now.distanceToRoute) }));
  return parts.join(' · ');
});
</script>

<template>
  <div v-if="shown" class="card live mb-1.5 px-3 py-2.5" role="status" aria-live="off" :aria-label="spoken">
    <!-- No fix yet: the shape of the answer, without the answer. -->
    <div v-if="!p" class="flex min-h-[24px] items-center gap-2">
      <span class="dot dot-pulse" role="img" :aria-label="t('live.dotAria')" />
      <span class="text-[13.5px] text-muted">{{ t('point.findingYou') }}</span>
      <span class="flex-1" />
      <span class="skeleton h-3 w-16 rounded" aria-hidden="true" />
      <button class="btn-quiet -my-1 px-2 py-1.5 text-[12.5px]" :aria-label="t('live.stopAria')" @click="stopLive()">
        {{ t('live.stop') }}
      </button>
    </div>

    <template v-else>
      <div class="flex min-h-[24px] items-center gap-2">
        <span class="dot" :class="stale ? 'is-stale' : ''" role="img" :aria-label="t('live.dotAria')" />
        <p
          v-if="p.arrived"
          class="flex items-center gap-1.5 text-[19px] font-semibold leading-none tracking-[-0.01em]"
        >
          <Icon name="check" :size="17" :style="{ color: 'var(--position)' }" />{{ t('live.arrived') }}
        </p>
        <p v-else class="text-[19px] font-semibold leading-none tracking-[-0.01em]">
          {{ fmtDistance(p.remainingMeters) }} · {{ fmtDuration(p.remainingSeconds) }}
        </p>
        <span class="flex-1" />
        <button class="btn-quiet -my-1 px-2 py-1.5 text-[12.5px]" :aria-label="t('live.stopAria')" @click="stopLive()">
          {{ t('live.stop') }}
        </button>
      </div>

      <div
        v-if="!p.arrived"
        class="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-[12.5px] text-muted"
      >
        <span>{{ t('live.arriveAbout', { time: arrivalClock(p.remainingSeconds) }) }}</span>
        <span
          v-if="p.remainingAscent >= 1"
          class="flex items-center gap-1"
          :aria-label="t('live.stillToClimb', { ascent: fmtAscent(p.remainingAscent) })"
        >
          <Icon name="ascent" :size="13" />{{ fmtAscent(p.remainingAscent) }}
        </span>
        <span class="flex items-center gap-1">
          <Icon :name="p.legMode" :size="13" :style="{ color: `var(--${p.legMode})` }" />
          {{ legModeLabel(p.legMode) }}
        </span>
        <span v-if="accuracy" class="text-faint">{{ accuracy }}</span>
      </div>

      <!-- Beside the route rather than on it: the figures still stand, they
           are simply measured from the nearest point of it. -->
      <p v-if="p.offRoute && !p.arrived" class="mt-1.5 text-[12px] leading-snug" :style="{ color: 'var(--dest)' }">
        {{ t('live.offRoute', { distance: fmtOffset(p.distanceToRoute) }) }}
      </p>
      <p v-if="stale" class="mt-1.5 text-[11.5px] leading-snug text-faint">{{ t('live.waiting') }}</p>
    </template>
  </div>
</template>

<style scoped>
/* The same hairline the alpine block uses, in the position's colour: the card
   belongs to the map's dot, not to the answer under it. */
.live {
  box-shadow: inset 2px 0 0 var(--position);
}
.dot {
  flex: none;
  width: 9px;
  height: 9px;
  border-radius: 999px;
  background: var(--position);
}
/* A fix that has stopped arriving stops looking current. */
.dot.is-stale {
  opacity: 0.45;
}
.dot-pulse {
  animation: live-breathe 1.2s ease-in-out infinite;
}
.skeleton {
  background: var(--surface-3);
  animation: live-breathe 1.5s ease-in-out infinite;
}
@keyframes live-breathe {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.4;
  }
}
</style>
