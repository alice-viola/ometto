#!/usr/bin/env bash
# Rebuild everything the router serves for one region, from the sources up.
#
#   tools/refresh-region.sh                 # the whole pipeline
#   tools/refresh-region.sh --dry-run       # print what it would run
#   tools/refresh-region.sh --from 6        # from the SAT join on
#   tools/refresh-region.sh --only 9        # just the basemap
#   tools/refresh-region.sh --skip-basemap  # everything but Planetiler
#
# Every step is idempotent: the PBF is only re-downloaded when the server's
# copy is newer, the DEM tiles and the map assets skip what is already on disk,
# and the basemap is only rebuilt when the extract is newer than the archive
# (--force-basemap overrides). Safe to re-run after a failure.
#
# It is CAPPED on purpose. Planetiler runs in Docker with 2 CPUs and 4 GB, and
# every Python step runs under nice, because this box also runs the demo.
set -euo pipefail

REGION="${REGION:-taa}"
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
WORK="${QUEEN_WORK:-$HOME/.cache/queen-region}"
PBF_URL="${PBF_URL:-https://download.openstreetmap.fr/extracts/europe/italy/trentino_alto_adige-latest.osm.pbf}"
PBF="$WORK/osm/$(basename "$PBF_URL")"
SAT_DIR="${SAT_DIR:-$WORK/pat/tracciati}"
HUT_DIR="${HUT_DIR:-$WORK/pat/rifugi}"
SAT="$SAT_DIR/sentieri_sat_v"
HUTS="$HUT_DIR/Rifugi e Bivacchi"
LOGDIR="${LOGDIR:-$WORK/logs}"
LOG="$LOGDIR/refresh-$REGION-$(date +%Y%m%d-%H%M%S).log"
NICE="${NICE:-15}"
PLANETILER_CPUS="${PLANETILER_CPUS:-2}"
PLANETILER_MEM="${PLANETILER_MEM:-4g}"
MARGIN_KM="${MARGIN_KM:-40}"

FROM=1; ONLY=0; DRY=0; SKIP_BASEMAP=0; FORCE_BASEMAP=0
while [ $# -gt 0 ]; do
  case "$1" in
    --from) FROM="$2"; shift 2 ;;
    --only) ONLY="$2"; FROM="$2"; shift 2 ;;
    --dry-run) DRY=1; shift ;;
    --skip-basemap) SKIP_BASEMAP=1; shift ;;
    --force-basemap) FORCE_BASEMAP=1; shift ;;
    -h|--help) sed -n '2,20p' "${BASH_SOURCE[0]}" | sed 's/^# \{0,1\}//'; exit 0 ;;
    *) echo "unknown argument: $1" >&2; exit 2 ;;
  esac
done

mkdir -p "$LOGDIR" "$WORK/osm" "$WORK/dem" "$WORK/planetiler/sources" "$WORK/planetiler/tmp"
if [ "$DRY" = 0 ]; then exec > >(tee -a "$LOG") 2>&1; fi
cd "$ROOT"

say()  { printf '\n=== %s ===\n' "$*"; }
run()  { if [ "$DRY" = 1 ]; then printf '   %s\n' "$*"; else eval "$@"; fi; }
want() { [ "$1" -ge "$FROM" ] && { [ "$ONLY" = 0 ] || [ "$ONLY" = "$1" ]; }; }
py()   { run "nice -n $NICE python3 $*"; }

echo "region $REGION, root $ROOT, work $WORK, log $LOG, started $(date -u +%FT%TZ)"

# 1 --------------------------------------------------------------- the extract
if want 1; then
  say "1. OSM extract"
  # -z: only download when the server's copy is newer than ours.
  run "curl -fL --progress-bar --remote-time -o '$PBF' ${DRY:+} $( [ -f "$PBF" ] && echo "-z '$PBF'" ) '$PBF_URL'"
  run "ls -la '$PBF'"
fi
[ "$DRY" = 1 ] || [ -f "$PBF" ] || { echo "no extract at $PBF"; exit 1; }

# 2 ------------------------------------------------------------- the boundary
if want 2; then
  say "2. region and province boundaries"
  py "tools/region-boundary.py '$PBF' web/public/region.geojson web/public/region-bbox.json"
fi
BB="$( [ -f web/public/region-bbox.json ] && python3 -c "import json;b=json.load(open('web/public/region-bbox.json'));print(f\"{b['south']},{b['west']},{b['north']},{b['east']}\")" || echo S,W,N,E )"

# 3 ---------------------------------------------------------------- the layers
if want 3; then
  say "3. OSM layers (roads, lifts, places, pois, water)"
  py "tools/pbf-layers.py '$PBF' data/osm-$REGION '$BB' 'Trentino-Alto Adige' roads,lifts,places,pois,water"
fi

