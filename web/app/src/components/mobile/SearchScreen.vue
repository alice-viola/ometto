<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Icon from '../Icon.vue';
import { api } from '../../lib/api';
import { fmtElevation, kindLabel } from '../../lib/format';
import type { GeocodeResult } from '../../lib/types';
import { favourites } from '../../composables/useFavourites';
import { history, historyLoaded, loadHistory } from '../../composables/useHistory';
import { canCompute, compute, parkingIndex, placeIndices, setPoint, slots } from '../../composables/usePlanner';
import { online } from '../../composables/useOffline';
import { toast } from '../../composables/useToast';
import { t } from '../../i18n';
import { detailText, wayName } from '../../i18n/service';

/**
 * Typing a place takes the whole screen, the way it does in every map app:
 * the field at the top with the keyboard under it, and a list that is the
 * favourites and the recent places until two letters have been typed, then
 * the matches. Picking one returns to the map with the point set.
 */
const props = defineProps<{ index: number }>();
const emit = defineEmits<{ close: [] }>();

const at = computed(() => placeIndices.value.indexOf(props.index));
const placeholder = computed(() =>
  at.value === 0
    ? t('point.from')
    : at.value === placeIndices.value.length - 1
      ? t('point.to')
      : props.index === parkingIndex.value
        ? t('point.park')
        : t('point.stop', { n: at.value }),
);

const text = ref(wayName(slots.value[props.index]?.point?.name));
const results = ref<GeocodeResult[]>([]);
const loading = ref(false);
const locating = ref(false);
const input = ref<HTMLInputElement | null>(null);

let timer: number | undefined;
let ctrl: AbortController | null = null;

const searched = computed(() => text.value.trim().length >= 2);

function search(q: string) {
  clearTimeout(timer);
  ctrl?.abort();
  if (q.trim().length < 2) {
    results.value = [];
    loading.value = false;
    return;
  }
  loading.value = true;
  timer = window.setTimeout(async () => {
    ctrl = new AbortController();
    try {
      results.value = await api.geocode(q.trim(), ctrl.signal);
    } catch (e) {
      if ((e as Error)?.name !== 'AbortError') results.value = [];
    } finally {
      loading.value = false;
    }
  }, 200);
}
watch(text, search);

/** With nothing typed: the favourites first, then the places recently gone to. */
const suggestions = computed<GeocodeResult[]>(() => {
  if (searched.value) return results.value;
  const out: GeocodeResult[] = [];
  // The same place, once: a name that was searched twice, or a marker that was
  // moved a few metres, must not fill the list with itself.
  const seen = new Set<string>();
  const fresh = (name: string, lat: number, lon: number) => {
    const keys = [name.trim().toLowerCase(), `${lat.toFixed(4)},${lon.toFixed(4)}`];
    if (keys.some((k) => seen.has(k))) return false;
    keys.forEach((k) => seen.add(k));
    return true;
  };
  for (const f of favourites.value) {
    if (!fresh(f.name, f.lat, f.lon)) continue;
    out.push({
      id: `fav-${f.id}`,
      name: f.name,
      kind: (f.kind ?? 'place') as GeocodeResult['kind'],
      locality: t('point.favourite'),
      lat: f.lat,
      lon: f.lon,
    });
  }
  for (const h of history.value) {
    for (const p of (h.request?.points ?? []).filter((p) => !p.via && p.name)) {
      if (!fresh(p.name!, p.lat, p.lon)) continue;
      out.push({
        id: `rec-${p.lat.toFixed(4)},${p.lon.toFixed(4)}`,
        name: p.name!,
        kind: (p.kind ?? 'place') as GeocodeResult['kind'],
        locality: t('point.recent'),
        lat: p.lat,
        lon: p.lon,
      });
    }
  }
  return out.slice(0, 12);
});

/** A place chosen here is a question asked: answered, framed and remembered. */
function ask() {
  if (canCompute.value) void compute();
}

function pick(r: GeocodeResult) {
  setPoint(props.index, { lat: r.lat, lon: r.lon, name: r.name, kind: r.kind });
  ask();
  emit('close');
}

function clearField() {
  text.value = '';
  results.value = [];
  setPoint(props.index, null);
  input.value?.focus();
}

function useMyLocation() {
  if (!navigator.geolocation) {
    toast(t('point.noGeolocation'));
    return;
  }
  locating.value = true;
  navigator.geolocation.getCurrentPosition(
    async (pos) => {
      const { latitude: lat, longitude: lon } = pos.coords;
      let name = t('point.myLocation');
      try {
        name = (await api.reverse(lat, lon)).name || name;
      } catch {
        /* the coordinate is enough */
      }
      locating.value = false;
      setPoint(props.index, { lat, lon, name });
      ask();
      emit('close');
    },
    () => {
      locating.value = false;
      toast(t('point.locationUnavailable'));
    },
    { enableHighAccuracy: false, timeout: 8000, maximumAge: 60000 },
  );
}

