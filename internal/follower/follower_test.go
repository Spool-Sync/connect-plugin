package follower

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/spool-sync/connect-plugin/internal/spoolsync"
	"github.com/spool-sync/connect-plugin/internal/store"
)

// metre is what 1000 mm of 1.75 mm PLA weighs at 1.24 g/cm³.
const metre = 2.98255

// fleet is the host's device door, held in memory.
type fleet struct {
	mu      sync.Mutex
	devices map[string]deviceapi.Device
	lists   int
}

func (f *fleet) set(d deviceapi.Device) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.devices[d.ID] = d
}

func (f *fleet) List(_ context.Context, p deviceapi.ListParams) (deviceapi.Page, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.lists++

	page := deviceapi.Page{Limit: p.Limit, Offset: p.Offset}
	for _, d := range f.devices {
		if d.Retired {
			continue
		}

		d.State = maps.Clone(d.State)
		page.Items = append(page.Items, d)
	}

	page.Total = len(page.Items)

	return page, nil
}

func (f *fleet) Get(_ context.Context, p deviceapi.GetParams) (deviceapi.Device, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.devices[p.ID]
	if !ok {
		return deviceapi.Device{}, deviceapi.ErrNoDevice
	}

	d.State = maps.Clone(d.State)

	return d, nil
}

// fakeSS is SpoolSync, held in memory, with the writes it was sent.
type fakeSS struct {
	mu       sync.Mutex
	spools   map[string]spoolsync.Spool
	byUUID   map[string]string
	writes   []float64
	sources  []string
	writeErr error
}

func (f *fakeSS) Get(_ context.Context, id string) (spoolsync.Spool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	sp, ok := f.spools[id]
	if !ok {
		return spoolsync.Spool{}, spoolsync.ErrNotFound
	}

	return sp, nil
}

func (f *fakeSS) FindByInstanceUUID(_ context.Context, uuid string) (spoolsync.Spool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	id, ok := f.byUUID[uuid]
	if !ok {
		return spoolsync.Spool{}, spoolsync.ErrNotFound
	}

	return f.spools[id], nil
}

func (f *fakeSS) SetWeight(_ context.Context, id string, gross float64, source string) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	if f.writeErr != nil {
		return f.writeErr
	}

	sp := f.spools[id]
	sp.GrossG = gross
	f.spools[id] = sp
	f.writes = append(f.writes, gross)
	f.sources = append(f.sources, source)

	return nil
}

func (f *fakeSS) written() []float64 {
	f.mu.Lock()
	defer f.mu.Unlock()

	return append([]float64(nil), f.writes...)
}

// plaSpool is a PLA spool of 200 g empty, at gross weight gross.
func plaSpool(id string, gross float64, uuid string) spoolsync.Spool {
	sp := spoolsync.Spool{ID: id, Material: "PLA", GrossG: gross, EmptyG: 200}
	sp.Filament = spoolsync.FilamentType{ID: "ft", Material: "PLA", DiameterMM: 1.75}

	if uuid != "" {
		sp.OptInstanceUUID = &uuid
	}

	return sp
}

type world struct {
	fleet *fleet
	ss    *fakeSS
	store *store.Store
	f     *Follower
	ctx   context.Context
}

// newWorld starts a follower over the printers, with an empty database and
// the spools given. Nothing is read until the test asks.
func newWorld(t *testing.T, spools []spoolsync.Spool, printers ...deviceapi.Device) *world {
	t.Helper()

	db, _, err := store.Open(t.Context(), wirerpc.DatabaseGrant{
		Engine: wirerpc.DatabaseSQLite, DSN: filepath.Join(t.TempDir(), "module.db"),
	}, os.DirFS("../.."))
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() { _ = db.Close() })

	w := &world{
		fleet: &fleet{devices: map[string]deviceapi.Device{}},
		ss:    &fakeSS{spools: map[string]spoolsync.Spool{}, byUUID: map[string]string{}},
		store: store.New(db, wirerpc.DatabaseSQLite),
		ctx:   t.Context(),
	}

	for _, sp := range spools {
		w.ss.spools[sp.ID] = sp

		if sp.OptInstanceUUID != nil {
			w.ss.byUUID[*sp.OptInstanceUUID] = sp.ID
		}
	}

	for _, d := range printers {
		w.fleet.set(d)
	}

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	subscribe := func(context.Context, busapi.SubscribeParams, func(busapi.Window) error) error { return nil }
	w.f = New(w.fleet, subscribe, w.ss, w.store, silent)

	if err = w.f.Full(w.ctx); err != nil {
		t.Fatal(err)
	}

	return w
}

