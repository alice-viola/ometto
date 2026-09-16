package route

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"strings"
	"testing"

	"ometto/internal/city"
)

// gridRegion is a 4x4 lattice of junctions 1000 m apart, every link a named
// residential street with an elevation that rises to the east, plus one steep
// T4 path across the north edge. It is the smallest map that can answer a
// multi-stop question and a grade question.
func gridRegion(t *testing.T) *Region {
	t.Helper()
	const n = 4
	const span = 1000.0
	r := &Region{
		Name: "grid", Lat0: 46, Lon0: 11,
		MPerDegLat: 111000, MPerDegLon: 77000,
		Extent: city.Extent{MinX: -500, MaxX: n * span, MinY: -500, MaxY: n * span},
	}
	id := func(ix, iy int) int { return iy*n + ix }
	for iy := 0; iy < n; iy++ {
		for ix := 0; ix < n; ix++ {
			r.Pts = append(r.Pts, [2]float64{float64(ix) * span, float64(iy) * span})
		}
	}
	mk := func(a, b int, cls, name string, sac int, sat string) {
		pa, pb := r.Pts[a], r.Pts[b]
		mid := [2]float64{(pa[0] + pb[0]) / 2, (pa[1] + pb[1]) / 2}
		pts := [][2]float64{pa, mid, pb}
		cum := make([]float64, 3)
		for i := 1; i < 3; i++ {
			cum[i] = cum[i-1] + math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
		}
		z := func(p [2]float64) float64 { return 200 + p[0]/20 }
		st := &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[2], A: a, B: b, Cls: cls, Name: name,
			Sac: sac, MTB: -1,
			Z: []float64{z(pts[0]), z(pts[1]), z(pts[2])},
		}
		if d := st.Z[2] - st.Z[0]; d > 0 {
			st.Up = d
		} else {
			st.Down = -d
		}
		r.Stretches = append(r.Stretches, st)
		// A SAT-graded path is a numbered one: the cadastre grades trails,
		// and the walking clock prices a numbered trail as the marked way.
		satNo := ""
		if sat != "" {
			satNo = "401"
		}
		r.Sat = append(r.Sat, Sat{Grade: SatGrade(sat), No: satNo})
		r.WithElevation++
	}
	for iy := 0; iy < n; iy++ {
		for ix := 0; ix < n; ix++ {
			if ix+1 < n {
				mk(id(ix, iy), id(ix+1, iy), "residential", "Via "+string(rune('A'+iy))+string(rune('0'+ix)), 0, "")
			}
			if iy+1 < n {
				mk(id(ix, iy), id(ix, iy+1), "residential", "Corso "+string(rune('A'+ix))+string(rune('0'+iy)), 0, "")
			}
		}
	}
	// A hard path along the north edge, T4 by OSM and EEA by SAT (and so
	// numbered by SAT, which is what makes it the marked way).
	mk(id(0, 0), id(3, 0), "path", "Sentiero Duro", 4, "EEA")
	return r
}

func nodeAt(t *testing.T, g *Graph, x, y float64) (lat, lon float64) {
	t.Helper()
	return g.R.LatLon(x, y)
}

func TestMultiStopConcatenation(t *testing.T) {
	g := Build(gridRegion(t))
	lat0, lon0 := nodeAt(t, g, 0, 0)
	lat1, lon1 := nodeAt(t, g, 3000, 0)
	lat2, lon2 := nodeAt(t, g, 3000, 3000)

	one := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "car"})
	two := g.Route(Request{Points: []Point{{Lat: lat1, Lon: lon1}, {Lat: lat2, Lon: lon2}}, Mode: "car"})
	trip := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}, {Lat: lat2, Lon: lon2}}, Mode: "car"})

	for _, r := range []*Result{one, two, trip} {
		if len(r.Routes) != 1 {
			t.Fatalf("want one route, got %d (%s)", len(r.Routes), r.Reason)
		}
	}
	a, b, c := one.Routes[0], two.Routes[0], trip.Routes[0]
	if len(c.Legs) != 2 {
		t.Fatalf("a three-point trip must be two legs, got %d", len(c.Legs))
	}
	if got, want := c.Meters, a.Meters+b.Meters; math.Abs(got-want) > 1 {
		t.Errorf("meters: trip %.0f, segments %.0f+%.0f", got, a.Meters, b.Meters)
	}
	if got, want := c.Seconds, a.Seconds+b.Seconds; math.Abs(got-want) > 2 {
		t.Errorf("seconds: trip %.0f, segments %.0f+%.0f", got, a.Seconds, b.Seconds)
	}
	if got, want := c.Legs[0].Meters+c.Legs[1].Meters, c.Meters; math.Abs(got-want) > 1 {
		t.Errorf("legs %.0f do not add up to the route %.0f", got, want)
	}
	// The geometry is one line: the legs meet, and the trip's polyline is the
	// two legs minus the shared point.
	l0, l1 := c.Legs[0].Geometry.Coordinates, c.Legs[1].Geometry.Coordinates
	if l0[len(l0)-1] != l1[0] {
		t.Errorf("legs do not meet: %v vs %v", l0[len(l0)-1], l1[0])
	}
	if got, want := len(c.Geometry.Coordinates), len(l0)+len(l1)-1; got != want {
		t.Errorf("route geometry %d points, legs %d+%d", got, len(l0), len(l1))
	}
	// Three stops, three snapped points, and the third one is where we asked.
	if len(trip.Snapped) != 3 {
		t.Fatalf("want 3 snapped points, got %d", len(trip.Snapped))
	}
	// The profile is sampled every 50 m and ends at the route's length.
	if len(c.Profile) < 2 {
		t.Fatalf("no profile")
	}
	if last := c.Profile[len(c.Profile)-1][0]; math.Abs(last-c.Meters) > 50 {
		t.Errorf("profile ends at %.0f m, route is %.0f m", last, c.Meters)
	}
	for i := 1; i < len(c.Profile); i++ {
		if d := c.Profile[i][0] - c.Profile[i-1][0]; d <= 0 || d > 50.5 {
			t.Fatalf("profile step %d is %.1f m", i, d)
			break
		}
	}
	// Steps are merged by name and add up to the route.
	total := 0.0
	for _, s := range c.Steps {
		total += s.Meters
	}
	if math.Abs(total-c.Meters) > 2 {
		t.Errorf("steps add up to %.0f, route is %.0f", total, c.Meters)
	}
}

func TestGradeLimitAndWarnings(t *testing.T) {
	g := Build(gridRegion(t))
	lat0, lon0 := nodeAt(t, g, 0, 0)
	lat1, lon1 := nodeAt(t, g, 3000, 0)

	easy := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "hike", Grade: "E"})
	if len(easy.Routes) != 1 {
		t.Fatalf("no easy route: %s", easy.Reason)
	}
	for _, s := range easy.Routes[0].Steps {
		if s.Name == "Sentiero Duro" {
			t.Fatalf("grade E walked the T4/EEA path")
		}
	}
	hard := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "hike", Grade: "EEA"})
	if len(hard.Routes) != 1 {
		t.Fatalf("no EEA route: %s", hard.Reason)
	}
	used := false
	for _, s := range hard.Routes[0].Steps {
		if s.Name == "Sentiero Duro" {
			used = true
		}
	}
	if !used {
		t.Errorf("grade EEA did not take the direct hard path (%.0f m)", hard.Routes[0].Meters)
	}
	if hard.Routes[0].Grade != "EEA" {
		t.Errorf("route grade is %q, want EEA", hard.Routes[0].Grade)
	}
	// Same distance along the north edge, but the path is a marked trail
	// (×0.9) and the streets walk at the road rate (×1.2): EEA is the faster way.
	if hard.Routes[0].Seconds >= easy.Routes[0].Seconds {
		t.Errorf("the hard path should be the quick way: %.0f s vs %.0f s", hard.Routes[0].Seconds, easy.Routes[0].Seconds)
	}
	if !hasWarning(easy.Routes[0], "paths above grade E were excluded") {
		t.Errorf("no exclusion warning on the easy route: %v", easy.Routes[0].Warnings)
	}
	// The warnings speak the SAT scale, never the OSM sac numbers.
	if !hasWarning(hard.Routes[0], "route uses an EEA path") {
		t.Errorf("no EEA warning on the hard route: %v", hard.Routes[0].Warnings)
	}
	for _, w := range append(append([]string{}, hard.Routes[0].Warnings...), easy.Routes[0].Warnings...) {
		for _, banned := range []string{"T3", "T4", "sac", "(T"} {
			if strings.Contains(w, banned) {
				t.Errorf("warning %q speaks the OSM scale", w)
			}
		}
	}
}

func TestAlternativesDiffer(t *testing.T) {
	g := Build(gridRegion(t))
	lat0, lon0 := nodeAt(t, g, 0, 0)
	lat1, lon1 := nodeAt(t, g, 3000, 3000)
	res := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "car", Alternatives: 3})
	if len(res.Routes) < 2 {
		t.Fatalf("want at least two routes on a grid, got %d", len(res.Routes))
	}
	seen := map[string]bool{}
	for _, r := range res.Routes {
		key := ""
		for _, s := range r.Steps {
			key += s.Name + "|"
		}
		if seen[key] {
			t.Errorf("two identical routes: %s", key)
		}
		seen[key] = true
		if r.ID == "" {
			t.Errorf("route without an id")
		}
	}
}

func TestSampleProfile(t *testing.T) {
	pts := [][2]float64{{0, 0}, {100, 0}, {100, 100}}
	zs := []float64{0, 100, 200}
	p := sampleProfile(pts, zs, 50)
	want := [][2]float64{{0, 0}, {50, 50}, {100, 100}, {150, 150}, {200, 200}}
	if len(p) != len(want) {
		t.Fatalf("got %v", p)
	}
	for i := range want {
		if math.Abs(p[i][0]-want[i][0]) > 0.01 || math.Abs(p[i][1]-want[i][1]) > 0.01 {
			t.Fatalf("sample %d = %v, want %v", i, p[i], want[i])
		}
	}
}

func hasWarning(r *Route, want string) bool {
	for _, w := range r.Warnings {
		if w == want {
			return true
		}
	}
	return false
}

// TestSatGradeBeatsSac proves the rule the region build needs: when a road
// carries the SAT grade, it decides, and the OSM sac_scale does not. Both
// directions matter — a path OSM calls T4 that SAT marks E must be walkable at
// E, and a path OSM leaves untagged that SAT marks EEA must not.
func TestSatGradeBeatsSac(t *testing.T) {
	r := gridRegion(t)
	// The north edge is the last stretch gridRegion added: OSM T4, SAT EEA.
	last := len(r.Stretches) - 1
	cases := []struct {
		name        string
		sac         int
		sat         string
		grade       string
		wantThePath bool
	}{
		{"osm T4, sat E, asked E", 4, "E", "E", true},
		{"osm T4, no sat, asked E", 4, "", "E", false},
		{"osm T1, sat EEA, asked E", 1, "EEA", "E", false},
		{"osm T1, sat EEA, asked EEA", 1, "EEA", "EEA", true},
		{"osm T4, sat T, asked T", 4, "T", "T", true},
	}
	for _, c := range cases {
		r.Stretches[last].Sac = c.sac
		r.Sat[last] = Sat{Grade: SatGrade(c.sat), No: "401", Name: "Sentiero Duro"}
		g := Build(r)
		lat0, lon0 := g.R.LatLon(0, 0)
		lat1, lon1 := g.R.LatLon(3000, 0)
		res := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "hike", Grade: c.grade})
		if len(res.Routes) != 1 {
			t.Fatalf("%s: no route (%s)", c.name, res.Reason)
		}
		used := false
		for _, s := range res.Routes[0].Steps {
			if s.Name == "Sentiero Duro" {
				used = true
			}
		}
		if used != c.wantThePath {
			t.Errorf("%s: walked the hard path = %v, want %v", c.name, used, c.wantThePath)
		}
	}
}

// twoModeRegion is the smallest map a car+hike plan can be measured on: a
// road from A to B that only a car may drive fast, and a path from B to C that
// only feet may take, each with its own climb.
func twoModeRegion() *Region {
	r := &Region{
		Name: "twomode", Lat0: 46, Lon0: 11,
		MPerDegLat: 111000, MPerDegLon: 77000,
	}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {2000, 0}}
	mk := func(a, b int, cls string, name string, z0, z1 float64) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		st := &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls,
			Name: name, MTB: -1, Z: []float64{z0, z1},
		}
		if z1 > z0 {
			st.Up = z1 - z0
		} else {
			st.Down = z0 - z1
		}
		r.Stretches = append(r.Stretches, st)
		r.Sat = append(r.Sat, Sat{})
		r.WithElevation++
	}
	mk(0, 1, "residential", "Strada del Passo", 100, 400) // the drive: +300
	mk(1, 2, "path", "Sentiero della Cima", 400, 900)     // the walk: +500
	return r
}

