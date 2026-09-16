/// <reference types="vite/client" />
/// <reference types="vite-plugin-pwa/client" />

declare module '*.vue' {
  import type { DefineComponent } from 'vue';
  const component: DefineComponent<object, object, unknown>;
  export default component;
}

declare module 'maplibre-contour' {
  const mlcontour: {
    DemSource: new (opts: {
      url: string;
      encoding?: 'terrarium' | 'mapbox';
      maxzoom: number;
      worker?: boolean;
      cacheSize?: number;
      timeoutMs?: number;
    }) => {
      sharedDemProtocolUrl: string;
      setupMaplibre(m: unknown): void;
      contourProtocolUrl(options: {
        thresholds: Record<number, number | number[]>;
        elevationKey?: string;
        levelKey?: string;
        contourLayer?: string;
        overzoom?: number;
        multiplier?: number;
      }): string;
    };
  };
  export default mlcontour;
}
