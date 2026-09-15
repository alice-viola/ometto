#!/usr/bin/env python3
"""
Turn an OSM .pbf extract into the per-layer JSON files tools/build-city.py
reads, in the shape Overpass's `out geom` gives: {"elements": [{"type": "way",
"id", "tags", "nodes", "geometry": [{"lat", "lon"}]}]}.

    python3 tools/pbf-layers.py extract.osm.pbf data/osm-taa S,W,N,E "Trentino-Alto Adige"
    python3 tools/pbf-layers.py extract.osm.pbf data/osm-taa S,W,N,E "TAA" roads,places,pois,water

An optional fifth argument names the layers to keep; the rest are not written
at all, which is what makes a region-sized extract fit: a routing map needs
roads (and the places and pois the geocoder searches), not 300k buildings.
Every layer is written as it is read, one element at a time, so the whole of
Trentino-Alto Adige never sits in memory at once.

Ways, plus the multipolygon relations assembled into areas (lakes and forests
are mostly relations), plus the place nodes (city, town, village, ...) as a
"places" layer for the labels, plus the huts -- which are a node only when
nobody has drawn the building yet, so the closed ways and the multipolygons
tagged as one are emitted as their centroid too (most of South Tyrol's rifugi
are buildings, Rifugio Firenze among them). Clipped to the bbox (a way is kept if any node
is inside). Roads keep every node, because the builder finds junctions by node
ids; every other layer is simplified (Douglas-Peucker, 3 m) since it is only
drawn. Tracks and footways are left out: they are not drivable, and at
province scale they are half the ways.
"""
import json, math, pathlib, re, sys, time, unicodedata
import osmium

ALL_LAYERS = ("roads", "lifts", "rail", "water", "green_parks", "green_farm",
              "green_wood", "landuse", "buildings", "places", "pois")

pbf, out, bbox, name = sys.argv[1], pathlib.Path(sys.argv[2]), sys.argv[3], sys.argv[4]
KEEP = ALL_LAYERS
if len(sys.argv) > 5 and sys.argv[5].strip():
    KEEP = tuple(x.strip() for x in sys.argv[5].split(",") if x.strip())
    bad = [k for k in KEEP if k not in ALL_LAYERS]
    if bad:
        sys.exit(f"unknown layer(s) {bad}; known: {', '.join(ALL_LAYERS)}")
S, W, N, E = (float(v) for v in bbox.split(","))
out.mkdir(parents=True, exist_ok=True)
lat0 = (S + N) / 2
MLAT = 111_132.92 - 559.82 * math.cos(2 * math.radians(lat0))
MLON = 111_412.84 * math.cos(math.radians(lat0))

ROADS = {"motorway", "motorway_link", "trunk", "trunk_link", "primary", "primary_link",
         "secondary", "secondary_link", "tertiary", "tertiary_link", "residential",
         "unclassified", "living_street", "service",
         # the hiking and cycling network
         "path", "footway", "steps", "bridleway", "track", "cycleway", "pedestrian", "via_ferrata"}
POIS = {"peak", "hut", "pass"}
HUT_TOURISM = {"alpine_hut", "wilderness_hut"}
# The furniture a hut's name is buried in, on both sides of the region.
HUT_WORDS = {"rifugio", "rifugi", "bivacco", "capanna", "baita", "malga",
             "hutte", "huette", "berghutte", "schutzhaus", "haus", "alm",
             "alpe", "chalet", "refuge", "al", "alla", "allo", "ai", "di",
             "del", "della", "dei", "delle", "da", "la", "il", "lo", "le",
             "der", "die", "das", "am", "zum", "zur", "von", "auf", "and"}
PARKS_L = {"park", "garden", "pitch", "playground", "golf_course", "sports_centre", "recreation_ground"}
PARKS_LU = {"grass", "village_green", "cemetery", "allotments"}
FARM = {"orchard", "vineyard", "meadow", "farmland"}
WOOD_N = {"wood", "scrub", "grassland", "heath"}
LANDUSE = {"residential", "industrial", "commercial", "retail", "railway", "quarry", "construction"}
AMEN = {"school", "university", "hospital", "parking"}
RAIL = {"rail", "light_rail", "tram", "subway", "narrow_gauge"}
# The aerialways that carry a person up: a station is a building, a goods line
# or a drag lift (platter, t-bar, j-bar, rope_tow, magic_carpet) is not
# something a route can put someone on, and zip_line goes the wrong way.
LIFTS = {"cable_car", "gondola", "chair_lift", "mixed_lift"}
# A lift that is not there any more is tagged, not deleted.
GONE = ("abandoned", "disused", "proposed", "construction", "razed", "demolished")
WATERWAY = {"river", "stream", "canal", "riverbank"}
PLACES = {"city", "town", "village", "hamlet", "suburb", "quarter", "neighbourhood", "locality"}
KEEP_TAGS = ("highway", "name", "oneway", "junction", "lanes", "bridge", "tunnel", "railway",
             "natural", "waterway", "area", "leisure", "landuse", "amenity", "building",
             "sac_scale", "trail_visibility", "mtb:scale", "bicycle", "foot", "surface", "ref",
             "aerialway", "duration", "aerialway:duration", "aerialway:occupancy")


