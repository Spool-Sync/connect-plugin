package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"maps"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/accessapi"
	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/follower"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/store"
)

// fakeSS is SpoolSync, held in memory, for the routes this module calls.
type fakeSS struct {
	mu     sync.Mutex
	types  map[string]map[string]any
	spools map[string]map[string]any
	locs   map[string]string
	seq    int
}

func newFakeSS() *fakeSS {
	return &fakeSS{
		types: map[string]map[string]any{
			"ft1": {"filamentTypeId": "ft1", "name": "Prusament PLA", "brand": "Prusament", "material": "PLA",
				"colorHex": "#3d3e3dff", "color": "Galaxy Black", "diameter_mm": 1.75, "density_g_cm3": nil,
				"spoolWeight_g": 200.0, "optMaterialUuid": "opt-pla", "custom": false},
		},
		spools: map[string]map[string]any{}, locs: map[string]string{},
	}
}

// typePage answers GET /filament-types/page as SpoolSync does, for the filters the
// module sends. The caller holds f.mu.
func (f *fakeSS) typePage(q url.Values) map[string]any {
	split := func(v string) []string {
		if v == "" {
			return nil
		}

		return strings.Split(v, ",")
	}
	only, not := split(q.Get("ids")), split(q.Get("exclude"))
	search := strings.ToLower(q.Get("search"))

	rows := []map[string]any{}

	for id, t := range f.types {
		name, _ := t["name"].(string)
		brand, _ := t["brand"].(string)
		material, _ := t["material"].(string)

		switch {
		case only != nil && !slices.Contains(only, id):
		case slices.Contains(not, id):
		case search != "" && !strings.Contains(strings.ToLower(name+" "+brand+" "+material), search):
		case q.Get("brand") != "" && !strings.EqualFold(q.Get("brand"), brand):
		case q.Get("material") != "" && !strings.EqualFold(q.Get("material"), material):
		case q.Get("source") == "custom" && t["custom"] != true:
		case q.Get("source") == "openprinttag" && t["optMaterialUuid"] == nil:
		default:
			rows = append(rows, t)
		}
	}

	key := map[string]string{"brand": "brand", "material": "material"}[q.Get("orderBy")]
	if key == "" {
		key = "name"
	}

	sort.SliceStable(rows, func(i, j int) bool {
		a, _ := rows[i][key].(string)
		b, _ := rows[j][key].(string)

		if q.Get("desc") == "true" {
			return a > b
		}

		return a < b
	})

	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	start := min(offset, len(rows))
	end := min(start+max(limit, 1), len(rows))

	return map[string]any{"items": rows[start:end], "total": len(rows), "offset": offset, "limit": limit}
}

func (f *fakeSS) id(prefix string) string {
	f.seq++

	return fmt.Sprintf("%s%d", prefix, f.seq)
}

// spoolWire is a spool as SpoolSync answers it.
func (f *fakeSS) spoolWire(id string) map[string]any {
	sp := f.spools[id]
	ft := f.types[sp["filamentTypeId"].(string)]
	out := map[string]any{
		"spoolId": id, "filamentTypeId": ft["filamentTypeId"], "initialWeight_g": sp["initialWeight_g"],
		"currentWeight_g": sp["currentWeight_g"], "status": "ACTIVE", "optInstanceUuid": sp["optInstanceUuid"],
		"notes": sp["notes"], "archivedAt": sp["archivedAt"], "archiveReason": sp["archiveReason"],
		"archiveNote": sp["archiveNote"], "createdAt": sp["createdAt"], "updatedAt": "2026-11-21T10:00:00Z",
		"filamentType": ft, "storageLocations": []any{},
	}

	if loc, ok := sp["locationId"].(string); ok && loc != "" {
		out["storageLocations"] = []any{map[string]any{"storageLocation": map[string]any{"storageLocationId": loc, "name": f.locs[loc]}}}
	}

	return out
}

