<script setup lang="ts">
import { DISCLAIMER_POINTS, acceptDisclaimer } from '../composables/useDisclaimer';
import Icon from './Icon.vue';
import BrandMark from './BrandMark.vue';
import { t } from '../i18n';
</script>

<template>
  <!--
    Shown before the first route, not after it. Someone who has just been given
    a time and a grade is in no mood to read what they are worth.
  -->
  <div
    class="fixed inset-0 z-50 flex items-center justify-center p-4"
    style="background: rgb(0 0 0 / 0.45)"
    role="dialog"
    aria-modal="true"
    aria-labelledby="disclaimer-title"
  >
    <div
      class="card scroll-quiet max-h-[86dvh] w-full max-w-[460px] overflow-y-auto"
      style="box-shadow: var(--shadow-2)"
    >
      <div class="flex items-center gap-2 px-5 pt-5">
        <BrandMark :size="18" class="text-ink" />
        <span class="text-[13px] font-medium">Ometto</span>
      </div>
      <div class="mt-3 flex items-start gap-2.5 px-5">
        <Icon name="warning" :size="20" class="relative top-0.5 shrink-0" :style="{ color: 'var(--dest)' }" />
        <h2 id="disclaimer-title" class="text-[17px] font-semibold leading-snug tracking-[-0.01em]">
          {{ t('disclaimer.title') }}
        </h2>
      </div>

      <ul class="mt-3 grid gap-2.5 px-5">
        <li
          v-for="(line, i) in DISCLAIMER_POINTS"
          :key="i"
          class="flex gap-2.5 text-[13px] leading-relaxed text-muted"
        >
          <span class="mt-[7px] size-1 shrink-0 rounded-full" :style="{ background: 'var(--faint)' }" />
          <span>{{ line }}</span>
        </li>
      </ul>

      <!-- Sticky, so a short phone in landscape shows the button before the
           list has been scrolled to its end. -->
      <div class="sticky bottom-0 mt-4 border-t border-line bg-surface px-5 py-4">
        <button class="btn-primary w-full px-4 py-3 text-[14px]" @click="acceptDisclaimer">
          {{ t('disclaimer.accept') }}
        </button>
      </div>
    </div>
  </div>
</template>
