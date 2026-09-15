package city

// Paths: the routes cars drive. Copied from the Trento traffic prototype
// (cmd/trento/paths.go), which is no longer in this repository.
//
// A path is an ordered list of (stretch, lane) steps from one end of a named
// road to the other. A car carries the index of its path and of the step it is
// on; at the end of a stretch it takes the next step instead of a random turn,
// and at the end of the path it disappears and a new car starts at the
// beginning. The partition is still the stretch, so none of the queue model
// changes: a path is just a rule for which partition a car is pushed into next.
//
// Paths are resolved from data/paths.json against the map, not drawn by hand:
// each entry names a road, and a shortest-path search over the allowed road
// classes joins its two extremities, preferring stretches that carry the name.
// That is what gets a route across the junctions where OpenStreetMap changes a
// road's name or splits it into carriageways.

import (
	"container/heap"
	"encoding/json"
	"fmt"
	"log"
	"math"
	"math/rand"
	"os"
	"sort"
)

type pathSpec struct {
	Road    string   `json:"road"`
	Classes []string `json:"classes"`
	Weight  float64  `json:"weight"`
}

// genSpec is the "generate" section of paths.json: trips across the city.
type genSpec struct {
	Trips      int     `json:"trips"`
	MinKm      float64 `json:"minKm"`
	MaxKm      float64 `json:"maxKm"`
	PerOrigin  int     `json:"perOrigin"`
	MaxOverlap float64 `json:"maxOverlap"`
	Weight     float64 `json:"weight"`
	Seed       int64   `json:"seed"`
}

// A Path is one route, with its geometry precomputed.
type Path struct {
	ID    int
	Name  string
	Road  string
	Kind  string // "road" for a named route, "trip" for a generated one
	Steps []Exit
	Len   float64
	// Cum[k] is the distance from the start of the path to the start of step
	// k. A car's position along its route is Cum[Step] + Pos, and that single
	// number is what the browser needs to draw it: it has the route's geometry.
	Cum    []float64
	Weight float64
	Color  string
}

// The palette paths are coloured with: saturated enough to read against white
// and yellow roads, and distinct from each other in pairs, because the two
// directions of one road sit next to each other on the map.
var pathColours = []string{
	"#e6194b", "#f58231", "#3cb44b", "#0082c8", "#911eb4", "#f032e6",
	"#46a3a3", "#d4a017", "#800000", "#008080", "#e6475f", "#1f4e9c",
	"#6a8f00", "#b5651d", "#5a3d99", "#c71585", "#2e8b57", "#cc5500",
	"#4169e1", "#8b4513",
}

// PathOf returns the path with that index, or the first one when the index is
// stale.
func (cm *Map) PathOf(id int) *Path {
	if id >= 0 && id < len(cm.Paths) {
		return cm.Paths[id]
	}
	return cm.Paths[0]
}

// ------------------------------------------------------------- dijkstra --

type pqItem struct {
	node int
	dist float64
}
type pqueue []pqItem

func (q pqueue) Len() int            { return len(q) }
func (q pqueue) Less(i, j int) bool  { return q[i].dist < q[j].dist }
func (q pqueue) Swap(i, j int)       { q[i], q[j] = q[j], q[i] }
func (q *pqueue) Push(x interface{}) { *q = append(*q, x.(pqItem)) }
func (q *pqueue) Pop() interface{} {
	old := *q
	it := old[len(old)-1]
	*q = old[:len(old)-1]
	return it
}

// shortest runs Dijkstra from start over the stretches allowed() accepts, with
// cost() as the weight of driving one, and returns the distance to every node
// reached and the step that arrived there.
func (cm *Map) shortest(start int, allowed func(*Stretch) bool, cost func(*Stretch) float64) (map[int]float64, map[int]Exit) {
	dist := map[int]float64{start: 0}
	prev := map[int]Exit{}
	q := &pqueue{{start, 0}}
	for q.Len() > 0 {
		it := heap.Pop(q).(pqItem)
		if it.dist > dist[it.node] {
			continue
		}
		for _, e := range cm.Out[it.node] {
			if !allowed(e.S) {
				continue
			}
			v := e.S.ExitNode(e.Lane)
			nd := it.dist + cost(e.S)
			if old, ok := dist[v]; !ok || nd < old {
				dist[v] = nd
				prev[v] = e
				heap.Push(q, pqItem{v, nd})
			}
		}
	}
	return dist, prev
}

// ---------------------------------------------------------------- resolve --

func loadPaths(file string, cm *Map) ([]*Path, error) {
	raw, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	var doc struct {
		Paths    []pathSpec `json:"paths"`
		Generate *genSpec   `json:"generate"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, fmt.Errorf("%s: %w", file, err)
	}

	var out []*Path
	for _, sp := range doc.Paths {
		for _, p := range cm.resolve(sp) {
			p.ID = len(out)
			p.Kind = "road"
			p.Color = pathColours[p.ID%len(pathColours)]
			out = append(out, p)
		}
	}
	if doc.Generate != nil && doc.Generate.Trips > 0 {
		for _, p := range cm.generateTrips(*doc.Generate) {
			p.ID = len(out)
			p.Kind = "trip"
			// Muted, and spread round the colour wheel by id: a few hundred
			// trips cannot each get a name-worthy colour, but neighbours should
			// still be told apart.
			p.Color = fmt.Sprintf("hsl(%d, 48%%, 46%%)", (p.ID*47)%360)
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%s: no path could be resolved on this map", file)
	}
	for _, p := range out {
		p.Cum = make([]float64, len(p.Steps))
		acc := 0.0
		for k, e := range p.Steps {
			p.Cum[k] = acc
			acc += e.S.Len
		}
	}
	return out, nil
}

// resolve turns one road into up to two directed paths along its long axis.
func (cm *Map) resolve(sp pathSpec) []*Path {
	allowedCls := map[string]bool{}
	for _, c := range sp.Classes {
		allowedCls[c] = true
	}
	inClass := func(s *Stretch) bool {
		return len(allowedCls) == 0 || allowedCls[s.Cls]
	}
	// The search may use any road a car can drive, but at a price: the named
	// road is cheap, another road of an allowed class costs four times as much,
	// and anything else twenty times. So a path stays on its road and its
	// class wherever it can, and still gets across the one short gap — a
	// roundabout, a slip road tagged "service" — that would otherwise make the
	// whole route impossible.
	allowed := func(s *Stretch) bool { return s.Cls != "pedestrian" }
	cost := func(s *Stretch) float64 {
		switch {
		case s.Name == sp.Road && inClass(s):
			return s.Len
		case inClass(s):
			return s.Len * 4
		default:
			return s.Len * 20
		}
	}

	// The road's own junctions, inside the map area. A way that runs off the
	// edge of the download still has nodes out there, and a path ending at one
	// of them would carry cars somewhere the map does not show.
	nodeSet := map[int]bool{}
	for _, s := range cm.Stretches {
		if s.Name != sp.Road || !inClass(s) {
			continue
		}
		for _, n := range []int{s.A, s.B} {
			if cm.inside(n) {
				nodeSet[n] = true
			}
		}
	}
	if len(nodeSet) < 2 {
		log.Printf("paths: %q is not on this map (or not in classes %v), skipped", sp.Road, sp.Classes)
		return nil
	}
	nodes := make([]int, 0, len(nodeSet))
	minX, maxX, minY, maxY := math.Inf(1), math.Inf(-1), math.Inf(1), math.Inf(-1)
	for n := range nodeSet {
		nodes = append(nodes, n)
		p := cm.Pts[n]
		minX, maxX = math.Min(minX, p[0]), math.Max(maxX, p[0])
		minY, maxY = math.Min(minY, p[1]), math.Max(maxY, p[1])
	}
	sort.Ints(nodes)

	// Which way the road runs decides the two directions. y grows southward.
	type dir struct {
		name   string
		vx, vy float64 // travel direction
	}
	var dirs []dir
	if maxY-minY >= maxX-minX {
		dirs = []dir{{"southbound", 0, 1}, {"northbound", 0, -1}}
	} else {
		dirs = []dir{{"eastbound", 1, 0}, {"westbound", -1, 0}}
	}
	span := math.Max(maxX-minX, maxY-minY)

	var out []*Path
	for _, d := range dirs {
		score := func(n int) float64 { p := cm.Pts[n]; return p[0]*d.vx + p[1]*d.vy }

		// Start near the back of the road in the direction of travel, end near
		// the front. A handful of candidates at each end, because on a divided
		// road the most extreme node may belong to the other carriageway.
		sorted := append([]int(nil), nodes...)
		sort.Slice(sorted, func(i, j int) bool { return score(sorted[i]) < score(sorted[j]) })
		k := 12
		if k > len(sorted) {
			k = len(sorted)
		}
		starts := sorted[:k]
		ends := sorted[len(sorted)-k:]

		bestProgress, bestDist := 0.0, math.Inf(1)
		var bestSteps []Exit
		for _, st := range starts {
			dist, prev := cm.shortest(st, allowed, cost)
			for _, en := range ends {
				dd, ok := dist[en]
				if !ok || en == st {
					continue
				}
				progress := score(en) - score(st)
				if progress > bestProgress+1 || (math.Abs(progress-bestProgress) <= 1 && dd < bestDist) {
					steps := walkBack(prev, st, en)
					if len(steps) == 0 {
						continue
					}
					bestProgress, bestDist, bestSteps = progress, dd, steps
				}
			}
		}
		// A path that covers less than a third of the road is not the road:
		// usually a one-way that cannot be driven in this direction at all.
		if len(bestSteps) == 0 || bestProgress < span/3 {
			log.Printf("paths: %s %s could not be driven end to end, skipped", sp.Road, d.name)
			continue
		}
		total := 0.0
		for _, e := range bestSteps {
			total += e.S.Len
		}
		out = append(out, &Path{
			Name:   sp.Road + " " + d.name,
			Road:   sp.Road,
			Steps:  bestSteps,
			Len:    total,
			Weight: sp.Weight / 2,
		})
		log.Printf("paths: %-50s %4d stretches, %5.1f km", sp.Road+" "+d.name, len(bestSteps), total/1000)
	}
	return out
}

// walkBack rebuilds the steps from start to end out of Dijkstra's prev map.
func walkBack(prev map[int]Exit, start, end int) []Exit {
	var rev []Exit
	n := end
	for guard := 0; n != start && guard < 100000; guard++ {
		e, ok := prev[n]
		if !ok {
			return nil
		}
		rev = append(rev, e)
		n = e.S.EntryNode(e.Lane)
	}
	if n != start {
		return nil
	}
	steps := make([]Exit, len(rev))
	for i := range rev {
		steps[i] = rev[len(rev)-1-i]
	}
	return steps
}

// PickPath chooses a path for a new car, in proportion to the paths' weights.
func PickPath(paths []*Path, r float64) *Path {
	total := 0.0
	for _, p := range paths {
		total += p.Weight
	}
	x := r * total
	for _, p := range paths {
		x -= p.Weight
		if x < 0 {
			return p
		}
	}
	return paths[len(paths)-1]
}

// spacing is the road a moving car needs to itself, in metres: roughly a
// two-second gap at that class's speed limit. It is what turns a route's length
// into the number of cars it can carry without turning into a car park.
func spacing(cls string) float64 {
	switch cls {
	case "motorway", "trunk":
		return 45
	case "primary", "secondary":
		return 28
	default:
		return 22
	}
}

// Capacity is how many cars a path carries at free flow.
func (p *Path) Capacity() int {
	c := 0.0
	for _, e := range p.Steps {
		c += e.S.Len / spacing(e.S.Cls)
	}
	return int(c)
}

// --------------------------------------------------------------- trips --

// speedOf is the travel speed a trip is routed by, in m/s. Service lanes are
// slow on purpose: a trip may use one to reach its destination, but should not
// cut through a car park to save a corner.
func speedOf(cls string) float64 {
	switch cls {
	case "motorway":
		return 27.7
	case "trunk":
		return 22
	case "primary":
		return 16.6
	case "secondary":
		return 14
	case "tertiary":
		return 12
	case "residential":
		return 8.3
	default:
		return 4
	}
}

// generateTrips draws trips across the city: an origin and a destination at
// junctions of city streets, a fastest route between them, and a few checks
// that keep only trips worth driving.
//
// Deterministic for a given seed and map, so a restart produces the same trips
// and the cars already in the queue are still on the paths they think they are.
func (cm *Map) generateTrips(g genSpec) []*Path {
	if g.PerOrigin <= 0 {
		g.PerOrigin = 3
	}
	if g.MaxOverlap <= 0 {
		g.MaxOverlap = 0.5
	}
	if g.Weight <= 0 {
		g.Weight = 1
	}
	cityCls := map[string]bool{"primary": true, "secondary": true, "tertiary": true, "residential": true}

	// Endpoints: junctions of city streets inside the downloaded area, in a
	// stable order so the random draws below are reproducible.
	set := map[int]bool{}
	for _, s := range cm.Stretches {
		if !cityCls[s.Cls] {
			continue
		}
		for _, n := range []int{s.A, s.B} {
			if cm.inside(n) && len(cm.Out[n]) > 1 {
				set[n] = true
			}
		}
	}
	nodes := make([]int, 0, len(set))
	for n := range set {
		nodes = append(nodes, n)
	}
	sort.Ints(nodes)
	if len(nodes) < 2 {
		log.Printf("trips: no city junctions on this map")
		return nil
	}

	allowed := func(s *Stretch) bool { return s.Cls != "pedestrian" }
	cost := func(s *Stretch) float64 { return s.Len / speedOf(s.Cls) }

	rng := rand.New(rand.NewSource(g.Seed))
	var trips []*Path
	var sets []map[string]bool
	tries := 0
	for len(trips) < g.Trips && tries < g.Trips*25 {
		tries++
		o := nodes[rng.Intn(len(nodes))]
		dist, prev := cm.shortest(o, allowed, cost)

		added := 0
		for k := 0; k < g.PerOrigin*12 && added < g.PerOrigin && len(trips) < g.Trips; k++ {
			d := nodes[rng.Intn(len(nodes))]
			if _, ok := dist[d]; !ok || d == o {
				continue
			}
			straight := math.Hypot(cm.Pts[d][0]-cm.Pts[o][0], cm.Pts[d][1]-cm.Pts[o][1])
			if straight < g.MinKm*1000 || straight > g.MaxKm*1000 {
				continue
			}
			steps := walkBack(prev, o, d)
			if len(steps) < 3 {
				continue
			}

			// Mostly inside the map: a fastest route that leaves the area to
			// take the motorway round is a real route, but not one anybody
			// watching the city can see.
			total, in := 0.0, 0.0
			ids := map[string]bool{}
			for _, e := range steps {
				total += e.S.Len
				if cm.inside(e.S.A) && cm.inside(e.S.B) {
					in += e.S.Len
				}
				ids[e.S.ID] = true
			}
			if in < total*0.9 || total > g.MaxKm*1000*1.6 {
				continue
			}

			// Not a near-duplicate of a trip already chosen.
			dup := false
			for _, other := range sets {
				shared := 0
				for id := range ids {
					if other[id] {
						shared++
					}
				}
				small := len(ids)
				if len(other) < small {
					small = len(other)
				}
				if small > 0 && float64(shared)/float64(small) > g.MaxOverlap {
					dup = true
					break
				}
			}
			if dup {
				continue
			}

			trips = append(trips, &Path{
				Name:   tripName(steps),
				Steps:  steps,
				Len:    total,
				Weight: g.Weight,
			})
			sets = append(sets, ids)
			added++
		}
	}
	km := 0.0
	for _, t := range trips {
		km += t.Len
	}
	log.Printf("trips: %d generated from %d origins, %.0f km of routes", len(trips), tries, km/1000)
	return trips
}

// tripName is "from street -> to street", from the first and last named steps.
func tripName(steps []Exit) string {
	from, to := "", ""
	for _, e := range steps {
		if e.S.Name != "" {
			from = e.S.Name
			break
		}
	}
	for i := len(steps) - 1; i >= 0; i-- {
		if steps[i].S.Name != "" {
			to = steps[i].S.Name
			break
		}
	}
	switch {
	case from == "" && to == "":
		return "City trip"
	case from == to || to == "":
		return from
	case from == "":
		return to
	default:
		return from + " → " + to
	}
}

// sharedCapacity is how many cars a path can carry at free flow, when every
// lane of every street it uses is split evenly among all the paths that use
// that lane. Counting each path's streets in full overstates what the city
// holds wherever routes overlap, and generated trips overlap on every arterial.
func sharedCapacity(paths []*Path) map[int]int {
	use := map[string]int{}
	for _, p := range paths {
		seen := map[string]bool{}
		for _, e := range p.Steps {
			k := fmt.Sprintf("%s:%d", e.S.ID, e.Lane)
			if !seen[k] {
				seen[k] = true
				use[k]++
			}
		}
	}
	out := make(map[int]int, len(paths))
	for _, p := range paths {
		c := 0.0
		for _, e := range p.Steps {
			k := fmt.Sprintf("%s:%d", e.S.ID, e.Lane)
			c += e.S.Len / spacing(e.S.Cls) / float64(use[k])
		}
		out[p.ID] = int(c)
	}
	return out
}
