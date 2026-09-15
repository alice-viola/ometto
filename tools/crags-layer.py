#!/usr/bin/env python3
"""
The climbing crags as a GeoJSON point layer.

    python3 tools/crags-layer.py data/osm-taa/crags.json web/public/crags.geojson [--no-dem]

One point per named crag, sector or climbing area that tools/pbf-layers.py
pulled out of OpenStreetMap, with what a climber asks first: the grade span,
the aspect, the rock, how many routes -- and, from the Copernicus DEM when the
mapper did not say, how high the base is. OSM's climbing tagging has had three
conventions in fifteen years and the region has all three, so the tags are
normalised here rather than in the page:

    climbing:grade:french:min/max (or :mean; or the UIAA ones)  -> grades  "4a–7c"
    climbing:orientation  "s", "south", "SW;S"                  -> aspect  "S", "SW/S"
    climbing:rock                                                -> rock (never "yes")
    climbing:routes, else the routes a site relation groups      -> routes
    climbing:length                                              -> length, metres
    climbing:sport / boulder / trad / multipitch / ice           -> styles

The same wall is often a node AND a cliff line, or a site relation AND its
sectors: two rows within 150 m that share a word that is not the word for
"crag" are one crag, the richer row kept and the other filling its gaps. A
sector that is only "Settore B" gets the wall it belongs to as `parent`: the
relation it is a member of when there is one, else the nearest crag with a
name of its own within 400 m.

Properties: name, kind ("crag"), group (true for a climbing area), parent,
ele, ele_src ("dem" when sampled), grades, aspect, rock, routes, length,
styles, website, source ("osm"), osm_type, osm_id.
"""
import json
import math
import pathlib
import re
import sys
import unicodedata

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))

# The words that say WHAT a thing is, not WHICH: two rows sharing only these
# are not the same crag.
GENERIC = {
    "falesia", "falesie", "klettergarten", "crag", "crags", "parete", "pareti",
    "placca", "placche", "settore", "sector", "sektor", "block", "blocco",
    "blocchi", "masso", "massi", "boulder", "zona", "area", "parte", "lato",
    "wand", "pfeiler", "pilastro", "torre", "turm", "sasso", "sass", "cima",
    "di", "del", "della", "dei", "delle", "da", "dal", "dalla", "la", "il",
    "le", "lo", "al", "alla", "ai", "alle", "in", "su", "sul", "sulla", "ed",
    "der", "die", "das", "dem", "den", "am", "an", "im", "zum", "zur", "von",
    "auf", "und", "the", "of",
}
# A name that is only a sector's: it needs the wall's name beside it.
SECTOR = re.compile(r"^(settore|sector|sektor|block|blocco|zona|area|parte|lato|teil|bereich)\b|^[a-z]{1,2}$|^[a-z]?\d{1,3}[a-z]?$")
COMPASS = {"N", "NNE", "NE", "ENE", "E", "ESE", "SE", "SSE",
           "S", "SSW", "SW", "WSW", "W", "WNW", "NW", "NNW"}
WORDS = {"north": "N", "nord": "N", "south": "S", "sud": "S", "sued": "S", "süd": "S",
         "east": "E", "est": "E", "ost": "E", "west": "W", "ovest": "W",
         "northeast": "NE", "nordest": "NE", "nordost": "NE",
         "southeast": "SE", "sudest": "SE", "suedost": "SE",
         "southwest": "SW", "sudovest": "SW", "suedwest": "SW",
         "northwest": "NW", "nordovest": "NW", "nordwest": "NW"}
STYLES = (("climbing:sport", "sport"), ("climbing:boulder", "boulder"),
          ("climbing:trad", "trad"), ("climbing:multipitch", "multipitch"),
          ("climbing:ice", "ice"))
TYPE_RANK = {"relation": 0, "node": 1, "way": 2}


def fold(s):
    s = unicodedata.normalize("NFKD", s or "")
    s = "".join(c for c in s if not unicodedata.combining(c)).lower()
    return " ".join(re.sub(r"[^a-z0-9 ]+", " ", s).split())


