package main

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/follower"
)

func entry(id string, spool string, active bool) follower.ToolEntry {
	return follower.ToolEntry{
		Tool:    connectorapi.Tool{ID: connectorapi.ToolID(id), Active: &active},
		Binding: follower.Binding{SpoolID: spool},
	}
}

// An MMU's slots are SpoolSync's slots; its head is not one.
func TestAnMMUsSlotsAreTheSlots(t *testing.T) {
	slots, active := slotsOf([]follower.ToolEntry{
		entry("0", "", true), entry("0.0", "s1", false), entry("0.1", "", false), entry("0.2", "s3", true),
	})

	if len(slots) != 3 || slots[0].Index != 0 || *slots[0].SpoolID != "s1" || slots[1].SpoolID != nil || slots[2].Index != 2 {
		t.Fatalf("slots %+v", slots)
	}

	if active == nil || *active != 2 {
		t.Fatalf("active %v, want slot 2", active)
	}
}

// A printer with no MMU has its heads as slots: one on an MK4, five on an XL.
func TestHeadsAreTheSlotsWithoutAnMMU(t *testing.T) {
	slots, active := slotsOf([]follower.ToolEntry{entry("0", "s1", false), entry("1", "", true), entry("4", "s5", false)})

	if len(slots) != 3 || slots[2].Index != 4 || *slots[2].SpoolID != "s5" {
		t.Fatalf("slots %+v", slots)
	}

	if active == nil || *active != 1 {
		t.Fatalf("active %v, want head 1", active)
	}
}

// Connect's states read as SpoolSync's statuses.
func TestStatusesReadAsSpoolSyncs(t *testing.T) {
	for state, want := range map[string]string{
		"PRINTING": "PRINTING", "PAUSED": "PAUSED", "ERROR": "ERROR", "ATTENTION": "ERROR",
		"IDLE": "OPERATIONAL", "FINISHED": "OPERATIONAL", "BUSY": "OPERATIONAL", "": "UNKNOWN",
	} {
		if got := statusOf(follower.Printer{Connected: true, State: state}); got != want {
			t.Errorf("%q reads as %s, want %s", state, got, want)
		}
	}

	if got := statusOf(follower.Printer{Connected: false, State: "PRINTING"}); got != "OFFLINE" {
		t.Errorf("a printer away reads as %s", got)
	}
}

// The report holds the printer as the follower has it, with the spool on its tool.
func TestTheReportHoldsThePrinterAndItsSpool(t *testing.T) {
	r := newRig(t, printerWith("IDLE", map[string]map[string]any{"0": {}}))
	r.access = allow{"user:alpha operate p1": true, "user:alpha observe p1": true}
	r.svc.access = r.access

	if rec := r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":500,"quantity":1}`, "user:alpha"); rec.Code != 201 {
		t.Fatalf("add answered %d %s", rec.Code, rec.Body.String())
	}

	var id string
	for k := range r.ss.spools {
		id = k
	}

	if rec := r.call(t, "PUT", "/printers/p1/tools/0", `{"spool_id":"`+id+`"}`, "user:alpha"); rec.Code >= 300 {
		t.Fatalf("assign answered %d %s", rec.Code, rec.Body.String())
	}

	reports := r.svc.printerReports()
	if len(reports) != 1 || reports[0].ConnectID != "p1" || reports[0].Status != "OPERATIONAL" || reports[0].Name != "Prusa XL" {
		t.Fatalf("reports %+v", reports)
	}

	if s := reports[0].Slots; len(s) != 1 || s[0].Index != 0 || s[0].SpoolID == nil || *s[0].SpoolID != id {
		t.Fatalf("the tool reports %+v, want spool %s in slot 0", s, id)
	}
}
