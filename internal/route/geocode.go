package route

// The geocoder: one in-memory index over the OSM place nodes, the peaks, huts,
// passes and crags, every distinct street name per settlement, and every
// marked trail number. Case- and accent-insensitive, prefix first and substring
// after, ranked by what a person means when they type three letters.

import (
	"encoding/json"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

// Entry is one searchable thing.
type Entry struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Kind     string  `json:"kind"` // place|peak|hut|pass|crag|street|trail
	Locality string  `json:"locality"`
	Lat      float64 `json:"lat"`
	Lon      float64 `json:"lon"`
	Ele      float64 `json:"ele,omitempty"`
	Place    string  `json:"place,omitempty"` // the OSM place type: city, village, hamlet
	// Detail is the one line a crag answers with — its grade span, its aspect
	// and how many routes it has — as far as the mapping says. Empty otherwise.
	Detail string `json:"detail,omitempty"`

	norm   string
	alts   []string // the halves of a bilingual name, each a name in its own right
	also   []string // the names of the rows merged into this one
	toks   []string
	weight float64 // population for a place, elevation for a summit, routes for a crag
}

// Geocoder is the index.
type Geocoder struct {
	entries []*Entry
	index   []tokRef // every token of every entry, sorted
	// Merged is how many rows were folded into another as the same hut.
	Merged int
}

type tokRef struct {
	tok string
	id  int32
}

// osmDoc is data/osm-<region>/places.json and pois.json: an Overpass answer
// trimmed to the nodes and the tags the product uses.
type osmDoc struct {
	Elements []struct {
		ID   int64   `json:"id"`
		Lat  float64 `json:"lat"`
		Lon  float64 `json:"lon"`
		Tags struct {
			Name       string      `json:"name"`
			Place      string      `json:"place"`
			Kind       string      `json:"kind"`
			Population string      `json:"population"`
			Ele        json.Number `json:"ele"`
		} `json:"tags"`
	} `json:"elements"`
}

