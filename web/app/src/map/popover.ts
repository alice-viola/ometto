/**
 * What the map popover is about. A bare coordinate offers the four ways of
 * using it; a clicked feature adds "route via here" and "avoid this"; a way
 * already avoided offers to stop; a via of the person's own offers removal.
 */
export interface PopoverState {
  lngLat: [number, number];
  x: number;
  y: number;
  name: string;
  loading: boolean;
  feature?: { name: string; kind: string };
  avoidedId?: string;
  viaIndex?: number;
}
