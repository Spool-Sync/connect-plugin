package main

import (
	"context"
	"io"
	"maps"
	"math"
	"net/http"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"github.com/spool-sync/connect-plugin/internal/follower"
	"github.com/spool-sync/connect-plugin/internal/spoolsync"
)

// mount is the handler of every route. A route runs with the person the host
// named, and a request the host named nobody for is refused.
type handler func(w http.ResponseWriter, r *http.Request, by string)

// acting refuses a request the host attributed to nobody, and hands every other
// request the name of the person it acts as.
func acting(next handler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		by := routesapi.PrincipalOf(r)
		if strings.TrimSpace(by) == "" {
			refuse(w, NoPrincipal, "the host named no person for this request", nil)

			return
		}

		next(w, r, by)
	}
}

// routes is the whole HTTP surface of this module. Every path is relative to the
// mount, so /materials is /api/v1/modules/<namespace>/materials to a caller.
func (s *service) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /status", s.status)
	mux.HandleFunc("GET /catalogue", acting(s.catalogue))
	mux.HandleFunc("POST /catalogue/download", acting(s.download))
	mux.HandleFunc("GET /catalogue/brands", acting(s.brands))
	mux.HandleFunc("GET /types", acting(s.types))
	mux.HandleFunc("GET /materials", acting(s.listMaterials))
	mux.HandleFunc("GET /materials/{uuid}", acting(s.material))
	mux.HandleFunc("GET /materials/{uuid}/spools", acting(s.materialSpools))
	mux.HandleFunc("POST /custom-materials", acting(s.createCustom))
	mux.HandleFunc("PATCH /custom-materials/{uuid}", acting(s.updateCustom))
	mux.HandleFunc("DELETE /custom-materials/{uuid}", acting(s.deleteCustom))
	mux.HandleFunc("PUT /favourites/{uuid}", acting(s.star(true)))
	mux.HandleFunc("DELETE /favourites/{uuid}", acting(s.star(false)))
	mux.HandleFunc("GET /locations", acting(s.locations))
	mux.HandleFunc("POST /locations", acting(s.createLocation))
	mux.HandleFunc("PATCH /locations/{id}", acting(s.renameLocation))
	mux.HandleFunc("DELETE /locations/{id}", acting(s.deleteLocation))
	mux.HandleFunc("GET /list-filters", acting(s.getFilters))
	mux.HandleFunc("PUT /list-filters", acting(s.putFilters))
	mux.HandleFunc("GET /overview", acting(s.overview))
	mux.HandleFunc("GET /spools", acting(s.listSpools))
	mux.HandleFunc("POST /spools", acting(s.addSpools))
	mux.HandleFunc("PATCH /spools/{id}", acting(s.updateSpool))
	mux.HandleFunc("DELETE /spools/{id}", acting(s.deleteSpool))
	mux.HandleFunc("POST /spools/delete", acting(s.deleteSpools))
	mux.HandleFunc("POST /spools/{id}/merge", acting(s.mergeSpool))
	mux.HandleFunc("GET /printers", acting(s.listPrinters))
	mux.HandleFunc("GET /printers/tools", acting(s.allTools))
	mux.HandleFunc("GET /printers/{device}/tools", acting(s.printerTools))
	mux.HandleFunc("PUT /printers/{device}/tools/{tool}", acting(s.assignTool))
	mux.HandleFunc("DELETE /printers/{device}/tools/{tool}", acting(s.unassignTool))
	mux.HandleFunc("POST /printers/{device}/tools/{tool}/spool", acting(s.addReportedSpool))

	// Everything else is this module's own 404, and the path it names is what
	// proves the mount was taken off before the request arrived.
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		refuse(w, NotFound, "this module serves no route at "+r.URL.Path, map[string]any{"path": r.URL.Path})
	})

	return mux
}

// status answers what the module is doing. It is the one route an unconfigured module answers.
func (s *service) status(w http.ResponseWriter, _ *http.Request) {
	printers, tools := s.fo.Counts()
	_, onServer := s.ss.(mirror)

	answer(w, http.StatusOK, map[string]any{
		"configured": true,
		"standalone": !onServer,
		"stats":      s.fo.Stats(),
		"printers":   printers,
		"tools":      tools,
	})
}

// --- catalogue ---------------------------------------------------------------

// importView is the state of the catalogue import, in Materials' shape.
type importView struct {
	Phase    string     `json:"phase"`
	Reason   string     `json:"reason,omitempty"`
	Started  *time.Time `json:"started,omitempty"`
	Finished *time.Time `json:"finished,omitempty"`
	Imported int        `json:"imported"`
	Updated  int        `json:"updated"`
	Skipped  int        `json:"skipped"`
	Error    string     `json:"error,omitempty"`
}

// catalogueView is what SpoolSync holds of the catalogue and its last import.
type catalogueView struct {
	Loaded bool       `json:"loaded"`
	Import importView `json:"import"`
}

func catalogueOf(st spoolsync.SyncStatus, loaded bool) catalogueView {
	v := catalogueView{Loaded: loaded, Import: importView{
		Phase: st.State, Started: st.StartedAt, Finished: st.FinishedAt,
		Imported: st.Imported, Updated: st.Updated, Skipped: st.Skipped,
	}}

	if st.State == "" {
		v.Import.Phase = "idle"
	}

	if st.Error != nil {
		v.Import.Error = *st.Error
	}

	return v
}