class Sink:
    """One open file per kept layer, written element by element.

    Holding the layers in memory costs about ten times what the JSON costs on
    disk -- every point is a dict of two floats -- and the region's roads layer
    alone is a quarter of a gigabyte of JSON.
    """

    def __init__(self, out, keep):
        self.n = {k: 0 for k in ALL_LAYERS}
        self.keep = set(keep)
        self.f = {}
        for k in keep:
            f = open(out / f"{k}.json", "w")
            f.write('{"elements":[')
            self.f[k] = f

    def add(self, layer, el):
        f = self.f.get(layer)
        if f is None:
            return
        if self.n[layer]:
            f.write(",")
        f.write(json.dumps(el, separators=(",", ":")))
        self.n[layer] += 1

    def close(self):
        for f in self.f.values():
            f.write("]}")
            f.close()


def is_hut(t):
    """A hut, however it is tagged: the point, the building, the multipolygon."""
    return bool(hut_type(t))


def hut_type(t):
    """Which kind of hut, or "" for anything that is not one.

    Staffed hut, unstaffed hut, and the basic_hut shelters, which in this
    region are the bivouacs. Worth keeping apart: one of them has a warden
    and a kitchen and the other two have a roof.
    """
    if t.get("tourism") in HUT_TOURISM:
        return t.get("tourism")
    if t.get("amenity") == "shelter" and t.get("shelter_type") == "basic_hut":
        return "basic_hut"
    return ""


def hut_tokens(name):
    """The words of a hut's name that identify it, folded and stripped.

    "Rifugio Firenze - Regensburger Hutte" and "Regensburger Hutte" must come
    out with a word in common, or the same hut mapped once as a node and once
    as a building is emitted twice a hundred metres apart.
    """
    n = unicodedata.normalize("NFKD", name or "")
    n = "".join(c for c in n if not unicodedata.combining(c)).lower()
    n = re.sub(r"[^a-z0-9 ]+", " ", n)
    return {w for w in n.split() if len(w) > 2 and w not in HUT_WORDS}


def ele_of(t):
    v = (t.get("ele") or "").replace(",", ".")
    try:
        return int(float(v))
    except ValueError:
        return 0


def centroid(pts):
    """Area-weighted centroid of a ring of (x, y, lat, lon, ...) points.

    A hut's footprint is metres across, so the mean of its corners would do;
    the shoelace is here because a multipolygon's outer ring can be a whole
    alpine farm, and its corners are not spread evenly.
    """
    a = cx = cy = 0.0
    for i in range(len(pts) - 1):
        x0, y0 = pts[i][3], pts[i][2]
        x1, y1 = pts[i + 1][3], pts[i + 1][2]
        f = x0 * y1 - x1 * y0
        a += f
        cx += (x0 + x1) * f
        cy += (y0 + y1) * f
    if abs(a) < 1e-12:
        return (sum(p[2] for p in pts) / len(pts), sum(p[3] for p in pts) / len(pts))
    return (cy / (3 * a), cx / (3 * a))


def rdp(pts, eps):
    """Douglas-Peucker on [(x, y, lat, lon)], returns the kept points."""
    if len(pts) < 3:
        return pts
    keep = [False] * len(pts)
    keep[0] = keep[-1] = True
    stack = [(0, len(pts) - 1)]
    while stack:
        a, b = stack.pop()
        ax, ay = pts[a][0], pts[a][1]
        bx, by = pts[b][0], pts[b][1]
        dx, dy = bx - ax, by - ay
        L = math.hypot(dx, dy) or 1e-9
        best, bi = 0.0, -1
        for i in range(a + 1, b):
            d = abs(dy * pts[i][0] - dx * pts[i][1] + bx * ay - by * ax) / L
            if d > best:
                best, bi = d, i
        if best > eps and bi > 0:
            keep[bi] = True
            stack.append((a, bi))
            stack.append((bi, b))
    return [p for p, k in zip(pts, keep) if k]


