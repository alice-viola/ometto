#!/usr/bin/env python3
"""Compare the route planner at localhost:8100 against the reference routes.

stdlib only.  Reads tests/routes.json, calls POST /api/route for every route,
prints a comparison table (reference vs ours: time, distance, ascent), and
runs the extra checks: a 3-stop route, one route in three modes, alternatives
distinctness, grade T vs EEA, and 20 concurrent requests.

    python3 tests/run-routes.py                     # run everything once
    python3 tests/run-routes.py --wait 120          # wait up to 120 min for the API
    python3 tests/run-routes.py --only calisio      # one route
    python3 tests/run-routes.py --json out.json     # machine-readable results

Verdicts follow the brief: within 15% = pass, 15-30% = check, over 30% = fail.
Hiking times in the model and in the SAT references follow the Alpine club
rule (4 km/h flat, 400 m/h up, 800 m/h down); Komoot references are faster,
which routes.json records per route in reference.scale.
"""

import argparse
import json
import math
import os
import re
import statistics
import sys
import threading
import time
import unicodedata
import urllib.error
import urllib.request

HERE = os.path.dirname(os.path.abspath(__file__))
DEFAULT_ROUTES = os.path.join(HERE, "routes.json")

PASS, CHECK, FAIL = "pass", "check", "fail"


# ---------------------------------------------------------------- transport


def post_json(base, path, payload, timeout=120):
    """POST and return (status, body_or_None, elapsed_seconds, error_or_None)."""
    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        base.rstrip("/") + path,
        data=data,
        headers={"Content-Type": "application/json", "Accept": "application/json"},
        method="POST",
    )
    t0 = time.monotonic()
    try:
        with urllib.request.urlopen(req, timeout=timeout) as r:
            raw = r.read()
            dt = time.monotonic() - t0
            try:
                return r.status, json.loads(raw.decode() or "null"), dt, None
            except ValueError as e:
                return r.status, None, dt, "bad json: %s: %.200s" % (e, raw[:200])
    except urllib.error.HTTPError as e:
        dt = time.monotonic() - t0
        raw = e.read()
        body = None
        try:
            body = json.loads(raw.decode() or "null")
        except ValueError:
            pass
        return e.code, body, dt, "HTTP %s: %.200s" % (e.code, raw[:200])
    except Exception as e:  # URLError, timeout, reset
        return 0, None, time.monotonic() - t0, "%s: %s" % (type(e).__name__, e)


def get_json(base, path, timeout=10):
    try:
        with urllib.request.urlopen(base.rstrip("/") + path, timeout=timeout) as r:
            raw = r.read()
            try:
                return r.status, json.loads(raw.decode() or "null"), None
            except ValueError:
                return r.status, raw.decode(errors="replace")[:200], None
    except Exception as e:
        return 0, None, "%s: %s" % (type(e).__name__, e)


def health_ok(base, min_junctions=0):
    """Up, and — when min_junctions is set — carrying a big enough graph.

    The service answers ok while a province-sized map is loaded; the whole
    region is several hundred thousand junctions more, so a run that needs
    Alto Adige waits on the number, not just on ok.
    """
    st, body, err = get_json(base, "/api/health")
    if st != 200:
        return False, err or ("status %s" % st)
    if isinstance(body, dict):
        j = ((body.get("graph") or {}).get("junctions")) or 0
        if min_junctions and j < min_junctions:
            return False, "graph has %s junctions, waiting for %s (map=%s region=%s)" % (
                j, min_junctions, body.get("map"), body.get("region"))
        v = str(body.get("status", body.get("ok", ""))).lower()
        if v in ("ok", "true", "1", "healthy", "up", "ready"):
            return True, body
        # a 200 with an unknown shape still counts as up, but say so
        return True, body
    return True, body