// loadedCatalogue is true when SpoolSync holds a filament type from the OpenPrintTag database.
func (s *service) loadedCatalogue(r *http.Request) (bool, error) {
	page, err := s.ss.FilamentTypePage(r.Context(), spoolsync.TypeQuery{Source: "openprinttag", Limit: 1})
	if err != nil {
		return false, err
	}

	return page.Total > 0, nil
}

func (s *service) catalogue(w http.ResponseWriter, r *http.Request, _ string) {
	st, err := s.ss.SyncState(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the catalogue", err)

		return
	}

	loaded, err := s.loadedCatalogue(r)
	if err != nil {
		s.spoolSyncFailed(w, "the catalogue", err)

		return
	}

	answer(w, http.StatusOK, catalogueOf(st, loaded))
}

func (s *service) download(w http.ResponseWriter, r *http.Request, _ string) {
	before, err := s.ss.SyncState(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the catalogue", err)

		return
	}

	if before.State == "running" {
		refuse(w, ImportRunning, "", nil)

		return
	}

	st, err := s.ss.StartSync(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the catalogue", err)

		return
	}

	loaded, _ := s.loadedCatalogue(r)
	answer(w, http.StatusAccepted, catalogueOf(st, loaded))
}

// brandView is a brand. SpoolSync keeps brands by name only, so the name stands for the uuid and slug.
type brandView struct {
	UUID string `json:"uuid"`
	Slug string `json:"slug"`
	Name string `json:"name"`
}

func (s *service) brands(w http.ResponseWriter, r *http.Request, _ string) {
	names, err := s.ss.Brands(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the brands", err)

		return
	}

	out := make([]brandView, 0, len(names))
	for _, n := range names {
		out = append(out, brandView{UUID: n, Slug: trimLower(n), Name: n})
	}

	answer(w, http.StatusOK, map[string]any{"brands": out})
}

func (s *service) types(w http.ResponseWriter, r *http.Request, _ string) {
	types, err := s.ss.FilamentKinds(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the materials", err)

		return
	}

	if types == nil {
		types = []string{}
	}

	answer(w, http.StatusOK, map[string]any{"types": types})
}

// --- materials ---------------------------------------------------------------

// rowView is a material in the list, in Materials' shape.
type rowView struct {
	UUID           string  `json:"uuid"`
	Name           string  `json:"name"`
	BrandName      string  `json:"brand_name"`
	Class          string  `json:"class"`
	Type           string  `json:"type"`
	ColorRGBA      string  `json:"color_rgba"`
	IsFavourite    bool    `json:"is_favourite"`
	IsCustom       bool    `json:"is_custom"`
	Spools         int     `json:"spools"`
	AssignedSpools int     `json:"assigned_spools"`
	RemainingG     float64 `json:"remaining_weight_g"`
	FreeG          float64 `json:"free_g"`
}

// stock is what the spools of one material add up to.
type stock struct {
	spools, assigned int
	remaining, free  float64
}

// stockByType adds every spool up per filament type, and in all. Archived spools count for nothing.
func (s *service) stockByType(spools []spoolsync.Spool) (map[string]stock, float64, int) {
	placed := s.placements()
	by := map[string]stock{}

	var totalG float64

	var totalSpools int

	for _, sp := range spools {
		if sp.ArchivedAt != nil {
			continue
		}

		totalG += sp.RemainingG
		totalSpools++

		st := by[sp.FilamentTypeID]
		st.spools++
		st.remaining += sp.RemainingG

		if _, on := placed[sp.ID]; on {
			st.assigned++
		} else {
			st.free += sp.RemainingG
		}

		by[sp.FilamentTypeID] = st
	}

	return by, totalG, totalSpools
}

// rowOf is one material in Materials' list shape.
func rowOf(ft spoolsync.FilamentType, st stock, favourite bool) rowView {
	return rowView{
		UUID: ft.ID, Name: ft.Name, BrandName: ft.Brand, Class: "FFF", Type: ft.Material,
		ColorRGBA: colourRGBA(ft.Color), IsFavourite: favourite, IsCustom: ft.Custom,
		Spools: st.spools, AssignedSpools: st.assigned, RemainingG: st.remaining, FreeG: st.free,
	}
}

// listing is what every materials read starts from: the spools and their stock, the
// person's favourites, and the materials a person cares about, which are few: those with
// a spool, the favourites, and the custom ones. The rest of the catalogue (some 16,500
// after an import) stays in SpoolSync and is read a page at a time.
type listing struct {
	stock       map[string]stock
	stockG      float64
	stockSpools int
	favourite   map[string]bool
	pinned      []rowView
	pinnedIDs   []string
}

