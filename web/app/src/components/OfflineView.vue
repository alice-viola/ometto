<script setup lang="ts">
import { onMounted, ref } from 'vue';
import Icon from './Icon.vue';
import { fmtDate, fmtDistance, fmtDuration } from '../lib/format';
import { applyQuery } from '../composables/usePlanner';
import {
  cancelDownload,
  downloadFor,
  downloadPercent,
  downloading,
  fmtBytes,
  forget,
  forgetAll,
  iosNotInstalled,
  loadSaved,
  online,
  savedLoaded,
  savedRoutes,
  usedBytes,
  deviceBytes,
} from '../composables/useOffline';
import type { SavedRoute } from '../lib/offline';
import { t } from '../i18n';
import { wayName } from '../i18n/service';

/**
 * What this browser can still answer with no network. A row opens its route
 * the same way a history row does — through `applyQuery` — and offline the
 * transport finds the saved answer waiting behind that same door.
 */
const emit = defineEmits<{ done: [] }>();

const used = ref(0);
const onDevice = ref<number | null>(null);

async function refresh() {
  used.value = await usedBytes();
  onDevice.value = await deviceBytes();
}

onMounted(async () => {
  await loadSaved();
  await refresh();
});

function open(rec: SavedRoute) {
  applyQuery(rec.query, true);
  emit('done');
}

/** A row is named by where you actually go; a via is shape, not an endpoint. */
function names(rec: SavedRoute): [string, string] {
  const pts = rec.query.points.filter((p) => !p.via);
  return [
    wayName(pts[0]?.name) || t('map.start'),
    wayName(pts[pts.length - 1]?.name) || t('map.destination'),
  ];
}

const best = (rec: SavedRoute) => rec.response.routes?.[0];

async function download(rec: SavedRoute) {
  await downloadFor(rec);
  await refresh();
}

async function remove(rec: SavedRoute) {
  await forget(rec);
  await refresh();
}

async function removeAll() {
  await forgetAll();
  await refresh();
}
</script>

<template>
  <section>
    <div v-if="!savedLoaded" class="grid gap-1.5">
      <div v-for="i in 2" :key="i" class="card h-[86px] animate-pulse bg-surface-2" />
    </div>

    <div v-else-if="!savedRoutes.length" class="pt-1">
      <p class="text-[13.5px] leading-relaxed text-muted">{{ t('offline.empty') }}</p>
    </div>

    <div v-else class="grid gap-1.5">
      <article
        v-for="rec in savedRoutes"
        :key="rec.key"
        class="card overflow-hidden transition-colors hover:border-line-strong"
      >
        <div class="flex items-stretch">
          <button class="min-w-0 flex-1 px-3 py-2.5 text-left" @click="open(rec)">
            <div class="flex items-center gap-1.5 text-[13.5px]">
              <span class="min-w-0 truncate font-medium">{{ names(rec)[0] }}</span>
              <Icon name="arrowRight" :size="13" class="shrink-0 text-faint" />
              <span class="min-w-0 truncate font-medium">{{ names(rec)[1] }}</span>
            </div>
            <div class="mt-1 flex items-center gap-2 text-[12px] text-muted">
              <Icon
                v-for="m in rec.query.mode.split('+')"
                :key="m"
                :name="m"
                :size="13"
                :style="{ color: `var(--${m})` }"
              />
              <span>{{ fmtDuration(best(rec)?.seconds ?? 0) }}</span>
              <span class="text-faint">·</span>
              <span>{{ fmtDistance(best(rec)?.meters ?? 0) }}</span>
              <Icon
                v-if="rec.query.lifts"
                name="lift"
                :size="13"
                :style="{ color: 'var(--lift)' }"
                :title="t('history.liftsAllowed')"
              />
              <span
                v-if="rec.query.mode.includes('hike')"
                class="font-semibold"
                :style="{ color: `var(--grade-${rec.query.grade.toLowerCase()})` }"
                :title="t('history.gradeTitle', { grade: rec.query.grade })"
              >
                {{ rec.query.grade }}
              </span>
              <span class="flex-1" />
              <span class="text-faint">{{ fmtDate(rec.savedAt) }}</span>
            </div>
          </button>
          <button
            class="btn-quiet shrink-0 px-2.5"
            :aria-label="t('offline.deleteAria', { from: names(rec)[0], to: names(rec)[1] })"
            :title="t('offline.delete')"
            @click="remove(rec)"
          >
            <Icon name="x" :size="14" />
          </button>
        </div>

        <!-- The map of the area, which is the slow half and can be missing. -->
        <div class="flex items-center gap-1.5 border-t border-line px-3 py-1.5 text-[11.5px]">
          <template v-if="downloading?.key === rec.key">
            <span class="text-muted">{{ t('offline.savingPercent', { n: downloadPercent }) }}</span>
            <span class="flex-1" />
            <button class="btn-quiet px-2 py-1 text-[11.5px]" @click="cancelDownload()">
              {{ t('fav.cancel') }}
            </button>
          </template>
          <template v-else-if="rec.tiles.done">
            <Icon name="check" :size="12" class="text-muted" />
            <span class="text-muted">{{ t('offline.map', { size: fmtBytes(rec.tiles.bytes) }) }}</span>
          </template>
          <template v-else>
            <Icon name="cloudOff" :size="12" class="text-faint" />
            <span class="text-faint">{{ t('offline.noMap') }}</span>
            <span class="flex-1" />
            <button
              class="btn-quiet flex items-center gap-1 px-2 py-1 text-[11.5px]"
              :disabled="!!downloading || !online"
              @click="download(rec)"
            >
              <Icon name="download" :size="12" /> {{ t('offline.downloadMap') }}
            </button>
          </template>
        </div>
        <div
          v-if="downloading?.key === rec.key"
          class="h-0.5"
          :style="{ width: `${downloadPercent}%`, background: 'var(--accent)' }"
          aria-hidden="true"
        />
      </article>

      <div class="mt-1 flex items-center gap-2">
        <p class="min-w-0 flex-1 text-[12px] text-faint">
          {{ t('offline.total', { n: savedRoutes.length, size: fmtBytes(used) })
          }}<template v-if="onDevice && onDevice > used + 1_000_000">
            · {{ t('offline.onDevice', { size: fmtBytes(onDevice) }) }}</template
          >
        </p>
        <button class="btn-quiet shrink-0 px-2 py-1.5 text-[12.5px]" @click="removeAll()">
          {{ t('history.deleteAll') }}
        </button>
      </div>
    </div>

    <!-- Worth saying once, and only where it is true: everywhere else the
         browser keeps what it is given. -->
    <p v-if="iosNotInstalled" class="mt-3 text-[11.5px] leading-snug text-faint">
      {{ t('offline.iosHint') }}
    </p>
  </section>
</template>
