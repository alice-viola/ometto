package route

// The search: A* over (junction, mode) states — the Dijkstra of
// cmd/pathfinder/inmem.go with the admissible bound of modes.go actually
// used, because the map is now a region and not a city — cut into legs,
// sampled into an elevation profile, merged into named steps, and repeated
// with penalties for alternatives.

import (
	"container/heap"
	"fmt"
	"math"
	"strings"
	"time"

	"ometto/internal/city"
)

// ---------------------------------------------------------------- the request

// Point is a WGS84 coordinate with an optional label the caller carries.
type Point struct {
	Lat  float64 `json:"lat"`
	Lon  float64 `json:"lon"`
	Name string  `json:"name,omitempty"`
	// Via is a point the route passes THROUGH: it shapes the line without
	// becoming a stop. It never parks the car, never cuts a leg, and does not
	// count against the six stops a trip may have.
	Via bool `json:"via,omitempty"`
}

// Request is one routing question: two to six points, a mode plan, a walking
// grade and how many routes to look for.
type Request struct {
	Points       []Point `json:"points"`
	Mode         string  `json:"mode"`
	Grade        string  `json:"grade"`
	Alternatives int     `json:"alternatives"`
	// Lifts lets a walk ride the aerialways. Off by default: a plan that puts
	// you in a cable car you did not ask for is worse than a long walk.
	Lifts bool `json:"lifts,omitempty"`
	// Avoid are places the route must keep off: each is resolved to the one
	// way it lands on, and that way is struck out of this request's search.
	Avoid []Point `json:"avoid,omitempty"`
	User  string  `json:"user,omitempty"`
}

// ---------------------------------------------------------------- the answer

// Geometry is a GeoJSON LineString in WGS84, longitude first.
type Geometry struct {
	Type        string       `json:"type"`
	Coordinates [][2]float64 `json:"coordinates"`
}

// Step is one named piece of the route: consecutive stretches that share a
// name (or a trail number) merged into one line of the directions.
type Step struct {
	Name    string  `json:"name"`
	Mode    string  `json:"mode"`
	Meters  float64 `json:"meters"`
	Seconds float64 `json:"seconds"`
}

// Leg is one stretch of the trip in one mode between two stops: the trip is
// cut at every stop and at every change of mode.
type Leg struct {
	Mode string `json:"mode"`
	// Name and LiftType are filled on a leg whose mode is "lift": the
	// aerialway's name and its kind.
	Name     string             `json:"name,omitempty"`
	LiftType string             `json:"liftType,omitempty"`
	Seconds  float64            `json:"seconds"`
	Meters   float64            `json:"meters"`
	Ascent   float64            `json:"ascent"`
	Descent  float64            `json:"descent"`
	Grade    string             `json:"grade"`
	Classes  map[string]float64 `json:"classes"`
	Geometry Geometry           `json:"geometry"`
	Profile  [][2]float64       `json:"profile,omitempty"`

	pts     [][2]float64
	zs      []float64
	hasZ    bool
	sac     int
	steps   []Step
	rawUp   float64 // the per-stretch sum, kept for a map without elevation
	rawDown float64
}

// Route is one answer: the whole trip.
type Route struct {
	ID      string  `json:"id"`
	Seconds float64 `json:"seconds"`
	Meters  float64 `json:"meters"`
	Ascent  float64 `json:"ascent"`
	Descent float64 `json:"descent"`
	// WalkMeters and WalkAscent are the trip's walking half: the sum over the
	// legs whose mode is hike. On a car+hike answer the totals above include
	// the drive, and it is the walk that decides whether a person goes.
	WalkMeters float64 `json:"walkMeters"`
	WalkAscent float64 `json:"walkAscent"`
	Grade      string  `json:"grade"`
	Legs       []*Leg  `json:"legs"`
	// Geometry and Profile are the legs' joined, and are NOT on the wire: a
	// route carried its whole line twice — once here and once cut into legs —
	// and a 94 km car+hike with three alternatives was 691 KB for it, more
	// than the queue's reply may hold. The page joins the legs itself
	// (consecutive legs share their meeting point).
	Geometry Geometry     `json:"-"`
	Profile  [][2]float64 `json:"-"`
	Steps    []Step       `json:"steps"`
	Warnings []string     `json:"warnings"`
	// Parking is present on a two-mode trip that actually changed mode.
	Parking *Parking `json:"parking,omitempty"`

	stretches map[int32]float64 // the stretches walked, by length: the overlap test
	walkSts   []int32           // the stretches gone over on foot, in travel order
	parkNode  int32             // the junction a mid-leg switch parked at, -1 when none
	parkPair  int               // and which pair of waypoints it happened between
	liftOn    int32             // the station the first ride was boarded at, -1 when none
}

// Parking is where the car (or the bike) is left on a two-mode trip. Point is
// the index into the request's points when the switch happens AT a stop, and
// null when it happens in the middle of a leg, at a trailhead the search
// chose: the page puts its P badge on a stop only when Point names one.
type Parking struct {
	Point *int    `json:"point"`
	Name  string  `json:"name"`
	Lat   float64 `json:"lat"`
	Lon   float64 `json:"lon"`
}

// Snap is where a requested point landed on the network, and how far it had
// to move to get there.
type Snap struct {
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Name     string  `json:"name"`
	Distance float64 `json:"distance"`
	Via      bool    `json:"via,omitempty"`
}

// Avoided is a way this request struck out, with the geometry the map needs to
// hatch it.
type Avoided struct {
	ID     string       `json:"id"`
	Name   string       `json:"name"`
	Class  string       `json:"class"`
	Points [][2]float64 `json:"points"`
}

// Result is what the engine answers.
type Result struct {
	Routes  []*Route `json:"routes"`
	Snapped []Snap   `json:"snapped"`
	Reason  string   `json:"reason,omitempty"`
	// NeededGrade is the easiest grade that WOULD answer this question, when
	// the requested one cannot: the frontend turns it into one click.
	NeededGrade string `json:"neededGrade,omitempty"`
	// Avoided are the ways the request struck out, as they were resolved.
	Avoided []Avoided `json:"avoided,omitempty"`

	Settled int           `json:"-"`
	Took    time.Duration `json:"-"`
	Cached  bool          `json:"-"` // answered from the refusal cache
}

// ---------------------------------------------------------------- the search

// SnapLimit is how far a point may be from the network before the answer is a
// refusal; SnapSearch is how far the grid is searched, so the refusal can say
// how far the nearest road actually is.
const (
	SnapLimit  = 1000.0
	SnapSearch = 10000.0
	// rampCost is the seconds charged for entering or leaving a motorway or a
	// trunk road: a slip road, the give-way at its end, and the fact that the
	// A22 is not free to join. Without it a two-minute hop onto the motorway
	// beats the parallel road on paper and never in the car.
	rampCost = 20.0
	// roadDeadBand is the metres a road's elevation line must move before it
	// counts as a climb. Calibrated against three published flat rides.
	roadDeadBand = 10.0
	// minStepM is the shortest piece of the directions worth a line of its
	// own, and maxSteps the most lines a route may have.
	minStepM = 150.0
	maxSteps = 40
	// parkReach is how far from a valley station a car may be left and still
	// count as parking at it: the station's own car park, the square in front
	// of it, the last widening of the road.
	parkReach = 300.0
)

type state = int64

func sk(node int32, mode int) state { return int64(node)*nModes + int64(mode) }
func nodeOf(s state) int32          { return int32(s / nModes) }
func modeOf(s state) int            { return int(s % nModes) }

type item struct {
	f float64
	s state
}

type pq []item

