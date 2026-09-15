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
import './styles/theme.css';
import App from './App.vue';

createApp(App).mount('#app');