def wait_for_api(base, minutes, every=120, min_junctions=0):
    deadline = time.time() + minutes * 60
    n = 0
    while time.time() < deadline:
        ok, info = health_ok(base, min_junctions)
        n += 1
        if ok:
            print("[health] up after %d probe(s): %s" % (n, info), flush=True)
            return True
        left = int((deadline - time.time()) / 60)
        print("[health] probe %d: not ready (%s); %d min left" % (n, info, left), flush=True)
        time.sleep(min(every, max(1, deadline - time.time())))
    ok, info = health_ok(base, min_junctions)
    if ok:
        print("[health] up: %s" % (info,), flush=True)
        return True
    print("[health] still down after %d min" % minutes, flush=True)
    return False


# ---------------------------------------------------------------- geometry


def haversine_m(a_lat, a_lon, b_lat, b_lon):
    R = 6371000.0
    p1, p2 = math.radians(a_lat), math.radians(b_lat)
    dp = p2 - p1
    dl = math.radians(b_lon - a_lon)
    h = math.sin(dp / 2) ** 2 + math.cos(p1) * math.cos(p2) * math.sin(dl / 2) ** 2
    return 2 * R * math.asin(math.sqrt(h))


def snapped_offsets(points, snapped):
    """Metres between each requested point and what the server snapped it to.

    The server reports `distance` itself now; we still recompute from the
    coordinates, and prefer the server's number when it has one, so the two
    can be compared if they ever disagree.
    """
    out = []
    if not isinstance(snapped, list):
        return out
    for i, p in enumerate(points):
        if i >= len(snapped):
            break
        s = snapped[i]
        if isinstance(s, dict):
            slat, slon = s.get("lat"), s.get("lon", s.get("lng"))
        elif isinstance(s, (list, tuple)) and len(s) >= 2:
            slat, slon = s[0], s[1]
        else:
            continue
        if slat is None or slon is None:
            continue
        mine = round(haversine_m(p["lat"], p["lon"], float(slat), float(slon)), 1)
        theirs = s.get("distance") if isinstance(s, dict) else None
        out.append(round(float(theirs), 1) if isinstance(theirs, (int, float)) else mine)
    return out


def snapped_names(snapped):
    """What the server snapped each point onto — the other half of the
    snapping diagnosis: 300 m off is fine at a trailhead, not on a summit."""
    if not isinstance(snapped, list):
        return []
    return [s.get("name") or "" for s in snapped if isinstance(s, dict)]


def leg_classes(route):
    """The road classes the route actually used, metres per class, summed over
    the legs: a 'hike' that spent 3 km on a primary road shows up here."""
    total = {}
    for leg in route.get("legs") or []:
        for cls, m in (leg.get("classes") or {}).items():
            total[cls] = round(total.get(cls, 0) + m)
    return dict(sorted(total.items(), key=lambda kv: -kv[1]))


# ---------------------------------------------------------------- matching


def norm(s):
    s = unicodedata.normalize("NFKD", str(s or ""))
    s = "".join(c for c in s if not unicodedata.combining(c))
    return re.sub(r"[^a-z0-9]+", " ", s.lower()).strip()


def tokens(s):
    return set(norm(s).split())


def step_names(route):
    names = []
    for key in ("steps", "legs"):
        for item in route.get(key) or []:
            if isinstance(item, dict):
                if item.get("name"):
                    names.append(str(item["name"]))
                for sub in item.get("steps") or []:
                    if isinstance(sub, dict) and sub.get("name"):
                        names.append(str(sub["name"]))
    return names


def landmark_hits(route, landmarks):
    """Which reference landmarks appear in the step names.

    A landmark matches if all of its significant words appear in one step name,
    or (for trail numbers like "SAT 401") if the bare number appears as a token.
    """
    names = step_names(route)
    per_name_tokens = [tokens(n) for n in names]
    joined = norm(" | ".join(names))
    hits, misses = [], []
    stop = {"di", "del", "della", "dei", "de", "da", "il", "la", "lo", "al", "alla",
            "sentiero", "trail", "via", "strada", "rifugio", "hutte", "malga", "sat", "cai"}
    for lm in landmarks or []:
        want = [w for w in norm(lm).split() if w]
        nums = [w for w in want if w.isdigit()]
        core = [w for w in want if w not in stop]
        ok = False
        if nums and any(n in tk for tk in per_name_tokens for n in nums):
            ok = True
        if not ok and core:
            for tk in per_name_tokens:
                if all(w in tk for w in core):
                    ok = True
                    break
        if not ok and core and len(core) >= 2 and all(w in joined.split() for w in core):
            ok = True
        (hits if ok else misses).append(lm)
    return hits, misses


