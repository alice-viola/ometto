import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import { defineConfig } from 'vite';
import vue from '@vitejs/plugin-vue';
import tailwindcss from '@tailwindcss/vite';

// The API server on :8100 serves this bundle from its own root with an
// index.html fallback, so a relative base keeps it mountable anywhere.
const API = process.env.API ?? 'http://localhost:8100';

/**
 * Dev only. Lets the frontend exercise the runtime-config path before the
 * router serves /api/config: if the real API answers 404, this answers with a
 * plausible config instead. Never part of a build.
 */
function stubConfig() {
  return {
    name: 'stub-api-config',
    apply: 'serve' as const,
    configureServer(server: { middlewares: { use: (fn: unknown) => void } }) {
      server.middlewares.use((req: { url?: string }, res: never, next: () => void) => {
        if (process.env.STUB_CONFIG !== '1') return next();
        const r0 = res as unknown as {
          setHeader: (k: string, v: string) => void;
          end: (b: string) => void;
        };
        // Serve the corrected styles at the paths the router will use, so the
        // whole config -> style path can be walked before the router exists.
        const style = /^\/map\/styles\/(light|dark)\.json/.exec(req.url ?? '');
        if (style) {
          r0.setHeader('content-type', 'application/json');
          r0.end(readFileSync(resolve(__dirname, `styles-source/${style[1]}.json`), 'utf8'));
          return;
        }
        if (!req.url?.startsWith('/api/config')) return next();
        const body = JSON.stringify({
          styles: { light: '/map/styles/light.json', dark: '/map/styles/dark.json' },
          terrain: {
            url: 'https://s3.amazonaws.com/elevation-tiles-prod/terrarium/{z}/{x}/{y}.png',
            attribution: 'Elevation: Copernicus GLO-30 via AWS Terrain Tiles',
          },
          dataDates: { osmExtract: '2026-09-14', satCadastre: '2026-06-30', buildDate: '2026-09-15' },
          disclaimerVersion: '2',
          publicUrl: 'https://quota.example.org',
        });
        const r = res as unknown as {
          setHeader: (k: string, v: string) => void;
          end: (b: string) => void;
        };
        r.setHeader('content-type', 'application/json');
        r.end(body);
      });
    },
  };
}

export default defineConfig({
  base: './',
  plugins: [vue(), tailwindcss(), stubConfig()],
  build: {
    outDir: 'dist',
    target: 'es2022',
    sourcemap: false,
    chunkSizeWarningLimit: 1400,
    rollupOptions: {
      output: {
        // MapLibre is by far the biggest dependency: keep it in its own chunk so
        // the panel can paint before the map engine has finished parsing.
        manualChunks: (id) => (id.includes('maplibre') ? 'map' : undefined),
      },
    },
  },
  server: {
    port: 5173,
    strictPort: true,
    // `/map` as well as `/api`: the basemap, glyphs and sprites are served by
    // the same box, so without this the dev server renders an empty map.
    proxy: {
      '/api': { target: API, changeOrigin: true },
      '/map': { target: API, changeOrigin: true },
    },
  },
  preview: {
    port: 4173,
    strictPort: true,
    // `/map` as well as `/api`: the basemap, glyphs and sprites are served by
    // the same box, so without this the dev server renders an empty map.
    proxy: {
      '/api': { target: API, changeOrigin: true },
      '/map': { target: API, changeOrigin: true },
    },
  },
});