func (s *service) listingOf(r *http.Request, by string) (listing, error) {
	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		return listing{}, err
	}

	favs, err := s.st.Favourites(r.Context(), by)
	if err != nil {
		return listing{}, err
	}

	l := listing{favourite: map[string]bool{}}
	l.stock, l.stockG, l.stockSpools = s.stockByType(spools)

	want := map[string]bool{}

	for _, id := range favs {
		l.favourite[id] = true
		want[id] = true
	}

	for _, sp := range spools {
		if sp.FilamentTypeID != "" {
			want[sp.FilamentTypeID] = true
		}
	}

	ids := make([]string, 0, len(want))
	for id := range want {
		ids = append(ids, id)
	}

	sort.Strings(ids)

	types, err := s.ss.FilamentTypesByID(r.Context(), ids)
	if err != nil {
		return listing{}, err
	}

	custom, err := s.ss.FilamentTypePage(r.Context(), spoolsync.TypeQuery{Source: "custom", Limit: 200})
	if err != nil {
		return listing{}, err
	}

	seen := map[string]bool{}

	for _, ft := range append(types, custom.Items...) {
		if seen[ft.ID] {
			continue
		}

		seen[ft.ID] = true
		l.pinned = append(l.pinned, rowOf(ft, l.stock[ft.ID], l.favourite[ft.ID]))
		l.pinnedIDs = append(l.pinnedIDs, ft.ID)
	}

	return l, nil
}

func (s *service) listMaterials(w http.ResponseWriter, r *http.Request, by string) {
	offset, limit, ok := pageParams(w, r)
	if !ok {
		return
	}

	q := r.URL.Query()

	favourites, _ := boolParam(w, r, "favourites")
	custom, _ := boolParam(w, r, "custom")
	inStock, _ := boolParam(w, r, "in_stock")
	assigned, _ := boolParam(w, r, "assigned")

	l, err := s.listingOf(r, by)
	if err != nil {
		s.spoolSyncFailed(w, "the materials", err)

		return
	}

	needle := trimLower(q.Get("q"))
	brand, typ := trimLower(q.Get("brand")), trimLower(q.Get("type"))
	class := q.Get("class")
	order, desc := q.Get("order"), q.Get("desc") == "true"

	kept := []rowView{}

	for _, row := range l.pinned {
		switch {
		case needle != "" && !strings.Contains(trimLower(row.Name+" "+row.BrandName+" "+row.Type), needle):
		case brand != "" && trimLower(row.BrandName) != brand:
		case typ != "" && trimLower(row.Type) != typ:
		case class != "" && !strings.EqualFold(class, row.Class):
		case favourites && !row.IsFavourite:
		case custom && !row.IsCustom:
		case inStock && row.Spools == 0:
		case assigned && row.AssignedSpools == 0:
		default:
			kept = append(kept, row)
		}
	}

	stockOf := map[string]any{"stock_g": l.stockG, "stock_spools": l.stockSpools}
	reply := func(rows []rowView, total, end int) {
		if rows == nil {
			rows = []rowView{}
		}

		body := map[string]any{"rows": rows, "total": total, "next": nextOffset(end, total)}
		maps.Copy(body, stockOf)
		answer(w, http.StatusOK, body)
	}

	// Favourites, custom, in stock and assigned are all among the pinned materials, and
	// so is every material of another class (SpoolSync holds FFF only): no catalogue read.
	if favourites || custom || inStock || assigned || (class != "" && !strings.EqualFold(class, "FFF")) {
		sortRows(kept, order, desc)

		total := len(kept)
		start := min(offset, total)
		end := min(start+limit, total)
		reply(kept[start:end], total, end)

		return
	}

	remote := spoolsync.TypeQuery{Search: q.Get("q"), Brand: q.Get("brand"), Material: q.Get("type")}

	// Ordered by a column: SpoolSync orders the whole catalogue, and the stock is added on.
	if column := map[string]string{"name": "name", "brand": "brand", "type": "material"}[order]; column != "" {
		remote.OrderBy, remote.Desc, remote.Offset, remote.Limit = column, desc, offset, limit

		page, err := s.ss.FilamentTypePage(r.Context(), remote)
		if err != nil {
			s.spoolSyncFailed(w, "the materials", err)

			return
		}

		rows := make([]rowView, 0, len(page.Items))
		for _, ft := range page.Items {
			rows = append(rows, rowOf(ft, l.stock[ft.ID], l.favourite[ft.ID]))
		}

		reply(rows, page.Total, offset+len(rows))

		return
	}

	// Materials' own order (or by spools or weight): the pinned materials first, as
	// sortRows orders them, then the rest of the catalogue by name. Every material that
	// is not pinned has no spool and is no favourite, so it sorts after them all.
	sortRows(kept, order, desc)

	pinned := len(kept)
	start := min(offset, pinned)
	rows := append([]rowView{}, kept[start:min(offset+limit, pinned)]...)

	remote.Exclude, remote.OrderBy = l.pinnedIDs, "name"
	remote.Offset = max(offset-pinned, 0)
	remote.Limit = max(limit-len(rows), 1)

	page, err := s.ss.FilamentTypePage(r.Context(), remote)
	if err != nil {
		s.spoolSyncFailed(w, "the materials", err)

		return
	}

	if len(rows) < limit {
		for _, ft := range page.Items {
			rows = append(rows, rowOf(ft, stock{}, false))
		}
	}

	reply(rows, pinned+page.Total, offset+len(rows))
}

// nextOffset is the offset of the next page, or nil on the last one.
func nextOffset(end, total int) any {
	if end >= total {
		return nil
	}

	return end
}

