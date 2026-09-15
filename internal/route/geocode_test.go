package route

import (
	"testing"

	"ometto/internal/city"
)

func idx() *Geocoder {
	return NewGeocoder([]*Entry{
		{ID: "1", Name: "Trento", Kind: "place", Place: "city", weight: 119359, Lat: 46.066, Lon: 11.125},
		{ID: "2", Name: "Trentino Alto Adige", Kind: "place", Place: "region", weight: 1000, Lat: 46.4, Lon: 11.3},
		{ID: "3", Name: "Molveno", Kind: "place", Place: "village", weight: 1102, Lat: 46.142, Lon: 10.963},
		{ID: "4", Name: "Rifugio Pedrotti", Kind: "hut", weight: 2491, Lat: 46.16, Lon: 10.88},
		{ID: "5", Name: "Cima Verde", Kind: "peak", weight: 2102, Lat: 45.995, Lon: 11.044},
		{ID: "6", Name: "Monte Verde", Kind: "peak", weight: 1500, Lat: 46.0, Lon: 11.0},
		{ID: "7", Name: "Via Asiago", Kind: "street", Locality: "Trento", Lat: 46.07, Lon: 11.12},
		{ID: "8", Name: "Via Asiago", Kind: "street", Locality: "Rovereto", Lat: 45.89, Lon: 11.04},
		{ID: "9", Name: "SAT 401", Kind: "trail", Locality: "Molveno", Lat: 46.15, Lon: 10.9},
		{ID: "10", Name: "Passo Sella", Kind: "pass", weight: 2244, Lat: 46.51, Lon: 11.76},
		{ID: "11", Name: "Sardagna", Kind: "place", Place: "hamlet", Locality: "Trento", weight: 1200, Lat: 46.07, Lon: 11.1},
	})
}

func TestRankingExactPrefixFirst(t *testing.T) {
	gc := idx()
	got := gc.Search("trento", 5)
	if len(got) == 0 || got[0].Name != "Trento" {
		t.Fatalf("want Trento first, got %v", names(got))
	}
	// A prefix of the whole name beats a token match inside a longer name.
	got = gc.Search("tren", 5)
	if got[0].Name != "Trento" {
		t.Errorf("tren -> %v", names(got))
	}
}

func TestRankingKindPriority(t *testing.T) {
	gc := idx()
	// "verde" matches two peaks; the higher one wins.
	got := gc.Search("verde", 5)
	if len(got) < 2 || got[0].Name != "Cima Verde" {
		t.Fatalf("verde -> %v", names(got))
	}
	// A place outranks a street of the same match quality.
	gc2 := NewGeocoder([]*Entry{
		{ID: "a", Name: "Asiago", Kind: "street", Locality: "Trento"},
		{ID: "b", Name: "Asiago", Kind: "place", Place: "town", weight: 6000},
		{ID: "c", Name: "Asiago", Kind: "trail"},
	})
	got = gc2.Search("asiago", 5)
	if got[0].Kind != "place" || got[1].Kind != "street" || got[2].Kind != "trail" {
		t.Errorf("kind order wrong: %v", kinds(got))
	}
}

func TestSearchAccentAndCaseInsensitive(t *testing.T) {
	gc := NewGeocoder([]*Entry{
		{ID: "1", Name: "Pejo", Kind: "place", weight: 10},
		{ID: "2", Name: "Sänzeno", Kind: "place", weight: 10},
		{ID: "3", Name: "Località Bòsco", Kind: "place", weight: 5},
	})
	for _, q := range []string{"SANZENO", "sanzeno", "sänzeno"} {
		if got := gc.Search(q, 3); len(got) == 0 || got[0].Name != "Sänzeno" {
			t.Errorf("%q -> %v", q, names(got))
		}
	}
	if got := gc.Search("bosco", 3); len(got) == 0 || got[0].Name != "Località Bòsco" {
		t.Errorf("bosco -> %v", names(got))
	}
}

func TestSearchMultiTokenAndSubstring(t *testing.T) {
	gc := idx()
	got := gc.Search("rifugio pedr", 5)
	if len(got) == 0 || got[0].Name != "Rifugio Pedrotti" {
		t.Fatalf("rifugio pedr -> %v", names(got))
	}
	// The second word alone finds it too (a token prefix, not a name prefix).
	got = gc.Search("pedrotti", 5)
	if len(got) == 0 || got[0].Name != "Rifugio Pedrotti" {
		t.Fatalf("pedrotti -> %v", names(got))
	}
	// A trail is found by its number alone and by "trail <n>".
	for _, q := range []string{"401", "sat 401", "trail 401"} {
		got = gc.Search(q, 5)
		if len(got) == 0 || got[0].Name != "SAT 401" {
			t.Errorf("%q -> %v", q, names(got))
		}
	}
}

func TestSearchLimitAndDuplicates(t *testing.T) {
	gc := idx()
	got := gc.Search("via asiago", 10)
	if len(got) != 2 {
		t.Fatalf("two localities of Via Asiago, got %v", names(got))
	}
	if got[0].Locality == got[1].Locality {
		t.Errorf("the two entries share a locality: %v", got[0].Locality)
	}
	if len(gc.Search("a", 3)) > 3 {
		t.Errorf("limit not honoured")
	}
	if len(gc.Search("   ", 3)) != 0 {
		t.Errorf("a blank query must answer nothing")
	}
}

