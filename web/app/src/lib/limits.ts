/**
 * What one question can hold. The router refuses anything past these
 * (cmd/router/server.go), and a shared link opens with no more than they allow,
 * so the planner, the link and the server have to agree on them.
 */

/** Places: start, stops, destination. */
export const MAX_POINTS = 20;
/** The whole sequence, vias included. */
export const MAX_SEQUENCE = 40;
export const MAX_AVOIDS = 20;