def words(name):
    return {w for w in fold(name).split() if len(w) >= 3 and w not in GENERIC}


def sectorish(name):
    return bool(SECTOR.match(fold(name)))


def num(v, biggest=False):
    """The number in a tag: "20", "12-25 m", "ca. 30". None when there is none."""
    found = [float(x.replace(",", ".")) for x in re.findall(r"\d+(?:[.,]\d+)?", v or "")]
    if not found:
        return None
    return int(round(max(found) if biggest else found[0]))


def grade(v, scale):
    v = (v or "").strip().replace(" ", "")
    if not v:
        return ""
    return v.lower() if scale == "french" else v.upper()


def grades(t):
    """"4a–7c" from the min/max pair, the mean alone when that is all, French
    before UIAA because that is what the region's guidebooks use."""
    for scale in ("french", "uiaa"):
        lo, hi, mean = (grade(t.get(f"climbing:grade:{scale}:{k}"), scale)
                        for k in ("min", "max", "mean"))
        if lo and hi:
            return lo if lo == hi else f"{lo}–{hi}"
        if lo or hi or mean:
            return lo or hi or mean
        single = grade(t.get(f"climbing:grade:{scale}"), scale)
        if single:
            return single
    return ""


def aspect(v):
    out = []
    for part in re.split(r"[;,/|+\s]+", (v or "").strip()):
        p = WORDS.get(part.lower(), part).upper()
        if p in COMPASS and p not in out:
            out.append(p)
    return "/".join(out)


def styles(t):
    return [label for key, label in STYLES
            if (t.get(key) or "").strip().lower() not in ("", "no", "0")]


def row_of(e):
    t = e["tags"]
    name = " ".join((t.get("name") or "").split())
    if not name:
        return None
    r = {"name": name, "kind": "crag", "lat": e["lat"], "lon": e["lon"],
         "osm_type": e["type"], "osm_id": e["id"]}
    if t.get("climbing") == "area":
        r["group"] = True
    if e.get("parent"):
        r["parent"] = e["parent"]
    ele = num(t.get("ele"))
    if ele:
        r["ele"] = ele
    g = grades(t)
    if g:
        r["grades"] = g
    a = aspect(t.get("climbing:orientation"))
    if a:
        r["aspect"] = a
    rock = (t.get("climbing:rock") or "").strip().lower()
    if rock and rock != "yes":
        r["rock"] = rock
    n = num(t.get("climbing:routes")) or int(e.get("routes") or 0)
    if n > 0:
        r["routes"] = n
    length = num(t.get("climbing:length"), biggest=True)
    if length:
        r["length"] = length
    st = styles(t)
    if st:
        r["styles"] = st
    if (t.get("website") or "").strip():
        r["website"] = t["website"].strip()
    return r


def dist_m(a, b):
    dlat = (a["lat"] - b["lat"]) * 111_132.0
    dlon = (a["lon"] - b["lon"]) * 111_412.84 * math.cos(math.radians(a["lat"]))
    return math.hypot(dlat, dlon)


def richness(r):
    """Which of two rows for the same wall to keep: the one with routes, then
    with grades, then with more said about it, then the relation (placed by
    its routes) before the node (placed by hand) before the cliff line."""
    return (-(r.get("routes", 0) > 0), -bool(r.get("grades")), -len(r), TYPE_RANK[r["osm_type"]])


def same_crag(r, kept):
    w = words(r["name"])
    fn = fold(r["name"])
    for k in kept:
        d = dist_m(r, k)
        if d > 400:
            continue
        if d <= 150 and w and words(k["name"]) & w:
            return k
        if fold(k["name"]) == fn and (not r.get("parent") or not k.get("parent")
                                       or r["parent"] == k["parent"]):
            return k
    return None


def fill(keep, other):
    for key, v in other.items():
        if key not in keep:
            keep[key] = v
    if other.get("routes", 0) > keep.get("routes", 0):
        keep["routes"] = other["routes"]


