import { ref } from 'vue';

export interface ToastAction {
  label: string;
  run: () => void;
}

export const toastMessage = ref<string>('');
export const toastAction = ref<ToastAction | null>(null);
let timer: number | undefined;

/**
 * One quiet line, bottom centre, gone in three seconds. Never an error dialog.
 * With an action it is the one place an accident can be taken back — a point
 * dragged by a scroll — so it stays twice as long, and shows a button.
 */
export function toast(message: string, action?: ToastAction): void {
  toastMessage.value = message;
  toastAction.value = action ?? null;
  clearTimeout(timer);
  timer = window.setTimeout(dismissToast, action ? 6000 : 3000);
}

export function dismissToast(): void {
  toastMessage.value = '';
  toastAction.value = null;
}
