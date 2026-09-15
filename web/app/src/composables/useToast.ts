import { ref } from 'vue';

export const toastMessage = ref<string>('');
let timer: number | undefined;

/** One quiet line, bottom centre, gone in three seconds. Never an error dialog. */
export function toast(message: string): void {
  toastMessage.value = message;
  clearTimeout(timer);
  timer = window.setTimeout(() => (toastMessage.value = ''), 3000);
}
