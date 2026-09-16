import { computed, ref, shallowRef, watch } from 'vue';
import { api, ApiError } from '../lib/api';
import type { HistoryEntry } from '../lib/types';
import { t } from '../i18n';
import { historyToken } from './usePlanner';

export const history = ref<HistoryEntry[]>([]);
export const historyLoaded = ref(false);
const historySay = shallowRef<(() => string) | null>(null);
export const historyError = computed(() => historySay.value?.() ?? '');

export async function loadHistory(): Promise<void> {
  try {
    history.value = await api.history();
    historySay.value = null;
  } catch (e) {
    history.value = [];
    const degraded = e instanceof ApiError && (e.status === 503 || e.status === 429);
    historySay.value = () => (degraded ? t('fav.degraded') : t('history.unavailable'));
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
