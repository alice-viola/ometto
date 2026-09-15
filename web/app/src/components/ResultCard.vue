<script setup lang="ts">
import { computed, nextTick, ref } from 'vue';
import Icon from './Icon.vue';
import ElevationProfile from './ElevationProfile.vue';
import LegList from './LegList.vue';
import StepList from './StepList.vue';
import {
  composition,
  fmtAscent,
  fmtDescent,
  fmtDistance,
  fmtDuration,
  isAlpineWarning,
  isSeasonalWarning,
  mergeClasses,
  sentence,
} from '../lib/format';
import type { RouteAlternative } from '../lib/types';
import { avoids, filledPoints, grade, lifts, mode, shareQuery } from '../composables/usePlanner';
import { isCompact } from '../composables/useMedia';
import { shareUrl } from '../lib/url';
import { addFavourite } from '../composables/useFavourites';
import { toast } from '../composables/useToast';

const props = defineProps<{
  route: RouteAlternative;
  selected: boolean;
  best: number | null;
  index: number;
}>();
const emit = defineEmits<{ select: [] }>();

const modes = computed(() => {
  const seen: string[] = [];
  for (const l of props.route.legs ?? []) if (!seen.includes(l.mode)) seen.push(l.mode);
  return seen.length ? seen : ['car'];
});

const comp = computed(() => composition(mergeClasses((props.route.legs ?? []).map((l) => l.classes))));

/**
 * On a car+hike trip the climb over the passes is not climb the walker does.
 * The headline reports the walk, and the drive is stated as its own line.
 */
const walk = computed(() => {
  const r = props.route;
  const legs = (r.legs ?? []).filter((l) => l.mode === 'hike');
  if (!legs.length) return null;
  return {
    meters: r.walkMeters ?? legs.reduce((a, l) => a + l.meters, 0),
    ascent: r.walkAscent ?? legs.reduce((a, l) => a + l.ascent, 0),
    descent: legs.reduce((a, l) => a + l.descent, 0),
    seconds: legs.reduce((a, l) => a + l.seconds, 0),
  };
});

const wheels = computed(() => {
  const legs = (props.route.legs ?? []).filter((l) => l.mode === 'car' || l.mode === 'bike');
  if (!legs.length) return null;
  return {
    mode: legs[0].mode,
    meters: legs.reduce((a, l) => a + l.meters, 0),
    ascent: legs.reduce((a, l) => a + l.ascent, 0),
    seconds: legs.reduce((a, l) => a + l.seconds, 0),
  };
});

/** A lift is ridden. Its climb belongs to the route, never to the walker. */
const ride = computed(() => {
  const legs = (props.route.legs ?? []).filter((l) => l.mode === 'lift');
  if (!legs.length) return null;
  return {
    seconds: legs.reduce((a, l) => a + l.seconds, 0),
    meters: legs.reduce((a, l) => a + l.meters, 0),
    ascent: legs.reduce((a, l) => a + l.ascent, 0),
    names: [...new Set(legs.map((l) => l.name).filter(Boolean))] as string[],
  };
});

/** More than one way of travelling in one answer: keep the numbers apart. */
const mixed = computed(() => !!walk.value && (!!wheels.value || !!ride.value));
const headAscent = computed(() => (mixed.value ? walk.value!.ascent : props.route.ascent));
const headDescent = computed(() => (mixed.value ? walk.value!.descent : props.route.descent));

