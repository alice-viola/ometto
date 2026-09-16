<script setup lang="ts">
import { computed, nextTick, ref } from 'vue';
import Icon from './Icon.vue';
import {
  addAvoid,
  avoidedWays,
  insertVia,
  placeFromMap,
  removeVia,
  stopAvoiding,
} from '../composables/usePlanner';
import { addFavourite } from '../composables/useFavourites';
import { isCompact } from '../composables/useMedia';
import { toast } from '../composables/useToast';
import type { PopoverState } from '../map/popover';
import { t } from '../i18n';
import { detailText, featureKind, wayName } from '../i18n/service';

const props = defineProps<{ state: PopoverState }>();
const emit = defineEmits<{ close: [] }>();

const clampedX = computed(() => {
  const w = typeof window !== 'undefined' ? window.innerWidth : 1000;
  return Math.min(Math.max(props.state.x, 124), Math.max(124, w - 124));
});

const naming = ref(false);
const favName = ref('');
const nameInput = ref<HTMLInputElement | null>(null);

const lat = computed(() => props.state.lngLat[1]);
const lon = computed(() => props.state.lngLat[0]);

/** A lift is worth saying out loud: routing via one means riding it. */
const viaLabel = computed(() =>
  props.state.feature?.kind === 'lift' ? t('popover.rideLift') : t('popover.routeVia'),
);

function point(name?: string) {
  return { lat: lat.value, lon: lon.value, name: name ?? props.state.name };
}

function use(where: 'start' | 'stop' | 'destination') {
  placeFromMap(point(), where);
  emit('close');
}

function routeVia() {
  // A via dropped from the popover has no place on the drawn line to measure
  // against, so it joins at the end of the sequence before the destination.
  insertVia(lat.value, lon.value, Number.POSITIVE_INFINITY);
  emit('close');
}

function avoid() {
  addAvoid(lat.value, lon.value);
  emit('close');
}

function unavoid() {
  const way = avoidedWays.value.find((w) => w.id === props.state.avoidedId);
  if (way) stopAvoiding(way);
  emit('close');
}

function dropVia() {
  if (props.state.viaIndex !== undefined) removeVia(props.state.viaIndex);
  emit('close');
}

async function startNaming() {
  favName.value = wayName(props.state.name);
  naming.value = true;
  await nextTick();
  nameInput.value?.select();
}

async function saveFavourite() {
  const name = favName.value.trim() || wayName(props.state.name) || t('card.savedPlace');
  try {
    await addFavourite({ name, lat: lat.value, lon: lon.value });
    toast(t('card.saved', { name }));
  } catch {
    toast(t('card.couldNotSave'));
  }
  emit('close');
}
</script>

