/** Point symbols drawn at runtime so they take the current ink colour. */
export type IconKind = 'peak' | 'hut' | 'pass' | 'crag' | 'place';

const SIZE = 26; // device pixels, added at pixelRatio 2 -> 13 css px

export function makeIcon(kind: IconKind, ink: string, halo: string) {
  const c = document.createElement('canvas');
  c.width = SIZE;
  c.height = SIZE;
  const g = c.getContext('2d');
  if (!g) return { width: SIZE, height: SIZE, data: new Uint8Array(SIZE * SIZE * 4) };
  g.lineJoin = 'round';
  g.lineCap = 'round';

  const draw = (stroke: boolean) => {
    g.beginPath();
    if (kind === 'peak') {
      g.moveTo(3, 21);
      g.lineTo(13, 5);
      g.lineTo(23, 21);
      g.closePath();
    } else if (kind === 'hut') {
      g.moveTo(4, 12);
      g.lineTo(13, 5);
      g.lineTo(22, 12);
      g.lineTo(22, 21);
      g.lineTo(4, 21);
      g.closePath();
    } else if (kind === 'pass') {
      // A saddle: two slopes meeting low in the middle.
      g.moveTo(3, 20);
      g.quadraticCurveTo(9, 20, 13, 12);
      g.quadraticCurveTo(17, 20, 23, 20);
      g.lineTo(23, 22);
      g.lineTo(3, 22);
      g.closePath();
    } else if (kind === 'crag') {
      // A wall: a block with a slanted top, the way a cliff reads from below.
      g.moveTo(5, 21);
      g.lineTo(5, 9);
      g.lineTo(11, 4);
      g.lineTo(21, 8);
      g.lineTo(21, 21);
      g.closePath();
    } else {
      g.arc(13, 13, 5.5, 0, Math.PI * 2);
    }
    if (stroke) {
      g.strokeStyle = halo;
      g.lineWidth = 4.5;
      g.stroke();
    } else {
      g.fillStyle = ink;
      g.fill();
    }
  };

  draw(true);
  draw(false);

  if (kind === 'hut') {
    // A lit window reads as shelter rather than as a generic building.
    g.fillStyle = halo;
    g.fillRect(11, 14, 4, 5);
  }
  if (kind === 'crag') {
    // A crack down the face: rock, not a building.
    g.strokeStyle = halo;
    g.lineWidth = 1.6;
    g.beginPath();
    g.moveTo(13.5, 9);
    g.lineTo(11.5, 13.5);
    g.lineTo(13.5, 18.5);
    g.stroke();
  }

  const img = g.getImageData(0, 0, SIZE, SIZE);
  return { width: SIZE, height: SIZE, data: new Uint8Array(img.data.buffer.slice(0)) };
}

/**
 * A stand-in for an image a style asks for but its sprite does not carry.
 * The dark style names the city dot `circle-11` while the shared sprite calls
 * it `circle_11_black`, so MapLibre warns on every load. Drawing the dot is
 * both truer to the intent and quieter than leaving it missing.
 */
export function makeFallbackImage(id: string, ink: string) {
  const size = 22;
  const c = document.createElement('canvas');
  c.width = size;
  c.height = size;
  const g = c.getContext('2d');
  if (!g) return { width: 1, height: 1, data: new Uint8Array(4) };
  if (/circle|dot/i.test(id)) {
    g.beginPath();
    g.arc(size / 2, size / 2, 4.5, 0, Math.PI * 2);
    g.fillStyle = ink;
    g.fill();
  }
  // Anything else stays deliberately blank: the icon was missing either way,
  // and a registered blank is what stops the warning.
  const img = g.getImageData(0, 0, size, size);
  return { width: size, height: size, data: new Uint8Array(img.data.buffer.slice(0)) };
}
