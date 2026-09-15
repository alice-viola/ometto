#!/usr/bin/env python3
"""Ask the OSM extract what is actually near a point, and whether a trail exists.

The route API's answers are only as good as data/osm-taa.  When a route
comes back wrong this tells you which of the three suspects it is: the point
has no graph near it (snapping), the trail is absent or misgraded (data), or
neither and the cost model is at fault (weights).

    python3 tests/osm-probe.py --routes tests/routes.json     # every endpoint
    python3 tests/osm-probe.py --point 46.1123,11.1543        # one point
    python3 tests/osm-probe.py --ref 401                      # where a trail is

stdlib only, one streaming pass over roads.json, bounded memory.
"""

import argparse
import json
import math
import os
import re
import sys

HERE = os.path.dirname(os.path.abspath(__file__))
ROOT = os.path.dirname(HERE)
# the region the product serves; the older Trento-only and Trentino-only
# extracts are the fallback when it has not been built yet
ROADS = next(
    (p for p in (os.path.join(ROOT, "data", "osm-taa", "roads.json"),
                 os.path.join(ROOT, "data", "osm-taa", "roads.json"),
                 os.path.join(ROOT, "data", "osm", "roads.json"))
     if os.path.exists(p)),
    os.path.join(ROOT, "data", "osm-taa", "roads.json"),
)

WAY = re.compile(
    r'\{"type":"way","id":(\d+),"tags":(\{.*?\}),"nodes":\[[^\]]*\],"geometry":(\[.*?\])\}',
    re.S,
)


def haversine_m(a_lat, a_lon, b_lat, b_lon):
    R = 6371000.0
    p1, p2 = math.radians(a_lat), math.radians(b_lat)
    h = (math.sin((p2 - p1) / 2) ** 2
         + math.cos(p1) * math.cos(p2) * math.sin(math.radians(b_lon - a_lon) / 2) ** 2)
    return 2 * R * math.asin(math.sqrt(h))


def scan(roads_path, targets, want_ref=None, pad=0.02):
    """targets: [(label, lat, lon)].  Returns nearest ways per target and, if
    want_ref is set, the bounding box of every way carrying that hiking_ref."""
    best = {lbl: [] for lbl, _, _ in targets}
    ref_hits = []
    buf = ""
    with open(roads_path) as f:
        while True:
            chunk = f.read(1 << 22)
            if not chunk:
                break
            buf += chunk
            last = 0
            for m in WAY.finditer(buf):
                last = m.end()
                try:
                    tags = json.loads(m.group(2))
                    geom = json.loads(m.group(3))
                except Exception:
                    continue
                if want_ref and tags.get("hiking_ref") == want_ref:
                    lats = [g["lat"] for g in geom]
                    lons = [g["lon"] for g in geom]
                    ref_hits.append({
                        "id": int(m.group(1)), "name": tags.get("name"),
                        "highway": tags.get("highway"), "sac": tags.get("sac_scale"),
                        "s": min(lats), "n": max(lats), "w": min(lons), "e": max(lons),
                    })
                for lbl, lat, lon in targets:
                    near = False
                    for g in geom:
                        if abs(g["lat"] - lat) < pad and abs(g["lon"] - lon) < pad:
                            near = True
                            break
                    if not near:
                        continue
                    d = min(haversine_m(lat, lon, g["lat"], g["lon"]) for g in geom)
                    rec = (round(d, 1), int(m.group(1)), tags.get("highway"),
                           tags.get("name"), tags.get("hiking_ref") or tags.get("ref"),
                           tags.get("sac_scale"), tags.get("foot"), tags.get("bicycle"))
                    lst = best[lbl]
                    lst.append(rec)
                    if len(lst) > 40:
                        lst.sort()
                        del lst[8:]
            buf = buf[last:] if last else buf[-400000:]
    for lbl in best:
        best[lbl].sort()
        del best[lbl][8:]
    return best, ref_hits


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--roads", default=ROADS)
    ap.add_argument("--routes", default=None)
    ap.add_argument("--point", action="append", default=[], help="lat,lon[,label]")
    ap.add_argument("--ref", default=None, help="a hiking_ref to locate")
    ap.add_argument("--json", default=None)
    args = ap.parse_args()

    targets = []
    if args.routes:
        cfg = json.load(open(args.routes))
        for r in cfg["routes"]:
            for i, p in enumerate(r["points"]):
                targets.append(("%s[%d] %s" % (r["id"], i, p.get("name", "")),
                                p["lat"], p["lon"]))
    for s in args.point:
        parts = s.split(",")
        lat, lon = float(parts[0]), float(parts[1])
        targets.append((parts[2] if len(parts) > 2 else s, lat, lon))
    if not targets and not args.ref:
        print("nothing to probe")
        return 2

    best, ref_hits = scan(args.roads, targets, args.ref)
    for lbl, lat, lon in targets:
        rows = best[lbl]
        print("\n== %s  (%.5f,%.5f)" % (lbl, lat, lon))
        if not rows:
            print("   NOTHING within ~2 km — outside the extract or a data hole")
            continue
        for d, wid, hw, name, ref, sac, foot, bike in rows[:6]:
            print("   %7.1f m  %-12s ref=%-8s sac=%-24s %s%s" % (
                d, hw or "-", ref or "-", sac or "-", name or "-",
                ("  foot=%s" % foot if foot else "") + ("  bike=%s" % bike if bike else "")))
    if args.ref:
        print("\n== hiking_ref %s: %d ways" % (args.ref, len(ref_hits)))
        if ref_hits:
            print("   lat %.4f..%.4f  lon %.4f..%.4f" % (
                min(h["s"] for h in ref_hits), max(h["n"] for h in ref_hits),
                min(h["w"] for h in ref_hits), max(h["e"] for h in ref_hits)))
            sacs = {}
            for h in ref_hits:
                sacs[h["sac"]] = sacs.get(h["sac"], 0) + 1
            print("   sac_scale:", sacs)
            for h in ref_hits[:8]:
                print("   way %-12d %-10s %-28s %.4f,%.4f" % (
                    h["id"], h["highway"] or "-", (h["name"] or "-")[:28], h["s"], h["w"]))
    if args.json:
        json.dump({"near": best, "ref": ref_hits}, open(args.json, "w"), indent=1)
    return 0


if __name__ == "__main__":
    sys.exit(main())
