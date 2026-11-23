package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/spool-sync/connect/internal/follower"
	"github.com/spool-sync/connect/internal/spoolsync"
	"github.com/spool-sync/connect/internal/store"
)

// The events module.yaml declares under events, and the types they make.
const (
	endpointTool  = "tool"
	endpointSpool = "spool"
)

// defaultLimit is what a page answers when the caller names no limit; maxLimit is the most.
const (
	defaultLimit = 50
	maxLimit     = 100
	// maxLocations is how many storage locations the module keeps.
	maxLocations = 100
	// maxBody bounds a request body.
	maxBody = 1 << 20
	// operateTTL is how long an answer about a person, action and printer is kept.
	operateTTL = 30 * time.Second
)

// accessAsker asks the host whether a person may do one thing to one object.
type accessAsker interface {
	Check(ctx context.Context, params accessapi.CheckParams) (accessapi.Decision, error)
}

// emitter publishes one event on the host's stream.
type emitter interface {
	Emit(ctx context.Context, endpoint, key string, data any) error
}

// service is what every handler reads: the inventory, the module's own database,
// the follower of the printers, the host's access check and the event stream.
type service struct {
	ss        inventory
	st        *store.Store
	fo        *follower.Follower
	access    accessAsker
	emit      emitter
	maxSpools int
	log       *slog.Logger

	mu       sync.Mutex
	operates map[operateKey]operateAnswer

	// mirrorKick asks runMirror for a report now.
	mirrorKick chan struct{}
}

type operateKey struct{ person, action, device string }

type operateAnswer struct {
	allowed bool
	until   time.Time
}

// may answers whether person may take action on device, from the host, kept for operateTTL.
// A failed question is an error and is not kept.
func (s *service) may(ctx context.Context, person, action, device string) (bool, error) {
	key, now := operateKey{person, action, device}, time.Now()

	s.mu.Lock()
	if a, ok := s.operates[key]; ok && now.Before(a.until) {
		s.mu.Unlock()

		return a.allowed, nil
	}
	s.mu.Unlock()

	decision, err := s.access.Check(ctx, accessapi.CheckParams{
		Subject: person, Action: action, ObjectType: "device", ObjectID: device,
	})
	if err != nil {
		return false, fmt.Errorf("ask whether %s may %s %s: %w", person, action, device, err)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	for k, a := range s.operates {
		if !now.Before(a.until) {
			delete(s.operates, k)
		}
	}

	s.operates[key] = operateAnswer{allowed: decision.Allowed, until: now.Add(operateTTL)}

	return decision.Allowed, nil
}

// rights answers whether person may observe the printer and whether they may operate it.
// Operate is asked first: an operator holds observe too.
func (s *service) rights(ctx context.Context, person, device string) (observe, operate bool, err error) {
	operate, err = s.may(ctx, person, "operate", device)
	if err != nil || operate {
		return operate, operate, err
	}

	observe, err = s.may(ctx, person, "observe", device)

	return observe, false, err
}

// storeFailed answers a failure of the module's database, and logs its cause.
func (s *service) storeFailed(w http.ResponseWriter, r *http.Request, err error) {
	s.log.Error("store failed", "method", r.Method, "path", r.URL.Path, "error", err)
	refuse(w, StoreFailed, "", nil)
}

// spoolSyncFailed answers an error of a SpoolSync call with the code it stands for.
func (s *service) spoolSyncFailed(w http.ResponseWriter, what string, err error) {
	var status *spoolsync.StatusError

	_, onServer := s.ss.(mirror)

	switch {
	case errors.Is(err, spoolsync.ErrNotFound) && !onServer:
		refuse(w, NotFound, what+" is not in the inventory", nil)
	case errors.Is(err, spoolsync.ErrNotFound):
		refuse(w, NotFound, what+" is not in SpoolSync", nil)
	case !onServer && !errors.As(err, &status):
		// A standalone module's inventory is its own database.
		s.log.Error("the inventory failed", "what", what, "error", err)
		refuse(w, StoreFailed, "", nil)
	case errors.As(err, &status):
		s.log.Warn("spoolsync refused", "what", what, "status", status.Status, "error", err)
		refuse(w, SpoolSyncRefused, fmt.Sprintf("SpoolSync answered %d to %s", status.Status, what),
			map[string]any{"status": status.Status})
	default:
		s.log.Warn("spoolsync unreachable", "what", what, "error", err)
		refuse(w, SpoolSyncUnavailable, "", nil)
	}
}

// decodeBody reads one JSON body of at most maxBody bytes into v. A body it cannot read is refused.
func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	b, err := io.ReadAll(io.LimitReader(r.Body, maxBody+1))
	if err != nil || len(b) > maxBody {
		refuse(w, InvalidQuery, "the body is larger than 1 MiB or cannot be read", nil)

		return false
	}

	if err = json.Unmarshal(b, v); err != nil {
		refuse(w, InvalidQuery, "the body is not the JSON the route takes", nil)

		return false
	}

	return true
}