// window applies one window of telemetry, as the stream would.
func (w *world) window(t *testing.T, telemetry ...json.RawMessage) {
	t.Helper()

	if err := w.f.window(w.ctx, busapi.Window{Telemetry: telemetry}); err != nil {
		t.Fatal(err)
	}
}

// sample is one telemetry envelope: the value at one path of printer p1's tree.
func sample(path string, value any) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"path": path, "value": value})
	env, _ := json.Marshal(event.Envelope{
		Type: event.TypeDeviceTelemetrySampled, Subject: "device:p1", Class: event.ClassTelemetry, Data: data,
	})

	return env
}

// printer is a printer with the given state paths.
func printer(id string, state map[string]any) deviceapi.Device {
	st := map[string]json.RawMessage{"/state": json.RawMessage(`"IDLE"`)}

	for path, v := range state {
		b, _ := json.Marshal(v)
		st[path] = b
	}

	return deviceapi.Device{Kind: "printer", ID: id, Name: id, Connected: true, State: st}
}

// head is the state of head 0 on p1: enabled, with PLA loaded, and nothing
// else until the test adds it.
func head(id connectorapi.ToolID, extra map[string]any) map[string]any {
	state := map[string]any{
		connectorapi.ToolPath(id, connectorapi.ToolEnabled):          true,
		connectorapi.ToolPath(id, connectorapi.MaterialAbbreviation): "PLA",
	}

	for member, v := range extra {
		state[connectorapi.ToolPath(id, member)] = v
	}

	return state
}

func merge(parts ...map[string]any) map[string]any {
	out := map[string]any{}
	for _, m := range parts {
		for k, v := range m {
			out[k] = v
		}
	}

	return out
}

// odometerState is the odometer members of a head, by member name.
func odometerState(id int64, e float64) map[string]any {
	return map[string]any{
		connectorapi.OdometerID: id,
		connectorapi.OdometerE:  e,
	}
}

func approx(got, want float64) bool { return math.Abs(got-want) < 1e-4 }

// A tagged tool writes its filament left plus the empty spool, once per whole
// gram: the same gram again is not sent.
func TestATaggedToolWritesItsFilamentLeftAsGrossOncePerGram(t *testing.T) {
	const uuid = "bf63e92d-9ca5-53d7-9fab-ffdd0240c585"

	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, uuid)}, printer("p1", merge(
		head("0", map[string]any{
			connectorapi.MaterialInstanceUUID: uuid,
			connectorapi.MaterialFullWeight:   1000.0,
			connectorapi.ConsumedWeight:       187.3,
		}),
	)))

	if got := w.ss.written(); len(got) != 1 || got[0] != 1012 {
		t.Fatalf("on read, writes %v, want [1012] (812.7 left + 200 empty, whole grams)", got)
	}

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 188.0))
	if got := w.ss.written(); len(got) != 1 {
		t.Fatalf("the same whole gram was written again: %v", got)
	}

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 189.5))
	if got := w.ss.written(); len(got) != 2 || got[1] != 1010 {
		t.Fatalf("writes %v, want [1012 1010]", got)
	}

	if sources := w.ss.sources; sources[0] != spoolsync.Source {
		t.Fatalf("written from %q, want %q", sources[0], spoolsync.Source)
	}

	if st := w.f.Stats(); st.Written != 2 || st.Failed != 0 {
		t.Fatalf("stats %+v", st)
	}

	entries, _ := w.f.Tools("p1")
	if len(entries) != 1 || entries[0].Binding != (Binding{Source: "tag", SpoolID: "s1"}) {
		t.Fatalf("the tool is %+v, want the tag's spool", entries)
	}
}

