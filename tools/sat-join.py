#!/usr/bin/env python3
"""
Join the Province of Trento's official SAT trail catalogue to the map's trails.

    python3 tools/sat-join.py <sat_shapefile_basename> <map.json> <sat.geojson>
                              [--radius 60] [--frac 0.8] [--geometry-pass]
                              [--geom-radius 30] [--geom-frac 0.95] [--dry-run]

The catalogue (3,879 trails, ETRS89 / UTM 32N) carries what OpenStreetMap does
not: the SAT's own grade for each trail -- T tourist, E hiking, EE experienced
hiker, EEA equipped/via ferrata -- and its official number and name. The map's
hiking stretches carry the route number OSM stamped on them (`hr`).

The join is by NUMBER and checked by GEOMETRY, and it must be both: a number is
only unique within a zone (the catalogue writes E401 and O401, OSM writes 401
for both), and geometry alone would join a trail to whatever parallel path runs
beside it. So each stretch is offered the catalogue rows whose number matches
once the zone prefix and the leading zeros are taken off, and a row is accepted
only when at least `frac` of the stretch's points lie within `radius` metres of
that row's line.

A stretch can also be on a catalogue trail under a name rather than a number:
the Sentiero della Pace, the Sentiero Italia, E5, the Alte Vie all run along
numbered SAT trails and OSM stamps THEIR name on the way. --geometry-pass gives
those a second chance on geometry alone, at a much tighter tolerance (95% of
the stretch's points within 30 m, and only for stretches that are on a marked
route already), which is the same trail on the ground rather than a guess.

Writes three fields on every stretch that matched -- "sat" (T/E/EE/EEA),
"satno" (the catalogue number), "satname" (the catalogue name, when it has one)
-- back into the map file, and the catalogue's own lines to a GeoJSON the page
can draw. Every other byte of the map file is left exactly as it was.
"""
import json
import math
import os
import pathlib
import re
import sys
import time
from collections import defaultdict

import numpy as np
import shapefile

sys.path.insert(0, str(pathlib.Path(__file__).resolve().parent))
import dem as demtool
from utm32 import to_lonlat

GRADES = ("T", "E", "EE", "EEA")



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

def norm(v):
    return re.sub(r"[^A-Z0-9]", "", (v or "").upper())


def keys(v):
    """The forms of a trail number two catalogues might agree on.

    "E518" -> {E518, 518}; "A01T" -> {A01T, 1T}; "406B" -> {406B}; and
    "E521A-01A", a section of trail E521A, -> {E521A01A, E521A, 521A}.

    Two things are deliberately NOT keys. The bare digits without the suffix
    letter: 1A and 1B are different trails that run side by side, and geometry
    could not tell them apart. And the digits of a MULTI-letter prefix: the
    catalogue's long-distance routes (AVO, CVO, CVE, BVE) are numbered inside
    their own series, so CVO011 is not trail 11 -- and because such a route
    follows the local trails, the 60 m check would happily confirm it.
    """
    out = set()
    for tok in (norm(v), norm(re.split(r"[^A-Za-z0-9]", (v or "").strip())[0])):
        if not tok:
            continue
        out.add(tok)
        m = re.fullmatch(r"([A-Z]?)0*(\d+)([A-Z]*)", tok)
        if m and m.group(2):
            out.add(m.group(2) + m.group(3))
    return out


def grade_of(raw):
    """EEA-PD and friends are via ferrata sub-grades; the field wants EEA."""
    g = (raw or "").strip().upper().split("-")[0]
    return g if g in GRADES else ""


def minutes(raw):
    """t_andata as minutes: "2:30", "2.30", "2 h 30", "45'", "1 ora"."""
    s = (raw or "").strip().lower()
    if not s:
        return None
    m = re.fullmatch(r"(\d+)\s*[:.,h]\s*(\d{1,2})'?", s)
    if m:
        return int(m.group(1)) * 60 + int(m.group(2))
    m = re.fullmatch(r"(\d+)\s*(?:h|ore|ora)?", s)
    if m:
        return int(m.group(1)) * 60
    m = re.fullmatch(r"(\d+)\s*(?:'|min|minuti)", s)
    if m:
        return int(m.group(1))
    return None


def rdp(xy, eps):
    """Douglas-Peucker on an (N, 2) array of METRES; returns a keep mask."""
    n = len(xy)
    keep = np.zeros(n, dtype=bool)
    if n < 3:
        keep[:] = True
        return keep
    keep[0] = keep[-1] = True
    stack = [(0, n - 1)]
    while stack:
        a, b = stack.pop()
        if b - a < 2:
            continue
        ax, ay = xy[a]
        dx, dy = xy[b] - xy[a]
        L = math.hypot(dx, dy)
        seg = xy[a + 1 : b]
        if L < 1e-9:
            d = np.hypot(seg[:, 0] - ax, seg[:, 1] - ay)
        else:
            d = np.abs(dy * (seg[:, 0] - ax) - dx * (seg[:, 1] - ay)) / L
        i = int(np.argmax(d))
        if d[i] > eps:
            k = a + 1 + i
            keep[k] = True
            stack.append((a, k))
            stack.append((k, b))
    return keep