# ---------------------------------------------------------------- verdicts


def pct(ours, ref):
    if ref in (None, 0) or ours is None:
        return None
    return (ours - ref) / float(ref) * 100.0


def verdict_of(deltas):
    worst = max((abs(d) for d in deltas if d is not None), default=None)
    if worst is None:
        return "n/a", None
    if worst <= 15:
        return PASS, worst
    if worst <= 30:
        return CHECK, worst
    return FAIL, worst


def best_route(body):
    routes = (body or {}).get("routes") or []
    if not routes:
        return None
    # the contract puts the best first; guard against an unsorted list
    return min(routes, key=lambda r: r.get("seconds") or float("inf"))


def walk_part(route):
    """The walking half of a car+hike or bike+hike trip.

    The engine reports walkMeters and walkAscent; the walking time is the sum
    of the legs whose mode is hike. A mixed route has to be scored on this and
    not on the whole trip, or the drive swamps the walk.
    """
    wm = route.get("walkMeters")
    wa = route.get("walkAscent")
    secs = sum(l.get("seconds") or 0 for l in (route.get("legs") or [])
               if isinstance(l, dict) and l.get("mode") == "hike")
    if wm is None:
        wm = sum(l.get("meters") or 0 for l in (route.get("legs") or [])
                 if isinstance(l, dict) and l.get("mode") == "hike") or None
    if wa is None:
        wa = sum(l.get("ascent") or 0 for l in (route.get("legs") or [])
                 if isinstance(l, dict) and l.get("mode") == "hike") or None
    return {
        "walk_km": round(wm / 1000.0, 2) if isinstance(wm, (int, float)) else None,
        "walk_minutes": round(secs / 60.0, 1) if secs else None,
        "walk_ascent": round(wa) if isinstance(wa, (int, float)) else None,
    }


def summarise(route):
    if not route:
        return {}
    sec = route.get("seconds")
    m = route.get("meters")
    return {
        "minutes": round(sec / 60.0, 1) if isinstance(sec, (int, float)) else None,
        "km": round(m / 1000.0, 2) if isinstance(m, (int, float)) else None,
        "ascent": route.get("ascent"),
        "descent": route.get("descent"),
        "grade": route.get("grade"),
        "steps": step_names(route),
        "warnings": route.get("warnings") or [],
        "n_steps": len(route.get("steps") or []),
        "n_legs": len(route.get("legs") or []),
    }


# ---------------------------------------------------------------- the runs


def call_route(base, points, mode, grade, alternatives=3, timeout=120):
    payload = {
        "points": [{"lat": p["lat"], "lon": p["lon"]} for p in points],
        "mode": mode,
        "grade": grade,
        "alternatives": alternatives,
    }
    return post_json(base, "/api/route", payload, timeout=timeout)