def nearest_named(r, kept, max_m):
    """The wall a sector belongs to: within max_m, one that shares a word of
    the sector's name ("Massi delle Traole Settore H" is Traole's, not the
    crag next door's), else simply the nearest with a name of its own."""
    w = words(r["name"])
    best, best_d, kin, kin_d = None, max_m, None, max_m
    for k in kept:
        if k is r or sectorish(k["name"]) or fold(k["name"]) == fold(r["name"]):
            continue
        d = dist_m(r, k)
        if d < best_d:
            best, best_d = k, d
        if d < kin_d and w & words(k["name"]):
            kin, kin_d = k, d
    return kin or best


ORDER = ("name", "kind", "group", "parent", "ele", "ele_src", "grades", "aspect", "rock",
         "routes", "length", "styles", "website", "source", "osm_type", "osm_id")


def main(argv):
    if len(argv) < 2:
        sys.exit(__doc__.strip())
    src, out = pathlib.Path(argv[0]), pathlib.Path(argv[1])
    use_dem = "--no-dem" not in argv

    els = json.loads(src.read_text())["elements"]
    rows = [r for r in (row_of(e) for e in els) if r]
    print(f"OSM: {len(els)} crag elements, {len(rows)} with a name")

    # -- one row per wall ---------------------------------------------------
    rows.sort(key=richness)
    kept, merged = [], 0
    for r in rows:
        k = same_crag(r, kept)
        if k is None:
            kept.append(r)
        else:
            fill(k, r)
            merged += 1
    print(f"merged: {merged} rows were another spelling or geometry of a wall already kept")

    # -- sectors get their wall ---------------------------------------------
    near = 0
    for r in kept:
        if r.get("parent") or not sectorish(r["name"]):
            continue
        p = nearest_named(r, kept, 400)
        if p is not None:
            r["parent"] = p["name"]
            near += 1
    orphans = sum(1 for r in kept if sectorish(r["name"]) and not r.get("parent"))
    print(f"parents: {sum(1 for r in kept if r.get('parent'))} sectors carry their wall's name, "
          f"{near} of them by proximity; {orphans} sector names stand alone")

    # -- the base's height, from the DEM where the tag has none -------------
    need = [r for r in kept if not r.get("ele")]
    if need and use_dem:
        try:
            import numpy as np
            from dem import Dem
            lat = np.array([r["lat"] for r in need])
            lon = np.array([r["lon"] for r in need])
            d = Dem(bounds=(lat.min(), lat.max(), lon.min(), lon.max()), verbose=False)
            z, _ = d.sample(lat, lon)
            for r, v in zip(need, z):
                r["ele"] = int(round(float(v)))
                r["ele_src"] = "dem"
            print(f"elevation from the DEM for {len(need)} crags with no ele tag")
        except SystemExit as e:  # Dem() exits when no tile is on disk
            print(f"no DEM ({e}); {len(need)} crags stay without an elevation")

    # -- crags.geojson --------------------------------------------------------
    kept.sort(key=lambda r: (-r.get("routes", 0), -bool(r.get("grades")), fold(r["name"])))
    feats = []
    for r in kept:
        props = {k: r[k] for k in ORDER if k in r}
        props["source"] = "osm"
        props = {k: props[k] for k in ORDER if k in props}
        feats.append({"type": "Feature", "properties": props,
                      "geometry": {"type": "Point",
                                   "coordinates": [round(r["lon"], 6), round(r["lat"], 6)]}})
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps({"type": "FeatureCollection", "features": feats},
                              separators=(",", ":"), ensure_ascii=False))
    n = len(feats)
    have = {k: sum(1 for r in kept if r.get(k)) for k in ("grades", "aspect", "rock", "routes", "length", "styles")}
    print(f"wrote {out} ({out.stat().st_size / 1e6:.2f} MB, {n} points: "
          f"{sum(1 for r in kept if r.get('group'))} areas, "
          f"{sum(1 for r in kept if r.get('parent'))} sectors, the rest crags; "
          + ", ".join(f"{v} with {k}" for k, v in have.items()) + ")")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
