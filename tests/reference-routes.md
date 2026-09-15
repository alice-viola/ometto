# Reference routes: ground truth for the Trentino-Alto Adige planner

Ten routes across both provinces and all modes, plus four supplementary ones, each
with a published reference for distance, time and climb, the landmarks the line
passes (so the *path* can be compared, not only the number), and start/end
coordinates good to about 50 m. The machine-readable form is
[`tests/routes.json`](routes.json); the measured comparison is
[`README.md`](../README.md); the measured runs behind those verdicts were
archived with the rest of `docs/` on 2026-09-15.

## Which clock the reference uses

This matters more than any single number, because the same walk is published with
times that differ by 40%.

| Scale | Rule | Used by |
|---|---|---|
| **SAT/CAI** | 4 km/h flat, 400 m/h up, 800 m/h down; the larger of the horizontal and vertical time counts whole, the smaller counts half | SAT's trail cadastre and hut pages, CAI signage, and **our model** (`internal/route/modes.go`) |
| Naive sum | flat time **plus** climb time, no halving | some guidebooks; runs ~25% slower than SAT/CAI |
| Komoot | its own fitness model | Komoot tours; usually faster on trails |
| Outdooractive | its own estimate | Outdooractive; varies, and its "route" pages are often **loops** quoted as if one-way |

Our model implements the SAT/CAI rule, so SAT-sourced references are the fair
test. Where only Komoot or Outdooractive numbers existed, the row says so.

Three traps found while collecting this, all of which would make a correct
planner look wrong:

1. **Loops published as one-way.** Outdooractive's Monte Calisio (7 km / 3 h) is
   a loop up SAT 401 and down 402; its Molveno-Pedrotti (22 km / 10:04) has
   ascent = descent, so it is a round trip.
2. **The trailhead convention moves the numbers.** SAT measures trail 319 from
   Ciclamino (950 m), not from Molveno village (864 m) - a difference of ~1.4 km
   and ~110 m. Vallesinella is quoted anywhere between 1499 m and 1524 m.
3. **Direction matters.** SAT tags both `duration:forward` and
   `duration:backward` (401 is 2:00 up, 1:25 down). A planner returning one
   number per route cannot match both.

## The ten

| # | id | Route | Mode | Start (lat, lon) | End (lat, lon) | km | min | ascent m | Scale |
|---|---|---|---|---|---|---|---|---|---|
| 1 | `calisio` | Martignano (Pinara) to Monte Calisio, SAT 401 | hike | 46.09604, 11.12843 | 46.098116, 11.143397 | 3.21 | 120 | 708 | SAT/CAI |
| 2 | `pedrotti` | Molveno (Ciclamino) to Rifugio Pedrotti alla Tosa, SAT 319 | hike | 46.149038, 10.949701 | 46.154155, 10.898740 | 9.47 | 270 | 1560 | SAT/CAI |
| 3 | `brentei` | Vallesinella to Rifugio Brentei, SAT 317 + 318 Bogani | hike | 46.20662, 10.85157 | 46.175243, 10.876025 | 5.2 | 145 | 675 | SAT/CAI |
| 4 | `cimaverde` | Sardagna to Cima Verde (Monte Bondone) | hike | 46.065979, 11.102692 | 45.995856, 11.044808 | 13.0 | 345 | 1675 | SAT/CAI, composite |
| 5 | `riva-car` | Trento (Piazza Duomo) to Riva del Garda, SS45bis | car | 46.06730, 11.12130 | 45.88487, 10.83968 | 42.5 | 50 | - | road planner, free flow |
| 6 | `rovereto-bike` | Trento (Ponte San Lorenzo) to Rovereto, Adige cycle path | bike | 46.07010, 11.11510 | 45.89082, 11.03391 | 25.19 | 95 | 31 | 15-18 km/h leisure |
| 7 | `merano-bike` | Bolzano (Ponte Talvera) to Merano (Terme), Etschradweg | bike | 46.50023, 11.34701 | 46.66937, 11.16109 | 30.3 | 120 | 70 | 15-18 km/h leisure |
| 8 | `firenze` | Ortisei to Rifugio Firenze / Regensburger Hütte | hike | 46.57630, 11.67420 | 46.57890, 11.71750 | 9.0 | 240 | 800 | hut's own table |
| 9 | `tenno` | Riva del Garda to Lago di Tenno, SAT 401 (Garda) | hike | 45.88487, 10.83968 | 45.93565, 10.81240 | 8.4 | 180 | 500 | walking guide |
| 10 | `cimatosa` | Trento to Cima Tosa, car to Molveno then on foot | car+hike | 46.07240, 11.11890 | 46.15652, 10.87113 | 57.0 | 505 | 2300 | SAT/CAI + road planner; **walking half re-baselined to grade A, see below** |

