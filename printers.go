package main

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/assign"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/follower"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/store"
)

// printerView is a printer in Materials' shape.
type printerView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Model      string `json:"model"`
	Connected  bool   `json:"connected"`
	State      string `json:"state"`
	MayOperate bool   `json:"may_operate"`
}

// reportedView is what a printer reports of the material on a tool.
type reportedView struct {
	Abbreviation string   `json:"abbreviation"`
	InstanceUUID string   `json:"instance_uuid"`
	PackageUUID  string   `json:"package_uuid"`
	MaterialUUID string   `json:"material_uuid"`
	Type         string   `json:"type"`
	ColorRGBA    string   `json:"color_rgba"`
	FullWeightG  *float64 `json:"full_weight_g,omitempty"`
}

// nozzleView is the nozzle of a head.
type nozzleView struct {
	DiameterMM *float64 `json:"diameter_mm,omitempty"`
	HighFlow   bool     `json:"high_flow"`
	Hardened   bool     `json:"hardened"`
	HighTemp   bool     `json:"high_temp"`
}

// toolView is one tool of a printer, in Materials' shape.
type toolView struct {
	ID         string          `json:"id"`
	Assignment *assignmentView `json:"assignment"`
	Reported   *reportedView   `json:"reported"`
	FromNFC    bool            `json:"from_nfc"`
	Assign     assign.Reason   `json:"assign"`
	Unassign   assign.Reason   `json:"unassign"`
	Kind       string          `json:"kind"`
	Active     bool            `json:"active"`
	Nozzle     nozzleView      `json:"nozzle"`
	Feeder     string          `json:"feeder,omitempty"`
}

// toolRowView is one tool of a page of printers, with the material and spool on it.
type toolRowView struct {
	DeviceID         string   `json:"device_id"`
	ToolID           string   `json:"tool_id"`
	MaterialUUID     string   `json:"material_uuid,omitempty"`
	MaterialName     string   `json:"material_name,omitempty"`
	BrandName        string   `json:"brand_name,omitempty"`
	Type             string   `json:"type,omitempty"`
	Abbreviation     string   `json:"abbreviation,omitempty"`
	ColorRGBA        string   `json:"color_rgba,omitempty"`
	By               string   `json:"by,omitempty"`
	FromNFC          bool     `json:"from_nfc"`
	SpoolID          string   `json:"spool_id,omitempty"`
	SpoolUUID        string   `json:"spool_uuid,omitempty"`
	RemainingWeightG *float64 `json:"remaining_weight_g,omitempty"`
	FullWeightG      *float64 `json:"full_weight_g,omitempty"`
}

// toolEvent is the payload of a tool event, in Materials' shape with string ids.
type toolEvent struct {
	DeviceID     string `json:"device_id"`
	ToolID       string `json:"tool_id"`
	MaterialUUID string `json:"material_uuid,omitempty"`
	SpoolID      string `json:"spool_id,omitempty"`
}

// emitTool tells the event stream what a tool holds now. The follower calls it when a binding changes.
func (s *service) emitTool(printerID, toolID string) {
	ev := toolEvent{DeviceID: printerID, ToolID: toolID}

	entries, _ := s.fo.Tools(printerID)
	for _, e := range entries {
		if string(e.Tool.ID) == toolID {
			ev.MaterialUUID, ev.SpoolID = e.Binding.FilamentTypeID, e.Binding.SpoolID
		}
	}

	if err := s.emit.Emit(context.Background(), endpointTool, printerID+"/"+toolID, ev); err != nil {
		s.log.Warn("could not tell of a tool", "device", printerID, "tool", toolID, "error", err)
	}
}

// observed finds a printer the follower holds that the caller may observe. A printer
// the module does not follow, or one the caller may not observe, is NOT_FOUND.
func (s *service) observed(w http.ResponseWriter, r *http.Request, by, device string) (follower.Printer, bool) {
	p, ok := s.fo.Printer(device)
	if !ok {
		refuse(w, NotFound, "", map[string]any{"device_id": device})

		return p, false
	}

	observe, _, err := s.rights(r.Context(), by, device)
	if err != nil {
		s.log.Warn("access unknown", "device", device, "error", err)
		refuse(w, AccessUnknown, "", nil)

		return p, false
	}

	if !observe {
		refuse(w, NotFound, "", map[string]any{"device_id": device})

		return p, false
	}

	return p, true
}