func (p pq) Len() int            { return len(p) }
func (p pq) Less(i, j int) bool  { return p[i].f < p[j].f }
func (p pq) Swap(i, j int)       { p[i], p[j] = p[j], p[i] }
func (p *pq) Push(x interface{}) { *p = append(*p, x.(item)) }
func (p *pq) Pop() interface{} {
	old := *p
	n := len(old)
	it := old[n-1]
	*p = old[:n-1]
	return it
}

// usedEdge is one move of the answer: a stretch travelled, or the parking
// stop where the trip changes mode. w is the seconds the move TAKES — never
// what a penalty round was willing to pay to avoid it.
type usedEdge struct {
	st     int32
	rev    bool
	mode   int
	w      float64
	parked bool  // the switch from one mode to the next; st is meaningless
	node   int32 // where that switch happened: the junction the car is left at
}

type stats struct {
	settled      int
	ferrataSkip  bool
	gradeSkipped int // the easiest grade that was refused, 0 when none was
}

// penalties is what a round of alternatives is willing to pay, on top of the
// honest travel time, to be shown a different answer: a factor on every
// stretch the routes before it used, and a flat charge for leaving the car
// where they left it.
//
// None of it is time anybody spends. It orders the heap and stops there — see
// searchSeg, where cost and weight part company.
type penalties struct {
	stretch  map[int32]float64 // by stretch id: the factor on its weight
	approach map[int32]bool    // the last of the fastest route's walk: see approachOf
	parkAt   [][2]float64      // where the routes kept so far left the car
	parkPen  float64           // charged for parking within parkSpread of one
}

// parkSpread is how far apart two trailheads must be before they are a
// different answer to "where do I leave the car". Inside three kilometres you
// are on the same side of the mountain, up the same valley, off the same road:
// two car parks a kilometre apart in Val Nambrone are one trailhead to a
// person choosing a day out, however different the walk above them looks.
const parkSpread = 3000.0

// otherParkShare is what the first penalty round will pay, as a share of the
// fastest trip's own clock, for a trailhead somewhere else. At 1 another side
// is worth a card while it costs less than roughly twice the fastest: Trento
// to Cima Presanella is six and a half hours up Val Nambrone, and the ways in
// from the valleys around it — under nine hours, ten, ten and a half — are all
// days out a person might choose. At three times over it would not be an
// alternative, it would be a different holiday.
//
// What it buys is a different valley, not a chosen one: the cheapest trailhead
// outside the radius wins, whichever side of the summit it stands on.
const otherParkShare = 1.0

// approachM is how much of the end of a walk is its APPROACH: the last three
// kilometres, or the whole of it when it is shorter. It is the part of a day
// that decides which side of a mountain you were on — the ridge, the glacier,
// the last trail above the huts — and it is what two cards have in common when
// they are the same answer with different parking.
const approachM = 3000.0

// approachFactor is what the first penalty round charges on that approach:
// eight times, against the ×1.5 everything else the route used pays. It has to
// outweigh any amount of cleverness with car parks, because that is exactly
// what it is there to beat — Trento to Cima Presanella can be started from
// four valleys and still finish up the same trail, and four cards that finish
// the same way are one card. At eight, a line that reaches the summit ANOTHER
// way wins whenever it costs less than about twice the fastest, and a line
// that only moves the car does not.
const approachFactor = 8.0

// factor is what this round charges for a stretch an earlier route travelled.
func (p *penalties) factor(st int32) float64 {
	if p == nil {
		return 1
	}
	if p.approach[st] {
		return approachFactor
	}
	if f, ok := p.stretch[st]; ok {
		return f
	}
	return 1
}

// isLift says a stretch is an aerialway: a ride, and never a step of the walk.
func (g *Graph) isLift(st int32) bool {
	return int(st) < len(g.R.Lift) && g.R.Lift[st].Is
}

// approachOf is the last approachM metres of a route's walk, as a set of
// stretches: counted backwards from the destination, over what was walked and
// never over what was ridden or driven. A route with no walk in it has no
// approach and nothing to protect.
func (g *Graph) approachOf(r *Route) map[int32]bool {
	if len(r.walkSts) == 0 {
		return nil
	}
	out := map[int32]bool{}
	m := 0.0
	for i := len(r.walkSts) - 1; i >= 0 && m < approachM; i-- {
		out[r.walkSts[i]] = true
		m += g.R.Stretches[r.walkSts[i]].Len
	}
	return out
}

// parkCost is what this round charges for leaving the car at n: nothing,
// unless n is within parkSpread of a trailhead an earlier route already chose.
func (p *penalties) parkCost(g *Graph, n int32) float64 {
	if p == nil || p.parkPen <= 0 {
		return 0
	}
	x, y := g.XY(n)
	for _, q := range p.parkAt {
		if math.Hypot(x-q[0], y-q[1]) <= parkSpread {
			return p.parkPen
		}
	}
	return 0
}

// searchSeg is one leg of the trip: A* over (junction, mode) from `from` to
// `to` under `plan`, refusing anything above `grade` on foot.
//
// COST IS NOT WEIGHT. What `pen` adds — a factor on a stretch an earlier route
// used, a charge for parking where it parked — orders the heap and is thrown
// away; what an edge RECORDS is w, the seconds the move really takes, and w is
// what the legs add up. A penalty that reached the answer made an alternative
// report a drive nobody drives: the same roads to Val Nambrone at 109 minutes
// on the third card and 75 on the first, half an hour of pure arithmetic. The
// charges that ARE time — the ramp, the parking, the wait at a lift station —
// go into w, and into the clock.
//
// The bound stays admissible because every cost is at least its weight: no
// penalty is ever a discount.
func (g *Graph) searchSeg(from, to int32, plan []int, grade int, lifts bool, avoid map[int32]bool, pen *penalties, st *stats) ([]usedEdge, bool) {
	next := map[int]int{}
	for i := 0; i+1 < len(plan); i++ {
		next[plan[i]] = plan[i+1]
	}
	// h is admissible: the straight line at the fastest speed the modes still
	// ahead in the plan can move. Once in the last mode nothing is faster.
	vrest := make([]float64, nModes)
	for i, m := range plan {
		vrest[m] = vmax(plan[i:])
	}
	dx, dy := g.XY(to)
	h := func(n int32, mode int) float64 {
		v := vrest[mode]
		if v <= 0 {
			v = vmax(plan)
		}
		if lifts && v < liftFastest {
			v = liftFastest // a cable car is the fastest thing on the map
		}
		x, y := g.XY(n)
		return math.Hypot(x-dx, y-dy) / v
	}

	type predRec struct {
		prev   state
		st     int32
		rev    bool
		w      float64
		parked bool
	}
	labels := make(map[state]float64, 1<<12)
	pred := make(map[state]predRec, 1<<12)
	h0 := &pq{}
	start := sk(from, plan[0])
	labels[start] = 0
	pred[start] = predRec{prev: -1}
	heap.Push(h0, item{h(from, plan[0]), start})

	goal := state(-1)
	for h0.Len() > 0 {
		it := heap.Pop(h0).(item)
		d, ok := labels[it.s]
		if !ok || it.f > d+h(nodeOf(it.s), modeOf(it.s))+1e-9 {
			continue // a stale entry: a better label was found after this push
		}
		node, m := nodeOf(it.s), modeOf(it.s)
		if node == to {
			goal = it.s
			break
		}
		st.settled++
		// What we arrived on, for the ramp charge. The labels are per
		// junction, so this is the class of the BEST arrival, not of every
		// arrival: an approximation that can only misprice a ramp by 20 s.
		prev, hasPrev := pred[it.s]
		onMotorway := hasPrev && prev.prev >= 0 && !prev.parked &&
			isMotorway(g.R.Stretches[prev.st].Cls)
		for _, e := range g.adj[m].at(node) {
			if avoid != nil && avoid[e.st] {
				continue
			}
			if m == modeHike {
				sh := g.R.Stretches[e.st]
				if sh.Ferrata && grade < 4 {
					st.ferrataSkip = true
					continue
				}
				if eg := g.effGrade(e.st); eg > gradeCeiling(grade) {
					if st.gradeSkipped == 0 || eg < st.gradeSkipped {
						st.gradeSkipped = eg
					}
					continue
				}
			}
			w := float64(e.w)
			if m == modeCar && hasPrev && prev.prev >= 0 && !prev.parked &&
				isMotorway(g.R.Stretches[e.st].Cls) != onMotorway {
				w += rampCost
			}
			s2, d2 := sk(e.to, m), d+w*pen.factor(e.st)
			if cur, seen := labels[s2]; !seen || d2 < cur-1e-9 {
				labels[s2] = d2
				pred[s2] = predRec{prev: it.s, st: e.st, rev: e.rev, w: w}
				heap.Push(h0, item{d2 + h(e.to, m), s2})
			}
		}
		if lifts && m == modeHike {
			// The aerialways and the short walks that reach them, a layer
			// nothing else can see.
			for _, e := range g.lift.at(node) {
				if avoid != nil && avoid[e.st] {
					continue
				}
				w := float64(e.w)
				s2, d2 := sk(e.to, m), d+w*pen.factor(e.st)
				if cur, seen := labels[s2]; !seen || d2 < cur-1e-9 {
					labels[s2] = d2
					pred[s2] = predRec{prev: it.s, st: e.st, rev: e.rev, w: w}
					heap.Push(h0, item{d2 + h(e.to, m), s2})
				}
			}
		}
		if nm, ok := next[m]; ok && g.trailhead(m, nm, node) {
			s2, d2 := sk(node, nm), d+switchCost+pen.parkCost(g, node)
			if cur, seen := labels[s2]; !seen || d2 < cur-1e-9 {
				labels[s2] = d2
				pred[s2] = predRec{prev: it.s, w: switchCost, parked: true}
				heap.Push(h0, item{d2 + h(node, nm), s2})
			}
		}
	}
	if goal < 0 {
		return nil, false
	}
	var rev []usedEdge
	for s := goal; ; {
		p, ok := pred[s]
		if !ok || p.prev < 0 {
			break
		}
		rev = append(rev, usedEdge{st: p.st, rev: p.rev, mode: modeOf(s), w: p.w, parked: p.parked, node: nodeOf(s)})
		s = p.prev
	}
	out := make([]usedEdge, len(rev))
	for i := range rev {
		out[i] = rev[len(rev)-1-i]
	}
	return out, true
}

