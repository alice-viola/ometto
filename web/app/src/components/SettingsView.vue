<script setup lang="ts">
import Icon from './Icon.vue';
import Toggle from './Toggle.vue';
import { available, layers } from '../composables/useLayers';
import { theme, type ThemeChoice } from '../composables/useTheme';
import { LOCALES, locale, setLocale, t, type Key } from '../i18n';

const emit = defineEmits<{ about: [] }>();

const THEMES: { id: ThemeChoice; label: Key; icon: string }[] = [
  { id: 'system', label: 'settings.system', icon: 'monitor' },
  { id: 'light', label: 'settings.light', icon: 'sun' },
  { id: 'dark', label: 'settings.dark', icon: 'moon' },
];
</script>

<template>
  <section class="grid gap-5">
    <!-- First, and each language named in itself: whoever cannot read the
         rest of this panel can still find their own word here. -->
    <div>
      <h3 class="label mb-2">{{ t('settings.language') }}</h3>
      <div
        role="radiogroup"
        :aria-label="t('settings.language')"
        class="grid gap-1.5"
        :style="{ gridTemplateColumns: `repeat(${LOCALES.length}, minmax(0, 1fr))` }"
      >
        <button
          v-for="l in LOCALES"
          :key="l.id"
          role="radio"
          :lang="l.id"
          :aria-checked="locale === l.id"
          class="lang-chip"
          :class="locale === l.id ? 'is-on' : ''"
          @click="setLocale(l.id)"
        >
          {{ l.label }}
        </button>
      </div>
    </div>

    <div>
      <h3 class="label mb-2">{{ t('settings.theme') }}</h3>
      <div role="radiogroup" :aria-label="t('settings.theme')" class="grid grid-cols-3 gap-1.5">
        <button
          v-for="th in THEMES"
          :key="th.id"
          role="radio"
          :aria-label="t(th.label)"
          :aria-checked="theme === th.id"
          class="theme-chip"
          :class="theme === th.id ? 'is-on' : ''"
          @click="theme = th.id"
        >
          <Icon :name="th.icon" :size="16" />
          <span>{{ t(th.label) }}</span>
        </button>
      </div>
    </div>

    <div>
      <h3 class="label mb-0.5">{{ t('settings.map') }}</h3>
      <div class="divide-y divide-line">
        <Toggle v-model="layers.terrain" :label="t('settings.terrain')" :hint="t('settings.terrainHint')" />
        <Toggle v-model="layers.contours" :label="t('settings.contours')" :hint="t('settings.contoursHint')" />
        <Toggle
          v-model="layers.sat"
          :label="t('settings.sat')"
          :hint="available.sat ? t('settings.satHint') : t('settings.notPublished')"
          :disabled="!available.sat"
        />
        <Toggle
          v-model="layers.pois"
          :label="t('settings.pois')"
          :hint="available.pois ? t('settings.poisHint') : t('settings.notPublished')"
          :disabled="!available.pois"
        />
        <Toggle
          v-model="layers.crags"
          :label="t('settings.crags')"
          :hint="available.crags ? t('settings.cragsHint') : t('settings.notPublished')"
          :disabled="!available.crags"
        />
        <Toggle
          v-model="layers.lifts"
          :label="t('settings.lifts')"
          :hint="available.lifts ? t('settings.liftsHint') : t('settings.notPublished')"
          :disabled="!available.lifts"
        />
      </div>
    </div>

    <div>
      <h3 class="label mb-1.5">{{ t('settings.units') }}</h3>
      <p class="text-[13px] text-muted">{{ t('settings.unitsNote') }}</p>
    </div>

    <div class="border-t border-line pt-3">
      <h3 class="label mb-1.5">{{ t('settings.about') }}</h3>
      <p class="text-[12px] leading-relaxed text-muted">
        {{ t('settings.aboutNote') }}
      </p>
    </div>
  </section>
</template>

<style scoped>
.note-action {
  color: var(--ink);
  font-size: 12px;
  text-decoration: underline;
  text-decoration-color: var(--line-strong);
  text-underline-offset: 2px;
}
.note-action:hover {
  text-decoration-color: var(--ink);
}
.theme-chip {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 5px;
  padding: 9px 4px 8px;
  border: 1px solid var(--line);
  border-radius: var(--radius-control);
  background: var(--surface-2);
  color: var(--muted);
  font-size: 12px;
  transition: background 0.14s ease, color 0.14s ease, border-color 0.14s ease;
}
.theme-chip:hover:not(.is-on) {
  color: var(--ink);
  border-color: var(--line-strong);
}
.theme-chip.is-on {
  background: var(--ink);
  border-color: var(--ink);
  color: var(--ink-invert);
}
.lang-chip {
  padding: 8px 4px;
  border: 1px solid var(--line);
  border-radius: var(--radius-control);
  background: var(--surface-2);
  color: var(--muted);
  font-size: 12.5px;
  transition: background 0.14s ease, color 0.14s ease, border-color 0.14s ease;
}
.lang-chip:hover:not(.is-on) {
  color: var(--ink);
  border-color: var(--line-strong);
}
.lang-chip.is-on {
  background: var(--ink);
  border-color: var(--ink);
  color: var(--ink-invert);
}
@media (max-width: 899px) {
  .lang-chip {
    min-height: 44px;
    font-size: 13.5px;
  }
}
</style>
