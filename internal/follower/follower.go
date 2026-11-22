// Package follower follows every printer the host has, and keeps the weight of
// the SpoolSync spool on each tool in step with what the printer reports.
//
// For each tool the follower works out which spool it holds:
//
//   - tagged: the printer reads an OpenPrintTag instance_uuid. The spool is the
//     SpoolSync spool with that uuid, and its filament left is the full weight
//     less the consumed weight the tag reports;
//   - assigned: no tag, and a person put a spool on the tool. The filament is
//     counted down from the odometer;
//   - none: neither. Nothing is written.
//
// A tag on a tool clears its assignment. A tag without a consumed weight is
// counted from the odometer too, against the spool of the tag.
//
// The follower writes to SpoolSync at most once per whole gram: a gross weight
// that is the same as the last one written is not sent again. Grams counted
// from an odometer wait in the database until a whole gram is due.
package follower

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"math"
	"slices"
	"sync"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
	"github.com/prusa3d/prusa-connect/sdk/deviceapi"
	"github.com/prusa3d/prusa-connect/sdk/event"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/consume"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/store"
)

// statePath is where a device keeps the value of its state machine.
const statePath = "/state"

// printing is the state machine value of a printer that is printing.
const printing = "PRINTING"

// The wait before the follower subscribes again after the stream failed. It
// doubles after each failure up to the ceiling.
const (
	firstRetry = time.Second
	lastRetry  = 30 * time.Second
)

// resyncEvery is how often every printer is read again, so a spool ingested in
// SpoolSync after its last event is found without waiting for it to print.
const resyncEvery = 15 * time.Minute

// Binding is the spool a tool holds, as the follower last worked it out.
type Binding struct {
	// Source is "tag", "assigned" or "none".
	Source string
	// FilamentTypeID is the material of the tool: the one of its spool, or the
	// one a person picked for a tool that holds no spool.
	FilamentTypeID string
	// SpoolID is the SpoolSync spool, empty for none, for a material alone and
	// for a tag SpoolSync holds no spool for.
	SpoolID string
	// UnknownTag is true for a tag whose uuid SpoolSync holds no spool for.
	UnknownTag bool
}

// Placement is the printer tool a spool is on.
type Placement struct {
	PrinterID string
	Tool      string
}

// Printer is one printer as the follower last read it.
type Printer struct {
	ID, Name, Model string
	Connected       bool
	// State is the value of the printer's state machine, such as IDLE.
	State string
}

// ToolEntry is one tool of a printer, with the spool it holds.
type ToolEntry struct {
	Tool    connectorapi.Tool
	Binding Binding
}

// Stats is what the follower has done since it started.
type Stats struct {
	// Written is the number of weights set in SpoolSync.
	Written int `json:"written"`
	// Failed is the number of steps that failed: a read of the printers, the
	// event stream, or a SpoolSync call.
	Failed int `json:"failed"`
	// Unknown is the number of tags SpoolSync held no spool for, at the last full read.
	Unknown    int       `json:"unknown"`
	LastWrite  time.Time `json:"last_write,omitzero"`
	LastError  string    `json:"last_error,omitempty"`
	LastFullAt time.Time `json:"last_full_read,omitzero"`
}

// Devices is the part of the host's device door the follower reads.
type Devices interface {
	List(ctx context.Context, params deviceapi.ListParams) (deviceapi.Page, error)
	Get(ctx context.Context, params deviceapi.GetParams) (deviceapi.Device, error)
}

// Subscribe opens the host's event stream, as busapi.Subscribe does on the
// session.
type Subscribe func(ctx context.Context, params busapi.SubscribeParams, onWindow func(busapi.Window) error) error

// SpoolSync is what the follower needs of the SpoolSync client.
type SpoolSync interface {
	Get(ctx context.Context, id string) (spoolsync.Spool, error)
	FindByInstanceUUID(ctx context.Context, uuid string) (spoolsync.Spool, error)
	SetWeight(ctx context.Context, spoolID string, grossG float64, source string) error
}

