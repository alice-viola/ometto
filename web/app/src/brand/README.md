# The Ometto mark

An *ometto* is the stone man: rocks above rocks, stacked beside a path to say
someone has been this way and the way goes on. The mark is that cairn.

It is **generated, not drawn**. `cairn.mjs` holds the parameters and the code
that turns them into geometry; every size, every export and the component in
the header come from that one file. There is no traced outline anywhere, and
nothing to redraw when a number changes.

## Parameters

All lengths are fractions of the **base width** — the width of the bottom
stone. The base is 100 units internally, so 0.78 means 78 units.

| Parameter | Value | What it does |
|---|---|---|
| `widths` | `[1, 0.78, 0.6, 0.42]` | Stone widths, bottom to top. Each stone is about four fifths of the one below. |
| `heightRatio` | `0.48` | Each stone's height as a fraction of **its own** width, so the stones stay in proportion as they narrow. |
| `gap` | `0.10` | Air between stones. |
| `sway` | `0.06` | Alternating horizontal offset. The stack is balanced by hand, not by a machine. |
| `tilt` | `8°` | The top stone only. A cairn's last stone is never square to the world. |
| `strokeRatio` | `1/12` | Outline weight, for the outline variant. |
| `squircle` | `[2.5, 3.1, 2.7, 3.4]` | Superellipse exponent per quadrant, clockwise from top right. |
| `pad` | `0.10` | Breathing room around the stack. |

### Why a superellipse, and why uneven

A stone is not a rounded rectangle. A rounded rectangle has four identical
corners and reads as a user-interface element; a superellipse has a continuous
curve with no straight run, and reads as something worn. Giving each quadrant a
slightly different exponent means no two corners of a stone match, which is
what stops the shape looking manufactured. The exponents are close enough
together (2.5 to 3.4) that the stone still reads as one solid form.

### The small-size variant

Under 24 px the four-stone stack cannot hold its gaps: at 16 px each gap is
under a pixel and the top two stones merge into a grey mass. `CAIRN_SMALL`
generates the same geometry from three chunkier stones instead:

| Parameter | Value |
|---|---|
| `widths` | `[1, 0.72, 0.46]` |
| `heightRatio` | `0.62` |
| `gap` | `0.13` |
| `sway` | `0.05` |

`specFor(size)` picks between them, so a caller only says how big it wants the
mark. This was decided by rendering both at 16, 20, 24 and 32 px, magnifying
eight times with nearest-neighbour, and looking: at `gap: 0.08` the four-stone
top merged at 16 **and** 20 px, at `0.10` it holds from 24 px up, and the
three-stone set stays separable down to 16 px.

## Colour

One ink, no gradients, no second colour. The mark takes `currentColor` in the
app, so the theme decides: `--ink` on `--bg`, either way round. Exported files
use `#15181c` (light) and `#e9ebee` (dark). The touch icon is the only file
with a ground of its own: the app's paper, `#f4f4f2`, because iOS does not
honour transparency.

## Files, and how to rebuild them

```
npm run brand      # regenerates everything below into public/
```

| File | What it is |
|---|---|
| `favicon.svg` | The mark, ink, transparent. |
| `favicon.ico` | 16, 32 and 48 px PNGs in one container. |
| `apple-touch-icon.png` | 180 px on paper. |
| `icon-192.png`, `icon-512.png` | For the web manifest. |
| `mark-24.svg`, `mark-32.svg` | The header mark at its two sizes, for reference. |
| `mark-dark.svg`, `mark-outline.svg` | The dark and outline variants. |
| `og.png` | 1200x630, mark and wordmark, for link previews. |

The PNGs are rasterised in `scripts/brand-export.mjs` by filling the same
superellipses the SVG describes, 4x4 supersampled — they are the same geometry,
not a screenshot of it. `og.png` is the one exception: it needs real type, so
it is composed on a canvas in the browser and posted back.

## The wordmark

"Ometto" in the interface font at medium weight, with "Trentino-Alto Adige"
beneath it in the muted tone. No custom lettering: the mark carries the
identity, and the word should look like the rest of the product.

## Seeing it at real size

`proof-sheet.html`, in this directory, is the sheet the mark was judged on.
Open it straight from the file system — every exported file is inlined, so it
needs no server. It shows:

- the mark at **16, 24, 32, 64 and 256 px**, on paper and on ink, filled and outline;
- the same sizes magnified x8, pixel by pixel, which is where the small-size
  variant earns its place;
- "in place" mocks — the panel header and a browser tab — because a mark is
  never seen on its own;
- every exported file, at its own size;
- the parameter table, so the sheet and this document cannot drift apart.

Regenerate it after changing `cairn.mjs`: `npm run brand`, then rerun
`scripts/proof.mjs` and inline the exports again.