// BuildGeocoder indexes the places, the POIs, the GeoJSON layers and the
// map's own names. Missing files are not an error: the index simply holds
// less.
//
// geojson is the served layers (huts.geojson, pois.geojson). They are indexed
// as well as the OSM extract because they carry what the extract does not: the
// Province's hut register, where "Rifugio Tosa Tommaso Pedrotti" lives. OSM
// maps that building as a way, so it is in no node file at all, and a planner
// that cannot find the hut everybody walks to is a planner nobody uses.
func BuildGeocoder(g *Graph, placesPath, poisPath string, geojson ...string) (*Geocoder, error) {
	gc := &Geocoder{}

	var places osmDoc
	if placesPath != "" {
		if raw, err := os.ReadFile(placesPath); err == nil {
			if err := json.Unmarshal(raw, &places); err != nil {
				return nil, err
			}
		}
	}
	// A grid over the settlements, so a street can name its locality without
	// scanning six thousand places per stretch.
	var refs []placeRef
	for _, el := range places.Elements {
		n := strings.TrimSpace(el.Tags.Name)
		if n == "" {
			continue
		}
		pop, _ := strconv.Atoi(el.Tags.Population)
		e := &Entry{
			ID: "p:" + strconv.FormatInt(el.ID, 10), Name: n, Kind: "place",
			Lat: el.Lat, Lon: el.Lon, Place: el.Tags.Place, weight: float64(pop),
		}
		gc.entries = append(gc.entries, e)
		x, y := g.R.XY(el.Lat, el.Lon)
		rank := 0
		switch el.Tags.Place {
		case "city", "town", "village", "suburb":
			rank = 2
		case "hamlet", "quarter", "neighbourhood":
			rank = 1
		}
		refs = append(refs, placeRef{x, y, n, rank})
	}
	// locality of a place: the nearest bigger settlement, for a hamlet.
	locOf := newPlaceGrid(refs, 2000)
	for i, e := range gc.entries {
		switch e.Place {
		case "city", "town", "village":
		default:
			if n, ok := locOf.nearest(refs, i, 8000, 2); ok {
				e.Locality = n
			}
		}
	}

	var pois osmDoc
	if poisPath != "" {
		if raw, err := os.ReadFile(poisPath); err == nil {
			if err := json.Unmarshal(raw, &pois); err != nil {
				return nil, err
			}
		}
	}
	for _, el := range pois.Elements {
		n := strings.TrimSpace(el.Tags.Name)
		if n == "" {
			continue
		}
		kind := el.Tags.Kind
		switch kind {
		case "peak", "hut", "pass", "parking":
		default:
			continue
		}
		ele, _ := el.Tags.Ele.Float64()
		e := &Entry{
			ID: kind[:2] + ":" + strconv.FormatInt(el.ID, 10), Name: n, Kind: kind,
			Lat: el.Lat, Lon: el.Lon, Ele: ele, weight: ele,
		}
		x, y := g.R.XY(el.Lat, el.Lon)
		e.Locality = locOf.locality(refs, x, y, 5000)
		gc.entries = append(gc.entries, e)
	}

	for _, path := range geojson {
		if path == "" {
			continue
		}
		gc.addGeoJSON(path, g, refs, locOf)
	}

	// Street names: one entry per distinct name per settlement, at the
	// midpoint of the longest stretch that carries it.
	type group struct {
		st   *Entry
		best float64
	}
	streets := map[string]*group{}
	trails := map[string]*group{}
	for si, st := range g.R.Stretches {
		mid := st.Pts[len(st.Pts)/2]
		if st.Cls == "lift" {
			continue // an aerialway is not a street
		}
		if st.Name != "" {
			loc := locOf.locality(refs, mid[0], mid[1], 3000)
			// One entry per distinct name per settlement — and the name is
			// canonical, so "Pista ciclabile Bolzano-Merano - Radweg
			// Meran-Bozen" and "Radweg Meran-Bozen - Pista ciclabile
			// Bolzano-Merano" are one street and not two. A named cycle route
			// is one object for the whole region: it runs through thirty
			// localities and a search for it wants one line, not thirty.
			key := canonicalName(st.Name) + "\x00" + loc
			if st.Cls == "cycleway" {
				key = canonicalName(st.Name)
			}
			gr := streets[key]
			if gr == nil {
				lat, lon := g.R.LatLon(mid[0], mid[1])
				gr = &group{st: &Entry{
					ID: "s:" + strconv.Itoa(len(streets)), Name: st.Name, Kind: "street",
					Locality: loc, Lat: round6(lat), Lon: round6(lon),
				}}
				streets[key] = gr
			}
			if st.Len > gr.best {
				gr.best = st.Len
				lat, lon := g.R.LatLon(mid[0], mid[1])
				gr.st.Lat, gr.st.Lon = round6(lat), round6(lon)
				gr.st.Locality = loc
			}
		}
		ref := st.HikeRef
		satName := ""
		if si < len(g.R.Sat) {
			if g.R.Sat[si].No != "" {
				ref = g.R.Sat[si].No
			}
			satName = g.R.Sat[si].Name
		}
		if ref != "" {
			gr := trails[ref]
			if gr == nil {
				lat, lon := g.R.LatLon(mid[0], mid[1])
				name := "SAT " + ref
				if satName != "" {
					name = satName + " (SAT " + ref + ")"
				}
				gr = &group{st: &Entry{
					ID: "t:" + ref, Name: name, Kind: "trail",
					Lat: round6(lat), Lon: round6(lon),
					Locality: locOf.locality(refs, mid[0], mid[1], 5000),
				}}
				trails[ref] = gr
			}
			if st.Len > gr.best {
				gr.best = st.Len
			}
		}
	}
	for _, gr := range streets {
		gc.entries = append(gc.entries, gr.st)
	}
	for _, gr := range trails {
		gc.entries = append(gc.entries, gr.st)
	}

	gc.Merged = gc.mergeDuplicateHuts(g)
	gc.build()

	// The names a parking may borrow: the settlements and, when the data has
	// them, the car parks. A trailhead 100 m from "Vallesinella" is called
	// Vallesinella, not "trail 382".
	var parks []ParkPlace
	for _, e := range gc.entries {
		if e.Kind == "place" || e.Kind == "parking" {
			parks = append(parks, ParkPlace{Name: e.Name, Lat: e.Lat, Lon: e.Lon})
		}
	}
	g.SetParkPlaces(parks)
	return gc, nil
}

