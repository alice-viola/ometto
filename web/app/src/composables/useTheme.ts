import { computed, ref, watchEffect } from 'vue';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';

export type ThemeChoice = 'system' | 'light' | 'dark';

const stored = readLocalRaw('theme');
export const theme = ref<ThemeChoice>(
  stored === 'light' || stored === 'dark' || stored === 'system' ? stored : 'system',
);

const media = typeof matchMedia === 'function' ? matchMedia('(prefers-color-scheme: dark)') : null;
const systemDark = ref(media?.matches ?? false);
media?.addEventListener?.('change', (e) => (systemDark.value = e.matches));

export const isDark = computed(() =>
  theme.value === 'system' ? systemDark.value : theme.value === 'dark',
);

watchEffect(() => {
  const dark = isDark.value;
  document.documentElement.classList.toggle('dark', dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
  writeLocalRaw('theme', theme.value);
});

export function useTheme() {
  return {
    theme,
    isDark,
    setTheme(t: ThemeChoice) {
      theme.value = t;
    },
    toggle() {
      theme.value = isDark.value ? 'light' : 'dark';
    },
  };
}