// ---------------------------------------------------------------- the trip

// Route answers a whole request: the points snapped, the segments routed and
// concatenated, the alternatives found by penalty rounds.
//
// MULTI-STOP. points[0] -> points[1] -> ... is routed one segment at a time
// and the legs are concatenated.
//
// A STOP IS WHERE YOU PARK. In a two-mode plan (car+hike, bike+hike) with
// three or more points, the first segment is driven and every later one is
// walked: "Trento -> Molveno -> Rifugio Pedrotti" drives to Molveno and walks
// from it, which is what a person who typed those three places meant. Left to
// the clock alone the planner drives past the stop to the nearest trailhead,
// because driving is faster — a correct answer to a question nobody asked.
//
// With only TWO points there is no stop to park at, so the trailhead rule
// decides as before: the trip may change mode once, at any junction where the
// next mode can leave by a way the current one cannot.
//
// AND WHERE A LIFT COMES IN SECTIONS the trailhead the clock chose is not the
// default: the car goes to the bottom of the chain and the whole lift is
// ridden, whenever a road gets there. See parkLowest, at the end of this
// section.
func (g *Graph) Route(req Request) *Result {
	if r, ok := g.cachedRefusal(req); ok {
		return r
	}
	plan := ParsePlan(req.Mode)
	grade := SatGrade(req.Grade)
	if grade == 0 {
		grade = 2 // E
	}
	want := req.Alternatives
	if want < 1 {
		want = 1
	}
	if want > 3 {
		want = 3
	}
	t0 := time.Now()
	res := &Result{Routes: []*Route{}}
	// A question with fewer than two points is not a question. The HTTP
	// handler refuses one long before here, but this is also what a message
	// off the queue reaches — a stale one, another client's, a bad deploy —
	// and a panic in a worker takes the whole service with it.
	if len(req.Points) < 2 {
		res.Reason = "a route needs at least two points"
		res.Took = time.Since(t0)
		return res
	}

	// Snap every point. The first needs a way out, the rest a way in — and a
	// point more than SnapLimit from any road of that mode is REFUSED, not
	// quietly moved: a click in the middle of a lake that comes back as a
	// route from the shore three kilometres away is a wrong answer wearing
	// the clothes of a right one.
	// WHAT TO KEEP OFF. Each avoid point is resolved to the one way it lands
	// on — within 50 m, among the classes this request's modes can use — and
	// that way is struck out of this search in both directions. A point that
	// lands on nothing is dropped, not refused: a stray click should not cost
	// a person their route.
	var avoid map[int32]bool
	var avoidIdx []int32
	for _, a := range req.Avoid {
		x, y := g.R.XY(a.Lat, a.Lon)
		si, ok := g.Avoid(x, y, 50, plan, req.Lifts)
		if !ok || (avoid != nil && avoid[si]) {
			continue
		}
		if avoid == nil {
			avoid = map[int32]bool{}
		}
		avoid[si] = true
		avoidIdx = append(avoidIdx, si)
		st := g.R.Stretches[si]
		res.Avoided = append(res.Avoided, Avoided{
			ID: st.ID, Name: g.stepName(si, nameMode(st.Cls)), Class: st.Cls,
			Points: g.geometry(st.Pts).Coordinates,
		})
	}

	// WHERE THE CAR IS LEFT, on a two-mode plan (car+hike, bike+hike).
	//
	// Every stop and the destination is snapped to the FIRST mode when a road
	// within a kilometre reaches it, and to the walking mode otherwise: three
	// Brenta huts are walk-only, and asking a car to reach Rifugio Brentei is
	// how a real trip got refused with "the nearest road is 3.9 km away".
	//
	// The mode switches once. A walk-only point forces the switch before it,
	// and the parking is the last STOP the car still reaches before it — a via
	// is never a parking — so "Trento -> Molveno -> Pedrotti" parks at
	// Molveno. When there is no such stop the trailhead rule chooses one
	// inside that leg, exactly as it does for a two-point trip. After the
	// switch every later point is walked to, road or no road.
	twoMode := len(plan) >= 2
	last := len(req.Points) - 1
	stops := 0
	for _, p := range req.Points {
		if !p.Via {
			stops++
		}
	}
	carOK := make([]bool, len(req.Points))
	if twoMode {
		for i := 1; i < len(req.Points); i++ {
			role := "via"
			if i == last {
				role = "to"
			}
			x, y := g.R.XY(req.Points[i].Lat, req.Points[i].Lon)
			if n, d := g.NearestD(x, y, role, plan[:1], SnapSearch); n >= 0 && d <= SnapLimit {
				carOK[i] = true
			}
		}
	}
	firstWalk := -1
	if twoMode {
		for i := 1; i < len(req.Points); i++ {
			if !carOK[i] {
				firstWalk = i
				break
			}
		}
	}
	// parkPt is the POINT the car is left at, -1 when the switch is mid-leg.
	parkPt := -1
	switch {
	case !twoMode:
	case firstWalk < 0:
		if stops >= 3 {
			for i := 1; i <= last; i++ {
				if !req.Points[i].Via {
					parkPt = i // nothing is walk-only: the first stop parks
					break
				}
			}
		}
	default:
		for i := firstWalk - 1; i >= 1; i-- {
			if !req.Points[i].Via && carOK[i] {
				parkPt = i
				break
			}
		}
	}
	// walked[i] is a point the trip arrives at on foot.
	walked := make([]bool, len(req.Points))
	if twoMode {
		for i := 1; i < len(req.Points); i++ {
			switch {
			case parkPt >= 1:
				walked[i] = i > parkPt
			case firstWalk >= 1:
				walked[i] = i >= firstWalk
			}
		}
	}

	// Snap every point, and turn it into waypoints: a via dropped on a lift
	// line becomes TWO — the end you board and the end you get off — so the
	// ride is not a suggestion.
	type waypoint struct {
		node  int32
		stop  bool
		pt    int
		force forcedLift
	}
	var wps []waypoint
	for i, p := range req.Points {
		x, y := g.R.XY(p.Lat, p.Lon)
		role, rolePlan := "via", plan
		if i == last {
			role = "to"
		}
		if twoMode {
			switch {
			case i == 0:
				role, rolePlan = "from", plan[:1]
			case i == parkPt:
				role, rolePlan = "park", plan // driven to, left on foot
			case walked[i]:
				rolePlan = plan[1:]
			case parkPt >= 1:
				rolePlan = plan[:1]
			}
		}
		// A via within 30 m of a lift line means "ride this lift", and is
		// resolved before anything else: a cable car runs nowhere near a
		// footpath junction, which is exactly why it is worth riding.
		if p.Via && req.Lifts {
			if si, ok := g.LiftNear(x, y, 30); ok {
				ea, eb, uphillIsB, oneway := g.LiftEnds(si)
				if ea >= 0 && eb >= 0 {
					first, second := ea, eb
					if oneway {
						if !uphillIsB {
							first, second = eb, ea // always bottom to top
						}
					} else if len(wps) > 0 {
						px, py := g.XY(wps[len(wps)-1].node)
						ax, ay := g.XY(ea)
						bx, by := g.XY(eb)
						if math.Hypot(bx-px, by-py) < math.Hypot(ax-px, ay-py) {
							first, second = eb, ea
						}
					}
					if rev, w, okr := g.LiftRide(si, first); okr {
						fx, fy := g.XY(first)
						flat, flon := g.R.LatLon(fx, fy)
						res.Snapped = append(res.Snapped, Snap{Lat: round6(flat), Lon: round6(flon),
							Name: g.R.Stretches[si].Name, Distance: round1(math.Hypot(fx-x, fy-y)), Via: true})
						wps = append(wps, waypoint{node: first, pt: i, force: forcedLift{st: si, rev: rev, w: w, ok: true}})
						wps = append(wps, waypoint{node: second, pt: i})
						continue
					}
				}
			}
		}
		namePlan := rolePlan
		n, d := g.NearestD(x, y, role, rolePlan, SnapSearch)
		if (n < 0 || d > SnapLimit) && p.Via {
			// A via is not a stop: any mode of the plan will do.
			n, d = g.NearestD(x, y, "via", plan, SnapSearch)
			namePlan = plan
		}
		if n < 0 || d > SnapLimit {
			// Refused. The pin stays where the finger was: a marker 8.6 km
			// away with the name of a road nobody asked for is worse than no
			// marker at all. The distance still says how far the network is.
			far := 0.0
			if n >= 0 {
				far = round1(d)
			}
			res.Snapped = append(res.Snapped, Snap{Lat: round6(p.Lat), Lon: round6(p.Lon), Name: "", Distance: far, Via: p.Via})
			res.Reason = fmt.Sprintf("no road within 1 km of %.5f,%.5f", p.Lat, p.Lon)
			res.Took = time.Since(t0)
			g.cacheRefusal(req, res)
			return res
		}
		lat, lon := g.LatLon(n)
		res.Snapped = append(res.Snapped, Snap{Lat: round6(lat), Lon: round6(lon), Name: g.NameAt(n, namePlan), Distance: round1(d), Via: p.Via})
		wps = append(wps, waypoint{node: n, stop: !p.Via, pt: i})
	}

	// The plan of every pair of waypoints: driven up to the parking, walked
	// after it, the full plan on the pair where the trailhead rule must choose.
	nodes := make([]int32, len(wps))
	isStop := make([]bool, len(wps))
	force := make([]forcedLift, len(wps))
	parkWp, firstWalkWp := -1, -1
	for k, w := range wps {
		nodes[k], isStop[k], force[k] = w.node, w.stop, w.force
		if parkWp < 0 && w.stop && w.pt == parkPt {
			parkWp = k
		}
		if firstWalkWp < 0 && firstWalk >= 0 && w.pt == firstWalk {
			firstWalkWp = k
		}
	}
	isStop[len(isStop)-1] = true // the destination always closes the last leg
	var segPlans [][]int
	if twoMode && (parkWp >= 1 || firstWalkWp >= 1) {
		for k := 0; k+1 < len(wps); k++ {
			switch {
			case parkWp >= 1 && k < parkWp:
				segPlans = append(segPlans, plan[:1])
			case parkWp >= 1:
				segPlans = append(segPlans, plan[1:])
			case k+1 == firstWalkWp:
				segPlans = append(segPlans, plan) // the trailhead rule decides
			case k+1 > firstWalkWp:
				segPlans = append(segPlans, plan[1:])
			default:
				segPlans = append(segPlans, plan[:1])
			}
		}
	}

	parkAt := ""
	if parkPt >= 1 {
		parkAt = strings.TrimSpace(req.Points[parkPt].Name)
		if parkAt == "" && parkPt < len(res.Snapped) {
			parkAt = res.Snapped[parkPt].Name
			// The caller named no stop and the junction is called after a
			// trail: name the parking the way a mid-leg one is named, so the
			// card never reads "parked at trail 382".
			if trailNumber(parkAt) {
				parkAt = ""
				if parkWp >= 0 {
					parkAt = g.ParkName(nodes[parkWp], plan[0])
				}
			}
		}
	}
	tp := &tripPlan{nodes: nodes, stop: isStop, plan: plan, segPlans: segPlans,
		force: force, parkAt: parkAt, parkSeg: parkWp, parkPoint: parkPt,
		grade: grade, lifts: req.Lifts, avoid: avoid}
	if parkPt >= 1 && parkPt < len(res.Snapped) {
		tp.parkLat, tp.parkLon = res.Snapped[parkPt].Lat, res.Snapped[parkPt].Lon
	}

	pen := &penalties{stretch: map[int32]float64{}}
	var sstats stats
	for round := 0; round < want+2 && len(res.Routes) < want; round++ {
		r := g.trip(tp, pen, &sstats)
		if r == nil {
			break
		}
		keep := true
		for _, prev := range res.Routes {
			if sameLine(r, prev) {
				keep = false
				break
			}
		}
		if keep {
			r.ID = fmt.Sprintf("r%d", len(res.Routes)+1)
			res.Routes = append(res.Routes, r)
		}
		if len(res.Routes) >= want {
			break
		}
		// Penalty round: everything this route used costs half as much again.
		for id := range r.stretches {
			if f, ok := pen.stretch[id]; ok {
				pen.stretch[id] = f * 1.5
			} else {
				pen.stretch[id] = 1.5
			}
		}
		// AND THE FIRST OF THEM ASKS FOR ANOTHER SIDE OF THE MOUNTAIN.
		// Penalising stretches never leaves the valley the fastest route drove
		// up: every way out of that car park is one more line to penalise, and
		// Trento to Cima Presanella answered with three tracks off the same
		// forest road in Val Nambrone while the trek in from Vermiglio went
		// unmentioned. A mountain has sides, and the side is the choice.
		//
		// The side is not WHERE YOU PARK — Presanella can be started from four
		// valleys and still finished up the same trail — it is HOW YOU FINISH.
		// So the first penalty round does two things, both only when the engine
		// chose the parking (a caller who NAMED a stop is owed that stop, not a
		// tour of the massif). It charges approachFactor on the last approachM
		// of the fastest route's walk, which is what a card has to give up to
		// count as another answer; and it charges otherParkShare of the fastest
		// trip's clock for leaving the car within parkSpread of where it has
		// already been left, so a second card does not merely shuffle car parks
		// along one valley.
		//
		// A round that comes back the same way anyway found nothing better and
		// is a candidate like any other: sameLine decides, as it does for every
		// round. The rounds after it penalise stretches alone.
		pen.approach, pen.parkAt, pen.parkPen = nil, nil, 0
		if round == 0 && len(plan) >= 2 && tp.parkPoint < 1 {
			pen.approach = g.approachOf(res.Routes[0])
			for _, kept := range res.Routes {
				if kept.parkNode >= 0 {
					x, y := g.XY(kept.parkNode)
					pen.parkAt = append(pen.parkAt, [2]float64{x, y})
				}
			}
			if len(pen.parkAt) > 0 {
				pen.parkPen = otherParkShare * res.Routes[0].Seconds
			}
		}
	}
	// PARK AT THE BOTTOM OF THE LIFT. The fastest trip is not the default when
	// a lift comes in sections and a road reaches the middle of one: see
	// parkLowest. The fast plan stays as an alternative, because driving to the
	// mid station is a real answer — just not the one to open with.
	if low := g.parkLowest(tp, res.Routes, &sstats); low != nil {
		// The fast plan is kept whatever it shares with the new default: it is
		// the answer to "and if I drive up to the mid station?". A penalty
		// round's route is not, once the default has swallowed it — that is
		// the round's own test, applied one route later.
		routes := []*Route{low, res.Routes[0]}
		for _, r := range res.Routes[1:] {
			if !sameLine(r, low) {
				routes = append(routes, r)
			}
		}
		if len(routes) > want {
			routes = routes[:want]
		}
		res.Routes = routes
		for i, r := range res.Routes {
			r.ID = fmt.Sprintf("r%d", i+1)
		}
	}
	// An avoid list that cuts the destination off says so, and names the way
	// that would have carried the trip: "no route without the A22" is an
	// answer a person can act on, "no route" is not.
	if len(res.Routes) == 0 && res.Reason == "" && avoid != nil {
		free := *tp
		free.avoid = nil
		var s2 stats
		if r := g.trip(&free, nil, &s2); r != nil {
			name := ""
			for k, si := range avoidIdx {
				if _, used := r.stretches[si]; used && k < len(res.Avoided) {
					name = res.Avoided[k].Name
					break
				}
			}
			if name == "" && len(res.Avoided) > 0 {
				name = res.Avoided[0].Name
			}
			if name != "" {
				res.Reason = "no route without " + name
			}
		}
	}
	if len(res.Routes) == 0 && res.Reason == "" {
		if hasHike(plan) {
			res.Reason = "no route up to grade " + GradeName(grade)
			// Something was refused on the way: would a harder grade do it?
			// Say WHICH, so the frontend can offer the one click that works.
			if sstats.gradeSkipped > 0 {
				for harder := grade + 1; harder <= 5; harder++ {
					var s2 stats
					hard := *tp
					hard.grade = harder
					if r := g.trip(&hard, nil, &s2); r != nil {
						res.NeededGrade = GradeName(harder)
						res.Reason = fmt.Sprintf("no route at %s: the trail to %s is %s",
							GradeName(grade), destName(req, res), GradeName(harder))
						break
					}
				}
			}
		} else {
			res.Reason = "no " + planName(plan) + " route between these points"
		}
	}
	res.Settled = sstats.settled
	res.Took = time.Since(t0)
	if len(res.Routes) == 0 {
		g.cacheRefusal(req, res)
	}
	return res
}

