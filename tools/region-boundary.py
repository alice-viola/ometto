#!/usr/bin/env python3
"""
The administrative boundary of Trentino-Alto Adige/Sudtirol, and of its two
provinces, straight out of an OSM extract.

    python3 tools/region-boundary.py <extract.osm.pbf> web/public/region.geojson \
            web/public/region-bbox.json [--eps 20]

Writes a WGS84 (lon, lat) FeatureCollection with three features -- the region
(admin_level 4) and the provinces of Trento and Bolzano (admin_level 6) -- and
the region's bounding box as {"south","west","north","east"}.

The boundary relations are assembled by hand rather than with the area handler:
a boundary relation is a bag of member ways in no particular order and in no
particular direction, so the ways are chained by their shared end nodes into
rings, the rings tagged outer or inner are kept apart, and each inner ring is
given to the outer ring that contains it (Lake Garda's province boundaries have
none, but the enclaves in the Alps do). Rings are then simplified with
Douglas-Peucker in METRES -- a degree of longitude is 76 km here and a degree of
latitude 111 km, so simplifying in degrees would thin the two axes unequally.
"""
import json
import math
import pathlib
import sys
import time

import osmium

# The three relations. Checked against the name in the extract; if an id has
# moved (relations do get re-created), the tag search below finds it anyway.
WANT = [
    (45757, 4, "region", "Trentino-Alto Adige/Südtirol", "Trentino-Alto Adige/Südtirol"),
    (45756, 6, "province", "Provincia di Trento", "Trento"),
    (47046, 6, "province", "Bolzano - Bozen", "Bolzano"),
]


def rdp(pts, eps):
    """Douglas-Peucker on [(x, y, lon, lat)] in metres; returns the kept points."""
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


class Rels(osmium.SimpleHandler):
    """Pass one: the member ways of the three relations, with their roles."""

    def __init__(self):
        super().__init__()
        self.found = {}          # relation id -> (level, kind, name, label)
        self.members = {}        # relation id -> [(way id, role)]
        self.way_ids = set()

    def relation(self, r):
        t = r.tags
        hit = None
        for rid, lvl, kind, osm_name, label in WANT:
            if r.id == rid:
                hit = (rid, lvl, kind, t.get("name", osm_name), label)
                break
            if (t.get("boundary") == "administrative"
                    and t.get("admin_level") == str(lvl)
                    and t.get("name") == osm_name):
                hit = (r.id, lvl, kind, osm_name, label)
                break
        if hit is None:
            return
        rid, lvl, kind, name, label = hit
        if rid in self.found:
            return
        self.found[rid] = (lvl, kind, name, label)
        ms = [(m.ref, m.role or "outer") for m in r.members if m.type == "w"]
        self.members[rid] = ms
        self.way_ids.update(w for w, _ in ms)


class Ways(osmium.SimpleHandler):
    """Pass two: the geometry of exactly those ways."""

    def __init__(self, want):
        super().__init__()
        self.want = want
        self.geom = {}
        self.dropped = 0

    def way(self, w):
        if w.id not in self.want:
            return
        pts = []
        for n in w.nodes:
            if not n.location.valid():
                self.dropped += 1
                return
            pts.append((n.ref, n.location.lon, n.location.lat))
        if len(pts) >= 2:
            self.geom[w.id] = pts


def chain(ways):
    """Chain [(first, last, pts)] into rings by shared end nodes.

    A point is (x, y, lon, lat, node id): the node id is what two ways share,
    and comparing coordinates instead would join two ways that merely pass
    close to each other.

    Returns (closed rings, open chains). A chain that will not close is a way
    the extract did not carry; it is closed with a chord and reported.
    """
    ends = {}
    for i, (a, b, _) in enumerate(ways):
        ends.setdefault(a, []).append(i)
        ends.setdefault(b, []).append(i)
    used = [False] * len(ways)
    closed, open_ = [], []
    for i in range(len(ways)):
        if used[i]:
            continue
        used[i] = True
        ch = list(ways[i][2])
        for forward in (True, False):
            while True:
                ref = ch[-1][NID] if forward else ch[0][NID]
                nxt = next((j for j in ends.get(ref, []) if not used[j]), None)
                if nxt is None:
                    break
                used[nxt] = True
                a, _b, pts = ways[nxt]
                seg = pts if (a == ref) == forward else pts[::-1]
                ch = ch + seg[1:] if forward else seg[:-1] + ch
                if ch[0][NID] == ch[-1][NID]:
                    break
            if ch[0][NID] == ch[-1][NID]:
                break
        (closed if ch[0][NID] == ch[-1][NID] and len(ch) >= 4 else open_).append(ch)
    return closed, open_


NID = 4   # index of the node id in a projected point


def ring_area(ring):
    """Twice the signed area in metres squared (x, y first two components)."""
    a = 0.0
    for i in range(len(ring) - 1):
        a += ring[i][0] * ring[i + 1][1] - ring[i + 1][0] * ring[i][1]
    return a


def inside(pt, ring):
    """Crossing number, on the projected x/y."""
    x, y = pt[0], pt[1]
    c = False
    for i in range(len(ring) - 1):
        x0, y0 = ring[i][0], ring[i][1]
        x1, y1 = ring[i + 1][0], ring[i + 1][1]
        if (y0 > y) != (y1 > y) and x < x0 + (y - y0) * (x1 - x0) / ((y1 - y0) or 1e-12):
            c = not c
    return c


