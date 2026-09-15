import { shallowRef } from 'vue';
import { STYLE_DARK, STYLE_LIGHT, DEM_TILES } from '../map/style';

/**
 * What the server tells the app about itself. Everything here has a working
 * default, so a dev setup without the endpoint behaves exactly as before.
 */
export interface AppConfig {
  styles: { light: string; dark: string };
  /** Served as `data`, specified as `dataDates`; both are read. */
  terrain: { url: string; attribution: string };
  dataDates: Record<string, string>;
  disclaimerVersion: string;
  publicUrl: string;
  /** False once the server has answered: the styles are then self-hosted. */
  fallback: boolean;
}

const DEFAULTS: AppConfig = {
  styles: { light: STYLE_LIGHT, dark: STYLE_DARK },
  terrain: { url: DEM_TILES, attribution: 'Elevation: Terrarium (Mapzen, AWS Open Data)' },
  dataDates: {},
  disclaimerVersion: '1',
  publicUrl: '',
  fallback: true,
};

/**
 * Reactive: the config lands after the first paint, and the footer, the About
 * panel and the disclaimer version all have to notice when it does.
 */
const config = shallowRef<AppConfig>({ ...DEFAULTS });
let loading: Promise<AppConfig> | null = null;

export function appConfig(): AppConfig {
  return config.value;
}

/** Fetched once; every caller awaits the same request. */
export function loadConfig(): Promise<AppConfig> {
  return (loading ??= fetch('/api/config', { headers: { accept: 'application/json' } })
    .then((r) => (r.ok ? r.json() : Promise.reject(new Error(String(r.status)))))
    .then((body: Partial<AppConfig> & { data?: Record<string, string> }) => {
      config.value = {
        styles: {
          light: body.styles?.light || DEFAULTS.styles.light,
          dark: body.styles?.dark || DEFAULTS.styles.dark,
        },
        terrain: {
          url: body.terrain?.url || DEFAULTS.terrain.url,
          attribution: body.terrain?.attribution || DEFAULTS.terrain.attribution,
        },
        // The router publishes these under `data`; the written contract said
        // `dataDates`. Accept either, and treat blank values as no date at all.
        dataDates: Object.fromEntries(
          Object.entries({ ...(body.data ?? {}), ...(body.dataDates ?? {}) }).filter(
            ([, v]) => typeof v === 'string' && v.trim() !== '',
          ),
        ) as Record<string, string>,
        disclaimerVersion: String(body.disclaimerVersion ?? DEFAULTS.disclaimerVersion),
        publicUrl: body.publicUrl || '',
        // Only a real answer retires the in-app corrections to the vendor style.
        fallback: !body.styles?.dark,
      };
      return config.value;
    })
    .catch(() => {
      config.value = { ...DEFAULTS };
      return config.value;
    }));
}

/** The origin a shared link should carry, which is not always this one. */
export function shareOrigin(): string {
  const p = config.value.publicUrl.trim().replace(/\/+$/, '');
  if (p) return p;
  try {
    return location.origin;
  } catch {
    return '';
  }
}
