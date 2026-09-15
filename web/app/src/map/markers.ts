import type { Tokens } from './style';

export type MarkerRole = 'start' | 'stop' | 'destination' | 'parking' | 'via';

/**
 * Start reads as a ring, destination as a pin, stops as numbered beads.
 * `thumb` pads the element out to a finger's width without changing what is
 * drawn; the pin keeps its tip on the point, so it is padded on three sides.
 */
export function markerElement(role: MarkerRole, t: Tokens, index?: number, thumb = false): HTMLElement {
  const el = document.createElement('div');
  el.className = 'marker-pin';
  el.style.willChange = 'transform';
  if (thumb) el.style.padding = role === 'destination' ? '10px 10px 0' : '10px';
  if (role === 'destination') {
    el.innerHTML = `<svg width="26" height="34" viewBox="0 0 26 34" aria-hidden="true">
      <path d="M13 33C13 33 24 20.4 24 13A11 11 0 1 0 2 13c0 7.4 11 20 11 20z"
            fill="${t.dest}" stroke="${t.surface}" stroke-width="2"/>
      <circle cx="13" cy="13" r="4.2" fill="${t.surface}"/>
    </svg>`;
  } else if (role === 'start') {
    el.innerHTML = `<svg width="24" height="24" viewBox="0 0 24 24" aria-hidden="true">
      <circle cx="12" cy="12" r="9" fill="${t.surface}" stroke="${t.ink}" stroke-width="3"/>
      <circle cx="12" cy="12" r="3.4" fill="${t.ink}"/>
    </svg>`;
  } else if (role === 'via') {
    // Shape, not a destination: a small bead on the line, clearly lesser than
    // the places either side of it.
    el.innerHTML = `<svg width="16" height="16" viewBox="0 0 16 16" aria-hidden="true">
      <circle cx="8" cy="8" r="5.4" fill="${t.accent}" stroke="${t.surface}" stroke-width="2.2"/>
    </svg>`;
  } else if (role === 'parking') {
    // Where the car is left: a plate, not another waypoint bead.
    el.innerHTML = `<svg width="22" height="22" viewBox="0 0 22 22" aria-hidden="true">
      <rect x="1.6" y="1.6" width="18.8" height="18.8" rx="5.5"
            fill="${t.surface}" stroke="${t.ink}" stroke-width="2"/>
      <text x="11" y="15.4" text-anchor="middle" font-size="12" font-weight="700"
            font-family="ui-sans-serif, system-ui, sans-serif" fill="${t.ink}">P</text>
    </svg>`;
  } else {
    el.innerHTML = `<svg width="22" height="22" viewBox="0 0 22 22" aria-hidden="true">
      <circle cx="11" cy="11" r="8.5" fill="${t.surface}" stroke="${t.ink}" stroke-width="2"/>
      <text x="11" y="14.6" text-anchor="middle" font-size="10" font-weight="600"
            font-family="ui-sans-serif, system-ui, sans-serif" fill="${t.ink}">${index ?? ''}</text>
    </svg>`;
  }
  return el;
}
