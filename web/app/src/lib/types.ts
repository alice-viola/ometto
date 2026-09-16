/** The wire contract of the routing service, verbatim. */

export type Mode = 'car' | 'bike' | 'hike' | 'car+hike' | 'bike+hike';
/** A leg is one way of moving. A lift is ridden, never walked. */
export type LegMode = 'car' | 'bike' | 'hike' | 'lift';
/** SAT grades, plus Alpine: unmarked ground above the marked-path scale. */
export type Grade = 'T' | 'E' | 'EE' | 'EEA' | 'A';
export type LiftType = 'cable_car' | 'gondola' | 'chair_lift' | 'mixed_lift';
export type PlaceKind = 'place' | 'peak' | 'hut' | 'pass' | 'crag' | 'street' | 'trail';

export interface LngLat {
  lat: number;
  lon: number;
}

/** A point in the query: a coordinate that may carry the name it was found by. */
export interface Waypoint extends LngLat {
  name?: string;
  kind?: PlaceKind;
  /**
   * Routed through, never stopped at: no leg break, no step break, never the
   * parking. Up to 12 points in all, of which 6 may be real stops.
   */
  via?: boolean;
}

export interface GeocodeResult {
  id: string;
  name: string;
  kind: PlaceKind;
  locality?: string;
  lat: number;
  lon: number;
  ele?: number;
  /** A crag's grade span, aspect and route count, as far as the mapping says. */
  detail?: string;
}

export interface ReverseResult {
  name: string;
  kind: PlaceKind;
  lat: number;
  lon: number;
}

export interface LineString {
  type: 'LineString';
  coordinates: [number, number][];
}

/** [metres along the route, elevation in metres] */
export type ProfilePoint = [number, number];

export interface Leg {
  mode: LegMode;
  /** The lift's own name, on a `lift` leg. */
  name?: string;
  liftType?: LiftType;
  seconds: number;
  meters: number;
  ascent: number;
  descent: number;
  grade?: Grade;
  classes?: Record<string, number>;
  geometry: LineString;
  profile?: ProfilePoint[];
}

export interface Step {
  name: string;
  mode: LegMode;
  meters: number;
  seconds: number;
}

export interface RouteAlternative {
  id: string;
  seconds: number;
  meters: number;
  ascent: number;
  descent: number;
  /** The walking part on its own: the headline must never bill road climb to it. */
  walkMeters?: number;
  walkAscent?: number;
  /**
   * Where the car or bike is left, on a two-mode plan. `point` indexes the
   * request's points; it is null when the switch happens mid-leg at a
   * trailhead the caller never named. Absent on single-mode plans, and absent
   * entirely from services that predate the field.
   */
  parking?: {
    point: number | null;
    name?: string;
    lat?: number;
    lon?: number;
  };
  grade?: Grade;
  legs: Leg[];
  /**
   * The whole line. Not on the wire since 2026-09-15 — the legs carry it, once
   * — and filled by `api.route()` from them (`joinLegs`). The profile likewise
   * lives on the legs; the profile component joins those itself.
   */
  geometry: LineString;
  profile?: ProfilePoint[];
  steps?: Step[];
  warnings?: string[];
}

/** A way the answer must not use, given as a coordinate on it. */
export interface AvoidPoint {
  lat: number;
  lon: number;
}

/** What the service resolved an avoid coordinate to, with its geometry. */
export interface AvoidedWay {
  id: string;
  name?: string;
  class?: string;
  points?: [number, number][];
}

export interface RouteRequest {
  /** {lat, lon} per the contract; the name is an additive hint the service
      stores with the query so history can show the place, not the street. */
  points: Waypoint[];
  mode: Mode;
  grade: Grade;
  alternatives: 1 | 3;
  user: string;
}

/** Where a point actually landed on the network, and how far it had to go. */
export interface SnappedPoint {
  lat: number;
  lon: number;
  name?: string;
  via?: boolean;
  /** Metres between the point that was asked for and the junction it landed on. */
  distance?: number;
}

export interface RouteResponse {
  routes: RouteAlternative[];
  snapped?: SnappedPoint[];
  avoided?: AvoidedWay[];
  /** The answer stands, but the service was working with less than usual. */
  degraded?: boolean;
  /** Present when the same question has an answer at a harder grade. */
  neededGrade?: Grade;
  computedMs?: number;
  reason?: string;
  /**
   * Never on the wire. Set by the page when the service could not be reached
   * and the answer came out of this browser's saved routes instead, so the
   * card can say how old it is.
   */
  fromSaved?: { at: number };
}

export interface Favourite {
  id: string;
  name: string;
  lat: number;
  lon: number;
  kind?: PlaceKind;
  createdAt?: string | number;
}

export interface HistoryEntry {
  id: string;
  at: string | number;
  request: {
    points: Waypoint[];
    mode: Mode;
    grade: Grade;
    alternatives: 1 | 3;
    lifts?: boolean;
    avoid?: AvoidPoint[];
  };
  summary: {
    seconds: number;
    meters: number;
    ascent: number;
    descent: number;
    fromName?: string;
    toName?: string;
  };
}

export interface Health {
  ok: boolean;
  region?: string;
  graph?: { junctions: number; stretches: number; withElevation: number };
}

export interface FeatureCollection {
  type: 'FeatureCollection';
  features: GeoFeature[];
}

export interface GeoFeature {
  type: 'Feature';
  geometry: { type: string; coordinates: unknown };
  properties: Record<string, unknown>;
}