function onKeydown(e: KeyboardEvent) {
  if (e.key === 'Enter') {
    // The keyboard's Go key takes the first match: nothing else is reachable
    // without leaving the keys.
    if (searched.value && results.value.length) {
      e.preventDefault();
      pick(results.value[0]);
    }
  } else if (e.key === 'Escape' || e.key === 'Esc') {
    e.preventDefault();
    emit('close');
  }
}

const iconOf = (k: string) =>
  k === 'peak' ? 'peak' : k === 'hut' ? 'hut' : k === 'pass' ? 'pass' : k === 'crag' ? 'crag' : k === 'street' ? 'street' : k === 'trail' ? 'trail' : 'place';

onMounted(() => {
  input.value?.focus();
  input.value?.select();
  if (!historyLoaded.value) void loadHistory();
});
onBeforeUnmount(() => {
  clearTimeout(timer);
  ctrl?.abort();
});
</script>

<template>
  <div class="fixed inset-0 z-50 flex flex-col bg-surface" role="dialog" :aria-label="t('screen.choose', { label: placeholder })">
    <div
      class="flex items-center gap-1 border-b border-line px-2 pb-2"
      :style="{ paddingTop: 'calc(env(safe-area-inset-top, 0px) + 8px)' }"
    >
      <button type="button" class="ctl" :aria-label="t('menu.backToMap')" @click="emit('close')">
        <Icon name="arrowLeft" :size="19" />
      </button>
      <div class="field flex min-w-0 flex-1 items-center gap-2 pl-3 pr-1">
        <Icon name="search" :size="16" class="shrink-0 text-muted" />
        <input
          ref="input"
          v-model="text"
          type="text"
          class="h-11 min-w-0 flex-1 bg-transparent text-[16px] outline-none placeholder:text-faint"
          :placeholder="placeholder"
          :aria-label="placeholder"
          autocomplete="off"
          autocapitalize="words"
          autocorrect="off"
          enterkeyhint="go"
          spellcheck="false"
          @keydown="onKeydown"
        />
        <button v-if="text" type="button" class="ctl" :aria-label="t('point.clearField', { label: placeholder })" @click="clearField">
          <Icon name="x" :size="15" />
        </button>
      </div>
    </div>

    <ul class="scroll-quiet min-h-0 flex-1 overflow-y-auto overscroll-contain pb-8" role="listbox">
      <li v-if="!searched" class="row" role="option" @click="useMyLocation">
        <Icon name="locate" :size="17" class="shrink-0 text-muted" />
        <span class="text-[15px]">{{ locating ? t('point.findingYou') : t('point.useMyLocation') }}</span>
      </li>
      <li v-if="!searched" class="row" role="option" @click="emit('close')">
        <Icon name="place" :size="17" class="shrink-0 text-muted" />
        <span class="text-[15px]">{{ t('screen.chooseOnMap') }}</span>
      </li>
      <li v-if="!searched && suggestions.length" class="label px-4 pt-3 pb-1">{{ t('screen.favAndRecent') }}</li>

      <li v-if="loading && !suggestions.length" class="px-4 py-3 text-[14px] text-muted">{{ t('point.searching') }}</li>
      <li v-else-if="searched && !loading && !suggestions.length" class="px-4 py-3 text-[14px] text-muted">
        {{ online ? t('screen.nothingFound') : t('offline.searchNeedsNetwork') }}
      </li>

      <li v-for="r in suggestions" :key="r.id" class="row" role="option" @click="pick(r)">
        <Icon :name="iconOf(r.kind)" :size="17" class="shrink-0 text-muted" />
        <span class="min-w-0 flex-1">
          <span class="flex items-baseline gap-2">
            <span class="min-w-0 flex-1 truncate text-[15px]">{{ wayName(r.name) }}</span>
            <span v-if="r.ele" class="shrink-0 text-[12.5px] text-muted">{{ fmtElevation(r.ele) }}</span>
          </span>
          <span class="block truncate text-[12.5px] text-faint">
            {{ kindLabel(r.kind) }}<template v-if="r.detail"> · {{ detailText(r.detail) }}</template><template v-if="r.locality"> · {{ r.locality }}</template>
          </span>
        </span>
      </li>
    </ul>
  </div>
</template>

<style scoped>
.ctl {
  display: grid;
  flex: 0 0 auto;
  place-items: center;
  width: 44px;
  height: 44px;
  border-radius: var(--radius-control);
  color: var(--muted);
}
.ctl:active {
  background: var(--surface-3);
  color: var(--ink);
}
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  min-height: 52px;
  padding: 6px 16px;
  cursor: pointer;
}
.row:active {
  background: var(--surface-2);
}
.row + .row {
  border-top: 1px solid var(--line);
}
</style>
