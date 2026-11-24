package main

import (
	"cmp"
	"net/http"
	"slices"
	"strings"

	"github.com/spool-sync/connect-plugin/internal/spoolsync"
	"github.com/spool-sync/connect-plugin/internal/store"
)

// maxJobs is how many prints one read of GET /jobs/usage answers: a page of the
// Jobs listing.
const maxJobs = 200

// jobTotal is the filament one print took, for its cell in the Jobs listing.
type jobTotal struct {
	JobID string  `json:"job_id"`
	Grams float64 `json:"grams"`
}

// jobSpool is the filament one print took off one spool on one tool.
type jobSpool struct {
	Tool      string  `json:"tool"`
	SpoolID   string  `json:"spool_id"`
	Name      string  `json:"name"`
	Brand     string  `json:"brand"`
	Material  string  `json:"material"`
	ColorRGBA string  `json:"color_rgba"`
	Grams     float64 `json:"grams"`
}

// jobsUsage is GET /jobs/usage?id=..: the filament each print named took. A
// print this module counted nothing for is absent.
func (s *service) jobsUsage(w http.ResponseWriter, r *http.Request, _ string) {
	// The browser's client joins a list with commas; a repeated id is read too.
	var ids []string

	for _, v := range r.URL.Query()["id"] {
		for _, id := range strings.Split(v, ",") {
			if id = strings.TrimSpace(id); id != "" {
				ids = append(ids, id)
			}
		}
	}

	ids = slices.Compact(slices.Sorted(slices.Values(ids)))
	if len(ids) > maxJobs {
		refusef(w, InvalidQuery, map[string]any{"max": maxJobs}, "at most %d prints at once", maxJobs)

		return
	}

	uses, err := s.st.JobUsage(r.Context(), ids)
	if err != nil {
		s.storeFailed(w, r, err)

		return
	}

	totals := map[string]float64{}
	for _, u := range uses {
		totals[u.JobID] += u.Grams
	}

	out := make([]jobTotal, 0, len(totals))
	for _, id := range ids {
		if g, ok := totals[id]; ok {
			out = append(out, jobTotal{JobID: id, Grams: g})
		}
	}

	answer(w, http.StatusOK, map[string]any{"usage": out})
}

// jobUsage is GET /jobs/{id}/usage: the filament one print took, tool by tool
// and spool by spool, with the spools named.
func (s *service) jobUsage(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")

	uses, err := s.st.JobUsage(r.Context(), []string{id})
	if err != nil {
		s.storeFailed(w, r, err)

		return
	}

	spools := map[string]spoolsync.Spool{}

	if len(uses) > 0 {
		all, err := s.spoolsAll(r.Context())
		if err != nil {
			s.spoolSyncFailed(w, "the spools", err)

			return
		}

		for _, sp := range all {
			spools[sp.ID] = sp
		}
	}

	out := jobBreakdown(uses, spools)

	total := 0.0
	for _, u := range out {
		total += u.Grams
	}

	answer(w, http.StatusOK, map[string]any{"job_id": id, "grams": total, "spools": out})
}

// jobBreakdown names each spool a print took filament off, in tool order. A
// spool since deleted keeps its row, with no name.
func jobBreakdown(uses []store.JobUse, spools map[string]spoolsync.Spool) []jobSpool {
	out := make([]jobSpool, 0, len(uses))

	for _, u := range uses {
		row := jobSpool{Tool: u.Tool, SpoolID: u.SpoolID, Grams: u.Grams}

		if sp, ok := spools[u.SpoolID]; ok {
			row.Name, row.Brand, row.Material = sp.Filament.Name, sp.Brand, sp.Material
			row.ColorRGBA = colourRGBA(sp.Color)
		}

		out = append(out, row)
	}

	slices.SortFunc(out, func(a, b jobSpool) int {
		return cmp.Or(cmp.Compare(a.Tool, b.Tool), cmp.Compare(b.Grams, a.Grams))
	})

	return out
}