// TestTwoModeLegAttribution pins what a car+hike answer must never get wrong:
// every metre, every metre of climb and every road class belongs to the leg of
// the mode that travelled it. A trip whose driving is billed to the walk reads
// as a 4,884 m ascent on foot, and no hiker would believe the plan again.
func TestTwoModeLegAttribution(t *testing.T) {
	g := Build(twoModeRegion())
	lat0, lon0 := g.R.LatLon(0, 0)
	lat1, lon1 := g.R.LatLon(2000, 0)
	res := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "car+hike", Grade: "E"})
	if len(res.Routes) != 1 {
		t.Fatalf("no route: %s", res.Reason)
	}
	r := res.Routes[0]
	if len(r.Legs) != 2 {
		t.Fatalf("want a car leg and a hike leg, got %d: %v", len(r.Legs), legModes(r))
	}
	car, hike := r.Legs[0], r.Legs[1]
	if car.Mode != "car" || hike.Mode != "hike" {
		t.Fatalf("legs are %v, want car then hike", legModes(r))
	}
	if car.Ascent != 300 || car.Descent != 0 {
		t.Errorf("the car leg climbed %v/%v, want 300/0", car.Ascent, car.Descent)
	}
	if hike.Ascent != 500 || hike.Descent != 0 {
		t.Errorf("the hike leg climbed %v/%v, want 500/0", hike.Ascent, hike.Descent)
	}
	if car.Meters != 1000 || hike.Meters != 1000 {
		t.Errorf("metres per leg %v/%v, want 1000/1000", car.Meters, hike.Meters)
	}
	if _, ok := car.Classes["residential"]; !ok || len(car.Classes) != 1 {
		t.Errorf("the car leg's classes are %v, want residential only", car.Classes)
	}
	if _, ok := hike.Classes["path"]; !ok || len(hike.Classes) != 1 {
		t.Errorf("the hike leg's classes are %v, want path only", hike.Classes)
	}
	if r.Ascent != 800 {
		t.Errorf("the trip climbed %v, want 800 (300 driven + 500 walked)", r.Ascent)
	}
	// The walking clock must price only the walk: the Alpine club rule on
	// 1000 m and +500 m is 500/0.1111 = 4500 s, and h = 900 s, so 4500+450 —
	// and the path is unmarked, which walks at ×1.2 of the rule (modes.go):
	// 5940 s. What must never be in here is the car's time.
	if want := 4950.0 * 1.2; math.Abs(hike.Seconds-want) > 60 {
		t.Errorf("the hike leg took %.0f s, want about %.0f", hike.Seconds, want)
	}
	if car.Seconds > 200 {
		t.Errorf("the car leg took %.0f s for a flat kilometre", car.Seconds)
	}
}

func legModes(r *Route) []string {
	out := make([]string, len(r.Legs))
	for i, l := range r.Legs {
		out[i] = l.Mode
	}
	return out
}

// TestSnapRefusesDistantPoint: a point far from every road is refused, with
// the distance to the nearest one, instead of being quietly moved onto it.
func TestSnapRefusesDistantPoint(t *testing.T) {
	g := Build(gridRegion(t))
	lat0, lon0 := g.R.LatLon(0, 0)
	lat1, lon1 := g.R.LatLon(6000, 1000) // 3 km east of the last junction
	res := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "car"})
	if len(res.Routes) != 0 {
		t.Fatalf("a point 3 km from the network was routed anyway")
	}
	if !strings.Contains(res.Reason, "no road within 1 km") {
		t.Errorf("reason is %q", res.Reason)
	}
	if len(res.Snapped) != 2 {
		t.Fatalf("want the snapped entries up to the refusal, got %d", len(res.Snapped))
	}
	if d := res.Snapped[1].Distance; d < 2500 || d > 3500 {
		t.Errorf("the refusal reports %.0f m to the nearest road, want about 3000", d)
	}
	// The refused pin stays where the finger was, with no name: a marker 3 km
	// away on a road nobody asked for is worse than no marker.
	if res.Snapped[1].Name != "" {
		t.Errorf("the refused point was given the name %q", res.Snapped[1].Name)
	}
	if math.Abs(res.Snapped[1].Lat-lat1) > 1e-5 || math.Abs(res.Snapped[1].Lon-lon1) > 1e-5 {
		t.Errorf("the refused pin moved: %v,%v instead of %v,%v",
			res.Snapped[1].Lat, res.Snapped[1].Lon, lat1, lon1)
	}
	// The point that DID snap keeps its junction.
	if res.Snapped[0].Name == "" {
		t.Errorf("the first point lost its snap")
	}
}

// TestRefusalIsCached: a refusal is the expensive answer, and asking twice
// must not pay twice — but the retry at a harder grade is a new question.
func TestRefusalIsCached(t *testing.T) {
	r := &Region{Name: "ferrata", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {0, 1000}}
	add := func(a, b int, cls, name string, sac int, sat string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls, Name: name, Sac: sac, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{Grade: SatGrade(sat)})
	}
	add(0, 1, "path", "Ferrata del Passo", 4, "EEA")
	add(0, 2, "residential", "Via del Paese", 0, "")
	g := Build(r)
	lat0, lon0 := g.R.LatLon(0, 0)
	lat1, lon1 := g.R.LatLon(1000, 0)
	ask := func(grade string) *Result {
		return g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "hike", Grade: grade})
	}
	first := ask("EE")
	if len(first.Routes) != 0 || first.Cached {
		t.Fatalf("the first EE ask should be a fresh refusal")
	}
	second := ask("EE")
	if !second.Cached {
		t.Errorf("the second identical refusal was searched again")
	}
	if second.Reason != first.Reason || second.NeededGrade != first.NeededGrade {
		t.Errorf("the cached refusal differs: %q/%q vs %q/%q", second.Reason, second.NeededGrade, first.Reason, first.NeededGrade)
	}
	if len(second.Snapped) != len(first.Snapped) {
		t.Errorf("the cached refusal lost its snapped points")
	}
	// The retry at the grade the answer named is a different question.
	ok := ask("EEA")
	if ok.Cached {
		t.Errorf("the EEA retry was served from the EE refusal")
	}
	if len(ok.Routes) != 1 {
		t.Errorf("the EEA retry found no route: %s", ok.Reason)
	}
	// And a route that exists is never cached as a refusal.
	if again := ask("EEA"); again.Cached {
		t.Errorf("a successful route was served from the refusal cache")
	}
}

// TestNeededGrade: when the only way through is harder than asked, the answer
// refuses AND says which grade would work, so the frontend can offer the one
// click that answers the question.
func TestNeededGrade(t *testing.T) {
	r := &Region{Name: "ferrata", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {0, 1000}}
	add := func(a, b int, cls, name string, sac int, sat string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls, Name: name,
			Sac: sac, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{Grade: SatGrade(sat)})
	}
	// The only way to the hut is an EEA path; the other way out is a street
	// that leads nowhere near it.
	add(0, 1, "path", "Ferrata del Passo", 4, "EEA")
	add(0, 2, "residential", "Via del Paese", 0, "")
	g := Build(r)
	lat0, lon0 := g.R.LatLon(0, 0)
	lat1, lon1 := g.R.LatLon(1000, 0)
	ask := func(grade string) *Result {
		return g.Route(Request{
			Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1, Name: "Rifugio Alto"}},
			Mode:   "hike", Grade: grade,
		})
	}
	res := ask("EE")
	if len(res.Routes) != 0 {
		t.Fatalf("grade EE walked an EEA path")
	}
	if res.NeededGrade != "EEA" {
		t.Errorf("neededGrade is %q, want EEA", res.NeededGrade)
	}
	if want := "no route at EE: the trail to Rifugio Alto is EEA"; res.Reason != want {
		t.Errorf("reason is %q, want %q", res.Reason, want)
	}
	if ok := ask("EEA"); len(ok.Routes) != 1 {
		t.Errorf("grade EEA did not find the path either: %s", ok.Reason)
	} else if ok.NeededGrade != "" {
		t.Errorf("a route that worked still reported neededGrade %q", ok.NeededGrade)
	}
}

// parkRegion: a road from A through B to C, and a path from B to C beside it.
// A car+hike trip A -> C would drive the whole way, because driving is faster;
// a trip A -> B -> C means "park at B and walk", and must.
func parkRegion() *Region {
	r := &Region{Name: "park", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {2000, 0}}
	add := func(a, b int, cls, name string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls, Name: name, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
	}
	add(0, 1, "residential", "Via del Paese")
	add(1, 2, "residential", "Strada del Rifugio")
	add(1, 2, "path", "Sentiero del Rifugio")
	return r
}

// TestStopParksTheCar: with three points and a two-mode plan the first segment
// is driven and the rest walked, whatever the clock would prefer; with two
// points the trailhead rule still decides.
func TestStopParksTheCar(t *testing.T) {
	g := Build(parkRegion())
	at := func(x float64) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon}
	}
	stop := at(1000)
	stop.Name = "Molveno"

	three := g.Route(Request{Points: []Point{at(0), stop, at(2000)}, Mode: "car+hike", Grade: "E"})
	if len(three.Routes) != 1 {
		t.Fatalf("no route: %s", three.Reason)
	}
	r := three.Routes[0]
	if got := legModes(r); len(got) != 2 || got[0] != "car" || got[1] != "hike" {
		t.Fatalf("legs are %v, want car then hike", got)
	}
	if r.Legs[1].Meters < 900 || r.Legs[1].Meters > 1100 {
		t.Errorf("the walk is %.0f m, want the whole second segment", r.Legs[1].Meters)
	}
	if !hasWarning(r, "parked at Molveno") {
		t.Errorf("no parking warning: %v", r.Warnings)
	}
	// The two minutes of parking are in the trip's clock, in no leg's.
	if sum := r.Legs[0].Seconds + r.Legs[1].Seconds; r.Seconds-sum < switchCost-1 {
		t.Errorf("the trip (%.0f s) does not carry the parking over its legs (%.0f s)", r.Seconds, sum)
	}

	// Two points: no stop, so the clock decides and it drives all the way.
	two := g.Route(Request{Points: []Point{at(0), at(2000)}, Mode: "car+hike", Grade: "E"})
	if len(two.Routes) != 1 {
		t.Fatalf("no two-point route: %s", two.Reason)
	}
	if got := legModes(two.Routes[0]); len(got) != 1 || got[0] != "car" {
		t.Errorf("two points gave %v, want one car leg (the trailhead rule, not the stop rule)", got)
	}
	if hasWarning(two.Routes[0], "parked at Molveno") {
		t.Errorf("a two-point trip parked at a stop it does not have")
	}
	// And a single-mode plan is untouched by any of this.
	hike := g.Route(Request{Points: []Point{at(0), stop, at(2000)}, Mode: "hike", Grade: "E"})
	if len(hike.Routes) != 1 || len(hike.Routes[0].Legs) != 2 {
		t.Fatalf("a three-point hike must still be two hike legs")
	}
	for _, l := range hike.Routes[0].Legs {
		if l.Mode != "hike" {
			t.Errorf("legs are %v, want hike twice", legModes(hike.Routes[0]))
		}
	}
}

// TestMergeSteps: the directions are lines a person reads, not a list of every
// junction. Crumbs fold into the piece they belong to, the longer name wins,
// modes never fuse, and no route shows more than forty lines.
func TestMergeSteps(t *testing.T) {
	in := []Step{
		{Name: "Via Roma", Mode: "hike", Meters: 90, Seconds: 90},
		{Name: "path", Mode: "hike", Meters: 10, Seconds: 12},
		{Name: "Via Dante", Mode: "hike", Meters: 30, Seconds: 30},
		{Name: "trail 401", Mode: "hike", Meters: 4200, Seconds: 4000},
		{Name: "track", Mode: "hike", Meters: 120, Seconds: 110},
		{Name: "trail 402", Mode: "hike", Meters: 2000, Seconds: 1800},
	}
	out := mergeSteps(in, 150, 40)
	if len(out) != 2 {
		t.Fatalf("got %d steps, want 2: %v", len(out), stepNames(out))
	}
	if out[0].Name != "trail 401" || out[1].Name != "trail 402" {
		t.Errorf("the long pieces should keep their names: %v", stepNames(out))
	}
	if got, want := totalM(out), totalM(in); math.Abs(got-want) > 0.5 {
		t.Errorf("merging lost metres: %.0f, want %.0f", got, want)
	}
	if got, want := totalS(out), totalS(in); math.Abs(got-want) > 0.5 {
		t.Errorf("merging lost seconds: %.0f, want %.0f", got, want)
	}

	// A crumb between two modes stays with its own: a car step never absorbs
	// a walking one.
	mixed := mergeSteps([]Step{
		{Name: "Tangenziale", Mode: "car", Meters: 8000, Seconds: 400},
		{Name: "car park", Mode: "hike", Meters: 40, Seconds: 60},
		{Name: "trail 319", Mode: "hike", Meters: 3000, Seconds: 5400},
	}, 150, 40)
	if len(mixed) != 2 || mixed[0].Mode != "car" || mixed[1].Mode != "hike" {
		t.Fatalf("modes fused: %v", mixed)
	}
	if mixed[1].Meters != 3040 {
		t.Errorf("the walking crumb went to the wrong leg: %v", mixed)
	}

	// Forty at most, whatever their length.
	var many []Step
	for i := 0; i < 120; i++ {
		many = append(many, Step{Name: "piece", Mode: "car", Meters: float64(500 + i), Seconds: 60})
	}
	capped := mergeSteps(many, 150, 40)
	if len(capped) > 40 {
		t.Errorf("%d steps, want at most 40", len(capped))
	}
	if got, want := totalM(capped), totalM(many); math.Abs(got-want) > 0.5 {
		t.Errorf("capping lost metres: %.0f, want %.0f", got, want)
	}
}

