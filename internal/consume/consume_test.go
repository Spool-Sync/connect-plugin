package consume

import (
	"math"
	"testing"
)

func TestDelta(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	i := func(v int64) *int64 { return &v }

	for name, c := range map[string]struct {
		prevE  *float64
		prevID *int64
		e      float64
		id     *int64
		want   float64
		stale  bool
	}{
		"first reading":            {nil, nil, 500, i(7), 0, false},
		"an increase":              {f(100), i(7), 1100, i(7), 1000, false},
		"no id either side":        {f(100), nil, 1100, nil, 1000, false},
		"a lower reading, same id": {f(1100), i(7), 40, i(7), 0, true},
		"a lower reading, no id":   {f(1100), nil, 40, nil, 0, false},
		"a new id, counter up":     {f(100), i(7), 5000, i(8), 0, false},
		"a new id, counter down":   {f(1100), i(7), 40, i(8), 0, false},
		"an id where none was":     {f(100), nil, 5000, i(8), 0, false},
		"no change":                {f(100), i(7), 100, i(7), 0, false},
	} {
		if got, stale := Delta(c.prevE, c.prevID, c.e, c.id); got != c.want || stale != c.stale {
			t.Errorf("%s: %v %v, want %v %v", name, got, stale, c.want, c.stale)
		}
	}
}

func TestGrams(t *testing.T) {
	d := func(v float64) *float64 { return &v }
	near := func(got, want float64) bool { return math.Abs(got-want) < 1e-4 }

	// 1 m of 1.75 mm PLA at 1.24 g/cm³: 2.4053 mm² × 1000 mm = 2.4053 cm³.
	if g := Grams(1000, nil, "PLA", nil); !near(g, 2.98255) {
		t.Errorf("1 m of 1.75 mm PLA: %v", g)
	}

	if g := Grams(1000, nil, "petg", nil); !near(g, 2.98255/1.24*1.27) {
		t.Errorf("PETG by type: %v", g)
	}

	if g := Grams(1000, nil, "UNKNOWN", nil); !near(g, 2.98255) {
		t.Errorf("an unknown type falls back to 1.24: %v", g)
	}

	// The spool's density and diameter win over the type's: 2.85 mm, 2.0 g/cm³.
	want := 1000 * math.Pi / 4 * 2.85 * 2.85 / 1000 * 2.0
	if g := Grams(1000, d(2.0), "PLA", d(2.85)); !near(g, want) {
		t.Errorf("the spool's density and diameter win: %v, want %v", g, want)
	}
}
