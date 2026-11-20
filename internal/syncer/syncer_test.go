package syncer

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/busapi"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
)

const ns = "com.github.aux.prusa3d.materials"

type fakeSS struct {
	mu     sync.Mutex
	spools map[string]spoolsync.Spool // by instance uuid
	finds  int
	set    map[string]float64 // gross weight by SpoolSync id
}

func (f *fakeSS) FindByInstanceUUID(_ context.Context, uuid string) (spoolsync.Spool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finds++

	sp, ok := f.spools[uuid]
	if !ok {
		return sp, spoolsync.ErrNotFound
	}

	return sp, nil
}

func (f *fakeSS) SetWeight(_ context.Context, id string, g float64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.set[id] = g

	for k, sp := range f.spools {
		if sp.ID == id {
			sp.CurrentWeightG = g
			f.spools[k] = sp
		}
	}

	return nil
}

func spool(id string, current, empty float64) spoolsync.Spool {
	sp := spoolsync.Spool{ID: id, CurrentWeightG: current}
	sp.FilamentType.SpoolWeightG = empty

	return sp
}

type fakeMaterials struct{ pages []string }

func (f fakeMaterials) Call(_ context.Context, p routesapi.CallParams) (routesapi.Response, error) {
	if p.Module != ns || p.Method != http.MethodGet || p.Path != "/spools" {
		return routesapi.Response{Status: http.StatusNotFound}, nil
	}

	switch p.Query {
	case "limit=100&offset=0":
		return routesapi.Response{Status: http.StatusOK, Body: []byte(f.pages[0])}, nil
	case "limit=100&offset=100":
		return routesapi.Response{Status: http.StatusOK, Body: []byte(f.pages[1])}, nil
	}

	return routesapi.Response{Status: http.StatusBadRequest}, nil
}

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func newFake() *fakeSS {
	return &fakeSS{spools: map[string]spoolsync.Spool{
		"tag-a": spool("s-a", 1200, 200),
		"tag-b": spool("s-b", 450, 250),
	}, set: map[string]float64{}}
}

// A full sync pushes only live printer spools, as filament left plus the empty spool.
func TestFullSyncPushesTaggedSpoolsWithTheEmptySpoolAdded(t *testing.T) {
	ss := newFake()
	mats := fakeMaterials{pages: []string{
		`{"spools":[
			{"uuid":"tag-a","source":"printer","remaining_weight_g":812.7},
			{"uuid":"manual-1","source":"manual","remaining_weight_g":500},
			{"uuid":"tag-gone","source":"printer","remaining_weight_g":10,"archived_at":"2026-11-01T00:00:00Z"}
		],"total":4,"next":100}`,
		`{"spools":[{"uuid":"tag-b","source":"printer","remaining_weight_g":200.2}],"total":4}`,
	}}
	s := New(ns, mats, nil, ss, quiet())

	if err := s.FullSync(t.Context()); err != nil {
		t.Fatal(err)
	}

	if got := ss.set["s-a"]; got != 1012.7 {
		t.Fatalf("s-a set to %v, want 1012.7", got)
	}

	// 200.2 + 250 is within half a gram of 450: nothing to write.
	if _, wrote := ss.set["s-b"]; wrote {
		t.Fatalf("s-b was written though its weight already matched")
	}

	if ss.finds != 2 {
		t.Fatalf("asked SpoolSync %d times, want 2 (manual and archived spools skipped)", ss.finds)
	}
}

func event(t *testing.T, typ string, data any) json.RawMessage {
	t.Helper()

	d, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}

	b, err := json.Marshal(map[string]any{"type": typ, "data": json.RawMessage(d)})
	if err != nil {
		t.Fatal(err)
	}

	return b
}

// Spool events push once per whole gram, and an unknown spool is asked about
// once until the next full sync.
func TestEventsPushOncePerGram(t *testing.T) {
	ss := newFake()
	s := New(ns, fakeMaterials{}, nil, ss, quiet())

	for _, raw := range []json.RawMessage{
		event(t, ns+".spool", spoolEvent{SpoolUUID: "tag-a", RemainingWeightG: 900.9}),
		event(t, ns+".spool", spoolEvent{SpoolUUID: "tag-a", RemainingWeightG: 900.1}), // same gram
		event(t, ns+".spool", spoolEvent{SpoolUUID: "tag-x", RemainingWeightG: 300}),
		event(t, ns+".spool", spoolEvent{SpoolUUID: "tag-x", RemainingWeightG: 299}),
		event(t, ns+".spool", spoolEvent{SpoolUUID: "tag-a", Deleted: true}),
		event(t, ns+".tool", spoolEvent{SpoolUUID: "tag-a", RemainingWeightG: 1}),
		json.RawMessage(`not json`),
	} {
		s.handle(t.Context(), raw)
	}

	if got := ss.set["s-a"]; got != 1100.9 {
		t.Fatalf("s-a set to %v, want 1100.9", got)
	}

	if ss.finds != 2 {
		t.Fatalf("asked SpoolSync %d times, want 2", ss.finds)
	}

	if st := s.Stats(); st.Pushed != 1 || st.Unknown != 1 {
		t.Fatalf("stats %+v", st)
	}
}

// Run reads every spool, then follows the stream; a resync reads them again.
func TestRunSyncsThenFollowsTheStream(t *testing.T) {
	ss := newFake()
	mats := fakeMaterials{pages: []string{`{"spools":[{"uuid":"tag-a","source":"printer","remaining_weight_g":1000}]}`}}

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var params busapi.SubscribeParams

	sub := func(_ context.Context, p busapi.SubscribeParams, on func(busapi.Window) error) error {
		params = p
		_ = on(busapi.Window{
			Events: []json.RawMessage{event(t, ns+".spool", spoolEvent{SpoolUUID: "tag-a", RemainingWeightG: 990})},
			Cursor: "c1",
		})

		cancel()

		return nil
	}

	s := New(ns, mats, sub, ss, quiet())
	_ = s.Run(ctx)

	if len(params.Types) != 1 || params.Types[0] != ns+".spool" {
		t.Fatalf("subscribed to %v", params.Types)
	}

	if got := ss.set["s-a"]; got != 1190 {
		t.Fatalf("s-a set to %v, want 1190 (990 left + 200 empty)", got)
	}
}
