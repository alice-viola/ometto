import { computed, ref } from 'vue';
import { appConfig } from '../lib/config';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';

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

/** The lines the dialog and the About panel both say, in one place. */
export const DISCLAIMER_POINTS: string[] = [
  'Times are estimates. They come from a rule of thumb, not from your legs, your pack or the weather.',
  'Trail grades come from the SAT cadastre and from OpenStreetMap. They can be wrong, or simply out of date.',
  'Alpine ground and via ferrata need experience and equipment. This app will route over them if you ask it to.',
  'Lifts and mountain roads have seasons and opening hours. A line on this map is not a promise that it runs.',
  'Check conditions and the forecast before you go. You are responsible for your own safety.',
];