func (f *fakeSS) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if r.Header.Get("Authorization") != "Bearer ss_test" {
		w.WriteHeader(http.StatusUnauthorized)

		return
	}

	body := map[string]any{}
	if r.Body != nil {
		b, _ := io.ReadAll(r.Body)
		if len(b) > 0 {
			_ = json.Unmarshal(b, &body)
		}
	}

	p := strings.TrimPrefix(r.URL.Path, "/api/v1")
	reply := func(status int, v any) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(v)
	}

	switch {
	case r.Method == "GET" && (p == "/filament-types" || p == "/filament-types/search"):
		var out []map[string]any
		for _, t := range f.types {
			out = append(out, t)
		}

		reply(200, out)
	case r.Method == "GET" && p == "/filament-types/page":
		reply(200, f.typePage(r.URL.Query()))
	case r.Method == "GET" && p == "/filament-types/kinds":
		seen := map[string]bool{}
		out := []string{}

		for _, t := range f.types {
			if m, _ := t["material"].(string); m != "" && !seen[m] {
				seen[m] = true
				out = append(out, m)
			}
		}

		slices.Sort(out)
		reply(200, out)
	case r.Method == "GET" && strings.HasPrefix(p, "/filament-types/by-opt/"):
		want := strings.TrimPrefix(p, "/filament-types/by-opt/")
		for _, t := range f.types {
			if t["optMaterialUuid"] == want {
				reply(200, t)

				return
			}
		}

		reply(404, map[string]any{"message": "no"})
	case r.Method == "GET" && strings.HasPrefix(p, "/filament-types/") && !strings.Contains(p, "sync"):
		t, ok := f.types[strings.TrimPrefix(p, "/filament-types/")]
		if !ok {
			reply(404, map[string]any{"message": "no"})

			return
		}

		reply(200, t)
	case r.Method == "POST" && p == "/filament-types":
		id := f.id("ft")
		t := map[string]any{"filamentTypeId": id, "name": body["name"], "brand": body["brand"], "material": body["material"],
			"colorHex": body["colorHex"], "diameter_mm": body["diameter_mm"], "density_g_cm3": body["density_g_cm3"],
			"spoolWeight_g": body["spoolWeight_g"], "optMaterialUuid": nil, "custom": true}
		f.types[id] = t
		reply(201, t)
	case r.Method == "GET" && p == "/spools":
		var ids []string
		for id := range f.spools {
			ids = append(ids, id)
		}

		items := []any{}
		for _, id := range ids {
			items = append(items, f.spoolWire(id))
		}

		reply(200, map[string]any{"items": items, "total": len(items), "page": 1, "pageSize": 200})
	case r.Method == "POST" && p == "/spools":
		id := f.id("s")
		f.spools[id] = map[string]any{"filamentTypeId": body["filamentTypeId"], "initialWeight_g": body["initialWeight_g"],
			"currentWeight_g": body["initialWeight_g"], "optInstanceUuid": body["optInstanceUuid"], "notes": body["notes"],
			"archivedAt": nil, "archiveReason": nil, "archiveNote": nil, "createdAt": "2026-11-21T09:00:00Z"}
		if loc, ok := body["storageLocationId"].(string); ok {
			f.spools[id]["locationId"] = loc
		}

		reply(201, f.spoolWire(id))
	case r.Method == "GET" && strings.HasPrefix(p, "/spools/by-opt/"):
		uuid := strings.TrimPrefix(p, "/spools/by-opt/")
		for id, sp := range f.spools {
			if sp["optInstanceUuid"] == uuid {
				reply(200, f.spoolWire(id))

				return
			}
		}

		reply(404, map[string]any{"message": "no"})
	case strings.HasPrefix(p, "/spools/") && strings.HasSuffix(p, "/update-weight"):
		id := strings.TrimSuffix(strings.TrimPrefix(p, "/spools/"), "/update-weight")
		f.spools[id]["currentWeight_g"] = body["weight_g"]
		reply(200, f.spoolWire(id))
	case strings.HasPrefix(p, "/spools/"):
		id := strings.TrimPrefix(p, "/spools/")
		sp, ok := f.spools[id]
		if !ok {
			reply(404, map[string]any{"message": "no"})

			return
		}

		switch r.Method {
		case "GET":
			reply(200, f.spoolWire(id))
		case "DELETE":
			delete(f.spools, id)
			w.WriteHeader(204)
		case "PUT":
			if v, ok := body["currentWeight_g"]; ok {
				sp["currentWeight_g"] = v
			}

			if v, ok := body["notes"]; ok {
				sp["notes"] = v
			}

			if v, ok := body["archivedAt"]; ok {
				sp["archivedAt"] = v
			}

			if v, ok := body["archiveReason"]; ok {
				sp["archiveReason"] = v
			}

			if v, ok := body["archiveNote"]; ok {
				sp["archiveNote"] = v
			}

			if v, ok := body["storageLocationId"]; ok {
				if v == nil {
					delete(sp, "locationId")
				} else {
					sp["locationId"] = v
				}
			}

			reply(200, f.spoolWire(id))
		}
	case r.Method == "GET" && p == "/storage-locations":
		out := []any{}
		for id, name := range f.locs {
			out = append(out, map[string]any{"storageLocationId": id, "name": name})
		}

		reply(200, out)
	case r.Method == "POST" && p == "/storage-locations":
		id := f.id("l")
		f.locs[id] = body["name"].(string)
		reply(201, map[string]any{"storageLocationId": id, "name": body["name"]})
	case r.Method == "DELETE" && strings.HasPrefix(p, "/storage-locations/"):
		delete(f.locs, strings.TrimPrefix(p, "/storage-locations/"))
		w.WriteHeader(204)
	default:
		reply(400, map[string]any{"message": "no route " + r.Method + " " + p})
	}
}