def run_one(base, r, timeout=120):
    grade = r.get("grade", "EE")
    st, body, dt, err = call_route(base, r["points"], r["mode"], grade, 3, timeout)
    out = {
        "id": r["id"],
        "name": r["name"],
        "mode": r["mode"],
        "status": st,
        "client_ms": round(dt * 1000),
        "error": err,
        "reference": r["reference"],
        "grade_asked": grade,
    }
    if not isinstance(body, dict):
        out["verdict"] = FAIL
        out["why"] = err or "no body"
        return out

    # A refusal now names the grade that would work. Ask again at that grade and
    # score the answer, keeping the refusal on the record: an EEA route offered
    # after an honest "no at EE" is a different product answer from a wrong one.
    if not (body.get("routes") or []) and body.get("neededGrade"):
        out["refused_at"] = grade
        out["refusal_reason"] = body.get("reason")
        out["needed_grade"] = body.get("neededGrade")
        st2, body2, dt2, err2 = call_route(
            base, r["points"], r["mode"], body["neededGrade"], 3, timeout)
        if isinstance(body2, dict) and (body2.get("routes") or []):
            body, st, dt, err = body2, st2, dt2, err2
            out["retried_at"] = out["needed_grade"]
            out["client_ms"] = round(dt2 * 1000)

    out["computedMs"] = body.get("computedMs")
    out["n_routes"] = len(body.get("routes") or [])
    out["snap_m"] = snapped_offsets(r["points"], body.get("snapped"))
    out["snap_names"] = snapped_names(body.get("snapped"))
    out["reason"] = body.get("reason")
    out["neededGrade"] = body.get("neededGrade")
    br = best_route(body)
    if br is None:
        out["verdict"] = FAIL
        out["why"] = body.get("reason") or "routes:[]"
        return out
    s = summarise(br)
    out.update(s)
    out["classes"] = leg_classes(br)
    ref = r["reference"]

    # A mixed trip is scored on its walking half against a walking reference:
    # the drive would otherwise swamp every number that matters to a walker.
    mixed = "+" in r["mode"]
    wref = ref.get("walk") or {}
    if mixed and wref:
        w = walk_part(br)
        out.update(w)
        out["scored_on"] = "walking leg"
        out["d_km"] = pct(w.get("walk_km"), wref.get("km"))
        out["d_min"] = pct(w.get("walk_minutes"), wref.get("minutes"))
        out["d_asc"] = pct(w.get("walk_ascent"), wref.get("ascent"))
        out["ref_shown"] = wref
    else:
        if mixed:
            out.update(walk_part(br))
        out["scored_on"] = "whole trip"
        out["d_km"] = pct(s.get("km"), ref.get("km"))
        out["d_min"] = pct(s.get("minutes"), ref.get("minutes"))
        out["d_asc"] = pct(s.get("ascent"), ref.get("ascent"))
        out["ref_shown"] = {"km": ref.get("km"), "minutes": ref.get("minutes"),
                            "ascent": ref.get("ascent")}
    out["verdict"], out["worst"] = verdict_of([out["d_min"], out["d_km"], out["d_asc"]])
    hits, misses = landmark_hits(br, ref.get("landmarks"))
    out["landmark_hits"] = hits
    out["landmark_misses"] = misses
    out["landmark_score"] = "%d/%d" % (len(hits), len(hits) + len(misses))
    alts = body.get("routes") or []
    out["alt_seconds"] = [a.get("seconds") for a in alts]
    out["alt_meters"] = [a.get("meters") for a in alts]
    out["alt_distinct"] = distinctness(alts)
    return out


def distinctness(routes):
    """How different the alternatives are: distinct (seconds, meters) pairs and
    the step-name overlap of each alternative against the first."""
    if len(routes) < 2:
        return {"n": len(routes), "distinct_pairs": len(routes), "overlap": []}
    sigs = {(r.get("seconds"), r.get("meters")) for r in routes}
    base = set(norm(n) for n in step_names(routes[0]))
    overlaps = []
    for r in routes[1:]:
        other = set(norm(n) for n in step_names(r))
        if not base and not other:
            overlaps.append(None)
            continue
        inter = len(base & other)
        union = len(base | other) or 1
        overlaps.append(round(inter / union * 100.0, 1))
    return {"n": len(routes), "distinct_pairs": len(sigs), "overlap": overlaps}


# ---------------------------------------------------------------- extras


def extra_multistop(base, cfg):
    e = cfg["extras"]["multistop"]
    st, body, dt, err = call_route(base, e["points"], e["mode"], e.get("grade", "EE"), 1)
    r = {"name": e["name"], "status": st, "client_ms": round(dt * 1000), "error": err}
    br = best_route(body) if isinstance(body, dict) else None
    if br is None:
        r["ok"] = False
        r["why"] = err or "routes:[] reason=%s" % ((body or {}).get("reason"),)
        return r
    s = summarise(br)
    legs = br.get("legs") or []
    r.update({"km": s["km"], "minutes": s["minutes"], "ascent": s["ascent"],
              "n_legs": len(legs), "expected_legs": len(e["points"]) - 1,
              "snap_m": snapped_offsets(e["points"], (body or {}).get("snapped"))})
    # do the legs add up to the total?
    lm = [l.get("meters") for l in legs if isinstance(l, dict)]
    if lm and all(isinstance(x, (int, float)) for x in lm) and br.get("meters"):
        r["legs_sum_km"] = round(sum(lm) / 1000.0, 2)
        r["legs_sum_matches"] = abs(sum(lm) - br["meters"]) / br["meters"] < 0.02
    r["ok"] = r["n_legs"] == r["expected_legs"]
    return r


