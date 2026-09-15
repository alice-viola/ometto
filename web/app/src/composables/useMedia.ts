import { ref, watch } from 'vue';
import { readLocalRaw, writeLocalRaw } from '../lib/storage';

const mq = typeof matchMedia === 'function' ? matchMedia('(max-width: 899px)') : null;
export const isCompact = ref(mq?.matches ?? false);
mq?.addEventListener?.('change', (e) => (isCompact.value = e.matches));

const fine = typeof matchMedia === 'function' ? matchMedia('(hover: hover) and (pointer: fine)') : null;
export const hasHover = ref(fine?.matches ?? true);
fine?.addEventListener?.('change', (e) => (hasHover.value = e.matches));

/**
 * How much of the map the bottom sheet is covering right now. The map frames
 * routes into the band that is actually visible, so it has to know.
 */
export const sheetHeight = ref(232);
/** Bumped when the sheet settles on a snap point, never mid-drag. */
export const sheetSettled = ref(0);
/** True while a finger holds the sheet: what follows it must not ease behind it. */
export const sheetDragging = ref(false);

/**
 * The desktop panel can be folded away for a full-map view. The choice is the
 * person's, so it outlives the session.
 */
export const panelHidden = ref(readLocalRaw('panel.hidden') === '1');
watch(panelHidden, (v) => writeLocalRaw('panel.hidden', v ? '1' : '0'));
