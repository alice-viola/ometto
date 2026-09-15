import { reactive, watch } from 'vue';
import { readLocal, writeLocal } from '../lib/storage';

export interface LayerState {
  sat: boolean;
  pois: boolean;
  lifts: boolean;
  contours: boolean;
  terrain: boolean;
}

const saved = readLocal<Partial<LayerState>>('layers', {});

export const layers = reactive<LayerState>({
  sat: saved.sat ?? true,
  pois: saved.pois ?? true,
  // On by default: knowing a lift exists is half the reason to allow one.
  lifts: saved.lifts ?? true,
  contours: saved.contours ?? false,
  terrain: saved.terrain ?? false,
});

/** A layer the service has not delivered yet: its switch is shown, but disabled. */
export const available = reactive({ sat: true, pois: true, lifts: true, region: true });

watch(
  () => ({ ...layers }),
  (v) => writeLocal('layers', v),
  { deep: true },
);
