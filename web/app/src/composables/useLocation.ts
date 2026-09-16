import { computed, ref, watch } from 'vue';
import { localeTag, t } from '../i18n';
import { toast } from './useToast';
import { selected } from './usePlanner';
import { progress, type Progress } from '../lib/progress';

/**
 * The live position, and what is left of the route from it.
 *
 * One watch for the whole page, held here rather than in a component: the map
 * draws the dot, the card counts down and the phone's folded strip quotes the
 * same figures, and all three have to be looking at the same fix.
 *
 * Nothing here caches `navigator.geolocation`. It is read at the moment it is
 * used, so a page that swaps in a fake one — QA, a recorded track — is
 * followed rather than ignored.
 */
export type LocationStatus = 'off' | 'locating' | 'on' | 'denied' | 'unavailable' | 'insecure';

export interface Fix {
  lat: number;
  lon: number;
  /** The radius the browser will vouch for, in metres. */
  accuracy: number;
  /** Degrees clockwise from north, when the device knows; null when it does not. */
  heading: number | null;
  /** Metres per second, or null. */
  speed: number | null;
  at: number;
}

export const status = ref<LocationStatus>('off');
export const position = ref<Fix | null>(null);
/** Whether the map moves with the fix. A pan or a pinch gives it up. */
export const following = ref(false);

/** A fix older than this is not the present tense any more. */
const STALE = 30_000;

/**
 * One tick a second while the sensor is live. The distance left changes when a
 * fix arrives; the arrival time changes because time passes, so it needs a
 * clock of its own — and a fix that stops coming has to be noticed.
 */
export const now = ref(Date.now());
let ticker: number | undefined;
watch(
  () => status.value === 'on' || status.value === 'locating',
  (live) => {
    clearInterval(ticker);
    ticker = undefined;
    if (!live) return;
    now.value = Date.now();
    ticker = window.setInterval(() => (now.value = Date.now()), 1000);
  },
);

export const stale = computed(() => {
  const p = position.value;
  return !!p && now.value - p.at > STALE;
});

/** What is left of the selected route from here, or nothing to say yet. */
export const live = computed<Progress | null>(() => {
  const p = position.value;
  const route = selected.value;
  return p && route ? progress(route, p) : null;
});

/** "14:32": when you get there if nothing changes. Never a duration. */
export function arrivalClock(seconds: number): string {
  const d = new Date(now.value + Math.max(0, seconds) * 1000);
  try {
    return d.toLocaleTimeString(localeTag(), { hour: '2-digit', minute: '2-digit', hour12: false });
  } catch {
    return `${String(d.getHours()).padStart(2, '0')}:${String(d.getMinutes()).padStart(2, '0')}`;
  }
}

// --- the sensor -------------------------------------------------------------

let watchId: number | null = null;

function clearWatch(): void {
  if (watchId === null) return;
  const id = watchId;
  watchId = null;
  try {
    navigator.geolocation?.clearWatch(id);
  } catch {
    /* the watch is gone either way */
  }
}

function onFix(p: GeolocationPosition): void {
  const c = p.coords;
  position.value = {
    lat: c.latitude,
    lon: c.longitude,
    accuracy: isFinite(c.accuracy) ? c.accuracy : 0,
    heading: typeof c.heading === 'number' && isFinite(c.heading) ? c.heading : null,
    speed: typeof c.speed === 'number' && isFinite(c.speed) ? c.speed : null,
    // When it reached us, not what the device's clock calls it: a phone whose
    // clock is minutes out would otherwise look stale from the first fix.
    at: Date.now(),
  };
  status.value = 'on';
  now.value = Date.now();
}

function onError(e: GeolocationPositionError): void {
  if (e?.code === 1 /* PERMISSION_DENIED */) {
    clearWatch();
    following.value = false;
    position.value = null;
    status.value = 'denied';
    toast(t('live.denied'));
    return;
  }
  // POSITION_UNAVAILABLE and TIMEOUT. With a fix already in hand the watch is
  // worth keeping — the next one usually arrives, and the card says meanwhile
  // that it is waiting. With none there is nothing to show and nothing to keep.
  if (position.value) return;
  clearWatch();
  following.value = false;
  status.value = 'unavailable';
  toast(t('live.unavailable'));
}

export function start(): void {
  if (watchId !== null) return;
  // A page served over plain http gets no position at all in any current
  // browser, and says so before asking for a permission that cannot be given.
  if (typeof window !== 'undefined' && window.isSecureContext === false) {
    status.value = 'insecure';
    toast(t('live.insecure'));
    return;
  }
  const geo = navigator.geolocation;
  if (!geo) {
    status.value = 'unavailable';
    toast(t('live.unavailable'));
    return;
  }
  status.value = 'locating';
  const id = geo.watchPosition(onFix, onError, {
    enableHighAccuracy: true,
    maximumAge: 2000,
    timeout: 20_000,
  });
  watchId = typeof id === 'number' ? id : 0;
}

export function stop(): void {
  clearWatch();
  following.value = false;
  position.value = null;
  status.value = 'off';
}

export function follow(): void {
  following.value = true;
  if (watchId === null) start();
}

/**
 * The one button: off starts and follows, following stops altogether, and a
 * fix the map has drifted away from is picked up again.
 */
export function toggle(): void {
  if (watchId !== null && following.value) {
    stop();
    return;
  }
  follow();
}

// --- keeping the screen on --------------------------------------------------

/** Only what we use of the Wake Lock API, which not every browser has. */
type Sentinel = { release(): Promise<void> };
type WakeLock = { request(kind: 'screen'): Promise<Sentinel> };

let sentinel: Sentinel | null = null;
let saidScreenStaysOn = false;

function wakeLock(): WakeLock | undefined {
  return (navigator as Navigator & { wakeLock?: WakeLock }).wakeLock;
}

/**
 * A map that follows you is a map you are looking at from a pocket or a bar
 * bag, and the screen going dark mid-climb is the whole complaint. Every step
 * is guarded: a browser without the API, or one that refuses the lock on
 * battery, still follows.
 */
async function acquireWakeLock(): Promise<void> {
  if (sentinel || !following.value) return;
  const api = wakeLock();
  if (!api || (typeof document !== 'undefined' && document.visibilityState !== 'visible')) return;
  try {
    const held = await api.request('screen');
    if (!following.value) {
      await held.release();
      return;
    }
    sentinel = held;
    if (!saidScreenStaysOn) {
      saidScreenStaysOn = true;
      toast(t('live.screenStaysOn'));
    }
  } catch {
    /* refused: following is still worth having */
  }
}

async function releaseWakeLock(): Promise<void> {
  const held = sentinel;
  sentinel = null;
  try {
    await held?.release();
  } catch {
    /* already released */
  }
}

watch(following, (on) => {
  if (on) void acquireWakeLock();
  else void releaseWakeLock();
});

if (typeof document !== 'undefined') {
  document.addEventListener('visibilitychange', () => {
    // The browser drops the lock itself when the page hides; coming back is
    // the only moment it can be asked for again.
    if (document.visibilityState === 'visible') void acquireWakeLock();
    else sentinel = null;
  });
}

if (typeof window !== 'undefined') {
  window.addEventListener('pagehide', () => stop());
}