// fleet is the host's device door, held in memory.
type fleet struct {
	devices map[string]deviceapi.Device
}

func (f *fleet) List(_ context.Context, _ deviceapi.ListParams) (deviceapi.Page, error) {
	var items []deviceapi.Device
	for _, d := range f.devices {
		items = append(items, d)
	}

	return deviceapi.Page{Items: items, Total: len(items), Limit: deviceapi.MaxPageLimit}, nil
}

func (f *fleet) Get(_ context.Context, p deviceapi.GetParams) (deviceapi.Device, error) {
	d, ok := f.devices[p.ID]
	if !ok {
		return deviceapi.Device{}, deviceapi.ErrNoDevice
	}

	return d, nil
}

// allow is the host's answer for each person, action and printer. Missing means no.
type allow map[string]bool

func (a allow) Check(_ context.Context, p accessapi.CheckParams) (accessapi.Decision, error) {
	return accessapi.Decision{Allowed: a[p.Subject+" "+p.Action+" "+p.ObjectID]}, nil
}

// recordEmitter keeps the events the module emits.
type recordEmitter struct {
	mu     sync.Mutex
	events []string
}

func (e *recordEmitter) Emit(_ context.Context, endpoint, key string, _ any) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.events = append(e.events, endpoint+" "+key)

	return nil
}

type rig struct {
	ss     *fakeSS
	svc    *service
	fleet  *fleet
	access allow
	events *recordEmitter
	ctx    context.Context
}

// printerWith is a printer with the tool state given, head 0 enabled.
func printerWith(state string, tools map[string]map[string]any) deviceapi.Device {
	st := map[string]json.RawMessage{"/state": json.RawMessage(fmt.Sprintf("%q", state))}

	for id, members := range tools {
		st[connectorapi.ToolPath(connectorapi.ToolID(id), connectorapi.ToolEnabled)] = json.RawMessage(`true`)

		for member, v := range members {
			b, _ := json.Marshal(v)
			st[connectorapi.ToolPath(connectorapi.ToolID(id), member)] = b
		}
	}

	return deviceapi.Device{Kind: "printer", ID: "p1", Name: "Prusa XL", Model: "XL", Connected: true, State: st}
}

func newRig(t *testing.T, printer deviceapi.Device) *rig {
	t.Helper()

	db, _, err := store.Open(t.Context(), wirerpc.DatabaseGrant{
		Engine: wirerpc.DatabaseSQLite, DSN: filepath.Join(t.TempDir(), "module.db"),
	}, os.DirFS("."))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	ss := newFakeSS()
	srv := httptest.NewServer(ss)
	t.Cleanup(srv.Close)

	client, err := spoolsync.New(srv.URL, "ss_test", srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	r := &rig{
		ss: ss, fleet: &fleet{devices: map[string]deviceapi.Device{printer.ID: printer}},
		access: allow{}, events: &recordEmitter{}, ctx: t.Context(),
	}

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	st := store.New(db, wirerpc.DatabaseSQLite)
	r.svc = newService(client, st, silent, 2000, r.access, r.events)
	r.svc.fo = follower.New(r.fleet, func(context.Context, busapi.SubscribeParams, func(busapi.Window) error) error { return nil },
		client, st, silent)
	r.svc.fo.Notify(r.svc.emitTool, r.svc.emitSpoolWritten)

	if err = r.svc.fo.Full(t.Context()); err != nil {
		t.Fatal(err)
	}

	return r
}

// call makes one request as the person, through the routes, and answers the recorder.
func (r *rig) call(t *testing.T, method, path, body, by string) *httptest.ResponseRecorder {
	t.Helper()

	req, err := routesapi.Request{Method: method, Path: path, Body: []byte(body), Principal: by}.HTTP(r.ctx)
	if err != nil {
		t.Fatal(err)
	}

	rec := httptest.NewRecorder()
	r.svc.routes().ServeHTTP(rec, req)

	return rec
}

// code reads the code of a refusal.
func code(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	var p struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &p); err != nil {
		t.Fatalf("the answer is not JSON: %s", rec.Body.String())
	}

	return p.Code
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()

	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("the answer is not JSON (%d): %s", rec.Code, rec.Body.String())
	}

	return out
}

