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
"places" layer for the labels, plus the crags (nodes, cliff lines at their
midpoint, site relations at the mean of their routes; see is_crag), plus the
huts -- which are a node only when
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
              "green_wood", "landuse", "buildings", "places", "pois", "crags")

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
# A climbing crag, however the mappers of the day tagged it: `climbing=crag`,
# or `climbing=area` for a group of crags, as the wiki says now; the older
# `sport=climbing` on a cliff, a rock or a bare node; or -- most of Trentino --
# `leisure=sports_centre` + `sport=climbing`, because the Italian for a crag is
# "palestra di roccia", a rock gym, and the mappers took the word at its word.
# A real gym has a building, says indoor, or is called a Halle. Not a route
# (`climbing=route*`: one line of bolts, three hundred of them mapped one by
# one), not a shop, a guide's office, an artificial wall or a signpost that
# mentions the sport. A named cliff with no sport tag counts only when its
# name says falesia or Klettergarten.
CRAG_KINDS = {"crag", "area"}
ROCKY = {"cliff", "rock", "bare_rock", "stone", "arete"}
NOT_A_CRAG = ("man_made", "club", "shop", "office", "amenity", "highway",
              "landuse", "information", "railway")
NOT_A_CRAG_VALUE = {"route", "route_bottom", "route_top", "abseil", "decent", "descent",
                    "scramble_route", "gym", "wall"}
GYM_WORDS = re.compile(r"halle\b|indoor|zentrum|stadium|centro d.?arrampicata|"
                       r"palestra (?:di |d')?arrampicata|\bboulder\b|rockarena|"
                       r"vertikale|\bcube\b|\bguide\b")
CRAG_WORDS = re.compile(r"falesi|klettergarten|palestra di roccia|\bcrag\b|arrampicat")
CRAG_TAGS = ("name", "ele", "natural", "sport", "climbing", "website", "description",
             "site", "type")
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


def fold(s):
    n = unicodedata.normalize("NFKD", s or "")
    return "".join(c for c in n if not unicodedata.combining(c)).lower()


def is_crag(t):
    """A crag, a sector of one, or an area of them -- and nothing else that
    carries the word climbing."""
    c = t.get("climbing")
    if c in NOT_A_CRAG_VALUE or t.get("indoor") in ("yes", "only"):
        return False
    if t.get("building") not in (None, "no"):
        return False
    leisure = t.get("leisure")
    if leisure == "sports_centre" and GYM_WORDS.search(fold(t.get("name"))):
        return False
    if c in CRAG_KINDS:
        return True
    if leisure not in (None, "sports_centre", "pitch"):
        return False
    if any(k in t for k in NOT_A_CRAG) or t.get("type") == "route":
        return False
    if t.get("tourism") in ("information", "viewpoint"):
        return False
    nat = t.get("natural")
    if t.get("sport") != "climbing" and c != "yes":
        return nat in ROCKY and bool(CRAG_WORDS.search(fold(t.get("name"))))
    if leisure == "sports_centre":
        return True
    if leisure == "pitch":
        return nat in ROCKY  # a pitch is an artificial wall unless it stands on rock
    return nat in ROCKY or nat is None


def crag_tags(t):
    """What the crag layer keeps of the tags: the identity, and every
    climbing:* detail (grades, aspect, rock, length, routes, styles)."""
    return {k: v for k, v in dict(t).items() if k in CRAG_TAGS or k.startswith("climbing:")}