Supplementary, added during the work:

| id | Route | Mode | Start | End | km | min | ascent | Why |
|---|---|---|---|---|---|---|---|---|
| `schlern` | Compatsch to Rifugio Bolzano / Schlernhaus | hike | 46.54240, 11.61700 | 46.50740, 11.57460 | 8.5 | 200 | 780 | an Alto Adige hike with an unusually solid reference |
| `ora-bike` | Bolzano to Ora / Auer, Etschradweg south | bike | 46.50023, 11.34701 | 46.36100, 11.29710 | 19.1 | 76 | 25 | a bike route well inside the served map |
| `calisio-cognola` | Cognola to Monte Calisio, SAT 402 | hike | 46.076567, 11.141721 | 46.098116, 11.143397 | 4.53 | 130 | 720 | the trail that really starts on the Via Asiago side |
| `riva-car-drivable` | Trento station to Riva del Garda | car | 46.07240, 11.11890 | 45.88487, 10.83968 | 42.5 | 50 | - | measures car quality from a start the graph can leave |

### Corrections to the brief's premises

- **SAT 401 does not start at Via Asiago.** Via Asiago is in Villazzano /
  Oltrefersina (46.0533, 11.1364 and 46.0460, 11.1386), south of the Fersina.
  SAT 401 "Sentiero di Predamala" starts at Pinara in **Martignano**. The trail
  that starts on the Cognola side is **SAT 402**, added as `calisio-cognola`.
  Both are tested.
- **SAT 319B does not exist** in the 2019 cadastre. The line is 319 alone; the
  variants are 340 (from Pradel) and 332 (equipped).
- **Cima Tosa's via normale is not a walk.** It is graded EEA / AR / II+, with a
  25 m chimney pitch. A planner that refuses it is right to.
- **Lago di Cavedine is not on the SS45bis**, and **SS12/A22 belong to the other
  variant**, not to the Gardesana route.

## The routes in detail

### 1. `calisio` - Martignano (Pinara) to Monte Calisio, SAT 401

