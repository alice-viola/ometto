package main

import (
	"testing"
	"time"

	"ometto/internal/route"
)

func item(at time.Time, mode, grade string, pts ...[2]float64) HistItem {
	var ps []route.Point
	for _, p := range pts {
		ps = append(ps, route.Point{Lat: p[0], Lon: p[1]})
	}
	return HistItem{
		ID: newID(), At: at.UTC().Format(time.RFC3339),
		Request: HistRequest{Points: ps, Mode: mode, Grade: grade, Alternatives: 1},
		Summary: HistSummary{Seconds: 100, Meters: 1000},
	}
}

// TestFoldHistory: the same question asked again within ten minutes updates
// the line that is there; a different question, or the same one an hour later,
// is a new line.
func TestFoldHistory(t *testing.T) {
	t0 := time.Date(2026, 9, 15, 8, 0, 0, 0, time.UTC)
	a := item(t0, "hike", "EE", [2]float64{46.0669, 11.1213}, [2]float64{45.8858, 10.8410})

	list := foldHistory(nil, a, historyFold)
	if len(list) != 1 {
		t.Fatalf("the first entry was not added")
	}

	// The same trip two minutes later: one line, the newer time, the same id.
	b := item(t0.Add(2*time.Minute), "hike", "EE", [2]float64{46.0669, 11.1213}, [2]float64{45.8858, 10.8410})
	b.Summary.Meters = 1234
	list = foldHistory(list, b, historyFold)
	if len(list) != 1 {
		t.Fatalf("the same question was added twice: %d entries", len(list))
	}
	if list[0].ID != a.ID {
		t.Errorf("the folded entry lost its id")
	}
	if list[0].At != b.At {
		t.Errorf("the folded entry kept the old time %q, want %q", list[0].At, b.At)
	}
	if list[0].Summary.Meters != 1234 {
		t.Errorf("the folded entry kept the old summary")
	}

	// A metre of drift is the same question; fifty metres is a new one.
	near := item(t0.Add(3*time.Minute), "hike", "EE", [2]float64{46.06690, 11.12131}, [2]float64{45.8858, 10.8410})
	if list = foldHistory(list, near, historyFold); len(list) != 1 {
		t.Errorf("a metre of drift started a new entry")
	}
	far := item(t0.Add(4*time.Minute), "hike", "EE", [2]float64{46.0675, 11.1213}, [2]float64{45.8858, 10.8410})
	if list = foldHistory(list, far, historyFold); len(list) != 2 {
		t.Fatalf("fifty metres away should be a new entry: %d", len(list))
	}

	// Another mode, another grade, another number of points: new lines.
	list = foldHistory(list, item(t0.Add(5*time.Minute), "car", "EE", [2]float64{46.0675, 11.1213}, [2]float64{45.8858, 10.8410}), historyFold)
	if len(list) != 3 {
		t.Errorf("a different mode folded")
	}
	list = foldHistory(list, item(t0.Add(6*time.Minute), "car", "EEA", [2]float64{46.0675, 11.1213}, [2]float64{45.8858, 10.8410}), historyFold)
	if len(list) != 4 {
		t.Errorf("a different grade folded")
	}
	list = foldHistory(list, item(t0.Add(7*time.Minute), "car", "EEA", [2]float64{46.0675, 11.1213}), historyFold)
	if len(list) != 5 {
		t.Errorf("a different number of points folded")
	}

	// The same question an hour later is a new line: the fold is a debounce,
	// not a deduplication of the whole history.
	old := list[0]
	again := item(t0.Add(67*time.Minute), old.Request.Mode, old.Request.Grade,
		[2]float64{old.Request.Points[0].Lat, old.Request.Points[0].Lon})
	if list = foldHistory(list, again, historyFold); len(list) != 6 {
		t.Errorf("an hour later should be a new entry: %d", len(list))
	}
}