def along_midpoint(pts):
    """The point half way along a line of (lat, lon): where a cliff line
    "is", better than the mean of its corners when the wall bends."""
    if len(pts) == 1:
        return pts[0]
    cum = [0.0]
    for a, b in zip(pts, pts[1:]):
        cum.append(cum[-1] + math.hypot((b[0] - a[0]) * MLAT, (b[1] - a[1]) * MLON))
    half = cum[-1] / 2
    for i in range(1, len(pts)):
        if cum[i] >= half:
            seg = cum[i] - cum[i - 1]
            f = (half - cum[i - 1]) / seg if seg > 0 else 0.0
            a, b = pts[i - 1], pts[i]
            return (a[0] + (b[0] - a[0]) * f, a[1] + (b[1] - a[1]) * f)
    return pts[-1]


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
        # A crag drawn as a site relation (its routes as members) or an area
        # of crags: the relation is the thing, and a member's parent.
        self.crag_rel = {}     # relation id -> {"tags", "members": [(type, ref)]}
        self.crag_member = {}  # (type, ref) -> the first crag relation it is in

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
        if "crags" in KEEP and "name" in t and is_crag(t):
            self.crag_rel[r.id] = {"tags": crag_tags(t), "members": [(m.type, m.ref) for m in r.members]}
            for m in r.members:
                self.crag_member.setdefault((m.type, m.ref), r.id)
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
        # Crags are buffered too: a sector's parent is a relation read last,
        # and a site relation stands where its members are.
        self.crag_rel, self.crag_member = mem.crag_rel, mem.crag_member
        self.crags = []
        self.crag_pos = {}     # (type, ref) of a relation member -> (lat, lon)
        self.crag_relpos = {}  # relation id -> (lat, lon), for the multipolygons
        self.crag_routes = {}  # relation id -> routes among its members
        self.crag_done = set() # relations placed by the area assembler

    def node(self, n):
        t = n.tags
        if "crags" in KEEP:
            self.crag_node(n)
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

    def crag_node(self, n):
        t = n.tags
        key = ("n", n.id)
        rid = self.crag_member.get(key)
        if rid is not None:
            self.crag_pos[key] = (n.location.lat, n.location.lon)
            if t.get("climbing") in ("route", "route_bottom", "route_top"):
                self.crag_routes[rid] = self.crag_routes.get(rid, 0) + 1
        if "name" in t and is_crag(t):
            self.add_crag(n.location.lat, n.location.lon, t, "node", n.id)

    def crag_way(self, w):
        t = w.tags
        key = ("w", w.id)
        member = key in self.crag_member
        own = "name" in t and is_crag(t)
        if not member and not own:
            return
        pts = [(n.location.lat, n.location.lon) for n in w.nodes if n.location.valid()]
        if not pts:
            return
        if member:
            self.crag_pos[key] = (sum(p[0] for p in pts) / len(pts), sum(p[1] for p in pts) / len(pts))
            if t.get("climbing") in ("route", "route_bottom", "route_top"):
                rid = self.crag_member[key]
                self.crag_routes[rid] = self.crag_routes.get(rid, 0) + 1
        if own:
            if len(pts) >= 3 and w.nodes[0].ref == w.nodes[-1].ref:
                lat, lon = centroid([(0.0, 0.0, p[0], p[1]) for p in pts])
            else:
                lat, lon = along_midpoint(pts)
            self.add_crag(lat, lon, t, "way", w.id)

    def add_crag(self, lat, lon, t, typ, oid, routes=0):
        """One crag, as it was mapped; the layer script merges the doubles."""
        if not (S <= lat <= N and W <= lon <= E):
            return
        el = {"type": typ, "id": oid, "lat": round(lat, 7), "lon": round(lon, 7),
              "tags": crag_tags(t)}
        if routes:
            el["routes"] = routes
        self.crags.append(el)

    def place_crag_relations(self):
        """A site relation has no geometry of its own: it stands where its
        members are -- the mean of the routes it groups (a crag) or of the
        crags it groups (an area). Three rounds, since an area's members can be
        relations themselves. Then every crag that is a member of a named
        relation learns its parent. Returns how many relations had nothing
        placeable in them."""
        pos = dict(self.crag_relpos)
        pending = {rid: rel for rid, rel in self.crag_rel.items() if rid not in self.crag_done}
        for _ in range(3):
            for rid, rel in list(pending.items()):
                pts = []
                for mtype, ref in rel["members"]:
                    p = self.crag_pos.get((mtype, ref)) if mtype in ("n", "w") else pos.get(ref)
                    if p:
                        pts.append(p)
                if pts:
                    pos[rid] = (sum(p[0] for p in pts) / len(pts), sum(p[1] for p in pts) / len(pts))
                    del pending[rid]
        for rid, (lat, lon) in pos.items():
            if rid in self.crag_done:
                continue
            self.add_crag(lat, lon, self.crag_rel[rid]["tags"], "relation", rid,
                          routes=self.crag_routes.get(rid, 0))
        letter = {"node": "n", "way": "w", "relation": "r"}
        for el in self.crags:
            rid = self.crag_member.get((letter[el["type"]], el["id"]))
            if rid is None:
                continue
            name = self.crag_rel[rid]["tags"].get("name")
            if name and name != el["tags"].get("name"):
                el["parent"] = name
        return len(pending)

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
        if "crags" in KEEP and "name" in t and is_crag(t):
            big = None
            for ring in a.outer_rings():
                pts = [(0.0, 0.0, n.location.lat, n.location.lon)
                       for n in ring if n.location.valid()]
                if len(pts) >= 4 and (big is None or len(pts) > len(big)):
                    big = pts
            if big is not None:
                lat, lon = centroid(big)
                self.crag_relpos[a.orig_id()] = (lat, lon)
                self.crag_done.add(a.orig_id())
                self.add_crag(lat, lon, t, "relation", a.orig_id())
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
        if "crags" in KEEP:
            self.crag_way(w)
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
if "crags" in KEEP:
    unplaced = h.place_crag_relations()
    for el in h.crags:
        sink.add("crags", el)
    by = {}
    for el in h.crags:
        by[el["type"]] = by.get(el["type"], 0) + 1
    print(f"crags: {len(h.crags)} named crags, sectors and areas kept ("
          + ", ".join(f"{v} {k}s" for k, v in sorted(by.items())) + f"), "
          f"{sum(1 for el in h.crags if el.get('parent'))} inside a named relation, "
          f"{unplaced} site relations with nothing placeable in them")
sink.close()
for k in KEEP:
    p = out / f"{k}.json"
    print(f"  {k:12s} {sink.n[k]:8d} ways  {p.stat().st_size / 1_048_576:6.1f} MB")
if set(KEEP) != set(ALL_LAYERS):
    print(f"  (layers not written: {', '.join(k for k in ALL_LAYERS if k not in KEEP)})")
(out / "bbox.json").write_text(json.dumps({"south": S, "west": W, "north": N, "east": E}))
(out / "name.txt").write_text(name)
print("done")