func stepNames(ss []Step) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = s.Name
	}
	return out
}

func totalM(ss []Step) float64 {
	t := 0.0
	for _, s := range ss {
		t += s.Meters
	}
	return t
}

func totalS(ss []Step) float64 {
	t := 0.0
	for _, s := range ss {
		t += s.Seconds
	}
	return t
}

// brentaRegion is Alice's case in miniature: a road that ends at a trailhead,
// and three huts along a path beyond it, each more than a kilometre from the
// last tarmac.
func brentaRegion() *Region {
	r := &Region{Name: "brenta", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {2500, 0}, {4000, 0}, {5500, 0}}
	add := func(a, b int, cls, name string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls, Name: name, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
	}
	add(0, 1, "residential", "Strada di Fondovalle") // the drive
	add(1, 2, "path", "Sentiero 318")                // the trailhead is junction 1
	add(2, 3, "path", "Sentiero 323")
	add(3, 4, "path", "Sentiero 305")
	return r
}

// TestWalkOnlyStopsChooseTheParking: her case. Every stop is walk-only, so
// there is no stop to park at and the trailhead rule picks one inside the
// first leg; the answer says so with parking.point = null.
func TestWalkOnlyStopsChooseTheParking(t *testing.T) {
	g := Build(brentaRegion())
	at := func(x float64, name string) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon, Name: name}
	}
	res := g.Route(Request{
		Points: []Point{at(0, "Trento"), at(2500, "Brentei"), at(4000, "Alimonta"), at(5500, "Pedrotti")},
		Mode:   "car+hike", Grade: "E",
	})
	if len(res.Routes) != 1 {
		t.Fatalf("refused: %s (%v)", res.Reason, res.Snapped)
	}
	r := res.Routes[0]
	modes := legModes(r)
	if len(modes) != 4 || modes[0] != "car" {
		t.Fatalf("legs are %v, want a drive then three walks", modes)
	}
	for _, m := range modes[1:] {
		if m != "hike" {
			t.Fatalf("legs are %v: everything after the switch is walked", modes)
		}
	}
	if r.Parking == nil {
		t.Fatalf("no parking in the answer")
	}
	if r.Parking.Point != nil {
		t.Errorf("parking.point is %d, want null: no stop is the parking here", *r.Parking.Point)
	}
	if r.Parking.Name == "" || r.Parking.Lat == 0 {
		t.Errorf("the parking has no place: %+v", r.Parking)
	}
	if !hasWarning(r, "parked at "+r.Parking.Name) {
		t.Errorf("warnings %v do not name the parking %q", r.Warnings, r.Parking.Name)
	}
	// The card must read "parked at <the road>", never "parked at <the
	// trail>": the car is not on the trail. With nothing named nearby, the
	// name is the way the FIRST mode can drive.
	if r.Parking.Name != "Strada di Fondovalle" {
		t.Errorf("the parking is called %q, want the road the car is on", r.Parking.Name)
	}

	// A place within 100 m of the trailhead lends it its name.
	lat, lon := g.R.LatLon(1000, 60)
	g.SetParkPlaces([]ParkPlace{{Name: "Vallesinella", Lat: lat, Lon: lon}})
	named := g.Route(Request{
		Points: []Point{at(0, "Trento"), at(2500, "Brentei"), at(4000, "Alimonta"), at(5500, "Pedrotti")},
		Mode:   "car+hike", Grade: "E",
	})
	if len(named.Routes) != 1 {
		t.Fatalf("refused with a place attached: %s", named.Reason)
	}
	np := named.Routes[0].Parking
	if np == nil || np.Name != "Vallesinella" {
		t.Errorf("the parking is called %+v, want Vallesinella", np)
	}
	if !hasWarning(named.Routes[0], "parked at Vallesinella") {
		t.Errorf("the warning does not follow the name: %v", named.Routes[0].Warnings)
	}
	// A place 400 m away is too far to name it.
	lat, lon = g.R.LatLon(1000, 400)
	g.SetParkPlaces([]ParkPlace{{Name: "Troppo Lontano", Lat: lat, Lon: lon}})
	far := g.Route(Request{
		Points: []Point{at(0, "Trento"), at(2500, "Brentei"), at(4000, "Alimonta"), at(5500, "Pedrotti")},
		Mode:   "car+hike", Grade: "E",
	})
	if p := far.Routes[0].Parking; p == nil || p.Name != "Strada di Fondovalle" {
		t.Errorf("a place 400 m away named the parking: %+v", p)
	}
	g.SetParkPlaces(nil)
	// The walk goes through both huts: 1.5 km from the trailhead to each.
	if w := r.WalkMeters; math.Abs(w-4500) > 50 {
		t.Errorf("walked %.0f m, want 4500 through both huts", w)
	}
	if len(res.Snapped) != 4 {
		t.Fatalf("four points, four snaps: %v", res.Snapped)
	}
	for i, s := range res.Snapped[1:] {
		if s.Distance > 5 {
			t.Errorf("stop %d snapped %.0f m away; the huts are on the path", i+1, s.Distance)
		}
	}
}

// TestDriveToTheStopThenWalk: the Molveno shape — the stop is on a road, the
// destination is not, so the stop IS the parking and the page can badge it.
func TestDriveToTheStopThenWalk(t *testing.T) {
	g := Build(brentaRegion())
	at := func(x float64, name string) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon, Name: name}
	}
	res := g.Route(Request{
		Points: []Point{at(0, "Trento"), at(1000, "Molveno"), at(2500, "Pedrotti")},
		Mode:   "car+hike", Grade: "E",
	})
	if len(res.Routes) != 1 {
		t.Fatalf("refused: %s", res.Reason)
	}
	r := res.Routes[0]
	if modes := legModes(r); len(modes) != 2 || modes[0] != "car" || modes[1] != "hike" {
		t.Fatalf("legs are %v, want car then hike", modes)
	}
	if r.Parking == nil || r.Parking.Point == nil {
		t.Fatalf("parking is %+v, want the stop", r.Parking)
	}
	if *r.Parking.Point != 1 {
		t.Errorf("parking.point is %d, want 1", *r.Parking.Point)
	}
	if r.Parking.Name != "Molveno" {
		t.Errorf("parking name is %q, want the stop's name", r.Parking.Name)
	}
	if !hasWarning(r, "parked at Molveno") {
		t.Errorf("warnings %v", r.Warnings)
	}
}

// TestNothingWithinAKilometre: neither road nor trail, so it is refused, and
// the single-mode plans are not touched by any of this.
func TestNothingWithinAKilometre(t *testing.T) {
	g := Build(brentaRegion())
	at := func(x, y float64) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon}
	}
	for _, mode := range []string{"car+hike", "hike", "car"} {
		res := g.Route(Request{Points: []Point{at(0, 0), at(2500, 3000)}, Mode: mode, Grade: "E"})
		if len(res.Routes) != 0 {
			t.Errorf("%s: routed to a point 3 km from anything", mode)
		}
		if !strings.Contains(res.Reason, "no road within 1 km") {
			t.Errorf("%s: reason is %q", mode, res.Reason)
		}
	}
	// A single-mode plan has no parking and no parking warning.
	for _, mode := range []string{"hike", "car"} {
		to := 5500.0
		if mode == "car" {
			to = 1000
		}
		res := g.Route(Request{Points: []Point{at(0, 0), at(to/2, 0), at(to, 0)}, Mode: mode, Grade: "E"})
		if len(res.Routes) != 1 {
			t.Fatalf("%s: refused: %s", mode, res.Reason)
		}
		if p := res.Routes[0].Parking; p != nil {
			t.Errorf("%s: a single-mode plan reported parking %+v", mode, p)
		}
		for _, w := range res.Routes[0].Warnings {
			if strings.HasPrefix(w, "parked at") {
				t.Errorf("%s: a single-mode plan warned %q", mode, w)
			}
		}
	}
}

// alpineRegion: a valley path that stops at a glacier, and two ways on from
// there — one sac 5, one sac 6 — which is how the Cevedale summits are mapped.
func alpineRegion() *Region {
	r := &Region{Name: "alpine", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {2000, 0}, {2000, 1000}}
	add := func(a, b int, cls, name string, sac int) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls, Name: name,
			Sac: sac, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
	}
	add(0, 1, "path", "Sentiero del Rifugio", 2) // E: the walk in
	add(1, 2, "path", "Vedretta", 5)             // T5: the glacier
	add(2, 3, "path", "Cresta", 6)               // T6: the ridge
	return r
}

// TestAlpineGrade: above EEA there is a grade, it has to be asked for, and it
// says so in words a person can act on.
func TestAlpineGrade(t *testing.T) {
	g := Build(alpineRegion())
	at := func(i int) Point {
		lat, lon := g.R.LatLon(g.R.Pts[i][0], g.R.Pts[i][1])
		return Point{Lat: lat, Lon: lon}
	}
	ask := func(to int, grade string) *Result {
		return g.Route(Request{Points: []Point{at(0), at(to)}, Mode: "hike", Grade: grade})
	}
	// The glacier is refused at EEA, and the answer names the grade that works.
	ee := ask(2, "EEA")
	if len(ee.Routes) != 0 {
		t.Fatalf("EEA walked a T5 glacier")
	}
	if ee.NeededGrade != "A" {
		t.Errorf("neededGrade is %q, want A", ee.NeededGrade)
	}
	if !strings.Contains(ee.Reason, "is A") {
		t.Errorf("reason is %q", ee.Reason)
	}
	// At A it is routed, with the warning that says what it really is.
	a := ask(2, "A")
	if len(a.Routes) != 1 {
		t.Fatalf("A did not route: %s", a.Reason)
	}
	r := a.Routes[0]
	if r.Grade != "A" {
		t.Errorf("route grade is %q, want A", r.Grade)
	}
	if !hasWarning(r, "alpine terrain beyond EEA: glacier, rope and crampons, not a marked path") {
		t.Errorf("no alpine warning: %v", r.Warnings)
	}
	for _, w := range r.Warnings {
		if strings.Contains(w, "an A path") || strings.Contains(w, "a A path") {
			t.Errorf("the warning calls it a path: %q", w)
		}
	}
	// sac 6 is in the graph now: the ridge is walkable at A and nowhere else.
	if six := ask(3, "A"); len(six.Routes) != 1 {
		t.Errorf("sac 6 is not in the graph: %s", six.Reason)
	}
	if six := ask(3, "EEA"); len(six.Routes) != 0 {
		t.Errorf("EEA walked a T6 ridge")
	}
	// The pace above EEA is slower than the Alpine club rule: the glacier
	// kilometre costs more than the path kilometre of the same shape.
	easy := ask(1, "E").Routes[0]
	hard := ask(2, "A").Routes[0]
	if glacier := hard.Seconds - easy.Seconds; glacier < easy.Seconds*1.4 {
		t.Errorf("the glacier took %.0f s against %.0f s of path: no alpine penalty", glacier, easy.Seconds)
	}
}

// liftRegion: a valley, a summit three kilometres and a thousand metres above
// it, a gondola between them, and a second lift standing in a field with
// nothing to link to.
func liftRegion(uphillOnly bool) *Region {
	r := &Region{Name: "lifts", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{
		{0, 0},        // 0 valley junction
		{0, -3000},    // 1 summit junction (north: y grows south)
		{50, 0},       // 2 lift bottom
		{50, -3000},   // 3 lift top
		{9000, 0},     // 4 orphan lift bottom
		{9000, -3000}, // 5 orphan lift top
	}
	z := []float64{200, 1200, 200, 1200, 500, 1500}
	r.PtZ = z
	add := func(a, b int, cls, name string, lf Lift) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		st := &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls, Name: name,
			MTB: -1, Z: []float64{z[a], z[b]},
		}
		if d := z[b] - z[a]; d > 0 {
			st.Up = d
		} else {
			st.Down = -d
		}
		r.Stretches = append(r.Stretches, st)
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, lf)
		if lf.Is {
			r.Lifts++
		}
	}
	add(0, 1, "path", "Sentiero del Monte", Lift{})
	add(2, 3, "lift", "Funivia del Monte", Lift{Is: true, Type: "gondola", Dur: 300, Uphill: uphillOnly})
	add(4, 5, "lift", "Funivia Fantasma", Lift{Is: true, Type: "chair_lift", Uphill: false})
	return r
}