<template>
  <!--
    On desktop the card hangs over the point it is about. On a phone the point
    can be anywhere in a short band above the sheet, and a card with six rows a
    thumb can press has nowhere to hang: it becomes a sheet of its own at the
    bottom, over the planner, where the thumb already is. The map marks the
    pressed point with a dot in its stead.
  -->
  <div :class="isCompact ? 'fixed inset-x-0 bottom-0 z-40' : 'pointer-events-none absolute inset-0 z-20'">
    <div
      :class="isCompact ? 'w-full' : 'pointer-events-auto absolute w-[236px] -translate-x-1/2 -translate-y-full'"
      :style="isCompact ? undefined : { left: `${clampedX}px`, top: `${state.y - 16}px` }"
      role="dialog"
      :aria-label="t('popover.actions')"
    >
      <div
        class="card overflow-hidden"
        :class="isCompact ? 'rounded-b-none rounded-t-[18px] border-b-0' : ''"
        :style="{
          boxShadow: 'var(--shadow-2)',
          paddingBottom: isCompact ? 'env(safe-area-inset-bottom)' : undefined,
        }"
      >
        <div class="flex items-start gap-2 px-3 pt-2.5 pb-2" :class="isCompact ? 'items-center px-4' : ''">
          <div class="min-w-0 flex-1">
            <div class="truncate text-[13px] font-semibold leading-snug" :class="isCompact ? 'text-[15px]' : ''">
              <span v-if="state.loading" class="text-muted">{{ t('popover.locating') }}</span>
              <span v-else>{{ wayName(state.name) }}</span>
            </div>
            <div class="mt-0.5 truncate text-[11px] text-faint" :class="isCompact ? 'text-[12px]' : ''">
              <template v-if="state.avoidedId">{{ t('popover.avoided') }}</template>
              <template v-else-if="state.viaIndex !== undefined">{{ t('popover.onYourRoute') }}</template>
              <template v-else-if="state.feature"
                >{{ featureKind(state.feature.kind)
                }}<template v-if="state.feature.detail"> · {{ detailText(state.feature.detail) }}</template></template
              >
              <template v-else>{{ lat.toFixed(4) }}, {{ lon.toFixed(4) }}</template>
            </div>
          </div>
          <button class="btn-quiet -mr-1 -mt-0.5 p-1" :aria-label="t('popover.close')" @click="emit('close')">
            <Icon name="x" :size="14" />
          </button>
        </div>

        <div v-if="!naming" class="border-t border-line">
          <!-- A via of one's own: the only thing to do with it is undo it. -->
          <button v-if="state.viaIndex !== undefined" class="popover-action" @click="dropVia">
            <Icon name="x" :size="15" /> {{ t('popover.removeVia') }}
          </button>

          <template v-else-if="state.avoidedId">
            <button class="popover-action" @click="unavoid">
              <Icon name="check" :size="15" /> {{ t('popover.stopAvoiding') }}
            </button>
          </template>

          <template v-else>
            <template v-if="state.feature">
              <button class="popover-action" @click="routeVia">
                <Icon :name="state.feature.kind === 'lift' ? 'lift' : 'route'" :size="15" />
                {{ viaLabel }}
              </button>
              <button class="popover-action" :style="{ color: 'var(--dest)' }" @click="avoid">
                <Icon name="warning" :size="15" /> {{ t('popover.avoidThis') }}
              </button>
            </template>

            <button class="popover-action" @click="use('start')">
              <Icon name="locate" :size="15" /> {{ t('popover.startHere') }}
            </button>
            <button class="popover-action" @click="use('stop')">
              <Icon name="plus" :size="15" /> {{ t('popover.addStop') }}
            </button>
            <button class="popover-action" @click="use('destination')">
              <Icon name="place" :size="15" /> {{ t('popover.goHere') }}
            </button>
            <button class="popover-action" @click="startNaming">
              <Icon name="star" :size="15" /> {{ t('popover.saveFavourite') }}
            </button>
          </template>
        </div>

        <form v-else class="border-t border-line p-2.5" :class="isCompact ? 'p-4' : ''" @submit.prevent="saveFavourite">
          <label class="label mb-1 block" for="fav-name">{{ t('popover.name') }}</label>
          <input
            id="fav-name"
            ref="nameInput"
            v-model="favName"
            class="field w-full px-2 py-1.5 text-[13px] outline-none"
            :placeholder="t('card.savedPlace')"
            enterkeyhint="done"
            @keydown.esc.stop.prevent="naming = false"
          />
          <div class="mt-2 flex gap-1.5">
            <button type="submit" class="btn-primary flex-1 px-2 py-1.5 text-[12px]">{{ t('fav.save') }}</button>
            <button type="button" class="btn-quiet px-2 py-1.5 text-[12px]" @click="naming = false">
              {{ t('fav.cancel') }}
            </button>
          </div>
        </form>
      </div>
      <div
        v-if="!isCompact"
        class="mx-auto h-0 w-0 border-x-[7px] border-t-[7px] border-x-transparent"
        :style="{ borderTopColor: 'var(--line)' }"
      />
    </div>
  </div>
</template>

<style scoped>
.popover-action {
  display: flex;
  width: 100%;
  align-items: center;
  gap: 8px;
  padding: 7px 12px;
  font-size: 13px;
  text-align: left;
  color: var(--ink);
}
.popover-action:hover {
  background: var(--surface-2);
}
.popover-action + .popover-action {
  border-top: 1px solid var(--line);
}
/* Rows a thumb can press: 44 px, and a little more type. */
@media (max-width: 899px) {
  .popover-action {
    min-height: 44px;
    padding: 10px 16px;
    font-size: 14px;
  }
}
</style>