// Follower reads every printer from the host, keeps it current from the event
// stream, and applies each tool's report to the spool on it.
type Follower struct {
	devices   Devices
	subscribe Subscribe
	ss        SpoolSync
	store     *store.Store
	log       *slog.Logger
	resync    time.Duration

	// work is held by everything that reads or writes the store or SpoolSync:
	// a window of the stream, a full read, a refresh after a change. The
	// caches below are guarded by it.
	work sync.Mutex
	// spools is the last spool read or written, by SpoolSync id. Its gross
	// weight is what a write is compared with.
	spools map[string]spoolsync.Spool
	// byTag is the spool id of each instance uuid found.
	byTag map[string]string
	// unknown is the instance uuids SpoolSync held no spool for, until the
	// next full read asks again.
	unknown map[string]bool

	// mu guards what the routes read: the printers, the bindings and the stats.
	mu       sync.Mutex
	printers map[string]deviceapi.Device
	bindings map[string]map[connectorapi.ToolID]Binding
	stats    Stats

	// cursor is where the stream resumes. Only Run touches it.
	cursor string

	// onTool is told of each tool whose binding changed, and onSpool of each
	// spool whose weight was written, with the filament left. Both run with
	// work held, and are nil for nobody.
	onTool  func(printerID, toolID string)
	onSpool func(spoolID string, remainingG float64)
}

// Notify sets who is told of a tool whose binding changed, and of a spool whose
// weight was written. Call it before Run.
func (f *Follower) Notify(onTool func(printerID, toolID string), onSpool func(spoolID string, remainingG float64)) {
	f.onTool, f.onSpool = onTool, onSpool
}

// New returns a follower that has read nothing yet.
func New(devices Devices, subscribe Subscribe, ss SpoolSync, st *store.Store, log *slog.Logger) *Follower {
	return &Follower{
		devices: devices, subscribe: subscribe, ss: ss, store: st, log: log,
		resync:   resyncEvery,
		spools:   map[string]spoolsync.Spool{},
		byTag:    map[string]string{},
		unknown:  map[string]bool{},
		printers: map[string]deviceapi.Device{},
		bindings: map[string]map[connectorapi.ToolID]Binding{},
	}
}

// Run reads every printer, then applies the event stream until ctx ends. A
// read that fails or a stream that ends is tried again after a wait, so the
// follower ends only with ctx. The periodic read runs beside the stream.
//
// The host sends no snapshot when a subscription opens, and never replays
// telemetry. A change between the listing and the first window would be
// lost, so the first window of each subscription reads the printers again.
func (f *Follower) Run(ctx context.Context) error {
	go f.every(ctx)

	params := busapi.SubscribeParams{Types: []string{
		event.TypeDeviceTelemetrySampled, event.TypeDevicePresenceChanged, event.TypeDeviceRecordChanged,
	}}

	for wait := firstRetry; ; {
		read := false

		err := f.Full(ctx)
		if err == nil {
			params.Resume = f.cursor
			err = f.subscribe(ctx, params, func(w busapi.Window) error {
				w.Resync = w.Resync || !read
				read = true

				return f.window(ctx, w)
			})
		}

		if ctx.Err() != nil {
			return nil
		}

		if read {
			wait = firstRetry
		}

		f.fail("follow the printers", err)
		f.log.WarnContext(ctx, "the printers could not be followed, trying again", "error", err, "wait", wait)

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(wait):
		}

		wait = min(wait+wait, lastRetry)
	}
}

// every reads every printer again on a timer.
func (f *Follower) every(ctx context.Context) {
	t := time.NewTicker(f.resync)
	defer t.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if err := f.Full(ctx); err != nil && ctx.Err() == nil {
				f.fail("read the printers", err)
			}
		}
	}
}

// Full reads every printer and every spool it reports again, and applies each
// tool. It also writes whatever whole grams are due and were not written yet.
func (f *Follower) Full(ctx context.Context) error {
	f.work.Lock()
	defer f.work.Unlock()

	return f.reloadLocked(ctx)
}