class Members(osmium.SimpleHandler):
    """Pass one: which ways belong to a water or forest relation. A relation
    cut by the extract's edge (Lake Garda: three quarters of it is Lombardy and
    Veneto) cannot be assembled, so its member ways inside the extract are
    chained by hand and closed with a chord across the missing part."""

    def __init__(self):
        super().__init__()
        self.want = {}
        self.hike_ref = {}  # way id -> the marked trail's number (route=hiking relations)
        self.bike_ref = {}

    def relation(self, r):
        t = r.tags
        if t.get("type") == "route":
            if "roads" not in KEEP:
                return
            ref = t.get("ref") or t.get("name") or ""
            if t.get("route") in ("hiking", "foot") and ref:
                for m in r.members:
                    if m.type == "w":
                        self.hike_ref.setdefault(m.ref, ref)
            elif t.get("route") in ("bicycle", "mtb") and ref:
                for m in r.members:
                    if m.type == "w":
                        self.bike_ref.setdefault(m.ref, ref)
            return
        if t.get("natural") == "water" or t.get("waterway") == "riverbank":
            layer = "water"
        elif t.get("landuse") == "forest" or t.get("natural") in WOOD_N:
            layer = "green_wood"
        else:
            return
        if layer not in KEEP:
            return
        for m in r.members:
            if m.type == "w" and m.role in ("outer", ""):
                self.want[m.ref] = (r.id, layer)


