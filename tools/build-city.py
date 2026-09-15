#!/usr/bin/env python3
"""
Turn the raw OpenStreetMap download into the map the renderer draws.

Reads data/osm-<region>/*.json (what tools/pbf-layers.py wrote) and writes
web/public/city.json:

    meta      bbox, the projection's metre-per-degree scale, the world size
    roads     one entry per way: class, name, oneway, lanes, bridge/tunnel,
              and the polyline in metres
    nodes     every junction, as an index into a shared coordinate list
    areas     water, green, landuse and building polygons, also in metres

Coordinates are projected to LOCAL METRES with the bbox's centre as the origin,
because a renderer that works in degrees has to fight the fact that a degree of
longitude is shorter than a degree of latitude at every latitude but zero.

    python3 tools/build-city.py

Environment: CITY_SRC, CITY_OUT, CITY_NAME, and for a region-sized routing map

    CITY_ONLY_ROADS=1     no buildings, water, rails or landuse: the drawing
                          layers are the bulk of the file and a router that
                          geocodes from places.json needs none of them (the
                          keys stay, empty, so the schema does not change)
    CITY_CLIP=<geojson>   drop any stretch lying entirely outside these
                          polygons, CITY_CLIP_BUFFER_M metres wide (3000 by
                          default): an extract is cut on a bbox, a region is
                          not a bbox, and a border crossing must survive
"""

import json
import math
import pathlib
import sys
import time
from collections import defaultdict

ROOT = pathlib.Path(__file__).resolve().parent.parent
import os
SRC = pathlib.Path(os.environ.get("CITY_SRC", ROOT / "data" / "osm"))
OUT = pathlib.Path(os.environ.get("CITY_OUT", ROOT / "web" / "public" / "city.json"))
NAME = os.environ.get("CITY_NAME", "Trento")
ONLY_ROADS = os.environ.get("CITY_ONLY_ROADS", "").lower() in ("1", "true", "yes")
CLIP = os.environ.get("CITY_CLIP", "")
CLIP_BUFFER_M = float(os.environ.get("CITY_CLIP_BUFFER_M", "3000"))

# How a highway tag becomes a drawing class. Order matters: first match wins.
ROAD_CLASS = [
    ("motorway", ("motorway", "motorway_link")),
    ("trunk", ("trunk", "trunk_link")),
    ("primary", ("primary", "primary_link")),
    ("secondary", ("secondary", "secondary_link")),
    ("tertiary", ("tertiary", "tertiary_link")),
    ("residential", ("residential", "unclassified", "living_street")),
    ("service", ("service",)),
    ("cycleway", ("cycleway",)),
    ("track", ("track",)),
    ("path", ("path", "footway", "steps", "bridleway", "via_ferrata")),
    ("pedestrian", ("pedestrian",)),
]
SAC = {"hiking": 1, "mountain_hiking": 2, "demanding_mountain_hiking": 3, "alpine_hiking": 4,
       "demanding_alpine_hiking": 5, "difficult_alpine_hiking": 6}


def duration_s(v):
    """OSM's duration in seconds: "12" is twelve minutes, "5:30" is five and a
    half, "1:05:30" is an hour and five. 0 when the lift has none.

    The tag to read is `aerialway:duration`, which is the one the lift schema
    actually uses: in this region 377 lifts carry it and exactly one carries a
    bare `duration`."""
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


# A chair lift and a mixed lift carry people one way and come back empty; a
# cable car and a gondola are a shuttle you ride in either direction. An
# explicit oneway tag beats the default.
LIFT_UPHILL = {"chair_lift", "mixed_lift"}


def access(v):
    """-1 forbidden, 1 designated or allowed, 0 unknown."""
    if v in ("no", "private", "dismount"):
        return -1
    if v in ("yes", "designated", "permissive"):
        return 1
    return 0

AREA_LAYERS = [
    ("park", "green_parks", None),
    ("farm", "green_farm", None),
    ("wood", "green_wood", None),
    ("landuse", "landuse", None),
    ("building", "buildings", None),
]