// TestLifts: a lift is a choice, never a surprise.
func TestLifts(t *testing.T) {
	g := Build(liftRegion(false))
	if g.LiftsLoaded != 2 {
		t.Fatalf("loaded %d lifts, want 2", g.LiftsLoaded)
	}
	if g.LiftsLinked != 1 {
		t.Errorf("linked %d lifts, want 1: the orphan has nothing within 100 m", g.LiftsLinked)
	}
	at := func(i int) Point {
		lat, lon := g.R.LatLon(g.R.Pts[i][0], g.R.Pts[i][1])
		return Point{Lat: lat, Lon: lon}
	}
	walk := g.Route(Request{Points: []Point{at(0), at(1)}, Mode: "hike", Grade: "E"})
	if len(walk.Routes) != 1 {
		t.Fatalf("no walk: %s", walk.Reason)
	}
	w := walk.Routes[0]
	for _, l := range w.Legs {
		if l.Mode == "lift" {
			t.Fatalf("a lift was used without being asked for")
		}
	}
	if hasWarning(w, "lifts run in season only; check the operator's dates") {
		t.Errorf("a walk warned about lifts: %v", w.Warnings)
	}

	ride := g.Route(Request{Points: []Point{at(0), at(1)}, Mode: "hike", Grade: "E", Lifts: true})
	if len(ride.Routes) != 1 {
		t.Fatalf("no route with lifts: %s", ride.Reason)
	}
	rr := ride.Routes[0]
	var lifts []*Leg
	for _, l := range rr.Legs {
		if l.Mode == "lift" {
			lifts = append(lifts, l)
		}
	}
	if len(lifts) != 1 {
		t.Fatalf("legs are %v, want one lift leg", legModes(rr))
	}
	if lifts[0].Name != "Funivia del Monte" || lifts[0].LiftType != "gondola" {
		t.Errorf("the lift leg is %+v, want the gondola's name and type", lifts[0])
	}
	if rr.Seconds >= w.Seconds {
		t.Errorf("the lift (%.0f s) did not beat the walk (%.0f s)", rr.Seconds, w.Seconds)
	}
	if !hasWarning(rr, "lifts run in season only; check the operator's dates") {
		t.Errorf("no seasonal warning: %v", rr.Warnings)
	}
	// The ride's climb is in the trip's ascent and in nobody's legs.
	if rr.Ascent < 900 {
		t.Errorf("the trip climbed %.0f m, the lift alone rises 1000", rr.Ascent)
	}
	if rr.WalkAscent > 50 {
		t.Errorf("walkAscent is %.0f m: the cable car's climb is nobody's effort", rr.WalkAscent)
	}
	if rr.WalkMeters > 300 {
		t.Errorf("walkMeters is %.0f m: only the two links are walked", rr.WalkMeters)
	}
	// The lift is one step, named after itself, and never folded away.
	found := false
	for _, s := range rr.Steps {
		if s.Name == "Funivia del Monte" && s.Mode == "lift" {
			found = true
		}
	}
	if !found {
		t.Errorf("the lift is not a step of its own: %v", rr.Steps)
	}

	// Uphill only: the same ride downhill is not offered.
	gu := Build(liftRegion(true))
	up := gu.Route(Request{Points: []Point{at(0), at(1)}, Mode: "hike", Grade: "E", Lifts: true})
	down := gu.Route(Request{Points: []Point{at(1), at(0)}, Mode: "hike", Grade: "E", Lifts: true})
	if len(up.Routes) != 1 || len(down.Routes) != 1 {
		t.Fatalf("uphill/downhill did not route")
	}
	upLift, downLift := false, false
	for _, l := range up.Routes[0].Legs {
		if l.Mode == "lift" {
			upLift = true
		}
	}
	for _, l := range down.Routes[0].Legs {
		if l.Mode == "lift" {
			downLift = true
		}
	}
	if !upLift {
		t.Errorf("the uphill-only lift was not used uphill")
	}
	if downLift {
		t.Errorf("the uphill-only lift was ridden downhill")
	}

	// A car never rides a lift.
	if car := g.Route(Request{Points: []Point{at(0), at(1)}, Mode: "car", Grade: "E", Lifts: true}); len(car.Routes) != 0 {
		t.Errorf("a car routed on a footpath and a cable car")
	}
}

// chainRegion is a lift in sections, which is how a lift system is mapped: a
// road up the valley to the valley station V at 900 m and on to the mid station
// M at 1200, an old chairlift V -> M, a gondola M -> T at 2000, and a path from
// the top to a hut no road reaches. Every station is joined to the footpaths by
// a junction a few metres away, as the builder joins the real ones, and the
// long trail from the mid station to the top is the way up when the lifts are
// shut.
//
// gap is the metres between the top of the first section and the bottom of the
// second: zero when the two sections meet at one junction, and a short walk
// when the map gives each its own node, which is what it usually does.
// roadToValley takes the road to the valley station away — then the road climbs
// the other side and only the mid station can be parked at.
func chainRegion(gap float64, roadToValley bool) *Region {
	r := &Region{Name: "chain", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	var zs []float64
	pt := func(x, y, z float64) int {
		r.Pts = append(r.Pts, [2]float64{x, y})
		zs = append(zs, z)
		return len(r.Pts) - 1
	}
	add := func(cls, name string, lf Lift, ps ...int) {
		pts := make([][2]float64, len(ps))
		z := make([]float64, len(ps))
		cum := make([]float64, len(ps))
		for i, p := range ps {
			pts[i], z[i] = r.Pts[p], zs[p]
			if i > 0 {
				cum[i] = cum[i-1] + math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			}
		}
		st := &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[len(ps)-1], A: ps[0], B: ps[len(ps)-1],
			Cls: cls, Name: name, MTB: -1, Z: z,
		}
		for i := 1; i < len(z); i++ {
			if d := z[i] - z[i-1]; d > 0 {
				st.Up += d
			} else {
				st.Down -= d
			}
		}
		r.Stretches = append(r.Stretches, st)
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, lf)
		if lf.Is {
			r.Lifts++
		}
		r.WithElevation++
	}
	a := pt(0, 0, 400)
	vr := pt(3000, 0, 900)  // the road at the valley station
	vp := pt(3030, 0, 900)  // the footpath that reaches it
	v := pt(3050, 0, 900)   // the valley station
	mr := pt(6000, 0, 1200) // the road at the mid station
	mp := pt(6030, 0, 1200)
	m1 := pt(6050, 0, 1200) // where the first section ends
	m2p, m2 := mp, m1       // and where the second begins
	if gap > 0 {
		m2p = pt(6030+gap, 0, 1200)
		m2 = pt(6050+gap, 0, 1200)
	}
	top := pt(9050, 0, 2000)
	tp := pt(9100, 0, 2000)
	d := pt(11000, 0, 1950)

	if roadToValley {
		add("residential", "Strada della Valle", Lift{}, a, vr)
		add("residential", "Strada delle Funivie", Lift{}, vr, mr)
	} else {
		add("residential", "Strada Alta", Lift{}, a, pt(3000, 2000, 800), mr)
		add("path", "Sentiero della Valle", Lift{}, vp, mp)
	}
	add("path", "Sentiero della Stazione", Lift{}, vr, vp)
	add("path", "Sentiero della Stazione Media", Lift{}, mr, mp)
	if gap > 0 {
		add("path", "Sentiero fra le Stazioni", Lift{}, mp, m2p)
	}
	add("path", "Sentiero dei Larici", Lift{}, mp, tp)
	add("path", "Sentiero del Rifugio", Lift{}, tp, d)
	add("lift", "Seggiovia della Valle", Lift{Is: true, Type: "chair_lift", Dur: 1800}, v, m1)
	add("lift", "Cabinovia del Ghiacciaio", Lift{Is: true, Type: "gondola"}, m2, top)
	r.PtZ = zs
	return r
}

// liftLegs are the rides of a route, in the order they are taken.
func liftLegs(r *Route) []*Leg {
	var out []*Leg
	for _, l := range r.Legs {
		if l.Mode == "lift" {
			out = append(out, l)
		}
	}
	return out
}

// parkedAt is where a route left the car, in the region's own metres along the
// valley — NaN when it left it nowhere.
func parkedAt(g *Graph, r *Route) float64 {
	if r.Parking == nil {
		return math.NaN()
	}
	x, _ := g.R.XY(r.Parking.Lat, r.Parking.Lon)
	return x
}

// TestParkAtTheLowestStation: a lift in sections, and a road that reaches the
// middle of it. The clock says to drive up to the mid station and ride the top
// half; the DEFAULT parks at the valley station and rides the whole chain,
// because that is the day out a person who asked for lifts asked for. The fast
// plan is not thrown away — it is the alternative.
func TestParkAtTheLowestStation(t *testing.T) {
	for _, gap := range []float64{0, 120} {
		g := Build(chainRegion(gap, true))
		at := func(x float64) Point {
			lat, lon := g.R.LatLon(x, 0)
			return Point{Lat: lat, Lon: lon}
		}
		res := g.Route(Request{Points: []Point{at(0), at(11000)},
			Mode: "car+hike", Grade: "E", Lifts: true, Alternatives: 3})
		if len(res.Routes) < 2 {
			t.Fatalf("gap %.0f: %d routes (%s)", gap, len(res.Routes), res.Reason)
		}
		def := res.Routes[0]
		if def.Parking == nil {
			t.Fatalf("gap %.0f: the default parked nowhere", gap)
		}
		if def.Parking.Point != nil {
			t.Errorf("gap %.0f: parking.point is %d, want null: the caller named no stop",
				gap, *def.Parking.Point)
		}
		if x := parkedAt(g, def); math.Abs(x-3000) > 60 {
			t.Errorf("gap %.0f: parked at %.0f m along the valley, want the valley station at 3000", gap, x)
		}
		if def.Parking.Name == "" || !hasWarning(def, "parked at "+def.Parking.Name) {
			t.Errorf("gap %.0f: warnings %v do not name the parking %q", gap, def.Warnings, def.Parking.Name)
		}
		rides := liftLegs(def)
		if len(rides) != 2 {
			t.Fatalf("gap %.0f: %d rides in %v, want the whole chain", gap, len(rides), legModes(def))
		}
		if rides[0].Name != "Seggiovia della Valle" || rides[1].Name != "Cabinovia del Ghiacciaio" {
			t.Errorf("gap %.0f: rode %q then %q, want the valley section first", gap, rides[0].Name, rides[1].Name)
		}
		// The two minutes of parking are in the trip's clock, in no leg's, and
		// the rest of the figures are the longer plan's own.
		sum := 0.0
		for _, lg := range def.Legs {
			sum += lg.Seconds
		}
		if def.Seconds-sum < switchCost-1 {
			t.Errorf("gap %.0f: the trip (%.0f s) does not carry the parking over its legs (%.0f s)",
				gap, def.Seconds, sum)
		}
		// The fast plan is still there: it drove to the mid station and rode
		// the top half, and it is quicker, which is why it was not the default.
		mid := -1
		for i, r := range res.Routes[1:] {
			if math.Abs(parkedAt(g, r)-6000) < 60 {
				mid = i + 1
			}
		}
		if mid < 0 {
			t.Fatalf("gap %.0f: no alternative parks at the mid station: %v", gap, parkings(g, res.Routes))
		}
		if len(liftLegs(res.Routes[mid])) != 1 {
			t.Errorf("gap %.0f: the mid-station plan rides %d sections, want the upper one",
				gap, len(liftLegs(res.Routes[mid])))
		}
		if def.Seconds <= res.Routes[mid].Seconds {
			t.Errorf("gap %.0f: the default (%.0f s) is not slower than the fast plan (%.0f s): one of them is lying",
				gap, def.Seconds, res.Routes[mid].Seconds)
		}
		// The contract the page reads: one snap per requested point, and ids
		// that are still r1, r2, r3.
		if len(res.Snapped) != 2 {
			t.Errorf("gap %.0f: %d snapped points for a two-point request", gap, len(res.Snapped))
		}
		seen := map[string]bool{}
		for i, r := range res.Routes {
			if want := fmt.Sprintf("r%d", i+1); r.ID != want || seen[r.ID] {
				t.Errorf("gap %.0f: route %d is %q, want %q", gap, i, r.ID, want)
			}
			seen[r.ID] = true
		}
		// One route, and the fast plan is simply replaced.
		one := g.Route(Request{Points: []Point{at(0), at(11000)},
			Mode: "car+hike", Grade: "E", Lifts: true})
		if len(one.Routes) != 1 {
			t.Fatalf("gap %.0f: %d routes for one alternative", gap, len(one.Routes))
		}
		if x := parkedAt(g, one.Routes[0]); math.Abs(x-3000) > 60 {
			t.Errorf("gap %.0f: the only route parks at %.0f m, want the valley station", gap, x)
		}
	}
}