// hutWords are the words that say WHAT a building is, not WHICH one: two rows
// that share only these are not the same hut.
var hutWords = map[string]bool{
	"rifugio": true, "rifugi": true, "hutte": true, "huette": true, "hut": true,
	"baita": true, "malga": true, "bivacco": true, "casa": true, "capanna": true,
	"haus": true, "berghaus": true, "alm": true, "alpino": true, "escursionistico": true,
	"al": true, "ai": true, "alla": true, "del": true, "della": true, "dei": true,
	"di": true, "da": true, "il": true, "la": true, "le": true, "and": true, "und": true,
}

// significant is a name's own words: what is left after the category words.
func significant(norm string) []string {
	var out []string
	for _, t := range tokens(norm) {
		if len(t) >= 3 && !hutWords[t] {
			out = append(out, t)
		}
	}
	return out
}

func sharesWord(a, b []string) bool {
	for _, x := range a {
		for _, y := range b {
			if x == y {
				return true
			}
		}
	}
	return false
}

// mergeDuplicateHuts folds the rows that are the same hut into one.
//
// Three sources describe the same buildings — the OSM nodes, the OSM ways as
// centroids and the Province register — and they spell them differently
// ("Rifugio Sasso Piatto", "Plattkofelhütte - Rifugio Sasso Piatto"). Two rows
// are the same hut when they are within 60 m AND share a word that is not the
// word for "hut": Rifugio Vajolet and Rifugio Paul Preuss stand 56 m apart and
// share nothing but the category, so they stay two. The row with the
// elevation and the fuller name is the one kept, and the other's name stays
// searchable on it.
func (gc *Geocoder) mergeDuplicateHuts(g *Graph) int {
	type row struct {
		i     int
		x, y  float64
		words []string
	}
	var huts []row
	for i, e := range gc.entries {
		if e.Kind != "hut" {
			continue
		}
		x, y := g.R.XY(e.Lat, e.Lon)
		huts = append(huts, row{i, x, y, significant(fold(e.Name))})
	}
	sort.SliceStable(huts, func(a, b int) bool {
		ea, eb := gc.entries[huts[a].i], gc.entries[huts[b].i]
		if (ea.Ele > 0) != (eb.Ele > 0) {
			return ea.Ele > 0
		}
		return len(ea.Name) > len(eb.Name)
	})
	dropped := make([]bool, len(gc.entries))
	merged := 0
	for a := range huts {
		if dropped[huts[a].i] {
			continue
		}
		keep := gc.entries[huts[a].i]
		for b := a + 1; b < len(huts); b++ {
			if dropped[huts[b].i] {
				continue
			}
			if math.Hypot(huts[a].x-huts[b].x, huts[a].y-huts[b].y) > 60 {
				continue
			}
			if !sharesWord(huts[a].words, huts[b].words) {
				continue
			}
			drop := gc.entries[huts[b].i]
			if keep.Ele == 0 && drop.Ele > 0 {
				keep.Ele, keep.weight = drop.Ele, drop.weight
			}
			if keep.Locality == "" {
				keep.Locality = drop.Locality
			}
			keep.also = append(keep.also, drop.Name)
			dropped[huts[b].i] = true
			merged++
		}
	}
	if merged == 0 {
		return 0
	}
	out := gc.entries[:0]
	for i, e := range gc.entries {
		if !dropped[i] {
			out = append(out, e)
		}
	}
	gc.entries = out
	return merged
}