// operating is observed, and then the caller must operate the printer.
func (s *service) operating(w http.ResponseWriter, r *http.Request, by, device string) (follower.Printer, bool) {
	p, ok := s.observed(w, r, by, device)
	if !ok {
		return p, false
	}

	operate, err := s.may(r.Context(), by, "operate", device)
	if err != nil {
		s.log.Warn("access unknown", "device", device, "error", err)
		refuse(w, AccessUnknown, "", nil)

		return p, false
	}

	if !operate {
		refuse(w, NotOperator, "", map[string]any{"device_id": device})

		return p, false
	}

	return p, true
}

func (s *service) listPrinters(w http.ResponseWriter, r *http.Request, by string) {
	offset, limit, ok := pageParams(w, r)
	if !ok {
		return
	}

	q := trimLower(r.URL.Query().Get("q"))
	ids := map[string]bool{}

	for _, id := range r.URL.Query()["id"] {
		ids[id] = true
	}

	views := []printerView{}

	for _, p := range s.fo.Printers() {
		if q != "" && !strings.Contains(trimLower(p.Name+" "+p.Model), q) {
			continue
		}

		if len(ids) > 0 && !ids[p.ID] {
			continue
		}

		observe, operate, err := s.rights(r.Context(), by, p.ID)
		if err != nil {
			refuse(w, AccessUnknown, "", nil)

			return
		}

		if !observe {
			continue
		}

		views = append(views, printerView{
			ID: p.ID, Name: p.Name, Model: p.Model, Connected: p.Connected, State: p.State, MayOperate: operate,
		})
	}

	total := len(views)
	if offset > total {
		offset = total
	}

	end := min(offset+limit, total)

	answer(w, http.StatusOK, map[string]any{"printers": views[offset:end], "total": total, "next": nextOffset(end, total)})
}

// spoolsByID is every spool SpoolSync holds, by id, for the tool views.
func (s *service) spoolsByID(ctx context.Context) (map[string]spoolsync.Spool, error) {
	all, err := s.spoolsAll(ctx)
	if err != nil {
		return nil, err
	}

	out := make(map[string]spoolsync.Spool, len(all))
	for _, sp := range all {
		out[sp.ID] = sp
	}

	return out, nil
}

// toolViewOf is one tool of a printer, with what is on it and why a change would be refused.
func (s *service) toolViewOf(r *http.Request, p follower.Printer, e follower.ToolEntry, spools map[string]spoolsync.Spool) toolView {
	t := e.Tool
	state := assign.StateOf(p.Connected, p.State)
	at := assign.ToolOf(t)

	v := toolView{ID: string(t.ID), Kind: "slot", Active: t.Active != nil && *t.Active}
	if t.ID.IsHead() {
		v.Kind = "head"
	}

	if t.Reported != nil && t.Reported.InstanceUUID != "" {
		v.FromNFC = true
	}

	if t.Material != nil {
		rep := reportedView{Abbreviation: *t.Material}
		if t.Reported != nil {
			rep.InstanceUUID = t.Reported.InstanceUUID
			rep.PackageUUID = t.Reported.PackageUUID
			rep.MaterialUUID = t.Reported.MaterialUUID
			rep.Type = t.Reported.Type
			rep.ColorRGBA = t.Reported.ColorRGBA
			rep.FullWeightG = t.Reported.FullWeight
		}

		v.Reported = &rep
	}

	// The candidate is what the tool reports, so the reasons shown are the ones
	// that do not depend on the material picked.
	cand := assign.Candidate{}
	if at.Reported != nil {
		cand = assign.Candidate{Abbreviation: *at.Reported, Type: *at.Reported}
	}

	v.Assign = assign.MayAssign(state, at, cand)
	v.Unassign = assign.MayUnassign(state, at)

	if t.Feeder != nil && t.Feeder.Model != "" {
		v.Feeder = t.Feeder.Model
	}

	v.Nozzle = nozzleOf(t)

	// The assignment: a spool the tag names, or what a person put on the tool.
	if e.Binding.SpoolID != "" && e.Binding.Source == "tag" {
		if sp, ok := spools[e.Binding.SpoolID]; ok {
			v.Assignment = &assignmentView{
				DeviceID: p.ID, ToolID: string(t.ID), MaterialUUID: sp.FilamentTypeID,
				MaterialSource: materialSource(sp), MaterialName: sp.Filament.Name, BrandName: sp.Filament.Brand,
				Type: sp.Filament.Material, Abbreviation: sp.Filament.Material, ColorRGBA: colourRGBA(sp.Filament.Color),
				SpoolID: sp.ID, By: "printer", UpdatedAt: sp.UpdatedAt,
			}
		}
	}

	if e.Binding.Source == "assigned" {
		if a, ok, err := s.st.Assignment(r.Context(), p.ID, string(t.ID)); err == nil && ok {
			v.Assignment = s.assignmentFor(r, p.ID, string(t.ID), a, spools)
		}
	}

	return v
}

