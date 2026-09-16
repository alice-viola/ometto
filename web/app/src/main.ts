/**
 * Paint the first frame in the right theme. This used to be an inline script
 * in index.html; the router serves `script-src 'self'`, which forbids inline
 * script, so it runs here instead — still before the app mounts.
 */
try {
  const stored = localStorage.getItem('trp.theme') || 'system';
  const dark = stored === 'dark' || (stored === 'system' && matchMedia('(prefers-color-scheme: dark)').matches);
  document.documentElement.classList.toggle('dark', dark);
  document.documentElement.style.colorScheme = dark ? 'dark' : 'light';
} catch {
  /* private window: the app sets the theme again as soon as it mounts */
}

import { createApp } from 'vue';
import { registerSW } from 'virtual:pwa-register';
import './styles/theme.css';
import App from './App.vue';
import { offerUpdate } from './composables/useOffline';
import { toast } from './composables/useToast';
import { t } from './i18n';

createApp(App).mount('#app');

/**
 * The worker is registered from the bundle, not from a snippet in the page:
 * the router serves `script-src 'self'`, which forbids inline script.
 *
 * A new version is never taken behind anyone's back — the map may be loaded,
 * a route may be on screen. The pill at the top offers the reload and this is
 * the function it calls.
 */
const updateSW = registerSW({
  immediate: true,
  onNeedRefresh: () => offerUpdate(updateSW),
  onOfflineReady: () => toast(t('offline.ready')),
});
