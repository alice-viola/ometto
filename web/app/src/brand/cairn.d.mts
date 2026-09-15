export interface CairnSpec {
  widths: number[];
  heightRatio: number;
  gap: number;
  sway: number;
  tilt: number;
  strokeRatio: number;
  squircle: number[];
  pad: number;
}
export const CAIRN: CairnSpec;
export const CAIRN_SMALL: CairnSpec;
export function specFor(size: number): CairnSpec;
export function cairnGeometry(opts?: {
  base?: number;
  spec?: CairnSpec;
  stones?: number;
}): { paths: string[]; stroke: number; box: { x: number; y: number; w: number; h: number } };
export function cairnSvg(opts?: {
  size?: number;
  color?: string;
  background?: string | null;
  variant?: 'filled' | 'outline';
  stones?: number;
  spec?: CairnSpec;
  square?: boolean;
  title?: string;
}): string;
export function cairnInner(opts?: {
  variant?: 'filled' | 'outline';
  stones?: number;
  spec?: CairnSpec;
}): string;
export function cairnViewBox(opts?: { spec?: CairnSpec; stones?: number }): {
  x: number;
  y: number;
  w: number;
  h: number;
};