# 4 ------------------------------------------------------------- the map file
if want 4; then
  say "4. the routing map, clipped to the boundary + 3 km"
  run "CITY_SRC=data/osm-$REGION CITY_OUT=web/public/$REGION.json CITY_NAME='Trentino-Alto Adige' \
       CITY_ONLY_ROADS=1 CITY_CLIP=web/public/region.geojson CITY_CLIP_BUFFER_M=3000 \
       nice -n $NICE python3 tools/build-city.py"
fi

# 5 -------------------------------------------------------------- elevation
if want 5; then
  say "5. elevation (Copernicus GLO-30)"
  run "QUEEN_DEM_DIR='$WORK/dem' nice -n $NICE python3 tools/dem.py fetch web/public/region-bbox.json"
  run "QUEEN_DEM_DIR='$WORK/dem' nice -n $NICE python3 tools/dem.py add-z web/public/$REGION.json"
fi

# 6 -------------------------------------------------------------- the SAT join
if want 6; then
  say "6. SAT catalogue join"
  if [ "$DRY" = 0 ] && [ ! -f "$SAT.shp" ]; then
    echo "no SAT catalogue at $SAT.shp"
    echo "  get 'Catasto dei sentieri SAT' (sentieri_sat_v) and 'Rifugi e Bivacchi' from the"
    echo "  Province of Trento's open data portal (dati.trentino.it) and unzip them into"
    echo "  $SAT_DIR and $HUT_DIR, then re-run with --from 6"
    exit 1
  fi
  py "tools/sat-join.py '$SAT' web/public/$REGION.json web/public/sat.geojson --geometry-pass"
fi

# 7 ------------------------------------------------------------ peaks and huts
if want 7; then
  say "7. peaks, passes and huts"
  py "tools/points-layers.py data/osm-$REGION/pois.json '$HUTS' web/public/pois.geojson web/public/huts.geojson"
fi

# 8 ------------------------------------------------------------------- lifts
if want 8; then
  say "8. the lift layer"
  py "tools/lifts-layer.py data/osm-$REGION/lifts.json web/public/lifts.geojson"
fi

# 9 ------------------------------------------------------------- the basemap
if want 9 && [ "$SKIP_BASEMAP" = 0 ]; then
  say "9. basemap (Planetiler), fonts, sprites, relief, styles"
  PM="web/tiles/$REGION.pmtiles"
  if [ "$FORCE_BASEMAP" = 1 ] || [ ! -f "$PM" ] || [ "$PBF" -nt "$PM" ]; then
    BOUNDS="$(python3 - "$MARGIN_KM" <<'PY'
import json, math, sys
km = float(sys.argv[1])
b = json.load(open("web/public/region-bbox.json"))
lat0 = (b["south"] + b["north"]) / 2
dlat = km * 1000 / 111132.0
dlon = km * 1000 / (111412.84 * math.cos(math.radians(lat0)))
print(f"{b['west']-dlon:.4f},{b['south']-dlat:.4f},{b['east']+dlon:.4f},{b['north']+dlat:.4f}")
PY
)"
    echo "   bounds $BOUNDS (region bbox + $MARGIN_KM km)"
    run "docker run --rm --cpus $PLANETILER_CPUS --memory $PLANETILER_MEM \
         -e JAVA_TOOL_OPTIONS=-Xmx2g \
         -v '$WORK/planetiler:/data' -v '$(dirname "$PBF"):/osm:ro' \
         ghcr.io/onthegomap/planetiler:latest \
         --osm-path=/osm/$(basename "$PBF") --bounds=$BOUNDS \
         --minzoom=0 --maxzoom=14 --download --download-dir=/data/sources \
         --tmpdir=/data/tmp --output=/data/$REGION.pmtiles --force"
    run "mkdir -p web/tiles && mv '$WORK/planetiler/$REGION.pmtiles' '$PM'"
  else
    echo "   $PM is newer than the extract, kept (--force-basemap to rebuild)"
  fi
  py "tools/map-assets.py all"
  py "tools/local-style.py web/app/styles-source/light.json web/tiles/styles/light.json --pmtiles '$PM' --name light"
  py "tools/local-style.py web/app/styles-source/dark.json  web/tiles/styles/dark.json  --pmtiles '$PM' --name dark"
fi

# 10 ---------------------------------------------------------------- the stamp
if want 10; then
  say "10. build-info"
  py "tools/build-info.py --region $REGION --pbf '$PBF' --sat '$SAT'"
fi

say "done $(date -u +%FT%TZ)"
if [ "$DRY" = 0 ]; then
  ls -la "web/public/$REGION.json" web/public/*.geojson "web/tiles/$REGION.pmtiles" \
         web/tiles/styles/*.json data/build-info.json 2>/dev/null || true
  echo "log: $LOG"
  echo "the router does not watch these files: restart it to pick them up."
fi
