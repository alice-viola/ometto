package route

// The mode model, copied from the pathfinder prototype's modes.go (no longer
// in this repository) and left alone except
// where the new product needs more: a label is per junction AND mode, so one
// search answers "drive to the trailhead, park, walk to the summit" as one
// route. A plan is the modes in order; a trip may switch from the first to the
// second once, at any junction, paying the time it takes to park.
//
// What changed here, and only here: via ferrata stays IN the hiking graph and
// is refused at search time by the query's grade (EEA allows it), the way T4
// and T5 already were, and the grade compared is the SAT one when the map
// carries it.

import (
	"math"
	"strings"

	"ometto/internal/city"
)

const (
	modeCar  = 0
	modeBike = 1
	modeHike = 2
	nModes   = 3
)

var modeName = [nModes]string{"car", "bike", "hike"}

// switchCost is parking the car or the bike and putting the boots on.
const switchCost = 120.0

// Lift speeds by aerialway type, in metres per second, for a lift whose
// operator publishes no duration; and the boarding cost every ride pays: the
// walk to the turnstile, the ticket, the queue and the wait for a cabin.
const (
	liftBoardS  = 300.0
	liftFastest = 10.0 // the quickest thing on the map when lifts are allowed
)

func liftSpeed(kind string) float64 {
	switch kind {
	case "cable_car":
		return 10
	case "gondola":
		return 5
	case "chair_lift":
		return 2.5
	case "mixed_lift":
		return 4
	}
	return 4
}

// ParsePlan turns "car", "hike", "car+hike", "bike+hike" into the mode order.
func ParsePlan(mode string) []int {
	var plan []int
	for _, part := range strings.Split(mode, "+") {
		switch strings.TrimSpace(part) {
		case "car":
			plan = append(plan, modeCar)
		case "bike":
			plan = append(plan, modeBike)
		case "hike":
			plan = append(plan, modeHike)
		}
	}
	if len(plan) == 0 {
		plan = []int{modeCar}
	}
	return plan
}

// ValidMode reports a mode string the API accepts.
func ValidMode(mode string) bool {
	switch mode {
	case "car", "bike", "hike", "car+hike", "bike+hike":
		return true
	}
	return false
}

func planName(plan []int) string {
	parts := make([]string, len(plan))
	for i, m := range plan {
		parts[i] = modeName[m]
	}
	return strings.Join(parts, "+")
}

// vmax is the fastest a plan can move, for the admissible bound.
func vmax(plan []int) float64 {
	v := 0.0
	for _, m := range plan {
		switch m {
		case modeCar:
			v = math.Max(v, city.Limit("motorway"))
		case modeBike:
			v = math.Max(v, 40/3.6)
		case modeHike:
			v = math.Max(v, 6/3.6)
		}
	}
	return v
}

// usable says whether a mode may travel a stretch at all. The grade limit is
// NOT applied here: T4, T5 and via ferrata stay in the graph and the query
// refuses them at search time, so one graph serves every grade.
func usable(mode int, st *city.Stretch) bool {
	switch mode {
	case modeCar:
		switch st.Cls {
		case "motorway", "trunk", "primary", "secondary", "tertiary", "residential", "service":
			return true
		}
		return false
	case modeBike:
		if st.BikeAccess < 0 || st.Steps || st.Ferrata || st.Cls == "lift" {
			return false
		}
		switch st.Cls {
		case "primary", "secondary", "tertiary", "residential", "service", "cycleway", "pedestrian":
			return true
		case "track":
			return st.MTB <= 2
		case "path":
			return st.BikeAccess > 0 || (st.MTB >= 0 && st.MTB <= 1)
		}
		return false
	case modeHike:
		// Every grade stays in the graph — T1 to the sac 6 of a glacier ridge
		// — and the query's grade limit refuses at search time what it will
		// not walk. A lift is never walked: it has its own layer.
		if st.FootAccess < 0 || st.Cls == "lift" {
			return false
		}
		switch st.Cls {
		case "path", "track", "pedestrian", "cycleway", "residential", "service", "tertiary", "secondary", "primary":
			return true
		}
		return st.Ferrata || st.Steps
	}
	return false
}

