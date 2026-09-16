<script setup lang="ts">
import Icon from './Icon.vue';
import { fmtAscent, fmtDistance, fmtDuration, legModeLabel, liftTypeLabel } from '../lib/format';
import { t } from '../i18n';
import type { Leg } from '../lib/types';

defineProps<{ legs: Leg[] }>();
</script>

<template>
  <ul class="grid gap-px overflow-hidden rounded-[9px] border border-line bg-line">
    <li v-for="(l, i) in legs" :key="i" class="flex items-center gap-2 bg-surface px-2.5 py-2">
      <span
        class="grid size-5 shrink-0 place-items-center"
        :style="{ color: `var(--${l.mode})` }"
        :title="legModeLabel(l.mode)"
      >
        <Icon :name="l.mode" :size="16" />
      </span>
      <span class="sr-only">{{ legModeLabel(l.mode) }}</span>
      <span class="flex min-w-0 flex-1 flex-col">
        <span class="whitespace-nowrap text-[13px] font-medium">{{ fmtDuration(l.seconds) }}</span>
        <!-- A lift is worth naming: which one, and what you get into. -->
        <span v-if="l.mode === 'lift'" class="truncate text-[11px] text-faint">
          {{ l.name || t('leg.lift') }} · {{ liftTypeLabel(l.liftType) }}
        </span>
      </span>
      <span class="shrink-0 whitespace-nowrap text-right text-[12.5px] text-muted">{{ fmtDistance(l.meters) }}</span>
      <span class="w-[58px] shrink-0 whitespace-nowrap text-right text-[12.5px] text-muted">{{ fmtAscent(l.ascent) }}</span>
      <span
        v-if="l.grade"
        class="w-[26px] shrink-0 text-right text-[11.5px] font-semibold"
        :style="{ color: `var(--grade-${l.grade.toLowerCase()})` }"
      >
        {{ l.grade }}
      </span>
      <span v-else class="w-[26px] shrink-0" />
    </li>
  </ul>
</template>