// A tag SpoolSync holds no spool for is shown and not written.
func TestAnUnknownTagIsShownNotWritten(t *testing.T) {
	w := newWorld(t, nil, printer("p1", head("0", map[string]any{
		connectorapi.MaterialInstanceUUID: "00000000-0000-0000-0000-000000000000",
		connectorapi.MaterialFullWeight:   1000.0,
		connectorapi.ConsumedWeight:       10.0,
	})))

	if got := w.ss.written(); len(got) != 0 {
		t.Fatalf("an unknown tag wrote %v", got)
	}

	entries, _ := w.f.Tools("p1")
	if len(entries) != 1 || !entries[0].Binding.UnknownTag || entries[0].Binding.SpoolID != "" {
		t.Fatalf("binding %+v, want an unknown tag", entries)
	}

	if st := w.f.Stats(); st.Unknown != 1 {
		t.Fatalf("stats %+v, want one unknown tag", st)
	}
}

// An assigned tool counts its odometer down from the spool's gross weight.
func TestAnAssignedToolCountsItsOdometer(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", merge(
		head("0", odometerState(7, 0)),
	)))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	// The baseline is the reading at the assignment; the first window counts from it.
	w.f.Refresh(w.ctx, "p1")
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 1000.0))

	if got := w.ss.written(); len(got) != 1 || got[0] != 1098 {
		t.Fatalf("writes %v, want [1098] (1 m of PLA is 2.98 g: 2 whole grams come off 1100)", got)
	}

	pending, _ := w.store.Pending(w.ctx, "s1")
	if !approx(pending, metre-2) {
		t.Fatalf("pending %v, want the fraction %v", pending, metre-2)
	}

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 1500.0))

	// 1.5 m is 4.47 g in all: 4 whole grams are written, and the rest waits.
	pending, _ = w.store.Pending(w.ctx, "s1")
	if got := w.ss.written(); len(got) != 2 || got[1] != 1096 || !approx(pending, 1.5*metre-4) {
		t.Fatalf("after 1.5 m: writes %v, pending %v", got, pending)
	}
}

// A printer that reports the weight of its odometer counts that weight, not the length.
func TestTheOdometerWeightIsPreferredWhenReported(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", merge(
		head("0", merge(odometerState(7, 0), map[string]any{
			connectorapi.OdometerWeight: 0.0,
		})),
	)))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")
	w.window(t,
		sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 1000.0),
		sample(connectorapi.ToolPath("0", connectorapi.OdometerWeight), 5.0),
	)

	if got := w.ss.written(); len(got) != 1 || got[0] != 1095 {
		t.Fatalf("writes %v, want [1095] (5 g reported, not the 2.98 g of the length)", got)
	}
}

// A new odometer id is a reset: nothing is charged, and the new reading is kept.
func TestAResetOdometerChargesNothing(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", head("0", odometerState(7, 5000))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")
	w.window(t,
		sample(connectorapi.ToolPath("0", connectorapi.OdometerID), 8),
		sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 40.0),
	)

	if got := w.ss.written(); len(got) != 0 {
		t.Fatalf("a reset wrote %v", got)
	}

	o, _, _ := w.store.Odometer(w.ctx, "p1", "0")
	if o.E != 40 || o.ID == nil || *o.ID != 8 {
		t.Fatalf("the kept reading is %+v, want id 8 at 40 mm", o)
	}
}

// A lower reading under the same id is stale: it is ignored, and the reading kept stands.
func TestAStaleReadingIsIgnored(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", head("0", odometerState(7, 1100))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 40.0))

	if got := w.ss.written(); len(got) != 0 {
		t.Fatalf("a stale reading wrote %v", got)
	}

	o, _, _ := w.store.Odometer(w.ctx, "p1", "0")
	if o.E != 1100 {
		t.Fatalf("the kept reading moved to %v, want 1100", o.E)
	}
}

