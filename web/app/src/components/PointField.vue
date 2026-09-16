<script setup lang="ts">
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import Icon from './Icon.vue';
import { api } from '../lib/api';
import { fmtElevation, fmtOffset, kindLabel } from '../lib/format';
import type { GeocodeResult, Waypoint } from '../lib/types';
import { favourites } from '../composables/useFavourites';
import type { PointNote } from '../composables/usePlanner';
import { online } from '../composables/useOffline';
import { toast } from '../composables/useToast';
import { locale, t } from '../i18n';
import { detailText, wayName } from '../i18n/service';

const props = defineProps<{
  modelValue: Waypoint | null;
  role: 'start' | 'stop' | 'destination' | 'parking';
  index: number;
  stopNumber?: number;
  canRemove: boolean;
  canMoveUp?: boolean;
  canMoveDown?: boolean;
  note?: PointNote | null;
}>();
const emit = defineEmits<{
  'update:modelValue': [Waypoint | null];
  remove: [];
  move: [number];
  locate: [Waypoint];
  acceptMove: [];
  dismissNote: [];
}>();

const text = ref(wayName(props.modelValue?.name));
const open = ref(false);
const results = ref<GeocodeResult[]>([]);
const active = ref(-1);
const loading = ref(false);
const locating = ref(false);
const input = ref<HTMLInputElement | null>(null);
const root = ref<HTMLElement | null>(null);
const listEl = ref<HTMLElement | null>(null);
const listId = `pf-list-${Math.random().toString(36).slice(2, 8)}`;

/**
 * Closing the list has to happen before the browser hit-tests the click that
 * closed it. An open list sits over whatever is below the field, so leaving it
 * up for one more frame is how a click meant for the next box was swallowed
 * and the keystrokes that followed went on appending to this one.
 */
function hideListNow() {
  open.value = false;
  active.value = -1;
  if (listEl.value) listEl.value.style.display = 'none';
}

function showList() {
  open.value = true;
  if (listEl.value) listEl.value.style.display = '';
}

function onDocumentPointerDown(e: Event) {
  if (!open.value) return;
  const r = root.value;
  if (r && e.target instanceof Node && r.contains(e.target)) return;
  hideListNow();
}

onMounted(() => document.addEventListener('pointerdown', onDocumentPointerDown, true));

let timer: number | undefined;
let ctrl: AbortController | null = null;

// The field shows the name in the page's language, so a switch of language
// is a reason to say it again — unless the person is typing in it.
watch([() => props.modelValue, locale], ([v]) => {
  if (document.activeElement !== input.value) text.value = wayName(v?.name);
});

const placeholder = computed(() =>
  props.role === 'start'
    ? t('point.from')
    : props.role === 'destination'
      ? t('point.to')
      : props.role === 'parking'
        ? t('point.park')
        : t('point.stop', { n: props.stopNumber ?? '' }).trim(),
);

/** A whole sentence, so the two buttons under it have something to answer. */
const movedText = computed(() => {
  const n = props.note;
  if (!n) return '';
  const d = fmtOffset(n.metres);
  if (n.kind === 'far') return t('point.far', { d });
  const key = props.role === 'start' ? 'point.starts' : props.role === 'destination' ? 'point.ends' : 'point.passes';
  const where = n.name ? t('point.onWay', { name: wayName(n.name) }) : '';
  return t(key, { d, where });
});

const suggestions = computed(() => {
  if (text.value.trim().length >= 2) return results.value;
  return favourites.value.slice(0, 5).map<GeocodeResult>((f) => ({
    id: `fav-${f.id}`,
    name: f.name,
    kind: (f.kind as GeocodeResult['kind']) ?? 'place',
    locality: t('point.favourite'),
    lat: f.lat,
    lon: f.lon,
  }));
});

const showLocate = computed(() => text.value.trim().length < 2);
const optionCount = computed(() => suggestions.value.length + (showLocate.value ? 1 : 0));
/** Typed something, got nothing, and there is no network: say which it is. */
const offlineHint = computed(
  () => !online.value && !loading.value && !suggestions.value.length && text.value.trim().length >= 2,
);

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

function onInput() {
  showList();
  active.value = -1;
  search(text.value);
  if (!text.value.trim() && props.modelValue) emit('update:modelValue', null);
}

function pick(r: GeocodeResult) {
  text.value = wayName(r.name);
  hideListNow();
  emit('update:modelValue', { lat: r.lat, lon: r.lon, name: r.name, kind: r.kind });
}

function clear() {
  text.value = '';
  results.value = [];
  emit('update:modelValue', null);
  nextTick(() => input.value?.focus());
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
      text.value = wayName(name);
      hideListNow();
      locating.value = false;
      emit('update:modelValue', { lat, lon, name });
    },
    () => {
      locating.value = false;
      toast(t('point.locationUnavailable'));
    },
    { enableHighAccuracy: false, timeout: 8000, maximumAge: 60000 },
  );
}