// Adding spools writes each one at its remaining weight plus the empty spool, and is
// refused past max_spools.
func TestAddingSpoolsWritesTheGrossWeightAndRefusesPastTheShelf(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))

	rec := r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":800,"quantity":2}`, "user:alpha")
	if rec.Code != http.StatusCreated {
		t.Fatalf("add answered %d: %s", rec.Code, rec.Body.String())
	}

	if got := decode(t, rec); got["added"] != 2.0 {
		t.Fatalf("added %v", got)
	}

	for id, sp := range r.ss.spools {
		if sp["currentWeight_g"] != 1000.0 {
			t.Fatalf("spool %s gross %v, want 1000 (800 left + 200 empty)", id, sp["currentWeight_g"])
		}
	}

	r.svc.maxSpools = 2

	if rec = r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":800,"quantity":1}`, "user:alpha"); code(t, rec) != "spool-sync/SHELF_FULL" {
		t.Fatalf("a spool past max_spools answered %s", rec.Body.String())
	}
}

// An archived spool leaves the stock totals and cannot be archived while on a tool.
func TestArchivingAndUnarchiving(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {connectorapi.MaterialAbbreviation: "PLA"}}))

	r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":500,"quantity":1}`, "user:alpha")

	var id string
	for k := range r.ss.spools {
		id = k
	}

	rec := r.call(t, "PATCH", "/spools/"+id, `{"archived":true,"archive_reason":"consumed","archive_reason_note":"empty"}`, "user:alpha")
	if rec.Code != http.StatusOK || decode(t, rec)["archived_at"] == nil {
		t.Fatalf("archive answered %d %s", rec.Code, rec.Body.String())
	}

	if got := decode(t, rec)["archive_reason"]; got != "consumed" {
		t.Fatalf("reason %v", got)
	}

	rec = r.call(t, "PATCH", "/spools/"+id, `{"archived":false}`, "user:alpha")
	if rec.Code != http.StatusOK || decode(t, rec)["archived_at"] != nil || decode(t, rec)["archive_reason"] != nil {
		t.Fatalf("unarchive answered %d %s", rec.Code, rec.Body.String())
	}
}

// A person who may not operate the printer is refused NOT_OPERATOR; one the host
// does not let observe it is NOT_FOUND.
func TestAssignNeedsOperate(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))

	r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":500,"quantity":1}`, "user:alpha")

	var id string
	for k := range r.ss.spools {
		id = k
	}

	body := fmt.Sprintf(`{"spool_id":%q}`, id)

	if rec := r.call(t, "PUT", "/printers/p1/tools/0", body, "user:nobody"); code(t, rec) != "spool-sync/NOT_FOUND" {
		t.Fatalf("an unobservable printer answered %s", rec.Body.String())
	}

	// The watcher may observe the printer and not operate it.
	r.access = allow{"user:watcher observe p1": true}
	r.svc.access = r.access

	if rec := r.call(t, "PUT", "/printers/p1/tools/0", body, "user:watcher"); code(t, rec) != "spool-sync/NOT_OPERATOR" {
		t.Fatalf("an observer who may not operate answered %s", rec.Body.String())
	}
}