// A tag on a tool clears the assignment on it, and the tag's spool is what the tool holds.
func TestATagAppearingClearsTheAssignment(t *testing.T) {
	const uuid = "bf63e92d-9ca5-53d7-9fab-ffdd0240c585"

	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, ""), plaSpool("s2", 900, uuid)}, printer("p1", head("0", odometerState(7, 0))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.MaterialInstanceUUID), uuid))

	if _, ok, _ := w.store.Assignment(w.ctx, "p1", "0"); ok {
		t.Fatal("the assignment outlived the tag")
	}

	entries, _ := w.f.Tools("p1")
	if len(entries) != 1 || entries[0].Binding.SpoolID != "s2" || entries[0].Binding.Source != "tag" {
		t.Fatalf("binding %+v, want the tagged spool", entries)
	}
}

// A printer that is not printing has its job end: the whole grams counted are
// written then, and a fraction waits for the next print.
func TestAJobEndWritesTheWholeGrams(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", merge(
		head("0", odometerState(7, 0)),
		map[string]any{"/state": "PRINTING"},
	)))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	if err := w.store.AddPending(w.ctx, "s1", 1.6); err != nil {
		t.Fatal(err)
	}

	// Printing: nothing is written, not even the whole gram.
	w.f.Refresh(w.ctx, "p1")
	if got := w.ss.written(); len(got) != 0 {
		t.Fatalf("writes while printing %v", got)
	}

	w.window(t, sample("/state", "IDLE"))

	if got := w.ss.written(); len(got) != 1 || got[0] != 1099 {
		t.Fatalf("writes %v, want the job end to write 1 g (1099)", got)
	}

	pending, _ := w.store.Pending(w.ctx, "s1")
	if !approx(pending, 0.6) {
		t.Fatalf("pending %v, want the fraction 0.6", pending)
	}
}

// A SpoolSync that refuses a write is counted, and the stream goes on: the next change tries again.
func TestASpoolSyncRefusalIsCountedAndTheStreamGoesOn(t *testing.T) {
	const uuid = "bf63e92d-9ca5-53d7-9fab-ffdd0240c585"

	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, uuid)}, printer("p1", head("0", map[string]any{
		connectorapi.MaterialInstanceUUID: uuid,
		connectorapi.MaterialFullWeight:   1000.0,
		connectorapi.ConsumedWeight:       0.0,
	})))

	w.ss.mu.Lock()
	w.ss.writeErr = errors.New("spoolsync: PUT answered 503")
	w.ss.mu.Unlock()

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 50.0))

	if st := w.f.Stats(); st.Failed == 0 || st.LastError == "" {
		t.Fatalf("stats %+v, want the failure counted", st)
	}

	w.ss.mu.Lock()
	w.ss.writeErr = nil
	w.ss.mu.Unlock()

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 60.0))

	// 1200 was written when the tool was read, before the refusals began.
	if got := w.ss.written(); len(got) != 2 || got[0] != 1200 || got[1] != 1140 {
		t.Fatalf("after SpoolSync came back, writes %v, want [1200 1140] (940 left + 200)", got)
	}
}

// Whole grams left pending by a failed write are written by the next full read.
func TestAFullReadWritesTheGramsLeftPending(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", head("0", odometerState(7, 0))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	if err := w.store.AddPending(w.ctx, "s1", 3.4); err != nil {
		t.Fatal(err)
	}

	if err := w.f.Full(w.ctx); err != nil {
		t.Fatal(err)
	}

	if got := w.ss.written(); len(got) != 1 || got[0] != 1097 {
		t.Fatalf("writes %v, want [1097]", got)
	}

	if pending, _ := w.store.Pending(w.ctx, "s1"); !approx(pending, 0.4) {
		t.Fatalf("pending %v, want 0.4", pending)
	}
}

