<script setup lang="ts">
import { computed } from 'vue';
import Icon from './Icon.vue';
import LiveCard from './LiveCard.vue';
import ResultCard from './ResultCard.vue';
import EmptyState from './EmptyState.vue';
import {
  compute,
  degraded,
  errorText,
  grade,
  hasResult,
  mode,
  neededGrade,
  noRouteReason,
  pointNotes,
  routes,
  savedCopyAt,
  selectRoute,
  selectedId,
  slots,
  status,
} from '../composables/usePlanner';
import { fmtDate, gradeName, sentence } from '../lib/format';
import { t } from '../i18n';

const best = computed(() => (routes.value.length > 1 ? routes.value[0].seconds : null));
const walks = computed(() => mode.value.includes('hike'));

/** A point the service could not reach the network from, if there is one. */
const farPoint = computed(() => {
  const entry = Object.entries(pointNotes.value).find(([, n]) => n.kind === 'far');
  if (!entry) return null;
  const i = Number(entry[0]);
  const slot = slots.value[i];
  return { name: slot?.point?.name };
});

// Whatever the service says, verbatim but capitalised. A hut a trail reaches
// is not unreachable, and the UI must not keep asserting otherwise.
const headline = computed(
  () => sentence(noRouteReason.value) || t('results.noRouteGrade', { grade: grade.value }),
);

/** The service says which grade would answer it; never guess one. */
function allowNeeded() {
  const g = neededGrade.value;
  if (!g) return;
  grade.value = g;
  void compute();
}
</script>

<template>
  <section :aria-label="t('results.title')" aria-live="polite">
    <!-- On the way: what is left of the chosen route, above the choice. -->
    <LiveCard />

    <!-- first computation -->
    <div v-if="status === 'loading' && !hasResult" class="grid gap-1.5" :aria-label="t('results.computing')">
      <div v-for="i in 2" :key="i" class="card px-3 py-3">
        <div class="skeleton h-4 w-24 rounded" />
        <div class="skeleton mt-2.5 h-3 w-40 rounded" />
        <div class="skeleton mt-1.5 h-2.5 w-32 rounded" />
      </div>
      <p class="mt-0.5 text-center text-[12px] text-faint">{{ t('results.working') }}</p>
    </div>

    <!-- failure -->
    <div v-else-if="status === 'error'" class="card px-3 py-3">
      <div class="flex items-start gap-2">
        <Icon name="warning" :size="15" class="relative top-0.5 shrink-0 text-muted" />
        <div class="min-w-0 flex-1">
          <p class="text-[13.5px] font-medium">{{ errorText }}</p>
          <button class="btn-quiet mt-2 px-2 py-1.5 text-[12.5px]" @click="compute()">{{ t('results.tryAgain') }}</button>
        </div>
      </div>
    </div>

    <!-- nothing found -->
    <div v-else-if="status === 'noroute'" class="card px-3 py-3">
      <p class="text-[13.5px] font-medium">{{ headline }}</p>

      <p class="mt-1 text-[12.5px] leading-snug text-muted">
        <template v-if="neededGrade === 'A'">
          {{ t('results.alpineOnly') }}
        </template>
        <template v-else-if="neededGrade">
          {{ t('results.harderGrade') }}
        </template>
        <template v-else-if="farPoint">
          {{ t('results.moveNearer') }}
        </template>
        <template v-else-if="walks">
          {{ t('results.tryTrail') }}
        </template>
        <template v-else>{{ t('results.tryRoad') }}</template>
      </p>

      <!-- Accepting alpine ground is not the same kind of click as accepting
           a harder marked path, and must not look like one. -->
      <button
        v-if="neededGrade"
        class="mt-2.5 flex items-center gap-1.5 px-3 py-2 text-[12.5px]"
        :class="neededGrade === 'A' ? 'btn-alert' : 'btn-primary'"
        :aria-label="t('results.allowAria', { grade: gradeName(neededGrade) })"
        @click="allowNeeded"
      >
        <Icon v-if="neededGrade === 'A'" name="warning" :size="13" />
        {{ t('results.allow', { grade: neededGrade === 'A' ? gradeName('A') : neededGrade }) }}
      </button>
    </div>

    <!-- results -->
    <div v-else-if="hasResult" class="grid gap-1.5">
      <div v-if="status === 'recomputing'" class="flex items-center gap-1.5 pb-0.5 text-[12px] text-muted">
        <span class="pulse-dot" /> {{ t('results.recomputing') }}
      </div>
      <p v-if="degraded" class="pb-0.5 text-[11.5px] leading-snug text-faint">
        {{ t('results.degraded') }}
      </p>
      <!-- Not an error and not a warning: the answer is the one that was
           given, and this says when. -->
      <p v-if="savedCopyAt" class="flex items-center gap-1.5 pb-0.5 text-[11.5px] leading-snug text-faint">
        <Icon name="cloudOff" :size="12" />
        {{ t('offline.savedCopy', { date: fmtDate(savedCopyAt) }) }}
      </p>
      <ResultCard
        v-for="(r, i) in routes"
        :key="r.id"
        :route="r"
        :index="i"
        :best="best"
        :selected="r.id === (selectedId ?? routes[0].id)"
        @select="selectRoute(r.id)"
      />
    </div>

    <!-- nothing asked yet -->
    <EmptyState v-else />
  </section>
</template>

<style scoped>
.skeleton {
  background: var(--surface-3);
  animation: breathe 1.5s ease-in-out infinite;
}
@keyframes breathe {
  0%,
  100% {
    opacity: 1;
  }
  50% {
    opacity: 0.45;
  }
}
.pulse-dot {
  width: 6px;
  height: 6px;
  border-radius: 999px;
  background: var(--accent);
  animation: breathe 1.1s ease-in-out infinite;
}
</style>