// A person who operates the printer assigns a spool; the spool is then on the tool,
// a second tool refuses it, and unassigning takes it off.
func TestAssignAndUnassignAndTheSpoolOnAnotherTool(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}, "1": {}}))
	r.access = allow{"user:alpha operate p1": true, "user:alpha observe p1": true}
	r.svc.access = r.access

	r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":500,"quantity":1}`, "user:alpha")

	var id string
	for k := range r.ss.spools {
		id = k
	}

	rec := r.call(t, "PUT", "/printers/p1/tools/0", fmt.Sprintf(`{"spool_id":%q}`, id), "user:alpha")
	if rec.Code != http.StatusOK {
		t.Fatalf("assign answered %d: %s", rec.Code, rec.Body.String())
	}

	if got := decode(t, rec)["spool_id"]; got != id {
		t.Fatalf("the assignment names %v, want %s", got, id)
	}

	if rec = r.call(t, "PUT", "/printers/p1/tools/1", fmt.Sprintf(`{"spool_id":%q}`, id), "user:alpha"); code(t, rec) != "spool-sync/SPOOL_ON_ANOTHER_TOOL" {
		t.Fatalf("a second tool answered %s", rec.Body.String())
	}

	if rec = r.call(t, "DELETE", "/printers/p1/tools/0", "", "user:alpha"); rec.Code != http.StatusNoContent {
		t.Fatalf("unassign answered %d", rec.Code)
	}

	if rec = r.call(t, "PUT", "/printers/p1/tools/1", fmt.Sprintf(`{"spool_id":%q}`, id), "user:alpha"); rec.Code != http.StatusOK {
		t.Fatalf("after unassign, assign answered %d: %s", rec.Code, rec.Body.String())
	}
}

// A printing printer refuses a change, and an empty tool refuses a material when the
// printer reports it empty.
func TestTheRulesRefuseAChangeWhilePrinting(t *testing.T) {
	r := newRig(t, printerWith("PRINTING", map[string]map[string]any{"0": {}}))
	r.access = allow{"user:alpha operate p1": true, "user:alpha observe p1": true}
	r.svc.access = r.access

	rec := r.call(t, "PUT", "/printers/p1/tools/0", `{"material_uuid":"ft1"}`, "user:alpha")
	if code(t, rec) != "spool-sync/TOOL_REFUSED" || decode(t, rec)["params"].(map[string]any)["reason"] != "printing" {
		t.Fatalf("a change while printing answered %s", rec.Body.String())
	}
}

// Locations are kept at most 100, a name is taken once in any case, and a name that
// exists is answered rather than made again.
func TestLocationsRules(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))

	rec := r.call(t, "POST", "/locations", `{"name":"Shelf A"}`, "user:alpha")
	if rec.Code != http.StatusOK {
		t.Fatalf("create answered %d: %s", rec.Code, rec.Body.String())
	}

	first := decode(t, rec)["id"]

	if again := decode(t, r.call(t, "POST", "/locations", `{"name":"shelf a"}`, "user:alpha")); again["id"] != first {
		t.Fatalf("the same name in another case made a second location: %v", again)
	}

	r.call(t, "POST", "/locations", `{"name":"Dryer"}`, "user:alpha")

	if rec = r.call(t, "PATCH", fmt.Sprintf("/locations/%v", first), `{"name":"DRYER"}`, "user:alpha"); code(t, rec) != "spool-sync/LOCATION_TAKEN" {
		t.Fatalf("a rename onto a taken name answered %s", rec.Body.String())
	}

	if rec = r.call(t, "POST", "/locations", `{"name":"   "}`, "user:alpha"); code(t, rec) != "spool-sync/LOCATION_INVALID" {
		t.Fatalf("an empty name answered %s", rec.Body.String())
	}
}

// Favourites are the person's own; list filters keep a JSON object of at most 2048 bytes.
func TestFavouritesAndListFilters(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))

	if rec := r.call(t, "PUT", "/favourites/ft1", "", "user:alpha"); rec.Code != http.StatusNoContent {
		t.Fatalf("star answered %d", rec.Code)
	}

	rows := decode(t, r.call(t, "GET", "/materials", "", "user:alpha"))["rows"].([]any)
	if len(rows) == 0 || rows[0].(map[string]any)["is_favourite"] != true {
		t.Fatalf("a starred material is not first: %v", rows)
	}

	if rows = decode(t, r.call(t, "GET", "/materials", "", "user:beta"))["rows"].([]any); rows[0].(map[string]any)["is_favourite"] == true {
		t.Fatal("another person sees the favourite")
	}

	if rec := r.call(t, "PUT", "/list-filters", `{"q":"prusa"}`, "user:alpha"); rec.Code != http.StatusNoContent {
		t.Fatalf("keep filters answered %d", rec.Code)
	}

	if got := decode(t, r.call(t, "GET", "/list-filters", "", "user:alpha"))["filters"]; got.(map[string]any)["q"] != "prusa" {
		t.Fatalf("kept filters %v", got)
	}

	big := `{"q":"` + strings.Repeat("a", maxFilters) + `"}`
	if rec := r.call(t, "PUT", "/list-filters", big, "user:alpha"); code(t, rec) != "spool-sync/INVALID_QUERY" {
		t.Fatalf("an oversized filter answered %s", rec.Body.String())
	}
}

