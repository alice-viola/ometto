import { ref } from 'vue';
import { api, ApiError } from '../lib/api';
import { readLocal, writeLocal } from '../lib/storage';
import type { AvoidPoint, Favourite, Grade, Mode, PlaceKind, Waypoint } from '../lib/types';

/**
 * How the person was travelling when they starred the place. The favourites
 * contract carries only the place itself, so the plan around it is remembered
 * in this browser and used to offer the same trip again.
 */
export interface FavouritePlan {
  mode: Mode;
  grade: Grade;
  lifts?: boolean;
  /** The shape the route was given, kept so the same trip can be asked again. */
  vias?: Waypoint[];
  avoid?: AvoidPoint[];
}
export const favouritePlans = ref<Record<string, FavouritePlan>>(
  readLocal<Record<string, FavouritePlan>>('favourite.plans', {}),
);

function rememberPlan(id: string, plan?: FavouritePlan) {
  if (!plan) return;
  favouritePlans.value = { ...favouritePlans.value, [id]: plan };
  writeLocal('favourite.plans', favouritePlans.value);
}

function forgetPlan(id: string) {
  if (!(id in favouritePlans.value)) return;
  const next = { ...favouritePlans.value };
  delete next[id];
  favouritePlans.value = next;
  writeLocal('favourite.plans', next);
}

export const favourites = ref<Favourite[]>([]);
export const favouritesLoaded = ref(false);
export const favouritesError = ref('');

export async function loadFavourites(): Promise<void> {
  try {
    favourites.value = await api.favourites();
    favouritesError.value = '';
  } catch (e) {
    favourites.value = [];
    // Routing keeps working while the store is down; say only what is true.
    favouritesError.value =
      e instanceof ApiError && (e.status === 503 || e.status === 429)
        ? 'Saved places are unavailable right now. Routing still works.'
        : 'Favourites are not available right now.';
  } finally {
    favouritesLoaded.value = true;
  }
}

export async function addFavourite(
  f: { name: string; lat: number; lon: number; kind?: PlaceKind },
  plan?: FavouritePlan,
): Promise<void> {
  const created = await api.addFavourite(f);
  // The service is the owner of the id; fall back to a local shape if it is terse.
  const item = created?.id ? created : { id: `f${Date.now()}`, ...f, createdAt: new Date().toISOString() };
  rememberPlan(item.id, plan);
  favourites.value = [item, ...favourites.value];
}

export async function removeFavourite(id: string): Promise<void> {
  favourites.value = favourites.value.filter((f) => f.id !== id);
  forgetPlan(id);
  await api.removeFavourite(id);
}

/** The contract has no rename: re-save under the new name, drop the old entry. */
export async function renameFavourite(fav: Favourite, name: string): Promise<void> {
  const clean = name.trim();
  if (!clean || clean === fav.name) return;
  await addFavourite(
    { name: clean, lat: fav.lat, lon: fav.lon, kind: fav.kind },
    favouritePlans.value[fav.id],
  );
  await removeFavourite(fav.id);
}

export function isFavourite(lat: number, lon: number): Favourite | undefined {
  return favourites.value.find(
    (f) => Math.abs(f.lat - lat) < 1e-5 && Math.abs(f.lon - lon) < 1e-5,
  );
}