// TestNamedStopKeepsTheParking: a person who names the mid station parks at the
// mid station. A stop is where you park, and the chain rule never moves one.
func TestNamedStopKeepsTheParking(t *testing.T) {
	g := Build(chainRegion(120, true))
	at := func(x float64, name string) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon, Name: name}
	}
	res := g.Route(Request{Points: []Point{at(0, "Trento"), at(6000, "Stazione Media"), at(11000, "Rifugio")},
		Mode: "car+hike", Grade: "E", Lifts: true})
	if len(res.Routes) != 1 {
		t.Fatalf("refused: %s", res.Reason)
	}
	r := res.Routes[0]
	if r.Parking == nil || r.Parking.Point == nil {
		t.Fatalf("parking is %+v, want the stop the caller named", r.Parking)
	}
	if *r.Parking.Point != 1 || r.Parking.Name != "Stazione Media" {
		t.Errorf("parking is %+v, want point 1 and its name", r.Parking)
	}
	if x := parkedAt(g, r); math.Abs(x-6000) > 60 {
		t.Errorf("parked at %.0f m, want the mid station at 6000", x)
	}
	if rides := liftLegs(r); len(rides) != 1 || rides[0].Name != "Cabinovia del Ghiacciaio" {
		t.Errorf("rode %d sections, want only the one above the stop: %v", len(rides), legModes(r))
	}
}

// TestNoRoadToTheValleyStation: "whenever possible". With no road within reach
// of the bottom station the fast plan is the answer again, and it says so the
// way it always did.
func TestNoRoadToTheValleyStation(t *testing.T) {
	g := Build(chainRegion(120, false))
	at := func(x float64) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon}
	}
	res := g.Route(Request{Points: []Point{at(0), at(11000)}, Mode: "car+hike", Grade: "E", Lifts: true})
	if len(res.Routes) != 1 {
		t.Fatalf("refused: %s", res.Reason)
	}
	r := res.Routes[0]
	if r.Parking == nil || r.Parking.Point != nil {
		t.Fatalf("parking is %+v, want one the engine chose", r.Parking)
	}
	if x := parkedAt(g, r); math.Abs(x-6000) > 60 {
		t.Errorf("parked at %.0f m, want the mid station: no road reaches the valley one", x)
	}
	if rides := liftLegs(r); len(rides) != 1 {
		t.Errorf("rode %d sections, want the upper one only: %v", len(rides), legModes(r))
	}
}

// TestTheChainRuleTouchesNothingElse: a walk has no car to park, and a car+hike
// that refused the lifts is the trip it always was.
func TestTheChainRuleTouchesNothingElse(t *testing.T) {
	g := Build(chainRegion(120, true))
	at := func(x float64) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon}
	}
	walk := g.Route(Request{Points: []Point{at(0), at(11000)}, Mode: "hike", Grade: "E", Lifts: true})
	if len(walk.Routes) != 1 {
		t.Fatalf("no walk: %s", walk.Reason)
	}
	if p := walk.Routes[0].Parking; p != nil {
		t.Errorf("a walk reported parking %+v", p)
	}
	for _, w := range walk.Routes[0].Warnings {
		if strings.HasPrefix(w, "parked at") {
			t.Errorf("a walk warned %q", w)
		}
	}
	noLift := g.Route(Request{Points: []Point{at(0), at(11000)}, Mode: "car+hike", Grade: "E"})
	if len(noLift.Routes) != 1 {
		t.Fatalf("no route without the lifts: %s", noLift.Reason)
	}
	r := noLift.Routes[0]
	if len(liftLegs(r)) != 0 {
		t.Fatalf("a trip that did not ask for lifts rode one: %v", legModes(r))
	}
	if x := parkedAt(g, r); math.Abs(x-6000) > 60 {
		t.Errorf("parked at %.0f m, want the mid station the road ends at", x)
	}
}

// TestBikeParksAtTheLowestStation: the rule is about the first mode, whatever
// it is. A bicycle left at the valley station is the same answer as a car.
func TestBikeParksAtTheLowestStation(t *testing.T) {
	g := Build(chainRegion(120, true))
	at := func(x float64) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon}
	}
	res := g.Route(Request{Points: []Point{at(0), at(11000)},
		Mode: "bike+hike", Grade: "E", Lifts: true, Alternatives: 3})
	if len(res.Routes) < 2 {
		t.Fatalf("%d routes: %s", len(res.Routes), res.Reason)
	}
	def := res.Routes[0]
	if def.Parking == nil || def.Parking.Point != nil {
		t.Fatalf("parking is %+v, want one the engine chose", def.Parking)
	}
	if x := parkedAt(g, def); math.Abs(x-3000) > 60 {
		t.Errorf("left the bike at %.0f m, want the valley station at 3000", x)
	}
	if rides := liftLegs(def); len(rides) != 2 {
		t.Errorf("rode %d sections, want the whole chain: %v", len(rides), legModes(def))
	}
	if legModes(def)[0] != "bike" {
		t.Errorf("the first leg is %q, want the ride to the valley station", legModes(def)[0])
	}
	mid := false
	for _, r := range res.Routes[1:] {
		if math.Abs(parkedAt(g, r)-6000) < 60 {
			mid = true
		}
	}
	if !mid {
		t.Errorf("no alternative parks at the mid station: %v", parkings(g, res.Routes))
	}
}

// parkings is where each route of an answer left the car, for a failure to
// print.
func parkings(g *Graph, rs []*Route) []float64 {
	out := make([]float64, len(rs))
	for i, r := range rs {
		out[i] = parkedAt(g, r)
	}
	return out
}

// shapeRegion: two ways from A to B, a short one through the middle and a
// longer one round the north. Enough to drag a route onto the long way, or to
// strike the short one out.
func shapeRegion() *Region {
	r := &Region{Name: "shape", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {2000, 0}, {1000, 0}, {1000, 800}}
	add := func(a, b int, name string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: "residential", Name: name, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
	}
	add(0, 2, "Via Diretta")
	add(2, 1, "Via Diretta")
	add(0, 3, "Via Lunga")
	add(3, 1, "Via Lunga")
	return r
}

// TestViaBendsTheRoute: a via shapes the line and cuts nothing.
func TestViaBendsTheRoute(t *testing.T) {
	g := Build(shapeRegion())
	at := func(x, y float64, via bool) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon, Via: via}
	}
	plain := g.Route(Request{Points: []Point{at(0, 0, false), at(2000, 0, false)}, Mode: "car"})
	if len(plain.Routes) != 1 {
		t.Fatalf("no plain route: %s", plain.Reason)
	}
	if m := plain.Routes[0].Meters; math.Abs(m-2000) > 50 {
		t.Fatalf("the plain route is %.0f m, want the short way", m)
	}
	bent := g.Route(Request{Points: []Point{at(0, 0, false), at(1000, 800, true), at(2000, 0, false)}, Mode: "car"})
	if len(bent.Routes) != 1 {
		t.Fatalf("no route through the via: %s", bent.Reason)
	}
	b := bent.Routes[0]
	if math.Abs(b.Meters-2561) > 60 {
		t.Errorf("the bent route is %.0f m, want the long way (~2561)", b.Meters)
	}
	// A via is not a stop: one leg, and the steps run straight through.
	if len(b.Legs) != 1 {
		t.Fatalf("a via cut the trip into %d legs", len(b.Legs))
	}
	if b.Parking != nil {
		t.Errorf("a single-mode trip reported parking")
	}
	// It is echoed, marked, and does not count as a stop.
	if len(bent.Snapped) != 3 || !bent.Snapped[1].Via || bent.Snapped[0].Via {
		t.Errorf("snapped is %+v", bent.Snapped)
	}
}

// TestViaNeverParks: the car is left at the stop, never at a via before it.
func TestViaNeverParks(t *testing.T) {
	g := Build(brentaRegion())
	at := func(x float64, via bool, name string) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon, Via: via, Name: name}
	}
	res := g.Route(Request{
		Points: []Point{at(0, false, "Trento"), at(500, true, ""), at(1000, false, "Vallesinella"), at(2500, false, "Brentei")},
		Mode:   "car+hike", Grade: "E",
	})
	if len(res.Routes) != 1 {
		t.Fatalf("refused: %s", res.Reason)
	}
	r := res.Routes[0]
	if r.Parking == nil || r.Parking.Point == nil {
		t.Fatalf("parking is %+v, want the stop", r.Parking)
	}
	if *r.Parking.Point != 2 {
		t.Errorf("parking.point is %d, want 2 (the stop, not the via at 1)", *r.Parking.Point)
	}
	if r.Parking.Name != "Vallesinella" {
		t.Errorf("parking name is %q", r.Parking.Name)
	}
	if modes := legModes(r); len(modes) != 2 || modes[0] != "car" || modes[1] != "hike" {
		t.Errorf("legs are %v, want car then hike: the via must not cut one", modes)
	}
}

// TestViaOnALiftForcesTheRide: dropping a via on a cable car means "ride it",
// and which end you board follows the direction of travel — unless the lift
// only runs one way, which decides for you.
func TestViaOnALiftForcesTheRide(t *testing.T) {
	g := Build(liftRegion(false))
	at := func(x, y float64, via bool) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon, Via: via}
	}
	mid := at(50, -1500, true) // the middle of the lift line
	up := g.Route(Request{Points: []Point{at(0, 0, false), mid, at(0, -3000, false)}, Mode: "hike", Grade: "E", Lifts: true})
	if len(up.Routes) != 1 {
		t.Fatalf("no route: %s", up.Reason)
	}
	var ride *Leg
	for _, l := range up.Routes[0].Legs {
		if l.Mode == "lift" {
			ride = l
		}
	}
	if ride == nil {
		t.Fatalf("the via did not force the ride: legs %v", legModes(up.Routes[0]))
	}
	if ride.Name != "Funivia del Monte" {
		t.Errorf("rode %q", ride.Name)
	}
	// Boarded at the bottom: the first coordinate of the ride is the end
	// nearer the point before it.
	first := ride.Geometry.Coordinates[0]
	blat, blon := g.R.LatLon(50, 0)
	if math.Abs(first[1]-blat) > 1e-4 || math.Abs(first[0]-blon) > 1e-4 {
		t.Errorf("boarded at %v, want the bottom station (%.5f,%.5f)", first, blat, blon)
	}
	// The other way round, on a lift that runs both ways, boards at the top.
	down := g.Route(Request{Points: []Point{at(0, -3000, false), mid, at(0, 0, false)}, Mode: "hike", Grade: "E", Lifts: true})
	for _, l := range down.Routes[0].Legs {
		if l.Mode == "lift" {
			tlat, tlon := g.R.LatLon(50, -3000)
			c := l.Geometry.Coordinates[0]
			if math.Abs(c[1]-tlat) > 1e-4 || math.Abs(c[0]-tlon) > 1e-4 {
				t.Errorf("downhill boarded at %v, want the top station", c)
			}
		}
	}
	// An uphill-only lift is boarded at the bottom whichever way the trip goes.
	gu := Build(liftRegion(true))
	d2 := gu.Route(Request{Points: []Point{at(0, -3000, false), mid, at(0, 0, false)}, Mode: "hike", Grade: "E", Lifts: true})
	if len(d2.Routes) != 1 {
		t.Fatalf("no uphill-only route: %s", d2.Reason)
	}
	for _, l := range d2.Routes[0].Legs {
		if l.Mode == "lift" {
			blat, blon := gu.R.LatLon(50, 0)
			c := l.Geometry.Coordinates[0]
			if math.Abs(c[1]-blat) > 1e-4 || math.Abs(c[0]-blon) > 1e-4 {
				t.Errorf("an uphill-only lift was boarded at %v, want the bottom", c)
			}
		}
	}
}