// An assigned spool SpoolSync no longer holds stays on the tool, and counts nothing.
func TestAnAssignedSpoolGoneFromSpoolSyncCountsNothing(t *testing.T) {
	w := newWorld(t, nil, printer("p1", head("0", odometerState(7, 0))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "gone", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 1000.0))

	if got := w.ss.written(); len(got) != 0 {
		t.Fatalf("a spool SpoolSync does not hold was written: %v", got)
	}

	entries, _ := w.f.Tools("p1")
	if len(entries) != 1 || entries[0].Binding != (Binding{Source: "assigned", SpoolID: "gone"}) {
		t.Fatalf("binding %+v", entries)
	}

	if st := w.f.Stats(); st.Failed != 0 {
		t.Fatalf("a spool gone from SpoolSync is a failure: %+v", st)
	}
}

// A head with a feeder holds no filament of its own: its slots are listed.
func TestAHeadWithAFeederIsNotListed(t *testing.T) {
	state := merge(
		head("0", map[string]any{connectorapi.FeederSlotCount: 4}),
		head("0.0", map[string]any{}),
	)
	state[connectorapi.ToolPath("0.0", connectorapi.MaterialAbbreviation)] = "PLA"

	w := newWorld(t, nil, printer("p1", state))

	entries, ok := w.f.Tools("p1")
	if !ok {
		t.Fatal("the printer is not held")
	}

	for _, e := range entries {
		if e.Tool.ID == "0" {
			t.Fatal("a head with a feeder is listed")
		}
	}

	if len(entries) != 1 || entries[0].Tool.ID != "0.0" {
		t.Fatalf("entries %+v, want the slot 0.0", entries)
	}
}

// Placements answers the tool each spool is on, for the spool lists.
func TestPlacementsNameTheToolOfEachSpool(t *testing.T) {
	const uuid = "bf63e92d-9ca5-53d7-9fab-ffdd0240c585"

	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, uuid), plaSpool("s2", 900, "")}, printer("p1", merge(
		head("0", map[string]any{connectorapi.MaterialInstanceUUID: uuid, connectorapi.MaterialFullWeight: 1000.0, connectorapi.ConsumedWeight: 0.0}),
		head("1", nil),
	)))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "1", SpoolID: "s2", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")

	got := w.f.Placements()
	if got["s1"] != (Placement{PrinterID: "p1", Tool: "0"}) || got["s2"] != (Placement{PrinterID: "p1", Tool: "1"}) {
		t.Fatalf("placements %+v", got)
	}

	if printers, tools := w.f.Counts(); printers != 1 || tools != 2 {
		t.Fatalf("counts %d printers, %d tools with a spool", printers, tools)
	}
}

// A retired printer lets go of its spools when the next full read finds it.
func TestARetiredPrinterLetsGoOfItsSpools(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", head("0", odometerState(7, 0))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	d := printer("p1", head("0", odometerState(7, 0)))
	d.Retired = true
	w.fleet.set(d)

	if err := w.f.Full(w.ctx); err != nil {
		t.Fatal(err)
	}

	if _, ok, _ := w.store.Assignment(w.ctx, "p1", "0"); ok {
		t.Fatal("a retired printer kept its assignment")
	}
}

// The first window of a subscription reads the printers again, since a change
// between the listing and the subscription would be lost.
func TestTheFirstWindowListsThePrintersAgain(t *testing.T) {
	w := newWorld(t, nil, printer("p1", head("0", nil)))

	before := w.fleet.lists
	ctx, cancel := context.WithCancel(w.ctx)

	subscribe := func(_ context.Context, _ busapi.SubscribeParams, on func(busapi.Window) error) error {
		if err := on(busapi.Window{Cursor: "c1"}); err != nil {
			return err
		}

		cancel()

		return ctx.Err()
	}

	silent := slog.New(slog.NewTextHandler(io.Discard, nil))
	f := New(w.fleet, subscribe, w.ss, w.store, silent)

	if err := f.Run(ctx); err != nil {
		t.Fatalf("Run ended with %v, want nil on cancel", err)
	}

	if w.fleet.lists-before < 2 {
		t.Fatalf("the printers were listed %d times, want the listing before the stream and the first window", w.fleet.lists-before)
	}
}