def load(name):
    p = SRC / f"{name}.json"
    if not p.exists():
        print(f"  (missing {name}.json, skipped)")
        return []
    return json.loads(p.read_text()).get("elements", [])


class Elements:
    """The elements of one or more layer files, parsed one at a time.

    json.loads on the region's 245 MB roads layer builds ten million
    two-float dicts and holds them all -- gigabytes, on a box that is also
    running the demo. The file is read once as text and each element is
    decoded where it sits, so only the element being looked at is alive; the
    roads are walked twice (once to count how often a node is used, once to
    build), which re-decodes rather than re-reads.
    """

    def __init__(self, *names):
        self.text = []
        for n in names:
            p = SRC / f"{n}.json"
            if not p.exists():
                print(f"  (missing {n}.json, skipped)")
                continue
            self.text.append(p.read_text())

    def __iter__(self):
        dec = json.JSONDecoder()
        for text in self.text:
            i = text.index("[", text.index('"elements"')) + 1
            n = len(text)
            while True:
                while i < n and text[i] in " \t\r\n,":
                    i += 1
                if i >= n or text[i] == "]":
                    break
                el, i = dec.raw_decode(text, i)
                yield el


def road_class(tags):
    hw = tags.get("highway", "")
    for cls, kinds in ROAD_CLASS:
        if hw in kinds:
            return cls
    return None