// materialSource is "catalogue" for a filament type with an OpenPrintTag uuid, else "custom".
func materialSource(sp spoolsync.Spool) string {
	if sp.Filament.OptMaterialUUID != nil {
		return "catalogue"
	}

	return "custom"
}

// assignmentFor is a stored assignment as the module answers it.
func (s *service) assignmentFor(r *http.Request, printerID, toolID string, a store.Assignment, spools map[string]spoolsync.Spool) *assignmentView {
	v := &assignmentView{
		DeviceID: printerID, ToolID: toolID, MaterialUUID: a.FilamentTypeID, By: "person",
		UpdatedAt: a.AssignedAt, MaterialSource: "custom",
	}

	if a.SpoolID != "" {
		v.SpoolID = a.SpoolID
	}

	if sp, ok := spools[a.SpoolID]; ok {
		v.MaterialUUID = sp.FilamentTypeID
		v.MaterialSource = materialSource(sp)
		v.MaterialName, v.BrandName = sp.Filament.Name, sp.Filament.Brand
		v.Type, v.Abbreviation = sp.Filament.Material, sp.Filament.Material
		v.ColorRGBA = colourRGBA(sp.Filament.Color)
	} else if ft, err := s.ss.FilamentType(r.Context(), a.FilamentTypeID); err == nil {
		v.MaterialSource = "custom"

		if ft.OptMaterialUUID != nil {
			v.MaterialSource = "catalogue"
		}

		v.MaterialName, v.BrandName = ft.Name, ft.Brand
		v.Type, v.Abbreviation = ft.Material, ft.Material
		v.ColorRGBA = colourRGBA(ft.Color)
	}

	return v
}

// nozzleOf reads a head's nozzle from its features.
func nozzleOf(t connectorapi.Tool) nozzleView {
	var n nozzleView

	if raw, ok := t.Feature(connectorapi.FeatureNozzleDiameter); ok {
		var d float64
		if jsonUnmarshal(raw, &d) == nil {
			n.DiameterMM = &d
		}
	}

	if raw, ok := t.Feature(connectorapi.FeatureNozzleHighFlow); ok {
		_ = jsonUnmarshal(raw, &n.HighFlow)
	}

	if raw, ok := t.Feature(connectorapi.FeatureNozzleHardened); ok {
		_ = jsonUnmarshal(raw, &n.Hardened)
	}

	if raw, ok := t.Feature(connectorapi.FeatureNozzleHighTemp); ok {
		_ = jsonUnmarshal(raw, &n.HighTemp)
	}

	return n
}