// reloadLocked is Full, with work held.
func (f *Follower) reloadLocked(ctx context.Context) error {
	listed := map[string]deviceapi.Device{}

	for offset := 0; ; {
		page, err := f.devices.List(ctx, deviceapi.ListParams{Limit: deviceapi.MaxPageLimit, Offset: offset})
		if err != nil {
			return fmt.Errorf("list the printers: %w", err)
		}

		for _, d := range page.Items {
			listed[d.ID] = d
		}

		offset += len(page.Items)
		if len(page.Items) == 0 || offset >= page.Total {
			break
		}
	}

	f.mu.Lock()
	f.printers = listed
	for id := range f.bindings {
		if _, ok := listed[id]; !ok {
			delete(f.bindings, id)
		}
	}
	f.mu.Unlock()

	// Every spool and tag is read again: one changed or ingested in SpoolSync
	// since the last read is seen now.
	clear(f.spools)
	clear(f.byTag)
	clear(f.unknown)

	for _, id := range slices.Sorted(maps.Keys(listed)) {
		f.applyLocked(ctx, id)
	}

	if err := f.forgetRetiredLocked(ctx, listed); err != nil {
		return err
	}

	f.flushAllLocked(ctx)

	f.mu.Lock()
	f.stats.LastFullAt = time.Now().UTC()
	f.stats.Unknown = len(f.unknown)
	f.mu.Unlock()

	return nil
}

// forgetRetiredLocked takes the spools off the printers retired while the
// stream was not read. A printer the host does not answer for keeps its
// spools: the follower may only have lost sight of it.
func (f *Follower) forgetRetiredLocked(ctx context.Context, listed map[string]deviceapi.Device) error {
	held, err := f.store.Printers(ctx)
	if err != nil {
		return err
	}

	for _, id := range held {
		if _, ok := listed[id]; ok {
			continue
		}

		d, getErr := f.devices.Get(ctx, deviceapi.GetParams{ID: id})
		if errors.Is(getErr, deviceapi.ErrNoDevice) {
			continue
		}

		if getErr != nil {
			return fmt.Errorf("read printer %s: %w", id, getErr)
		}

		if d.Retired {
			if err = f.forgetLocked(ctx, id); err != nil {
				return err
			}
		}
	}

	return nil
}

// forgetLocked takes the spools off a retired printer.
func (f *Follower) forgetLocked(ctx context.Context, id string) error {
	if err := f.store.ForgetPrinter(ctx, id); err != nil {
		return fmt.Errorf("clear the tools of retired printer %s: %w", id, err)
	}

	f.mu.Lock()
	delete(f.bindings, id)
	f.mu.Unlock()

	f.log.InfoContext(ctx, "a retired printer's tools were cleared", "printer", id)

	return nil
}

// window applies one window of the stream. Telemetry changes one path of a
// tree. Any other event reads its printer again, which covers its presence
// and its name.
func (f *Follower) window(ctx context.Context, w busapi.Window) error {
	f.work.Lock()
	defer f.work.Unlock()

	if w.Resync {
		if err := f.reloadLocked(ctx); err != nil {
			return err
		}
	}

	changed := f.samples(w.Telemetry)

	for _, raw := range w.Events {
		id, ok := deviceOf(raw)
		if !ok {
			continue
		}

		d, err := f.devices.Get(ctx, deviceapi.GetParams{ID: id})
		if errors.Is(err, deviceapi.ErrNoDevice) {
			f.mu.Lock()
			delete(f.printers, id)
			f.mu.Unlock()

			continue
		}

		if err != nil {
			return fmt.Errorf("read printer %s: %w", id, err)
		}

		if d.Retired {
			f.mu.Lock()
			delete(f.printers, id)
			f.mu.Unlock()

			if err = f.forgetLocked(ctx, id); err != nil {
				return err
			}

			continue
		}

		f.mu.Lock()
		f.printers[id] = d
		f.mu.Unlock()

		changed[id] = true
	}

	for _, id := range slices.Sorted(maps.Keys(changed)) {
		f.applyLocked(ctx, id)
	}

	f.cursor = w.Cursor

	return nil
}

// unread are the tool members the follower never reads: the heater and fan
// readings, which a printer sends every few seconds.
//
//nolint:gochecknoglobals // a fixed set, read-only.
var unread = map[string]bool{
	connectorapi.NozzleTemp: true, connectorapi.NozzleTargetTemp: true,
	connectorapi.PrintFanRPM: true, connectorapi.HeatbreakFanRPM: true,
}

