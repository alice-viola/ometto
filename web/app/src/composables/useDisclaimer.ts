import { computed, ref } from 'vue';
import { appConfig } from '../lib/config';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';
import { t } from '../i18n';

/** The disclaimer version this browser has accepted, if any. */
const acceptedVersion = ref<string>(readLocalRaw('disclaimer') ?? '');
/** Set once the config is in, so the first paint does not flash the dialog. */
export const disclaimerReady = ref(false);

export const disclaimerAccepted = computed(
  () => disclaimerReady.value && acceptedVersion.value === appConfig().disclaimerVersion,
);

export function acceptDisclaimer(): void {
  acceptedVersion.value = appConfig().disclaimerVersion;
  writeLocalRaw('disclaimer', acceptedVersion.value);
}

/**
 * The lines the dialog and the About panel both say, in one place. Accepting
 * them in one language is accepting them: the version is the text's meaning,
 * not its wording, so switching language does not ask again.
 */
export const DISCLAIMER_POINTS = computed(() => [
  t('disclaimer.times'),
  t('disclaimer.grades'),
  t('disclaimer.alpine'),
  t('disclaimer.seasons'),
  t('disclaimer.responsibility'),
]);
