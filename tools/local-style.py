#!/usr/bin/env python3
"""
Point a MapLibre style at our own tiles instead of a public tile server.

    python3 tools/local-style.py web/app/styles-source/light.json \
            web/tiles/styles/light.json --pmtiles web/tiles/taa.pmtiles

The frontend keeps the styles it actually wants (OpenFreeMap's liberty and its
corrected dark) under web/app/styles-source with the vendor URLs still in them,
because that is what it edits against. This turns one of those into the served
style: every source, the glyphs and the sprite are rewritten to the router's
/map/ paths, the vector source gets the archive's own zoom range and bounds
(without them MapLibre asks for tiles outside the region and paints holes), and
the attribution that was coming from the vendor's TileJSON is written into the
style, because a style with tiles in it no longer fetches that TileJSON.

Nothing else is touched: the layers, the filters and the colours come through
exactly as the frontend wrote them.
"""
import json
import pathlib
import sys
import time

ATTRIB_VECTOR = (
    '<a href="https://www.openstreetmap.org/copyright" target="_blank">'
    "&copy; OpenStreetMap contributors</a> "
    '<a href="https://www.openmaptiles.org/" target="_blank">&copy; OpenMapTiles</a> '
    '<a href="https://openfreemap.org/" target="_blank">&copy; OpenFreeMap</a>'
)
ATTRIB_RASTER = (
    '<a href="https://www.naturalearthdata.com/" target="_blank">Natural Earth</a> '
    '<a href="https://openfreemap.org/" target="_blank">&copy; OpenFreeMap</a>'
)
VENDOR = "openfreemap.org"


def archive_info(path):
    """min zoom, max zoom and bounds, read from the PMTiles header itself.

    Taking them from the archive rather than from the command line is what
    keeps the style honest when the next build changes the zoom range.
    """
    from pmtiles.reader import Reader, MmapSource
    with open(path, "rb") as f:
        h = Reader(MmapSource(f)).header()
        return {
            "minzoom": int(h["min_zoom"]), "maxzoom": int(h["max_zoom"]),
            "bounds": [round(h["min_lon_e7"] / 1e7, 6), round(h["min_lat_e7"] / 1e7, 6),
                       round(h["max_lon_e7"] / 1e7, 6), round(h["max_lat_e7"] / 1e7, 6)],
        }


def main(argv):
    if len(argv) < 2:
        sys.exit(__doc__.strip())
    src, out = pathlib.Path(argv[0]), pathlib.Path(argv[1])
    pm = argv[argv.index("--pmtiles") + 1] if "--pmtiles" in argv else "web/tiles/taa.pmtiles"
    prefix = argv[argv.index("--prefix") + 1] if "--prefix" in argv else "/map"
    sprite = argv[argv.index("--sprite") + 1] if "--sprite" in argv else "ofm"
    name = argv[argv.index("--name") + 1] if "--name" in argv else out.stem

    info = archive_info(pm)
    d = json.loads(src.read_text())

    d["name"] = f"Trentino-Alto Adige {name}"
    d["glyphs"] = f"{prefix}/fonts/{{fontstack}}/{{range}}.pbf"
    d["sprite"] = f"{prefix}/sprites/{sprite}"

    rewritten = []
    for key, s in d.get("sources", {}).items():
        was = s.get("url") or (s.get("tiles") or [""])[0]
        if s.get("type") == "vector":
            s.pop("url", None)
            s["tiles"] = [f"{prefix}/tiles/{{z}}/{{x}}/{{y}}.pbf"]
            s["minzoom"] = info["minzoom"]
            s["maxzoom"] = info["maxzoom"]
            s["bounds"] = info["bounds"]
            s["attribution"] = ATTRIB_VECTOR
        elif s.get("type") == "raster":
            s.pop("url", None)
            s["tiles"] = [f"{prefix}/natural_earth/{{z}}/{{x}}/{{y}}.png"]
            s.setdefault("tileSize", 256)
            s.setdefault("maxzoom", 6)
            # Bounds here too: we only hold the relief over the archive, and
            # without them MapLibre asks for the rest of the world and gets 404s.
            s["bounds"] = info["bounds"]
            s["attribution"] = ATTRIB_RASTER
        else:
            continue
        rewritten.append((key, s["type"], was))

    d["metadata"] = {
        **(d.get("metadata") or {}),
        "queen:source": str(src),
        "queen:archive": str(pm),
        "queen:built": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
    }

    blob = json.dumps(d, indent=2, ensure_ascii=False)
    left = blob.count(VENDOR)
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(blob + "\n")
    print(f"{src} -> {out} ({out.stat().st_size/1024:.0f} KB, {len(d['layers'])} layers)")
    for k, t, was in rewritten:
        print(f"  source {k} ({t}): {was or '(inline)'} -> {d['sources'][k]['tiles'][0]}")
    print(f"  glyphs {d['glyphs']}   sprite {d['sprite']}")
    print(f"  vector source: zoom {info['minzoom']}..{info['maxzoom']}, bounds {info['bounds']}")
    if left:
        print(f"  !! {left} mention(s) of {VENDOR} left in the style "
              f"(attribution links are fine, a tile URL is not)")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
