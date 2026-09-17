<script setup lang="ts">
import { computed } from 'vue';
import { appConfig } from '../lib/config';
import { DISCLAIMER_POINTS } from '../composables/useDisclaimer';
import { userIdWasReplaced } from '../lib/storage';
import { t, type Key } from '../i18n';

const cfg = computed(() => appConfig());

/** Whatever dates the service publishes, in the order it publishes them. */
const LABELS: Record<string, Key> = {
  osm: 'about.date.osm',
  osmExtract: 'about.date.osm',
  sat: 'about.date.sat',
  satCadastre: 'about.date.sat',
  build: 'about.date.build',
  buildDate: 'about.date.build',
  dem: 'about.date.dem',
  lifts: 'about.date.lifts',
};
const dates = computed(() =>
  Object.entries(cfg.value.dataDates).map(([k, v]) => ({
    label: LABELS[k]
      ? t(LABELS[k])
      : k.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/^./, (c) => c.toUpperCase()),
    // Timestamps published by the service, read as dates.
    value: String(v).slice(0, 10),
  })),
);

const SOURCES: { name: Key; href: string; note: Key }[] = [
  { name: 'about.src.osm', href: 'https://www.openstreetmap.org/copyright', note: 'about.src.osmNote' },
  { name: 'about.src.sat', href: 'https://www.sat.tn.it/', note: 'about.src.satNote' },
  { name: 'about.src.omt', href: 'https://openmaptiles.org/', note: 'about.src.omtNote' },
  { name: 'about.src.terrain', href: 'https://registry.opendata.aws/terrain-tiles/', note: 'about.src.terrainNote' },
  { name: 'about.src.copernicus', href: 'https://spacedata.copernicus.eu/', note: 'about.src.copernicusNote' },
  { name: 'about.src.noto', href: 'https://fonts.google.com/noto', note: 'about.src.notoNote' },
];

/** Where the code this page runs is published (AGPL-3.0, section 13). A copy run elsewhere points this at its own. */
const SOURCE_CODE = 'https://github.com/alice-viola/ometto';

const LIMITS: Key[] = ['about.limit.realtime', 'about.limit.seasons', 'about.limit.crags'];
</script>

<template>
  <section class="grid gap-5">
    <div>
      <p class="text-[13.5px] leading-relaxed text-muted">
        {{ t('about.intro') }}
      </p>
    </div>

    <div>
      <h3 class="label mb-2">{{ t('about.planningAid') }}</h3>
      <ul class="grid gap-2">
        <li
          v-for="(line, i) in DISCLAIMER_POINTS"
          :key="i"
          class="flex gap-2 text-[12.5px] leading-relaxed text-muted"
        >
          <span class="mt-[7px] size-1 shrink-0 rounded-full" :style="{ background: 'var(--faint)' }" />
          <span>{{ line }}</span>
        </li>
      </ul>
    </div>

    <div v-if="dates.length">
      <h3 class="label mb-2">{{ t('about.data') }}</h3>
      <dl class="grid gap-px overflow-hidden rounded-[9px] border border-line bg-line">
        <div v-for="d in dates" :key="d.label" class="flex items-baseline gap-3 bg-surface px-2.5 py-2">
          <dt class="min-w-0 flex-1 truncate text-[12.5px] text-muted">{{ d.label }}</dt>
          <dd class="shrink-0 text-[12.5px]">{{ d.value }}</dd>
        </div>
      </dl>
    </div>

    <div>
      <h3 class="label mb-2">{{ t('about.sources') }}</h3>
      <ul class="grid gap-2.5">
        <li v-for="s in SOURCES" :key="s.name">
          <a
            class="text-[13px] underline decoration-line-strong underline-offset-2 hover:decoration-current max-[899px]:inline-block max-[899px]:py-1.5"
            :href="s.href"
            target="_blank"
            rel="noopener noreferrer"
            >{{ t(s.name) }}</a
          >
          <p class="text-[11.5px] leading-snug text-faint">{{ t(s.note) }}</p>
        </li>
      </ul>
    </div>

    <div>
      <h3 class="label mb-2">{{ t('about.code') }}</h3>
      <a
        class="text-[13px] underline decoration-line-strong underline-offset-2 hover:decoration-current max-[899px]:inline-block max-[899px]:py-1.5"
        :href="SOURCE_CODE"
        target="_blank"
        rel="noopener noreferrer"
        >{{ SOURCE_CODE.replace('https://', '') }}</a
      >
      <p class="text-[11.5px] leading-snug text-faint">{{ t('about.codeNote') }}</p>
    </div>

    <div>
      <h3 class="label mb-2">{{ t('about.limits') }}</h3>
      <ul class="grid gap-2 text-[12.5px] leading-relaxed text-muted">
        <li>
          {{ t('about.limit.dem.before') }}<em>{{ t('about.limit.dem.em') }}</em>{{ t('about.limit.dem.after') }}
        </li>
        <li v-for="key in LIMITS" :key="key">{{ t(key) }}</li>
      </ul>
    </div>

    <p v-if="userIdWasReplaced()" class="border-t border-line pt-3 text-[11.5px] leading-snug text-faint">
      {{ t('about.idReplaced') }}
    </p>
  </section>
</template>