func names(es []*Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Name
	}
	return out
}

func kinds(es []*Entry) []string {
	out := make([]string, len(es))
	for i, e := range es {
		out[i] = e.Kind
	}
	return out
}

// TestBilingualNames: South Tyrol is named twice on every sign, and a person
// types one of the two. Both halves must rank as a whole name.
func TestBilingualNames(t *testing.T) {
	gc := NewGeocoder([]*Entry{
		{ID: "1", Name: "Bolzano - Bozen", Kind: "place", Place: "city", weight: 105713},
		{ID: "2", Name: "Brixen - Bressanone", Kind: "place", Place: "town", weight: 21000},
		{ID: "3", Name: "Merano - Meran", Kind: "place", Place: "town", weight: 41000},
		{ID: "4", Name: "Urtijëi - St. Ulrich - Ortisei", Kind: "place", Place: "town", weight: 4600},
		{ID: "5", Name: "Bolzanetto", Kind: "place", Place: "hamlet", weight: 400},
		{ID: "6", Name: "Via Bolzano", Kind: "street", Locality: "Trento"},
	})
	for q, want := range map[string]string{
		"bolzano": "Bolzano - Bozen", "bozen": "Bolzano - Bozen",
		"brixen": "Brixen - Bressanone", "bressanone": "Brixen - Bressanone",
		"merano": "Merano - Meran", "meran": "Merano - Meran",
		"ortisei": "Urtijëi - St. Ulrich - Ortisei", "st ulrich": "Urtijëi - St. Ulrich - Ortisei",
		"urtijei": "Urtijëi - St. Ulrich - Ortisei",
	} {
		got := gc.Search(q, 5)
		if len(got) == 0 || got[0].Name != want {
			t.Errorf("%q -> %v, want %q first", q, names(got), want)
		}
	}
}

// TestMergeDuplicateHuts: three sources spell the same hut three ways and it
// must be one row; two huts that stand 56 m apart with different names must
// stay two.
func TestMergeDuplicateHuts(t *testing.T) {
	r := &Region{Name: "huts", Lat0: 46, Lon0: 11, MPerDegLat: 111000, MPerDegLon: 77000}
	r.Pts = [][2]float64{{0, 0}, {10, 0}}
	r.Stretches = []*city.Stretch{{ID: "s", Pts: [][2]float64{{0, 0}, {10, 0}}, Cum: []float64{0, 10}, Len: 10, A: 0, B: 1, Cls: "path", MTB: -1}}
	r.Sat = []Sat{{}}
	g := Build(r)
	at := func(dx, dy float64) (float64, float64) { // metres from the origin
		return g.R.LatLon(dx, dy)
	}
	la, lo := at(0, 0)
	la2, lo2 := at(30, 0) // 30 m away: the same hut, another source
	la3, lo3 := at(45, 0) // 45 m away: the register row
	lb, lob := at(0, 56)  // 56 m away: a different hut
	gc := NewGeocoder([]*Entry{
		{ID: "1", Name: "Plattkofelhütte - Rifugio Sasso Piatto", Kind: "hut", Lat: la, Lon: lo, Ele: 2305, weight: 2305},
		{ID: "2", Name: "Rifugio Sasso Piatto", Kind: "hut", Lat: la2, Lon: lo2, Ele: 2300, weight: 2300},
		{ID: "3", Name: "SASSO PIATTO", Kind: "hut", Lat: la3, Lon: lo3},
		{ID: "4", Name: "Rifugio Vajolet", Kind: "hut", Lat: la, Lon: lo, Ele: 2244, weight: 2244},
		{ID: "5", Name: "Rifugio Paul Preuss", Kind: "hut", Lat: lb, Lon: lob, Ele: 2247, weight: 2247},
	})
	if n := gc.mergeDuplicateHuts(g); n != 2 {
		t.Fatalf("merged %d rows, want 2 (the three Sasso Piatto rows into one)", n)
	}
	gc.build()
	if got := len(gc.entries); got != 3 {
		t.Fatalf("%d entries left, want 3: %v", got, names(gc.entries))
	}
	// The kept row is the one with an elevation and the fuller name, and the
	// spellings that were folded into it still find it.
	for _, q := range []string{"sasso piatto", "plattkofelhutte", "plattkofelhütte"} {
		got := gc.Search(q, 3)
		if len(got) != 1 || got[0].Name != "Plattkofelhütte - Rifugio Sasso Piatto" {
			t.Errorf("%q -> %v", q, names(got))
		}
	}
	// 56 m apart and nothing in common but the word for hut: two huts.
	for _, q := range []string{"vajolet", "preuss"} {
		if got := gc.Search(q, 3); len(got) != 1 {
			t.Errorf("%q -> %v, want exactly one", q, names(got))
		}
	}
	if len(gc.Search("rifugio", 10)) != 3 {
		t.Errorf("the category word should still list all three huts")
	}
}
