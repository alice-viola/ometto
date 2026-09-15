// Package city is the Trento map, shared by every role of the two-sided build.
//
// It is a COPY of the geometry of the Trento traffic prototype that this
// repository used to hold (cmd/trento), lifted out
// so fleets, receivers, citymind and citygw all resolve the same stretch to the
// same metres. The originals stay where they are and keep running the
// single-sided simulation; the rule there was copy, not
// move. Place, the Dijkstra, the path resolution and the seeding allocation are
// byte-identical to cmd/trento: a car placed by one and drawn by the other
// lands on the same pixel.
//
// What is NEW here is what the two-sided build needs and the single-sided one
// never did: tiles (the feed's unit of interest), junctions with their light
// plans, the railway lines the trains run on, and the level crossings where
// those lines cut a road.
package city

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"strconv"
)

// cityDoc is web/public/city.json as tools/build-city.py writes it.
//
// "points" is the shared coordinate table in metres and a road's "p" is a list
// of INDEXES into it. "rails" is not: each rail is a list of [x,y] pairs
// already in metres. Reading one as the other is the single easiest way to put
// a train in the wrong valley.
type cityDoc struct {
	Meta struct {
		Extent Extent `json:"extent"`
	} `json:"meta"`
	Points [][2]float64 `json:"points"`
	Roads  []struct {
		C string `json:"c"`
		P []int  `json:"p"`
		O int    `json:"o"`
		N string `json:"n"`
		// B and T are the bridge and tunnel flags. cmd/trento never read them;
		// a level crossing needs them, because a road that crosses a railway on
		// a bridge or under it does not cross it at all.
		B int `json:"b"`
		T int `json:"t"`
		// R marks a roundabout way (OSM junction=roundabout): one-way ring,
		// circulating traffic has priority over entering traffic.
		R int `json:"r"`
		// The hiking and cycling attributes (province builds only): sac_scale
		// 1..6, the marked trail's number, the bike route's number, via
		// ferrata, steps, mtb:scale, bicycle and foot access (-1 forbidden,
		// 1 allowed or designated, 0 unknown).
		S  int    `json:"s"`
		HR string `json:"hr"`
		BR string `json:"br"`
		V  int    `json:"v"`
		ST int    `json:"st"`
		M  int    `json:"m"`
		BK int    `json:"bk"`
		FT int    `json:"ft"`
	} `json:"roads"`
	Rails [][][2]float64 `json:"rails"`
	// Z is the elevation of every point in metres (tools/dem.py), optional.
	Z []float64 `json:"z"`
}

// Extent is the rectangle the map was projected into, in metres.
type Extent struct{ MinX, MaxX, MinY, MaxY float64 }

// A Stretch of road: one partition of the queue.
type Stretch struct {
	ID   string
	Pts  [][2]float64 // polyline in metres
	Cum  []float64    // arc length to each point
	Len  float64      // total length in metres
	A, B int          // the junction points it runs between
	Cls  string
	One  bool
	Name string
	// Bridge and Tunnel say the stretch is not at ground level here.
	Bridge bool
	Tunnel bool
	// Roundabout says the stretch is part of a roundabout ring.
	Roundabout bool
	// The hiking and cycling attributes, zero when the map has none.
	Sac        int       // sac_scale 1..6 (T1 hiking .. T6 difficult alpine)
	Ref        string    // the ROAD's number — SP 235, SS 45bis — "" when it has none
	HikeRef    string    // the marked trail's number, "" when unmarked
	BikeRef    string    // the bike route's number
	Ferrata    bool      // highway=via_ferrata
	Steps      bool      // highway=steps
	MTB        int       // mtb:scale, -1 when untagged
	BikeAccess int       // -1 forbidden, 1 allowed, 0 unknown
	FootAccess int       // -1 forbidden, 1 allowed, 0 unknown
	Z          []float64 // elevation at every point, nil without a DEM
	Up, Down   float64   // metres climbed and descended driving A -> B
}

