# The Trentino-Alto Adige map: how it is built, and what is in it

Everything here is derived from two public sources, with one command per step.
It is the only map Ometto has: an earlier province-only build
(`web/public/trentino.json`, `data/osm-trentino/`, a hand-cut tile pyramid) belonged
to the prototypes that left this repository on 2026-09-15, and comparisons against it
below are history rather than something you can re-run.

Sources:

| what | where | size |
|---|---|---|
| OSM extract of the region | openstreetmap.fr, `trentino_alto_adige-latest.osm.pbf` | 135 MB |
| Copernicus GLO-30 DEM | `copernicus-dem-30m.s3.amazonaws.com`, nine 1 degree tiles | 395 MB |
| SAT trail catalogue | Province of Trento, `sentieri_sat_v.shp` (ETRS89 / UTM 32N) | 3,879 trails |
| Hut register | Province of Trento, `Rifugi e Bivacchi.shp` | 191 huts |

The two Province downloads and the extract live outside the repo, in the
session scratchpad; the paths below are written out in full so the commands can
be pasted as they are. `$PBF`, `$SAT` and `$HUTS` are only there to keep the
lines short.

```sh
SCRATCH=/private/tmp/claude-502/-Users-alice-Work-queen/bcc5eab1-da8d-416d-bc4f-0e3823053063/scratchpad
PBF=$SCRATCH/osm/trentino_alto_adige-latest.osm.pbf
SAT=$SCRATCH/pat/tracciati/sentieri_sat_v
HUTS="$SCRATCH/pat/rifugi/Rifugi e Bivacchi"
```

## The commands, in order

```sh
# 1. the administrative boundary and the region's bbox            (2.5 s)
python3 tools/region-boundary.py $PBF web/public/region.geojson web/public/region-bbox.json

# 2. the OSM layers for the whole region, roads and the two point layers
#    (water only for the Lake Garda check; no buildings, no landuse)  (45 s)
python3 tools/pbf-layers.py $PBF data/osm-taa \
        45.672867,10.38184,47.092149,12.477975 "Trentino-Alto Adige" \
        roads,lifts,places,pois,water
# one layer on its own, when only that one needs rebuilding          (25 s)
# (the fifth argument is what gets written, so roads.json is not touched)
python3 tools/pbf-layers.py $PBF data/osm-taa \
        45.672867,10.38184,47.092149,12.477975 "Trentino-Alto Adige" pois

# 3. the routing map, clipped to the boundary plus 3 km     (12 s warm, 45 s cold)
CITY_SRC=data/osm-taa CITY_OUT=web/public/taa.json \
CITY_NAME="Trentino-Alto Adige" CITY_ONLY_ROADS=1 \
CITY_CLIP=web/public/region.geojson CITY_CLIP_BUFFER_M=3000 \
python3 tools/build-city.py

# 4. elevation: the nine tiles the region's bbox touches, then "z"
python3 tools/dem.py fetch web/public/region-bbox.json                # 25 s, 220 MB
python3 tools/dem.py add-z web/public/taa.json                        # 3.7 s

# 5. the SAT catalogue: grades onto the map's trails, lines to a GeoJSON (16 s)
python3 tools/sat-join.py $SAT web/public/taa.json web/public/sat.geojson --geometry-pass

# 6. peaks, passes and huts                                          (1.4 s)
python3 tools/points-layers.py data/osm-taa/pois.json "$HUTS" \
        web/public/pois.geojson web/public/huts.geojson

# 7. the passenger lifts as a drawable layer                        (0.03 s)
python3 tools/lifts-layer.py data/osm-taa/lifts.json web/public/lifts.geojson

# 8. the basemap: Planetiler, capped at 2 CPUs and 4 GB              (2 min)
docker run --rm --cpus 2 --memory 4g -e JAVA_TOOL_OPTIONS=-Xmx2g \
    -v "$SCRATCH/planetiler:/data" -v "$SCRATCH/osm:/osm:ro" \
    ghcr.io/onthegomap/planetiler:latest \
    --osm-path=/osm/trentino_alto_adige-latest.osm.pbf \
    --bounds=9.8614,45.3129,12.9984,47.4521 \
    --minzoom=0 --maxzoom=14 --download --download-dir=/data/sources \
    --tmpdir=/data/tmp --output=/data/taa.pmtiles --force
mv "$SCRATCH/planetiler/taa.pmtiles" web/tiles/taa.pmtiles

# 9. glyphs, sprites, shaded relief, LICENSES                         (20 s)
python3 tools/map-assets.py all

# 10. the two styles, pointed at our own paths                      (0.2 s)
python3 tools/local-style.py web/app/styles-source/light.json \
        web/tiles/styles/light.json --pmtiles web/tiles/taa.pmtiles --name light
python3 tools/local-style.py web/app/styles-source/dark.json \
        web/tiles/styles/dark.json  --pmtiles web/tiles/taa.pmtiles --name dark

# 11. the stamp the router serves on /api/config                    (2 s)
python3 tools/build-info.py --pbf $PBF --sat $SAT
```