// A material a person picked with no spool sits on the tool, and counts nothing.
func TestAMaterialAloneSitsOnTheToolAndCountsNothing(t *testing.T) {
	w := newWorld(t, nil, printer("p1", head("0", odometerState(7, 0))))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", FilamentTypeID: "ft1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 1000.0))

	entries, _ := w.f.Tools("p1")
	if len(entries) != 1 || entries[0].Binding != (Binding{Source: "assigned", FilamentTypeID: "ft1"}) {
		t.Fatalf("binding %+v", entries)
	}

	if got := w.f.Placements(); len(got) != 0 {
		t.Fatalf("a material alone is a spool placement: %+v", got)
	}
}

// A tag that holds no full weight starts from the spool's weight and charges
// each rise of its consumed weight after the first reading.
func TestATagWithNoFullWeightChargesWhatItsCountRises(t *testing.T) {
	const uuid = "bf63e92d-9ca5-53d7-9fab-ffdd0240c586"

	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, uuid)}, printer("p1", head("0", map[string]any{
		connectorapi.MaterialInstanceUUID: uuid,
		connectorapi.ConsumedWeight:       50.0,
	})))

	if got := w.ss.written(); len(got) != 0 {
		t.Fatalf("the first reading wrote %v; it is the start", got)
	}

	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 53.4))
	if got := w.ss.written(); len(got) != 1 || got[0] != 1097 {
		t.Fatalf("writes %v, want [1097] (3 whole grams of 3.4)", got)
	}

	// A tag written again counts from its new reading and charges nothing.
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 0.0))
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.ConsumedWeight), 1.0))
	if got := w.ss.written(); len(got) != 2 || got[1] != 1096 {
		t.Fatalf("writes %v, want [1097 1096] (0.4 pending + 1)", got)
	}
}

// jobEnvelope is a job event of printer p1 for one print.
func jobEnvelope(kind, jobID string) json.RawMessage {
	data, _ := json.Marshal(map[string]any{"job_id": jobID, "device_job_id": "7"})
	env, _ := json.Marshal(event.Envelope{Type: kind, Subject: "device:p1", Data: data})

	return env
}

// A print is charged the grams its tools feed between its start and its end,
// the last readings of the window it ends in included. Filament fed with no
// print running is charged to none.
func TestAPrintIsChargedTheFilamentItsToolsFed(t *testing.T) {
	w := newWorld(t, []spoolsync.Spool{plaSpool("s1", 1100, "")}, printer("p1", merge(
		head("0", odometerState(7, 0)),
	)))

	if err := w.store.SetAssignment(w.ctx, store.Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedBy: "user:alpha"}); err != nil {
		t.Fatal(err)
	}

	w.f.Refresh(w.ctx, "p1")

	// Fed before the print: charged to the spool and to no print.
	w.window(t, sample(connectorapi.ToolPath("0", connectorapi.OdometerE), 1000.0))

	odometer := connectorapi.ToolPath("0", connectorapi.OdometerE)
	steps := []busapi.Window{
		{Events: []json.RawMessage{jobEnvelope(event.TypeDeviceJobStarted, "job-1")}},
		{Telemetry: []json.RawMessage{sample(odometer, 2000.0)}},
		// The last reading and the end of the print arrive in one window.
		{
			Telemetry: []json.RawMessage{sample(odometer, 3000.0)},
			Events:    []json.RawMessage{jobEnvelope(event.TypeDeviceJobSettled, "job-1")},
		},
		// After the print ends, feeding charges no print.
		{Telemetry: []json.RawMessage{sample(odometer, 4000.0)}},
	}

	for _, step := range steps {
		if err := w.f.window(w.ctx, step); err != nil {
			t.Fatal(err)
		}
	}

	uses, err := w.store.JobUsage(w.ctx, []string{"job-1"})
	if err != nil {
		t.Fatal(err)
	}

	if len(uses) != 1 || uses[0].SpoolID != "s1" || uses[0].Tool != "0" || !approx(uses[0].Grams, 2*metre) {
		t.Fatalf("the print took %+v, want 2 m of PLA (%v g) off s1 on tool 0", uses, 2*metre)
	}

	if _, running, _ := w.store.RunningJob(w.ctx, "p1"); running {
		t.Fatal("the printer still holds a print after it settled")
	}
}