def main():
    bbox = json.loads((SRC / "bbox.json").read_text())
    lat0 = (bbox["south"] + bbox["north"]) / 2
    lon0 = (bbox["west"] + bbox["east"]) / 2

    # Equirectangular about the bbox centre: at city scale the error is
    # centimetres, and every coordinate downstream is a metre.
    m_per_deg_lat = 111_132.92 - 559.82 * math.cos(2 * math.radians(lat0))
    m_per_deg_lon = 111_412.84 * math.cos(math.radians(lat0))

    def project(lat, lon):
        return (
            round((lon - lon0) * m_per_deg_lon, 1),
            round((lat0 - lat) * m_per_deg_lat, 1),  # y grows south, as on screen
        )

    # ---------------------------------------------------------------- roads --
    # A junction is a node shared by two or more ways. Overpass gives us the
    # node ids along each way, so counting them is all it takes — and that is
    # what makes this a graph rather than a pile of lines.
    raw_roads = Elements("roads", "paths")

    # The clip: a region is not a bbox, and the extract carries whatever fell
    # in the bbox's corners. A stretch with one point inside stays whole.
    keep = None
    if CLIP:
        sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
        from geomask import Mask
        print(f"  clip {CLIP} + {CLIP_BUFFER_M:.0f} m")
        keep = Mask.from_geojson(CLIP, buffer_m=CLIP_BUFFER_M)

    t_roads = time.time()
    dropped = [0]

    def wanted(el):
        if el.get("type") != "way" or not el.get("nodes"):
            return False
        if road_class(el.get("tags", {})) is None:
            return False
        if keep is not None and not keep.contains_any(el.get("geometry") or []):
            dropped[0] += 1
            return False
        return True

    use_count = defaultdict(int)
    for el in raw_roads:
        if not wanted(el):
            continue
        for nid in el["nodes"]:
            use_count[nid] += 1
    if keep is not None:
        print(f"  clip dropped {dropped[0]} ways entirely outside")
        dropped[0] = 0

    coords = {}   # osm node id -> index into `points`
    points = []

    def point_index(nid, lat, lon):
        if nid in coords:
            return coords[nid]
        coords[nid] = len(points)
        points.append(list(project(lat, lon)))
        return coords[nid]

    roads = []
    seen_way = set()
    for el in raw_roads:
        if not wanted(el) or el.get("id") in seen_way:
            continue
        tags = el.get("tags", {})
        cls = road_class(tags)
        geom = el.get("geometry") or []
        nids = el.get("nodes") or []
        if cls is None or len(geom) < 2 or len(geom) != len(nids):
            continue
        seen_way.add(el["id"])

        # Split the way at every junction: one entry per stretch between two
        # junctions is what makes routing possible later.
        idx = [point_index(nids[i], g["lat"], g["lon"]) for i, g in enumerate(geom)]
        cuts = [0] + [i for i in range(1, len(nids) - 1) if use_count[nids[i]] > 1] + [len(nids) - 1]
        # junction=roundabout implies one-way in OSM, tagged or not.
        oneway = tags.get("oneway") in ("yes", "true", "1", "-1") or tags.get("junction") in ("roundabout", "circular")
        lanes = tags.get("lanes")
        for a, b in zip(cuts, cuts[1:]):
            if b - a < 1:
                continue
            roads.append({
                "c": cls,
                "p": idx[a : b + 1],
                **({"n": tags["name"]} if "name" in tags else {}),
                **({"o": 1} if oneway else {}),
                **({"r": 1} if tags.get("junction") in ("roundabout", "circular") else {}),
                **({"l": int(lanes)} if lanes and lanes.isdigit() else {}),
                **({"b": 1} if tags.get("bridge") else {}),
                **({"t": 1} if tags.get("tunnel") else {}),
                # the hiking and cycling attributes
                **({"s": SAC[tags["sac_scale"]]} if tags.get("sac_scale") in SAC else {}),
                **({"hr": tags["hiking_ref"]} if tags.get("hiking_ref") else {}),
                **({"br": tags["bike_ref"]} if tags.get("bike_ref") else {}),
                **({"v": 1} if tags.get("highway") == "via_ferrata" else {}),
                **({"st": 1} if tags.get("highway") == "steps" else {}),
                **({"m": int(str(tags["mtb:scale"])[0])} if str(tags.get("mtb:scale", ""))[:1].isdigit() else {}),
                **({"bk": access(tags.get("bicycle"))} if access(tags.get("bicycle")) else {}),
                **({"ft": access(tags.get("foot"))} if access(tags.get("foot")) else {}),
            })

    # Junctions: the points more than two road stretches touch.
    touch = defaultdict(int)
    for r in roads:
        touch[r["p"][0]] += 1
        touch[r["p"][-1]] += 1
    junctions = sorted(i for i, n in touch.items() if n > 2)

    # ---------------------------------------------------------------- lifts --
    # A cable car is not a road. It is one stretch from station to station,
    # never split, and its ends are deliberately NOT junctions: the router
    # links each station to the nearest walking junction when it loads, which
    # is the only way to get on a lift anyway. It does share the point array,
    # so tools/dem.py gives every pylon an elevation like everything else, and
    # the climb of a lift leg is real.
    lifts = 0
    for el in load("lifts"):
        tags = el.get("tags", {})
        geom = el.get("geometry") or []
        nids = el.get("nodes") or []
        if len(geom) < 2 or len(geom) != len(nids):
            continue
        if keep is not None and not keep.contains_any(geom):
            continue
        kind = tags.get("aerialway", "")
        name = tags.get("name") or tags.get("ref") or ""
        ow = 1 if kind in LIFT_UPHILL else 0
        if tags.get("oneway") in ("yes", "true", "1", "-1"):
            ow = 1
        elif tags.get("oneway") in ("no", "false", "0"):
            ow = 0
        roads.append({
            "c": "lift",
            "p": [point_index(nids[i], g["lat"], g["lon"]) for i, g in enumerate(geom)],
            **({"n": name} if name else {}),
            "lt": kind,
            "dur": duration_s(tags.get("aerialway:duration") or tags.get("duration")),
            "ow": ow,
        })
        lifts += 1


    # Water is two different things and they cannot share a layer: a lake or a
    # riverbank is an AREA, while a river, a stream or a canal is a LINE down
    # the middle of it. Filling the second kind as if it were the first turns
    # every mountain stream into a blue wedge across the map.
    water_areas, water_lines = [], []
    for el in ([] if ONLY_ROADS else load("water")):
        tags = el.get("tags", {})
        geom = el.get("geometry") or []
        if len(geom) < 2:
            continue
        pts = [list(project(g["lat"], g["lon"])) for g in geom]
        if el.get("type") == "relation":
            for m in el.get("members", []):
                g = m.get("geometry")
                if m.get("role") in ("outer", "") and g and len(g) >= 3:
                    water_areas.append([list(project(p["lat"], p["lon"])) for p in g])
            continue
        closed = len(pts) >= 4 and pts[0] == pts[-1]
        is_area = tags.get("natural") == "water" or tags.get("waterway") == "riverbank" \
            or tags.get("area") == "yes"
        if is_area and closed:
            water_areas.append(pts)
        else:
            width = 6.0 if tags.get("waterway") == "river" else 2.5
            water_lines.append({"w": width, "p": pts})

    # ---------------------------------------------------------------- areas --
    areas = {}
    for key, fname, _ in AREA_LAYERS:
        polys = []
        for el in ([] if ONLY_ROADS else load(fname)):
            geom = el.get("geometry")
            if el.get("type") == "way" and geom and len(geom) >= 3:
                polys.append([project(g["lat"], g["lon"]) for g in geom])
            elif el.get("type") == "relation":
                for m in el.get("members", []):
                    g = m.get("geometry")
                    if m.get("role") in ("outer", "") and g and len(g) >= 3:
                        polys.append([project(p["lat"], p["lon"]) for p in g])
        areas[key] = [[list(p) for p in poly] for poly in polys]
    areas["water"] = water_areas

    # Rail is drawn as lines, not areas.
    rails = []
    for el in ([] if ONLY_ROADS else load("rail")):
        g = el.get("geometry") or []
        if len(g) >= 2:
            rails.append([list(project(p["lat"], p["lon"])) for p in g])

    # The extent is the BBOX, not the bounding box of the geometry: a relation
    # whose members run off to the next valley would otherwise stretch the
    # world to twice the size of the city that was asked for.
    x0, y0 = project(bbox["north"], bbox["west"])
    x1, y1 = project(bbox["south"], bbox["east"])
    xs = [x0, x1]
    ys = [y0, y1]
    doc = {
        "meta": {
            "name": NAME,
            "bbox": bbox,
            "origin": {"lat": lat0, "lon": lon0},
            "mPerDegLat": round(m_per_deg_lat, 3),
            "mPerDegLon": round(m_per_deg_lon, 3),
            "extent": {
                "minX": min(xs), "maxX": max(xs),
                "minY": min(ys), "maxY": max(ys),
            },
        },
        "points": points,
        "roads": roads,
        "junctions": junctions,
        "rails": rails,
        "waterLines": water_lines,
        "areas": areas,
    }

    OUT.parent.mkdir(parents=True, exist_ok=True)
    OUT.write_text(json.dumps(doc, separators=(",", ":")))

    print(f"{OUT} written  ({OUT.stat().st_size / 1_048_576:.1f} MB)"
          + ("  [routing only: no drawing layers]" if ONLY_ROADS else ""))
    print(f"  points     {len(points):7d}")
    print(f"  roads      {len(roads):7d} stretches, {len(junctions)} junctions")
    if lifts:
        by_lift = defaultdict(int)
        secs = 0
        for r in roads:
            if r["c"] == "lift":
                by_lift[r["lt"]] += 1
                secs += 1 if r["dur"] else 0
        print(f"  lifts      {lifts:7d} passenger lifts ("
              + ", ".join(f"{k} {v}" for k, v in sorted(by_lift.items(), key=lambda kv: -kv[1]))
              + f"), {secs} with a duration")
    by = defaultdict(int)
    for r in roads:
        by[r["c"]] += 1
    for k in sorted(by, key=lambda k: -by[k]):
        print(f"    {k:12s} {by[k]:6d}")
    print(f"  rails      {len(rails):7d}")
    for k, v in areas.items():
        print(f"  {k:10s} {len(v):7d} polygons")
    e = doc["meta"]["extent"]
    print(f"  extent     {e['maxX'] - e['minX']:.0f} x {e['maxY'] - e['minY']:.0f} metres")


if __name__ == "__main__":
    main()