// weight is the seconds a mode needs along a stretch in one direction; rev
// means driving B -> A, which swaps climb and descent. marked says the
// stretch is a marked trail — an OSM hiking ref or a SAT number — which only
// the walking clock cares about.
func weight(mode int, st *city.Stretch, rev, marked bool) float64 {
	up, down := st.Up, st.Down
	if rev {
		up, down = down, up
	}
	switch mode {
	case modeCar:
		return st.Len / city.Limit(st.Cls)
	case modeBike:
		// A cyclist's road: cycle paths first, quiet streets next, main roads
		// only when there is nothing else. The speeds are what a touring
		// cyclist actually holds, measured against published rides on the
		// Adige cycle paths (Trento-Rovereto 25.2 km in 95 min, Bolzano-Merano
		// 30.3 in 120, Bolzano-Ora 19.1 in 76): 15 to 16 km/h door to door,
		// not the 21 the first model reported.
		//
		// A marked bike route carries NO bonus. A bonus would make the
		// reported time faster than the ride, which is the one error a
		// planner must not make; the preference is expressed as a small
		// penalty on everything else instead.
		base, factor := 15.0, 1.0
		switch st.Cls {
		case "cycleway":
			base = 16
		case "primary":
			base, factor = 15, 1.6
		case "secondary":
			base, factor = 15, 1.25
		case "tertiary":
			base = 15
		case "track":
			base = 12
		case "path":
			base = 8
		case "pedestrian":
			base = 8
		}
		if st.BikeRef == "" {
			factor *= 1.05
		}
		slope := 0.0
		if st.Len > 0 {
			slope = math.Max(-0.3, math.Min(0.3, (up-down)/st.Len))
		}
		v := base
		if slope > 0 {
			v = base / (1 + 12*slope)
		} else if slope < 0 {
			v = math.Min(base*(1-3*slope), 40)
		}
		return factor * st.Len / (v / 3.6)
	case modeHike:
		// The Alpine clubs' walking-time rule (SAC, and DIN 33466 with the
		// steeper rates): 4 km/h on the flat, 400 m/h up, 800 m/h down; the
		// larger of the two counts whole, the smaller half.
		h := st.Len / (4000.0 / 3600)
		v := up/(400.0/3600) + down/(800.0/3600)
		t := math.Max(h, v) + math.Min(h, v)/2
		// The factor on the clock is what decides between two ways to the
		// same place. A marked trail is the one to take, at ×0.9. An
		// UNMARKED path or track walks at ×1.2, the same as a street: at the
		// rule's own pace (×1.0) a forest track beat the numbered trail
		// beside it whenever it was a little shorter, and Trento → Monte
		// Stivo walked 20 km of tracks and provincial road before joining
		// SAT 623 under the summit. At ×1.2 a marked trail wins until it is
		// a third longer. Measured on the reference suite (tests/run-routes.py):
		// every verdict is unchanged at ×1.2; ×1.3 or more turns the Cima Tosa
		// row into a "check". A main road is ×1.5: nobody walks the SS45bis
		// for pleasure.
		switch {
		case marked:
			t *= 0.9
		case st.Cls == "primary":
			t *= 1.5
		default:
			t *= 1.2
		}
		// Above EEA the Alpine club rule stops describing the ground: sac 5 is
		// a glacier or a ridge where a party ropes up and moves at half pace,
		// sac 6 is climbing. The times are stretched rather than invented —
		// x1.6 and x2.0 — which puts a sac 5 crossing at about 250 m of
		// ascent an hour instead of 400.
		switch st.Sac {
		case 5:
			t *= 1.6
		case 6:
			t *= 2.0
		}
		return t
	}
	return math.Inf(1)
}