// Some browsers and automation harnesses still emit the pre-standard names.
const DOWN = ['ArrowDown', 'Down'];
const UP = ['ArrowUp', 'Up'];
const ESC = ['Escape', 'Esc'];

function onKeydown(e: KeyboardEvent) {
  if (DOWN.includes(e.key) || UP.includes(e.key)) {
    e.preventDefault();
    if (!open.value) showList();
    const n = optionCount.value;
    if (!n) return;
    const down = DOWN.includes(e.key);
    // From nothing, Down takes the first option and Up the last; the old
    // arithmetic skipped straight past the first one.
    active.value = active.value < 0 ? (down ? 0 : n - 1) : (active.value + (down ? 1 : -1) + n) % n;
    document.getElementById(`${listId}-${active.value}`)?.scrollIntoView({ block: 'nearest' });
  } else if (e.key === 'Enter') {
    if (!open.value) return;
    // With nothing chosen, Enter takes the first match: on a phone the
    // keyboard's Go key is the only way to commit without leaving the keys.
    // Favourites, shown before anything is typed, are never taken unasked.
    const searched = text.value.trim().length >= 2 && suggestions.value.length > 0;
    const at = active.value >= 0 ? active.value : searched ? 0 : -1;
    if (at < 0) return;
    e.preventDefault();
    const locateIndex = suggestions.value.length;
    if (showLocate.value && at === locateIndex) useMyLocation();
    else pick(suggestions.value[at]);
  } else if (ESC.includes(e.key)) {
    if (open.value) {
      e.stopPropagation();
      e.preventDefault(); // the panel must not fold away on this Escape
      hideListNow();
    }
  } else if (e.key === 'Tab') {
    hideListNow();
  }
}

function onBlur() {
  // Picking an option keeps focus (its mousedown is prevented), so a blur
  // always means the person left: close, and put back what this field holds.
  hideListNow();
  if (!text.value.trim()) emit('update:modelValue', null);
  else if (props.modelValue) text.value = props.modelValue.name != null ? wayName(props.modelValue.name) : text.value;
}

onBeforeUnmount(() => {
  clearTimeout(timer);
  ctrl?.abort();
  document.removeEventListener('pointerdown', onDocumentPointerDown, true);
});
</script>

