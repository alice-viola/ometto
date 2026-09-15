// Package route is the routing engine of the router service: the map, the
// graph per mode, the search, the geocoder.
//
// It is a COPY of the mode model and the in-memory Dijkstra of
// the pathfinder prototype (modes.go and inmem.go), which is no longer in
// this repository, lifted out so the new product can
// grow multi-stop trips, alternatives, SAT grades, elevation profiles and
// turn-by-turn steps without touching the live demo. The rule of
// the rule that governed them applies here too: copy, not move.
//
// What is new: the map is REGION wide (the Trentino build is 365k ways over
// 2.6M points), so the graph is a CSR (compressed adjacency) over dense node
// ids rather than a map per junction, the search is A* with the admissible
// bound that was already in modes.go, and a click snaps through a uniform
// grid instead of a scan over every junction.
package route

import (
	"encoding/json"
	"math"
	"os"
	"strconv"

	"ometto/internal/city"
)

// regionDoc is web/public/<region>.json as tools/build-city.py writes it.
// Only the four keys the router needs are declared; the rest of the document
// (junctions, rails, water, areas) is skipped by the decoder.
//
// The three SAT fields are the ones the data agent adds for the region build:
// the grade of a marked trail ("T", "E", "EE", "EEA"), its number and its
// name. They are optional, and a map without them loads exactly as before.
type regionDoc struct {
	Meta struct {
		Name   string `json:"name"`
		Origin struct {
			Lat float64 `json:"lat"`
			Lon float64 `json:"lon"`
		} `json:"origin"`
		MPerDegLat float64     `json:"mPerDegLat"`
		MPerDegLon float64     `json:"mPerDegLon"`
		Extent     city.Extent `json:"extent"`
	} `json:"meta"`
	Points [][2]float64 `json:"points"`
	Roads  []struct {
		C  string  `json:"c"`
		P  []int32 `json:"p"`
		O  int     `json:"o"`
		N  string  `json:"n"`
		B  int     `json:"b"`
		T  int     `json:"t"`
		R  int     `json:"r"`
		S  int     `json:"s"`
		RF string  `json:"ref"`
		HR string  `json:"hr"`
		BR string  `json:"br"`
		V  int     `json:"v"`
		ST int     `json:"st"`
		M  int     `json:"m"`
		BK int     `json:"bk"`
		FT int     `json:"ft"`

		Sat     string `json:"sat"`
		SatNo   string `json:"satno"`
		SatName string `json:"satname"`

		// A lift (class "lift"): its aerialway type, the ride in seconds when
		// the operator publishes one, and whether it only runs uphill.
		LT  string  `json:"lt"`
		Dur float64 `json:"dur"`
		OW  int     `json:"ow"`
	} `json:"roads"`
	Z []float64 `json:"z"`
}

// Sat is the marked-trail information of a stretch, empty when the map has
// none. Grade is the SAT letter turned into the same 1..4 scale the OSM
// sac_scale is compared on: T=1, E=2, EE=3, EEA=4.
type Sat struct {
	Grade int
	No    string
	Name  string
}

// Lift is what a stretch of class "lift" carries: an aerialway, its type, the
// published ride time (0 when there is none) and whether it runs uphill only.
// The table is parallel to Stretches, like Sat.
type Lift struct {
	Is     bool
	Type   string
	Dur    float64
	Uphill bool
}

// Region is a loaded map: the geometry, the projection that turns WGS84 into
// the metres the geometry is in, and the per-stretch SAT table.
type Region struct {
	File       string
	Name       string
	Lat0, Lon0 float64
	MPerDegLat float64
	MPerDegLon float64
	Extent     city.Extent

	Pts       [][2]float64
	PtZ       []float64 // elevation per point, nil without a DEM: the links need it
	Stretches []*city.Stretch
	Sat       []Sat  // parallel to Stretches
	Lift      []Lift // parallel to Stretches

	WithElevation int // stretches that carry a DEM profile
	Lifts         int // stretches of class "lift"
	Links         int // synthetic stretches joining a lift to the footpaths
}

// SatGrade turns a SAT letter into the numeric grade the search compares on.
// An unknown or empty letter is 0: "not declared", which passes every limit,
// exactly like an untagged sac_scale.
func SatGrade(s string) int {
	switch s {
	case "T", "t":
		return 1
	case "E", "e":
		return 2
	case "EE", "ee":
		return 3
	case "EEA", "eea":
		return 4
	case "A", "a", "ALP", "alp", "Alpine", "alpine":
		// Above the SAT scale: sac 5 and 6, where a party ropes up. It has no
		// letter on a signpost, which is the point.
		return 5
	}
	return 0
}

// GradeName is the inverse: the SAT letter of a numeric grade.
func GradeName(g int) string {
	switch {
	case g <= 0:
		return ""
	case g == 1:
		return "T"
	case g == 2:
		return "E"
	case g == 3:
		return "EE"
	case g == 4:
		return "EEA"
	default:
		return "A"
	}
}