// samples writes the telemetry of one window into the printers' trees. A null
// value removes its path. It answers the printers whose tools or state changed.
func (f *Follower) samples(telemetry []json.RawMessage) map[string]bool {
	changed := map[string]bool{}

	f.mu.Lock()
	defer f.mu.Unlock()

	for _, raw := range telemetry {
		id, path, value, ok := sampleOf(raw)
		if !ok {
			continue
		}

		d, held := f.printers[id]
		if !held {
			continue
		}

		_, member, isTool := connectorapi.SplitToolPath(path)
		if path == statePath || (isTool && !unread[member]) {
			changed[id] = true
		}

		write(&d, path, value)
		f.printers[id] = d
	}

	return changed
}

// sampleOf reads one telemetry envelope: its printer, the path and the value.
func sampleOf(raw json.RawMessage) (id, path string, value json.RawMessage, ok bool) {
	var (
		envelope event.Envelope
		reading  struct {
			Path  string          `json:"path"`
			Value json.RawMessage `json:"value"`
		}
	)

	if json.Unmarshal(raw, &envelope) != nil || envelope.Type != event.TypeDeviceTelemetrySampled ||
		json.Unmarshal(envelope.Data, &reading) != nil || reading.Path == "" {
		return "", "", nil, false
	}

	id, ok = deviceOf(raw)

	return id, reading.Path, reading.Value, ok
}

// write sets one path of a printer's tree, and removes it for a null value.
func write(d *deviceapi.Device, path string, value json.RawMessage) {
	if d.State == nil {
		d.State = map[string]json.RawMessage{}
	}

	if len(value) == 0 || string(value) == "null" {
		delete(d.State, path)
	} else {
		d.State[path] = value
	}
}

// deviceOf reads the device id out of an envelope's subject, device:<id>.
func deviceOf(raw json.RawMessage) (string, bool) {
	var envelope event.Envelope
	if json.Unmarshal(raw, &envelope) != nil {
		return "", false
	}

	origin, err := event.ParseOrigin(envelope.Subject)
	if err != nil || origin.Kind() != event.KindDevice {
		return "", false
	}

	return origin.ID(), true
}

// Refresh applies the printer's tools again, after a spool was put on one of
// them or taken off it.
func (f *Follower) Refresh(ctx context.Context, printerID string) {
	f.work.Lock()
	defer f.work.Unlock()

	f.applyLocked(ctx, printerID)
}

// SpoolChanged forgets the weight the follower last knew of a spool, because
// someone set it in SpoolSync directly. The next write is compared with the
// weight SpoolSync holds.
func (f *Follower) SpoolChanged(spoolID string) {
	f.work.Lock()
	defer f.work.Unlock()

	delete(f.spools, spoolID)
}

// applyLocked puts the spool each tool holds onto the tool's binding, and
// charges what the tools fed. work is held.
func (f *Follower) applyLocked(ctx context.Context, id string) {
	f.mu.Lock()
	d, held := f.printers[id]
	f.mu.Unlock()

	if !held {
		return
	}

	tree := connectorapi.ReadTools(d.State)
	isPrinting := printerState(d) == printing

	ids := slices.Collect(maps.Keys(tree.Tools))
	slices.SortFunc(ids, connectorapi.ToolID.Compare)

	bindings := map[connectorapi.ToolID]Binding{}

	for _, toolID := range ids {
		tool := tree.Tools[toolID]
		if tool.Disabled || tool.HasFeeder() {
			continue
		}

		bindings[toolID] = f.bindTool(ctx, id, toolID, *tool)
	}

	// A printer that is not printing has ended its job: the whole grams
	// counted off its spools are written now, not at the next gram.
	if !isPrinting {
		for _, b := range bindings {
			if b.SpoolID != "" {
				f.flushLocked(ctx, b.SpoolID)
			}
		}
	}

	f.mu.Lock()
	before := f.bindings[id]
	f.bindings[id] = bindings
	f.mu.Unlock()

	if f.onTool != nil {
		for _, toolID := range ids {
			if before[toolID] != bindings[toolID] {
				f.onTool(id, string(toolID))
			}
		}
	}
}