// featureDoc is a served layer: a FeatureCollection of points with a name, a
// kind (OSM) or a type (the Province register) and an elevation. The crag
// layer adds the wall a sector belongs to and what a climber asks first.
type featureDoc struct {
	Features []struct {
		Properties struct {
			Name   string      `json:"name"`
			Kind   string      `json:"kind"`
			Type   string      `json:"type"`
			Ele    json.Number `json:"ele"`
			Parent string      `json:"parent"`
			Grades string      `json:"grades"`
			Aspect string      `json:"aspect"`
			Routes json.Number `json:"routes"`
		} `json:"properties"`
		Geometry struct {
			Type        string     `json:"type"`
			Coordinates [2]float64 `json:"coordinates"` // lon, lat
		} `json:"geometry"`
	} `json:"features"`
}

// addGeoJSON indexes one layer, skipping anything already in the index (the
// pois layer repeats the hut register, and the OSM extract repeats the peaks).
func (gc *Geocoder) addGeoJSON(path string, g *Graph, refs []placeRef, locOf *placeGrid) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return
	}
	var doc featureDoc
	if json.Unmarshal(raw, &doc) != nil {
		return
	}
	seen := map[string]bool{}
	for _, e := range gc.entries {
		seen[dedupKey(e.Name, e.Lat, e.Lon)] = true
	}
	for i := range doc.Features {
		f := &doc.Features[i]
		if f.Geometry.Type != "Point" {
			continue
		}
		name := strings.TrimSpace(f.Properties.Name)
		if name == "" {
			continue
		}
		kind := f.Properties.Kind
		switch strings.ToUpper(f.Properties.Type) {
		case "RIFUGIO ALPINO", "RIFUGIO ESCURSIONISTICO":
			kind, name = "hut", titled("Rifugio", register(name))
		case "BIVACCO":
			kind, name = "hut", titled("Bivacco", register(name))
		}
		switch kind {
		case "peak", "hut", "pass", "parking", "crag":
		default:
			continue
		}
		lon, lat := f.Geometry.Coordinates[0], f.Geometry.Coordinates[1]
		if key := dedupKey(name, lat, lon); seen[key] {
			continue
		} else {
			seen[key] = true
		}
		ele, _ := f.Properties.Ele.Float64()
		ent := &Entry{
			ID:   kind[:2] + ":" + strconv.Itoa(len(gc.entries)),
			Name: name, Kind: kind, Lat: lat, Lon: lon, Ele: ele, weight: ele,
		}
		if kind == "crag" {
			// "Settore B" is nothing without the wall it is on. The map draws
			// it beside the wall; a list has to say so.
			if p := strings.TrimSpace(f.Properties.Parent); p != "" && fold(p) != fold(name) {
				ent.Name = name + " (" + p + ")"
			}
			// Ranked by size, not by height: a crag's elevation is where the
			// walk ends, and the wall with three hundred routes is the one
			// people mean.
			routes, _ := f.Properties.Routes.Float64()
			ent.weight = routes
			ent.Detail = cragDetail(f.Properties.Grades, f.Properties.Aspect, int(routes))
		}
		x, y := g.R.XY(lat, lon)
		ent.Locality = locOf.locality(refs, x, y, 5000)
		gc.entries = append(gc.entries, ent)
	}
}

// cragDetail is the line under a crag's name — "4a–7c · S · 62 routes" —
// from whichever parts the mapping has.
func cragDetail(grades, aspect string, routes int) string {
	var parts []string
	if g := strings.TrimSpace(grades); g != "" {
		parts = append(parts, g)
	}
	if a := strings.TrimSpace(aspect); a != "" {
		parts = append(parts, a)
	}
	if routes > 0 {
		parts = append(parts, strconv.Itoa(routes)+" routes")
	}
	return strings.Join(parts, " · ")
}

// titled puts the category in front of a register name, unless the name
// already says it: the register has been reformatted more than once, and
// "Rifugio Rifugio Vajolet" is how you find out.
func titled(word, name string) string {
	if t := tokens(fold(name)); len(t) > 0 && t[0] == fold(word) {
		return name
	}
	return word + " " + name
}

