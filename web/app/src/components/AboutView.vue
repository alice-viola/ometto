<script setup lang="ts">
import { computed } from 'vue';
import { appConfig } from '../lib/config';
import { DISCLAIMER_POINTS } from '../composables/useDisclaimer';
import { userIdWasReplaced } from '../lib/storage';

const cfg = computed(() => appConfig());

/** Whatever dates the service publishes, in the order it publishes them. */
const LABELS: Record<string, string> = {
  osm: 'OpenStreetMap extract',
  osmExtract: 'OpenStreetMap extract',
  sat: 'SAT cadastre',
  satCadastre: 'SAT cadastre',
  build: 'Build',
  buildDate: 'Build',
  dem: 'Elevation model',
  lifts: 'Lifts',
};
const dates = computed(() =>
  Object.entries(cfg.value.dataDates).map(([k, v]) => ({
    label: LABELS[k] ?? k.replace(/([a-z])([A-Z])/g, '$1 $2').replace(/^./, (c) => c.toUpperCase()),
    // Timestamps published by the service, read as dates.
    value: String(v).slice(0, 10),
  })),
);

const SOURCES = [
  {
    name: 'OpenStreetMap contributors',
    href: 'https://www.openstreetmap.org/copyright',
    note: 'Roads, paths, lifts, huts and place names. Open Database Licence.',
  },
  {
    name: 'SAT and the Province of Trento',
    href: 'https://www.sat.tn.it/',
    note: 'The marked-trail cadastre: numbers and grades.',
  },
  {
    name: 'OpenMapTiles and OpenFreeMap',
    href: 'https://openmaptiles.org/',
    note: 'The vector tile schema and the styles these are derived from.',
  },
  {
    name: 'AWS Terrain Tiles — Mapzen Terrarium',
    href: 'https://registry.opendata.aws/terrain-tiles/',
    note: 'The elevation the hillshade, the contours and the 3D view are drawn from.',
  },
  {
    name: 'Copernicus GLO-30 DEM',
    href: 'https://spacedata.copernicus.eu/',
    note: 'The elevation model behind the terrain tiles over this region.',
  },
  {
    name: 'Noto Sans',
    href: 'https://fonts.google.com/noto',
    note: 'The lettering on the map. SIL Open Font Licence.',
  },
];
</script>

<template>
  <section class="grid gap-5">
    <div>
      <p class="text-[13.5px] leading-relaxed text-muted">
        Ometto plans a way through Trentino-Alto Adige on foot, by bike or by car, and tells you what
        it costs in time and in climbing. It is built on open data and it runs on one small machine.
      </p>
    </div>

    <div>
      <h3 class="label mb-2">A planning aid, not a guide</h3>
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
      <h3 class="label mb-2">Data</h3>
      <dl class="grid gap-px overflow-hidden rounded-[9px] border border-line bg-line">
        <div v-for="d in dates" :key="d.label" class="flex items-baseline gap-3 bg-surface px-2.5 py-2">
          <dt class="min-w-0 flex-1 truncate text-[12.5px] text-muted">{{ d.label }}</dt>
          <dd class="shrink-0 text-[12.5px]">{{ d.value }}</dd>
        </div>
      </dl>
    </div>

    <div>
      <h3 class="label mb-2">Sources</h3>
      <ul class="grid gap-2.5">
        <li v-for="s in SOURCES" :key="s.name">
          <a
            class="text-[13px] underline decoration-line-strong underline-offset-2 hover:decoration-current max-[899px]:inline-block max-[899px]:py-1.5"
            :href="s.href"
            target="_blank"
            rel="noopener noreferrer"
            >{{ s.name }}</a
          >
          <p class="text-[11.5px] leading-snug text-faint">{{ s.note }}</p>
        </li>
      </ul>
    </div>

    <div>
      <h3 class="label mb-2">Known limits</h3>
      <ul class="grid gap-2 text-[12.5px] leading-relaxed text-muted">
        <li>
          The elevation is a <em>surface</em> model: it sits on treetops and roofs, so climb over
          wooded ground reads a little high and a tunnel does not read at all.
        </li>
        <li>
          Nothing here is real time. Closures, snow, rockfall, works and lift timetables are not
          known to it.
        </li>
        <li>
          Seasons are not modelled. A summer path and a winter one are the same line on this map.
        </li>
      </ul>
    </div>

    <p v-if="userIdWasReplaced()" class="border-t border-line pt-3 text-[11.5px] leading-snug text-faint">
      This browser's anonymous id was replaced with a longer, private one. Favourites and recent
      routes saved under the old id are no longer shown.
    </p>
  </section>
</template>