// sortRows orders the list. The default is the order Materials shows: favourites first,
// then materials with filament in stock, most filament first, then custom ones, then the rest.
func sortRows(rows []rowView, order string, desc bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		a, b := rows[i], rows[j]

		var less bool

		switch order {
		case "name":
			less = trimLower(a.Name) < trimLower(b.Name)
		case "brand":
			less = trimLower(a.BrandName) < trimLower(b.BrandName)
		case "type":
			less = trimLower(a.Type) < trimLower(b.Type)
		case "spools":
			less = a.Spools < b.Spools
		case "weight":
			less = a.RemainingG < b.RemainingG
		default:
			switch {
			case a.IsFavourite != b.IsFavourite:
				return a.IsFavourite
			case (a.RemainingG > 0) != (b.RemainingG > 0):
				return a.RemainingG > 0
			case a.RemainingG != b.RemainingG:
				return a.RemainingG > b.RemainingG
			case a.IsCustom != b.IsCustom:
				return a.IsCustom
			}

			return trimLower(a.Name) < trimLower(b.Name)
		}

		if order != "" && desc {
			return !less && (trimLower(a.Name) != trimLower(b.Name))
		}

		return less
	})
}

// materialOf finds one material by its uuid, the SpoolSync filament type id.
func (s *service) materialOf(r *http.Request, uuid string) (spoolsync.FilamentType, error) {
	return s.ss.FilamentType(r.Context(), uuid)
}

func (s *service) material(w http.ResponseWriter, r *http.Request, by string) {
	uuid := r.PathValue("uuid")

	ft, err := s.materialOf(r, uuid)
	if err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return
	}

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return
	}

	favs, err := s.st.Favourites(r.Context(), by)
	if err != nil {
		s.storeFailed(w, r, err)

		return
	}

	st, _, _ := s.stockByType(spools)
	row := rowOf(ft, st[ft.ID], slices.Contains(favs, ft.ID))

	// Materials' detail is the row with its extra fields beside it.
	answer(w, http.StatusOK, map[string]any{
		"uuid": row.UUID, "name": row.Name, "brand_name": row.BrandName, "class": row.Class, "type": row.Type,
		"color_rgba": row.ColorRGBA, "is_favourite": row.IsFavourite, "is_custom": row.IsCustom,
		"spools": row.Spools, "assigned_spools": row.AssignedSpools, "remaining_weight_g": row.RemainingG,
		"free_g": row.FreeG, "packages": []any{}, "custom": ifCustom(ft),
	})
}

// ifCustom is the custom material of a filament type, nil for a catalogue one.
func ifCustom(ft spoolsync.FilamentType) any {
	if !ft.Custom {
		return nil
	}

	return customOf(ft)
}

// customView is a custom material in Materials' shape.
type customView struct {
	UUID         string   `json:"uuid"`
	Name         string   `json:"name"`
	BrandName    string   `json:"brand_name"`
	Class        string   `json:"class"`
	Type         string   `json:"type"`
	Abbreviation string   `json:"abbreviation"`
	ColorRGBA    string   `json:"color_rgba"`
	DiameterUM   int      `json:"diameter_um,omitempty"`
	Density      *float64 `json:"density,omitempty"`
}

func customOf(ft spoolsync.FilamentType) customView {
	return customView{
		UUID: ft.ID, Name: ft.Name, BrandName: ft.Brand, Class: "FFF", Type: ft.Material,
		Abbreviation: ft.Material, ColorRGBA: colourRGBA(ft.Color),
		DiameterUM: int(ft.DiameterMM * 1000), Density: ft.DensityG,
	}
}

func (s *service) materialSpools(w http.ResponseWriter, r *http.Request, _ string) {
	uuid := r.PathValue("uuid")

	offset, limit, ok := pageParams(w, r)
	if !ok {
		return
	}

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	views := s.spoolViews(spools, func(sp spoolsync.Spool) bool { return sp.FilamentTypeID == uuid })
	s.answerSpools(w, views, offset, limit)
}

// spoolViews is the spools that pass keep, oldest first, in the module's shape.
func (s *service) spoolViews(spools []spoolsync.Spool, keep func(spoolsync.Spool) bool) []spoolView {
	placed := s.placements()

	var kept []spoolsync.Spool

	for _, sp := range spools {
		if keep(sp) {
			kept = append(kept, sp)
		}
	}

	sort.SliceStable(kept, func(i, j int) bool { return kept[i].CreatedAt.Before(kept[j].CreatedAt) })

	out := make([]spoolView, 0, len(kept))
	for _, sp := range kept {
		var p *follower.Placement
		if pl, ok := placed[sp.ID]; ok {
			p = &pl
		}

		out = append(out, spoolOf(sp, p))
	}

	return out
}

// answerSpools writes one page of spool views as Materials' Spools answer does.
func (s *service) answerSpools(w http.ResponseWriter, views []spoolView, offset, limit int) {
	total := len(views)
	if offset > total {
		offset = total
	}

	end := min(offset+limit, total)

	answer(w, http.StatusOK, map[string]any{
		"spools": views[offset:end], "total": total, "next": nextOffset(end, total),
	})
}

// --- custom materials --------------------------------------------------------

// materialDraft is the body of a custom material, in Materials' MaterialFields.
type materialDraft struct {
	Name         string   `json:"name"`
	BrandName    string   `json:"brand_name"`
	Class        string   `json:"class"`
	Type         string   `json:"type"`
	Abbreviation string   `json:"abbreviation"`
	ColorRGBA    string   `json:"color_rgba"`
	DiameterUM   int      `json:"diameter_um"`
	Density      *float64 `json:"density"`
}