def seg_dist(px, py, x0, y0, x1, y1):
    """Distance from each point (P,) to each segment (S,), as a (P, S) array."""
    dx = x1 - x0
    dy = y1 - y0
    L2 = dx * dx + dy * dy
    L2 = np.where(L2 == 0, 1e-12, L2)
    t = ((px[:, None] - x0[None, :]) * dx[None, :]
         + (py[:, None] - y0[None, :]) * dy[None, :]) / L2[None, :]
    np.clip(t, 0.0, 1.0, out=t)
    ex = x0[None, :] + t * dx[None, :] - px[:, None]
    ey = y0[None, :] + t * dy[None, :] - py[:, None]
    return np.hypot(ex, ey)


def main(argv):
    if len(argv) < 3:
        sys.exit(__doc__.strip())
    shp, mappath, geopath = argv[0], pathlib.Path(argv[1]), pathlib.Path(argv[2])
    radius = float(argv[argv.index("--radius") + 1]) if "--radius" in argv else 60.0
    frac = float(argv[argv.index("--frac") + 1]) if "--frac" in argv else 0.8
    dry = "--dry-run" in argv
    geom_pass = "--geometry-pass" in argv
    g_radius = float(argv[argv.index("--geom-radius") + 1]) if "--geom-radius" in argv else 30.0
    g_frac = float(argv[argv.index("--geom-frac") + 1]) if "--geom-frac" in argv else 0.95
    t0 = time.time()

    # ------------------------------------------------------------ catalogue --
    r = open_shp(shp)
    fields = [f[0] for f in r.fields[1:]]
    idx = {name: i for i, name in enumerate(fields)}
    recs = r.records()
    shapes = r.shapes()
    print(f"catalogue: {len(recs)} trails, "
          f"{sum(len(s.points) for s in shapes)} points ({time.time()-t0:.1f}s)")

    # One conversion for every point in the file, then split back per trail.
    allpts = np.array([p for s in shapes for p in s.points], dtype=np.float64)
    lon, lat = to_lonlat(allpts[:, 0], allpts[:, 1])
    off, k = [], 0
    for s in shapes:
        off.append((k, k + len(s.points)))
        k += len(s.points)

    # ------------------------------------------------------------- the map --
    print(f"read {mappath} ({mappath.stat().st_size/1e6:.1f} MB)")
    text = mappath.read_text()
    members, _close = demtool.scan_top_level(text)
    span = {key: (s, e) for key, s, e in members}
    dec = json.JSONDecoder()

    def value_start(key):
        s, _e = span[key]
        i = text.index(":", s) + 1
        while text[i] in " \t\r\n":
            i += 1
        return i

    meta = dec.raw_decode(text, value_start("meta"))[0]
    pts = demtool.points_xy(text, value_start("points"), span["points"][1])
    roads = dec.raw_decode(text, value_start("roads"))[0]
    print(f"  {len(roads)} stretches, {len(pts)} points ({time.time()-t0:.1f}s)")

    lat0, lon0 = meta["origin"]["lat"], meta["origin"]["lon"]
    m_lat, m_lon = float(meta["mPerDegLat"]), float(meta["mPerDegLon"])
    sx = (lon - lon0) * m_lon                 # the map's own frame, y growing south
    sy = (lat0 - lat) * m_lat

    # ------------------------------------------------- catalogue, indexed --
    trails = []
    by_key = defaultdict(list)
    for i, rec in enumerate(recs):
        a, b = off[i]
        parts = list(shapes[i].parts) + [b - a]
        segs = []
        for p0, p1 in zip(parts, parts[1:]):
            if p1 - p0 >= 2:
                segs.append((a + p0, a + p1))
        if not segs:
            continue
        numero = (rec[idx["numero"]] or "").strip()
        x0s = np.concatenate([sx[s:e - 1] for s, e in segs])
        y0s = np.concatenate([sy[s:e - 1] for s, e in segs])
        x1s = np.concatenate([sx[s + 1:e] for s, e in segs])
        y1s = np.concatenate([sy[s + 1:e] for s, e in segs])
        t = {
            "i": i, "numero": numero, "segs": segs,
            "grade": grade_of(rec[idx["difficolta"]]),
            "grade_raw": (rec[idx["difficolta"]] or "").strip(),
            "name": (rec[idx["denominaz"]] or "").strip(),
            "start": (rec[idx["loc_inizio"]] or "").strip(),
            "end": (rec[idx["loc_fine"]] or "").strip(),
            "len": rec[idx["lun_inclin"]],
            "time": minutes(rec[idx["t_andata"]] if "t_andata" in idx else ""),
            "x0": x0s, "y0": y0s, "x1": x1s, "y1": y1s,
            "bbox": (min(x0s.min(), x1s.min()), min(y0s.min(), y1s.min()),
                     max(x0s.max(), x1s.max()), max(y0s.max(), y1s.max())),
            "hits": 0, "hit_m": 0.0,
        }
        trails.append(t)
        for key in keys(numero):
            by_key[key].append(len(trails) - 1)
    numbered = sum(1 for t in trails if t["numero"])
    print(f"  {len(trails)} usable trails, {numbered} with a number, "
          f"{len(by_key)} distinct keys, {sum(1 for t in trails if t['grade'])} graded")

    # ------------------------------------------------------------- the join --
    stats = {"no_hr": 0, "no_key": 0, "no_candidate": 0, "too_far": 0, "joined": 0}
    best_frac_hist = []
    graded = defaultdict(int)
    t_join = time.time()
    for road in roads:
        hr = road.get("hr")
        if not hr:
            stats["no_hr"] += 1
            continue
        cand = set()
        for token in re.split(r"[;,/]", hr):
            for key in keys(token):
                cand.update(by_key.get(key, ()))
        if not cand:
            stats["no_key"] += 1
            continue
        p = road["p"]
        px = pts[p, 0]
        py = pts[p, 1]
        x_lo, x_hi = px.min() - radius, px.max() + radius
        y_lo, y_hi = py.min() - radius, py.max() + radius
        best = None
        for ti in cand:
            t = trails[ti]
            bx0, by0, bx1, by1 = t["bbox"]
            if bx1 < x_lo or bx0 > x_hi or by1 < y_lo or by0 > y_hi:
                continue
            d = seg_dist(px, py, t["x0"], t["y0"], t["x1"], t["y1"]).min(axis=1)
            f = float((d <= radius).mean())
            med = float(np.median(d))
            if f >= frac:
                # The stretch can only carry one grade, but for "which trails
                # of the catalogue does the map know?" every trail it lies on
                # counts: several sections of one trail, and the long-distance
                # routes that run along it, all pass here.
                t["hits"] += 1
            if best is None or (f, -med) > (best[0], -best[1]):
                best = (f, med, ti)
        if best is None:
            stats["no_candidate"] += 1
            continue
        best_frac_hist.append(best[0])
        if best[0] < frac:
            stats["too_far"] += 1
            continue
        t = trails[best[2]]
        if t["grade"]:
            road["sat"] = t["grade"]
            graded[t["grade"]] += 1
        if t["numero"]:
            road["satno"] = t["numero"]
        if t["name"]:
            road["satname"] = t["name"]
        stats["joined"] += 1
    print(f"  join in {time.time()-t_join:.1f}s")

    if geom_pass:
        # A grid over the catalogue's segments, so "what is near this stretch"
        # is a lookup in nine cells and not a scan of two thousand trails.
        t_g = time.time()
        CELL = 500.0
        grid = defaultdict(list)
        for ti, t in enumerate(trails):
            gx = np.floor(np.minimum(t["x0"], t["x1"]) / CELL).astype(np.int64)
            gy = np.floor(np.minimum(t["y0"], t["y1"]) / CELL).astype(np.int64)
            for cx, cy in set(zip(gx.tolist(), gy.tolist())):
                grid[(cx, cy)].append(ti)
        added = 0
        for road in roads:
            if "sat" in road or not road.get("hr"):
                continue
            p = road["p"]
            px = pts[p, 0]
            py = pts[p, 1]
            cand = set()
            for x, y in zip(px, py):
                cx, cy = int(x // CELL), int(y // CELL)
                for dx in (-1, 0, 1):
                    for dy in (-1, 0, 1):
                        cand.update(grid.get((cx + dx, cy + dy), ()))
            best = None
            for ti in cand:
                t = trails[ti]
                if not t["grade"]:
                    continue
                bx0, by0, bx1, by1 = t["bbox"]
                if (bx1 < px.min() - g_radius or bx0 > px.max() + g_radius
                        or by1 < py.min() - g_radius or by0 > py.max() + g_radius):
                    continue
                d = seg_dist(px, py, t["x0"], t["y0"], t["x1"], t["y1"]).min(axis=1)
                f = float((d <= g_radius).mean())
                if f < g_frac:
                    continue
                med = float(np.median(d))
                if best is None or (f, -med) > (best[0], -best[1]):
                    best = (f, med, ti)
            if best is None:
                continue
            t = trails[best[2]]
            road["sat"] = t["grade"]
            if t["numero"]:
                road["satno"] = t["numero"]
            if t["name"]:
                road["satname"] = t["name"]
            t["hits"] += 1
            graded[t["grade"]] += 1
            added += 1
        stats["geometry"] = added
        stats["joined"] += added
        print(f"  geometry pass ({g_frac:.0%} of points within {g_radius:.0f} m): "
              f"{added} more stretches in {time.time()-t_g:.1f}s")

    with_hr = len(roads) - stats["no_hr"]
    hist = np.array(best_frac_hist) if best_frac_hist else np.zeros(0)
    print(f"\njoin rate")
    print(f"  stretches in the map            {len(roads)}")
    print(f"  ... carrying an OSM route ref   {with_hr}")
    print(f"  ... ref not in the catalogue    {stats['no_key']}")
    print(f"  ... number matched, too far     {stats['too_far']} "
          f"(under {frac:.0%} of points within {radius:.0f} m)")
    print(f"  ... number matched, elsewhere   {stats['no_candidate']} (bbox apart)")
    if stats.get("geometry"):
        print(f"  ... joined on geometry alone    {stats['geometry']} "
              f"(ref not a catalogue number, but on the line)")
    print(f"  ... JOINED                      {stats['joined']}"
          f"  ({100.0*stats['joined']/max(with_hr,1):.1f}% of the ones with a ref,"
          f" {100.0*stats['joined']/max(len(roads),1):.1f}% of all stretches)")
    print(f"  of those, with a SAT grade      {sum(graded.values())}  "
          + ", ".join(f"{g} {graded[g]}" for g in GRADES if graded[g]))
    if len(hist):
        for f in (1.0, 0.9, 0.8, 0.5):
            print(f"    at frac >= {f:.1f}: {int((hist >= f).sum())} stretches would join")
    hit = sum(1 for t in trails if t["hits"])
    hit_n = sum(1 for t in trails if t["hits"] and t["numero"])
    print(f"  catalogue trails with an OSM counterpart   {hit} of {len(trails)} "
          f"({hit_n} of {numbered} numbered)")
    print(f"  catalogue trails with NO OSM counterpart   {len(trails)-hit} "
          f"({numbered-hit_n} of them numbered)")

    # --------------------------------------------------------- sat.geojson --
    feats = []
    for t in trails:
        lines = []
        for s, e in t["segs"]:
            utm = allpts[s:e]
            m = rdp(utm, 10.0)
            lines.append([[round(float(lon[s:e][k]), 6), round(float(lat[s:e][k]), 6)]
                          for k in np.nonzero(m)[0]])
        lines = [l for l in lines if len(l) >= 2]
        if not lines:
            continue
        glen = 0.0
        for s, e in t["segs"]:
            d = np.diff(allpts[s:e], axis=0)
            glen += float(np.hypot(d[:, 0], d[:, 1]).sum())
        props = {
            "numero": t["numero"],
            "grade": t["grade"],
            "name": t["name"],
            "start": t["start"],
            "end": t["end"],
            "length_m": int(t["len"]) if t["len"] not in (None, "") else int(round(glen)),
            "osm_stretches": t["hits"],
        }
        if t["grade_raw"] and t["grade_raw"] != t["grade"]:
            props["grade_raw"] = t["grade_raw"]
        if t["time"] is not None:
            props["time_min"] = t["time"]
        feats.append({
            "type": "Feature", "properties": props,
            "geometry": ({"type": "LineString", "coordinates": lines[0]} if len(lines) == 1
                         else {"type": "MultiLineString", "coordinates": lines}),
        })
    if not dry:
        geopath.parent.mkdir(parents=True, exist_ok=True)
        geopath.write_text(json.dumps({"type": "FeatureCollection", "features": feats},
                                      separators=(",", ":"), ensure_ascii=False))
        print(f"\nwrote {geopath} ({geopath.stat().st_size/1e6:.1f} MB, {len(feats)} features, "
              f"{sum(len(c) for f in feats for c in ([f['geometry']['coordinates']] if f['geometry']['type']=='LineString' else f['geometry']['coordinates']))} points)")

    # ------------------------------------------------- the map, written back --
    if dry:
        print("dry run: the map was not touched")
        return 0
    roads_json = json.dumps(roads, separators=(",", ":"))
    tmp = mappath.with_suffix(mappath.suffix + ".tmp")
    step = 1 << 23
    with open(tmp, "w") as f:
        f.write("{")
        for n, (key, s, e) in enumerate(members):
            if n:
                f.write(",")
            if key == "roads":
                f.write('"roads":')
                f.write(roads_json)
                continue
            for c in range(s, e, step):
                f.write(text[c : min(c + step, e)])
        f.write("}")
    os.replace(tmp, mappath)
    print(f"wrote {mappath} ({mappath.stat().st_size/1e6:.1f} MB) in {time.time()-t0:.1f}s total")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