/** The journey said in the order it happens: drive, ride, then walk. */
const journey = computed(() => {
  const out: string[] = [];
  const seen = new Set<string>();
  for (const l of props.route.legs ?? []) {
    const kind = l.mode === 'hike' ? 'walk' : l.mode === 'lift' ? 'ride' : 'wheels';
    if (seen.has(kind)) continue;
    seen.add(kind);
    if (kind === 'wheels' && wheels.value) {
      const by = wheels.value.mode === 'bike' ? 'bike' : 'car';
      out.push(
        `${fmtDuration(wheels.value.seconds)} by ${by} (${fmtDistance(wheels.value.meters)}, ${fmtAscent(wheels.value.ascent)})`,
      );
    } else if (kind === 'ride' && ride.value) {
      out.push(`${fmtDuration(ride.value.seconds)} by lift (${fmtAscent(ride.value.ascent)})`);
    } else if (kind === 'walk' && walk.value) {
      out.push(`${fmtDuration(walk.value.seconds)} on foot (${fmtDistance(walk.value.meters)})`);
    }
  }
  return out;
});

const alpineWarning = computed(() => (props.route.warnings ?? []).find(isAlpineWarning) ?? '');

/** Everything the alpine block and the parking line have already said. */
const otherWarnings = computed(() =>
  (props.route.warnings ?? []).filter((w) => !isAlpineWarning(w)),
);

const delta = computed(() => {
  if (props.best === null || props.index === 0) return '';
  const d = Math.round((props.route.seconds - props.best) / 60);
  if (d <= 0) return '';
  return `+${d < 60 ? `${d} min` : fmtDuration(d * 60)}`;
});

const isParking = (w: string) => /^parked\b/i.test(w.trim());

/**
 * Where the car was left. The service usually says so in a warning; when it
 * only sends the `parking` object — the mid-leg case, where no stop wears a P
 * badge — the card says it instead. Never both.
 */
const parkingNote = computed(() => {
  const p = props.route.parking;
  if (!p) return '';
  // Nothing was parked unless something was ridden or driven. The contract
  // says `parking` is absent on single-mode plans; when it arrives anyway,
  // saying "parked" on a pure walk would be plainly wrong.
  if (!props.route.legs.some((l) => l.mode === 'car' || l.mode === 'bike')) return '';
  if ((props.route.warnings ?? []).some(isParking)) return '';
  return `Parked at ${p.name || 'the trailhead'}`;
});

const naming = ref(false);
const favName = ref('');
const favInput = ref<HTMLInputElement | null>(null);

const shareLink = ref('');
const linkInput = ref<HTMLInputElement | null>(null);

async function showLink(url: string) {
  // Last resort, but a usable one: the link itself, selected and ready to copy.
  shareLink.value = url;
  await nextTick();
  linkInput.value?.select();
}

async function share() {
  const url = shareUrl(shareQuery());
  // On a phone the system sheet is the thing people actually expect.
  if (isCompact.value && typeof navigator.share === 'function') {
    try {
      await navigator.share({ url, title: 'Ometto — a route in Trentino-Alto Adige' });
      return;
    } catch (e) {
      if ((e as Error)?.name === 'AbortError') return;
      /* no sheet, or it refused: fall through to the clipboard */
    }
  }
  try {
    await navigator.clipboard.writeText(url);
    toast('Link copied');
    return;
  } catch {
    /* insecure context, or permission refused */
  }
  if (typeof navigator.share === 'function') {
    try {
      await navigator.share({ url, title: 'Ometto — a route in Trentino-Alto Adige' });
      return;
    } catch (e) {
      if ((e as Error)?.name === 'AbortError') return;
    }
  }
  void showLink(url);
}

async function copyFromField() {
  const el = linkInput.value;
  if (!el) return;
  el.select();
  try {
    await navigator.clipboard.writeText(el.value);
    toast('Link copied');
    shareLink.value = '';
  } catch {
    try {
      document.execCommand('copy');
      toast('Link copied');
      shareLink.value = '';
    } catch {
      toast('Select the link and copy it.');
    }
  }
}

async function startNaming() {
  const last = filledPoints.value[filledPoints.value.length - 1];
  favName.value = last?.name ?? 'Destination';
  naming.value = true;
  await nextTick();
  favInput.value?.select();
}