// pageParams reads offset and limit from the query. A limit above maxLimit is refused.
func pageParams(w http.ResponseWriter, r *http.Request) (offset, limit int, ok bool) {
	limit = defaultLimit

	if v := r.URL.Query().Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			refuse(w, InvalidQuery, "offset is a whole number from 0", nil)

			return 0, 0, false
		}

		offset = n
	}

	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 || n > maxLimit {
			refuse(w, InvalidQuery, "limit is a whole number from 0 to 100", nil)

			return 0, 0, false
		}

		limit = n
	}

	return offset, limit, true
}

// boolParam reads a yes-or-no query parameter. A value that is neither is refused.
func boolParam(w http.ResponseWriter, r *http.Request, name string) (value, ok bool) {
	v := r.URL.Query().Get(name)
	switch v {
	case "", "false":
		return false, true
	case "true":
		return true, true
	}

	refuse(w, InvalidQuery, name+" is true or false", nil)

	return false, false
}

// answer writes one JSON body.
func answer(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)

	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(body)
}

// validText checks a text a person writes: at most max bytes, with no control
// character but a tab or a line break.
func validText(text string, max int) bool {
	if len(text) > max {
		return false
	}

	for _, r := range text {
		if r < 0x20 && r != '\t' && r != '\n' {
			return false
		}
	}

	return true
}

// colourRGBA is a SpoolSync colour as the #rrggbbaa text Materials keeps; "" for none.
func colourRGBA(hex *string) string {
	if hex == nil {
		return ""
	}

	return *hex
}

// newService returns the service over SpoolSync, the database and the host's doors.
// The follower is set by the caller, which needs the service's callbacks first.
func newService(ss inventory, st *store.Store, log *slog.Logger, maxSpools int,
	access accessAsker, emit emitter,
) *service {
	return &service{
		ss: ss, st: st, log: log, maxSpools: maxSpools, access: access, emit: emit,
		operates: map[operateKey]operateAnswer{}, mirrorKick: make(chan struct{}, 1),
	}
}

// spoolView is a spool in Materials' shape, as the module answers it.
type spoolView struct {
	ID                string     `json:"id"`
	UUID              string     `json:"uuid"`
	Source            string     `json:"source"`
	MaterialUUID      string     `json:"material_uuid"`
	MaterialSource    string     `json:"material_source"`
	MaterialName      string     `json:"material_name"`
	BrandName         string     `json:"brand_name"`
	Class             string     `json:"class"`
	Type              string     `json:"type"`
	ColorRGBA         string     `json:"color_rgba"`
	Density           *float64   `json:"density,omitempty"`
	DiameterUM        int        `json:"diameter_um,omitempty"`
	FullWeightG       float64    `json:"full_weight_g"`
	EmptyWeightG      float64    `json:"empty_weight_g"`
	RemainingWeightG  float64    `json:"remaining_weight_g"`
	LocationID        string     `json:"location_id"`
	Location          string     `json:"location"`
	Notes             string     `json:"notes"`
	CreatedBy         string     `json:"created_by"`
	CreatedAt         time.Time  `json:"created_at"`
	UpdatedAt         time.Time  `json:"updated_at"`
	ArchivedAt        *time.Time `json:"archived_at"`
	ArchiveReason     string     `json:"archive_reason,omitempty"`
	ArchiveReasonNote string     `json:"archive_reason_note,omitempty"`
	Tool              *toolRef   `json:"tool"`
}

