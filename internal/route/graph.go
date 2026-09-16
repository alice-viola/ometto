package route

// The graph: one adjacency per mode over dense node ids, packed as CSR
// (offsets + one flat edge array), plus the union of every stretch for naming
// and snapping, the connected components per mode, and a uniform grid so a
// click finds its junction without scanning the region.
//
// Why CSR and not cmd/pathfinder's map[int][]edge: the region build is 365k
// ways over 2.6M points. A map per mode costs hundreds of megabytes and a
// cache miss per neighbour; the flat array costs 16 bytes per directed edge
// and reads in order.

import (
	"math"
	"sort"
	"strconv"
	"strings"
	"sync"

	"ometto/internal/city"
)

// cedge is one directed edge: the dense node it arrives at, the stretch it
// runs along, the seconds it costs in that direction, and whether the stretch
// is travelled B -> A.
type cedge struct {
	to  int32
	st  int32
	w   float32
	rev bool
}

type csr struct {
	off []int32 // len = nodes+1
	e   []cedge
}

func (c *csr) at(n int32) []cedge { return c.e[c.off[n]:c.off[n+1]] }

// Graph is a region ready to be searched.
type Graph struct {
	R *Region

	dense []int32 // point index -> dense node id, -1 when the point is no junction
	pt    []int32 // dense node id -> point index

	adj   [nModes]csr
	uni   csr             // every stretch, both ways: naming and snapping
	indeg [nModes][]int32 // stretches leading INTO a junction: a destination needs one
	comp  [nModes][]int32
	main  [nModes]int32
	scc   [nModes][]bool // the largest STRONGLY connected component, where one-ways make one
	weakN [nModes]int
	SccN  [nModes]int

	// The lifts and the short walks that join them to the footpaths. They are
	// a layer of their own, consulted only when a request asks for lifts, so
	// that nothing else — snapping, components, a plain walk — can see them.
	lift        csr
	liftSecs    []liftSec // every section, its two stations sorted by height
	LiftsLoaded int
	LiftsLinked int

	// the snapping grid
	cell               float64
	gw, gh             int
	gridOff            []int32
	gridN              []int32
	minX, minY         float64
	Junctions          int
	DirectedEdges      int
	CarN, BikeN, HikeN int

	// the refusal cache: see search.go
	rmu      sync.Mutex
	refusals map[string]refusal

	// named points that can lend their name to a parking (SetParkPlaces)
	parkRefs []placeRef
	parkGrid *placeGrid
}

// ParkPlace is a named point that may lend its name to a parking: a car park,
// a locality, the name on the sign at the end of the road.
type ParkPlace struct {
	Name     string
	Lat, Lon float64
}

// SetParkPlaces gives the graph the names it may call a parking by. The
// geocoder calls it with the places and car parks it indexed; without it the
// naming simply falls through to the ways.
func (g *Graph) SetParkPlaces(ps []ParkPlace) {
	refs := make([]placeRef, 0, len(ps))
	for _, p := range ps {
		if p.Name == "" {
			continue
		}
		x, y := g.R.XY(p.Lat, p.Lon)
		refs = append(refs, placeRef{x: x, y: y, name: p.Name})
	}
	if len(refs) == 0 {
		g.parkRefs, g.parkGrid = nil, nil
		return
	}
	g.parkRefs = refs
	g.parkGrid = newPlaceGrid(refs, 500)
}

// ParkName is what to call the place where a two-mode trip leaves the car,
// when the switch happens inside a leg rather than at a stop the caller named.
//
// In order: a parking or a place within 100 m of the junction; the nearest
// named road the FIRST mode can actually drive, ten hops or a kilometre along
// that mode's own network, because the last stretch into a car park is usually
// an unnamed service road; the junction's own name; and, when that is only a
// trail number, the place it stands in, out to two kilometres. A name that is
// only a trail number is not a name for a parking: "parked at trail 382" is
// true and useless, the car is not on the trail.
func (g *Graph) ParkName(n int32, mode int) string {
	x, y := g.XY(n)
	if g.parkGrid != nil {
		if nm := g.parkGrid.nearestXY(g.parkRefs, x, y, 100, 0); nm != "" {
			return nm
		}
	}
	type step struct {
		n int32
		d float64
	}
	seen := map[int32]bool{n: true}
	frontier := []step{{n, 0}}
	for hop := 0; hop < 10 && len(frontier) > 0; hop++ {
		var next []step
		for _, v := range frontier {
			for _, e := range g.adj[mode].at(v.n) {
				st := g.R.Stretches[e.st]
				d := v.d + st.Len
				if st.Name != "" && !trailNumber(st.Name) {
					return st.Name
				}
				if !seen[e.to] && d <= 1000 {
					seen[e.to] = true
					next = append(next, step{e.to, d})
				}
			}
		}
		frontier = next
	}
	// Whatever the junction calls itself, in the vocabulary of the mode that
	// drove there — so never a trail number, and the road's own ref when it
	// has one — unless that is only a number, in which case the name of the
	// place it stands in is better: "parked at Pian Venezia", not "parked at
	// trail 102". The car park at a trailhead is often a kilometre from the
	// last named thing, and a class word ("service road") is not a name.
	if nm := g.nameNear(n, mode); nm != "" && !trailNumber(nm) {
		return nm
	}
	if g.parkGrid != nil {
		if nm := g.parkGrid.nearestXY(g.parkRefs, x, y, 2000, 0); nm != "" {
			return nm
		}
	}
	// Only the junction's own name is left and it is a trail number, which is
	// the one thing this must not answer: the road the car is on carries the
	// trail's ref for a while, the car park is not the trail. No name at all
	// is better — the answer still carries the coordinates.
	return ""
}

// trailNumber says a name is not a place a car is left.
//
// Two shapes qualify. The first is the SYNTHETIC one: "trail <ref>" is what
// NameAt builds for an unnamed way that carries a route ref, and the ref can
// be anything the mapper wrote — 102, 358A, SF, E401A, O3 — so the prefix is
// what decides, never the shape of what follows it. The second is a name that
// is only a number, with or without its club's initials: "102", "SAT 102".
// Both name a line on a map; neither names a car park.
func trailNumber(name string) bool {
	if strings.HasPrefix(strings.ToLower(strings.TrimSpace(name)), "trail ") {
		return true
	}
	f := strings.Fields(strings.ToLower(name))
	if len(f) == 0 || len(f) > 2 {
		return false
	}
	last := f[len(f)-1]
	if len(f) == 2 {
		switch f[0] {
		case "trail", "sat", "sentiero", "cai", "avs", "weg":
		default:
			return false
		}
	}
	digits := false
	for i, r := range last {
		switch {
		case r >= '0' && r <= '9':
			digits = true
		case i == len(last)-1 && ((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')):
		default:
			return false
		}
	}
	return digits
}

// Build makes the graph of a region.
func Build(r *Region) *Graph {
	g := &Graph{R: r}
	g.dense = make([]int32, len(r.Pts))
	for i := range g.dense {
		g.dense[i] = -1
	}
	add := func(p int) int32 {
		if p < 0 || p >= len(g.dense) {
			return -1
		}
		if g.dense[p] < 0 {
			g.dense[p] = int32(len(g.pt))
			g.pt = append(g.pt, int32(p))
		}
		return g.dense[p]
	}
	for _, st := range r.Stretches {
		if st.Len <= 0 {
			continue
		}
		add(st.A)
		add(st.B)
	}
	n := int32(len(g.pt))
	g.Junctions = int(n)

	// One pass per mode: count the directed edges of every node, then fill.
	build := func(want func(st *city.Stretch) (ok bool, both bool)) csr {
		deg := make([]int32, n+1)
		for _, st := range r.Stretches {
			if st.Len <= 0 {
				continue
			}
			ok, both := want(st)
			if !ok {
				continue
			}
			a, b := g.dense[st.A], g.dense[st.B]
			if a < 0 || b < 0 {
				continue
			}
			deg[a]++
			if both {
				deg[b]++
			}
		}
		off := make([]int32, n+1)
		var acc int32
		for i := int32(0); i < n; i++ {
			off[i] = acc
			acc += deg[i]
		}
		off[n] = acc
		e := make([]cedge, acc)
		fill := append([]int32(nil), off[:n]...)
		for si, st := range r.Stretches {
			if st.Len <= 0 {
				continue
			}
			ok, both := want(st)
			if !ok {
				continue
			}
			a, b := g.dense[st.A], g.dense[st.B]
			if a < 0 || b < 0 {
				continue
			}
			_ = si
			e[fill[a]] = cedge{to: b, st: int32(si), rev: false}
			fill[a]++
			if both {
				e[fill[b]] = cedge{to: a, st: int32(si), rev: true}
				fill[b]++
			}
		}
		return csr{off: off, e: e}
	}

	for m := 0; m < nModes; m++ {
		mode := m
		g.adj[mode] = build(func(st *city.Stretch) (bool, bool) {
			if !usable(mode, st) {
				return false, false
			}
			// a one-way is for wheels; feet go both ways
			return true, !st.One || mode == modeHike
		})
		g.indeg[mode] = make([]int32, n)
		for i := range g.adj[mode].e {
			e := &g.adj[mode].e[i]
			st := r.Stretches[e.st]
			marked := st.HikeRef != "" || (int(e.st) < len(r.Sat) && r.Sat[e.st].No != "")
			e.w = float32(weight(mode, st, e.rev, marked))
			g.indeg[mode][e.to]++
		}
		g.DirectedEdges += len(g.adj[mode].e)
	}
	g.uni = build(func(st *city.Stretch) (bool, bool) { return st.Cls != "lift", true })
	for i := range g.uni.e {
		e := &g.uni.e[i]
		e.w = float32(r.Stretches[e.st].Len)
	}

	g.components()
	g.strong()
	g.buildGrid()
	g.buildLifts()
	for i := int32(0); i < n; i++ {
		if len(g.adj[modeCar].at(i)) > 0 {
			g.CarN++
		}
		if len(g.adj[modeBike].at(i)) > 0 {
			g.BikeN++
		}
		if len(g.adj[modeHike].at(i)) > 0 {
			g.HikeN++
		}
	}
	return g
}

// buildLifts puts every aerialway in a layer of its own, and joins each of its
// ends to the nearest walking junction within 100 m by a link — a short real
// walk, given its own synthetic stretch so the answer can draw it and count
// its metres. A lift whose end finds nothing to link to is loaded and
// unreachable; the count says how many.
func (g *Graph) buildLifts() {
	r := g.R
	if r.Lifts == 0 {
		return
	}
	type edge struct {
		from, to int32
		st       int32
		w        float32
		rev      bool
	}
	var edges []edge
	linked := 0
	// The link's own stretch: two points, walked at the pace of a path.
	link := func(liftNode, walkNode int32, d float64) int32 {
		pa, pb := g.pt[liftNode], g.pt[walkNode]
		pts := [][2]float64{r.Pts[pa], r.Pts[pb]}
		st := &city.Stretch{
			ID: "link" + strconv.Itoa(len(r.Stretches)), Pts: pts, Cum: []float64{0, d},
			Len: d, A: int(pa), B: int(pb), Cls: "path", MTB: -1, FootAccess: 1,
		}
		if len(r.PtZ) == len(r.Pts) {
			st.Z = []float64{r.PtZ[pa], r.PtZ[pb]}
			if up := st.Z[1] - st.Z[0]; up > 0 {
				st.Up = up
			} else {
				st.Down = -up
			}
		}
		r.Stretches = append(r.Stretches, st)
		r.Sat = append(r.Sat, Sat{})
		r.Lift = append(r.Lift, Lift{})
		r.Links++
		return int32(len(r.Stretches) - 1)
	}

	for si := 0; si < len(r.Lift) && si < len(r.Stretches); si++ {
		lf := r.Lift[si]
		if !lf.Is {
			continue
		}
		st := r.Stretches[si]
		if st.Len <= 0 {
			continue
		}
		a, b := g.dense[st.A], g.dense[st.B]
		if a < 0 || b < 0 {
			continue
		}
		g.LiftsLoaded++
		// The ride: its published duration, or its length at the speed of its
		// kind, plus the boarding every ride pays.
		ride := g.liftCost(int32(si))
		up := true // A -> B is the way up, unless the elevations say otherwise
		if len(st.Z) >= 2 {
			up = st.Z[len(st.Z)-1] >= st.Z[0]
		}
		if !lf.Uphill || up {
			edges = append(edges, edge{a, b, int32(si), float32(ride), false})
		}
		if !lf.Uphill || !up {
			edges = append(edges, edge{b, a, int32(si), float32(ride), true})
		}
		// Which end is the valley station, for the chain: a section whose map
		// carries no elevation has no downhill end and is left out of it.
		if len(st.Z) >= 2 {
			low, high := a, b
			if !up {
				low, high = b, a
			}
			g.liftSecs = append(g.liftSecs, liftSec{si: int32(si), low: low, high: high})
		}
		// The two links.
		ends := 0
		for _, n := range []int32{a, b} {
			x, y := g.XY(n)
			w, d := g.NearestD(x, y, "to", []int{modeHike}, 100)
			if w < 0 || d > 100 || w == n {
				continue
			}
			ls := link(n, w, d)
			lw := float32(weight(modeHike, r.Stretches[ls], false, false))
			edges = append(edges, edge{n, w, ls, lw, false})
			edges = append(edges, edge{w, n, ls, lw, true})
			ends++
		}
		if ends == 2 {
			linked++
		}
	}
	g.LiftsLinked = linked

	n := int32(len(g.pt))
	deg := make([]int32, n+1)
	for _, e := range edges {
		deg[e.from]++
	}
	off := make([]int32, n+1)
	var acc int32
	for i := int32(0); i < n; i++ {
		off[i] = acc
		acc += deg[i]
	}
	off[n] = acc
	out := csr{off: off, e: make([]cedge, acc)}
	fill := append([]int32(nil), off[:n]...)
	for _, e := range edges {
		out.e[fill[e.from]] = cedge{to: e.to, st: e.st, w: e.w, rev: e.rev}
		fill[e.from]++
	}
	g.lift = out
}

// liftCost is the seconds a ride costs: the published duration when there is
// one, else its length at the speed of its kind, plus the boarding every ride
// pays whatever it is.
func (g *Graph) liftCost(si int32) float64 {
	lf := g.R.Lift[si]
	ride := lf.Dur
	if ride <= 0 {
		ride = g.R.Stretches[si].Len / liftSpeed(lf.Type)
	}
	return ride + liftBoardS
}

// liftSec is one section of an aerialway as the chain reads it: the stretch and
// its two stations, the valley one first.
type liftSec struct {
	si        int32
	low, high int32
}

// chainGap is how far apart the two ends of consecutive sections may be and
// still be the same place. A lift system is mapped section by section, and
// where one ends and the next begins is usually two OSM nodes a few dozen
// metres apart — two buildings, or the two ends of a walk across a terrace —
// never the same node.
const chainGap = 250.0

// liftChainBottom is the valley station of the chain a station stands in, or -1
// when the station is already the bottom of it (or the map has no elevations to
// tell). It follows the sections downhill: the section whose UPPER station is
// this one, then the section below that, as far as they go.
//
// This is what the "multiple tracks of lift" rule needs to know. A gondola to a
// mid station and a chairlift on to the top are two sections of one lift, and a
// road that reaches the middle of the chain makes riding only the upper half
// the fastest plan — which is not the day out a person who asked for lifts had
// in mind. A chain in the region file is a handful of sections, so the hops are
// counted and the stations already visited are remembered: a map that loops one
// section back onto another cannot make this walk forever.
func (g *Graph) liftChainBottom(n int32) int32 {
	z, ok := g.nodeZ(n)
	if !ok {
		return -1
	}
	cur, curZ := n, z
	seen := map[int32]bool{n: true}
	for hop := 0; hop < 8; hop++ {
		x, y := g.XY(cur)
		best, bestZ := int32(-1), curZ
		for _, s := range g.liftSecs {
			if s.high != cur {
				hx, hy := g.XY(s.high)
				if math.Hypot(hx-x, hy-y) > chainGap {
					continue
				}
			}
			lz, ok := g.nodeZ(s.low)
			if !ok || seen[s.low] || lz >= bestZ {
				continue
			}
			best, bestZ = s.low, lz
		}
		if best < 0 {
			break
		}
		seen[best] = true
		cur, curZ = best, bestZ
	}
	if cur == n {
		return -1
	}
	return cur
}

// nodeZ is what a junction stands at, when the map carries elevations.
func (g *Graph) nodeZ(n int32) (float64, bool) {
	if len(g.R.PtZ) != len(g.R.Pts) {
		return 0, false
	}
	return g.R.PtZ[g.pt[n]], true
}

// LiftNear is the lift line within maxM of a point, nearest first: a via
// dropped on a cable car means "ride this one".
func (g *Graph) LiftNear(x, y, maxM float64) (int32, bool) {
	best, bestD := int32(-1), maxM
	for si := range g.R.Lift {
		if !g.R.Lift[si].Is {
			continue
		}
		if d := pointToLine(g.R.Stretches[si].Pts, x, y); d < bestD {
			best, bestD = int32(si), d
		}
	}
	return best, best >= 0
}

// Ends are a lift's two junctions and which one is uphill.
func (g *Graph) LiftEnds(si int32) (a, b int32, uphillIsB, oneway bool) {
	st := g.R.Stretches[si]
	a, b = g.dense[st.A], g.dense[st.B]
	uphillIsB = true
	if len(st.Z) >= 2 {
		uphillIsB = st.Z[len(st.Z)-1] >= st.Z[0]
	}
	return a, b, uphillIsB, g.R.Lift[si].Uphill
}

// LiftRide is the edge of a forced ride: the stretch, whether it is travelled
// B -> A, and what it costs.
func (g *Graph) LiftRide(si int32, from int32) (rev bool, w float64, ok bool) {
	st := g.R.Stretches[si]
	a, b := g.dense[st.A], g.dense[st.B]
	switch from {
	case a:
		return false, g.liftCost(si), true
	case b:
		return true, g.liftCost(si), true
	}
	return false, 0, false
}

// Avoid resolves a point to the one way a person meant to strike out: the
// nearest usable way within maxM. Ways are found through the junctions around
// the point, so a way whose ends are both more than a kilometre and a half
// away — a long tunnel, a motorway viaduct — is out of reach of a click in
// its middle; nothing else is.
func (g *Graph) Avoid(x, y, maxM float64, plan []int, lifts bool) (int32, bool) {
	cand := map[int32]bool{}
	cx := int((x - g.minX) / g.cell)
	cy := int((y - g.minY) / g.cell)
	rings := int(1500/g.cell) + 1
	for ring := 0; ring <= rings; ring++ {
		for gy := cy - ring; gy <= cy+ring; gy++ {
			if gy < 0 || gy >= g.gh {
				continue
			}
			for gx := cx - ring; gx <= cx+ring; gx++ {
				if gx < 0 || gx >= g.gw {
					continue
				}
				if ring > 0 && gx != cx-ring && gx != cx+ring && gy != cy-ring && gy != cy+ring {
					continue
				}
				c := gy*g.gw + gx
				for _, n := range g.gridN[g.gridOff[c]:g.gridOff[c+1]] {
					for _, e := range g.uni.at(n) {
						cand[e.st] = true
					}
					if lifts {
						for _, e := range g.lift.at(n) {
							cand[e.st] = true
						}
					}
				}
			}
		}
	}
	best, bestD := int32(-1), maxM
	for si := range cand {
		st := g.R.Stretches[si]
		ok := false
		for _, m := range plan {
			if usable(m, st) {
				ok = true
				break
			}
		}
		if !ok && lifts && int(si) < len(g.R.Lift) && g.R.Lift[si].Is {
			ok = true
		}
		if !ok {
			continue
		}
		if d := pointToLine(st.Pts, x, y); d < bestD {
			best, bestD = si, d
		}
	}
	return best, best >= 0
}

// pointToLine is the metres from a point to a polyline.
func pointToLine(pts [][2]float64, x, y float64) float64 {
	best := math.Inf(1)
	for i := 1; i < len(pts); i++ {
		ax, ay := pts[i-1][0], pts[i-1][1]
		bx, by := pts[i][0], pts[i][1]
		dx, dy := bx-ax, by-ay
		l2 := dx*dx + dy*dy
		t := 0.0
		if l2 > 0 {
			t = ((x-ax)*dx + (y-ay)*dy) / l2
			t = math.Max(0, math.Min(1, t))
		}
		if d := math.Hypot(x-(ax+t*dx), y-(ay+t*dy)); d < best {
			best = d
		}
	}
	if len(pts) == 1 {
		best = math.Hypot(x-pts[0][0], y-pts[0][1])
	}
	return best
}

// components labels every junction with its weakly connected component per
// mode, so a click never snaps to a stub cut off from the network (a service
// road inside a pedestrian zone, a trail cut by the extract's edge). A
// one-way makes the walk directional, so connectivity is computed on the
// UNDIRECTED graph, by union-find.
func (g *Graph) components() {
	n := int32(len(g.pt))
	for m := 0; m < nModes; m++ {
		parent := make([]int32, n)
		for i := range parent {
			parent[i] = int32(i)
		}
		var find func(int32) int32
		find = func(x int32) int32 {
			for parent[x] != x {
				parent[x] = parent[parent[x]]
				x = parent[x]
			}
			return x
		}
		for a := int32(0); a < n; a++ {
			for _, e := range g.adj[m].at(a) {
				ra, rb := find(a), find(e.to)
				if ra != rb {
					parent[ra] = rb
				}
			}
		}
		size := map[int32]int{}
		comp := make([]int32, n)
		best, bestSize := int32(-1), 0
		for i := int32(0); i < n; i++ {
			r := find(i)
			comp[i] = r
			if len(g.adj[m].at(i)) == 0 {
				continue // an isolated point is in no component of this mode
			}
			size[r]++
			if size[r] > bestSize {
				best, bestSize = r, size[r]
			}
		}
		g.comp[m], g.main[m] = comp, best
		g.weakN[m] = bestSize
	}
}

// strong keeps, for the modes that drive, the largest STRONGLY connected
// component: the junctions you can both reach and leave.
//
// A weak component is not enough where there are one-ways. Nine of twenty-five
// points on a grid around Trento cathedral snapped into a one-way pocket — a
// junction inside the pedestrian ring that a car can enter and never leave, or
// leave and never enter — and the route was refused with "no route" as if the
// map were broken. The SCC is computed the cheap way: the junctions reachable
// FROM the busiest junction of the main component, intersected with those that
// reach it. In a road network that hub is in the middle of everything, so its
// SCC is the largest one; if it somehow came out small, the weak component is
// kept rather than refusing every click.
func (g *Graph) strong() {
	n := int32(len(g.pt))
	for _, m := range []int{modeCar, modeBike} {
		hub, bestDeg := int32(-1), -1
		for i := int32(0); i < n; i++ {
			if g.comp[m][i] != g.main[m] {
				continue
			}
			if d := len(g.adj[m].at(i)); d > bestDeg {
				bestDeg, hub = d, i
			}
		}
		if hub < 0 {
			continue
		}
		rev := g.reverse(m)
		fwd := reach(g.adj[m], hub, n)
		bwd := reach(rev, hub, n)
		in := make([]bool, n)
		cnt := 0
		for i := int32(0); i < n; i++ {
			if fwd[i] && bwd[i] {
				in[i] = true
				cnt++
			}
		}
		g.SccN[m] = cnt
		if cnt*4 < g.weakN[m] {
			continue // implausible: keep the weak component
		}
		g.scc[m] = in
	}
}

// reverse is the mode's adjacency with every edge turned around. Only the
// endpoint matters (it exists for reachability), so the weights are not copied.
func (g *Graph) reverse(m int) csr {
	n := int32(len(g.pt))
	deg := make([]int32, n+1)
	for _, e := range g.adj[m].e {
		deg[e.to]++
	}
	off := make([]int32, n+1)
	var acc int32
	for i := int32(0); i < n; i++ {
		off[i] = acc
		acc += deg[i]
	}
	off[n] = acc
	out := csr{off: off, e: make([]cedge, acc)}
	fill := append([]int32(nil), off[:n]...)
	for a := int32(0); a < n; a++ {
		for _, e := range g.adj[m].at(a) {
			out.e[fill[e.to]] = cedge{to: a, st: e.st, w: e.w, rev: e.rev}
			fill[e.to]++
		}
	}
	return out
}

// reach is the set of junctions a walk over c can get to from start.
func reach(c csr, start, n int32) []bool {
	seen := make([]bool, n)
	seen[start] = true
	stack := []int32{start}
	for len(stack) > 0 {
		v := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		for _, e := range c.at(v) {
			if !seen[e.to] {
				seen[e.to] = true
				stack = append(stack, e.to)
			}
		}
	}
	return seen
}

// connected says a junction is one the mode can both reach and leave: the
// strongly connected component where the mode has one-ways, the weakly
// connected one where it does not (on foot every stretch goes both ways).
func (g *Graph) connected(m int, n int32) bool {
	if g.scc[m] != nil {
		return g.scc[m][n]
	}
	return len(g.adj[m].at(n)) > 0 && g.comp[m][n] == g.main[m]
}

// XY of a dense node.
func (g *Graph) XY(n int32) (float64, float64) {
	p := g.R.Pts[g.pt[n]]
	return p[0], p[1]
}

// LatLon of a dense node.
func (g *Graph) LatLon(n int32) (float64, float64) {
	x, y := g.XY(n)
	return g.R.LatLon(x, y)
}

// ------------------------------------------------------------------ snapping

func (g *Graph) buildGrid() {
	g.cell = 500
	e := g.R.Extent
	g.minX, g.minY = e.MinX, e.MinY
	if e.MaxX <= e.MinX || e.MaxY <= e.MinY { // a map without a declared extent
		g.minX, g.minY = math.Inf(1), math.Inf(1)
		maxX, maxY := math.Inf(-1), math.Inf(-1)
		for _, p := range g.pt {
			q := g.R.Pts[p]
			g.minX, g.minY = math.Min(g.minX, q[0]), math.Min(g.minY, q[1])
			maxX, maxY = math.Max(maxX, q[0]), math.Max(maxY, q[1])
		}
		e.MinX, e.MinY, e.MaxX, e.MaxY = g.minX, g.minY, maxX, maxY
	}
	g.gw = int((e.MaxX-e.MinX)/g.cell) + 2
	g.gh = int((e.MaxY-e.MinY)/g.cell) + 2
	if g.gw < 1 {
		g.gw = 1
	}
	if g.gh < 1 {
		g.gh = 1
	}
	cells := g.gw * g.gh
	deg := make([]int32, cells+1)
	cellOf := func(n int32) int {
		x, y := g.XY(n)
		cx := int((x - g.minX) / g.cell)
		cy := int((y - g.minY) / g.cell)
		if cx < 0 {
			cx = 0
		}
		if cy < 0 {
			cy = 0
		}
		if cx >= g.gw {
			cx = g.gw - 1
		}
		if cy >= g.gh {
			cy = g.gh - 1
		}
		return cy*g.gw + cx
	}
	for n := int32(0); n < int32(len(g.pt)); n++ {
		deg[cellOf(n)]++
	}
	off := make([]int32, cells+1)
	var acc int32
	for i := 0; i < cells; i++ {
		off[i] = acc
		acc += deg[i]
	}
	off[cells] = acc
	nodes := make([]int32, acc)
	fill := append([]int32(nil), off[:cells]...)
	for n := int32(0); n < int32(len(g.pt)); n++ {
		c := cellOf(n)
		nodes[fill[c]] = n
		fill[c]++
	}
	g.gridOff, g.gridN = off, nodes
}

// Nearest picks the junction for a point, exactly as cmd/pathfinder does: a
// destination must have a stretch leading into it, a start one leaving it, and
// a street beats a driveway (service road) within 150 m, so a click near a
// house lands on the road rather than on the drive.
//
// role is "from" (a way out in the plan's first mode) or "to" (a way in by any
// mode of the plan). It returns -1 when nothing usable is within maxM metres.
func (g *Graph) Nearest(x, y float64, role string, plan []int, maxM float64) int32 {
	n, _ := g.NearestD(x, y, role, plan, maxM)
	return n
}

// NearestD is Nearest with the metres between the point and the junction it
// landed on, so a caller can refuse a snap that moved the point too far.
func (g *Graph) NearestD(x, y float64, role string, plan []int, maxM float64) (int32, float64) {
	// A destination needs a stretch leading INTO it, a start one leaving it,
	// and a stop in the middle needs both: on a one-way street the two are
	// different junctions, and a destination without an incoming edge is a
	// place the search can leave but never reach.
	fits := func(n int32) bool {
		switch role {
		case "to":
			for _, m := range plan {
				if g.connected(m, n) && g.indeg[m][n] > 0 {
					return true
				}
			}
			return false
		case "via":
			for _, m := range plan {
				if g.connected(m, n) && g.indeg[m][n] > 0 && len(g.adj[m].at(n)) > 0 {
					return true
				}
			}
			return false
		case "park":
			// The stop of a two-mode trip: driven to in the first mode, left
			// in the second. A car park at the end of a one-way that no path
			// leaves is not one.
			if len(plan) < 2 {
				return false
			}
			return g.connected(plan[0], n) && g.indeg[plan[0]][n] > 0 &&
				g.connected(plan[1], n) && len(g.adj[plan[1]].at(n)) > 0
		}
		return g.connected(plan[0], n) && len(g.adj[plan[0]].at(n)) > 0
	}
	street := func(n int32) bool {
		for _, e := range g.uni.at(n) {
			if g.R.Stretches[e.st].Cls != "service" {
				return true
			}
		}
		return false
	}
	cx := int((x - g.minX) / g.cell)
	cy := int((y - g.minY) / g.cell)
	bestN, bestD := int32(-1), math.Inf(1)
	streetN, streetD := int32(-1), math.Inf(1)
	maxRing := int(maxM/g.cell) + 2
	for ring := 0; ring <= maxRing; ring++ {
		// Once a candidate is closer than this ring can possibly be, stop.
		if bestN >= 0 && float64(ring-1)*g.cell > math.Sqrt(bestD) {
			break
		}
		for gy := cy - ring; gy <= cy+ring; gy++ {
			if gy < 0 || gy >= g.gh {
				continue
			}
			for gx := cx - ring; gx <= cx+ring; gx++ {
				if gx < 0 || gx >= g.gw {
					continue
				}
				if ring > 0 && gx != cx-ring && gx != cx+ring && gy != cy-ring && gy != cy+ring {
					continue // inside: already visited
				}
				c := gy*g.gw + gx
				for _, n := range g.gridN[g.gridOff[c]:g.gridOff[c+1]] {
					if !fits(n) {
						continue
					}
					px, py := g.XY(n)
					d := (px-x)*(px-x) + (py-y)*(py-y)
					if d < bestD {
						bestN, bestD = n, d
					}
					if d < streetD && street(n) {
						streetN, streetD = n, d
					}
				}
			}
		}
	}
	if bestN >= 0 && bestD > maxM*maxM {
		return -1, math.Sqrt(bestD)
	}
	if streetN >= 0 && streetD < 150*150 {
		return streetN, math.Sqrt(streetD)
	}
	if bestN < 0 {
		return -1, math.Inf(1)
	}
	return bestN, math.Sqrt(bestD)
}

// NameAt is the via at a junction, in the vocabulary of the plan that reached
// it: the first named stretch leaving it, else the nearest named one within
// three hops (a driveway or a parking aisle takes the name of the street it
// hangs off), else the class of the road. On foot a trail number counts as a
// name; on wheels it never does, and the road's own ref (SP 235) does — the
// same rule as stepName, for the same reason: a marked trail runs along
// roads, and "the route starts 120 m away on trail 608B" told a driver the
// car was on a footpath.
func (g *Graph) NameAt(n int32, plan []int) string {
	if nm := g.nameNear(n, g.modeAt(n, plan)); nm != "" {
		return nm
	}
	if es := g.uni.at(n); len(es) > 0 {
		return g.R.Stretches[es[0].st].Cls + " road"
	}
	return "unnamed road"
}

// nameNear is NameAt without the last resort: the nearest name within three
// hops in the vocabulary of one mode, or "" when there is none, so a caller
// with a better fallback of its own (ParkName has the place grid) can tell a
// name from a class word.
func (g *Graph) nameNear(n int32, mode int) string {
	seen := map[int32]bool{n: true}
	frontier := []int32{n}
	for hop := 0; hop < 4 && len(frontier) > 0; hop++ {
		var next []int32
		for _, v := range frontier {
			for _, e := range g.uni.at(v) {
				st := g.R.Stretches[e.st]
				if st.Name != "" {
					return st.Name
				}
				if mode == modeHike {
					if st.HikeRef != "" {
						return "trail " + st.HikeRef
					}
				} else if st.Ref != "" {
					return st.Ref
				}
				if !seen[e.to] {
					seen[e.to] = true
					next = append(next, e.to)
				}
			}
		}
		frontier = next
	}
	return ""
}

// modeAt is the mode of a plan that a junction belongs to: the first of the
// plan's modes with a way out of it, else the plan's first. A point snapped
// with a two-mode plan lands on the road or on the path, and its name should
// be in the vocabulary of whichever it is.
func (g *Graph) modeAt(n int32, plan []int) int {
	for _, m := range plan {
		if m >= 0 && m < nModes && len(g.adj[m].at(n)) > 0 {
			return m
		}
	}
	if len(plan) > 0 {
		return plan[0]
	}
	return modeHike
}

// KindAt reports what kind of way the junction sits on, for /api/reverse.
func (g *Graph) KindAt(n int32) string {
	best := ""
	rank := func(cls string) int {
		switch cls {
		case "motorway", "trunk":
			return 6
		case "primary":
			return 5
		case "secondary":
			return 4
		case "tertiary":
			return 3
		case "residential":
			return 2
		case "service":
			return 1
		}
		return 0
	}
	bestR := -1
	for _, e := range g.uni.at(n) {
		st := g.R.Stretches[e.st]
		if r := rank(st.Cls); r > bestR {
			bestR, best = r, st.Cls
		}
	}
	if best == "" {
		return "path"
	}
	return best
}

// trailhead says a trip in mode m may park at junction n and go on by mode nm:
// only where nm has a way out that m could not take (a trail leaving a road).
// Elsewhere you would simply drive on.
func (g *Graph) trailhead(m, nm int, n int32) bool {
	for _, e := range g.adj[nm].at(n) {
		if !usable(m, g.R.Stretches[e.st]) {
			return true
		}
	}
	return false
}

// effGrade is the grade a stretch is walked at: the SAT one when the map
// carries it, else the OSM sac_scale. 0 means "not declared".
func (g *Graph) effGrade(st int32) int {
	if int(st) < len(g.R.Sat) && g.R.Sat[st].Grade > 0 {
		return g.R.Sat[st].Grade
	}
	return g.R.Stretches[st].Sac
}

// SortedNames is every distinct street name in the region, for tests and for
// the geocoder's smoke checks.
func (g *Graph) SortedNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, st := range g.R.Stretches {
		if st.Name != "" && !seen[st.Name] {
			seen[st.Name] = true
			out = append(out, st.Name)
		}
	}
	sort.Strings(out)
	return out
}
