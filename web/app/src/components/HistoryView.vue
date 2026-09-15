<script setup lang="ts">
import { onMounted } from 'vue';
import Icon from './Icon.vue';
import { fmtDate, fmtDistance, fmtDuration } from '../lib/format';
import { applyQuery } from '../composables/usePlanner';
import { clearHistory, history, historyError, historyLoaded, loadHistory, removeHistory } from '../composables/useHistory';
import type { HistoryEntry } from '../lib/types';

const emit = defineEmits<{ done: [] }>();

onMounted(() => {
  if (!historyLoaded.value) void loadHistory();
  else void loadHistory();
});

function replay(h: HistoryEntry) {
  applyQuery(
    {
      points: h.request.points,
      mode: h.request.mode,
      grade: h.request.grade,
      alternatives: h.request.alternatives ?? 1,
      lifts: !!h.request.lifts,
      avoid: h.request.avoid ?? [],
    },
    true,
  );
  emit('done');
}

const found = (h: HistoryEntry) => (h.summary?.meters ?? 0) > 0;

/** What the person asked for beats the name of the road it landed on. */
function names(h: HistoryEntry): [string, string] {
  // Vias are shape, not endpoints: a row is named by where you actually go.
  const pts = (h.request?.points ?? []).filter((p) => !p.via);
  const from = pts[0]?.name || h.summary?.fromName || 'Start';
  const to = pts[pts.length - 1]?.name || h.summary?.toName || 'Destination';
  return [from, to];
}
</script>

<template>
  <section>
    <div v-if="!historyLoaded" class="grid gap-1.5">
      <div v-for="i in 3" :key="i" class="card h-[64px] animate-pulse bg-surface-2" />
    </div>

    <p v-else-if="historyError" class="text-[13px] text-muted">{{ historyError }}</p>

    <div v-else-if="!history.length" class="pt-1">
      <p class="text-[13.5px] leading-relaxed text-muted">
        Routes you compute appear here, so you can pick up the same plan tomorrow.
      </p>
    </div>

    <div v-else class="grid gap-1.5">
      <article
        v-for="h in history"
        :key="h.id"
        class="card group flex items-stretch overflow-hidden transition-colors hover:border-line-strong"
      >
        <button class="min-w-0 flex-1 px-3 py-2.5 text-left" @click="replay(h)">
          <div class="flex items-center gap-1.5 text-[13.5px]">
            <span class="min-w-0 truncate font-medium">{{ names(h)[0] }}</span>
            <Icon name="arrowRight" :size="13" class="shrink-0 text-faint" />
            <span class="min-w-0 truncate font-medium">{{ names(h)[1] }}</span>
          </div>
          <div class="mt-1 flex items-center gap-2 text-[12px] text-muted">
            <Icon
              v-for="m in (h.request?.mode ?? 'car').split('+')"
              :key="m"
              :name="m"
              :size="13"
              :style="{ color: `var(--${m})` }"
            />
            <!-- A question that came back empty is still worth keeping, but it
                 must not wear the clothes of an answer. -->
            <span v-if="!found(h)" class="text-faint">No route</span>
            <template v-else>
              <span>{{ fmtDuration(h.summary?.seconds ?? 0) }}</span>
              <span class="text-faint">·</span>
              <span>{{ fmtDistance(h.summary?.meters ?? 0) }}</span>
            </template>
            <!-- Two walks to the same summit differ by the grade that was
                 asked for, and by nothing else on this row. -->
            <Icon
              v-if="h.request?.lifts"
              name="lift"
              :size="13"
              :style="{ color: 'var(--lift)' }"
              title="Lifts allowed"
            />
            <span
              v-if="(h.request?.mode ?? '').includes('hike') && h.request?.grade"
              class="font-semibold"
              :style="{ color: `var(--grade-${h.request.grade.toLowerCase()})` }"
              :title="`Trail grade ${h.request.grade}`"
            >
              {{ h.request.grade }}
            </span>
            <span class="flex-1" />
            <span class="text-faint">{{ fmtDate(h.at) }}</span>
          </div>
        </button>
        <button
          class="btn-quiet shrink-0 px-2.5"
          :aria-label="`Delete ${names(h)[0]} to ${names(h)[1]}`"
          @click="removeHistory(h.id)"
        >
          <Icon name="x" :size="14" />
        </button>
      </article>

      <button class="btn-quiet mt-1 justify-self-start px-2 py-1.5 text-[12.5px]" @click="clearHistory()">
        Delete all
      </button>
    </div>
  </section>
</template>