// Place resolves a distance along a lane into a position and a heading.
// Lane 0 runs a->b, lane 1 runs b->a, and each sits to the right of the centre.
func (s *Stretch) Place(lane int, pos float64) (x, y, hdg float64) {
	// A car queued at the entrance has a negative pos: it is still in the
	// junction, waiting for room. It is drawn back along the lane's first
	// stretch of road rather than clamped onto the car in front of it.
	if pos < 0 {
		x0, y0, h0 := s.Place(lane, 0)
		return x0 - math.Cos(h0)*(-pos), y0 - math.Sin(h0)*(-pos), h0
	}
	d := pos
	if lane == 1 {
		d = s.Len - pos
	}
	if d < 0 {
		d = 0
	}
	if d > s.Len {
		d = s.Len
	}
	i := sort.SearchFloat64s(s.Cum, d)
	if i < 1 {
		i = 1
	}
	if i >= len(s.Cum) {
		i = len(s.Cum) - 1
	}
	span := s.Cum[i] - s.Cum[i-1]
	t := 0.0
	if span > 1e-9 {
		t = (d - s.Cum[i-1]) / span
	}
	ax, ay := s.Pts[i-1][0], s.Pts[i-1][1]
	bx, by := s.Pts[i][0], s.Pts[i][1]
	px, py := ax+(bx-ax)*t, ay+(by-ay)*t
	tx, ty := bx-ax, by-ay
	l := math.Hypot(tx, ty)
	if l < 1e-9 {
		tx, ty, l = 1, 0, 1
	}
	tx, ty = tx/l, ty/l
	if lane == 1 {
		tx, ty = -tx, -ty
	}
	const off = 2.4 // half a carriageway
	return px + -ty*off, py + tx*off, math.Atan2(ty, tx)
}

// ExitNode is the junction a lane arrives at.
func (s *Stretch) ExitNode(lane int) int {
	if lane == 1 {
		return s.A
	}
	return s.B
}

// EntryNode is the junction a lane starts from.
func (s *Stretch) EntryNode(lane int) int {
	if lane == 1 {
		return s.B
	}
	return s.A
}

// Drivable reports a stretch cars may use. Footways and steps are not for cars.
func (s *Stretch) Drivable() bool { return s.Cls != "pedestrian" }

// An Exit is a stretch leaving a junction, with the lane to take.
type Exit struct {
	S    *Stretch
	Lane int
}

// Map is the whole city: its roads, its routes and its railways.
type Map struct {
	Stretches []*Stretch
	ByID      map[string]*Stretch
	Pts       [][2]float64
	Extent    Extent
	Paths     []*Path
	// Out[node] are the stretches leaving that junction, with the lane to take.
	Out map[int][]Exit

	rails [][][2]float64
}

// Load reads the map and resolves the routes cars drive.
//
//	cm, err := city.Load("web/public/city.json", "data/paths.json")
func Load(cityPath, pathsPath string) (*Map, error) {
	cm, err := loadCity(cityPath, false)
	if err != nil {
		return nil, err
	}
	if cm.Paths, err = loadPaths(pathsPath, cm); err != nil {
		return nil, err
	}
	return cm, nil
}

// LoadCity loads the map without routes and with EVERY class of way, footways
// and trails included: the pathfinder decides per mode what is usable.
func LoadCity(path string) (*Map, error) { return loadCity(path, true) }

