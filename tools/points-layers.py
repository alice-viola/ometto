#!/usr/bin/env python3
"""
The map's points of interest as GeoJSON: peaks, passes and huts.

    python3 tools/points-layers.py data/osm-taa/pois.json <rifugi_shapefile_base> \
            web/public/pois.geojson web/public/huts.geojson [--merge-m 1000]

Two sources. OpenStreetMap gives peaks (with the surveyed elevation on the
tag), mountain passes, and the huts, whether they are a node or a building
(tools/pbf-layers.py emits a hut polygon as its centroid, which is how most of
South Tyrol's rifugi are mapped). The Province of Trento's own
register gives 191 huts and bivouacs with their official name, kind and
altitude, and that is the authority for Trentino.

The two overlap, so a register hut is merged into the OSM one when they are the
same place: within `merge-m` metres AND with a name in common once "rifugio",
"bivacco", "malga" and the rest of the furniture is taken off (the register
writes BOCCA DI TRAT "NINO PERNICI", OSM writes Rifugio Nino Pernici), or
within 150 m whatever the names say. A peak or pass with no elevation on the
tag gets the Copernicus DEM's, marked as such: the DEM is a SURFACE model and
reads a few tens of metres low on a sharp summit, so a tagged elevation always
wins.
"""
import json
import math
import pathlib
import re
import sys
import unicodedata

import numpy as np
import shapefile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
from utm32 import to_lonlat

GENERIC = {
    "rifugio", "rifugi", "bivacco", "bivacchi", "malga", "baita", "capanna",
    "casera", "hutte", "huette", "berghuette", "schutzhaus", "haus", "alm",
    "alpe", "albergo", "ristoro", "punto", "sat", "cai", "avs", "sezione",
    "al", "alla", "allo", "ai", "agli", "alle", "di", "del", "della", "dei",
    "delle", "da", "dal", "in", "su", "sul", "la", "il", "lo", "le", "i",
    "e", "ex", "der", "die", "das", "am", "zum", "zur", "von", "auf",
}



def open_shp(base):
    """pyshp, with the encoding the .cpg names.

    The two Province downloads disagree: the hut register ships a .cpg saying
    utf-8, the trail catalogue ships none and is Windows-1252 (its "Localita
    Puza Dali" kills a strict utf-8 decode). Assuming either one for both turns
    COLDOSE into mojibake or throws; read the sidecar and believe it.
    """
    cpg = pathlib.Path(str(base) + ".cpg")
    enc = cpg.read_text().strip() if cpg.exists() else ""
    return shapefile.Reader(str(base), encoding=enc or "cp1252", encodingErrors="replace")

def fold(s):
    s = unicodedata.normalize("NFKD", s or "")
    s = "".join(c for c in s if not unicodedata.combining(c)).lower()
    return re.sub(r"[^a-z0-9 ]+", " ", s)


def tokens(s):
    return {t for t in fold(s).split() if t and t not in GENERIC and len(t) > 2}


def similar(a, b):
    """Containment: "ANTERMOIA" inside "Rifugio Antermoia" scores 1."""
    ta, tb = tokens(a), tokens(b)
    if not ta or not tb:
        return 0.0
    return len(ta & tb) / min(len(ta), len(tb))