// bindTool works out the spool of one tool, and writes or counts what its
// report says.
func (f *Follower) bindTool(ctx context.Context, printerID string, toolID connectorapi.ToolID, tool connectorapi.Tool) Binding {
	if rep := tool.Reported; rep != nil && rep.InstanceUUID != "" {
		// A tag wins over an assignment: the tool's assignment goes.
		if _, err := f.store.DeleteAssignment(ctx, printerID, string(toolID)); err != nil {
			f.fail("clear the assignment of "+printerID+" "+string(toolID), err)
		}

		sp, found, err := f.spoolByTag(ctx, rep.InstanceUUID)
		if err != nil {
			f.fail("find the spool of tag "+rep.InstanceUUID, err)

			return Binding{Source: "tag"}
		}

		if !found {
			return Binding{Source: "tag", UnknownTag: true}
		}

		if tool.ConsumedWeight != nil && rep.FullWeight != nil {
			// The tag's own count is the spool's truth, so grams counted off
			// an odometer before it wait no longer.
			if err = f.store.ClearPending(ctx, sp.ID); err != nil {
				f.fail("clear the pending grams of spool "+sp.ID, err)
			}

			left := math.Max(0, *rep.FullWeight-*tool.ConsumedWeight)
			f.setGross(ctx, sp, math.Floor(left+sp.EmptyG))

			return Binding{Source: "tag", FilamentTypeID: sp.FilamentTypeID, SpoolID: sp.ID}
		}

		f.count(ctx, printerID, toolID, tool, sp)

		return Binding{Source: "tag", FilamentTypeID: sp.FilamentTypeID, SpoolID: sp.ID}
	}

	a, ok, err := f.store.Assignment(ctx, printerID, string(toolID))
	if err != nil {
		f.fail("read the assignment of "+printerID+" "+string(toolID), err)

		return Binding{Source: "none"}
	}

	if !ok {
		return Binding{Source: "none"}
	}

	// A material a person picked with no spool: the tool holds it, and no
	// spool is counted.
	if a.SpoolID == "" {
		return Binding{Source: "assigned", FilamentTypeID: a.FilamentTypeID}
	}

	sp, err := f.spoolByID(ctx, a.SpoolID)
	if err != nil {
		if !errors.Is(err, spoolsync.ErrNotFound) {
			f.fail("read spool "+a.SpoolID, err)
		}

		// The spool is gone from SpoolSync, or unreadable for now: the tool
		// keeps its assignment and counts nothing.
		return Binding{Source: "assigned", FilamentTypeID: a.FilamentTypeID, SpoolID: a.SpoolID}
	}

	f.count(ctx, printerID, toolID, tool, sp)

	return Binding{Source: "assigned", FilamentTypeID: a.FilamentTypeID, SpoolID: sp.ID}
}

// spoolByTag is the spool with an instance uuid. found is false for a uuid
// SpoolSync holds no spool for.
func (f *Follower) spoolByTag(ctx context.Context, uuid string) (sp spoolsync.Spool, found bool, err error) {
	if id, ok := f.byTag[uuid]; ok {
		if sp, ok = f.spools[id]; ok {
			return sp, true, nil
		}
	}

	if f.unknown[uuid] {
		return spoolsync.Spool{}, false, nil
	}

	sp, err = f.ss.FindByInstanceUUID(ctx, uuid)
	if errors.Is(err, spoolsync.ErrNotFound) {
		f.unknown[uuid] = true

		return spoolsync.Spool{}, false, nil
	}

	if err != nil {
		return spoolsync.Spool{}, false, err
	}

	f.byTag[uuid] = sp.ID
	f.spools[sp.ID] = sp

	return sp, true, nil
}

// spoolByID is the spool with a SpoolSync id, read once until a full read.
func (f *Follower) spoolByID(ctx context.Context, id string) (spoolsync.Spool, error) {
	if sp, ok := f.spools[id]; ok {
		return sp, nil
	}

	sp, err := f.ss.Get(ctx, id)
	if err != nil {
		return spoolsync.Spool{}, err
	}

	f.spools[id] = sp

	return sp, nil
}