// parkLowest is the trip a person means when a lift comes in sections: the car
// left at the LOWEST station of the chain, and the whole chain ridden.
//
// A lift system is mapped section by section — a valley station, a mid station,
// a top station — and where a road reaches the mid station the clock says to
// drive up to it and ride only the upper half. That is the fastest answer and
// the wrong default: a person who asked for lifts asked for the lift, not for
// the last third of it. So when the engine chose the parking itself, the
// fastest trip is planned again with a stop at the bottom of the chain, and
// that becomes the default; the fast plan is kept as an alternative by the
// caller. A person who NAMED the stop keeps it — a stop is where you park.
//
// "Whenever possible" is two questions the map answers: is there a junction the
// first mode can reach within parkReach of the valley station, and does anything
// route from there. When either says no, the fastest trip stands.
//
// The parking is spliced into the waypoints as a stop, in the pair of them the
// switch happened in, and driven to and walked from by the segment plans the
// caller's own stop already uses — so the trip is built once, by one machinery,
// and its seconds, metres and climb are the honest figures of the longer plan.
// Nothing is added to the request, so Snapped still holds one entry per point.
func (g *Graph) parkLowest(tp *tripPlan, routes []*Route, st *stats) *Route {
	if len(routes) == 0 || len(tp.plan) < 2 || !tp.lifts || tp.parkPoint >= 1 {
		return nil
	}
	fast := routes[0]
	// Only a trip that really parked, at a trailhead the search chose: a
	// parking with a Point is the caller's own stop, and no parking at all
	// means no car ever moved.
	if fast.Parking == nil || fast.Parking.Point != nil || fast.liftOn < 0 {
		return nil
	}
	bottom := g.liftChainBottom(fast.liftOn)
	if bottom < 0 {
		return nil
	}
	x, y := g.XY(bottom)
	p, d := g.NearestD(x, y, "park", tp.plan, parkReach)
	if p < 0 || d > parkReach || p == fast.parkNode {
		return nil
	}
	cut := fast.parkPair + 1
	if cut < 1 || cut >= len(tp.nodes) {
		return nil
	}
	low := *tp
	low.nodes = insertAt(tp.nodes, cut, p)
	low.stop = insertAt(tp.stop, cut, true)
	low.force = insertAt(tp.force, cut, forcedLift{})
	low.segPlans = make([][]int, len(low.nodes)-1)
	for k := range low.segPlans {
		if k < cut {
			low.segPlans[k] = tp.plan[:1]
		} else {
			low.segPlans[k] = tp.plan[1:]
		}
	}
	low.parkSeg, low.parkPoint = cut, -1
	low.parkAt = g.ParkName(p, tp.plan[0])
	lat, lon := g.LatLon(p)
	low.parkLat, low.parkLon = round6(lat), round6(lon)
	return g.trip(&low, nil, st)
}