def main(argv):
    if len(argv) < 4:
        sys.exit(__doc__.strip())
    osm_pois, shp = pathlib.Path(argv[0]), argv[1]
    out_pois, out_huts = pathlib.Path(argv[2]), pathlib.Path(argv[3])
    merge_m = float(argv[argv.index("--merge-m") + 1]) if "--merge-m" in argv else 1000.0

    els = json.loads(osm_pois.read_text())["elements"]
    osm_huts_all = [e for e in els if e["tags"]["kind"] == "hut"]
    poly = sum(1 for e in osm_huts_all if e.get("type") != "node")
    print(f"OSM: {len(els)} points, {len(osm_huts_all)} of them huts "
          f"({poly} from a building or a multipolygon)")

    r = open_shp(shp)
    f = [x[0] for x in r.fields[1:]]
    idx = {n: i for i, n in enumerate(f)}
    recs = r.records()
    shapes = r.shapes()
    pat = []
    for rec, sh in zip(recs, shapes):
        if not sh.points:
            continue
        lon, lat = to_lonlat(np.array([sh.points[0][0]]), np.array([sh.points[0][1]]))
        q = (rec[idx["quota"]] or "").strip()
        pat.append({
            "name": (rec[idx["nome_strut"]] or "").strip(),
            "type": (rec[idx["sottotipol"]] or "").strip(),
            "ele": int(float(q)) if re.fullmatch(r"\d+(\.\d+)?", q) else 0,
            "lat": float(lat[0]), "lon": float(lon[0]),
            "osm": None,
        })
    print(f"Province register: {len(pat)} huts and bivouacs, "
          f"{sum(1 for p in pat if p['ele'])} with an altitude")

    # -- merge ------------------------------------------------------------
    osm_huts = [e for e in els if e["tags"]["kind"] == "hut"]
    merged = 0
    for p in pat:
        best = None
        for e in osm_huts:
            dlat = (e["lat"] - p["lat"]) * 111_132.0
            dlon = (e["lon"] - p["lon"]) * 111_412.84 * math.cos(math.radians(p["lat"]))
            d = math.hypot(dlat, dlon)
            if d > merge_m:
                continue
            sim = similar(p["name"], e["tags"]["name"])
            ok = sim >= 0.34 or d <= 150.0
            if ok and (best is None or (sim, -d) > (best[0], -best[1])):
                best = (sim, d, e)
        if best is not None:
            e = best[2]
            p["osm"] = e["tags"]["name"]
            e.setdefault("pat", {})
            e["pat"] = {"name": p["name"], "type": p["type"], "ele": p["ele"],
                        "dist_m": round(best[1]), "sim": round(best[0], 2)}
            merged += 1
    print(f"merged: {merged} register huts are an OSM hut too; "
          f"{len(pat)-merged} are only in the register; "
          f"{len(osm_huts)-merged} OSM huts are outside the register (South Tyrol, mostly)")

    # -- elevations from the DEM where the tag has none --------------------
    need = [e for e in els if not e["tags"].get("ele")]
    if need:
        from dem import Dem
        lat = np.array([e["lat"] for e in need])
        lon = np.array([e["lon"] for e in need])
        d = Dem(bounds=(lat.min(), lat.max(), lon.min(), lon.max()), verbose=False)
        z, _ = d.sample(lat, lon)
        for e, v in zip(need, z):
            e["tags"]["ele"] = int(round(float(v)))
            e["dem"] = True
        print(f"elevation from the DEM for {len(need)} points with no ele tag")

    # -- pois.geojson ------------------------------------------------------
    feats = []
    for e in els:
        t = e["tags"]
        props = {"name": t["name"], "kind": t["kind"], "ele": int(t.get("ele") or 0),
                 "source": "osm", "osm_id": e["id"]}
        if e.get("dem"):
            props["ele_src"] = "dem"
        if t.get("hut_type"):
            props["type"] = t["hut_type"]
        if e.get("pat"):
            props["source"] = "osm+pat"
            props["type"] = e["pat"]["type"]
            props["pat_name"] = e["pat"]["name"]
            if e["pat"]["ele"]:
                props["ele"] = e["pat"]["ele"]
                props["ele_src"] = "pat"
        feats.append({"type": "Feature", "properties": props,
                      "geometry": {"type": "Point", "coordinates": [round(e["lon"], 6), round(e["lat"], 6)]}})
    for p in pat:
        if p["osm"]:
            continue
        feats.append({
            "type": "Feature",
            "properties": {"name": p["name"], "kind": "hut", "ele": p["ele"],
                           "source": "pat", "type": p["type"],
                           **({"ele_src": "pat"} if p["ele"] else {})},
            "geometry": {"type": "Point", "coordinates": [round(p["lon"], 6), round(p["lat"], 6)]}})
    out_pois.parent.mkdir(parents=True, exist_ok=True)
    out_pois.write_text(json.dumps({"type": "FeatureCollection", "features": feats},
                                   separators=(",", ":"), ensure_ascii=False))
    by = {}
    for f_ in feats:
        by[f_["properties"]["kind"]] = by.get(f_["properties"]["kind"], 0) + 1
    print(f"wrote {out_pois} ({out_pois.stat().st_size/1e6:.2f} MB, {len(feats)} points: "
          + ", ".join(f"{k} {v}" for k, v in sorted(by.items())) + ")")

    # -- huts.geojson ------------------------------------------------------
    # The register, plus the OSM huts that are drawn as a building: those are
    # the ones no register covers (South Tyrol's), and a planner that cannot
    # find Rifugio Firenze is not planning anything in the Odle.
    hfeats = [{
        "type": "Feature",
        "properties": {"name": p["name"], "type": p["type"], "ele": p["ele"],
                       "source": "pat",
                       **({"osm_name": p["osm"]} if p["osm"] else {})},
        "geometry": {"type": "Point", "coordinates": [round(p["lon"], 6), round(p["lat"], 6)]},
    } for p in pat]
    added = 0
    for e in osm_huts_all:
        if e.get("type") == "node" or e.get("pat"):
            continue          # a node is not a building; a merged one is in already
        t = e["tags"]
        hfeats.append({
            "type": "Feature",
            "properties": {"name": t["name"], "kind": "hut", "ele": int(t.get("ele") or 0),
                           "source": "osm", "osm_id": e["id"],
                           **({"type": t["hut_type"]} if t.get("hut_type") else {})},
            "geometry": {"type": "Point", "coordinates": [round(e["lon"], 6), round(e["lat"], 6)]}})
        added += 1
    print(f"huts.geojson: {len(pat)} from the register + {added} OSM huts mapped "
          f"as a building")
    out_huts.write_text(json.dumps({"type": "FeatureCollection", "features": hfeats},
                                   separators=(",", ":"), ensure_ascii=False))
    kinds = {}
    for h in hfeats:
        k = h["properties"].get("type", "")
        kinds[k] = kinds.get(k, 0) + 1
    print(f"wrote {out_huts} ({out_huts.stat().st_size/1e6:.2f} MB, {len(hfeats)} huts: "
          + ", ".join(f"{k or '(no type)'} {v}" for k, v in sorted(kinds.items(), key=lambda kv: -kv[1])) + ")")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