func (s *service) printerTools(w http.ResponseWriter, r *http.Request, by string) {
	device := r.PathValue("device")

	p, ok := s.observed(w, r, by, device)
	if !ok {
		return
	}

	entries, _ := s.fo.Tools(device)

	spools, err := s.spoolsByID(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	tools := []toolView{}
	for _, e := range entries {
		tools = append(tools, s.toolViewOf(r, p, e, spools))
	}

	operate, err := s.may(r.Context(), by, "operate", device)
	if err != nil {
		refuse(w, AccessUnknown, "", nil)

		return
	}

	answer(w, http.StatusOK, map[string]any{
		"id": p.ID, "name": p.Name, "model": p.Model, "connected": p.Connected, "state": p.State,
		"may_operate": operate, "tools": tools,
	})
}

// toolEntry finds one enabled tool of a printer, and refuses NOT_FOUND for one that is not there.
func (s *service) toolEntry(w http.ResponseWriter, device, tool string) (follower.ToolEntry, bool) {
	entries, _ := s.fo.Tools(device)

	for _, e := range entries {
		if string(e.Tool.ID) == tool {
			return e, true
		}
	}

	refuse(w, NotFound, "", map[string]any{"device_id": device, "tool_id": tool})

	return follower.ToolEntry{}, false
}

// assignRequest names a material, or a spool and its material, and not both.
type assignRequest struct {
	MaterialUUID string `json:"material_uuid,omitempty"`
	SpoolID      string `json:"spool_id,omitempty"`
}

func (s *service) assignTool(w http.ResponseWriter, r *http.Request, by string) {
	device, tool := r.PathValue("device"), r.PathValue("tool")

	if _, ok := toolID(tool); !ok {
		refuse(w, NotFound, "", map[string]any{"device_id": device, "tool_id": tool})

		return
	}

	p, ok := s.operating(w, r, by, device)
	if !ok {
		return
	}

	var body assignRequest
	if !decodeBody(w, r, &body) {
		return
	}

	if (body.MaterialUUID == "") == (body.SpoolID == "") {
		refuse(w, InvalidQuery, "the body names a material_uuid or a spool_id, and only one of the two", nil)

		return
	}

	e, ok := s.toolEntry(w, device, tool)
	if !ok {
		return
	}

	cand := assign.Candidate{}

	var filamentID, spoolID string

	if body.SpoolID != "" {
		sp, err := s.ss.Get(r.Context(), body.SpoolID)
		if err != nil {
			s.spoolSyncFailed(w, "the spool", err)

			return
		}

		if sp.ArchivedAt != nil {
			refuse(w, SpoolArchived, "", map[string]any{"spool_id": sp.ID})

			return
		}

		if pl, on := s.fo.Placements()[sp.ID]; on && (pl.PrinterID != device || pl.Tool != tool) {
			refuse(w, SpoolOnAnotherTool, "", map[string]any{"spool_id": sp.ID, "device_id": pl.PrinterID, "tool_id": pl.Tool})

			return
		}

		filamentID, spoolID = sp.FilamentTypeID, sp.ID
		cand = assign.Candidate{Abbreviation: sp.Filament.Material, Type: sp.Filament.Material}
	} else {
		ft, err := s.ss.FilamentType(r.Context(), body.MaterialUUID)
		if err != nil {
			s.spoolSyncFailed(w, "the material", err)

			return
		}

		filamentID = ft.ID
		cand = assign.Candidate{Abbreviation: ft.Material, Type: ft.Material}
	}

	state := assign.StateOf(p.Connected, p.State)
	if reason := assign.MayAssign(state, assign.ToolOf(e.Tool), cand); reason != assign.Allowed {
		refuse(w, ToolRefused, "the tool cannot be changed now: "+string(reason),
			map[string]any{"reason": string(reason), "state": p.State})

		return
	}

	s.setAssignment(w, r, device, tool, e, store.Assignment{
		FilamentTypeID: filamentID, SpoolID: spoolID, AssignedBy: by, AssignedAt: time.Now().UTC(),
	})
}

// setAssignment stores what a person put on a tool, takes the tool's reading as the
// odometer baseline, and applies it.
func (s *service) setAssignment(w http.ResponseWriter, r *http.Request, device, tool string, e follower.ToolEntry, a store.Assignment) {
	a.PrinterID, a.Tool = device, tool

	if err := s.st.SetAssignment(r.Context(), a); err != nil {
		s.storeFailed(w, r, err)

		return
	}

	// The count starts from what the tool reads now, not from an old reading.
	var err error
	if e.Tool.OdometerE != nil {
		err = s.st.SetOdometer(r.Context(), device, tool, store.Odometer{
			ID: e.Tool.OdometerID, E: *e.Tool.OdometerE, Weight: e.Tool.OdometerWeight,
		}, time.Now())
	} else {
		err = s.st.DeleteOdometer(r.Context(), device, tool)
	}

	if err != nil {
		s.storeFailed(w, r, err)

		return
	}

	s.fo.Refresh(r.Context(), device)

	spools, err := s.spoolsByID(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	p, _ := s.fo.Printer(device)
	entries, _ := s.fo.Tools(device)

	for _, en := range entries {
		if string(en.Tool.ID) == tool {
			answer(w, http.StatusOK, s.toolViewOf(r, p, en, spools).Assignment)

			return
		}
	}

	answer(w, http.StatusOK, nil)
}

func (s *service) unassignTool(w http.ResponseWriter, r *http.Request, by string) {
	device, tool := r.PathValue("device"), r.PathValue("tool")

	p, ok := s.operating(w, r, by, device)
	if !ok {
		return
	}

	e, ok := s.toolEntry(w, device, tool)
	if !ok {
		return
	}

	state := assign.StateOf(p.Connected, p.State)
	if reason := assign.MayUnassign(state, assign.ToolOf(e.Tool)); reason != assign.Allowed {
		refuse(w, ToolRefused, "the tool cannot be changed now: "+string(reason),
			map[string]any{"reason": string(reason), "state": p.State})

		return
	}

	if _, err := s.st.DeleteAssignment(r.Context(), device, tool); err != nil {
		s.storeFailed(w, r, err)

		return
	}

	if err := s.st.DeleteOdometer(r.Context(), device, tool); err != nil {
		s.storeFailed(w, r, err)

		return
	}

	s.fo.Refresh(r.Context(), device)
	w.WriteHeader(http.StatusNoContent)
}

// addReportedSpool adds the spool a printer read off its NFC tag, and puts it on the tool.
func (s *service) addReportedSpool(w http.ResponseWriter, r *http.Request, by string) {
	device, tool := r.PathValue("device"), r.PathValue("tool")

	if _, ok := s.operating(w, r, by, device); !ok {
		return
	}

	e, ok := s.toolEntry(w, device, tool)
	if !ok {
		return
	}

	rep := e.Tool.Reported
	if rep == nil || rep.InstanceUUID == "" {
		refuse(w, NotReported, "", map[string]any{"device_id": device, "tool_id": tool})

		return
	}

	existing, err := s.ss.FindByInstanceUUID(r.Context(), rep.InstanceUUID)

	switch {
	case err == nil:
		// A tag a spool already carries adds nothing. An archived spool comes back.
		if existing.ArchivedAt != nil {
			f := false
			if existing, err = s.ss.Update(r.Context(), existing.ID, spoolsync.SpoolUpdate{Archive: &f}); err != nil {
				s.spoolSyncFailed(w, "the spool", err)

				return
			}
		}

		s.fo.Refresh(r.Context(), device)
		answer(w, http.StatusOK, spoolOf(existing, s.placementOf(existing.ID)))

		return
	case err != nil && !isNotFound(err):
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	spools, err := s.spoolsAll(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	if liveCount(spools)+1 > s.maxSpools {
		refuse(w, ShelfFull, "", map[string]any{"max": s.maxSpools})

		return
	}

	// The catalogue's material for the tag, when it holds one. A custom one is
	// made only once the weight is settled, so a refusal for want of a weight
	// leaves nothing behind.
	var ft spoolsync.FilamentType

	known := false

	if rep.MaterialUUID != "" {
		switch ft, err = s.ss.FilamentTypeByOpt(r.Context(), rep.MaterialUUID); {
		case err == nil:
			known = true
		case !errors.Is(err, spoolsync.ErrNotFound):
			s.spoolSyncFailed(w, "the material", err)

			return
		}
	}

	// A spool whose full weight nobody knows needs the person's: how much
	// filament is left on it, and if they know it, how much it held new.
	var given struct {
		RemainingWeightG *float64 `json:"remaining_weight_g"`
		FullWeightG      *float64 `json:"full_weight_g"`
	}

	if r.ContentLength != 0 && !decodeBody(w, r, &given) {
		return
	}

	for _, g := range []*float64{given.RemainingWeightG, given.FullWeightG} {
		if g != nil && (*g < 0 || *g > 100000) {
			refuse(w, SpoolInvalid, "a weight is from 0 to 100000 grams", nil)

			return
		}
	}

	// The full weight is the person's, else the tag's weighed one, else the
	// nominal weight of the catalogue's package for the material. What the
	// tag says was used comes off it.
	full, remaining := rep.FullWeight, given.RemainingWeightG
	if given.FullWeightG != nil {
		full = given.FullWeightG
	}

	if full == nil && known {
		full = ft.NetWeightG
	}

	if remaining == nil && full != nil {
		left := *full
		if e.Tool.ConsumedWeight != nil {
			left = max(0, *full-*e.Tool.ConsumedWeight)
		}

		remaining = &left
	}

	if remaining == nil {
		refuse(w, WeightNeeded, "", map[string]any{"device_id": device, "tool_id": tool})

		return
	}

	if full == nil || *full < *remaining {
		full = remaining
	}

	if !known {
		ftID, err := s.materialForTag(r, rep)
		if err == nil {
			ft, err = s.ss.FilamentType(r.Context(), ftID)
		}

		if err != nil {
			s.spoolSyncFailed(w, "the material", err)

			return
		}
	}

	uuid := rep.InstanceUUID
	sp, err := s.ss.Create(r.Context(), spoolsync.NewSpool{
		FilamentTypeID: ft.ID, InitialWeightG: *full + ft.SpoolWeightG, OptInstanceUUID: uuid,
	})
	if err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	gross := *remaining + ft.SpoolWeightG
	if sp, err = s.ss.Update(r.Context(), sp.ID, spoolsync.SpoolUpdate{CurrentWeightG: &gross}); err != nil {
		s.spoolSyncFailed(w, "the spool", err)

		return
	}

	// The follower took the tag for one SpoolSync does not hold; from now on
	// it counts what the printer uses off it.
	s.fo.TagAdded(uuid)
	s.fo.Refresh(r.Context(), device)
	s.spoolChanged(r, sp.ID)

	answer(w, http.StatusCreated, spoolOf(sp, s.placementOf(sp.ID)))
}

// materialForTag is the filament type of a tag: the catalogue's when it holds the reported
// material uuid, else a custom one made from what the printer reports.
func (s *service) materialForTag(r *http.Request, rep *connectorapi.ReportedMaterial) (string, error) {
	if rep.MaterialUUID != "" {
		ft, err := s.ss.FilamentTypeByOpt(r.Context(), rep.MaterialUUID)

		switch {
		case err == nil:
			return ft.ID, nil
		case !errors.Is(err, spoolsync.ErrNotFound):
			return "", err
		}
	}

	typ := rep.Type
	if typ == "" {
		typ = "PLA"
	}

	created, err := s.ss.CreateFilamentType(r.Context(), spoolsync.NewFilamentType{
		// The tag's material uuid rides along, so the catalogue import turns
		// this stand-in into the real material, with its spools on it.
		OptMaterialUUID: nonEmpty(rep.MaterialUUID),
		Brand:           "", Material: typ, Name: typ, Color: nonEmpty(rep.ColorRGBA), DiameterMM: 1.75, SpoolWeightG: 200,
	})
	if err != nil {
		return "", err
	}

	return created.ID, nil
}

// nonEmpty is a pointer to the text, nil for the empty text.
func nonEmpty(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

// allTools answers the tools of one page of the printers the caller may observe.
func (s *service) allTools(w http.ResponseWriter, r *http.Request, by string) {
	offset, limit, ok := pageParams(w, r)
	if !ok {
		return
	}

	spools, err := s.spoolsByID(r.Context())
	if err != nil {
		s.spoolSyncFailed(w, "the spools", err)

		return
	}

	var visible []follower.Printer

	for _, p := range s.fo.Printers() {
		observe, _, err := s.rights(r.Context(), by, p.ID)
		if err != nil {
			refuse(w, AccessUnknown, "", nil)

			return
		}

		if observe {
			visible = append(visible, p)
		}
	}

	total := len(visible)
	if offset > total {
		offset = total
	}

	end := min(offset+limit, total)

	rows := []toolRowView{}

	for _, p := range visible[offset:end] {
		entries, _ := s.fo.Tools(p.ID)
		for _, e := range entries {
			rows = append(rows, toolRowOf(p, e, spools))
		}
	}

	answer(w, http.StatusOK, map[string]any{"tools": rows, "total": total, "next": nextOffset(end, total)})
}

// toolRowOf is one tool as a row of the page of tools.
func toolRowOf(p follower.Printer, e follower.ToolEntry, spools map[string]spoolsync.Spool) toolRowView {
	t := e.Tool
	row := toolRowView{DeviceID: p.ID, ToolID: string(t.ID), FromNFC: t.Reported != nil && t.Reported.InstanceUUID != ""}

	if t.Material != nil {
		row.Abbreviation = *t.Material
	}

	if e.Binding.Source == "tag" {
		row.By = "printer"
	} else if e.Binding.Source == "assigned" {
		row.By = "person"
	}

	row.MaterialUUID = e.Binding.FilamentTypeID

	if sp, ok := spools[e.Binding.SpoolID]; ok {
		row.SpoolID, row.SpoolUUID = sp.ID, ""
		row.MaterialUUID = sp.FilamentTypeID
		row.MaterialName, row.BrandName = sp.Filament.Name, sp.Filament.Brand
		row.Type, row.ColorRGBA = sp.Filament.Material, colourRGBA(sp.Filament.Color)
		remaining, full := sp.RemainingG, sp.InitialG
		row.RemainingWeightG, row.FullWeightG = &remaining, &full

		if sp.OptInstanceUUID != nil {
			row.SpoolUUID = *sp.OptInstanceUUID
		}
	}

	return row
}

// isNotFound is true for a SpoolSync answer of 404.
func isNotFound(err error) bool { return errors.Is(err, spoolsync.ErrNotFound) }

// linkTagged puts a tagged spool on the material its tag names. A spool on a
// stand-in custom material moves to the catalogue's material when SpoolSync
// holds it, and otherwise the stand-in takes the tag's material uuid, so the
// next catalogue import turns it into the real one. It answers whether the
// spool moved.
func (s *service) linkTagged(ctx context.Context, spoolID, filamentTypeID, materialUUID string) bool {
	ft, err := s.ss.FilamentType(ctx, filamentTypeID)
	if err != nil || !ft.Custom {
		return false
	}

	real, err := s.ss.FilamentTypeByOpt(ctx, materialUUID)

	switch {
	case err == nil && real.ID != ft.ID:
		if _, err = s.ss.Update(ctx, spoolID, spoolsync.SpoolUpdate{FilamentTypeID: &real.ID}); err != nil {
			s.log.Warn("could not move a tagged spool to its material", "spool", spoolID, "material", real.ID, "error", err)

			return false
		}

		s.log.Info("moved a tagged spool to its catalogue material", "spool", spoolID, "from", ft.ID, "to", real.ID)

		return true
	case errors.Is(err, spoolsync.ErrNotFound) && ft.OptMaterialUUID == nil:
		if _, err = s.ss.UpdateFilamentType(ctx, ft.ID, map[string]any{"optMaterialUuid": materialUUID}); err != nil {
			s.log.Warn("could not name the tag's material on a stand-in", "material", ft.ID, "error", err)
		}
	}

	return false
}