// register turns the hut register's shouting into a name: TOSA "TOMMASO
// PEDROTTI" is Tosa Tommaso Pedrotti, which is what people type.
func register(s string) string {
	s = strings.NewReplacer("\"", " ", "«", " ", "»", " ").Replace(s)
	words := strings.Fields(strings.ToLower(s))
	for i, w := range words {
		r := []rune(w)
		r[0] = []rune(strings.ToUpper(string(r[0])))[0]
		// d'Ambiez, dell'Adamello: the letter after an apostrophe is a name too
		for k := 1; k+1 < len(r); k++ {
			if r[k] == '\'' {
				r[k+1] = []rune(strings.ToUpper(string(r[k+1])))[0]
			}
		}
		words[i] = string(r)
	}
	return strings.Join(words, " ")
}

func dedupKey(name string, lat, lon float64) string {
	return fold(name) + "|" + strconv.FormatFloat(math.Round(lat*2000), 'f', 0, 64) +
		"," + strconv.FormatFloat(math.Round(lon*2000), 'f', 0, 64)
}

// build normalizes every entry and sorts the token index. It is idempotent:
// calling it again after the entries changed rebuilds the index rather than
// appending to it, which would leave ids pointing past the end.
func (gc *Geocoder) build() {
	gc.index = gc.index[:0]
	for i, e := range gc.entries {
		e.norm = fold(e.Name)
		e.alts, e.toks = nil, nil
		e.alts = splitNames(e.norm)
		e.toks = tokens(e.norm)
		for _, other := range e.also {
			n := fold(other)
			e.alts = append(e.alts, n)
			e.alts = append(e.alts, splitNames(n)...)
			e.toks = append(e.toks, tokens(n)...)
		}
		switch e.Kind {
		case "trail":
			e.toks = append(e.toks, "trail", "sentiero")
		case "hut":
			// A hut is a rifugio whatever its sign says. Half of them are
			// named "Ncisles - Regensburger - Firenze" or "Schlernhaus", and
			// the word a person types first is the category.
			e.toks = append(e.toks, "rifugio", "hutte", "hut", "baita")
		case "crag":
			// Likewise a crag: "falesia Nago" in one province, "Klettergarten"
			// in the other, and the mapped name is rarely either.
			e.toks = append(e.toks, "falesia", "crag", "klettergarten", "arrampicata")
		}
		for _, t := range e.toks {
			gc.index = append(gc.index, tokRef{t, int32(i)})
		}
	}
	sort.Slice(gc.index, func(i, j int) bool {
		if gc.index[i].tok != gc.index[j].tok {
			return gc.index[i].tok < gc.index[j].tok
		}
		return gc.index[i].id < gc.index[j].id
	})
}

// Len is how many things the index holds.
func (gc *Geocoder) Len() int { return len(gc.entries) }

// Search answers a query. Ranked: a prefix of the whole name first, then the
// kind (a town beats a hut beats a street beats a trail), then how big the
// town is or how high the summit.
func (gc *Geocoder) Search(q string, limit int) []*Entry {
	q = strings.TrimSpace(fold(q))
	if q == "" {
		return nil
	}
	if limit <= 0 {
		limit = 10
	}
	qtoks := tokens(q)
	if len(qtoks) == 0 {
		return nil
	}
	// Candidates come from the rarest of the query's tokens; the others
	// filter. A query of one common word ("via") therefore costs one range.
	bestTok, bestLo, bestHi := "", 0, 0
	for i, t := range qtoks {
		lo, hi := gc.prefixRange(t)
		if i == 0 || hi-lo < bestHi-bestLo {
			bestTok, bestLo, bestHi = t, lo, hi
		}
	}
	_ = bestTok
	seen := map[int32]bool{}
	var cand []*Entry
	for _, r := range gc.index[bestLo:bestHi] {
		if seen[r.id] {
			continue
		}
		seen[r.id] = true
		e := gc.entries[r.id]
		if matchRank(e, q, qtoks) > 0 {
			cand = append(cand, e)
		}
	}
	// Not enough: fall back to a substring scan (the same query, anywhere in
	// the name). Bounded by the whole index, which answers in a millisecond.
	if len(cand) < limit {
		for i, e := range gc.entries {
			if seen[int32(i)] {
				continue
			}
			if strings.Contains(e.norm, q) {
				seen[int32(i)] = true
				cand = append(cand, e)
			}
		}
	}
	// Still nothing: the query names something the index does not hold
	// ("Rifugio Pedrotti" when the huts are mapped as buildings, not nodes).
	// Answer what its longest word finds rather than an empty list.
	if len(cand) == 0 {
		// Nothing was kept, so nothing is spoken for: the candidate pass marks
		// every entry of the rarest word's range as seen before it tests them,
		// and those are exactly the ones this pass wants to offer.
		seen = map[int32]bool{}
		// The RAREST word first, not the longest: "Rifugio Firenze" should
		// answer with what is called Firenze, not with every rifugio in the
		// region. Rarity is the size of the word's range in the token index.
		order := append([]string(nil), qtoks...)
		rare := map[string]int{}
		for _, t := range order {
			lo, hi := gc.prefixRange(t)
			rare[t] = hi - lo
		}
		sort.SliceStable(order, func(i, j int) bool {
			if rare[order[i]] != rare[order[j]] {
				return rare[order[i]] < rare[order[j]]
			}
			return len(order[i]) > len(order[j])
		})
		for _, t := range order {
			if len(t) < 3 {
				continue
			}
			for i, e := range gc.entries {
				if seen[int32(i)] {
					continue
				}
				if strings.Contains(e.norm, t) {
					seen[int32(i)] = true
					cand = append(cand, e)
				}
			}
			if len(cand) > 0 {
				break
			}
		}
	}
	sort.SliceStable(cand, func(i, j int) bool {
		a, b := cand[i], cand[j]
		// A town outranks a street of the same name, always: whoever types
		// "Bozen" means the town, and the street named after it is the second
		// line, never the first.
		if a.Kind != b.Kind && sameNamed(a, b) {
			if a.Kind == "place" {
				return true
			}
			if b.Kind == "place" {
				return false
			}
		}
		ra, rb := matchRank(a, q, qtoks), matchRank(b, q, qtoks)
		if ra != rb {
			return ra > rb
		}
		ka, kb := kindRank(a.Kind), kindRank(b.Kind)
		if ka != kb {
			return ka > kb
		}
		if a.weight != b.weight {
			return a.weight > b.weight
		}
		if len(a.Name) != len(b.Name) {
			return len(a.Name) < len(b.Name)
		}
		return a.Name < b.Name
	})
	if len(cand) > limit {
		cand = cand[:limit]
	}
	return cand
}

// prefixRange is the half-open range of the token index whose tokens start
// with p.
func (gc *Geocoder) prefixRange(p string) (int, int) {
	lo := sort.Search(len(gc.index), func(i int) bool { return gc.index[i].tok >= p })
	hi := sort.Search(len(gc.index), func(i int) bool {
		return gc.index[i].tok > p && !strings.HasPrefix(gc.index[i].tok, p)
	})
	if hi < lo {
		hi = lo
	}
	return lo, hi
}

// canonicalName is a name reduced to the set of words in it, in one order, so
// that every way of writing the same bilingual street groups together:
// "Radweg Meran-Bozen - Pista ciclabile Bolzano-Merano" and "Radweg Bozen /
// Meran - Pista ciclabile Bolzano-Merano" are the same cycleway written by two
// municipalities, and a search for "Bozen" should answer with one line. A name
// with a word the other does not have — "Raccordo pista ciclabile
// Bolzano-Merano" — stays its own thing, which is right: it is the slip road.
func canonicalName(name string) string {
	toks := tokens(fold(name))
	if len(toks) == 0 {
		return fold(name)
	}
	uniq := append([]string(nil), toks...)
	sort.Strings(uniq)
	out := uniq[:0]
	for i, t := range uniq {
		if i == 0 || t != uniq[i-1] {
			out = append(out, t)
		}
	}
	return strings.Join(out, " ")
}