// sameLine says a route is not an alternative to another one: more than four
// fifths of what it travels was already in that one, so the two are one answer
// drawn twice.
func sameLine(r, prev *Route) bool {
	if r.Meters <= 0 {
		return false
	}
	shared := 0.0
	for id, l := range r.stretches {
		if _, ok := prev.stretches[id]; ok {
			shared += l
		}
	}
	return shared/r.Meters > 0.8
}

// insertAt puts v at index i, leaving the slice it was given alone.
func insertAt[T any](s []T, i int, v T) []T {
	out := make([]T, 0, len(s)+1)
	out = append(out, s[:i]...)
	out = append(out, v)
	return append(out, s[i:]...)
}

// ------------------------------------------------------- the refusal cache
//
// A refusal is the expensive answer: the search only knows there is no route
// when it has settled every junction it can reach, ~240 ms on the region
// graph, where a route that exists is found in ten. A person who asks twice —
// the app retrying, a double click, two tabs — should not pay it twice. Only
// refusals are cached, for a minute, and the grade is part of the key: the
// retry at the neededGrade is a different question and is searched properly.

const refusalTTL = 60 * time.Second
const refusalCacheMax = 2048

type refusal struct {
	res *Result
	at  time.Time
}

func refusalKey(req Request) string {
	var b strings.Builder
	b.WriteString(req.Mode)
	b.WriteByte('|')
	b.WriteString(req.Grade)
	if req.Lifts {
		b.WriteString("|lifts")
	}
	for _, p := range req.Points {
		fmt.Fprintf(&b, "|%.6f,%.6f", p.Lat, p.Lon)
		if p.Via {
			b.WriteString("v")
		}
	}
	for _, a := range req.Avoid {
		fmt.Fprintf(&b, "|x%.6f,%.6f", a.Lat, a.Lon)
	}
	return b.String()
}