// TestAvoid: strike a way out and the second best answers; strike them all out
// and the refusal says which way it needed.
func TestAvoid(t *testing.T) {
	g := Build(shapeRegion())
	at := func(x, y float64) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon}
	}
	res := g.Route(Request{
		Points: []Point{at(0, 0), at(2000, 0)}, Mode: "car",
		Avoid: []Point{at(500, 0)}, // on Via Diretta
	})
	if len(res.Routes) != 1 {
		t.Fatalf("avoiding the short way refused everything: %s", res.Reason)
	}
	if m := res.Routes[0].Meters; math.Abs(m-2561) > 60 {
		t.Errorf("the route is %.0f m, want the long way round", m)
	}
	if len(res.Avoided) != 1 || res.Avoided[0].Name != "Via Diretta" || res.Avoided[0].Class != "residential" {
		t.Fatalf("avoided is %+v", res.Avoided)
	}
	if len(res.Avoided[0].Points) < 2 {
		t.Errorf("the avoided way has no geometry to hatch")
	}
	for _, s := range res.Routes[0].Steps {
		if s.Name == "Via Diretta" {
			t.Errorf("the route used the way it was told to keep off")
		}
	}
	// A point on nothing is dropped, not refused.
	far := g.Route(Request{Points: []Point{at(0, 0), at(2000, 0)}, Mode: "car", Avoid: []Point{at(9000, 9000)}})
	if len(far.Routes) != 1 || len(far.Avoided) != 0 {
		t.Errorf("a stray avoid cost the route: %d routes, avoided %v", len(far.Routes), far.Avoided)
	}
	// Both ways out struck: refused, and the refusal names one of them.
	both := g.Route(Request{Points: []Point{at(0, 0), at(2000, 0)}, Mode: "car",
		Avoid: []Point{at(500, 0), at(500, 400)}})
	if len(both.Routes) != 0 {
		t.Fatalf("routed with both ways struck out")
	}
	if !strings.HasPrefix(both.Reason, "no route without ") {
		t.Errorf("reason is %q", both.Reason)
	}
	if !strings.Contains(both.Reason, "Via ") {
		t.Errorf("the refusal does not name a way: %q", both.Reason)
	}
	// And the plain request is untouched by any of this.
	plain := g.Route(Request{Points: []Point{at(0, 0), at(2000, 0)}, Mode: "car"})
	if len(plain.Routes) != 1 || math.Abs(plain.Routes[0].Meters-2000) > 50 || plain.Avoided != nil {
		t.Errorf("a request with neither via nor avoid changed: %+v", plain.Routes[0].Meters)
	}
}

// TestNoPhantomParking: a walk has no parking, whatever the plan said.
//
// The page draws a P badge on whatever `parking` names, so `parking` has to be
// a fact about the legs. A car+hike trip whose stop sits six metres from the
// start produced a route of one hike leg and a parking at "Via Dolomiti" —
// the car never moved.
func TestNoPhantomParking(t *testing.T) {
	g := Build(parkRegion())
	at := func(x float64, name string) Point {
		lat, lon := g.R.LatLon(x, 0)
		return Point{Lat: lat, Lon: lon, Name: name}
	}
	// A single-mode plan never carries one, with or without stops.
	for _, mode := range []string{"hike", "car", "bike"} {
		for _, pts := range [][]Point{
			{at(0, ""), at(2000, "")},
			{at(0, ""), at(1000, "middle"), at(2000, "")},
		} {
			res := g.Route(Request{Points: pts, Mode: mode, Grade: "E"})
			if len(res.Routes) != 1 {
				t.Fatalf("%s: refused: %s", mode, res.Reason)
			}
			r := res.Routes[0]
			if r.Parking != nil {
				t.Errorf("%s with %d points reported parking %+v", mode, len(pts), r.Parking)
			}
			for _, w := range r.Warnings {
				if strings.HasPrefix(w, "parked at") {
					t.Errorf("%s warned %q", mode, w)
				}
			}
		}
	}
	// A two-mode plan whose stop is on top of the start: the car never moves,
	// so there is no parking, no warning and no two minutes of it.
	near := at(0.5, "Via Dolomiti")
	res := g.Route(Request{Points: []Point{at(0, ""), near, at(2000, "")}, Mode: "car+hike", Grade: "E"})
	if len(res.Routes) != 1 {
		t.Fatalf("refused: %s", res.Reason)
	}
	r := res.Routes[0]
	drove := false
	for _, lg := range r.Legs {
		if lg.Mode == "car" {
			drove = true
		}
	}
	if !drove {
		if r.Parking != nil {
			t.Errorf("a trip that never drove reported parking %+v", r.Parking)
		}
		for _, w := range r.Warnings {
			if strings.HasPrefix(w, "parked at") {
				t.Errorf("a trip that never drove warned %q", w)
			}
		}
		sum := 0.0
		for _, lg := range r.Legs {
			sum += lg.Seconds
		}
		if r.Seconds-sum > 1 {
			t.Errorf("the trip carries %.0f s more than its legs: phantom parking", r.Seconds-sum)
		}
	}
	// And the real thing still says so: driving then walking keeps its parking.
	real := g.Route(Request{Points: []Point{at(0, "Trento"), at(1000, "Molveno"), at(2000, "")}, Mode: "car+hike", Grade: "E"})
	rr := real.Routes[0]
	if rr.Parking == nil || rr.Parking.Point == nil || *rr.Parking.Point != 1 {
		t.Fatalf("the real parking went missing: %+v", rr.Parking)
	}
	if !hasWarning(rr, "parked at Molveno") {
		t.Errorf("warnings %v", rr.Warnings)
	}
}

// TestParkingNameSkipsTrailRefs: "parked at trail SF" is as useless as "parked
// at trail 102" — the car is on neither. Every synthetic "trail <ref>" name is
// refused whatever the ref looks like, and the place two kilometres away is
// used instead; real names are left alone.
func TestParkingNameSkipsTrailRefs(t *testing.T) {
	for _, n := range []string{
		"trail SF", "trail E401A", "trail O3", "trail 102", "trail 358A",
		"TRAIL sf", "102", "SAT 102", "cai 5",
	} {
		if !trailNumber(n) {
			t.Errorf("%q is taken for a parking name", n)
		}
	}
	for _, n := range []string{
		"Via Vallesinella", "Strada di Fondovalle", "Pian Venezia",
		"Sentiero del Masare", "Strada Provinciale 87 di Peio", "Trailfoot Road",
	} {
		if trailNumber(n) {
			t.Errorf("%q was refused as a parking name", n)
		}
	}

	// A trailhead whose only name is a route ref: the place 1.2 km away is
	// the answer, not "trail SF".
	r := &Region{Name: "sf", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, {2500, 0}}
	add := func(a, b int, cls, ref string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: cls + ref, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls,
			HikeRef: ref, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
	}
	add(0, 1, "service", "SF") // the unnamed access road, carrying a route ref
	add(1, 2, "path", "SF")    // the path on from the trailhead
	g := Build(r)
	head := g.dense[1]
	if got := g.NameAt(head, []int{modeHike}); got != "trail SF" {
		t.Fatalf("the junction is called %q, the test needs it to be \"trail SF\"", got)
	}
	// With nothing else to call it, NO name: the road carries the trail's ref
	// for a while, and "parked at trail SF" says the car is on the footpath.
	// The answer still carries the coordinates, and the card says trailhead.
	if got := g.ParkName(head, modeCar); got != "" {
		t.Errorf("with nothing but a route ref to go on, the parking is %q, want no name", got)
	}
	lat, lon := g.R.LatLon(1000, 1200) // a place 1.2 km away
	g.SetParkPlaces([]ParkPlace{{Name: "Malga Mare", Lat: lat, Lon: lon}})
	if got := g.ParkName(head, modeCar); got != "Malga Mare" {
		t.Errorf("the parking is %q, want the place rather than the route ref", got)
	}
}

// TestRouteRefusesNonsense: whatever reaches the engine, it answers instead of
// panicking. A message off the queue is not a validated request — a stale one,
// another client's, a probe somebody fired at the broker — and a panic in a
// worker takes the whole service with it (it did, once).
func TestRouteRefusesNonsense(t *testing.T) {
	g := Build(gridRegion(t))
	for _, req := range []Request{
		{},
		{Points: []Point{}},
		{Points: []Point{{Lat: 46, Lon: 11}}},
		{Mode: "car+hike", Grade: "EEA", Alternatives: 3},
		{Points: []Point{{Lat: 46, Lon: 11}}, Mode: "hike", Lifts: true,
			Avoid: []Point{{Lat: 46, Lon: 11}}},
	} {
		res := g.Route(req)
		if len(res.Routes) != 0 {
			t.Errorf("%+v was routed", req)
		}
		if res.Reason == "" {
			t.Errorf("%+v was refused without a reason", req)
		}
	}
}

// A marked trail runs along the valley road for its first kilometres, so the
// road carries hiking_ref even where it is asphalt and public. Naming a
// DRIVING step after it told the driver to turn onto a footpath: the route to
// Monte Stivo announced "trail 608B" for 724 m of unclassified road.
func TestDrivingStepsAreNeverNamedAfterATrail(t *testing.T) {
	g := testGraphRefs(t)
	road := int32(0) // unnamed unclassified road carrying hiking_ref 608B
	if got := g.stepName(road, modeCar); got != "lane" {
		t.Errorf("car step on an unnamed road with a trail ref = %q, want the class word", got)
	}
	if got := g.stepName(road, modeBike); got != "lane" {
		t.Errorf("bike step = %q, want the class word", got)
	}
	if got := g.stepName(road, modeHike); got != "trail 608B" {
		t.Errorf("walking step = %q, want the trail", got)
	}
	path := int32(1) // an unnamed path carrying the same ref
	if got := g.stepName(path, modeHike); got != "trail 608B" {
		t.Errorf("walking a marked path = %q, want the trail", got)
	}
	if got := g.stepName(path, modeCar); got != "path" {
		t.Errorf("a path named for a car = %q, want the class word", got)
	}
	named := int32(2) // the same road, but with a name
	for _, m := range []int{modeCar, modeBike, modeHike} {
		if got := g.stepName(named, m); got != "Via S. Antonio" {
			t.Errorf("a named road in mode %d = %q, want its name", m, got)
		}
	}
	ref := int32(3) // unnamed, but the road's own number
	if got := g.stepName(ref, modeCar); got != "SP 235" {
		t.Errorf("a road with a ref = %q, want the ref", got)
	}
	if got := g.stepName(ref, modeHike); got != "trail 608B" {
		t.Errorf("walking the same road = %q, want the trail it carries", got)
	}
	fast := int32(4) // unnamed secondary
	if got := g.stepName(fast, modeCar); got != "road" {
		t.Errorf("an unnamed secondary = %q, want \"road\"", got)
	}
}

// testGraphRefs is five stretches that differ only in what names them.
func testGraphRefs(t *testing.T) *Graph {
	t.Helper()
	r := &Region{
		Stretches: []*city.Stretch{
			{Cls: "residential", HikeRef: "608B", Len: 100},
			{Cls: "path", HikeRef: "608B", Len: 100},
			{Cls: "residential", Name: "Via S. Antonio", HikeRef: "608B", Len: 100},
			{Cls: "residential", Ref: "SP 235", HikeRef: "608B", Len: 100},
			{Cls: "secondary", Len: 100},
		},
		Sat: make([]Sat, 5),
	}
	return &Graph{R: r}
}

// TestWalkingClockFactors pins what the Alpine club rule is multiplied by. On
// a flat kilometre the rule says 900 s; a marked trail walks it at ×0.9, an
// unmarked path, a track or a street at ×1.2, a main road at ×1.5. The order
// is the preference — a numbered trail beats the track beside it — and the
// factor is in the reported time, so it may only ever err on the slow side.
func TestWalkingClockFactors(t *testing.T) {
	flat := func(cls string) *city.Stretch {
		return &city.Stretch{Cls: cls, Len: 1000, Pts: [][2]float64{{0, 0}, {1000, 0}}, Cum: []float64{0, 1000}, MTB: -1}
	}
	cases := []struct {
		name   string
		st     *city.Stretch
		marked bool
		want   float64
	}{
		{"marked trail on a path", flat("path"), true, 810},
		{"marked trail on a track", flat("track"), true, 810},
		{"unmarked path", flat("path"), false, 1080},
		{"unmarked track", flat("track"), false, 1080},
		{"pedestrian street", flat("pedestrian"), false, 1080},
		{"residential street", flat("residential"), false, 1080},
		{"main road", flat("primary"), false, 1350},
		{"marked trail along a main road", flat("primary"), true, 810},
	}
	for _, c := range cases {
		if got := weight(modeHike, c.st, false, c.marked); math.Abs(got-c.want) > 0.5 {
			t.Errorf("%s: %.0f s, want %.0f", c.name, got, c.want)
		}
	}
}

// forkRegion is two ways from the same start to the same end, both flat: a
// straight unmarked track of 1000 m, and a marked trail that bends through a
// third junction and is longer by the detour. The trail carries only a SAT
// number — no OSM hiking ref — so the cadastre's marking is what has to count.
func forkRegion(bend [2]float64) *Region {
	r := &Region{
		Name: "fork", Lat0: 46, Lon0: 11,
		MPerDegLat: 111000, MPerDegLon: 77000,
	}
	r.Pts = [][2]float64{{0, 0}, {1000, 0}, bend}
	mk := func(a, b int, cls, name, satNo string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		st := &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b, Cls: cls,
			Name: name, MTB: -1, Z: []float64{500, 500},
		}
		r.Stretches = append(r.Stretches, st)
		r.Sat = append(r.Sat, Sat{No: satNo, Name: name})
		r.WithElevation++
	}
	mk(0, 1, "track", "Strada forestale", "")
	mk(0, 2, "path", "Sentiero 623 sud", "623")
	mk(2, 1, "path", "Sentiero 623 nord", "623")
	return r
}