def extra_modes(base, cfg):
    e = cfg["extras"]["modes"]
    rows = []
    for mode in e["modes"]:
        st, body, dt, err = call_route(base, e["points"], mode, e.get("grade", "EE"), 1)
        br = best_route(body) if isinstance(body, dict) else None
        row = {"mode": mode, "status": st, "client_ms": round(dt * 1000), "error": err}
        if br is None:
            row["why"] = err or "routes:[] reason=%s" % ((body or {}).get("reason"),)
        else:
            row.update(summarise(br))
            row.pop("steps", None)
        rows.append(row)
    times = {r["mode"]: r.get("minutes") for r in rows}
    ok = all(v is not None for v in times.values())
    if ok:
        order = [m for m in ("car", "bike", "hike") if m in times]
        ok = all(times[order[i]] <= times[order[i + 1]] for i in range(len(order) - 1))
    return {"name": e["name"], "rows": rows, "ordered_car_bike_hike": ok, "times": times}


def extra_alternatives(base, cfg):
    e = cfg["extras"]["alternatives"]
    st, body, dt, err = call_route(base, e["points"], e["mode"], e.get("grade", "EE"), 3)
    routes = (body or {}).get("routes") or []
    d = distinctness(routes)
    return {"name": e["name"], "status": st, "asked": 3, "got": len(routes),
            "client_ms": round(dt * 1000), "error": err,
            "seconds": [r.get("seconds") for r in routes],
            "meters": [r.get("meters") for r in routes],
            "distinct": d,
            "ok": len(routes) >= 2 and d["distinct_pairs"] == len(routes)}


def extra_grades(base, cfg):
    e = cfg["extras"]["grades"]
    rows = []
    for g in e["grades"]:
        st, body, dt, err = call_route(base, e["points"], e["mode"], g, 1)
        br = best_route(body) if isinstance(body, dict) else None
        row = {"grade": g, "status": st, "client_ms": round(dt * 1000), "error": err,
               "reason": (body or {}).get("reason") if isinstance(body, dict) else None}
        if br is None:
            row["routed"] = False
        else:
            s = summarise(br)
            row.update({"routed": True, "km": s["km"], "minutes": s["minutes"],
                        "ascent": s["ascent"], "grade_out": s["grade"],
                        "warnings": s["warnings"], "n_steps": s["n_steps"],
                        "steps": s["steps"][:12]})
        rows.append(row)
    sigs = {(r.get("routed"), r.get("km"), r.get("minutes")) for r in rows}
    return {"name": e["name"], "rows": rows, "differs": len(sigs) > 1}


def extra_concurrency(base, cfg, n=20):
    e = cfg["extras"]["concurrency"]
    jobs = []
    for i in range(n):
        spec = e["routes"][i % len(e["routes"])]
        jobs.append(spec)
    lat, codes, errs = [], [], []
    lock = threading.Lock()

    def work(spec):
        st, body, dt, err = call_route(
            base, spec["points"], spec["mode"], spec.get("grade", "EE"), 1, timeout=180
        )
        okroute = bool(best_route(body)) if isinstance(body, dict) else False
        with lock:
            lat.append(dt * 1000)
            codes.append(st)
            if err or not okroute:
                errs.append(err or "no route")

    threads = [threading.Thread(target=work, args=(j,)) for j in jobs]
    t0 = time.monotonic()
    for t in threads:
        t.start()
    for t in threads:
        t.join()
    wall = time.monotonic() - t0
    lat.sort()

    def p(q):
        if not lat:
            return None
        k = min(len(lat) - 1, int(math.ceil(q / 100.0 * len(lat))) - 1)
        return round(lat[max(0, k)], 1)

    return {"n": n, "answered": sum(1 for c in codes if c == 200),
            "codes": sorted(set(codes)), "errors": errs[:5], "n_errors": len(errs),
            "wall_s": round(wall, 2),
            "p50": p(50), "p90": p(90), "p99": p(99),
            "min": round(lat[0], 1) if lat else None,
            "max": round(lat[-1], 1) if lat else None,
            "mean": round(statistics.fmean(lat), 1) if lat else None,
            "throughput_rps": round(n / wall, 2) if wall else None}