func (g *Graph) cachedRefusal(req Request) (*Result, bool) {
	g.rmu.Lock()
	defer g.rmu.Unlock()
	r, ok := g.refusals[refusalKey(req)]
	if !ok || time.Since(r.at) > refusalTTL {
		return nil, false
	}
	// A copy, so a caller that edits its answer cannot edit everybody's.
	out := *r.res
	out.Cached = true
	out.Took = 0
	return &out, true
}

func (g *Graph) cacheRefusal(req Request, res *Result) {
	g.rmu.Lock()
	defer g.rmu.Unlock()
	if g.refusals == nil {
		g.refusals = map[string]refusal{}
	}
	if len(g.refusals) >= refusalCacheMax {
		for k, v := range g.refusals { // drop what has expired, else drop anything
			if time.Since(v.at) > refusalTTL {
				delete(g.refusals, k)
			}
		}
		for k := range g.refusals {
			if len(g.refusals) < refusalCacheMax {
				break
			}
			delete(g.refusals, k)
		}
	}
	g.refusals[refusalKey(req)] = refusal{res: res, at: time.Now()}
}

// destName is what to call the destination in a refusal: the name the caller
// gave the last point, else the way it snapped to.
func destName(req Request, res *Result) string {
	if n := len(req.Points); n > 0 && strings.TrimSpace(req.Points[n-1].Name) != "" {
		return strings.TrimSpace(req.Points[n-1].Name)
	}
	if n := len(res.Snapped); n > 0 && res.Snapped[n-1].Name != "" {
		return strings.TrimPrefix(res.Snapped[n-1].Name, "trail ")
	}
	return "the destination"
}

// gradeCeiling is the hardest sac a grade admits. "A" is the top of the scale
// and takes everything the map holds, sac 6 included; below it the grade is
// the limit itself.
func gradeCeiling(g int) int {
	if g >= 5 {
		return 6
	}
	return g
}

// gradeArticle is "an EE", "an EEA", "an E", "a T": the article English wants
// in front of each grade.
func gradeArticle(g int) string {
	name := GradeName(g)
	if name == "T" {
		return "a " + name
	}
	return "an " + name
}

func hasHike(plan []int) bool {
	for _, m := range plan {
		if m == modeHike {
			return true
		}
	}
	return false
}

// forcedLift is a ride the caller asked for by dropping a via on it: taken as
// it stands, not searched for.
type forcedLift struct {
	st  int32
	rev bool
	w   float64
	ok  bool
}

// tripPlan is one trip as the search needs it: the waypoints, which of them
// are stops (a via is passed through and cuts nothing), the plan of each pair,
// the rides that are forced, where the car is left, and what to keep off.
type tripPlan struct {
	nodes     []int32
	stop      []bool
	plan      []int
	segPlans  [][]int
	force     []forcedLift
	parkAt    string
	parkSeg   int
	parkPoint int     // the request's index of the parking stop, -1 when mid-leg
	parkLat   float64 // and where it is
	parkLon   float64
	grade     int
	lifts     bool
	avoid     map[int32]bool
}

// trip routes every pair of waypoints and builds one Route out of them.
//
// segPlans, when given, is the plan of each pair (a stop parks the car); when
// nil the modes carry forward and a pair may change mode at a trailhead. Legs
// are closed at STOPS only: a via continues the leg it is in, which is what
// makes dragging a route reshape it instead of chopping it.
func (g *Graph) trip(tp *tripPlan, pen *penalties, st *stats) *Route {
	r := &Route{stretches: map[int32]float64{}, Legs: []*Leg{}, Warnings: []string{},
		parkNode: -1, liftOn: -1}
	cur := tp.plan // the modes still available: a pair starts where the last one ended
	usedLift := false
	usedFerrata := false
	maxSac := 0
	noZ := false
	parked := false
	var span []usedEdge
	closeSpan := func() {
		if len(span) == 0 {
			return
		}
		legs := g.legs(span, &r.stretches)
		for _, lg := range legs {
			if !lg.hasZ {
				noZ = true
			}
			if lg.sac > maxSac {
				maxSac = lg.sac
			}
			if lg.Mode == "lift" {
				usedLift = true
			}
			r.Legs = append(r.Legs, lg)
		}
		// The next pair starts in the mode this span ended in (only when the
		// pairs were not planned in advance).
		if len(tp.segPlans) == 0 && len(legs) > 0 {
			last := legs[len(legs)-1].Mode
			for k, m := range cur {
				if modeName[m] == last {
					cur = cur[k:]
					break
				}
			}
		}
		span = span[:0]
	}
	for i := 0; i+1 < len(tp.nodes); i++ {
		if i < len(tp.segPlans) {
			cur = tp.segPlans[i]
			if tp.parkSeg >= 1 && i == tp.parkSeg {
				// The walk starts here. Whether the car was ever driven is a
				// question for the legs, once there are legs.
				parked = true
			}
		}
		if tp.nodes[i] == tp.nodes[i+1] {
			if tp.stop[i+1] {
				closeSpan()
			}
			continue
		}
		var edges []usedEdge
		if i < len(tp.force) && tp.force[i].ok {
			// A via on a lift line: this pair IS the ride.
			f := tp.force[i]
			edges = []usedEdge{{st: f.st, rev: f.rev, mode: modeHike, w: f.w}}
		} else {
			var ok bool
			edges, ok = g.searchSeg(tp.nodes[i], tp.nodes[i+1], cur, tp.grade, tp.lifts, tp.avoid, pen, st)
			if !ok {
				return nil
			}
		}
		for _, e := range edges {
			switch {
			case e.parked:
				r.Seconds += e.w
				r.parkNode, r.parkPair = e.node, i
			case g.isLift(e.st):
				// Where the first ride is boarded: the station the chain rule
				// asks its question about.
				if r.liftOn < 0 {
					s := g.R.Stretches[e.st]
					on := g.dense[s.A]
					if e.rev {
						on = g.dense[s.B]
					}
					r.liftOn = on
				}
			case g.R.Stretches[e.st].Ferrata:
				usedFerrata = true
			}
			// What was WALKED, in the order it was walked. The end of it is
			// the approach, and the approach is the side of the mountain a
			// day was spent on: see approachOf.
			if e.mode == modeHike && !e.parked && !g.isLift(e.st) {
				r.walkSts = append(r.walkSts, e.st)
			}
		}
		span = append(span, edges...)
		if tp.stop[i+1] {
			closeSpan()
		}
	}
	closeSpan()
	if len(r.Legs) == 0 {
		return nil
	}
	for _, lg := range r.Legs {
		r.Seconds += lg.Seconds
		r.Meters += lg.Meters
		r.Ascent += lg.Ascent
		r.Descent += lg.Descent
		if lg.Mode == modeName[modeHike] {
			// The ride is not the walk: a cable car's 1,200 m of climb is
			// nobody's effort.
			r.WalkMeters += lg.Meters
			r.WalkAscent += lg.Ascent
		}
	}
	// The whole geometry and the whole profile.
	var pts [][2]float64
	var zs []float64
	hasZ := true
	for _, lg := range r.Legs {
		if !lg.hasZ {
			hasZ = false
		}
		for k, p := range lg.pts {
			if k == 0 && len(pts) > 0 {
				continue
			}
			pts = append(pts, p)
			if k < len(lg.zs) {
				zs = append(zs, lg.zs[k])
			} else {
				zs = append(zs, math.NaN())
			}
		}
	}
	r.Geometry = g.geometry(pts)
	if hasZ && len(zs) == len(pts) {
		r.Profile = sampleProfile(pts, zs, 50)
	}
	r.Steps = g.steps(r.Legs)
	r.Grade = GradeName(maxSac)
	r.Seconds = math.Round(r.Seconds)
	r.Meters = math.Round(r.Meters)
	r.Ascent = math.Round(r.Ascent)
	r.Descent = math.Round(r.Descent)

	// Every warning speaks the SAT scale — T, E, EE, EEA — and never the OSM
	// sac numbers: the page shows one scale and a person learns one scale.
	if st.ferrataSkip && tp.grade < 4 {
		r.Warnings = append(r.Warnings, "via ferrata excluded")
	}
	if usedFerrata {
		r.Warnings = append(r.Warnings, "route uses a via ferrata")
	}
	switch {
	case maxSac >= 5:
		// Above EEA there is no letter to print and no path to speak of.
		r.Warnings = append(r.Warnings,
			"alpine terrain beyond EEA: glacier, rope and crampons, not a marked path")
	case maxSac >= 3:
		r.Warnings = append(r.Warnings, "route uses "+gradeArticle(maxSac)+" path")
	}
	if usedLift {
		r.Warnings = append(r.Warnings, "lifts run in season only; check the operator's dates")
	}
	if noZ {
		r.Warnings = append(r.Warnings, "no elevation data")
	}
	if st.gradeSkipped > 0 && st.gradeSkipped <= 4 {
		r.Warnings = append(r.Warnings,
			"paths above grade "+GradeName(tp.grade)+" were excluded")
	}
	// WHERE THE CAR IS LEFT is a fact about the legs, not about the plan. A
	// two-mode trip whose first mode never carried a metre — a stop six
	// metres from the start, a segment that collapsed — is a walk, and a walk
	// has no parking, no parked-at warning and no two minutes of it in the
	// clock. The page badges what this says, so it has to be true.
	if len(tp.plan) >= 2 {
		first, walk := false, false
		for _, lg := range r.Legs {
			switch lg.Mode {
			case modeName[tp.plan[0]]:
				first = true
			case modeName[modeHike]:
				walk = true
			}
		}
		switch {
		case !first || !walk:
			// no switch happened: nothing to say
		case parked:
			// Left at a stop: one the caller named, or — when parkPoint says
			// the caller named none — the one parkLowest put at the foot of a
			// lift chain. The page badges a stop only when Point names one, so
			// a parking the engine chose carries none.
			r.Seconds += switchCost
			name := tp.parkAt
			if name == "" {
				name = "the stop"
				if tp.parkPoint < 1 {
					name = "the trailhead"
				}
			}
			r.Parking = &Parking{Name: tp.parkAt, Lat: tp.parkLat, Lon: tp.parkLon}
			if tp.parkPoint >= 1 {
				idx := tp.parkPoint
				r.Parking.Point = &idx
			}
			r.Warnings = append(r.Warnings, "parked at "+name)
		case r.parkNode >= 0:
			// left at a trailhead the search chose, inside a leg
			lat, lon := g.LatLon(r.parkNode)
			name := g.ParkName(r.parkNode, tp.plan[0])
			r.Parking = &Parking{Name: name, Lat: round6(lat), Lon: round6(lon)}
			if name != "" {
				r.Warnings = append(r.Warnings, "parked at "+name)
			} else {
				r.Warnings = append(r.Warnings, "parked at the trailhead")
			}
		}
	}
	return r
}