// toolRef names the tool a spool is on.
type toolRef struct {
	DeviceID string `json:"device_id"`
	ToolID   string `json:"tool_id"`
	Gone     bool   `json:"gone"`
}

// spoolOf is a SpoolSync spool in the module's shape. placed is the tool it is on, nil for none.
func spoolOf(sp spoolsync.Spool, placed *follower.Placement) spoolView {
	ft := sp.Filament

	v := spoolView{
		ID:               sp.ID,
		Source:           sp.Source,
		MaterialUUID:     sp.FilamentTypeID,
		MaterialSource:   "custom",
		MaterialName:     ft.Name,
		BrandName:        ft.Brand,
		Class:            "FFF",
		Type:             ft.Material,
		ColorRGBA:        colourRGBA(ft.Color),
		Density:          ft.DensityG,
		DiameterUM:       int(math.Round(ft.DiameterMM * 1000)),
		FullWeightG:      sp.InitialG,
		EmptyWeightG:     sp.EmptyG,
		RemainingWeightG: sp.RemainingG,
		LocationID:       "",
		Location:         "",
		Notes:            "",
		CreatedAt:        sp.CreatedAt,
		UpdatedAt:        sp.UpdatedAt,
		ArchivedAt:       sp.ArchivedAt,
	}

	if ft.OptMaterialUUID != nil {
		v.MaterialSource = "catalogue"
	}

	if sp.OptInstanceUUID != nil {
		v.UUID = *sp.OptInstanceUUID
	}

	if sp.LocationID != nil {
		v.LocationID = *sp.LocationID
	}

	if sp.Location != nil {
		v.Location = *sp.Location
	}

	if sp.Notes != nil {
		v.Notes = *sp.Notes
	}

	if sp.ArchiveReason != nil {
		v.ArchiveReason = *sp.ArchiveReason
	}

	if sp.ArchiveNote != nil {
		v.ArchiveReasonNote = *sp.ArchiveNote
	}

	if placed != nil {
		v.Tool = &toolRef{DeviceID: placed.PrinterID, ToolID: placed.Tool}
	}

	return v
}

// placements answers the tool each spool is on, from the follower.
func (s *service) placements() map[string]follower.Placement {
	return s.fo.Placements()
}

// spoolsAll reads every spool SpoolSync holds, as views.
func (s *service) spoolsAll(ctx context.Context) ([]spoolsync.Spool, error) {
	return s.ss.AllSpools(ctx)
}

// liveCount is how many spools are in stock: not archived.
func liveCount(spools []spoolsync.Spool) int {
	n := 0

	for _, sp := range spools {
		if sp.ArchivedAt == nil {
			n++
		}
	}

	return n
}

// assignmentView is a stored assignment as the module answers it.
type assignmentView struct {
	DeviceID       string    `json:"device_id"`
	ToolID         string    `json:"tool_id"`
	MaterialUUID   string    `json:"material_uuid"`
	MaterialSource string    `json:"material_source"`
	MaterialName   string    `json:"material_name"`
	BrandName      string    `json:"brand_name"`
	Type           string    `json:"type"`
	Abbreviation   string    `json:"abbreviation"`
	ColorRGBA      string    `json:"color_rgba"`
	SpoolID        string    `json:"spool_id,omitempty"`
	By             string    `json:"by"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// toolID reads a tool path such as 0 or 0.3, and false for text that is not one.
func toolID(s string) (connectorapi.ToolID, bool) {
	id := connectorapi.ToolID(s)

	return id, id.Valid()
}

// trimLower folds a name for comparing names.
func trimLower(s string) string { return strings.ToLower(strings.TrimSpace(s)) }

// jsonUnmarshal decodes JSON; a named helper so the call sites read as one step.
func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }
