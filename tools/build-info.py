#!/usr/bin/env python3
"""
What went into this map, and when: data/build-info.json.

    python3 tools/build-info.py [--out data/build-info.json]

The router serves it on /api/config and uses it for health, so it is written
by the build rather than by hand: every number in it is read back out of the
artefacts that were just produced (the map file, the layers, the PMTiles
archive, the Province's shapefile), and nothing is passed in on faith.

The OSM extract's date is the replication timestamp Planetiler copied into the
archive's metadata, or, when there is no archive yet, the one in the PBF header.
"""
import json
import os
import pathlib
import subprocess
import sys
import time

ROOT = pathlib.Path(__file__).resolve().parent.parent
OSM_SOURCE = ("https://download.openstreetmap.fr/extracts/europe/italy/"
              "trentino_alto_adige-latest.osm.pbf")
SAT_SOURCE = ("Provincia autonoma di Trento, Catasto dei sentieri SAT "
              "(sentieri_sat_v, ETRS89 / UTM 32N)")
HUT_SOURCE = "Provincia autonoma di Trento, Rifugi e Bivacchi"


def mtime(p):
    p = pathlib.Path(p)
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime(p.stat().st_mtime)) if p.exists() else None


def size(p):
    p = pathlib.Path(p)
    return p.stat().st_size if p.exists() else None


def pmtiles_info(path):
    path = pathlib.Path(path)
    if not path.exists():
        return None
    try:
        from pmtiles.reader import Reader, MmapSource
    except ImportError:
        return {"bytes": size(path)}
    with open(path, "rb") as f:
        r = Reader(MmapSource(f))
        h = r.header()
        md = r.metadata()
        return {
            "bytes": size(path),
            "minzoom": int(h["min_zoom"]), "maxzoom": int(h["max_zoom"]),
            "bounds": [round(h["min_lon_e7"] / 1e7, 6), round(h["min_lat_e7"] / 1e7, 6),
                       round(h["max_lon_e7"] / 1e7, 6), round(h["max_lat_e7"] / 1e7, 6)],
            "tiles": int(h["addressed_tiles_count"]),
            "schema": md.get("name"), "version": md.get("version"),
            "planetiler": md.get("planetiler:version"),
            "osmReplication": md.get("planetiler:osm:osmosisreplicationtime"),
            "built": md.get("planetiler:buildtime"),
            "layers": [l["id"] for l in (md.get("vector_layers") or [])],
        }


def osm_extract_date(pbf, archive):
    if archive and archive.get("osmReplication"):
        return archive["osmReplication"]
    try:
        import osmium
        h = osmium.io.Reader(str(pbf)).header()
        t = h.get("osmosis_replication_timestamp")
        if t:
            return t
    except Exception:
        pass
    return mtime(pbf)


def sat_dates(shp):
    """The catalogue's own dates: when a row was last updated, when exported."""
    try:
        import shapefile
    except ImportError:
        return None, None
    base = pathlib.Path(str(shp) + ".dbf")
    if not base.exists():
        return None, None
    r = shapefile.Reader(str(shp), encoding="cp1252", encodingErrors="replace")
    f = [x[0] for x in r.fields[1:]]
    agg = fine = ""
    for rec in r.records():
        for name, cur in (("dataagg", "agg"), ("datafine", "fine")):
            if name in f:
                v = (rec[f.index(name)] or "").strip()
                if cur == "agg":
                    agg = max(agg, v)
                else:
                    fine = max(fine, v)
    return (agg[:10] or None), (fine[:19] or None)


def main(argv):
    out = pathlib.Path(argv[argv.index("--out") + 1] if "--out" in argv else ROOT / "data" / "build-info.json")
    region = argv[argv.index("--region") + 1] if "--region" in argv else "taa"
    pbf = argv[argv.index("--pbf") + 1] if "--pbf" in argv else os.environ.get("QUEEN_PBF", "")
    sat = argv[argv.index("--sat") + 1] if "--sat" in argv else os.environ.get("QUEEN_SAT", "")

    m = ROOT / "web" / "public" / f"{region}.json"
    doc = json.loads(m.read_text()) if m.exists() else {}
    roads = doc.get("roads", [])
    lifts = [r for r in roads if r.get("c") == "lift"]

    def feats(p):
        p = ROOT / p
        return len(json.loads(p.read_text()).get("features", [])) if p.exists() else None

    def els(p):
        p = ROOT / p
        return len(json.loads(p.read_text()).get("elements", [])) if p.exists() else None

    archive = pmtiles_info(ROOT / "web" / "tiles" / f"{region}.pmtiles")
    agg, fine = sat_dates(sat) if sat else (None, None)
    git = ""
    try:
        git = subprocess.run(["git", "-C", str(ROOT), "rev-parse", "--short", "HEAD"],
                             capture_output=True, text=True).stdout.strip()
    except Exception:
        pass

    info = {
        "region": region,
        "regionName": doc.get("meta", {}).get("name"),
        "buildDate": time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime()),
        "commit": git or None,

        "osmSource": OSM_SOURCE,
        "osmExtractDate": osm_extract_date(pbf, archive) if pbf or archive else None,
        "satCadastre": SAT_SOURCE,
        "satCadastreDate": agg,
        "satCadastreExport": fine,
        "hutRegister": HUT_SOURCE,
        "demSource": "Copernicus GLO-30",
        "basemapSource": "Planetiler, OpenMapTiles schema; fonts and sprites from OpenFreeMap",

        "bbox": doc.get("meta", {}).get("bbox"),
        "origin": doc.get("meta", {}).get("origin"),
        "extent": doc.get("meta", {}).get("extent"),

        "points": len(doc.get("points", [])),
        "stretches": len(roads),
        "junctions": len(doc.get("junctions", [])),
        "elevation": bool(doc.get("z")),
        "lifts": len(lifts),
        "liftsByType": {k: sum(1 for r in lifts if r.get("lt") == k)
                        for k in sorted({r.get("lt", "") for r in lifts})},
        "satGraded": sum(1 for r in roads if "sat" in r),
        "satTrails": feats("web/public/sat.geojson"),
        "pois": feats("web/public/pois.geojson"),
        "huts": feats("web/public/huts.geojson"),
        "places": els(f"data/osm-{region}/places.json"),
        "basemap": archive,
        "files": {p: size(ROOT / p) for p in (
            f"web/public/{region}.json", "web/public/sat.geojson", "web/public/pois.geojson",
            "web/public/huts.geojson", "web/public/lifts.geojson", "web/public/region.geojson",
            f"web/tiles/{region}.pmtiles", "web/tiles/styles/light.json", "web/tiles/styles/dark.json",
        )},
    }
    out.parent.mkdir(parents=True, exist_ok=True)
    out.write_text(json.dumps(info, indent=2) + "\n")
    print(f"wrote {out} ({out.stat().st_size} bytes)")
    for k in ("region", "buildDate", "osmExtractDate", "satCadastreDate", "stretches",
              "junctions", "lifts", "satGraded", "pois", "huts", "places"):
        print(f"  {k}: {info[k]}")
    if archive:
        print(f"  basemap: {archive['bytes']/1e6:.1f} MB, z{archive['minzoom']}-{archive['maxzoom']}, "
              f"{archive['tiles']} tiles, planetiler {archive['planetiler']}")
    return 0


if __name__ == "__main__":
    sys.exit(main(sys.argv[1:]))