// count charges what a tool fed since its last reading to the spool on it,
// and keeps the reading. A first reading, a reset and a stale one charge
// nothing.
func (f *Follower) count(ctx context.Context, printerID string, toolID connectorapi.ToolID, tool connectorapi.Tool, sp spoolsync.Spool) {
	if tool.OdometerE == nil {
		return
	}

	cur := store.Odometer{ID: tool.OdometerID, E: *tool.OdometerE, Weight: tool.OdometerWeight}

	prev, had, err := f.store.Odometer(ctx, printerID, string(toolID))
	if err != nil {
		f.fail("read the odometer of "+printerID+" "+string(toolID), err)

		return
	}

	if had && sameReading(prev, cur) {
		return
	}

	if !had {
		f.keep(ctx, printerID, toolID, cur)

		return
	}

	grams, stale := gramsBetween(prev, cur, sp)
	if stale {
		return
	}

	f.keep(ctx, printerID, toolID, cur)

	if grams <= 0 {
		return
	}

	if err = f.store.AddPending(ctx, sp.ID, grams); err != nil {
		f.fail("count the filament of spool "+sp.ID, err)

		return
	}

	f.flushLocked(ctx, sp.ID)
}

// keep stores a reading of a tool's odometer.
func (f *Follower) keep(ctx context.Context, printerID string, toolID connectorapi.ToolID, cur store.Odometer) {
	if err := f.store.SetOdometer(ctx, printerID, string(toolID), cur, time.Now()); err != nil {
		f.fail("keep the odometer of "+printerID+" "+string(toolID), err)
	}
}

// sameReading is true when a reading is the one the tool holds already.
func sameReading(prev, cur store.Odometer) bool {
	return prev.E == cur.E && equalID(prev.ID, cur.ID) && equalWeight(prev.Weight, cur.Weight)
}

func equalID(a, b *int64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

func equalWeight(a, b *float64) bool { return (a == nil) == (b == nil) && (a == nil || *a == *b) }

// gramsBetween is the weight of the filament fed between two readings. Where
// the printer reports weight, its delta is the weight; otherwise the length
// fed is turned into weight with the spool's density and diameter.
func gramsBetween(prev store.Odometer, cur store.Odometer, sp spoolsync.Spool) (grams float64, stale bool) {
	mm, stale := consume.Delta(&prev.E, prev.ID, cur.E, cur.ID)
	if stale {
		return 0, true
	}

	if cur.Weight == nil {
		return consume.Grams(mm, sp.Filament.DensityG, sp.Material, &sp.Filament.DiameterMM), false
	}

	if prev.Weight == nil {
		// The printer started to report weight: the first reading of it
		// charges nothing.
		return 0, false
	}

	return consume.Delta(prev.Weight, prev.ID, *cur.Weight, cur.ID)
}

// flushLocked writes the whole grams counted off a spool that were not written
// yet, as one write of its gross weight less them. A fraction of a gram waits.
func (f *Follower) flushLocked(ctx context.Context, spoolID string) {
	pending, err := f.store.Pending(ctx, spoolID)
	if err != nil {
		f.fail("read the pending grams of spool "+spoolID, err)

		return
	}

	whole := math.Floor(pending)
	if whole < 1 {
		return
	}

	sp, err := f.ss.Get(ctx, spoolID)
	if errors.Is(err, spoolsync.ErrNotFound) {
		// The spool is gone from SpoolSync: nothing to write the grams to.
		if err = f.store.ClearPending(ctx, spoolID); err != nil {
			f.fail("clear the pending grams of spool "+spoolID, err)
		}

		return
	}

	if err != nil {
		f.fail("read spool "+spoolID, err)

		return
	}

	f.spools[spoolID] = sp

	if !f.setGross(ctx, sp, math.Max(math.Floor(sp.GrossG)-whole, sp.EmptyG)) {
		return
	}

	if err = f.store.TakePending(ctx, spoolID, whole); err != nil {
		f.fail("take the pending grams of spool "+spoolID, err)
	}
}

// flushAllLocked writes the whole grams due on every spool that has grams
// counted, which a full read does so a gram counted while SpoolSync was out of
// reach is not kept for ever.
func (f *Follower) flushAllLocked(ctx context.Context) {
	spools, err := f.store.PendingSpools(ctx)
	if err != nil {
		f.fail("list the pending grams", err)

		return
	}

	for _, id := range slices.Sorted(maps.Keys(spools)) {
		f.flushLocked(ctx, id)
	}
}

// setGross sets the gross weight of a spool, unless SpoolSync holds it already.
// It reports whether the spool holds the gross weight afterwards.
func (f *Follower) setGross(ctx context.Context, sp spoolsync.Spool, gross float64) bool {
	if sp.GrossG == gross {
		return true
	}

	if err := f.ss.SetWeight(ctx, sp.ID, gross, spoolsync.Source); err != nil {
		f.fail("set the weight of spool "+sp.ID, err)

		return false
	}

	sp.GrossG = gross
	f.spools[sp.ID] = sp

	f.mu.Lock()
	f.stats.Written++
	f.stats.LastWrite = time.Now().UTC()
	f.mu.Unlock()

	if f.onSpool != nil {
		f.onSpool(sp.ID, math.Max(0, gross-sp.EmptyG))
	}

	return true
}

// Printers answers every printer the follower holds, sorted by id.
func (f *Follower) Printers() []Printer {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := make([]Printer, 0, len(f.printers))
	for _, id := range slices.Sorted(maps.Keys(f.printers)) {
		out = append(out, printerOf(f.printers[id]))
	}

	return out
}

// Printer answers the printer with that id, and false for one the follower does not hold.
func (f *Follower) Printer(id string) (Printer, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.printers[id]
	if !ok {
		return Printer{}, false
	}

	return printerOf(d), true
}

// Tools answers the tools of one printer that a person sees: the enabled ones,
// except a head that has a feeder, whose slots are listed instead. false for a
// printer the follower does not hold.
func (f *Follower) Tools(printerID string) ([]ToolEntry, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()

	d, ok := f.printers[printerID]
	if !ok {
		return nil, false
	}

	tree := connectorapi.ReadTools(d.State)
	ids := slices.Collect(maps.Keys(tree.Tools))
	slices.SortFunc(ids, connectorapi.ToolID.Compare)

	out := []ToolEntry{}

	for _, id := range ids {
		tool := tree.Tools[id]
		if tool.Disabled || tool.HasFeeder() {
			continue
		}

		b := f.bindings[printerID][id]
		if b.Source == "" {
			b.Source = "none"
		}

		out = append(out, ToolEntry{Tool: *tool, Binding: b})
	}

	return out, true
}

// Placements answers the tool each spool the follower placed is on, by spool id.
// A material alone is on a tool and no spool.
func (f *Follower) Placements() map[string]Placement {
	f.mu.Lock()
	defer f.mu.Unlock()

	out := map[string]Placement{}

	for printerID, tools := range f.bindings {
		for toolID, b := range tools {
			if b.SpoolID != "" && b.Source != "none" {
				out[b.SpoolID] = Placement{PrinterID: printerID, Tool: string(toolID)}
			}
		}
	}

	return out
}

// Stats is a copy of what the follower has done.
func (f *Follower) Stats() Stats {
	f.mu.Lock()
	defer f.mu.Unlock()

	return f.stats
}

// Counts answers how many printers the follower holds, and how many tools hold a spool.
func (f *Follower) Counts() (printers, toolsWithSpool int) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, tools := range f.bindings {
		for _, b := range tools {
			if b.SpoolID != "" && b.Source != "none" {
				toolsWithSpool++
			}
		}
	}

	return len(f.printers), toolsWithSpool
}

// fail counts a failed step and keeps its message for the status.
func (f *Follower) fail(what string, err error) {
	if err == nil {
		return
	}

	f.mu.Lock()
	f.stats.Failed++
	f.stats.LastError = what + ": " + err.Error()
	f.mu.Unlock()

	f.log.Warn("sync failed", "step", what, "err", err)
}

// printerState is the value of the printer's state machine.
func printerState(d deviceapi.Device) string {
	var state string

	_ = json.Unmarshal(d.State[statePath], &state)

	return state
}

func printerOf(d deviceapi.Device) Printer {
	return Printer{
		ID: d.ID, Name: d.Name, Model: d.Model, Connected: d.Connected, State: printerState(d),
	}
}