// splitNames breaks a bilingual South Tyrol name into the names it is made
// of: "Bolzano - Bozen", "Brixen - Bressanone", "Urtijëi - St. Ulrich -
// Ortisei", "Rifugio Doss dei Cembri / Doss dei Gembri". Each half is a name a
// person types on its own, so each half ranks like a whole name.
func splitNames(norm string) []string {
	var parts []string
	for _, sep := range []string{" - ", " / ", " – "} {
		if strings.Contains(norm, sep) {
			for _, p := range strings.Split(norm, sep) {
				if p = strings.TrimSpace(p); p != "" {
					parts = append(parts, p)
				}
			}
			break
		}
	}
	if len(parts) < 2 {
		return nil
	}
	return parts
}

// matchRank says how well an entry answers the query: 4 the whole name (or
// one language of it), 3 a prefix of it, 2 every query token a prefix of some
// token of the name, 1 the query somewhere inside the name, 0 no match.
func matchRank(e *Entry, q string, qtoks []string) int {
	switch {
	case e.norm == q:
		return 4
	case strings.HasPrefix(e.norm, q):
		return 3
	}
	for _, a := range e.alts {
		if a == q {
			return 4
		}
	}
	for _, a := range e.alts {
		if strings.HasPrefix(a, q) {
			return 3
		}
	}
	all := true
	for _, qt := range qtoks {
		hit := false
		for _, t := range e.toks {
			if strings.HasPrefix(t, qt) {
				hit = true
				break
			}
		}
		if !hit {
			all = false
			break
		}
	}
	if all {
		return 2
	}
	if strings.Contains(e.norm, q) {
		return 1
	}
	return 0
}

// sameNamed says two entries are called the same thing, in either language.
func sameNamed(a, b *Entry) bool {
	for _, x := range append([]string{a.norm}, a.alts...) {
		for _, y := range append([]string{b.norm}, b.alts...) {
			if x != "" && x == y {
				return true
			}
		}
	}
	return false
}

func kindRank(k string) int {
	switch k {
	case "place":
		return 4
	case "hut", "peak", "pass", "crag":
		return 3
	case "street":
		return 2
	}
	return 1
}