<template>
  <!-- `min-w-0`: as a grid item this box would otherwise refuse to be narrower
       than its widest nowrap line — one long locality in the open list, and
       the whole row grew past a phone's edge. -->
  <div ref="root" class="relative min-w-0">
    <div class="field flex items-center gap-2 pl-2.5 pr-1">
      <span class="grid size-5 shrink-0 place-items-center" aria-hidden="true">
        <svg v-if="role === 'start'" width="14" height="14" viewBox="0 0 14 14">
          <circle cx="7" cy="7" r="5" fill="none" stroke="var(--ink)" stroke-width="2.4" />
        </svg>
        <svg v-else-if="role === 'destination'" width="13" height="16" viewBox="0 0 13 16">
          <path
            d="M6.5 15.5S12 9.4 12 6.1A5.5 5.5 0 1 0 1 6.1C1 9.4 6.5 15.5 6.5 15.5z"
            fill="var(--dest)"
          />
          <circle cx="6.5" cy="6.1" r="2" fill="var(--surface)" />
        </svg>
        <svg v-else-if="role === 'parking'" width="14" height="14" viewBox="0 0 14 14">
          <rect x="1.2" y="1.2" width="11.6" height="11.6" rx="3" fill="none" stroke="var(--muted)" stroke-width="1.6" />
          <text x="7" y="10" text-anchor="middle" font-size="8" font-weight="700" fill="var(--muted)">P</text>
        </svg>
        <svg v-else width="14" height="14" viewBox="0 0 14 14">
          <circle cx="7" cy="7" r="5.2" fill="none" stroke="var(--muted)" stroke-width="1.6" />
          <text x="7" y="9.6" text-anchor="middle" font-size="7" font-weight="600" fill="var(--muted)">
            {{ stopNumber }}
          </text>
        </svg>
      </span>

      <input
        ref="input"
        v-model="text"
        type="text"
        class="h-10 min-w-0 flex-1 bg-transparent text-[14px] outline-none placeholder:text-faint"
        :placeholder="placeholder"
        :aria-label="placeholder"
        role="combobox"
        :aria-expanded="open"
        :aria-controls="listId"
        aria-autocomplete="list"
        :aria-activedescendant="active >= 0 ? `${listId}-${active}` : undefined"
        autocomplete="off"
        autocapitalize="words"
        autocorrect="off"
        enterkeyhint="go"
        spellcheck="false"
        @input="onInput"
        @focus="showList"
        @blur="onBlur"
        @keydown="onKeydown"
      />

      <button
        v-if="text"
        class="btn-quiet p-1.5"
        :aria-label="t('point.clearField', { label: placeholder })"
        @mousedown.prevent
        @click="clear"
      >
        <Icon name="x" :size="14" />
      </button>
      <button
        v-if="canMoveUp"
        class="btn-quiet p-1.5"
        :aria-label="t('point.moveEarlier')"
        @mousedown.prevent
        @click="emit('move', -1)"
      >
        <Icon name="chevronUp" :size="14" />
      </button>
      <button
        v-if="canMoveDown"
        class="btn-quiet p-1.5"
        :aria-label="t('point.moveLater')"
        @mousedown.prevent
        @click="emit('move', 1)"
      >
        <Icon name="chevronDown" :size="14" />
      </button>
      <button
        v-if="canRemove"
        class="btn-quiet p-1.5"
        :aria-label="t('point.remove')"
        @mousedown.prevent
        @click="emit('remove')"
      >
        <Icon name="trash" :size="14" />
      </button>
    </div>

    <!-- On a phone the sentence takes its own line and the two answers sit
         under it as buttons a thumb can hit. -->
    <p
      v-if="note"
      class="mt-1 flex flex-wrap items-baseline gap-x-2 gap-y-0.5 pl-[30px] text-[11.5px] leading-snug max-[899px]:items-center max-[899px]:gap-x-2.5 max-[899px]:gap-y-1.5"
      :style="{ color: note.kind === 'far' ? 'var(--dest)' : 'var(--muted)' }"
    >
      <span class="max-[899px]:basis-full">{{ movedText }}</span>
      <template v-if="note.kind === 'moved'">
        <button
          class="note-action"
          :title="t('point.moveMarkerTitle')"
          :aria-label="t('point.moveMarkerAria')"
          @click="emit('acceptMove')"
        >
          {{ t('point.moveMarker') }}
        </button>
        <button
          class="note-action"
          :title="t('point.keepTitle')"
          :aria-label="t('point.keepTitle')"
          @click="emit('dismissNote')"
        >
          {{ t('point.keep') }}
        </button>
      </template>
    </p>

    <!--
      In flow, not floating. A list that hangs over the next field turns a
      click meant for that field into a selection in this one: the pointer is
      inside the list, so no amount of outside-click handling can save it.
      Pushing the rest of the panel down costs a reflow and buys the rule that
      a click always lands where it was aimed.
    -->
    <ul
      v-show="open && (optionCount > 0 || loading || offlineHint)"
      ref="listEl"
      :id="listId"
      role="listbox"
      class="card scroll-quiet mt-1 max-h-[260px] overflow-y-auto py-1"
      style="box-shadow: var(--shadow-1)"
    >
      <li v-if="loading && !suggestions.length" class="px-3 py-2 text-[13px] text-muted">{{ t('point.searching') }}</li>
      <li v-else-if="offlineHint" class="px-3 py-2 text-[13px] text-muted">{{ t('offline.searchNeedsNetwork') }}</li>
      <li
        v-for="(r, i) in suggestions"
        :id="`${listId}-${i}`"
        :key="r.id"
        role="option"
        :aria-selected="active === i"
        class="cursor-pointer px-3 py-1.5"
        :class="active === i ? 'bg-surface-3' : ''"
        @mousedown.prevent="pick(r)"
        @mousemove="active = i"
      >
        <div class="flex items-baseline gap-2">
          <Icon
            :name="r.kind === 'peak' ? 'peak' : r.kind === 'hut' ? 'hut' : r.kind === 'pass' ? 'pass' : r.kind === 'crag' ? 'crag' : r.kind === 'street' ? 'street' : r.kind === 'trail' ? 'trail' : 'place'"
            :size="14"
            class="relative top-0.5 text-muted"
          />
          <span class="min-w-0 flex-1 truncate text-[13.5px]">{{ wayName(r.name) }}</span>
          <span v-if="r.ele" class="text-[12px] text-muted">{{ fmtElevation(r.ele) }}</span>
        </div>
        <div class="ml-[22px] truncate text-[11.5px] text-faint">
          {{ kindLabel(r.kind) }}<template v-if="r.detail"> · {{ detailText(r.detail) }}</template><template v-if="r.locality"> · {{ r.locality }}</template>
        </div>
      </li>
      <li
        v-if="showLocate"
        :id="`${listId}-${suggestions.length}`"
        role="option"
        :aria-selected="active === suggestions.length"
        class="mt-1 cursor-pointer border-t border-line px-3 pt-2 pb-1.5 max-[899px]:py-3"
        :class="active === suggestions.length ? 'bg-surface-3' : ''"
        @mousedown.prevent="useMyLocation"
        @mousemove="active = suggestions.length"
      >
        <div class="flex items-center gap-2 text-[13px]">
          <Icon name="locate" :size="14" class="text-muted" />
          {{ locating ? t('point.findingYou') : t('point.useMyLocation') }}
        </div>
      </li>
    </ul>
  </div>
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
/* Under a thumb the two answers are buttons, and look it. */
@media (max-width: 899px) {
  .note-action {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 14px;
    border: 1px solid var(--line);
    border-radius: var(--radius-control);
    background: var(--surface-2);
    color: var(--ink);
    font-size: 13px;
    text-decoration: none;
  }
}
</style>
