#!/usr/bin/env python3
"""
The passenger lifts as a GeoJSON line layer.

    python3 tools/lifts-layer.py data/osm-taa/lifts.json web/public/lifts.geojson

One LineString per lift way, WGS84, with the name (the ref when OSM gave it no
name), the aerialway type, the length along the ground in metres and the ride
in seconds. The map file carries the same lifts as routable stretches of class
"lift"; this layer is what a page draws, so it keeps the geometry and not the
graph.
"""
import json
import math
import pathlib
import sys
from collections import Counter


def duration_s(v):
    """OSM's duration: minutes, MM:SS or HH:MM:SS. Same rule as the builder,
    and the tag to read is `aerialway:duration` before the bare `duration`."""
    v = (v or "").strip().lower().replace(",", ".")
    # "6:30 min", "9 min": the unit is written out often enough to matter.
    for unit in ("minutes", "minuti", "mins", "min", "'"):
        if v.endswith(unit):
            v = v[: -len(unit)].strip()
            break
    if not v:
        return 0
    try:
        if ":" in v:
            n = [float(x) for x in v.split(":")]
            if len(n) == 2:
                return int(round(n[0] * 60 + n[1]))
            if len(n) == 3:
                return int(round(n[0] * 3600 + n[1] * 60 + n[2]))
            return 0
        return int(round(float(v) * 60))
    except ValueError:
        return 0


UPHILL = {"chair_lift", "mixed_lift"}


def main(argv):
    if len(argv) < 2:
        sys.exit(__doc__.strip())
    src, out = pathlib.Path(argv[0]), pathlib.Path(argv[1])
    els = json.loads(src.read_text())["elements"]

    feats = []
    kinds = Counter()
    total = 0.0
    for el in els:
        g = el.get("geometry") or []
        if len(g) < 2:
            continue
        t = el.get("tags", {})
        lat0 = sum(p["lat"] for p in g) / len(g)
        m_lat = 111_132.92 - 559.82 * math.cos(2 * math.radians(lat0))
        m_lon = 111_412.84 * math.cos(math.radians(lat0))
        length = sum(
            math.hypot((g[i + 1]["lat"] - g[i]["lat"]) * m_lat,
                       (g[i + 1]["lon"] - g[i]["lon"]) * m_lon)
            for i in range(len(g) - 1))
        kind = t.get("aerialway", "")
        ow = 1 if kind in UPHILL else 0
        if t.get("oneway") in ("yes", "true", "1", "-1"):
            ow = 1
        elif t.get("oneway") in ("no", "false", "0"):
            ow = 0
        props = {
            "name": t.get("name") or t.get("ref") or "",
            "type": kind,
            "length_m": int(round(length)),
            "duration_s": duration_s(t.get("aerialway:duration") or t.get("duration")),
            "oneway": ow,
            "osm_id": el["id"],
        }
        if t.get("aerialway:occupancy"):
            props["occupancy"] = t["aerialway:occupancy"]
        feats.append({
            "type": "Feature", "properties": props,
            "geometry": {"type": "LineString",
                         "coordinates": [[round(p["lon"], 6), round(p["lat"], 6)] for p in g]},
        })
        kinds[kind] += 1
        total += length

    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps({"type": "FeatureCollection", "features": feats},
                              separators=(",", ":"), ensure_ascii=False))
    named = sum(1 for f in feats if f["properties"]["name"])
    timed = sum(1 for f in feats if f["properties"]["duration_s"])
    print(f"wrote {out} ({out.stat().st_size/1e6:.2f} MB, {len(feats)} lifts: "
          + ", ".join(f"{k} {v}" for k, v in kinds.most_common()) + ")")
    print(f"  {named} with a name, {timed} with a duration, "
          f"{total/1000:.0f} km of cable in total")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