# ---------------------------------------------------------------- output


def fmt(v, spec="%.1f"):
    return "-" if v is None else (spec % v if isinstance(v, float) else str(v))


def dpct(v):
    return "-" if v is None else ("%+.0f%%" % v)


def print_table(rows):
    hdr = ("%-22s %-5s | %7s %7s %6s | %7s %7s %6s | %6s %6s %6s | %-5s %-9s %s"
           % ("route", "mode", "ref km", "ref min", "ref+m", "our km", "our min",
              "our+m", "Δkm", "Δmin", "Δasc", "verd", "landmarks", "reason"))
    print(hdr)
    print("-" * len(hdr))
    for r in rows:
        ref = r.get("ref_shown") or r["reference"]
        ours = ((r.get("walk_km"), r.get("walk_minutes"), r.get("walk_ascent"))
                if r.get("scored_on") == "walking leg"
                else (r.get("km"), r.get("minutes"), r.get("ascent")))
        print("%-22s %-5s | %7s %7s %6s | %7s %7s %6s | %6s %6s %6s | %-5s %-9s %s"
              % (r["id"][:22], r["mode"],
                 fmt(ref.get("km")), fmt(ref.get("minutes"), "%.0f"), fmt(ref.get("ascent")),
                 fmt(ours[0]), fmt(ours[1], "%.0f"), fmt(ours[2]),
                 dpct(r.get("d_km")), dpct(r.get("d_min")), dpct(r.get("d_asc")),
                 r.get("verdict", "?"), r.get("landmark_score", "-"),
                 (("refused at %s, retried at %s; " % (r["refused_at"], r["retried_at"]))
                  if r.get("retried_at") else "")
                 + (r.get("reason") or r.get("why") or "")))


def distinct(seq):
    out = []
    for s in seq:
        if s and s not in out:
            out.append(s)
    return out