// valid checks a draft as Materials does: a name, a class of FFF or SLA, and texts in bounds.
func (d materialDraft) valid() bool {
	return strings.TrimSpace(d.Name) != "" && (d.Class == "FFF" || d.Class == "SLA") &&
		validText(d.Name, 200) && validText(d.BrandName, 200) && validText(d.Type, 200) && validText(d.Abbreviation, 200)
}

// colour reads a colour of #rrggbb or #rrggbbaa, or nil for none.
func (d materialDraft) colour() *string {
	if d.ColorRGBA == "" {
		return nil
	}

	c := d.ColorRGBA

	return &c
}

func (s *service) createCustom(w http.ResponseWriter, r *http.Request, _ string) {
	var d materialDraft
	if !decodeBody(w, r, &d) {
		return
	}

	if !d.valid() {
		refuse(w, MaterialInvalid, "a custom material needs a name, a class of FFF or SLA, and texts within bounds", nil)

		return
	}

	ft, err := s.ss.CreateFilamentType(r.Context(), draftToFilament(d))
	if err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return
	}

	answer(w, http.StatusCreated, customOf(ft))
}

// draftToFilament maps a custom material to a SpoolSync filament type.
func draftToFilament(d materialDraft) spoolsync.NewFilamentType {
	diameter := 1.75
	if d.DiameterUM > 0 {
		diameter = float64(d.DiameterUM) / 1000
	}

	typ := d.Type
	if typ == "" {
		typ = d.Abbreviation
	}

	return spoolsync.NewFilamentType{
		Brand: d.BrandName, Material: typ, Name: d.Name, Color: d.colour(),
		DiameterMM: diameter, SpoolWeightG: 200, DensityG: d.Density,
	}
}

// customFor reads a custom material, refusing a catalogue one as not found.
func (s *service) customFor(w http.ResponseWriter, r *http.Request) (spoolsync.FilamentType, bool) {
	ft, err := s.ss.FilamentType(r.Context(), r.PathValue("uuid"))
	if err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return ft, false
	}

	if !ft.Custom {
		refuse(w, NotFound, "that material is not a custom one", map[string]any{"uuid": ft.ID})

		return ft, false
	}

	return ft, true
}

func (s *service) updateCustom(w http.ResponseWriter, r *http.Request, _ string) {
	if _, ok := s.customFor(w, r); !ok {
		return
	}

	var d materialDraft
	if !decodeBody(w, r, &d) {
		return
	}

	if !d.valid() {
		refuse(w, MaterialInvalid, "a custom material needs a name, a class of FFF or SLA, and texts within bounds", nil)

		return
	}

	n := draftToFilament(d)
	body := map[string]any{
		"brand": n.Brand, "material": n.Material, "name": n.Name, "diameter_mm": n.DiameterMM,
		"colorHex": n.Color, "density_g_cm3": n.DensityG,
	}

	ft, err := s.ss.UpdateFilamentType(r.Context(), r.PathValue("uuid"), body)
	if err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return
	}

	answer(w, http.StatusOK, customOf(ft))
}

func (s *service) deleteCustom(w http.ResponseWriter, r *http.Request, _ string) {
	uuid := r.PathValue("uuid")

	if _, ok := s.customFor(w, r); !ok {
		return
	}

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	for _, sp := range spools {
		if sp.FilamentTypeID == uuid && sp.ArchivedAt == nil {
			refuse(w, MaterialInUse, "", map[string]any{"uuid": uuid})

			return
		}
	}

	if err = s.ss.DeleteFilamentType(r.Context(), uuid); err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// star makes a material a favourite of the person, or takes it off.
func (s *service) star(on bool) handler {
	return func(w http.ResponseWriter, r *http.Request, by string) {
		uuid := r.PathValue("uuid")

		if _, err := s.ss.FilamentType(r.Context(), uuid); err != nil {
			s.spoolSyncFailed(w, "the material", err)

			return
		}

		if err := s.st.SetFavourite(r.Context(), by, uuid, on); err != nil {
			s.storeFailed(w, r, err)

			return
		}

		w.WriteHeader(http.StatusNoContent)
	}
}

// --- locations ---------------------------------------------------------------

// locationView is a storage location with the spools in stock kept there.
type locationView struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Spools int    `json:"spools"`
}

// locationName checks a location name: non-empty, one line, and at most 200 bytes.
func locationName(name string) bool {
	n := strings.TrimSpace(name)

	return n != "" && len(n) <= 200 && !strings.ContainsAny(n, "\r") && validText(n, 200)
}

func (s *service) locationViews(r *http.Request) ([]locationView, error) {
	locs, err := s.ss.Locations(r.Context())
	if err != nil {
		return nil, err
	}

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		return nil, err
	}

	counts := map[string]int{}

	for _, sp := range spools {
		if sp.ArchivedAt == nil && sp.LocationID != nil {
			counts[*sp.LocationID]++
		}
	}

	out := make([]locationView, 0, len(locs))
	for _, l := range locs {
		out = append(out, locationView{ID: l.ID, Name: l.Name, Spools: counts[l.ID]})
	}

	sort.Slice(out, func(i, j int) bool { return trimLower(out[i].Name) < trimLower(out[j].Name) })

	return out, nil
}

func (s *service) locations(w http.ResponseWriter, r *http.Request, _ string) {
	views, err := s.locationViews(r)
	if err != nil {
		s.spoolSyncFailed(w, "the locations", err)

		return
	}

	answer(w, http.StatusOK, map[string]any{"locations": views})
}

func (s *service) createLocation(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct {
		Name string `json:"name"`
	}

	if !decodeBody(w, r, &body) {
		return
	}

	if !locationName(body.Name) {
		refuse(w, LocationInvalid, "a location needs a name of one line and at most 200 bytes", nil)

		return
	}

	views, err := s.locationViews(r)
	if err != nil {
		s.spoolSyncFailed(w, "the locations", err)

		return
	}

	name := strings.TrimSpace(body.Name)

	for _, v := range views {
		if trimLower(v.Name) == trimLower(name) {
			answer(w, http.StatusOK, v)

			return
		}
	}

	if len(views) >= maxLocations {
		refuse(w, LocationsFull, "", map[string]any{"max": maxLocations})

		return
	}

	l, err := s.ss.CreateLocation(r.Context(), name)
	if err != nil {
		s.spoolSyncFailed(w, "the location", err)

		return
	}

	answer(w, http.StatusOK, locationView{ID: l.ID, Name: l.Name})
}

func (s *service) renameLocation(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")

	var body struct {
		Name string `json:"name"`
	}

	if !decodeBody(w, r, &body) {
		return
	}

	if !locationName(body.Name) {
		refuse(w, LocationInvalid, "a location needs a name of one line and at most 200 bytes", nil)

		return
	}

	views, err := s.locationViews(r)
	if err != nil {
		s.spoolSyncFailed(w, "the locations", err)

		return
	}

	name := strings.TrimSpace(body.Name)

	for _, v := range views {
		if v.ID != id && trimLower(v.Name) == trimLower(name) {
			refuse(w, LocationTaken, "", map[string]any{"name": name})

			return
		}
	}

	l, err := s.ss.UpdateLocation(r.Context(), id, name)
	if err != nil {
		s.spoolSyncFailed(w, "the location", err)

		return
	}

	answer(w, http.StatusOK, locationView{ID: l.ID, Name: l.Name})
}