async function saveFavourite() {
  const last = filledPoints.value[filledPoints.value.length - 1];
  if (!last) return;
  const name = favName.value.trim() || last.name || 'Saved place';
  naming.value = false;
  try {
    // Keep the plan with the place, so the favourite can be travelled to the
    // same way and not merely used as a coordinate.
    await addFavourite(
      { name, lat: last.lat, lon: last.lon, kind: last.kind },
      {
        mode: mode.value,
        grade: grade.value,
        lifts: lifts.value,
        vias: filledPoints.value.filter((p) => p.via),
        avoid: avoids.value.slice(),
      },
    );
    toast(`${name} saved to favourites`);
  } catch {
    toast('That could not be saved.');
  }
}
</script>

<template>
  <article
    class="card overflow-hidden transition-colors"
    :class="selected ? 'border-ink bg-surface' : 'bg-surface hover:border-line-strong'"
  >
    <button
      class="w-full px-3 pt-2.5 pb-2.5 text-left"
      :aria-pressed="selected"
      :aria-label="`Route ${index + 1}: ${fmtDuration(route.seconds)}, ${fmtDistance(route.meters)}`"
      @click="emit('select')"
    >
      <div class="flex items-baseline gap-2">
        <span class="text-[19px] font-semibold leading-none tracking-[-0.01em]">
          {{ fmtDuration(route.seconds) }}
        </span>
        <span v-if="delta" class="text-[12px] text-faint">{{ delta }}</span>
        <span v-else-if="index === 0 && best !== null" class="text-[11px] font-medium text-muted">Fastest</span>
        <span class="flex-1" />
        <span class="flex items-center gap-1">
          <Icon v-for="m in modes" :key="m" :name="m" :size="16" :style="{ color: `var(--${m})` }" />
        </span>
        <span
          v-if="route.grade"
          class="ml-0.5 rounded px-1.5 py-0.5 text-[11px] font-semibold"
          :style="{ color: `var(--grade-${route.grade.toLowerCase()})`, background: 'var(--surface-2)' }"
          :title="`Hardest section on this route: grade ${route.grade}`"
          :aria-label="`Hardest section on this route: grade ${route.grade}`"
        >
          {{ route.grade }}
        </span>
      </div>

      <div class="mt-1.5 flex flex-wrap items-center gap-x-2.5 gap-y-0.5 text-[12.5px] text-muted">
        <span>{{ fmtDistance(route.meters) }}</span>
        <span class="flex items-center gap-1">
          <Icon name="ascent" :size="13" />{{ fmtAscent(headAscent) }}<template v-if="mixed">
            on foot</template>
        </span>
        <span class="flex items-center gap-1">
          <Icon name="descent" :size="13" />{{ fmtDescent(headDescent) }}
        </span>
      </div>

      <p v-if="mixed" class="mt-1 text-[12px] text-muted">{{ journey.join(', then ') }}</p>

      <p v-if="comp" class="mt-1 truncate text-[11.5px] text-faint">{{ comp }}</p>
    </button>

    <div v-if="selected" class="border-t border-line px-3 pt-2.5 pb-3">
      <!-- Not a caveat in a list: unmarked glacier ground is the first thing
           this card has to say. -->
      <div
        v-if="alpineWarning"
        class="mb-3 flex items-start gap-2 rounded-[9px] px-2.5 py-2"
        :style="{ background: 'var(--alert-soft)', boxShadow: 'inset 2px 0 0 var(--dest)' }"
        role="alert"
      >
        <Icon name="warning" :size="15" class="relative top-0.5 shrink-0" :style="{ color: 'var(--dest)' }" />
        <span class="text-[12.5px] font-medium leading-snug">{{ sentence(alpineWarning) }}</span>
      </div>

      <ElevationProfile :route="route" />

      <div v-if="otherWarnings.length || parkingNote" class="mt-3 grid gap-1.5">
        <p
          v-if="parkingNote"
          class="flex items-start gap-1.5 rounded-[8px] bg-surface-2 px-2 py-1.5 text-[12px] leading-snug text-muted"
        >
          <svg width="13" height="13" viewBox="0 0 14 14" class="relative top-0.5 shrink-0" aria-hidden="true">
            <rect x="0.8" y="0.8" width="12.4" height="12.4" rx="3" fill="none" stroke="currentColor" stroke-width="1.4" />
            <text x="7" y="10.2" text-anchor="middle" font-size="8.5" font-weight="700" fill="currentColor">P</text>
          </svg>
          <span>{{ parkingNote }}</span>
        </p>
        <p
          v-for="(w, i) in otherWarnings"
          :key="i"
          class="flex items-start gap-1.5 rounded-[8px] bg-surface-2 px-2 py-1.5 text-[12px] leading-snug"
          :class="isSeasonalWarning(w) ? 'text-ink' : 'text-muted'"
        >
          <!-- Parking is not a caveat, it is part of the plan. -->
          <svg
            v-if="isParking(w)"
            width="13"
            height="13"
            viewBox="0 0 14 14"
            class="relative top-0.5 shrink-0"
            aria-hidden="true"
          >
            <rect x="0.8" y="0.8" width="12.4" height="12.4" rx="3" fill="none" stroke="currentColor" stroke-width="1.4" />
            <text x="7" y="10.2" text-anchor="middle" font-size="8.5" font-weight="700" fill="currentColor">P</text>
          </svg>
          <Icon
            v-else-if="isSeasonalWarning(w)"
            name="lift"
            :size="13"
            class="relative top-0.5 shrink-0"
            :style="{ color: 'var(--lift)' }"
          />
          <Icon v-else name="warning" :size="13" class="relative top-0.5 shrink-0" />
          <span>{{ sentence(w) }}</span>
        </p>
      </div>

      <div v-if="route.legs?.length > 1" class="mt-3">
        <h4 class="label mb-1.5">Legs</h4>
        <LegList :legs="route.legs" />
      </div>

      <div v-if="route.steps?.length" class="mt-3">
        <h4 class="label mb-0.5">Steps</h4>
        <StepList :steps="route.steps" />
      </div>

      <div v-if="shareLink" class="mt-3">
        <label class="label mb-1 block" :for="`share-${route.id}`">Link to this route</label>
        <div class="flex items-center gap-1.5">
          <input
            :id="`share-${route.id}`"
            ref="linkInput"
            class="field h-9 min-w-0 flex-1 px-2 text-[12px] outline-none"
            :value="shareLink"
            readonly
            spellcheck="false"
            @focus="($event.target as HTMLInputElement).select()"
          />
          <button class="btn-primary px-3 py-1.5 text-[12.5px]" @click="copyFromField">Copy</button>
          <button class="btn-quiet px-2 py-1.5 text-[12.5px]" @click="shareLink = ''">Done</button>
        </div>
      </div>

      <div v-else-if="!naming" class="mt-3 flex items-center gap-1.5">
        <button class="btn-quiet flex items-center gap-1.5 px-2 py-1.5 text-[12.5px]" @click="share">
          <Icon name="share" :size="14" /> Share
        </button>
        <button class="btn-quiet flex items-center gap-1.5 px-2 py-1.5 text-[12.5px]" @click="startNaming">
          <Icon name="star" :size="14" /> Save destination
        </button>
      </div>
      <form v-else class="mt-3 flex items-center gap-1.5" @submit.prevent="saveFavourite">
        <input
          ref="favInput"
          v-model="favName"
          class="field h-9 min-w-0 flex-1 px-2 text-[13px] outline-none"
          aria-label="Favourite name"
          @keydown.esc.prevent="naming = false"
        />
        <button type="submit" class="btn-primary px-3 py-1.5 text-[12.5px]">Save</button>
        <button type="button" class="btn-quiet px-2 py-1.5 text-[12.5px]" @click="naming = false">Cancel</button>
      </form>
    </div>
  </article>
</template>
