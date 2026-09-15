/** Small typed wrapper over localStorage: never throws, never blocks a render. */

const PREFIX = 'trp.';

export function readLocal<T>(key: string, fallback: T): T {
  try {
    const raw = localStorage.getItem(PREFIX + key);
    if (raw === null) return fallback;
    return JSON.parse(raw) as T;
  } catch {
    return fallback;
  }
}

export function writeLocal(key: string, value: unknown): void {
  try {
    localStorage.setItem(PREFIX + key, JSON.stringify(value));
  } catch {
    /* private window, full quota: the app keeps working without it */
  }
}

export function readLocalRaw(key: string): string | null {
  try {
    return localStorage.getItem(PREFIX + key);
  } catch {
    return null;
  }
}

export function writeLocalRaw(key: string, value: string): void {
  try {
    localStorage.setItem(PREFIX + key, value);
  } catch {
    /* ignored */
  }
}

/** 128 bits of randomness as 32 lowercase hex characters. */
function newId(): string {
  const bytes = new Uint8Array(16);
  if (typeof crypto !== 'undefined' && crypto.getRandomValues) crypto.getRandomValues(bytes);
  else for (let i = 0; i < 16; i++) bytes[i] = Math.floor(Math.random() * 256);
  return [...bytes].map((b) => b.toString(16).padStart(2, '0')).join('');
}

const ID_SHAPE = /^[0-9a-f]{32}$/;

let cachedUser: string | null = null;
let replaced = false;

/**
 * The anonymous identity that owns this browser's history and favourites.
 * Anything that is not 32 hex characters predates this shape and is replaced;
 * the lists under the old id stay on the server but are no longer addressed,
 * which the About panel says once.
 */
export function userId(): string {
  if (cachedUser) return cachedUser;
  const existing = readLocalRaw('user');
  if (existing && ID_SHAPE.test(existing)) {
    cachedUser = existing;
    return cachedUser;
  }
  if (existing) {
    replaced = true;
    writeLocalRaw('user.replaced', '1');
  }
  cachedUser = newId();
  writeLocalRaw('user', cachedUser);
  return cachedUser;
}

/** True when this browser's old, shorter id was retired at some point. */
export function userIdWasReplaced(): boolean {
  return replaced || readLocalRaw('user.replaced') === '1';
}