// legs cuts a segment's moves into legs at every change of mode, and fills
// each leg's geometry, elevation, classes and grade.
func (g *Graph) legs(edges []usedEdge, used *map[int32]float64) []*Leg {
	var out []*Leg
	var cur *Leg
	for _, e := range edges {
		if e.parked {
			cur = nil // the next stretch opens a new leg in the new mode
			continue
		}
		st := g.R.Stretches[e.st]
		// A ride is a leg of its own, named after the lift: two lifts in a row
		// are two legs, because they are two queues and two tickets.
		mode, liftName, liftType := modeName[e.mode], "", ""
		if int(e.st) < len(g.R.Lift) && g.R.Lift[e.st].Is {
			mode, liftName, liftType = "lift", st.Name, g.R.Lift[e.st].Type
		}
		if cur == nil || cur.Mode != mode || (mode == "lift" && cur.Name != liftName) {
			cur = &Leg{Mode: mode, Name: liftName, LiftType: liftType,
				Classes: map[string]float64{}, hasZ: true}
			out = append(out, cur)
		}
		cur.Seconds += e.w
		cur.Meters += st.Len
		up, down := st.Up, st.Down
		if e.rev {
			up, down = down, up
		}
		cur.rawUp += up
		cur.rawDown += down
		cur.Classes[st.Cls] += st.Len
		if e.mode == modeHike && cur.Mode == "hike" {
			if eg := g.effGrade(e.st); eg > cur.sac {
				cur.sac = eg
			}
		}
		(*used)[e.st] += st.Len
		name := g.stepName(e.st, e.mode)
		if n := len(cur.steps); n > 0 && cur.steps[n-1].Name == name {
			cur.steps[n-1].Meters += st.Len
			cur.steps[n-1].Seconds += e.w
		} else {
			cur.steps = append(cur.steps, Step{Name: name, Mode: mode, Meters: st.Len, Seconds: e.w})
		}

		pts, zs := stretchPts(st, e.rev)
		if zs == nil {
			cur.hasZ = false
		}
		for k, p := range pts {
			if k == 0 && len(cur.pts) > 0 {
				continue
			}
			cur.pts = append(cur.pts, p)
			if zs != nil {
				cur.zs = append(cur.zs, zs[k])
			} else {
				cur.zs = append(cur.zs, math.NaN())
			}
		}
	}
	for _, lg := range out {
		lg.Geometry = g.geometry(lg.pts)
		if lg.hasZ {
			lg.Profile = sampleProfile(lg.pts, lg.zs, 50)
			// The climb is integrated over the LEG's elevation line, not
			// summed per stretch. Summing per stretch counts the surface
			// model's noise once per stretch and never lets it cancel: the
			// flat cycle path from Bolzano to Merano came out at 152 m of
			// climb over 30 km whose two ends differ by 63. Integrating the
			// whole line with a dead band lets the wiggle cancel across the
			// joins and leaves the hill.
			lg.Ascent, lg.Descent = climbOf(lg.zs, deadBand(lg.Classes, lg.Meters))
		} else {
			lg.Ascent, lg.Descent = lg.rawUp, lg.rawDown
		}
		lg.Grade = ""
		if lg.Mode == "hike" {
			lg.Grade = GradeName(lg.sac)
		}
		lg.Seconds = math.Round(lg.Seconds)
		lg.Meters = math.Round(lg.Meters)
		lg.Ascent = math.Round(lg.Ascent)
		lg.Descent = math.Round(lg.Descent)
		for k, v := range lg.Classes {
			lg.Classes[k] = math.Round(v)
		}
	}
	return out
}

// deadBand is how much a line must move before it counts as a climb: 20 m on
// a road, where the DEM is a surface model over open ground and the ride is
// nearly flat, 10 m on a path, where the steps are real and the canopy is the
// lesser evil. It follows the ground the leg is actually on, not its mode: a
// walk along a valley road is a road.
func deadBand(classes map[string]float64, meters float64) float64 {
	rough := 0.0
	for cls, m := range classes {
		switch cls {
		case "path", "track", "pedestrian", "steps":
			rough += m
		}
	}
	if meters > 0 && rough/meters > 0.5 {
		return 10
	}
	return roadDeadBand
}