class H(osmium.SimpleHandler):
    def __init__(self, mem, sink):
        super().__init__()
        self.want = mem.want
        self.hike_ref, self.bike_ref = mem.hike_ref, mem.bike_ref
        self.parts = {}      # (relation id, layer) -> [(first ref, last ref, [pts])]
        self.assembled = set()
        self.sink = sink
        self.seen = 0
        # Huts are buffered instead of streamed: the same hut is often a node
        # AND a building, and the building is only read long after the node.
        self.huts = []        # (lat, lon, tokens, element)
        self.hut_poly = 0

    def node(self, n):
        t = n.tags
        pl = t.get("place")
        kind = None
        if t.get("natural") == "peak":
            kind = "peak"
        elif t.get("mountain_pass") == "yes":
            kind = "pass"
        elif is_hut(t):
            kind = "hut"
        if kind and "pois" not in KEEP:
            return
        if pl and not kind and "places" not in KEEP:
            return
        if (pl in PLACES or kind) and "name" in t and n.location.valid():
            lat, lon = n.location.lat, n.location.lon
            if not (S <= lat <= N and W <= lon <= E):
                return
            if kind == "hut":
                self.add_hut(lat, lon, t["name"], ele_of(t), "node", n.id, hut_type(t))
            elif kind:
                self.sink.add("pois", {"type": "node", "id": n.id, "lat": lat, "lon": lon,
                                            "tags": {"name": t["name"], "kind": kind, "ele": ele_of(t)}})
            else:
                self.sink.add("places", {"type": "node", "id": n.id, "lat": lat, "lon": lon,
                                              "tags": {"name": t["name"], "place": pl, "population": t.get("population", "")}})

    def add_hut(self, lat, lon, name, ele, typ, oid, ht=""):
        """One hut, unless the same one is already within 100 m under a name
        that shares a word. Node first, building second: the node wins."""
        toks = hut_tokens(name)
        for hlat, hlon, htoks, _el in self.huts:
            if abs(hlat - lat) * MLAT > 100 or abs(hlon - lon) * MLON > 100:
                continue
            if math.hypot((hlat - lat) * MLAT, (hlon - lon) * MLON) > 100:
                continue
            if toks & htoks or not toks or not htoks:
                return False
        self.huts.append((lat, lon, toks, {
            "type": typ, "id": oid, "lat": round(lat, 7), "lon": round(lon, 7),
            "tags": {"name": name, "kind": "hut", "ele": ele,
                     **({"hut_type": ht} if ht else {})}}))
        if typ != "node":
            self.hut_poly += 1
        return True

    def area(self, a):
        # Closed ways come through way(); here only the multipolygon relations,
        # one polygon per outer ring (the holes are not cut).
        if a.from_way():
            return
        self.assembled.add(a.orig_id())
        t = a.tags
        if is_hut(t) and "name" in t and "pois" in KEEP:
            big = None
            for ring in a.outer_rings():
                pts = [(0.0, 0.0, n.location.lat, n.location.lon)
                       for n in ring if n.location.valid()]
                if len(pts) >= 4 and (big is None or len(pts) > len(big)):
                    big = pts
            if big is not None:
                lat, lon = centroid(big)
                if S <= lat <= N and W <= lon <= E:
                    self.add_hut(lat, lon, t["name"], ele_of(t), "relation",
                                 -a.orig_id(), hut_type(t))
        if t.get("natural") == "water" or t.get("waterway") == "riverbank":
            layer = "water"
        elif t.get("leisure") in PARKS_L or t.get("landuse") in PARKS_LU:
            layer = "green_parks"
        elif t.get("landuse") in FARM:
            layer = "green_farm"
        elif t.get("landuse") == "forest" or t.get("natural") in WOOD_N:
            layer = "green_wood"
        elif t.get("landuse") in LANDUSE or t.get("amenity") in AMEN:
            layer = "landuse"
        elif "building" in t:
            layer = "buildings"
        else:
            return
        if layer not in KEEP:
            return
        tags = {k: t.get(k) for k in KEEP_TAGS if k in t}
        for ring in a.outer_rings():
            pts = []
            for n in ring:
                if not n.location.valid():
                    pts = []
                    break
                lat, lon = n.location.lat, n.location.lon
                pts.append(((lon - W) * MLON, (lat - S) * MLAT, lat, lon, n.ref))
            if len(pts) < 4 or not any(S <= p[2] <= N and W <= p[3] <= E for p in pts):
                continue
            far = max(range(1, len(pts) - 1), key=lambda i: (pts[i][0] - pts[0][0]) ** 2 + (pts[i][1] - pts[0][1]) ** 2)
            pts = rdp(pts[: far + 1], 3.0) + rdp(pts[far:], 3.0)[1:]
            self.sink.add(layer, {
                "type": "way", "id": -a.orig_id(), "tags": tags, "nodes": [p[4] for p in pts],
                "geometry": [{"lat": round(p[2], 7), "lon": round(p[3], 7)} for p in pts],
            })

    def way(self, w):
        self.seen += 1
        if w.id in self.want:
            pts = []
            for n in w.nodes:
                if not n.location.valid():
                    pts = []
                    break
                lat, lon = n.location.lat, n.location.lon
                pts.append(((lon - W) * MLON, (lat - S) * MLAT, lat, lon, n.ref))
            if len(pts) >= 2:
                self.parts.setdefault(self.want[w.id], []).append((pts[0][4], pts[-1][4], pts))
        t = w.tags
        aw = t.get("aerialway")
        if aw in LIFTS and not any(k in t for k in GONE):
            if "lifts" in KEEP:
                self.emit(w, t, "lifts")
            return
        if is_hut(t) and "name" in t and "pois" in KEEP:
            pts = [(0.0, 0.0, n.location.lat, n.location.lon)
                   for n in w.nodes if n.location.valid()]
            if len(pts) >= 3:
                lat, lon = centroid(pts if pts[0] == pts[-1] else pts + [pts[0]])
                if S <= lat <= N and W <= lon <= E:
                    self.add_hut(lat, lon, t["name"], ele_of(t), "way", w.id, hut_type(t))
        hw = t.get("highway")
        if hw:
            if hw not in ROADS:
                return
            layer = "roads"
        elif t.get("railway") in RAIL:
            layer = "rail"
        elif t.get("natural") == "water" or t.get("waterway") in WATERWAY:
            layer = "water"
        elif t.get("leisure") in PARKS_L or t.get("landuse") in PARKS_LU:
            layer = "green_parks"
        elif t.get("landuse") in FARM:
            layer = "green_farm"
        elif t.get("landuse") == "forest" or t.get("natural") in WOOD_N:
            layer = "green_wood"
        elif t.get("landuse") in LANDUSE or t.get("amenity") in AMEN:
            layer = "landuse"
        elif "building" in t:
            layer = "buildings"
        else:
            return
        if layer not in KEEP:
            return
        self.emit(w, t, layer)

    def emit(self, w, t, layer):
        pts = []
        for n in w.nodes:
            if not n.location.valid():
                return
            lat, lon = n.location.lat, n.location.lon
            pts.append(((lon - W) * MLON, (lat - S) * MLAT, lat, lon, n.ref))
        if len(pts) < 2 or not any(S <= p[2] <= N and W <= p[3] <= E for p in pts):
            return
        if layer == "buildings":
            a = 0.0
            for i in range(len(pts) - 1):
                a += pts[i][0] * pts[i + 1][1] - pts[i + 1][0] * pts[i][1]
            if abs(a) / 2 < 60:  # sheds and kiosks: not worth a polygon at province scale
                return
        tags = {k: t.get(k) for k in KEEP_TAGS if k in t}
        if layer == "roads":
            if w.id in self.hike_ref:
                tags["hiking_ref"] = self.hike_ref[w.id]
            if w.id in self.bike_ref:
                tags["bike_ref"] = self.bike_ref[w.id]
        if layer not in ("roads", "lifts"):
            # Roads and lifts keep every node: the first because junctions are
            # shared node ids, the second because a cable car is four points
            # and Douglas-Peucker would leave two.
            if len(pts) > 3 and pts[0][4] == pts[-1][4]:
                # a closed ring: split it at the point farthest from the start,
                # or Douglas-Peucker sees a zero-length chord and keeps nothing
                far = max(range(1, len(pts) - 1), key=lambda i: (pts[i][0] - pts[0][0]) ** 2 + (pts[i][1] - pts[0][1]) ** 2)
                pts = rdp(pts[: far + 1], 3.0) + rdp(pts[far:], 3.0)[1:]
            else:
                pts = rdp(pts, 3.0)
        self.sink.add(layer, {
            "type": "way", "id": w.id, "tags": tags,
            "nodes": [p[4] for p in pts],
            "geometry": [{"lat": round(p[2], 7), "lon": round(p[3], 7)} for p in pts],
        })