func loadCity(path string, all bool) (*Map, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc cityDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}

	cm := &Map{ByID: map[string]*Stretch{}, Out: map[int][]Exit{}, Pts: doc.Points, rails: doc.Rails}
	cm.Extent = doc.Meta.Extent
	for i, r := range doc.Roads {
		// Footways and steps are not for cars.
		if (!all && r.C == "pedestrian") || len(r.P) < 2 {
			continue
		}
		pts := make([][2]float64, len(r.P))
		for k, pi := range r.P {
			if pi < 0 || pi >= len(doc.Points) {
				pts = nil
				break
			}
			pts[k] = doc.Points[pi]
		}
		if pts == nil {
			continue
		}
		cum := make([]float64, len(pts))
		for k := 1; k < len(pts); k++ {
			cum[k] = cum[k-1] + math.Hypot(pts[k][0]-pts[k-1][0], pts[k][1]-pts[k-1][1])
		}
		total := cum[len(cum)-1]
		// Short stretches are KEPT. Dropping stubs under 6 m looked harmless
		// with random turns, but on a one-way carriageway a stub is a link in
		// the only chain there is: removing one cut the A22 southbound and the
		// Tangenziale in two, and no path could be driven along them.
		if total < 0.5 {
			continue
		}
		s := &Stretch{
			ID: "s" + strconv.Itoa(i), Pts: pts, Cum: cum, Len: total,
			A: r.P[0], B: r.P[len(r.P)-1], Cls: r.C, One: r.O == 1, Name: r.N, Roundabout: r.R == 1,
			Bridge: r.B == 1, Tunnel: r.T == 1,
			Sac: r.S, HikeRef: r.HR, BikeRef: r.BR, Ferrata: r.V == 1, Steps: r.ST == 1,
			MTB: -1, BikeAccess: r.BK, FootAccess: r.FT,
		}
		if r.M > 0 {
			s.MTB = r.M
		}
		if len(doc.Z) == len(doc.Points) && len(doc.Z) > 0 {
			s.Z = make([]float64, len(r.P))
			for k, pi := range r.P {
				s.Z[k] = doc.Z[pi]
			}
			// The DEM is a surface model: roofs, bridges and tree canopy put
			// 10 to 20 m bumps on a flat road. Count a climb or a descent only
			// once it has run 10 m past the last turning point, so the bumps
			// cancel and the real hill is what remains.
			const hyst = 10.0
			last := s.Z[0]
			for k := 1; k < len(s.Z); k++ {
				if s.Z[k] > last+hyst {
					s.Up += s.Z[k] - last
					last = s.Z[k]
				} else if s.Z[k] < last-hyst {
					s.Down += last - s.Z[k]
					last = s.Z[k]
				}
			}
			// The stretch's own end-to-end rise always counts, bumps or not.
			if net := s.Z[len(s.Z)-1] - s.Z[0]; net > s.Up-s.Down {
				s.Up += net - (s.Up - s.Down)
			} else if net < s.Up-s.Down {
				s.Down += (s.Up - s.Down) - net
			}
		}
		cm.Stretches = append(cm.Stretches, s)
		cm.ByID[s.ID] = s
		cm.Out[s.A] = append(cm.Out[s.A], Exit{s, 0})
		if !s.One {
			cm.Out[s.B] = append(cm.Out[s.B], Exit{s, 1})
		}
	}
	return cm, nil
}

// Limit is the speed limit of a road class, in m/s. Copied from cmd/trento's
// step: the free-flow target a car drives at when nothing is in its way.
func Limit(cls string) float64 {
	switch cls {
	case "motorway", "trunk":
		return 27.7
	case "primary", "secondary":
		return 16.6
	case "service":
		return 8.3
	default:
		return 13.9 // 50 km/h
	}
}

// Major reports the road classes whose speed is worth sampling: the prototypes'
// section 2, "major roads".
func Major(cls string) bool {
	switch cls {
	case "motorway", "trunk", "primary", "secondary":
		return true
	}
	return false
}

// inside reports whether a node is within the area the map was downloaded for.
func (cm *Map) inside(n int) bool {
	if n < 0 || n >= len(cm.Pts) {
		return false
	}
	p := cm.Pts[n]
	e := cm.Extent
	return p[0] >= e.MinX && p[0] <= e.MaxX && p[1] >= e.MinY && p[1] <= e.MaxY
}
