<script setup lang="ts">
import { nextTick, onMounted, ref } from 'vue';
import Icon from './Icon.vue';
import {
  favourites,
  favouritePlans,
  favouritesError,
  favouritesLoaded,
  loadFavourites,
  removeFavourite,
  renameFavourite,
} from '../composables/useFavourites';
import { avoids, grade, lifts, mode, setPoint, slots } from '../composables/usePlanner';
import { MODE_LABELS } from '../lib/format';
import type { Favourite } from '../lib/types';

const emit = defineEmits<{ done: [] }>();

const editing = ref<string | null>(null);
const draft = ref('');
const editInput = ref<HTMLInputElement | null>(null);

onMounted(() => {
  if (!favouritesLoaded.value) void loadFavourites();
});

function use(f: Favourite, where: 'start' | 'destination') {
  const point = { lat: f.lat, lon: f.lon, name: f.name, kind: f.kind };
  // A favourite remembers how you were travelling when you saved it.
  const plan = favouritePlans.value[f.id];
  if (plan && where === 'destination') {
    mode.value = plan.mode;
    grade.value = plan.grade;
    lifts.value = !!plan.lifts;
    avoids.value = (plan.avoid ?? []).slice();
  }
  setPoint(where === 'start' ? 0 : slots.value.length - 1, point);
  emit('done');
}

async function startEdit(f: Favourite) {
  editing.value = f.id;
  draft.value = f.name;
  await nextTick();
  editInput.value?.select();
}

async function commit(f: Favourite) {
  const name = draft.value;
  editing.value = null;
  await renameFavourite(f, name);
}
</script>

<template>
  <section>
    <div v-if="!favouritesLoaded" class="grid gap-1.5">
      <div v-for="i in 3" :key="i" class="card h-[52px] animate-pulse bg-surface-2" />
    </div>

    <p v-else-if="favouritesError" class="text-[13px] text-muted">{{ favouritesError }}</p>

    <div v-else-if="!favourites.length" class="pt-1">
      <p class="text-[13.5px] leading-relaxed text-muted">
        Star a place on the map, or save a destination from a route, and it waits here for the next
        time you set off.
      </p>
    </div>

    <div v-else class="grid gap-1.5">
      <article v-for="f in favourites" :key="f.id" class="card px-3 py-2.5">
        <form v-if="editing === f.id" class="flex items-center gap-1.5" @submit.prevent="commit(f)">
          <input
            ref="editInput"
            v-model="draft"
            class="field h-8 min-w-0 flex-1 px-2 text-[13px] outline-none"
            aria-label="Favourite name"
            @keydown.esc.prevent="editing = null"
          />
          <button type="submit" class="btn-primary px-2.5 py-1.5 text-[12px]">Save</button>
        </form>

        <template v-else>
          <div class="flex items-center gap-2">
            <Icon
              :name="f.kind === 'peak' ? 'peak' : f.kind === 'hut' ? 'hut' : f.kind === 'pass' ? 'pass' : 'place'"
              :size="15"
              class="shrink-0 text-muted"
            />
            <span class="min-w-0 flex-1 truncate text-[13.5px] font-medium">{{ f.name }}</span>
            <span
              v-if="favouritePlans[f.id]"
              class="flex shrink-0 items-center gap-0.5"
              :title="`Saved for ${MODE_LABELS[favouritePlans[f.id].mode]}`"
              :aria-label="`Saved for ${MODE_LABELS[favouritePlans[f.id].mode]}`"
            >
              <Icon
                v-for="m in favouritePlans[f.id].mode.split('+')"
                :key="m"
                :name="m"
                :size="13"
                :style="{ color: `var(--${m})` }"
              />
            </span>
            <button class="btn-quiet p-1.5" :aria-label="`Rename ${f.name}`" @click="startEdit(f)">
              <Icon name="pencil" :size="13" />
            </button>
            <button class="btn-quiet p-1.5" :aria-label="`Delete ${f.name}`" @click="removeFavourite(f.id)">
              <Icon name="trash" :size="13" />
            </button>
          </div>
          <div class="mt-1.5 flex gap-1.5">
            <button class="chip" @click="use(f, 'start')">Use as start</button>
            <button class="chip" @click="use(f, 'destination')">Use as destination</button>
          </div>
        </template>
      </article>
    </div>
  </section>
</template>

<style scoped>
.chip {
  border: 1px solid var(--line);
  border-radius: 7px;
  background: var(--surface-2);
  padding: 4px 8px;
  font-size: 12px;
  color: var(--muted);
  transition: color 0.14s ease, border-color 0.14s ease;
}
.chip:hover {
  color: var(--ink);
  border-color: var(--line-strong);
}
@media (max-width: 899px) {
  .chip {
    display: inline-flex;
    align-items: center;
    min-height: 44px;
    padding: 0 12px;
    font-size: 13px;
  }
}
</style>