func (s *service) deleteLocation(w http.ResponseWriter, r *http.Request, _ string) {
	if err := s.ss.DeleteLocation(r.Context(), r.PathValue("id")); err != nil {
		s.spoolSyncFailed(w, "the location", err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- list filters ------------------------------------------------------------

// maxFilters bounds the list filters a person keeps.
const maxFilters = 2048

func (s *service) getFilters(w http.ResponseWriter, r *http.Request, by string) {
	raw, ok, err := s.st.ListFilters(r.Context(), by)
	if err != nil {
		s.storeFailed(w, r, err)

		return
	}

	filters := any(map[string]any{})
	if ok {
		filters = rawJSON(raw)
	}

	answer(w, http.StatusOK, map[string]any{"filters": filters})
}

// rawJSON is stored JSON, written as it is.
type rawJSON string

func (j rawJSON) MarshalJSON() ([]byte, error) { return []byte(j), nil }

func (s *service) putFilters(w http.ResponseWriter, r *http.Request, by string) {
	buf, err := io.ReadAll(io.LimitReader(r.Body, maxFilters+1))

	var object map[string]any
	if err != nil || len(buf) == 0 || len(buf) > maxFilters || jsonUnmarshal(buf, &object) != nil || object == nil {
		refuse(w, InvalidQuery, "the list filters are a JSON object of at most 2048 bytes", nil)

		return
	}

	if err = s.st.SetListFilters(r.Context(), by, string(buf)); err != nil {
		s.storeFailed(w, r, err)

		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// --- spools ------------------------------------------------------------------

func (s *service) listSpools(w http.ResponseWriter, r *http.Request, _ string) {
	offset, limit, ok := pageParams(w, r)
	if !ok {
		return
	}

	material := r.URL.Query().Get("material")

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	views := s.spoolViews(spools, func(sp spoolsync.Spool) bool {
		return material == "" || sp.FilamentTypeID == material
	})
	s.answerSpools(w, views, offset, limit)
}

// spoolDraft is the body of POST /spools, in Materials' SpoolsDraft.
type spoolDraft struct {
	MaterialUUID     string   `json:"material_uuid"`
	FullWeightG      *float64 `json:"full_weight_g"`
	EmptyWeightG     *float64 `json:"empty_weight_g"`
	RemainingWeightG *float64 `json:"remaining_weight_g"`
	Notes            string   `json:"notes"`
	Quantity         int      `json:"quantity"`
	LocationID       string   `json:"location_id"`
}

func (s *service) addSpools(w http.ResponseWriter, r *http.Request, _ string) {
	var d spoolDraft
	if !decodeBody(w, r, &d) {
		return
	}

	if d.MaterialUUID == "" || d.RemainingWeightG == nil || d.Quantity < 1 || d.Quantity > 100 ||
		*d.RemainingWeightG < 0 || *d.RemainingWeightG > 100000 || !validText(d.Notes, 2000) {
		refuse(w, SpoolInvalid, "a spool needs a material, a remaining weight, and a quantity from 1 to 100", nil)

		return
	}

	ft, err := s.ss.FilamentType(r.Context(), d.MaterialUUID)
	if err != nil {
		s.spoolSyncFailed(w, "the material", err)

		return
	}

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	if liveCount(spools)+d.Quantity > s.maxSpools {
		refuse(w, ShelfFull, "", map[string]any{"max": s.maxSpools})

		return
	}

	empty := ft.SpoolWeightG
	if d.EmptyWeightG != nil {
		empty = *d.EmptyWeightG
	}

	net := *d.RemainingWeightG
	if d.FullWeightG != nil {
		net = *d.FullWeightG
	}

	var notes *string
	if d.Notes != "" {
		notes = &d.Notes
	}

	ids := make([]string, 0, d.Quantity)

	for range d.Quantity {
		sp, err := s.ss.Create(r.Context(), spoolsync.NewSpool{
			FilamentTypeID: ft.ID, InitialWeightG: net + empty, LocationID: d.LocationID, Notes: notes,
		})
		if err != nil {
			s.spoolSyncFailed(w, "the spool", err)

			return
		}

		// SpoolSync starts a spool full; the remaining weight is set after.
		gross := *d.RemainingWeightG + empty
		if _, err = s.ss.Update(r.Context(), sp.ID, spoolsync.SpoolUpdate{CurrentWeightG: &gross}); err != nil {
			s.spoolSyncFailed(w, "the spool", err)

			return
		}

		ids = append(ids, sp.ID)
		s.spoolChanged(r, sp.ID)
	}

	answer(w, http.StatusCreated, map[string]any{"added": len(ids), "ids": ids})
}

// spoolPatch is the body of PATCH /spools/{id}, in Materials' SpoolPatch.
type spoolPatch struct {
	RemainingWeightG *float64 `json:"remaining_weight_g"`
	Notes            *string  `json:"notes"`
	LocationID       *string  `json:"location_id"`
	Archived         *bool    `json:"archived"`
	ArchiveReason    *string  `json:"archive_reason"`
	ArchiveNote      *string  `json:"archive_reason_note"`
}

// archiveReasons are the reasons a spool may be archived with.
//
//nolint:gochecknoglobals // a closed table read at run time, never written.
var archiveReasons = map[string]bool{
	"consumed": true, "defective": true, "lost": true, "expired": true, "damaged": true, "other": true,
}

func (s *service) updateSpool(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")

	var p spoolPatch
	if !decodeBody(w, r, &p) {
		return
	}

	sp, err := s.ss.Get(r.Context(), id)
	if err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	if _, onTool := s.placements()[id]; onTool && (p.RemainingWeightG != nil || p.Archived != nil) {
		refuse(w, SpoolAssigned, "", map[string]any{"spool_id": id})

		return
	}

	if p.Archived != nil && *p.Archived && p.ArchiveReason != nil && *p.ArchiveReason != "" && !archiveReasons[*p.ArchiveReason] {
		refuse(w, SpoolInvalid, "the archive reason is not one of the reasons", nil)

		return
	}

	if p.ArchiveNote != nil && !validText(*p.ArchiveNote, 2000) {
		refuse(w, SpoolInvalid, "the archive note is too long or holds a control character", nil)

		return
	}

	if p.Notes != nil && !validText(*p.Notes, 2000) {
		refuse(w, SpoolInvalid, "the notes are too long or hold a control character", nil)

		return
	}

	upd := spoolsync.SpoolUpdate{Notes: p.Notes}

	if p.RemainingWeightG != nil {
		if *p.RemainingWeightG < 0 || *p.RemainingWeightG > 100000 {
			refuse(w, SpoolInvalid, "the remaining weight is from 0 to 100000 g", nil)

			return
		}

		gross := math.Floor(*p.RemainingWeightG + sp.EmptyG)
		upd.CurrentWeightG = &gross
	}

	if p.LocationID != nil {
		loc := *p.LocationID
		upd.LocationID = &loc
	}

	if p.Archived != nil {
		archived := *p.Archived
		upd.Archive = &archived

		if archived {
			upd.ArchiveReason = ""
			if p.ArchiveReason != nil {
				upd.ArchiveReason = *p.ArchiveReason
			}

			if p.ArchiveNote != nil {
				upd.ArchiveNote = *p.ArchiveNote
			}
		}
	}

	updated, err := s.ss.Update(r.Context(), id, upd)
	if err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	s.spoolChanged(r, id)

	answer(w, http.StatusOK, spoolOf(updated, s.placementOf(id)))
}

// placementOf is the tool a spool is on, nil for none, in the shape spoolOf takes.
func (s *service) placementOf(spoolID string) *follower.Placement {
	p, ok := s.placements()[spoolID]
	if !ok {
		return nil
	}

	return &p
}

func (s *service) deleteSpool(w http.ResponseWriter, r *http.Request, _ string) {
	id := r.PathValue("id")

	if _, on := s.placements()[id]; on {
		refuse(w, SpoolAssigned, "", map[string]any{"spool_id": id})

		return
	}

	if err := s.ss.DeleteSpool(r.Context(), id); err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	s.spoolDeleted(r, id)
	w.WriteHeader(http.StatusNoContent)
}

func (s *service) deleteSpools(w http.ResponseWriter, r *http.Request, _ string) {
	var body struct {
		IDs []string `json:"ids"`
	}

	if !decodeBody(w, r, &body) {
		return
	}

	if len(body.IDs) < 1 || len(body.IDs) > 100 {
		refuse(w, SpoolInvalid, "delete from 1 to 100 spools at once", nil)

		return
	}

	placed := s.placements()

	// All or none: every spool is checked before the first is deleted.
	for _, id := range body.IDs {
		if _, err := s.ss.Get(r.Context(), id); err != nil {
			s.spoolSyncFailed(w, "the spool "+id, err)

			return
		}

		if _, on := placed[id]; on {
			refuse(w, SpoolAssigned, "", map[string]any{"spool_id": id})

			return
		}
	}

	for _, id := range body.IDs {
		if err := s.ss.DeleteSpool(r.Context(), id); err != nil {
			s.spoolSyncFailed(w, "the spool "+id, err)

			return
		}

		s.spoolDeleted(r, id)
	}

	w.WriteHeader(http.StatusNoContent)
}

// mergeRequest names the printer spool a manual spool is merged into.
type mergeRequest struct {
	Into string `json:"into"`
}

func (s *service) mergeSpool(w http.ResponseWriter, r *http.Request, _ string) {
	manualID := r.PathValue("id")

	var body mergeRequest
	if !decodeBody(w, r, &body) || body.Into == "" {
		refuse(w, InvalidQuery, "the body names the spool to merge into", nil)

		return
	}

	manual, err := s.ss.Get(r.Context(), manualID)
	if err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	printer, err := s.ss.Get(r.Context(), body.Into)
	if err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	if manual.Source != "manual" || printer.Source != "printer" || manual.FilamentTypeID != printer.FilamentTypeID {
		refuse(w, MergeRefused, "only a manual spool of the same material merges into a printer's", nil)

		return
	}

	if _, on := s.placements()[manualID]; on {
		refuse(w, SpoolAssigned, "", map[string]any{"spool_id": manualID})

		return
	}

	// The printer spool takes the other's location and notes where it has none.
	upd := spoolsync.SpoolUpdate{}
	if printer.LocationID == nil && manual.LocationID != nil {
		loc := *manual.LocationID
		upd.LocationID = &loc
	}

	if (printer.Notes == nil || *printer.Notes == "") && manual.Notes != nil && *manual.Notes != "" {
		notes := *manual.Notes
		upd.Notes = &notes
	}

	merged := printer

	if upd.LocationID != nil || upd.Notes != nil {
		if merged, err = s.ss.Update(r.Context(), printer.ID, upd); err != nil {
			s.spoolSyncFailed(w, "the spool", err)

			return
		}
	}

	if err = s.ss.DeleteSpool(r.Context(), manualID); err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	s.spoolDeleted(r, manualID)
	s.spoolChanged(r, merged.ID)

	answer(w, http.StatusOK, spoolOf(merged, s.placementOf(merged.ID)))
}

// spoolChanged tells the follower a spool was set by hand, and the event stream the spool changed.
func (s *service) spoolChanged(r *http.Request, id string) {
	s.fo.SpoolChanged(id)
	s.emitSpool(r, id, false)
}

// spoolDeleted tells the event stream a spool is gone.
func (s *service) spoolDeleted(r *http.Request, id string) {
	s.fo.SpoolChanged(id)

	ev := spoolEvent{SpoolID: id, Deleted: true}
	if err := s.emit.Emit(r.Context(), endpointSpool, "spool/"+id, ev); err != nil {
		s.log.Warn("could not tell of a spool", "spool", id, "error", err)
	}
}

// spoolEvent is the payload of a spool event, in Materials' shape with string ids.
type spoolEvent struct {
	SpoolID          string  `json:"spool_id"`
	SpoolUUID        string  `json:"spool_uuid,omitempty"`
	MaterialUUID     string  `json:"material_uuid,omitempty"`
	RemainingWeightG float64 `json:"remaining_weight_g"`
	Deleted          bool    `json:"deleted,omitempty"`
}

// emitSpool tells the event stream what a spool holds now.
func (s *service) emitSpool(r *http.Request, id string, _ bool) {
	sp, err := s.ss.Get(r.Context(), id)
	if err != nil {
		return
	}

	ev := spoolEvent{SpoolID: id, MaterialUUID: sp.FilamentTypeID, RemainingWeightG: sp.RemainingG}
	if sp.OptInstanceUUID != nil {
		ev.SpoolUUID = *sp.OptInstanceUUID
	}

	if err = s.emit.Emit(r.Context(), endpointSpool, "spool/"+id, ev); err != nil {
		s.log.Warn("could not tell of a spool", "spool", id, "error", err)
	}
}

// emitSpoolWritten tells the event stream a spool's weight was written from a
// printer's report. The follower calls it once per whole gram.
func (s *service) emitSpoolWritten(spoolID string, _ float64) {
	sp, err := s.ss.Get(context.Background(), spoolID)
	if err != nil {
		return
	}

	ev := spoolEvent{SpoolID: spoolID, MaterialUUID: sp.FilamentTypeID, RemainingWeightG: sp.RemainingG}
	if sp.OptInstanceUUID != nil {
		ev.SpoolUUID = *sp.OptInstanceUUID
	}

	if err = s.emit.Emit(context.Background(), endpointSpool, "spool/"+spoolID, ev); err != nil {
		s.log.Warn("could not tell of a spool", "spool", spoolID, "error", err)
	}
}