def main(argv):
    if len(argv) < 3:
        sys.exit(__doc__.strip())
    pbf, out_geo, out_bbox = argv[0], pathlib.Path(argv[1]), pathlib.Path(argv[2])
    eps = 20.0
    if "--eps" in argv:
        eps = float(argv[argv.index("--eps") + 1])

    t0 = time.time()
    rels = Rels()
    rels.apply_file(pbf)
    if not rels.found:
        sys.exit("no admin_level 4/6 boundary relation found in the extract")
    print(f"relations: {len(rels.found)}, {len(rels.way_ids)} member ways "
          f"({time.time()-t0:.1f}s)")
    for rid, (lvl, kind, name, label) in rels.found.items():
        print(f"  r{rid} level {lvl} {label:9s} {name}  {len(rels.members[rid])} members")

    ways = Ways(rels.way_ids)
    ways.apply_file(pbf, locations=True, idx="flex_mem")
    print(f"ways: {len(ways.geom)} of {len(rels.way_ids)} carried geometry "
          f"({time.time()-t0:.1f}s)")

    # One projection for every feature, so the 20 m is the same 20 m everywhere.
    lats = [p[2] for g in ways.geom.values() for p in g]
    lons = [p[1] for g in ways.geom.values() for p in g]
    lat0 = (min(lats) + max(lats)) / 2
    m_lat = 111_132.92 - 559.82 * math.cos(2 * math.radians(lat0))
    m_lon = 111_412.84 * math.cos(math.radians(lat0))

    def project(lon, lat):
        return (lon * m_lon, lat * m_lat, lon, lat)

    feats = []
    region_bbox = None
    for rid, (lvl, kind, name, label) in sorted(rels.found.items(), key=lambda kv: kv[1][0]):
        by_role = {"outer": [], "inner": []}
        missing = 0
        for wid, role in rels.members[rid]:
            g = ways.geom.get(wid)
            if g is None:
                missing += 1
                continue
            role = "inner" if role == "inner" else "outer"
            pts = [project(lon, lat) + (nid,) for nid, lon, lat in g]
            by_role[role].append((g[0][0], g[-1][0], pts))

        polys = []
        holes = []
        for role, group in by_role.items():
            if not group:
                continue
            closed, open_ = chain(group)
            for ch in open_:
                if len(ch) >= 4:          # closed with a chord, as for a cut lake
                    closed.append(ch + [ch[0]])
            for ring in closed:
                ring = [(p[0], p[1], p[2], p[3]) for p in ring]
                far = max(range(1, len(ring) - 1),
                          key=lambda k: (ring[k][0] - ring[0][0]) ** 2 + (ring[k][1] - ring[0][1]) ** 2)
                simp = rdp(ring[: far + 1], eps) + rdp(ring[far:], eps)[1:]
                if len(simp) < 4:
                    continue
                if simp[0][2:4] != simp[-1][2:4]:
                    simp.append(simp[0])
                (holes if role == "inner" else polys).append(simp)

        # Biggest ring first; every hole goes to the first outer ring holding it.
        polys.sort(key=lambda r: -abs(ring_area(r)))
        rings = [[p] for p in polys]
        for h in holes:
            for k, p in enumerate(polys):
                if inside(h[0], p):
                    rings[k].append(h)
                    break

        def out(ring, ccw):
            r = ring if (ring_area(ring) > 0) == ccw else ring[::-1]
            return [[round(p[2], 6), round(p[3], 6)] for p in r]

        coords = [[out(p[0], True)] + [out(h, False) for h in p[1:]] for p in rings]
        pts_n = sum(len(r) for p in coords for r in p)
        la = [p[3] for r in polys for p in r]
        lo = [p[2] for r in polys for p in r]
        bbox = {"south": round(min(la), 6), "west": round(min(lo), 6),
                "north": round(max(la), 6), "east": round(max(lo), 6)}
        if kind == "region":
            region_bbox = bbox
        feats.append({
            "type": "Feature",
            "properties": {"name": label, "osm_name": name, "kind": kind,
                           "admin_level": lvl, "osm_id": rid,
                           "rings": len(coords), "points": pts_n},
            "geometry": {"type": "MultiPolygon", "coordinates": coords},
        })
        print(f"  {label:9s} {len(coords)} polygon(s), {pts_n} points after {eps:.0f} m "
              f"(missing member ways: {missing}), bbox {bbox['south']}..{bbox['north']} N "
              f"{bbox['west']}..{bbox['east']} E")

    out_geo.parent.mkdir(parents=True, exist_ok=True)
    out_geo.write_text(json.dumps({"type": "FeatureCollection", "features": feats},
                                  separators=(",", ":"), ensure_ascii=False))
    if region_bbox is None:
        sys.exit("no admin_level 4 feature: refusing to write a bbox")
    out_bbox.write_text(json.dumps(region_bbox))
    print(f"wrote {out_geo} ({out_geo.stat().st_size/1e6:.2f} MB) and {out_bbox}")
    print(f"done in {time.time()-t0:.1f}s")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
