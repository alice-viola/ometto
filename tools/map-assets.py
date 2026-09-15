#!/usr/bin/env python3
"""
The pieces of the basemap that are not tiles: glyphs, sprites, shaded relief.

    python3 tools/map-assets.py all            # fonts + sprites + relief + LICENSES
    python3 tools/map-assets.py fonts
    python3 tools/map-assets.py sprites        # both pixel ratios, checked
    python3 tools/map-assets.py scan web/tiles/taa.pmtiles    # which glyph blocks?

Everything is fetched once from OpenFreeMap and then served by our own router,
so the app depends on nobody at runtime. Idempotent: a file already on disk is
left alone, which is what makes this safe to put in the monthly refresh.

GLYPH BLOCKS. A style asks for glyphs 256 codepoints at a time, and only for
the ranges the labels it draws actually use. OpenFreeMap serves all 256 ranges
of each stack, 105 MB for the three stacks we need, which is a lot of repo for
glyphs nothing in this region will ever render. BLOCKS below is every range
that appears in a string anywhere in the archive, Korean and Japanese exonyms
included (a style that switched to name:ko would still have its glyphs), which
is 22 MB. `scan` recomputes the list from an archive; pass --blocks all to take
everything, or --blocks 0-4,30-33 for Latin and punctuation alone.
"""
import json
import pathlib
import subprocess
import sys
import urllib.parse
from concurrent.futures import ThreadPoolExecutor

BASE = "https://tiles.openfreemap.org"
STACKS = ["Noto Sans Regular", "Noto Sans Italic", "Noto Sans Bold"]
SPRITE_SET = "ofm_f384"
SPRITE_NAME = "ofm"
# Every 256-codepoint block used by a string in web/tiles/taa.pmtiles, plus
# Latin, Greek, Cyrillic, punctuation and U+FFFD unconditionally. Regenerate
# with: python3 tools/map-assets.py scan web/tiles/taa.pmtiles
BLOCKS = ("0-6,12-14,16,30-33,48,79,81-83,92,94-95,98,110,114,126,139-141,143,"
          "172,177-178,181,184-185,188,194,197,200-201,204,209-211,214,255")
ROOT = pathlib.Path(__file__).resolve().parent.parent
TILES = ROOT / "web" / "tiles"
RELIEF_BOUNDS = None      # taken from the archive when it is there

LICENSES = """Fonts, sprites and shaded relief in this directory are third-party
assets, fetched once from OpenFreeMap (https://openfreemap.org) so that the
router can serve them itself. They are redistributed under their own licences.

fonts/Noto Sans {Regular,Italic,Bold}/*.pbf
    Noto Sans, (c) Google and contributors.
    SIL Open Font License 1.1 - https://openfontlicense.org
    Packed into MapLibre SDF glyph ranges by OpenFreeMap.

sprites/ofm{,@2x}.{png,json}
    The OpenFreeMap sprite set (ofm_f384), built from Maki (CC0, Mapbox) and
    Temaki / OSM Carto icons (CC0 / public domain), with OpenFreeMap's own
    additions. CC0 / BSD-2-Clause as published by OpenFreeMap.

natural_earth/{z}/{x}/{y}.png
    Natural Earth II shaded relief, raster tiles served by OpenFreeMap.
    Natural Earth raster data is public domain - https://www.naturalearthdata.com

taa.pmtiles (one level up)
    Built by Planetiler (Apache-2.0) from OpenStreetMap data,
    (c) OpenStreetMap contributors, ODbL 1.0 - https://www.openstreetmap.org/copyright
    in the OpenMapTiles schema (https://openmaptiles.org, BSD-3-Clause schema,
    CC-BY 4.0 for the produced tiles).

styles/{light,dark}.json
    Derived from OpenFreeMap's Liberty and Dark styles (MIT / CC0 as published
    by OpenFreeMap), with this project's corrections and local URLs.
"""


def parse_blocks(spec):
    if spec == "all":
        return list(range(256))
    out = []
    for part in spec.split(","):
        if "-" in part:
            a, b = part.split("-")
            out.extend(range(int(a), int(b) + 1))
        else:
            out.append(int(part))
    return sorted(set(out))


def get(url, out, force=False):
    out.parent.mkdir(parents=True, exist_ok=True)
    if out.exists() and out.stat().st_size > 0 and not force:
        return out.stat().st_size, True
    r = subprocess.run(["curl", "-sSf", "-m", "60", "-o", str(out), url], capture_output=True)
    if r.returncode or not out.exists() or out.stat().st_size == 0:
        out.unlink(missing_ok=True)
        return None, False
    return out.stat().st_size, False