// climbOf integrates an elevation line into metres up and metres down, with a
// dead band, and makes the two ends count whole however the line wandered.
func climbOf(zs []float64, hyst float64) (up, down float64) {
	if len(zs) < 2 {
		return 0, 0
	}
	last := zs[0]
	for _, z := range zs[1:] {
		if math.IsNaN(z) {
			return 0, 0
		}
		if z > last+hyst {
			up += z - last
			last = z
		} else if z < last-hyst {
			down += last - z
			last = z
		}
	}
	if net, d := zs[len(zs)-1]-zs[0], up-down; net > d {
		up += net - d
	} else if net < d {
		down += d - net
	}
	return up, down
}

// stretchPts is the stretch's polyline and elevations oriented the way it is
// travelled.
func stretchPts(st *city.Stretch, rev bool) ([][2]float64, []float64) {
	if !rev {
		return st.Pts, st.Z
	}
	pts := make([][2]float64, len(st.Pts))
	for i, p := range st.Pts {
		pts[len(pts)-1-i] = p
	}
	if st.Z == nil {
		return pts, nil
	}
	zs := make([]float64, len(st.Z))
	for i, z := range st.Z {
		zs[len(zs)-1-i] = z
	}
	return pts, zs
}

// steps merges consecutive stretches that share a name into the directions,
// then folds away the crumbs.
func (g *Graph) steps(legs []*Leg) []Step {
	var out []Step
	for _, lg := range legs {
		for _, s := range lg.steps {
			if n := len(out); n > 0 && out[n-1].Name == s.Name && out[n-1].Mode == s.Mode {
				out[n-1].Meters += s.Meters
				out[n-1].Seconds += s.Seconds
				continue
			}
			out = append(out, s)
		}
	}
	out = mergeSteps(out, minStepM, maxSteps)
	for i := range out {
		out[i].Meters = math.Round(out[i].Meters)
		out[i].Seconds = math.Round(out[i].Seconds)
	}
	return out
}

// mergeSteps folds a step shorter than minM into a neighbour of the same mode,
// shortest first, and keeps folding until no crumb is left and the list is at
// most maxN long. The surviving name is the one with more metres behind it: a
// 6 km walk that opens "90 m, 10 m, 30 m" tells the reader nothing, and the
// junction it names is not the one they are looking for.
func mergeSteps(in []Step, minM float64, maxN int) []Step {
	out := append([]Step(nil), in...)
	sameMode := func(i, j int) bool {
		return j >= 0 && j < len(out) && out[i].Mode == out[j].Mode
	}
	for len(out) > 1 {
		// The shortest step that is too short (or over the cap) AND has a
		// neighbour of its own mode to go into. A lift is never a crumb: it is
		// a queue, a ticket and the name the reader is looking for.
		worst, worstM := -1, math.Inf(1)
		for i, s := range out {
			if s.Mode == "lift" || (s.Meters >= minM && len(out) <= maxN) {
				continue
			}
			if !sameMode(i, i-1) && !sameMode(i, i+1) {
				continue
			}
			if s.Meters < worstM {
				worst, worstM = i, s.Meters
			}
		}
		if worst < 0 {
			break
		}
		// its neighbour: the same mode, the longer of the two.
		into := -1
		if sameMode(worst, worst-1) {
			into = worst - 1
		}
		if sameMode(worst, worst+1) && (into < 0 || out[worst+1].Meters > out[into].Meters) {
			into = worst + 1
		}
		keep := out[into]
		if out[worst].Meters > keep.Meters {
			keep.Name = out[worst].Name
		}
		keep.Meters += out[worst].Meters
		keep.Seconds += out[worst].Seconds
		out[into] = keep
		out = append(out[:worst], out[worst+1:]...)
	}
	return out
}

// geometry turns a polyline in metres into GeoJSON.
func (g *Graph) geometry(pts [][2]float64) Geometry {
	co := make([][2]float64, 0, len(pts))
	for _, p := range pts {
		lat, lon := g.R.LatLon(p[0], p[1])
		co = append(co, [2]float64{round6(lon), round6(lat)})
	}
	return Geometry{Type: "LineString", Coordinates: co}
}

// sampleProfile walks a polyline and emits [distance, elevation] every step
// metres, the two ends always included.
func sampleProfile(pts [][2]float64, zs []float64, step float64) [][2]float64 {
	if len(pts) < 2 || len(zs) != len(pts) {
		return nil
	}
	for _, z := range zs {
		if math.IsNaN(z) {
			return nil
		}
	}
	out := [][2]float64{{0, round1(zs[0])}}
	cum, target := 0.0, step
	for i := 1; i < len(pts); i++ {
		seg := math.Hypot(pts[i][0]-pts[i-1][0], pts[i][1]-pts[i-1][1])
		if seg <= 0 {
			continue
		}
		for cum+seg >= target {
			t := (target - cum) / seg
			z := zs[i-1] + (zs[i]-zs[i-1])*t
			out = append(out, [2]float64{math.Round(target), round1(z)})
			target += step
		}
		cum += seg
	}
	if last := out[len(out)-1][0]; last < cum-1 {
		out = append(out, [2]float64{math.Round(cum), round1(zs[len(zs)-1])})
	}
	return out
}

// isMotorway is the class pair a ramp connects.
func isMotorway(cls string) bool { return cls == "motorway" || cls == "trunk" }

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }

// stepName is what the directions call a stretch, in the mode that is on it.
//
// The mode is not decoration. A trail number names a WALK: an unnamed
// unclassified road that happens to carry hiking_ref=608B is a paved public
// road, and "in 700 m, trail 608B" told a driver to turn onto a footpath.
// A marked trail runs along roads for a while — that is what a valley
// approach IS — so the ref is on the road whether or not anyone drives it.
// For a car and a bicycle the order is the road's own name, then its road
// ref, then a word for what it is; only a walking step is named by a trail.
func (g *Graph) stepName(st int32, mode int) string {
	s := g.R.Stretches[st]
	if s.Name != "" {
		return s.Name
	}
	if mode == modeHike {
		if s.HikeRef != "" {
			return "trail " + s.HikeRef
		}
		if int(st) < len(g.R.Sat) {
			if g.R.Sat[st].No != "" {
				return "SAT " + g.R.Sat[st].No
			}
			if g.R.Sat[st].Name != "" {
				return g.R.Sat[st].Name
			}
		}
	}
	// The ref of the route this mode is actually following: SP/SS for a car
	// (not in the region file yet — build-city.py keeps no OSM `ref`, so this
	// reads as empty until it does), the cycle network's number for a bike.
	if s.Ref != "" {
		return s.Ref
	}
	if mode == modeBike && s.BikeRef != "" {
		return "cycle route " + s.BikeRef
	}
	// Nothing names it: say what it is, in the vocabulary of the mode. A
	// reader skims "road, lane, track" from a car and "path, track, street"
	// on foot; neither skims "unnamed tertiary".
	if mode == modeCar || mode == modeBike {
		switch s.Cls {
		case "motorway", "trunk", "primary", "secondary", "tertiary":
			return "road"
		case "track":
			return "track"
		case "path", "steps", "pedestrian", "cycleway":
			return "path"
		}
		return "lane"
	}
	switch s.Cls {
	case "path", "steps", "pedestrian", "cycleway":
		return "path"
	case "track":
		return "track"
	}
	return "street"
}

// nameMode is the mode an avoided stretch is named for. An avoid list is drawn
// on the map rather than travelled, so a road there is named as a road and a
// footpath as a footpath.
func nameMode(cls string) int {
	switch cls {
	case "path", "steps", "pedestrian":
		return modeHike
	}
	return modeCar
}