// Load reads a region map. Every class of way is kept, footways and trails
// included: the graph decides per mode what is usable.
func Load(path string) (*Region, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var doc regionDoc
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil, err
	}
	raw = nil

	r := &Region{
		File: path, Name: doc.Meta.Name,
		Lat0: doc.Meta.Origin.Lat, Lon0: doc.Meta.Origin.Lon,
		MPerDegLat: doc.Meta.MPerDegLat, MPerDegLon: doc.Meta.MPerDegLon,
		Extent: doc.Meta.Extent, Pts: doc.Points,
	}
	if r.MPerDegLat == 0 {
		r.MPerDegLat = 111132.0
	}
	if r.MPerDegLon == 0 {
		r.MPerDegLon = 111320.0 * math.Cos(r.Lat0*math.Pi/180)
	}
	hasZ := len(doc.Z) == len(doc.Points) && len(doc.Z) > 0
	if hasZ {
		r.PtZ = doc.Z
	}

	r.Stretches = make([]*city.Stretch, 0, len(doc.Roads))
	r.Sat = make([]Sat, 0, len(doc.Roads))
	r.Lift = make([]Lift, 0, len(doc.Roads))
	for i := range doc.Roads {
		rd := &doc.Roads[i]
		if len(rd.P) < 2 {
			continue
		}
		pts := make([][2]float64, len(rd.P))
		ok := true
		for k, pi := range rd.P {
			if pi < 0 || int(pi) >= len(doc.Points) {
				ok = false
				break
			}
			pts[k] = doc.Points[pi]
		}
		if !ok {
			continue
		}
		cum := make([]float64, len(pts))
		for k := 1; k < len(pts); k++ {
			cum[k] = cum[k-1] + math.Hypot(pts[k][0]-pts[k-1][0], pts[k][1]-pts[k-1][1])
		}
		total := cum[len(cum)-1]
		// Short stretches are KEPT: on a one-way carriageway a stub is a link
		// in the only chain there is (internal/city says the same).
		if total < 0.5 {
			continue
		}
		s := &city.Stretch{
			ID: "s" + strconv.Itoa(i), Pts: pts, Cum: cum, Len: total,
			A: int(rd.P[0]), B: int(rd.P[len(rd.P)-1]), Cls: rd.C, One: rd.O == 1,
			Name: rd.N, Roundabout: rd.R == 1, Bridge: rd.B == 1, Tunnel: rd.T == 1,
			Sac: rd.S, Ref: rd.RF, HikeRef: rd.HR, BikeRef: rd.BR, Ferrata: rd.V == 1, Steps: rd.ST == 1,
			MTB: -1, BikeAccess: rd.BK, FootAccess: rd.FT,
		}
		if rd.M > 0 {
			s.MTB = rd.M
		}
		if hasZ {
			s.Z = make([]float64, len(rd.P))
			for k, pi := range rd.P {
				s.Z[k] = doc.Z[pi]
			}
			// The DEM is a surface model: roofs, bridges and canopy put 10 to
			// 20 m bumps on a FLAT road, and a road is where they are worst —
			// a cycle path along the Adige came out with 148 m of climb over
			// 30 flat kilometres. So a road integrates with a 20 m dead band
			// and only a path, a track or a walkway keeps the 10 m one, where
			// the real steps are small and the canopy is the lesser evil.
			hyst := 20.0
			switch rd.C {
			case "path", "track", "pedestrian", "steps":
				hyst = 10.0
			}
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
			if net := s.Z[len(s.Z)-1] - s.Z[0]; net > s.Up-s.Down {
				s.Up += net - (s.Up - s.Down)
			} else if net < s.Up-s.Down {
				s.Down += (s.Up - s.Down) - net
			}
			r.WithElevation++
		}
		r.Stretches = append(r.Stretches, s)
		r.Sat = append(r.Sat, Sat{Grade: SatGrade(rd.Sat), No: rd.SatNo, Name: rd.SatName})
		lf := Lift{Is: rd.C == "lift", Type: rd.LT, Dur: rd.Dur, Uphill: rd.OW == 1}
		if lf.Is {
			r.Lifts++
		}
		r.Lift = append(r.Lift, lf)
	}
	return r, nil
}

// XY projects WGS84 into the map's metres: x east of the origin meridian,
// y SOUTH of the origin parallel (y grows south, as the map is drawn).
func (r *Region) XY(lat, lon float64) (x, y float64) {
	return (lon - r.Lon0) * r.MPerDegLon, (r.Lat0 - lat) * r.MPerDegLat
}

// LatLon is the inverse of XY.
func (r *Region) LatLon(x, y float64) (lat, lon float64) {
	return r.Lat0 - y/r.MPerDegLat, r.Lon0 + x/r.MPerDegLon
}
