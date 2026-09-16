/**
 * One language at a time, chosen once and remembered.
 *
 * `en.ts` is the catalogue of record: its keys are the type every other
 * locale is checked against, so a missing Italian line is a build error and
 * not a blank label on the page. Adding a language is a file and a row in
 * LOCALES — nothing else here knows how many there are.
 *
 * `t()` reads the `locale` ref, so calling it inside a template or a computed
 * is all the reactivity a component needs: switching the language re-renders
 * the page without a reload.
 */
import { ref, watchEffect } from 'vue';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';
import en from './en';
import it from './it';

export type Messages = typeof en;
export type Key = keyof Messages;
export type Locale = 'en' | 'it';

/** Everything a locale needs: its catalogue, its name, and its number rules. */
export const LOCALES: { id: Locale; label: string; tag: string }[] = [
  { id: 'en', label: 'English', tag: 'en-GB' },
  { id: 'it', label: 'Italiano', tag: 'it-IT' },
];

const CATALOGUES: Record<Locale, Messages> = { en, it };

function isLocale(v: unknown): v is Locale {
  return LOCALES.some((l) => l.id === v);
}

/**
 * A language someone chose, else what the browser asks for, if we speak it.
 * `it-CH` and `it` are both Italian; anything we do not have falls back to
 * English rather than to a page of keys. Only a choice is stored: a guess is
 * made again on every visit, so a browser whose languages change is followed.
 */
function detect(): Locale {
  const stored = readLocalRaw('lang');
  if (isLocale(stored)) return stored;
  try {
    for (const tag of navigator.languages ?? [navigator.language]) {
      const base = String(tag).toLowerCase().split('-')[0];
      if (isLocale(base)) return base;
    }
  } catch {
    /* no navigator: English */
  }
  return 'en';
}

export const locale = ref<Locale>(detect());

/** The BCP 47 tag for Intl, which is not always the locale's own id. */
export function localeTag(): string {
  return LOCALES.find((l) => l.id === locale.value)?.tag ?? 'en-GB';
}

/**
 * The page's own language is part of the translation: a screen reader picks
 * its voice from it, and so does the browser's offer to translate.
 */
watchEffect(() => {
  try {
    document.documentElement.lang = locale.value;
    document.title = t('meta.title');
    document
      .querySelector('meta[name="description"]')
      ?.setAttribute('content', t('meta.description'));
  } catch {
    /* no document: nothing to label */
  }
});

type Params = Record<string, string | number>;

/**
 * A line in the current language. `{name}` slots are filled from `params`; a
 * catalogue line with a `|` in it is a singular and a plural, chosen by `n`.
 */
export function t(key: Key, params?: Params): string {
  const dict = CATALOGUES[locale.value];
  let s: string = dict[key] ?? en[key] ?? String(key);
  if (s.includes('|')) {
    const forms = s.split('|');
    const n = Number(params?.n);
    s = (Math.abs(n) === 1 ? forms[0] : forms[1] ?? forms[0]).trim();
  }
  if (params) s = s.replace(/\{(\w+)\}/g, (m, k: string) => (k in params ? String(params[k]) : m));
  return s;
}

/** A number with this locale's decimal mark: 12.3 km in English, 12,3 in Italian. */
export function decimal(n: number, digits: number): string {
  try {
    return new Intl.NumberFormat(localeTag(), {
      minimumFractionDigits: digits,
      maximumFractionDigits: digits,
      useGrouping: false,
    }).format(n);
  } catch {
    return n.toFixed(digits);
  }
}

/** A choice, as opposed to a guess: this one is remembered. */
export function setLocale(l: Locale): void {
  locale.value = l;
  writeLocalRaw('lang', l);
}
