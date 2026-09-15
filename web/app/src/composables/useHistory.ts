import { ref, watch } from 'vue';
import { api, ApiError } from '../lib/api';
import type { HistoryEntry } from '../lib/types';
import { historyToken } from './usePlanner';

export const history = ref<HistoryEntry[]>([]);
export const historyLoaded = ref(false);
export const historyError = ref('');

export async function loadHistory(): Promise<void> {
  try {
    history.value = await api.history();
    historyError.value = '';
  } catch (e) {
    history.value = [];
    historyError.value =
      e instanceof ApiError && (e.status === 503 || e.status === 429)
        ? 'Saved places are unavailable right now. Routing still works.'
        : 'Recent routes are not available right now.';
  } finally {
    historyLoaded.value = true;
  }
}

export async function removeHistory(id: string): Promise<void> {
  history.value = history.value.filter((h) => h.id !== id);
  await api.removeHistory(id);
}

export async function clearHistory(): Promise<void> {
  history.value = [];
  await api.clearHistory();
}

// A freshly computed route belongs in the list the next time it is opened.
watch(historyToken, () => {
  if (historyLoaded.value) void loadHistory();
});