def print_detail(rows, nsteps=14):
    """Where each point landed and what the route is actually made of."""
    print("\n== per route: snapped names, then the distinct step names ==")
    for r in rows:
        print("\n%s (%s) %s" % (r["id"], r["mode"], r.get("verdict", "?")))
        snaps = r.get("snap_names") or []
        offs = r.get("snap_m") or []
        pairs = ["%s (%s m)" % (n or "?", offs[i] if i < len(offs) else "?")
                 for i, n in enumerate(snaps)]
        print("  snapped: %s" % ("; ".join(pairs) if pairs else "-"))
        if r.get("refusal_reason"):
            print("  refused: at %s: %s -> retried at %s"
                  % (r.get("refused_at"), r["refusal_reason"], r.get("retried_at") or "not retried"))
        if r.get("reason") or r.get("why"):
            print("  reason:  %s" % (r.get("reason") or r.get("why")))
        if r.get("scored_on") == "walking leg":
            print("  walk:    %s km, %s min, %s m climbed (scored on this)"
                  % (r.get("walk_km"), r.get("walk_minutes"), r.get("walk_ascent")))
        ds = distinct(r.get("steps") or [])
        if ds:
            print("  steps:   %s%s" % (" > ".join(ds[:nsteps]),
                                       " ... (%d more)" % (len(ds) - nsteps)
                                       if len(ds) > nsteps else ""))
        if r.get("landmark_hits"):
            print("  found:   %s" % ", ".join(r["landmark_hits"]))
        if r.get("landmark_misses"):
            print("  missed:  %s" % ", ".join(r["landmark_misses"]))
        if r.get("warnings"):
            print("  warn:    %s" % "; ".join(r["warnings"]))
        if r.get("classes"):
            print("  classes: %s" % r["classes"])


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--base", default=os.environ.get("ROUTE_API", "http://localhost:8100"))
    ap.add_argument("--routes", default=DEFAULT_ROUTES)
    ap.add_argument("--only", default=None, help="run one route id")
    ap.add_argument("--wait", type=int, default=0, help="minutes to wait for /api/health")
    ap.add_argument("--min-junctions", type=int, default=0,
                    help="wait until the loaded graph has at least this many junctions "
                         "(the whole region is ~400k+, one province is ~295k)")
    ap.add_argument("--json", default=None, help="write full results here")
    ap.add_argument("--no-extras", action="store_true")
    ap.add_argument("--concurrency", type=int, default=20)
    ap.add_argument("--timeout", type=int, default=120)
    args = ap.parse_args()

    cfg = json.load(open(args.routes))
    routes = cfg["routes"]
    if args.only:
        routes = [r for r in routes if r["id"] == args.only]
        if not routes:
            print("no route with id %r" % args.only)
            return 2

    if args.wait:
        if not wait_for_api(args.base, args.wait, min_junctions=args.min_junctions):
            return 3
    else:
        ok, info = health_ok(args.base, args.min_junctions)
        print("[health] %s %s" % ("up" if ok else "DOWN", info if ok else ""))
        if not ok:
            print("API is not up at %s; use --wait N to poll." % args.base)
            return 3

    results = {"base": args.base, "when": time.strftime("%Y-%m-%d %H:%M:%S"), "routes": []}
    for r in routes:
        row = run_one(args.base, r, args.timeout)
        results["routes"].append(row)
        print("[route] %-22s %-5s %s %s" % (r["id"], r["mode"], row.get("verdict"),
                                            row.get("why", "")), flush=True)

    print()
    print_table(results["routes"])
    print_detail(results["routes"])
    print()
    for r in results["routes"]:
        if r.get("snap_m"):
            far = [m for m in r["snap_m"] if m and m > 150]
            if far:
                print("[snap] %s: offsets %s m" % (r["id"], r["snap_m"]))
        if r.get("landmark_misses"):
            print("[landmarks missed] %s: %s" % (r["id"], ", ".join(r["landmark_misses"])))
        if r.get("warnings"):
            print("[warnings] %s: %s" % (r["id"], r["warnings"]))

    if not args.no_extras:
        print("\n== extras ==")
        results["multistop"] = extra_multistop(args.base, cfg)
        print("[multistop] %s" % json.dumps(results["multistop"], ensure_ascii=False)[:400])
        results["modes"] = extra_modes(args.base, cfg)
        print("[modes] %s" % json.dumps(results["modes"]["times"], ensure_ascii=False))
        results["alternatives"] = extra_alternatives(args.base, cfg)
        print("[alternatives] %s" % json.dumps(results["alternatives"], ensure_ascii=False)[:400])
        results["grades"] = extra_grades(args.base, cfg)
        print("[grades] differs=%s %s" % (
            results["grades"]["differs"],
            json.dumps([{k: v for k, v in r.items() if k != "steps"}
                        for r in results["grades"]["rows"]], ensure_ascii=False)[:400]))
        results["concurrency"] = extra_concurrency(args.base, cfg, args.concurrency)
        c = results["concurrency"]
        print("[concurrency] %d/%d answered  p50=%s p90=%s p99=%s max=%s ms  wall=%ss  %s rps"
              % (c["answered"], c["n"], c["p50"], c["p90"], c["p99"], c["max"],
                 c["wall_s"], c["throughput_rps"]))

    tally = {}
    for r in results["routes"]:
        tally[r.get("verdict", "?")] = tally.get(r.get("verdict", "?"), 0) + 1
    print("\n== %s ==" % ", ".join("%s %d" % (k, v) for k, v in sorted(tally.items())))

    if args.json:
        with open(args.json, "w") as f:
            json.dump(results, f, indent=1, ensure_ascii=False)
        print("wrote %s" % args.json)
    return 0


if __name__ == "__main__":
    sys.exit(main())