All of it, in order and idempotent, is `tools/refresh-region.sh` (see the last
section).

About a minute and a half end to end on this Mac (two minutes from a cold
cache), plus the one-off tile download. Peak memory, measured with
`/usr/bin/time -l`: step 3 2.2 GB, step 5 1.4 GB, step 2 0.65 GB (pyosmium's
flex_mem location index over the whole extract; the layers themselves are
streamed to disk rather than held), everything else under 400 MB. Every step is
single threaded and takes `nice`: the demo kept running throughout.

Order matters in one place only: step 5 rewrites the `roads` member of
`taa.json` and copies every other byte, so running it after step 4 keeps the
`z` array. Running it before works too; running step 3 again does not, it
writes a fresh file without `z` and without the SAT fields.

## What came out

| file | size | what |
|---|---|---|
| `web/public/taa.json` | 153.9 MB | the routing map: 4,267,792 points, 545,134 stretches (544,625 roads and paths, 509 lifts), 262,674 junctions, `z` for every point |
| `web/public/region.geojson` | 0.19 MB | 3 features: the region and the two provinces, WGS84, simplified to 20 m |
| `web/public/region-bbox.json` | 77 B | `{"south":45.672867,"west":10.38184,"north":47.092149,"east":12.477975}` |
| `web/public/lifts.geojson` | 0.23 MB | 509 passenger lifts as lines, with name, type, length and ride time |
| `web/public/sat.geojson` | 3.9 MB | 3,879 catalogue trails, WGS84, simplified to 10 m, 151,298 points |
| `web/public/pois.geojson` | 1.18 MB | 6,190 points: 4,686 peaks, 813 passes, 691 huts |
| `web/public/huts.geojson` | 120 KB | 579 huts: the Province's 191, plus the 388 OSM maps as a building |
| `data/osm-taa/places.json` | 1.15 MB | 8,753 named places for the geocoder |
| `data/osm-taa/pois.json` | 0.81 MB | 6,167 peaks, passes and huts, OSM only |
| `data/osm-taa/lifts.json` | 0.3 MB | the 509 lifts with their tags, the input to steps 3 and 7 |
| `data/osm-taa/roads.json`, `water.json` | 245 + 22 MB | intermediates; only step 3 and the Garda check read them |

Region bbox: **45.672867 to 47.092149 N, 10.38184 to 12.477975 E**, which is
161 x 158 km in the map's local metres. The boundary polygons measure 13,627
km2 for the region, 6,182 for Trento and 7,445 for Bolzano, against the
official 13,605 / 6,207 / 7,398: the 20 m simplification accounts for the
difference. Trento, Bolzano, Merano, Bressanone, Riva del Garda and Dobbiaco
fall inside the right polygons, Verona, Brescia and Innsbruck outside all three.

Against Trentino (`web/public/trentino.json`, the earlier province build): 177,068
junctions to 262,674 (+48%), 365,670 stretches to 544,625 (+49%), 2.6M points
to 4.26M (+64%). Not quite the doubling we guessed: the old bbox already
reached 46.54 N, so it held Bolzano and everything south of it, and what the
region adds is the northern third.

## The basemap: our own tiles, not a public server

Everything the map needs at runtime is under `web/tiles/`, and the router
serves it at `/map/`. Nothing is fetched from a third party while the app runs.

