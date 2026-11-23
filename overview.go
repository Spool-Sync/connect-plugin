package main

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/spool-sync/connect-plugin/internal/spoolsync"
)

// usageDays is how far back the usage chart reaches, SpoolSync's dashboard's own span.
const usageDays = 30

// maxSwatches is how many colours a material's tile shows.
const maxSwatches = 8

// overview is GET /overview: what SpoolSync's dashboard shows of the stock, without
// the printers, the reorder state and the recent spools.
type overview struct {
	Spools     int            `json:"spools"`
	Grams      float64        `json:"grams"`
	Materials  int            `json:"materials"`
	ByMaterial []materialTile `json:"by_material"`
	ByType     []typeBar      `json:"by_type"`
	Usage      []usageLine    `json:"usage"`
}

// materialTile is one material (PLA, PETG...) in stock.
type materialTile struct {
	Material string   `json:"material"`
	Spools   int      `json:"spools"`
	Grams    float64  `json:"grams"`
	Colours  []string `json:"colours"`
}

// typeBar is the filament left of one filament type.
type typeBar struct {
	ID     string  `json:"id"`
	Label  string  `json:"label"`
	Colour *string `json:"colour"`
	Grams  float64 `json:"grams"`
}

// usageLine is one filament type's grams on spools, a point a day.
type usageLine struct {
	ID     string       `json:"id"`
	Label  string       `json:"label"`
	Colour *string      `json:"colour"`
	Points []usagePoint `json:"points"`
}

type usagePoint struct {
	Date  string  `json:"date"`
	Grams float64 `json:"grams"`
}

func (s *service) overview(w http.ResponseWriter, r *http.Request, _ string) {
	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	out := summarise(spools)

	// The chart is the one part read from elsewhere; without it the rest still stands.
	trend, err := s.ss.UsageTrend(r.Context(), usageDays)
	if err != nil {
		s.log.Warn("spoolsync unreachable", "what", "the usage trend", "error", err)
	}

	out.Usage = usageLines(trend)

	answer(w, http.StatusOK, out)
}

// summarise totals the spools in stock: by material and by filament type, most filament first.
func summarise(spools []spoolsync.Spool) overview {
	out := overview{ByMaterial: []materialTile{}, ByType: []typeBar{}, Usage: []usageLine{}}
	materials := map[string]*materialTile{}
	types := map[string]*typeBar{}

	for _, sp := range spools {
		if sp.ArchivedAt != nil {
			continue
		}

		out.Spools++
		out.Grams += sp.RemainingG

		name := cmp.Or(strings.TrimSpace(sp.Material), "Unknown")

		m := materials[name]
		if m == nil {
			m = &materialTile{Material: name, Colours: []string{}}
			materials[name] = m
		}

		m.Spools++
		m.Grams += sp.RemainingG

		if sp.Color != nil && len(m.Colours) < maxSwatches && !slices.Contains(m.Colours, *sp.Color) {
			m.Colours = append(m.Colours, *sp.Color)
		}

		t := types[sp.FilamentTypeID]
		if t == nil {
			t = &typeBar{ID: sp.FilamentTypeID, Label: strings.TrimSpace(sp.Brand + " " + sp.Name), Colour: sp.Color}
			types[sp.FilamentTypeID] = t
		}

		t.Grams += sp.RemainingG
	}

	for _, m := range materials {
		out.ByMaterial = append(out.ByMaterial, *m)
	}

	for _, t := range types {
		out.ByType = append(out.ByType, *t)
	}

	out.Materials = len(out.ByMaterial)

	slices.SortFunc(out.ByMaterial, func(a, b materialTile) int {
		return cmp.Or(cmp.Compare(b.Grams, a.Grams), cmp.Compare(a.Material, b.Material))
	})
	slices.SortFunc(out.ByType, func(a, b typeBar) int {
		return cmp.Or(cmp.Compare(b.Grams, a.Grams), cmp.Compare(a.Label, b.Label))
	})

	return out
}

// usageLines is SpoolSync's usage trend in this module's shape.
func usageLines(trend []spoolsync.UsageSeries) []usageLine {
	out := make([]usageLine, 0, len(trend))

	for _, s := range trend {
		points := make([]usagePoint, 0, len(s.Data))
		for _, p := range s.Data {
			points = append(points, usagePoint{Date: p.Date, Grams: p.TotalG})
		}

		out = append(out, usageLine{ID: s.FilamentTypeID, Label: s.Label, Colour: s.ColorHex, Points: points})
	}

	return out
}