t0 = time.time()
mem = Members()
mem.apply_file(pbf)
print(f"relations: {len(mem.want)} member ways of water and forest relations, {len(mem.hike_ref)} ways on marked hiking routes, {len(mem.bike_ref)} on bike routes, {time.time() - t0:.0f}s")
sink = Sink(out, KEEP)
h = H(mem, sink)
h.apply_file(pbf, locations=True, idx="flex_mem")
print(f"read {h.seen} ways in {time.time() - t0:.0f}s")

# The relations the assembler could not close: chain their member ways by
# shared end nodes; an open chain is drawn closed, the chord lies outside.
cut = 0
for (rid, layer), ways in h.parts.items():
    if rid in h.assembled:
        continue
    used = [False] * len(ways)
    ends = {}
    for i, (a, b, _) in enumerate(ways):
        ends.setdefault(a, []).append(i)
        ends.setdefault(b, []).append(i)
    for i in range(len(ways)):
        if used[i]:
            continue
        used[i] = True
        chain = list(ways[i][2])
        for forward in (True, False):
            while True:
                ref = chain[-1][4] if forward else chain[0][4]
                nxt = next((j for j in ends.get(ref, []) if not used[j]), None)
                if nxt is None:
                    break
                used[nxt] = True
                a, b, pts = ways[nxt]
                seg = pts if (a == ref) == forward else pts[::-1]
                chain = chain + seg[1:] if forward else seg[:-1] + chain
        if len(chain) < 4 or not any(S <= p[2] <= N and W <= p[3] <= E for p in chain):
            continue
        far = max(range(1, len(chain) - 1), key=lambda k: (chain[k][0] - chain[0][0]) ** 2 + (chain[k][1] - chain[0][1]) ** 2)
        chain = rdp(chain[: far + 1], 3.0) + rdp(chain[far:], 3.0)[1:]
        h.sink.add(layer, {
            "type": "way", "id": -rid, "tags": {"natural": "water" if layer == "water" else "wood"},
            "nodes": [p[4] for p in chain],
            "geometry": [{"lat": round(p[2], 7), "lon": round(p[3], 7)} for p in chain],
        })
        cut += 1
print(f"cut relations closed by hand: {cut}")
for _lat, _lon, _t, el in h.huts:
    sink.add("pois", el)
if h.huts:
    print(f"huts: {len(h.huts)} kept, {h.hut_poly} of them from a building or a "
          f"multipolygon (the rest are nodes)")
sink.close()
for k in KEEP:
    p = out / f"{k}.json"
    print(f"  {k:12s} {sink.n[k]:8d} ways  {p.stat().st_size / 1_048_576:6.1f} MB")
if set(KEEP) != set(ALL_LAYERS):
    print(f"  (layers not written: {', '.join(k for k in ALL_LAYERS if k not in KEEP)})")
(out / "bbox.json").write_text(json.dumps({"south": S, "west": W, "north": N, "east": E}))
(out / "name.txt").write_text(name)
print("done")