| path | size | what |
|---|---|---|
| `web/tiles/taa.pmtiles` | 93.8 MB | the basemap, OpenMapTiles schema, z0-14, 9,400 tiles |
| `web/tiles/styles/light.json` | 104 KB | Liberty with the frontend's corrections, local URLs |
| `web/tiles/styles/dark.json` | 49 KB | the corrected dark style, local URLs |
| `web/tiles/fonts/<stack>/<range>.pbf` | 22 MB | 3 stacks x 48 glyph ranges |
| `web/tiles/sprites/ofm{,@2x}.{png,json}` | 224 KB | the OpenFreeMap sprite set, 1x and 2x |
| `web/tiles/natural_earth/{z}/{x}/{y}.png` | 9.6 MB | 56 shaded-relief tiles, z0-6 |
| `web/tiles/LICENSES` | 2 KB | who owns what, and under which licence |

The archive is Planetiler's default OpenMapTiles profile over the same extract
the routing map is built from, bounds = the region bbox plus 40 km
(9.8614, 45.3129 to 12.9984, 47.4521), zoom 0 to 14. It carries 16 vector
layers (aerodrome_label, aeroway, boundary, building, housenumber, landcover,
landuse, mountain_peak, park, place, poi, transportation, transportation_name,
water, water_name, waterway) and 3.35M features, and it took 1 min 56 s at two
CPUs, including the 48 s Planetiler spent downloading its own sources (Natural
Earth, water polygons, lake centrelines: 1.4 GB, cached under the work
directory and never downloaded twice).

Outside those bounds there are no tiles, on purpose: pan to Verona and the
basemap is empty. Inside them, only the region has detail, because the extract
itself is cut on the region's boundary; the 40 km margin is there so that the
edge of the mask never sits on the edge of the data.

**The styles** are not the vendor's. The frontend keeps what it actually wants
under `web/app/styles-source/{light,dark}.json` (Liberty, and the dark style
with its contrast and sprite corrections) with the vendor URLs still in them,
because that is what it edits against; `tools/local-style.py` turns those into
the served styles. It rewrites the vector source to `/map/tiles/{z}/{x}/{y}.pbf`
with the archive's own zoom range and bounds read out of the PMTiles header, the
Natural Earth raster source to `/map/natural_earth/{z}/{x}/{y}.png`, `glyphs` to
`/map/fonts/{fontstack}/{range}.pbf` and `sprite` to `/map/sprites/ofm`, and
writes the attribution into each source, because a style with `tiles` in it no
longer fetches the TileJSON the attribution used to come from. The layers, the
filters and the colours pass through untouched. The only mentions of
openfreemap.org left in either file are the two attribution links, which stay.

**Glyphs.** A style asks for glyphs 256 codepoints at a time and only for the
ranges its labels use. OpenFreeMap serves all 256 ranges of each stack, which
for the three stacks Liberty needs is 105 MB, most of it CJK that nothing here
will render. `tools/map-assets.py` fetches the 48 ranges that cover every
codepoint appearing in any string in the archive (Latin, Greek, Cyrillic,
punctuation, and the Korean and Japanese exonyms OSM carries for Trento and
Lake Garda), 22 MB; `tools/map-assets.py scan web/tiles/taa.pmtiles` recomputes
that list from an archive, and `--blocks all` takes everything.

**Sprites come in pairs.** A retina screen asks for `ofm@2x.png` and falls back
to nothing, so `tools/map-assets.py sprites` fetches both pixel ratios on every
run and checks them: a sprite JSON has to parse into icons and a PNG has to
start with the PNG magic, or it is fetched again. An interrupted run that left
a half-written file is the case this catches, because a truncated file would
otherwise be skipped as "already there" forever.

## What the backend must read

`web/public/taa.json` has the schema of `trentino.json`. Three things differ.

1. **Three new optional fields on a road stretch**, all absent unless the
   stretch was joined to the SAT catalogue:

   | field | meaning | count |
   |---|---|---|
   | `sat` | the SAT grade, one of `T`, `E`, `EE`, `EEA` | 21,879 stretches (T 542, E 17,189, EE 3,163, EEA 985) |
   | `satno` | the catalogue number, with its zone letter (`E518`, `O223`) | 21,879 |
   | `satname` | the catalogue name, when the trail has one | 9,678 |

   `sat` is the Province's own grade and is worth more than `s` (the OSM
   `sac_scale`): it is surveyed, it exists where `sac_scale` does not, and the
   two disagree in both directions. `EEA` means equipped or via ferrata; the
   sub-grade (`EEA-F` to `EEA-E`) is not on the stretch, it is in
   `sat.geojson` as `grade_raw`. Note that `sat` says nothing about whether the
   stretch is a via ferrata: `v` still does that.

2. **A new class of stretch, `lift`**: 509 of them (chair_lift 304, gondola
   153, cable_car 48, mixed_lift 4), carrying three fields nothing else has.

   | field | meaning |
   |---|---|
   | `lt` | the aerialway type, as OSM writes it |
   | `dur` | the ride in seconds, 0 when OSM does not say (313 of 509 do) |
   | `ow` | 1 when it carries passengers uphill only |

   `ow` defaults to 1 for a chair lift and a mixed lift and 0 for a cable car
   and a gondola, and an explicit `oneway` tag beats the default (36 chair
   lifts say `oneway=no` and 20 gondolas say `oneway=yes`). The name is in the
   usual `n`, the `ref` when there is no name; 502 of 509 have one.

   A lift is ONE stretch from station to station, never split, and **its ends
   are not junctions**: `junctions` is 262,674 with the lifts in, exactly what
   it was without them. Linking a station to the walking network is the
   router's job (nearest walking junction within 100 m at load). The lifts do
   share the point array, so `z` covers their pylons like everything else and
   the climb of a lift leg is real: Funifor Pejo 3000 reads 1,997 m at the
   bottom and 2,976 m at the top.

3. **The drawing layers are empty**, on purpose: `rails: []`,
   `waterLines: []`, `areas: {park:[], farm:[], wood:[], landuse:[],
   building:[], water:[]}`. The keys are there, so a decoder that expects them
   is happy; the file would be roughly 400 MB with them, and the page draws
   its ground from the tile provider now.

4. **`z` is present** (4,267,792 entries, one per point, int metres, min 64 on
   Lake Garda, max 3,873 near the Ortles). Same shape as Trentino's.

`meta` carries `name` "Trentino-Alto Adige", the region bbox, the origin
(46.382508 N, 11.4299075 E), `mPerDegLat` 111159.926, `mPerDegLon` 76857.1 and
the extent, 161,103 x 157,767 m. The tile grid is about 2.2 times Trentino's
area, so a 2 km tile is roughly 6,400 tiles against 3,480.

`data/build-info.json` is the stamp: region, build date, commit, the OSM
extract's replication timestamp (2026-09-13T02:07:49Z for this build), the SAT
catalogue's own update date, the DEM and basemap sources, the bbox, and the
counts (points, stretches, junctions, lifts by type, SAT-graded stretches,
pois, huts, places) plus the size of every file the router serves and the
archive's zoom range, bounds, tile count and Planetiler version. It is written
by `tools/build-info.py` as the last step of the build, so a rebuild refreshes
it; the router reads it for `/api/config` and health.

For the geocoder, `data/osm-taa/places.json` and `data/osm-taa/pois.json` have
exactly the shape of `data/osm-taa/*.json`: `{"elements":[{"type":...,
"id","lat","lon","tags":{...}}]}`, tags `name`/`place`/`population` for places
and `name`/`kind`/`ele` for pois. One difference: `type` is `"node"` for a
peak, a pass or a hut node, and `"way"` or `"relation"` for a hut that OSM
draws as a building, whose `lat`/`lon` are the centroid of that building and
whose tags carry `hut_type` as well. The geocoder reads `id`, `lat`, `lon` and
those tags and ignores `type`, so nothing has to change for it; the ids of the
two kinds come from different OSM id spaces, so a way hut and a node hut can
in principle share one, and the geocoder's own key is name plus position.

**South Tyrol names are bilingual and the order is not fixed**: `Bolzano -
Bozen`, `Brixen - Bressanone`, `Merano - Meran`, `Sterzing - Vipiteno`. A
geocoder that matches the whole string will miss half of them; split on " - "
and index both halves.

## What the frontend gets

`region.geojson` is a FeatureCollection of three MultiPolygons, one ring each,
WGS84 lon/lat, properties `name` (`Trentino-Alto Adige/Südtirol`, `Trento`,
`Bolzano`), `osm_name`, `kind` (`region` or `province`), `admin_level`,
`osm_id`, `rings`, `points`.

`sat.geojson` is one feature per catalogue row, `LineString` or
`MultiLineString`, with `numero`, `grade` (`T`/`E`/`EE`/`EEA`, empty on 1,079
rows), `name`, `start`, `end`, `length_m`, `osm_stretches` (how many stretches
of the map were joined to this row, 0 means the map has no counterpart), and
`grade_raw` only where it differs from `grade` (the via ferrata sub-grades).

`lifts.geojson` is one LineString per lift way, with `name` (the `ref` when
OSM gave it no name), `type` (chair_lift, gondola, cable_car, mixed_lift),
`length_m` along the ground, `duration_s` (0 on the 196 OSM does not time),
`oneway`, `osm_id`, and `occupancy` on the 474 that carry it. 636 km of cable
in all, the shortest lift 135 m and the longest 5,072 m.

`pois.geojson` has `name`, `kind` (`peak` 4,686, `pass` 813, `hut` 691), `ele`,
`source` (`osm` 6,001, `osm+pat` 166, `pat` 23), `osm_id` where there is one,
`ele_src` (`dem` on 436, `pat` on 189) when the elevation did not come from the
OSM tag, and for huts `type` (the register's kind when it has one, else the OSM
tag: `alpine_hut` 285, `wilderness_hut` 166, `basic_hut` 51) and `pat_name`.

`huts.geojson` is the huts on their own, 579 of them, each with `source`:
`pat` for the register's 191 (`type` RIFUGIO ALPINO 79, RIFUGIO ESCURSIONISTICO
68, BIVACCO 44, plus `osm_name` on the 168 that are an OSM hut too), and `osm`
for the 388 that OSM maps as a building and no register covers (`kind` "hut",
`type` alpine_hut 211, wilderness_hut 139, basic_hut 38, `osm_id`). The register
huts that ARE an OSM building appear once, as the register row.

## The SAT join, and how far it goes

Joined **by number, checked by geometry**. A stretch's OSM route ref (`hr`) is
offered the catalogue rows whose `numero` matches once the zone letter and the
leading zeros come off (`E518` and `518` are the same key, `E521A-01A` also
answers to `E521A` and `521A`), and a row is accepted only when at least 80% of
the stretch's points lie within 60 m of its line. The threshold barely matters:
18,341 of the 18,370 number matches are at 100%.

Two keys are deliberately not generated. The bare digits without the trailing
letter, because 1A and 1B are different trails that run side by side. And the
digits of a multi-letter prefix, because `CVO011` is a section of a long
distance route, not trail 11, and since such a route follows the local trails
the 60 m check would cheerfully confirm the wrong thing.

`--geometry-pass` then gives a second chance to stretches whose ref is not a
catalogue number at all (`E5`, `SdP` for the Sentiero della Pace, `SI` for the
Sentiero Italia, the Alte Vie, which OSM stamps instead of the number) at a
much tighter tolerance, 95% of points within 30 m, and only for stretches that
are already on a marked route. It added 3,509 stretches. Drop the flag to get
the number-only join.

| | |
|---|---|
| stretches in the map | 544,625 |
| carrying an OSM route ref (`hr`) | 106,943 |
| ... of which inside the province of Trento, the only place the catalogue covers | 32,198 |
| joined by number | 18,370 |
| joined by geometry alone | 3,509 |
| **joined, total** | **21,879** = 20.5% of the ones with a ref, **68.0% of the ones in the province of Trento** |
| catalogue rows with an OSM counterpart | 2,259 of 3,879 (2,259 of the 2,834 that have a number) |
| catalogue rows with none | 1,620, of which 575 have a number |