// tokens splits a normalized name into its words.
func tokens(s string) []string {
	f := strings.FieldsFunc(s, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	out := f[:0]
	for _, t := range f {
		if t != "" {
			out = append(out, t)
		}
	}
	return out
}

// fold lowercases and strips the accents of the languages this region is
// named in (Italian, German, Ladin), so "Sanzeno", "Sänzeno" and "SANZENO"
// are one string.
func fold(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range strings.ToLower(s) {
		switch r {
		case 'à', 'á', 'â', 'ã', 'ä', 'å':
			b.WriteByte('a')
		case 'è', 'é', 'ê', 'ë':
			b.WriteByte('e')
		case 'ì', 'í', 'î', 'ï':
			b.WriteByte('i')
		case 'ò', 'ó', 'ô', 'õ', 'ö':
			b.WriteByte('o')
		case 'ù', 'ú', 'û', 'ü':
			b.WriteByte('u')
		case 'ç':
			b.WriteByte('c')
		case 'ñ':
			b.WriteByte('n')
		case 'ß':
			b.WriteString("ss")
		case '\'', '`', '´':
			b.WriteByte(' ')
		default:
			b.WriteRune(r)
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// ------------------------------------------------------------- the place grid

// placeRef is a settlement in the grid: where it is, what it is called, and
// how much of a settlement it is — 2 a town one belongs to, 1 a hamlet or a
// quarter, 0 an OSM `locality`, which is a named spot in the woods and must
// never be the locality of a street when a village is in reach.
type placeRef struct {
	x, y float64
	name string
	rank int
}

type placeGrid struct {
	cell        float64
	minX, minY  float64
	gw, gh      int
	cells       [][]int32
	initialized bool
}

func newPlaceGrid(refs []placeRef, cell float64) *placeGrid {
	pg := &placeGrid{cell: cell}
	if len(refs) == 0 {
		return pg
	}
	minX, minY := math.Inf(1), math.Inf(1)
	maxX, maxY := math.Inf(-1), math.Inf(-1)
	for _, r := range refs {
		minX, minY = math.Min(minX, r.x), math.Min(minY, r.y)
		maxX, maxY = math.Max(maxX, r.x), math.Max(maxY, r.y)
	}
	pg.minX, pg.minY = minX, minY
	pg.gw = int((maxX-minX)/cell) + 1
	pg.gh = int((maxY-minY)/cell) + 1
	pg.cells = make([][]int32, pg.gw*pg.gh)
	for i, r := range refs {
		c := pg.cellOf(r.x, r.y)
		pg.cells[c] = append(pg.cells[c], int32(i))
	}
	pg.initialized = true
	return pg
}

func (pg *placeGrid) cellOf(x, y float64) int {
	cx := int((x - pg.minX) / pg.cell)
	cy := int((y - pg.minY) / pg.cell)
	if cx < 0 {
		cx = 0
	}
	if cy < 0 {
		cy = 0
	}
	if cx >= pg.gw {
		cx = pg.gw - 1
	}
	if cy >= pg.gh {
		cy = pg.gh - 1
	}
	return cy*pg.gw + cx
}

// locality is the settlement a point belongs to: the nearest real one
// (village, hamlet, quarter) within maxM, and only if there is none, the
// nearest named spot of any kind.
func (pg *placeGrid) locality(refs []placeRef, x, y, maxM float64) string {
	// A town first, even when a quarter of it is nearer: "Via Asiago, Trento"
	// is what a person searching for a street means, not the name of the
	// block it runs through. Then a hamlet, then whatever is named there.
	for _, rank := range []int{2, 1, 0} {
		if n := pg.nearestXY(refs, x, y, maxM, rank); n != "" {
			return n
		}
	}
	return ""
}

// nearestXY is the name of the closest settlement of at least minRank within
// maxM metres, "" when there is none.
func (pg *placeGrid) nearestXY(refs []placeRef, x, y, maxM float64, minRank int) string {
	if !pg.initialized {
		return ""
	}
	cx := int((x - pg.minX) / pg.cell)
	cy := int((y - pg.minY) / pg.cell)
	rings := int(maxM/pg.cell) + 1
	best, bestD := "", maxM*maxM
	for ring := 0; ring <= rings; ring++ {
		if best != "" && float64(ring-1)*pg.cell > math.Sqrt(bestD) {
			break
		}
		for gy := cy - ring; gy <= cy+ring; gy++ {
			if gy < 0 || gy >= pg.gh {
				continue
			}
			for gx := cx - ring; gx <= cx+ring; gx++ {
				if gx < 0 || gx >= pg.gw {
					continue
				}
				if ring > 0 && gx != cx-ring && gx != cx+ring && gy != cy-ring && gy != cy+ring {
					continue
				}
				for _, i := range pg.cells[gy*pg.gw+gx] {
					r := refs[i]
					if r.rank < minRank {
						continue
					}
					d := (r.x-x)*(r.x-x) + (r.y-y)*(r.y-y)
					if d < bestD {
						best, bestD = r.name, d
					}
				}
			}
		}
	}
	return best
}

// nearest is nearestXY for an entry already in the list, skipping itself.
func (pg *placeGrid) nearest(refs []placeRef, self int, maxM float64, minRank int) (string, bool) {
	if self >= len(refs) {
		return "", false
	}
	me := refs[self]
	n := pg.nearestXY(refs, me.x, me.y, maxM, minRank)
	if n == "" || n == me.name {
		return "", false
	}
	return n, true
}

// NewGeocoder indexes entries that were built elsewhere (the tests, and any
// caller that has its own source of names).
func NewGeocoder(entries []*Entry) *Geocoder {
	gc := &Geocoder{entries: entries}
	gc.build()
	return gc
}