3.21 km, 2 h 00 up (1 h 25 down), +708 m, difficulty E. Start loc. Pinara, Via ai
Bolleri (~390 m); summit 1096 m with cross and altar.
Landmarks in order: **401** - **Predamala** (460 m) - junction with the Sentiero
delle Milizie (401A, closed) - **Strada della Flora** mule track - **Quattro
Strade** - **Stoi** (~1040 m) - a short cabled step or the WWI summit gallery.
Sources: [OSM relation 7009824](https://www.openstreetmap.org/relation/7009824) ·
[SAT cadastre 2019](https://www.sat.tn.it/wp-content/uploads/2020/05/CATASTO-2019-TUTTI-I-SENTIERI-.pdf) ·
[Outdooractive (the 7 km loop)](https://www.outdooractive.com/en/route/mountain-hike/trento/monte-calisio-trek/2808586/)

### 2. `pedrotti` - Molveno to Rifugio Pedrotti alla Tosa, SAT 319

9.47 km, 4 h 30 up (3 h down), +1560 m, difficulty E.
Landmarks: Via Dolomiti - **Baita Ciclamino** (950 m) - forest road up **Val
delle Seghe** - bivio 1324 (**Rifugio Croz dell'Altissimo** 46.170604, 10.931698) -
**Rifugio della Selvata** 1630 m (46.161262, 10.926293) - Acqua della Dosola -
**Baito dei Massodi** (~1994 m) - **Rifugio Tosa** (2439 m) - **Rifugio Pedrotti**
(2491 m).
Sources: [OSM relation 313435](https://www.openstreetmap.org/relation/313435) ·
[SAT hut page](https://www.sat.tn.it/rifugio-tosa-e-tommaso-pedrotti/) ·
[hut's approach table](https://rifugiotosapedrotti.it/en/pedrotti-mountain-hut/getting-there/)

### 3. `brentei` - Vallesinella to Rifugio Brentei, SAT 317 + 318

~5.2 km, ~2 h 25 up, +675 m. E to Casinei, **EE** on the Bogani (two exposed
ledges, ~80 m of fixed cable, a rock gallery, a gully that holds snow into July).
Landmarks: Rifugio Vallesinella car park (1513 m) - bridge over the Sarca -
**SAT 317** - **Rifugio Casinei** 1825 m (46.199806, 10.858130; the Tuckett
turnoff) - **SAT 318 "Sentiero Arnaldo Bogani"** - **Sella del Fridolìn** ~2043 m -
galleria Bogani - **Rifugio Brentei** 2182 m.
Sources: [OSM relation 7031562](https://www.openstreetmap.org/relation/7031562) ·
[Stefano Ardito](https://www.stefanoardito.it/2025/04/17/da-vallesinella-al-rifugio-brentei-dolomiti-di-brenta-trentino/) ·
[trentino.com](https://www.trentino.com/en/leisure-activities/mountains-and-hiking/hiking-in-summer/from-the-rifugio-vallesinella-to-the-rifugio-brentei/)

### 4. `cimaverde` - Sardagna to Cima Verde (Monte Bondone)

No single published one-way reference exists; the ~13 km / ~5 h 45 / ~1675 m is
composed from SAT segments and is marked ESTIMATE.
Legs: **SAT 645** "Trento nostra / via direttissima" (the full line
Piedicastello-Vaneze is 6.56 km / 1087 m / 3:00) via **Busa dei Orsi**, **San
Rocco**, **Candriai**, **Croce Giulio Segata** to **Vaneze** (1292 m) - **Norge**
(1417 m) - **Vason** (1647 m) - **Piana delle Viote** (1539-1564 m) - **SAT 636**
"Sentiero delle Tre Cime" (5.01 km / 605 m / 2:40 to Cornetto, EE) to **Cima
Verde** 2102 m. SAT 607 does *not* touch Sardagna.
Sources: [OSM relation 313457 (SAT 645)](https://www.openstreetmap.org/relation/313457) ·
[OSM relation 1169757 (SAT 636)](https://www.openstreetmap.org/relation/1169757) ·
[ilbarbuto.blog, the whole line walked](https://www.ilbarbuto.blog/2024/09/06/da-viote-a-cima-verde-e-ritorno-a-trento/) ·
[sentres, Sardagna-Vaneze western variant](https://www.sentres.com/it/escursioni/da-sardagna-a-vaneze)

### 5. `riva-car` - Trento to Riva del Garda by car

42.5 km, ~50 min free-flow (OSRM 44.2 km/49 min, Valhalla 42.4 km/65 min,
aggregators 41-42 km/34-43 min). Both endpoints are pedestrian squares on
purpose: that is what a user clicks.
Roads and towns in order: Via Livio Druso - **SS45bis "Strada Gardesana"** -
Galleria del Forte - **Bus de Véla** - Vela - **Cadine** (491 m) - high point 498 m
at km 11.2 - Vigolo Baselga / Terlago - **Vezzano** - **Padergnone** (Lago di
Toblino) - **Sarche** - Pietramurata - **Dro** - Ceniga - **Arco** - Riva.
Sources: [Garda Trentino, by car](https://www.gardatrentino.it/en/plan-your-trip/how-to-get-here/by-car) ·
[percorsomigliore](https://www.percorsomigliore.com/distanza/trient-it/riva-del-garda/)

### 6. `rovereto-bike` - Trento to Rovereto by bike

25.19 km measured on the cycleway, ~1 h 35 at 15-18 km/h, +31 / -34 m: flat.
Landmarks with km from Ponte San Lorenzo: Piedicastello 0.0 - Ravina 2.0 -
Romagnano 4-5 - **Mattarello** 7.6 - **Besenello** 12-14 - below **Castel Beseno**
15.9 - **Calliano** 16.5 - **Bicigrill "Asgard" di Nomi** at km 18.1
(**45.927303, 11.078836**, 114 m off the path) - **Volano** 20.3 - Borgo Sacco -
**Rovereto FS** 25.19. The corridor is EuroVelo 7 / Ciclopista del Sole, also
signed Via Claudia Augusta.
Sources: [Ciclovia della Valle dell'Adige](https://www.visittrentino.info/it/guida/tour/ciclovia-della-valle-dell-adige_tour_7024832) ·
[Bicigrill Nomi](https://www.visittrentino.info/it/guida/attivita-outdoor/bicigrill/asgard-bici-grill-nomi_md_84753776) ·
[Vallagarina cycle track](https://www.trentino.com/en/leisure-activities/mountain-biking-and-cycling/cycle-paths-in-the-trentino/vallagarina-cycle-track-trento-rovereto-avio/)

### 7. `merano-bike` - Bolzano to Merano by bike

30.3 km, ~2 h at 15-18 km/h (tourist boards allow 2 h 15-2 h 20 with stops).
**Merano is higher than Bolzano**: 262 m vs 325 m by comune, +70 / -30 m along
the path.
Landmarks with km: Ponte Talvera 0.0 - **Castel Firmiano** 3.65 (the path passes
within 6 m) - **Settequerce** 9.1 - **Terlano** 10-14 - **Vilpiano** 16 -
**Gargazzone** 18.4 - **Postal** 21.8 - Lana is 2.8 km west on a branch, *not* on
the main line - **Sinigo** 27.3 - Maia Bassa - **Terme di Merano** 30.3.
Sources: [suedtirolerland](https://www.suedtirolerland.it/en/leisure-activities/mountain-biking-and-cycling/cycle-paths-in-south-tyrol/valle-dell-adige/) ·
[weinstrasse](https://www.weinstrasse.com/en/leisure-activities/mountain-biking-and-cycling/adige-cycle-route-bolzano-merano/) ·
[meranerland](https://www.meranerland.org/en/leisure-activities/mountain-biking-and-cycling/val-dadige-bicycle-track/)

### 8. `firenze` - Ortisei to Rifugio Firenze / Regensburger Hütte

The hut's own approach table: **trail 4 via the San Giacomo hamlet, about 4 h on
foot from Ortisei**; the 2 h figure on the same page starts from the Seceda
cable-car top station, not the village. Hut at 2037-2040 m, Ortisei 1236 m, so
~800 m of climb; the 9 km is an ESTIMATE from the hut's map.
Landmarks: Ortisei - Val d'Anna - San Giacomo / St. Jakob - trail 6 / 4 -
Rifugio Firenze. Fallback destination if needed: Rifugio Resciesa 2163 m.
Sources: [rifugiofirenze.com](https://www.rifugiofirenze.com/en/) ·
[Wikipedia](https://it.wikipedia.org/wiki/Rifugio_Firenze)

### 9. `tenno` - Riva del Garda to Lago di Tenno on foot

8.4 km, ~3 h, quoted at 400 m of climb; the net rise from the lake shore (~70 m)
to Lago di Tenno (~570 m) is ~500 m, which is the figure scored.
Landmarks: Piazza 3 Novembre - **SAT 401** (the Garda 401, unrelated to the
Calisio one) - **Varone** - **Cascata del Varone** - Tenno - **Canale di Tenno** -
**Lago di Tenno**.
Sources: [souvenirdiviaggio](https://souvenirdiviaggio.it/visitare-lago-di-tenno-passeggiata-escursioni/) ·
[Garda Trentino](https://www.gardatrentino.it/en/activity/from-lake-tenno-to-canale-a-stroll-through-the-countryside_8244)

### 10. `cimatosa` - Trento to Cima Tosa, car then on foot

Car Trento-Molveno ~46 km / ~55 min, then 4 h 30 Molveno-Pedrotti and 2 h 30 to
3 h Pedrotti-summit by the via normale: ~8 h 25 in total, ~2300 m of climb.
**The summit route is alpinism, not walking**: EEA / AR / II+, the equipped
Sentiero Livio Brentari (358) around Brenta Bassa, the Vedretta della Tosa, and a
~25 m chimney pitch. An honest refusal is a correct answer for this route.
Landmarks: SS45bis or SS43/SS421 - Andalo - **Molveno** - **319** - **Rifugio
Tosa** - **358 Brentari** - Vedretta della Tosa - **Cima Tosa** 3136 m
(46.15652, 10.87113).

**Re-baselined 2026-09-15 to grade A.** The rebuilt map carries sac 6 ground, so
the planner now snaps the summit to trail 358 at 0 m rather than 451 m short, and
refuses the line at EEA with "the trail to 358 is A". That is the correct reading
of the terrain: everything above Rifugio Pedrotti is a guided summer climb on
glacier and UIAA II+ rock, rope, harness and descender mandatory. The route entry
therefore asks at grade **A**, and the walking reference time moves from 390 to
**420 min**, using the upper published figure for the summit stretch (180 min;
vienormali and the guide companies quote 2 h 30 to 3 h one way) instead of
it.wikipedia's 150 min, which is a plain ascent-rate number that does not price
alpine pacing. Distance and climb are unchanged.
Sources: [Rifugio Tosa Pedrotti, via normale](https://rifugiotosapedrotti.it/cima-tosa-via-normale/) ·
[VieNormali](https://www.vienormali.it/montagna/cima_scheda.asp?cod=1591)

### `schlern` - Compatsch to Rifugio Bolzano / Schlernhaus

8.5 km, 3 h 20 (the hut's own figure for trails 10-5-1; the SAT/CAI rule gives
194 min for the same line, so reference and model share a convention), +780 /
-175 m, 1857 m to 2457 m.
Landmarks: **Compatsch** cable-car top - trail **10** - trail **5** - Gstatsch -
**Saltner Hütte** - **Touristensteig** (trail **1**) - Schlern plateau -
**Schlernhaus** 2457 m. Monte Pez (2563-2568 m) is +110 m / 25 min further.
Sources: [schlernhaus.it](https://www.schlernhaus.it/en/plan-your-tour/ascent) ·
[Outdooractive](https://www.outdooractive.com/en/route/hiking-trail/seiser-alm/from-compatsch-to-the-schlern/67102794/) ·
[seiser-alm.it](https://www.seiser-alm.it/en/leisure-activities/mountains-and-hiking/to-mount-sciliar/)
Note: seiser-alm.it misstates the hut at 2475 m (it is 2457 m); do not take
elevations from that page.

### `ora-bike` - Bolzano to Ora / Auer by bike

19.1 km, ~76 min at 15 km/h (the tourist board's 2 h 30 for the full 37 km to
Salorno is ~14.8 km/h, which agrees), 274 m to 221 m, gross +25 / -75 m.
Landmarks: Ponte Talvera - Bolzano Sud - **Laives** ~9.4 - **Bronzolo** ~12.7 -
Vadena (on the opposite bank, ~1 km away, not ridden through) - **Ora station**
19.1. The route is South Tyrol regional cycle route **no. 1**.
Sources: [suedtirolerland, Bassa Atesina](https://www.suedtirolerland.it/en/leisure-activities/mountain-biking-and-cycling/cycle-paths-in-south-tyrol/bassa-atesina/) ·
[bicitalia](https://www.bicitalia.org/it/percorsi/119-pista-ciclabile-bassa-atesina)

### `calisio-cognola` - Cognola to Monte Calisio, SAT 402

4.53 km, 2 h 10 up (1 h 30 down), +720 m. Start Piazza dell'Argentario, Cognola
(~365 m). This is the "Sentiero Natura Cognola - Monte Calisio".
Sources: [OSM relation 129334](https://www.openstreetmap.org/relation/129334) ·
[La Traccia](https://latracciaescursioniemontagna.com/2022/03/22/681-sentiero-natura-cognola-monte-calisio/)

## How the coordinates were fixed

Village, summit and pass coordinates come from the planner's own OSM extract
(`data/osm-taa/places.json` and `pois.json`), so they agree with the graph by
construction: Monte Calisio 46.09812/11.14340 ele 1096, Cima Verde
45.99586/11.04481 ele 2102, Cima Tosa 46.15652/10.87113 ele 3136. Hut
coordinates come from the SAT/OSM hiking relations and the huts' own pages, and
were checked against the extract with [`tests/osm-probe.py`](osm-probe.py),
which reports what the graph actually has near a point.

Three coordinates in circulation are wrong and were discarded: Rifugio Bolzano
is at 46.5074/11.5746, **not** 46.512/11.549; Rifugio Brentei is at
46.175243/10.876025, and a longitude of 10.860114 seen in search summaries is
1.2 km out; Rifugio Pedrotti is at 46.154155/10.898740, about 1.1 km south of
where a first pass had put it.