The ceiling is the catalogue's own extent: 74,745 of the map's 106,943
ref-carrying stretches are in South Tyrol, where the SAT catalogue has no rows
at all (those trails are the AVS's). Within Trentino the join covers about two
thirds; the rest is refs the catalogue does not use (named routes, park
networks) and numbers that exist in the catalogue but somewhere else.

## Known gaps

- **Only the four passenger aerialway types are kept**: cable_car, gondola,
  chair_lift, mixed_lift. The extract has 1,129 aerialway ways in all, and the
  rest are a station (157), a goods line (131), a drag lift (platter 125,
  magic_carpet 119, drag_lift 24, t-bar 8, rope_tow 5), a zip line (21) or an
  explosive line (7); 11 more are a passenger type tagged abandoned, disused
  or proposed and are dropped as well, which is how 520 becomes 509. Seven
  ways say only `aerialway=yes` and one says `cablecar`, a typo: those have no
  usable type and are not kept either.
- **196 of the 509 lifts have no ride time.** The tag to read is
  `aerialway:duration` (313 lifts), not the bare `duration` (one lift in the
  whole region); both are parsed, in minutes, MM:SS, HH:MM:SS, and with a
  written-out "min". Where it is missing the router has the length and the
  climb and nothing else.

- **No times in the catalogue.** `t_andata` is empty on all 3,879 rows in this
  release, and so are `quota_iniz`, `quota_fine`, `quota_min`, `quota_max` and
  `lun_planim`. `sat-join.py` parses `t_andata` when it is there, so a later
  release needs no code change; today no feature carries `time_min`.
  `length_m` is `lun_inclin` (the slope length), present on 3,877 rows, and the
  geometric length on the other two.
- **1,045 catalogue rows have no number and no grade.** They are the DIGIWAY
  verified segments (`competenza` "progetto DIGIWAY"). They are in
  `sat.geojson` with an empty `grade` and can never join by number.
- **575 numbered catalogue rows found no OSM counterpart.** A sample says most
  of them do have a path on the ground in OSM, under a different ref or none.
- **The SAT catalogue stops at the provincial border.** South Tyrol's trails
  have no official grade in this build. The AVS catalogue would be the
  equivalent source.
- **Huts are collected from three tags and from any shape.**
  `tourism=alpine_hut`, `tourism=wilderness_hut`, and `amenity=shelter` with
  `shelter_type=basic_hut`, as a node, a closed way or a multipolygon; a
  polygon is emitted as its area centroid. That is 668 OSM huts (131 nodes,
  536 ways, 1 relation) where reading nodes alone found 114, and it is what
  puts Rifugio Firenze, Rifugio Sasso Piatto and Schlernhaus / Rifugio Bolzano
  on the map: in South Tyrol a rifugio is almost always a building. The same
  hut mapped twice is dropped when the second one is within 100 m of the first
  and their names share a word once "rifugio", "hutte", "malga" and the rest
  are taken off; node first, then way, then relation. 19 pairs of huts survive
  within 100 m of each other and all of them are real (a hut and its winter
  room, or two rifugi side by side, Vajolet and Preuss 56 m apart).
  Still missing: a hut tagged only `tourism=chalet` or `amenity=restaurant`.
- **Marmolada is not a peak node.** OSM carries its summit as `Punta Penia`
  (3,343 m, 46.4345 N 11.8513 E); nothing in the extract is a peak named
  Marmolada. Cima Tosa (3,136 m) and `Ortler - Ortles` (3,905 m) are there
  under those names.
- **65 of the 5,789 pois lie outside the region plus 3 km**, all of them peaks
  a few km over the Austrian border that the extract carries anyway. They are
  left in; nothing routes to them.
- **Places without a name are not in `places.json`** (the layer tool requires
  `name`), and 5,696 of the 8,753 are `locality`, which is mostly field and
  forest names. A geocoder that ranks by `place` kind will want to keep city,
  town and village first: there are 2 cities, 16 towns, 711 villages.
- **The elevation is a surface model.** On a sharp summit the 30 m cell reads
  low: Cima Tosa 3,101 against 3,136, Ortles 3,856 against 3,905, Punta Penia
  3,261 against 3,343. In the valleys it is within a couple of metres (Riva del
  Garda 66 against 65, Brunico 837 against 835, Brennero 1,371 against 1,370).
  Trento reads 202 against 194 because the DSM sees the roofs. The 10 m
  hysteresis in `city.Load` is still what keeps a flat drive from climbing.
- **`data/osm-taa/` is 282 MB and is not in `.gitignore`.** Of it, `places.json`
  and `pois.json` are read at run time by the geocoder and go into the image;
  `roads.json`, `water.json` and `lifts.json` are intermediates that only step 3,
  the lifts layer and the Garda check read. Deleting those three costs a
  re-download of the extract on the next full build, nothing else.

## Lake Garda (the data sanity check)

Relation 8569, `Lago di Garda`, 81 members, is in `data/osm-taa/water.json` as
**four chains, not one**: 809, 294, 22 and 18 vertices. The big one spans
45.75415 to 45.88481 N and 10.74973 to 10.86752 E (14.5 x 9.1 km) and covers
11.9 km2 once the closing chord is counted; the chord is 16.0 km long and runs
from 45.7542 N 10.7521 E to 45.8734 N 10.8664 E, roughly along the provincial
border, which is where the extract stops. None of the four is closed in the
data (first node is not last node): a cut relation is chained and left for the
renderer to fill, which is what `pbf-layers.py` has always done. The same four
pieces, with the same vertex counts to within the four points the different
bbox costs, are in `data/osm-trentino/water.json` from the earlier build, so
the streaming rewrite of the tool changed nothing here.

The frontend draws lakes from the tile provider, so this is a check and not a
layer; `taa.json` carries no water at all.

## What changed in the tools

- `tools/region-boundary.py`, `tools/sat-join.py`, `tools/points-layers.py`,
  `tools/lifts-layer.py`, `tools/utm32.py` and `tools/geomask.py` are new.
- `tools/pbf-layers.py` takes an optional fifth argument, the layers to keep,
  and writes every layer element by element instead of holding all of them.
  It also emits huts drawn as a building or a multipolygon as their centroid,
  de-duplicated against the hut nodes, with the sub-tag in `tags.hut_type`,
  and it has a `lifts` layer: the four passenger aerialway types, with every
  node kept (a cable car is four points and Douglas-Peucker would leave two).
  With no fifth argument it still writes every layer.
- `tools/build-city.py` takes `CITY_ONLY_ROADS` and `CITY_CLIP` /
  `CITY_CLIP_BUFFER_M`, streams the roads layer instead of parsing it into
  memory whole, and appends the lifts as stretches of class `lift` after the
  junctions are counted, which is what keeps their ends out of the junction
  list. With neither variable set, its output is byte for byte what it
  was (checked against the previous version on the same input).
- `tools/dem.py` takes a bbox for `fetch` (`S,W,N,E` or a bbox file) and
  mosaics whatever tiles are in the tile directory rather than a fixed four,
  so `add-z` works on any extent. A bare `fetch` still fetches Trentino's four.
- `tools/utm32.py` run on its own prints its self-checks: round trip under
  1.2 mm anywhere in the region, and the northing on the central meridian
  against the meridian arc to the micrometre. There is no pyproj on this box.

## Refreshing: once a month

`tools/refresh-region.sh` runs the eleven steps above end to end, with these
paths and flags, and logs to `$QUEEN_WORK/logs/refresh-taa-<timestamp>.log`.

```sh
tools/refresh-region.sh                 # everything
tools/refresh-region.sh --dry-run       # print the commands, run nothing
tools/refresh-region.sh --from 6        # from the SAT join on
tools/refresh-region.sh --only 9        # just the basemap and its assets
tools/refresh-region.sh --skip-basemap  # leave the tiles alone
```

Every step is idempotent. The extract is only re-downloaded when the server's
copy is newer (`curl -z`), the DEM tiles and the fonts, sprites and relief skip
whatever is already on disk, and the basemap is only rebuilt when the extract
is newer than the archive (`--force-basemap` to insist). Planetiler runs in
Docker with `--cpus 2 --memory 4g` and every Python step runs under `nice -n
15`, because this box also runs the demo. Roughly four minutes of work plus the
download, and about 1.5 GB of cache under `$QUEEN_WORK` (the extract, the nine
DEM tiles, Planetiler's own sources).

**Monthly is the right cadence, and the reason is the OSM extract.** New and
re-tagged trails, lifts and huts land in OpenStreetMap continuously, and
openstreetmap.fr rebuilds the regional extract daily; a month of drift is a
few hundred changed ways in a region this size. The other sources move more
slowly: the Province republishes the SAT catalogue a few times a year (the
build records `satCadastreDate` so a jump is visible), the hut register moves
about once a year, and Copernicus GLO-30 is a fixed 2019-2021 product that will
not change at all, which is why `dem.py fetch` is a no-op after the first run.
The basemap is worth rebuilding on the same pass: it comes from the same
extract, so letting it drift away from the routing graph means the map shows a
building the router does not know about.

What a refresh changes, in practice: the stretch, junction and lift counts move
by a few hundred, the SAT join rate moves by a fraction of a percent, and
`data/build-info.json` gets a new `osmExtractDate` and `buildDate`. What it
does not change: the region boundary (an administrative border), the bbox, the
projection, and therefore the shape of every file the backend reads.

**The router does not watch these files.** It loads the map, the layers and the
archive at startup, so a refresh is not visible until it is restarted.