// Merging takes a manual spool into a printer spool of the same material, and refuses any other.
func TestMergeNeedsAManualIntoAPrinterSpool(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))

	r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":500,"quantity":2}`, "user:alpha")

	var ids []string
	for k := range maps.Keys(r.ss.spools) {
		ids = append(ids, k)
	}

	// Neither spool carries a tag, so both are manual and the merge is refused.
	rec := r.call(t, "POST", "/spools/"+ids[0]+"/merge", fmt.Sprintf(`{"into":%q}`, ids[1]), "user:alpha")
	if code(t, rec) != "spool-sync/MERGE_REFUSED" {
		t.Fatalf("a manual into a manual answered %s", rec.Body.String())
	}
}

// A tool that reads no tag refuses an NFC add, and a tag already on a spool adds nothing.
func TestAddReportedNeedsATag(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))
	r.access = allow{"user:alpha operate p1": true, "user:alpha observe p1": true}
	r.svc.access = r.access

	if rec := r.call(t, "POST", "/printers/p1/tools/0/spool", "", "user:alpha"); code(t, rec) != "spool-sync/NOT_REPORTED" {
		t.Fatalf("an untagged tool answered %s", rec.Body.String())
	}
}

// The printers a person may observe are listed, with whether they may operate each.
func TestListPrintersAnswersWhatThePersonMayObserve(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))
	r.access = allow{"user:alpha observe p1": true, "user:alpha operate p1": true}
	r.svc.access = r.access

	got := decode(t, r.call(t, "GET", "/printers", "", "user:alpha"))["printers"].([]any)
	if len(got) != 1 || got[0].(map[string]any)["may_operate"] != true {
		t.Fatalf("printers %v", got)
	}

	if got = decode(t, r.call(t, "GET", "/printers", "", "user:nobody"))["printers"].([]any); len(got) != 0 {
		t.Fatalf("a person who may observe nothing sees %v", got)
	}
}

// A request the host named nobody for is refused.
func TestARequestWithNoPersonIsRefused(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))

	if rec := r.call(t, "GET", "/spools", "", ""); code(t, rec) != "spool-sync/NO_PRINCIPAL" {
		t.Fatalf("no person answered %s", rec.Body.String())
	}
}

// The tools of one printer say whether the caller may operate it, and a list filter
// that is larger than the bound is refused even when it arrives in pieces.
func TestPrinterToolsSayWhetherTheCallerMayOperate(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))
	r.access = allow{"user:watcher observe p1": true}
	r.svc.access = r.access

	got := decode(t, r.call(t, "GET", "/printers/p1/tools", "", "user:watcher"))
	if got["may_operate"] != false {
		t.Fatalf("a watcher may operate: %v", got["may_operate"])
	}
}

// A tag that holds no full weight asks for the spool's weight, and takes it.
func TestAddReportedAsksForTheWeightATagLacks(t *testing.T) {
	const uuid = "bf63e92d-9ca5-53d7-9fab-ffdd0240c587"

	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {
		connectorapi.MaterialInstanceUUID: uuid,
		connectorapi.MaterialType:         "PLA",
		connectorapi.ConsumedWeight:       12.0,
	}}))
	r.access = allow{"user:alpha operate p1": true, "user:alpha observe p1": true}
	r.svc.access = r.access

	if rec := r.call(t, "POST", "/printers/p1/tools/0/spool", "", "user:alpha"); code(t, rec) != "spool-sync/WEIGHT_NEEDED" {
		t.Fatalf("a tag with no weight answered %s", rec.Body.String())
	}

	if rec := r.call(t, "POST", "/printers/p1/tools/0/spool", `{"remaining_weight_g":-1}`, "user:alpha"); code(t, rec) != "spool-sync/SPOOL_INVALID" {
		t.Fatalf("a negative weight answered %s", rec.Body.String())
	}

	rec := r.call(t, "POST", "/printers/p1/tools/0/spool", `{"remaining_weight_g":640}`, "user:alpha")
	if rec.Code != http.StatusCreated {
		t.Fatalf("with a weight it answered %d %s", rec.Code, rec.Body.String())
	}

	got := decode(t, rec)
	if got["remaining_weight_g"] != 640.0 || got["full_weight_g"] != 640.0 || got["uuid"] != uuid {
		t.Fatalf("the spool is %v, want 640 g left of 640 g, with the tag's uuid", got)
	}
}