def cmd_fonts(blocks):
    jobs = []
    for st in STACKS:
        for b in blocks:
            rng = f"{b * 256}-{b * 256 + 255}"
            jobs.append((f"{BASE}/fonts/{urllib.parse.quote(st)}/{rng}.pbf",
                         TILES / "fonts" / st / f"{rng}.pbf"))
    got = had = miss = total = 0
    with ThreadPoolExecutor(max_workers=6) as ex:
        for size, cached in ex.map(lambda j: get(*j), jobs):
            if size is None:
                miss += 1
            else:
                total += size
                had += cached
                got += not cached
    print(f"fonts: {len(STACKS)} stacks x {len(blocks)} ranges -> {got} fetched, {had} already there, "
          f"{miss} not served, {total/1e6:.1f} MB")
    return miss


def sprite_files():
    """Both pixel ratios, both halves. A retina screen asks for the @2x pair
    and nothing falls back to the 1x one: a missing ofm@2x.png is a 404 and a
    map with no icons on exactly the displays this is demoed on."""
    return [f"{SPRITE_NAME}{r}{e}" for r in ("", "@2x") for e in (".json", ".png")]


def sprite_ok(path):
    """Present, non-empty, and actually the thing it claims to be.

    A half-written file from an interrupted run is worse than a missing one:
    it is skipped as "already there" forever. So the sprite JSON has to parse
    into icons and the PNG has to start with the PNG magic, or it is fetched
    again.
    """
    if not path.exists() or path.stat().st_size == 0:
        return False
    try:
        if path.suffix == ".json":
            return len(json.loads(path.read_text())) > 0
        with open(path, "rb") as f:
            return f.read(8) == b"\x89PNG\r\n\x1a\n"
    except Exception:
        return False


def cmd_sprites():
    miss = 0
    for f in sprite_files():
        out = TILES / "sprites" / f
        bad = out.exists() and not sprite_ok(out)
        size, cached = get(f"{BASE}/sprites/{SPRITE_SET}/{f}", out, force=bad)
        state = "MISSING" if size is None else ("re-fetched (was corrupt)" if bad
                                                else "cached" if cached else "fetched")
        print(f"  sprite {f}: {state}" + (f", {size} bytes" if size else ""))
        miss += size is None or not sprite_ok(out)
    have = [f for f in sprite_files() if (TILES / "sprites" / f).exists()]
    print(f"  sprites: {len(have)} of {len(sprite_files())} files present "
          f"({', '.join(have)})")
    if miss:
        print("  !! the sprite set is incomplete; the styles will 404 on it")
    return miss


def relief_bounds():
    pm = TILES / "taa.pmtiles"
    if not pm.exists():
        return (9.86, 45.31, 13.0, 47.46)
    from pmtiles.reader import Reader, MmapSource
    with open(pm, "rb") as f:
        h = Reader(MmapSource(f)).header()
    return (h["min_lon_e7"] / 1e7, h["min_lat_e7"] / 1e7,
            h["max_lon_e7"] / 1e7, h["max_lat_e7"] / 1e7)


def cmd_relief(maxzoom=6):
    import math
    W, S, E, N = relief_bounds()

    def xy(lat, lon, z):
        n = 2 ** z
        x = int((lon + 180) / 360 * n)
        y = int((1 - math.log(math.tan(math.radians(lat)) + 1 / math.cos(math.radians(lat))) / math.pi) / 2 * n)
        return max(0, min(n - 1, x)), max(0, min(n - 1, y))

    jobs = []
    for z in range(0, maxzoom + 1):
        n = 2 ** z
        x0, y0 = xy(N, W, z)
        x1, y1 = xy(S, E, z)
        for x in range(max(0, x0 - 1), min(n - 1, x1 + 1) + 1):
            for y in range(max(0, y0 - 1), min(n - 1, y1 + 1) + 1):
                jobs.append((f"{BASE}/natural_earth/ne2sr/{z}/{x}/{y}.png",
                             TILES / "natural_earth" / str(z) / str(x) / f"{y}.png"))
    got = had = miss = total = 0
    with ThreadPoolExecutor(max_workers=4) as ex:
        for size, cached in ex.map(lambda j: get(*j), jobs):
            if size is None:
                miss += 1
            else:
                total += size
                had += cached
                got += not cached
    print(f"shaded relief z0-{maxzoom} over {W:.2f},{S:.2f}..{E:.2f},{N:.2f}: "
          f"{got} fetched, {had} already there, {miss} missing, {total/1e6:.1f} MB")
    return miss


def cmd_scan(path):
    """Which 256-codepoint blocks does this archive's text actually use?"""
    import gzip, math, collections
    from pmtiles.reader import Reader, MmapSource

    def varint(b, i):
        s = v = 0
        while True:
            c = b[i]
            i += 1
            v |= (c & 0x7F) << s
            s += 7
            if not c & 0x80:
                return v, i

    def strings(raw):
        out = []
        i, n = 0, len(raw)
        while i < n:
            k, i = varint(raw, i)
            fld, wt = k >> 3, k & 7
            if wt == 2:
                ln, i = varint(raw, i)
                ch = raw[i:i + ln]
                i += ln
                if fld == 3:
                    j = 0
                    while j < len(ch):
                        k2, j = varint(ch, j)
                        f2, w2 = k2 >> 3, k2 & 7
                        if w2 == 2:
                            l2, j = varint(ch, j)
                            c2 = ch[j:j + l2]
                            j += l2
                            if f2 == 4:
                                m = 0
                                while m < len(c2):
                                    k3, m = varint(c2, m)
                                    f3, w3 = k3 >> 3, k3 & 7
                                    if w3 == 2:
                                        l3, m = varint(c2, m)
                                        if f3 == 1:
                                            out.append(c2[m:m + l3])
                                        m += l3
                                    elif w3 == 0:
                                        _, m = varint(c2, m)
                                    elif w3 == 5:
                                        m += 4
                                    elif w3 == 1:
                                        m += 8
                                    else:
                                        break
                        elif w2 == 0:
                            _, j = varint(ch, j)
                        elif w2 == 5:
                            j += 4
                        else:
                            break
            elif wt == 0:
                _, i = varint(raw, i)
            elif wt == 5:
                i += 4
            elif wt == 1:
                i += 8
            else:
                break
        return out

    with open(path, "rb") as fh:
        r = Reader(MmapSource(fh))
        h = r.header()
        W, S, E, N = (h["min_lon_e7"] / 1e7, h["min_lat_e7"] / 1e7,
                      h["max_lon_e7"] / 1e7, h["max_lat_e7"] / 1e7)
        blocks = collections.Counter()
        for z in range(h["min_zoom"], h["max_zoom"] + 1):
            n = 2 ** z
            def xy(lat, lon):
                x = int((lon + 180) / 360 * n)
                y = int((1 - math.log(math.tan(math.radians(lat)) + 1 / math.cos(math.radians(lat))) / math.pi) / 2 * n)
                return x, y
            x0, y0 = xy(N, W)
            x1, y1 = xy(S, E)
            for x in range(x0, x1 + 1):
                for y in range(y0, y1 + 1):
                    t = r.get(z, x, y)
                    if not t:
                        continue
                    raw = gzip.decompress(t) if t[:2] == b"\x1f\x8b" else t
                    for b_ in strings(raw):
                        try:
                            s = b_.decode("utf-8")
                        except UnicodeDecodeError:
                            continue
                        for ch in s:
                            blocks[ord(ch) // 256] += 1
    b = sorted(set(blocks) | {0, 1, 2, 3, 4, 30, 31, 32, 33, 255})
    runs, st, pv = [], b[0], b[0]
    for x in b[1:]:
        if x == pv + 1:
            pv = x
            continue
        runs.append((st, pv))
        st = pv = x
    runs.append((st, pv))
    spec = ",".join(f"{a}-{c}" if a != c else str(a) for a, c in runs)
    print(f"{len(b)} blocks used; BLOCKS = \"{spec}\"")
    return 0


def main(argv):
    if not argv:
        sys.exit(__doc__.strip())
    cmd = argv[0]
    blocks = parse_blocks(argv[argv.index("--blocks") + 1] if "--blocks" in argv else BLOCKS)
    if cmd == "scan":
        return cmd_scan(argv[1] if len(argv) > 1 else TILES / "taa.pmtiles")
    miss = 0
    if cmd in ("fonts", "all"):
        miss += cmd_fonts(blocks)
    if cmd in ("sprites", "all"):
        miss += cmd_sprites()
    if cmd in ("relief", "all"):
        miss += cmd_relief()
    if cmd in ("licenses", "all"):
        (TILES / "LICENSES").write_text(LICENSES)
        print(f"wrote {TILES/'LICENSES'}")
    if cmd not in ("fonts", "sprites", "relief", "licenses", "all"):
        sys.exit(__doc__.strip())
    return 1 if miss else 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
