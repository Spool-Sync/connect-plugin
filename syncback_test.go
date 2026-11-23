package main

import (
	"testing"
	"time"

	"github.com/spool-sync/connect-plugin/internal/spoolsync"
	"github.com/spool-sync/connect-plugin/internal/store"
)

// storeAssignment is what a person put on a tool, for a test.
func storeAssignment(printer, tool, spool string, at time.Time) store.Assignment {
	return store.Assignment{
		PrinterID: printer, Tool: tool, FilamentTypeID: "ft1", SpoolID: spool,
		AssignedAt: at, AssignedBy: "person",
	}
}

// A slot a person filled in SpoolSync is adopted, and its spool counted from
// what the tool reads now.
func TestASpoolAssignedInSpoolSyncGoesOnTheTool(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))

	spool := "s1"
	at := time.Now().UTC()

	err := r.svc.applyConnectState(r.ctx, spoolsync.ConnectState{
		GeneratedAt: at,
		Printers: []spoolsync.ConnectPrinter{{ConnectID: "p1", Slots: []spoolsync.ConnectSlot{
			{Index: 0, SpoolID: &spool, UpdatedAt: at},
		}}},
		Spools: []spoolsync.ConnectSpool{{SpoolID: spool, FilamentTypeID: "ft1", GrossG: 900, Status: "ACTIVE"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	a, ok, err := r.svc.st.Assignment(r.ctx, "p1", "0")
	if err != nil || !ok {
		t.Fatalf("assignment: %+v %v %v", a, ok, err)
	}

	if a.SpoolID != spool || a.FilamentTypeID != "ft1" || a.AssignedBy != "spoolsync" {
		t.Fatalf("assignment %+v", a)
	}

	entries, _ := r.svc.fo.Tools("p1")
	if len(entries) != 1 || entries[0].Binding.SpoolID != spool || entries[0].Binding.Source != "assigned" {
		t.Fatalf("tool binding %+v", entries)
	}
}

// A slot a person cleared in SpoolSync is taken off the tool.
func TestASlotClearedInSpoolSyncComesOffTheTool(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))

	assigned := time.Now().UTC().Add(-time.Minute)
	if err := r.svc.st.SetAssignment(r.ctx, storeAssignment("p1", "0", "s1", assigned)); err != nil {
		t.Fatal(err)
	}

	cleared := time.Now().UTC()
	err := r.svc.applyConnectState(r.ctx, spoolsync.ConnectState{
		GeneratedAt: cleared,
		Printers: []spoolsync.ConnectPrinter{{ConnectID: "p1", Slots: []spoolsync.ConnectSlot{
			{Index: 0, SpoolID: nil, UpdatedAt: cleared},
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}

	if _, ok, err := r.svc.st.Assignment(r.ctx, "p1", "0"); err != nil || ok {
		t.Fatalf("the assignment stayed: %v %v", ok, err)
	}
}

// A slot the module changed after SpoolSync's snapshot is not undone by it.
func TestTheModulesLaterAssignmentWins(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))

	snapshot := time.Now().UTC().Add(-time.Minute)
	mine := time.Now().UTC()
	if err := r.svc.st.SetAssignment(r.ctx, storeAssignment("p1", "0", "mine", mine)); err != nil {
		t.Fatal(err)
	}

	theirs := "theirs"
	err := r.svc.applyConnectState(r.ctx, spoolsync.ConnectState{
		GeneratedAt: mine,
		Printers: []spoolsync.ConnectPrinter{{ConnectID: "p1", Slots: []spoolsync.ConnectSlot{
			{Index: 0, SpoolID: &theirs, UpdatedAt: snapshot},
		}}},
		Spools: []spoolsync.ConnectSpool{{SpoolID: theirs, FilamentTypeID: "ft1", GrossG: 500, Status: "ACTIVE"}},
	})
	if err != nil {
		t.Fatal(err)
	}

	a, _, _ := r.svc.st.Assignment(r.ctx, "p1", "0")
	if a.SpoolID != "mine" {
		t.Fatalf("SpoolSync's older slot undid the module's: %+v", a)
	}
}

// A weight a person corrected in SpoolSync drops the grams counted but not yet
// written, which the correction supersedes.
func TestACorrectedWeightDropsThePendingGrams(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))

	if err := r.svc.st.AddPending(r.ctx, "s1", 3); err != nil {
		t.Fatal(err)
	}

	state := func(g float64) spoolsync.ConnectState {
		return spoolsync.ConnectState{
			GeneratedAt: time.Now().UTC(),
			Spools:      []spoolsync.ConnectSpool{{SpoolID: "s1", FilamentTypeID: "ft1", GrossG: g, Status: "ACTIVE"}},
		}
	}

	if err := r.svc.applyConnectState(r.ctx, state(900)); err != nil {
		t.Fatal(err)
	}

	if pending, _ := r.svc.st.Pending(r.ctx, "s1"); pending != 3 {
		t.Fatalf("the first snapshot dropped the pending grams: %v", pending)
	}

	if err := r.svc.applyConnectState(r.ctx, state(850)); err != nil {
		t.Fatal(err)
	}

	if pending, _ := r.svc.st.Pending(r.ctx, "s1"); pending != 0 {
		t.Fatalf("the correction left %v g pending", pending)
	}
}