// TestMarkedTrailsWinOnFoot is the Trento → Monte Stivo rule: where a marked
// trail and an unmarked track both reach the same place, the walk takes the
// trail unless it is more than a third longer. At the rule's own pace the
// track won whenever it was a little shorter, and the walk to the Stivo took
// 20 km of tracks and provincial road before touching SAT 623.
func TestMarkedTrailsWinOnFoot(t *testing.T) {
	cases := []struct {
		name      string
		bend      [2]float64
		wantTrail bool
	}{
		// 1171 m of trail: 1054 s by the rule, 948 s at ×0.9 — against 1000 m
		// of track: 900 s by the rule, 1080 s at ×1.2.
		{"a trail a sixth longer wins", [2]float64{600, 300}, true},
		// 1562 m of trail: 1265 s at ×0.9, against the track's 1080 s.
		{"a trail half as long again loses", [2]float64{500, 600}, false},
	}
	for _, c := range cases {
		g := Build(forkRegion(c.bend))
		lat0, lon0 := g.R.LatLon(0, 0)
		lat1, lon1 := g.R.LatLon(1000, 0)
		res := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "hike", Grade: "E"})
		if len(res.Routes) != 1 {
			t.Fatalf("%s: no route (%s)", c.name, res.Reason)
		}
		trail := false
		for _, s := range res.Routes[0].Steps {
			if strings.HasPrefix(s.Name, "Sentiero 623") {
				trail = true
			}
		}
		if trail != c.wantTrail {
			t.Errorf("%s: walked the marked trail = %v, want %v (steps %v)",
				c.name, trail, c.wantTrail, stepNames(res.Routes[0].Steps))
		}
	}
}

// TestNameAtNeverCallsARoadATrail: the name a point snaps to obeys the same
// rule as a step. An unnamed road that a marked trail follows is "trail 608B"
// to the walker on it and a road to the driver on it — "the route starts
// 120 m away on trail 608B" told a driver the car was on a footpath. On
// wheels the road's own ref is the name, and with neither it is the class.
func TestNameAtNeverCallsARoadATrail(t *testing.T) {
	build := func(refOnSecond string) *Graph {
		r := &Region{Name: "stivo", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
		r.Pts = [][2]float64{{0, 0}, {1000, 0}, {2000, 0}, {3000, 0}}
		mk := func(a, b int, name, ref string) {
			pts := [][2]float64{r.Pts[a], r.Pts[b]}
			st := &city.Stretch{ID: "s" + name + ref + string(rune('0'+a)), Pts: pts, Cum: []float64{0, 1000}, Len: 1000,
				A: a, B: b, Cls: "residential", Name: name, Ref: ref, HikeRef: "608B", MTB: -1}
			r.Stretches = append(r.Stretches, st)
			r.Sat = append(r.Sat, Sat{})
		}
		mk(0, 1, "", "")          // an unnamed road the trail follows
		mk(1, 2, "", refOnSecond) // the same, perhaps with the road's own ref
		mk(2, 3, "Via S. Antonio", "")
		return Build(r)
	}
	car, hike := []int{modeCar}, []int{modeHike}
	g := build("SP 235")
	if got := g.NameAt(0, hike); got != "trail 608B" {
		t.Errorf("on foot the junction is %q, want \"trail 608B\"", got)
	}
	if got := g.NameAt(0, car); got != "SP 235" {
		t.Errorf("by car the junction is %q, want the road's own ref", got)
	}
	if got := g.NameAt(3, car); got != "Via S. Antonio" {
		t.Errorf("by car a named road is %q, want its name", got)
	}
	g = build("")
	if got := g.NameAt(0, car); got == "trail 608B" || got == "" {
		t.Errorf("by car, with no name and no ref within reach, the junction is %q: never the trail, never empty", got)
	}
	if got := g.NameAt(0, hike); got != "trail 608B" {
		t.Errorf("on foot the same junction is %q, want the trail", got)
	}
	// A two-mode plan names the junction after the mode that reaches it.
	if got := g.NameAt(0, []int{modeCar, modeHike}); got == "trail 608B" {
		t.Errorf("a car+hike start on a road is %q: the car reaches it, so not the trail", got)
	}
}

// TestRouteCarriesEachLineOnce pins the wire shape that keeps an answer inside
// the queue's reply: the line and the profile ride on the legs and are not
// repeated at the top of the route. The page joins the legs.
func TestRouteCarriesEachLineOnce(t *testing.T) {
	g := Build(twoModeRegion())
	lat0, lon0 := g.R.LatLon(0, 0)
	lat1, lon1 := g.R.LatLon(2000, 0)
	res := g.Route(Request{Points: []Point{{Lat: lat0, Lon: lon0}, {Lat: lat1, Lon: lon1}}, Mode: "car+hike", Grade: "E"})
	if len(res.Routes) != 1 {
		t.Fatalf("no route: %s", res.Reason)
	}
	raw, err := json.Marshal(res.Routes[0])
	if err != nil {
		t.Fatal(err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatal(err)
	}
	for _, k := range []string{"geometry", "profile"} {
		if _, dup := wire[k]; dup {
			t.Errorf("the route repeats %q at the top level; the legs carry it", k)
		}
	}
	var legs []map[string]json.RawMessage
	if err := json.Unmarshal(wire["legs"], &legs); err != nil || len(legs) != 2 {
		t.Fatalf("want two legs on the wire, got %d (%v)", len(legs), err)
	}
	for i, l := range legs {
		if _, ok := l["geometry"]; !ok {
			t.Errorf("leg %d has no geometry on the wire", i)
		}
		if _, ok := l["profile"]; !ok {
			t.Errorf("leg %d has no profile on the wire", i)
		}
	}
	// And the struct still holds the joined line for the engine's own use.
	r := res.Routes[0]
	if want := len(r.Legs[0].Geometry.Coordinates) + len(r.Legs[1].Geometry.Coordinates) - 1; len(r.Geometry.Coordinates) != want {
		t.Errorf("joined line has %d points, want %d", len(r.Geometry.Coordinates), want)
	}
}

// branchRegion is a valley road that every answer has to drive — the stem —
// and three ways on from the junction at its head, straight on, round the
// north and round the south. There is no second way onto the stem, so every
// alternative after the first is found while the first one's penalty sits on
// the stem: the shape that catches a search-only cost leaking into the clock.
func branchRegion() *Region {
	r := &Region{Name: "branch", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{
		{0, 0},        // 0 the start
		{3000, 0},     // 1 the head of the valley
		{6000, 0},     // 2 straight on
		{6000, 1500},  // 3 round the north
		{6000, -2500}, // 4 round the south
		{9000, 0},     // 5 the end
	}
	add := func(a, b int, name string) {
		pts := [][2]float64{r.Pts[a], r.Pts[b]}
		cum := []float64{0, math.Hypot(pts[1][0]-pts[0][0], pts[1][1]-pts[0][1])}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[1], A: a, B: b,
			Cls: "residential", Name: name, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
	}
	add(0, 1, "Strada del Fondovalle")
	add(1, 2, "Via Diretta")
	add(2, 5, "Via Diretta")
	add(1, 3, "Via del Nord")
	add(3, 5, "Via del Nord")
	add(1, 4, "Via del Sud")
	add(4, 5, "Via del Sud")
	return r
}

// TestAlternativesReportHonestTimes: what an alternative says it takes is what
// it takes. A penalty round makes a stretch an earlier route used cost half as
// much again SO THAT the search looks elsewhere; it is not a road that got
// slower, and the seconds on the card must be the seconds of the line. Trento
// to Cima Presanella reported the third card's 79.5 km at 109 minutes and the
// first card's 72.6 km of the same roads at 75: half an hour of arithmetic.
//
// Every route here drives the stem, which is penalised from the second round
// on, so each alternative is compared with the same line asked for on its own.
func TestAlternativesReportHonestTimes(t *testing.T) {
	g := Build(branchRegion())
	at := func(x, y float64) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon}
	}
	res := g.Route(Request{Points: []Point{at(0, 0), at(9000, 0)}, Mode: "car", Alternatives: 3})
	if len(res.Routes) != 3 {
		t.Fatalf("%d routes on a three-way branch (%s)", len(res.Routes), res.Reason)
	}
	// The same three lines, each forced on its own by striking out the ways
	// the clock would otherwise prefer. No penalty is anywhere near these.
	alone := []*Result{
		g.Route(Request{Points: []Point{at(0, 0), at(9000, 0)}, Mode: "car"}),
		g.Route(Request{Points: []Point{at(0, 0), at(9000, 0)}, Mode: "car",
			Avoid: []Point{at(4500, 0)}}),
		g.Route(Request{Points: []Point{at(0, 0), at(9000, 0)}, Mode: "car",
			Avoid: []Point{at(4500, 0), at(4500, 750)}}),
	}
	for i, a := range alone {
		if len(a.Routes) != 1 {
			t.Fatalf("the forced route %d was refused: %s", i+1, a.Reason)
		}
	}
	for i, r := range res.Routes {
		want := alone[i].Routes[0]
		if got, w := r.Meters, want.Meters; math.Abs(got-w) > 1 {
			t.Fatalf("%s is %.0f m and the line asked for on its own is %.0f: they are not the same route",
				r.ID, got, w)
		}
		if got, w := r.Seconds, want.Seconds; math.Abs(got-w) > 1 {
			t.Errorf("%s reports %.0f s for a line that takes %.0f (%.2fx): the penalty round's cost reached the clock",
				r.ID, got, w, got/w)
		}
		// And the trip's clock is still its legs' clock.
		sum := 0.0
		for _, lg := range r.Legs {
			sum += lg.Seconds
		}
		if math.Abs(r.Seconds-sum) > 2 {
			t.Errorf("%s is %.0f s over legs adding up to %.0f", r.ID, r.Seconds, sum)
		}
	}
}

// twoSidesRegion is a summit with a side each: a short road to the south
// trailhead with three trails off it, and a longer road round to the north
// trailhead with one. The south is the fastest and the north is worth showing
// — under twice the clock — but no amount of penalising stretches ever leaves
// the south, because every penalty only sends the search to the next trail out
// of the same car park. northRoad is what the second side hangs on.
func twoSidesRegion(northRoad bool) *Region {
	r := &Region{Name: "twosides", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{
		{0, 0},        // 0 the town
		{9000, 1000},  // 1 the south trailhead
		{9000, 6000},  // 2 the summit
		{2000, 10000}, // 3 the north trailhead
		{0, 10000},    // 4 the corner the north road turns at
		{9300, 3500},  // 5 bends, one per south trail
		{10200, 3200}, // 6
		{7000, 3800},  // 7
	}
	add := func(cls, name string, ps ...int) {
		pts := make([][2]float64, len(ps))
		cum := make([]float64, len(ps))
		for i, p := range ps {
			pts[i] = r.Pts[p]
			if i > 0 {
				cum[i] = cum[i-1] + math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			}
		}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[len(ps)-1], A: ps[0], B: ps[len(ps)-1],
			Cls: cls, Name: name, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
	}
	add("residential", "Strada della Val Bassa", 0, 1)
	add("path", "Sentiero delle Malghe", 1, 5, 2)
	add("path", "Sentiero dei Larici", 1, 6, 2)
	add("path", "Sentiero della Cresta", 1, 7, 2)
	if northRoad {
		add("residential", "Strada di Stavel", 0, 4, 3)
	}
	add("path", "Sentiero del Versante Nord", 3, 2)
	return r
}

// TestAlternativesTryAnotherTrailhead is the complaint itself, on a map small
// enough to check by hand: three alternatives to a summit that all park in the
// same valley are one answer drawn three times, and the walk in from the other
// side never gets a card. The first penalty round now asks for a DIFFERENT
// trailhead, and pays about the fastest trip's own clock for it.
func TestAlternativesTryAnotherTrailhead(t *testing.T) {
	g := Build(twoSidesRegion(true))
	at := func(x, y float64) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon}
	}
	res := g.Route(Request{Points: []Point{at(0, 0), at(9000, 6000)},
		Mode: "car+hike", Grade: "E", Alternatives: 3})
	if len(res.Routes) != 3 {
		t.Fatalf("%d routes (%s)", len(res.Routes), res.Reason)
	}
	if x := parkedAt(g, res.Routes[0]); math.Abs(x-9000) > 500 {
		t.Fatalf("the fastest parks at %.0f, want the south trailhead at 9000", x)
	}
	north := -1
	for i, r := range res.Routes {
		if math.Abs(parkedAt(g, r)-2000) < 500 {
			north = i
		}
	}
	if north < 0 {
		t.Fatalf("no alternative parks on the north side: %v", parkings(g, res.Routes))
	}
	// It is an alternative, not a different holiday: under twice the fastest.
	if got, fast := res.Routes[north].Seconds, res.Routes[0].Seconds; got > 2*fast {
		t.Errorf("the north side takes %.0f s against the fastest %.0f: %.2fx, and it was shown anyway",
			got, fast, got/fast)
	}
	// The rounds after the first penalise stretches, as they always did, so
	// the third card is the second trail out of the fastest one's car park.
	same := 0
	for _, r := range res.Routes {
		if math.Abs(parkedAt(g, r)-9000) < 500 {
			same++
		}
	}
	if same != 2 {
		t.Errorf("%d routes park at the south trailhead, want two: %v", same, parkings(g, res.Routes))
	}

	// With no road to the north trailhead there is no other side to find, and
	// the answer is the three trails the stretch penalties find, as before.
	only := Build(twoSidesRegion(false))
	res = only.Route(Request{Points: []Point{at(0, 0), at(9000, 6000)},
		Mode: "car+hike", Grade: "E", Alternatives: 3})
	if len(res.Routes) != 3 {
		t.Fatalf("no north road: %d routes (%s)", len(res.Routes), res.Reason)
	}
	for _, r := range res.Routes {
		if x := parkedAt(only, r); math.Abs(x-9000) > 500 {
			t.Errorf("no north road: %s parked at %.0f, and there is nowhere else", r.ID, x)
		}
	}
	seen := map[string]bool{}
	for _, r := range res.Routes {
		key := strings.Join(stepNames(r.Steps), "|")
		if seen[key] {
			t.Errorf("no north road: two identical routes: %s", key)
		}
		seen[key] = true
	}

	// A STOP IS WHERE YOU PARK. A caller who named the south trailhead asked
	// for it, and no round may offer to drive round the mountain instead.
	stop := at(9000, 1000)
	stop.Name = "Malga Bassa"
	named := g.Route(Request{Points: []Point{at(0, 0), stop, at(9000, 6000)},
		Mode: "car+hike", Grade: "E", Alternatives: 3})
	if len(named.Routes) == 0 {
		t.Fatalf("named stop: no route (%s)", named.Reason)
	}
	for _, r := range named.Routes {
		if r.Parking == nil || r.Parking.Point == nil || *r.Parking.Point != 1 {
			t.Errorf("named stop: %s parked at %+v, want the stop the caller named", r.ID, r.Parking)
			continue
		}
		if x := parkedAt(g, r); math.Abs(x-9000) > 500 {
			t.Errorf("named stop: %s parked at %.0f, want Malga Bassa at 9000", r.ID, x)
		}
	}
}

// TestPresanellaOnTheRegionMap is the complaint itself, against the map the
// service serves: Trento to Cima Presanella answered with three walks off the
// same forest road in Val Nambrone, two of them reporting a drive half an hour
// longer than the first card reported for the same roads.
//
// The map is derived from an OSM extract and a DEM and is not in git — see
// tools/refresh-region.sh — so this skips in a tree that has not built it.
func TestPresanellaOnTheRegionMap(t *testing.T) {
	const mapPath = "../../web/public/taa.json"
	if _, err := os.Stat(mapPath); err != nil {
		t.Skip("no web/public/taa.json in this tree: the region map is derived, see tools/refresh-region.sh")
	}
	reg, err := Load(mapPath)
	if err != nil {
		t.Fatalf("loading %s: %v", mapPath, err)
	}
	g := Build(reg)
	trento := Point{Lat: 46.06642, Lon: 11.12576}
	summit := Point{Lat: 46.21993, Lon: 10.66412}
	res := g.Route(Request{Points: []Point{trento, summit},
		Mode: "car+hike", Grade: "A", Alternatives: 3})
	if len(res.Routes) != 3 {
		t.Fatalf("%d routes to Cima Presanella (%s)", len(res.Routes), res.Reason)
	}
	for _, r := range res.Routes {
		if r.Parking == nil {
			t.Fatalf("%s left the car nowhere on a car+hike trip", r.ID)
		}
		// THE CLOCK IS THE LINE'S OWN. A drive is only ever as slow as the
		// roads it is on: what it reports may exceed the free-flow time of the
		// metres it covers by the ramp charges and by nothing else. A penalty
		// round put 79.5 km of Val Rendena at 109 minutes on the third card
		// while 72.6 km of the same roads took 75 on the first.
		for _, lg := range r.Legs {
			if lg.Mode != modeName[modeCar] {
				continue
			}
			free := 0.0
			for cls, m := range lg.Classes {
				free += m / city.Limit(cls)
			}
			if slack := lg.Seconds - free; slack > 10*rampCost {
				t.Errorf("%s drives %.1f km in a reported %.0f s where those roads run in %.0f at their own limits: %.0f s of penalty reached the clock",
					r.ID, lg.Meters/1000, lg.Seconds, free, slack)
			}
		}
		// And no card may be quicker than the best trip through its own car
		// park: ask for that parking as a stop and the answer is this trip or
		// a better one, never a worse one wearing this one's figures.
		stop := Point{Lat: r.Parking.Lat, Lon: r.Parking.Lon, Name: r.Parking.Name}
		alone := g.Route(Request{Points: []Point{trento, stop, summit}, Mode: "car+hike", Grade: "A"})
		if len(alone.Routes) != 1 {
			t.Errorf("%s: no route through its own parking (%s)", r.ID, alone.Reason)
			continue
		}
		if got := alone.Routes[0].Seconds; got > r.Seconds*1.01 {
			t.Errorf("%s reports %.0f s, and the best trip through its own parking takes %.0f: it is claiming a line it did not walk",
				r.ID, r.Seconds, got)
		}
	}
	// A MOUNTAIN HAS SIDES, and the side is the choice. Three cards that all
	// leave the car in the same valley are one answer drawn three times: at
	// least one alternative parks further than parkSpread from the trailhead
	// the fastest route chose.
	fx, fy := g.R.XY(res.Routes[0].Parking.Lat, res.Routes[0].Parking.Lon)
	far := 0.0
	for _, r := range res.Routes[1:] {
		x, y := g.R.XY(r.Parking.Lat, r.Parking.Lon)
		far = math.Max(far, math.Hypot(x-fx, y-fy))
	}
	if far <= parkSpread {
		t.Errorf("every alternative parks within %.1f km of the fastest one's trailhead: %v",
			far/1000, parkings(g, res.Routes))
	}
	// AND THE SIDE IS HOW YOU FINISH. Presanella is started from four valleys
	// and finished up two trails: 219 over the Vedretta d'Amola, which is what
	// the fastest way up Val Nambrone walks, and 220 over the Vedretta
	// Presanella, which is the trek the owner missed. One alternative must
	// come up that other way — none of the last three kilometres the fastest
	// route walked, and another trail under the summit.
	app := g.approachOf(res.Routes[0])
	other := -1
	for i, r := range res.Routes[1:] {
		shared := false
		for _, st := range r.walkSts {
			if app[st] {
				shared = true
				break
			}
		}
		if !shared && lastWay(r) != lastWay(res.Routes[0]) {
			other = i + 1
			break
		}
	}
	if other < 0 {
		t.Fatalf("every alternative finishes the way the fastest one does, up %q", lastWay(res.Routes[0]))
	}
	if got, want := lastWay(res.Routes[other]), "trail 220"; got != want {
		t.Errorf("%s comes up %q; the side the owner asked for is %q, over the Vedretta Presanella",
			res.Routes[other].ID, got, want)
	}
	if p := res.Routes[other].Parking; p.Lon >= 10.70 {
		t.Errorf("%s parks at %.5f,%.5f, east of the summit: that is Val Nambrone again",
			res.Routes[other].ID, p.Lat, p.Lon)
	}
}

// lastWay is the last named thing a route walks on: what it finishes by, and
// so which side of a summit it came up. The unnamed ground above the last
// trail is nobody's approach — every way up crosses it.
func lastWay(r *Route) string {
	for i := len(r.Steps) - 1; i >= 0; i-- {
		s := r.Steps[i]
		if s.Mode != "hike" {
			continue
		}
		switch s.Name {
		case "path", "track", "steps", "":
			continue
		}
		return s.Name
	}
	return ""
}

// approachRegion is a summit with two ridges to it and three car parks: two of
// them on opposite sides of the valley that both finish up the east ridge, and
// one round the back that finishes up the north. The far car park is nearer in
// the clock than the north one — so a rule that only asks for a DIFFERENT
// PLACE TO PARK offers the same day out twice, from two car parks eight
// kilometres apart, and never mentions the other ridge.
func approachRegion() *Region {
	r := &Region{Name: "approach", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{
		{0, 0},        // 0 the town
		{8000, 0},     // 1 the near car park
		{16000, 0},    // 2 the far one, on the same ridge
		{2000, 10000}, // 3 the one round the back
		{12000, 3000}, // 4 the foot of the east ridge
		{9000, 6000},  // 5 the foot of the north ridge
		{12000, 6000}, // 6 the summit
		{8000, -3000}, // 7 the bend on the road to the far car park
		{0, 10000},    // 8 and on the road round the back
	}
	add := func(cls, name string, ps ...int) {
		pts := make([][2]float64, len(ps))
		cum := make([]float64, len(ps))
		for i, p := range ps {
			pts[i] = r.Pts[p]
			if i > 0 {
				cum[i] = cum[i-1] + math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
			}
		}
		r.Stretches = append(r.Stretches, &city.Stretch{
			ID: name, Pts: pts, Cum: cum, Len: cum[len(ps)-1], A: ps[0], B: ps[len(ps)-1],
			Cls: cls, Name: name, MTB: -1,
		})
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
	}
	add("residential", "Strada della Val Bassa", 0, 1)
	add("residential", "Strada del Doss", 0, 7, 2)
	add("residential", "Strada del Passo", 0, 8, 3)
	add("path", "Sentiero delle Malghe", 1, 4)
	add("path", "Sentiero del Doss", 2, 4)
	add("path", "Sentiero 219", 4, 6)
	add("path", "Sentiero 220", 5, 6)
	add("path", "Sentiero del Versante Nord", 3, 5)
	return r
}

// TestAlternativesTryAnotherApproach is what the parking charge alone cannot
// buy. A card that ends the same way is the same side of the mountain whatever
// car park it started from, so the first penalty round charges approachFactor
// on the last approachM of the fastest route's walk. The answer that only
// moves the car is not thrown away — it is the third card, found by the
// stretch penalties in the round after, exactly as before.
func TestAlternativesTryAnotherApproach(t *testing.T) {
	g := Build(approachRegion())
	at := func(x, y float64) Point {
		lat, lon := g.R.LatLon(x, y)
		return Point{Lat: lat, Lon: lon}
	}
	res := g.Route(Request{Points: []Point{at(0, 0), at(12000, 6000)},
		Mode: "car+hike", Grade: "E", Alternatives: 3})
	if len(res.Routes) != 3 {
		t.Fatalf("%d routes (%s)", len(res.Routes), res.Reason)
	}
	want := []struct {
		park float64
		way  string
	}{
		{8000, "Sentiero 219"},  // the fastest, up the east ridge
		{2000, "Sentiero 220"},  // the other ridge, though its car park is further
		{16000, "Sentiero 219"}, // and only then the same ridge from the far car park
	}
	for i, w := range want {
		r := res.Routes[i]
		if x := parkedAt(g, r); math.Abs(x-w.park) > 500 {
			t.Errorf("%s parks at %.0f, want %.0f: %v", r.ID, x, w.park, parkings(g, res.Routes))
		}
		if got := lastWay(r); got != w.way {
			t.Errorf("%s finishes up %q, want %q", r.ID, got, w.way)
		}
	}
	// The approach the fastest route walked is the last approachM of it and no
	// more: the trail below it is penalised the ordinary way, which is how the
	// third card gets to use it again.
	app := g.approachOf(res.Routes[0])
	if len(app) != 1 || !app[5] {
		t.Errorf("the approach is %v, want the east ridge alone", app)
	}
	// And every card still reports the honest time of its own line: ask for
	// each one's parking as a stop and the clock is the same to the second.
	for _, r := range res.Routes {
		stop := Point{Lat: r.Parking.Lat, Lon: r.Parking.Lon, Name: "P"}
		alone := g.Route(Request{Points: []Point{at(0, 0), stop, at(12000, 6000)},
			Mode: "car+hike", Grade: "E"})
		if len(alone.Routes) != 1 {
			t.Fatalf("%s: no route through its own parking (%s)", r.ID, alone.Reason)
		}
		if got := alone.Routes[0].Seconds; math.Abs(got-r.Seconds) > 2 {
			t.Errorf("%s reports %.0f s for a trip that takes %.0f", r.ID, r.Seconds, got)
		}
	}
}
