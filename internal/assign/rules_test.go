// Copied from extensions/materials/internal/assign/rules_test.go in the prusa-connect
// repository, by Prusa Research a.s., licensed under
// OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1 (https://github.com/prusa3d/prusa-connect), as this module is.
// Unchanged apart from this note.
package assign

import (
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// A printer that is not connected is offline.

func reported(abbreviation string) Tool { return Tool{Reports: true, Reported: &abbreviation} }

// notReporting is a tool of a printer that reports no material, with PETG stored on it.
func notReporting() Tool { return Tool{Reported: new("PETG")} }

func material(abbreviation string) Candidate { return Candidate{Abbreviation: abbreviation} }

func TestStateOf(t *testing.T) {
	for _, c := range []struct {
		connected bool
		state     string
		want      State
	}{
		{false, "PRINTING", Offline},
		{true, "PRINTING", Printing},
		{true, "PAUSED", Printing},
		{true, "BUSY", Busy},
		{true, "MANIPULATING", Busy},
		{true, "IDLE", Standby},
		{true, "READY", Standby},
		{true, "FINISHED", Standby},
		{true, "STOPPED", Standby},
		{true, "ATTENTION", Other},
		{true, "ERROR", Other},
		{true, "UNKNOWN", Other},
		{true, "", Other},
		{true, "SOMETHING_NEW", Other},
	} {
		if got := StateOf(c.connected, c.state); got != c.want {
			t.Errorf("StateOf(%v, %q) = %v, want %v", c.connected, c.state, got, c.want)
		}
	}
}

func TestMayAssign(t *testing.T) {
	plaPlus := Candidate{Abbreviation: "PLA+", Type: "PLA"}

	for _, c := range []struct {
		name      string
		connected bool
		state     string
		tool      Tool
		candidate Candidate
		want      Reason
	}{
		{"offline over another material", false, "", reported("PETG"), material("PLA"), Allowed},
		{"offline with nothing reported", false, "", Tool{Reports: true}, material("PLA"), Allowed},
		{"type matches", true, "IDLE", reported("PLA"), plaPlus, Allowed},
		{"same material in IDLE", true, "IDLE", reported("PETG"), material("PETG"), Allowed},
		{"same material in READY", true, "READY", reported("PETG"), material("PETG"), Allowed},
		{"same material in FINISHED", true, "FINISHED", reported("PETG"), material("PETG"), Allowed},
		{"same material in STOPPED", true, "STOPPED", reported("PETG"), material("PETG"), Allowed},
		{"case differs", true, "IDLE", reported("petg"), material("PETG"), Allowed},
		{"another material", true, "IDLE", reported("PETG"), material("PLA"), ReportedLoaded},
		{"nothing reported", true, "IDLE", Tool{Reports: true}, material("PLA"), ReportedEmpty},
		{"candidate without abbreviation", true, "IDLE", reported("PETG"), material(""), ReportedLoaded},
		{"reporter in PRINTING", true, "PRINTING", reported("PETG"), material("PETG"), WhilePrinting},
		{"reporter in PAUSED", true, "PAUSED", reported("PETG"), material("PETG"), WhilePrinting},
		{"reporter in BUSY", true, "BUSY", reported("PETG"), material("PETG"), WhileBusy},
		{"reporter in MANIPULATING", true, "MANIPULATING", reported("PETG"), material("PETG"), WhileBusy},
		{"reporter in ATTENTION", true, "ATTENTION", reported("PETG"), material("PETG"), WhileBusy},
		{"reporter in ERROR", true, "ERROR", reported("PETG"), material("PETG"), WhileBusy},
		{"reporter in UNKNOWN", true, "UNKNOWN", reported("PETG"), material("PETG"), WhileBusy},
		{"reporter in no state", true, "", reported("PETG"), material("PETG"), WhileBusy},
		{"reporter in SOMETHING_NEW", true, "SOMETHING_NEW", reported("PETG"), material("PETG"), WhileBusy},
		{"PC takes PCCF", true, "IDLE", reported("PC"), material("PCCF"), Allowed},
		{"PLA takes WOOD", true, "IDLE", reported("PLA"), material("WOOD"), Allowed},
		{"alias ignores case", true, "IDLE", reported("PLA"), material("Wood"), Allowed},
		{"FLEX takes TPU", true, "IDLE", reported("FLEX"), material("TPU"), Allowed},
		{"FLEX takes TPE", true, "IDLE", reported("FLEX"), material("TPE"), Allowed},
		{"WOOD takes PLA of its group", true, "IDLE", reported("WOOD"), material("PLA"), Allowed},
		{"alias does not cover TPU", true, "IDLE", reported("PLA"), material("TPU"), ReportedLoaded},
		{"non-reporter offline", false, "", notReporting(), material("PLA"), Allowed},
		{"non-reporter in IDLE", true, "IDLE", notReporting(), material("PLA"), Allowed},
		{"non-reporter in READY", true, "READY", notReporting(), material("PLA"), Allowed},
		{"non-reporter in FINISHED", true, "FINISHED", notReporting(), material("PLA"), Allowed},
		{"non-reporter in STOPPED", true, "STOPPED", notReporting(), material("PLA"), Allowed},
		{"non-reporter in BUSY", true, "BUSY", notReporting(), material("PLA"), Allowed},
		{"non-reporter in MANIPULATING", true, "MANIPULATING", notReporting(), material("PLA"), Allowed},
		{"non-reporter in PRINTING", true, "PRINTING", notReporting(), material("PLA"), WhilePrinting},
		{"non-reporter in PAUSED", true, "PAUSED", notReporting(), material("PLA"), WhilePrinting},
		{"non-reporter in ATTENTION", true, "ATTENTION", notReporting(), material("PLA"), WhileBusy},
		{"non-reporter in ERROR", true, "ERROR", notReporting(), material("PLA"), WhileBusy},
		{"non-reporter in UNKNOWN", true, "UNKNOWN", notReporting(), material("PLA"), WhileBusy},
		{"non-reporter in no state", true, "", notReporting(), material("PLA"), WhileBusy},
		{"non-reporter in SOMETHING_NEW", true, "SOMETHING_NEW", notReporting(), material("PLA"), WhileBusy},
		{"custom name takes any material", true, "IDLE", reported("CUSTOM1"), material("PLA"), Allowed},
		{"custom name takes a catalogue spool", true, "IDLE", reported("CUSTOM1"), material("PETG"), Allowed},
		// Printing locks every action regardless of capability
		{"printing on a reporter", true, "PRINTING", Tool{Reports: true}, material("PLA"), WhilePrinting},
		{"printing on a non-reporter", true, "PRINTING", Tool{}, material("PLA"), WhilePrinting},
		// Reporting printer (MK4), offline allows assign and remove
		{"reporter offline", false, "", Tool{Reports: true}, material("PLA"), Allowed},
		// Reporting printer (MK4), online and empty allows only remove
		{"reporter online and empty", true, "IDLE", Tool{Reports: true}, material("PLA"), ReportedEmpty},
		{"reporter reports no material", true, "IDLE", reported(""), material("PLA"), ReportedEmpty},
		// Reporting printer (MK4), online with a loaded material allows only assign
		{"reporter online and loaded", true, "IDLE", reported("PETG"), material("PETG"), Allowed},
		// Reporting printer (MK4), busy locks every action
		{"reporter busy", true, "BUSY", reported("PETG"), material("PETG"), WhileBusy},
		// A spool read off its NFC tag refuses re-assignment
		{"NFC", true, "IDLE", Tool{Reports: true, Reported: new("PETG"), FromNFC: true}, material("PETG"), ReadOffTag},
		{"NFC while busy", true, "BUSY", Tool{Reports: true, Reported: new("PETG"), FromNFC: true}, material("PETG"), WhileBusy},
		{"NFC offline", false, "", Tool{Reports: true, Reported: new("PETG"), FromNFC: true}, material("PETG"), ReadOffTag},
		// Non-reporting printer (MK3), offline, online and busy all allow
		{"non-reporter offline with no report", false, "", Tool{}, material("PLA"), Allowed},
		{"non-reporter online", true, "IDLE", Tool{}, material("PLA"), Allowed},
		{"non-reporter busy", true, "BUSY", Tool{}, material("PLA"), Allowed},
	} {
		if got := MayAssign(StateOf(c.connected, c.state), c.tool, c.candidate); got != c.want {
			t.Errorf("%s: MayAssign = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestMayUnassign(t *testing.T) {
	for _, c := range []struct {
		name      string
		connected bool
		state     string
		tool      Tool
		want      Reason
	}{
		{"offline", false, "", reported("PETG"), Allowed},
		{"nothing reported", true, "IDLE", Tool{Reports: true}, Allowed},
		{"material reported", true, "IDLE", reported("PETG"), Allowed},
		{"reporter in PRINTING", true, "PRINTING", Tool{Reports: true}, WhilePrinting},
		{"reporter in PAUSED", true, "PAUSED", Tool{Reports: true}, WhilePrinting},
		{"reporter in BUSY", true, "BUSY", Tool{Reports: true}, WhileBusy},
		{"reporter in MANIPULATING", true, "MANIPULATING", Tool{Reports: true}, WhileBusy},
		{"reporter in ATTENTION", true, "ATTENTION", Tool{Reports: true}, WhileBusy},
		{"reporter in ERROR", true, "ERROR", Tool{Reports: true}, WhileBusy},
		{"reporter in UNKNOWN", true, "UNKNOWN", Tool{Reports: true}, WhileBusy},
		{"reporter in no state", true, "", Tool{Reports: true}, WhileBusy},
		{"reporter in SOMETHING_NEW", true, "SOMETHING_NEW", Tool{Reports: true}, WhileBusy},
		{"non-reporter offline", false, "", notReporting(), Allowed},
		{"non-reporter in IDLE", true, "IDLE", notReporting(), Allowed},
		{"non-reporter in READY", true, "READY", notReporting(), Allowed},
		{"non-reporter in FINISHED", true, "FINISHED", notReporting(), Allowed},
		{"non-reporter in STOPPED", true, "STOPPED", notReporting(), Allowed},
		{"non-reporter in BUSY", true, "BUSY", notReporting(), Allowed},
		{"non-reporter in MANIPULATING", true, "MANIPULATING", notReporting(), Allowed},
		{"non-reporter in PRINTING", true, "PRINTING", notReporting(), WhilePrinting},
		{"non-reporter in PAUSED", true, "PAUSED", notReporting(), WhilePrinting},
		{"non-reporter in ATTENTION", true, "ATTENTION", notReporting(), WhileBusy},
		{"non-reporter in ERROR", true, "ERROR", notReporting(), WhileBusy},
		{"non-reporter in UNKNOWN", true, "UNKNOWN", notReporting(), WhileBusy},
		{"non-reporter in no state", true, "", notReporting(), WhileBusy},
		{"non-reporter in SOMETHING_NEW", true, "SOMETHING_NEW", notReporting(), WhileBusy},
		// Blocks removing the assignment while the printer is printing
		{"printing", true, "PRINTING", reported("PETG"), WhilePrinting},
		// Blocks removing the assignment while the printer is changing filament
		{"changing filament", true, "BUSY", reported("PETG"), WhileBusy},
		// Allows the unset of an MMU slot that still reports its material type
		{"slot still reports", true, "IDLE", reported("PETG"), Allowed},
		// Allows clearing an offline printer
		{"offline with a material reported", false, "", reported("PETG"), Allowed},
		// The design: a spool read off its NFC tag may still be unassigned.
		{"NFC", true, "IDLE", Tool{Reports: true, Reported: new("PETG"), FromNFC: true}, Allowed},
	} {
		if got := MayUnassign(StateOf(c.connected, c.state), c.tool); got != c.want {
			t.Errorf("%s: MayUnassign = %q, want %q", c.name, got, c.want)
		}
	}
}

func TestACustomNameIsNoKnownType(t *testing.T) {
	for _, name := range []string{"CUSTOM1", "MYFIL", "PLA-X"} {
		if !isCustom(name) {
			t.Errorf("%q is a custom name", name)
		}
	}

	for _, name := range []string{"PLA", "PETG", "PA12", "PEEK", "FLEX", "PCCF", "petg"} {
		if isCustom(name) {
			t.Errorf("%q is a known type", name)
		}
	}
}

func TestMatches(t *testing.T) {
	for _, c := range []struct {
		reported  string
		candidate Candidate
		want      bool
	}{
		{"PLA", material("PLA"), true},
		{"PLA", Candidate{Abbreviation: "PLA+", Type: "PLA"}, true},
		{"PLA", material("WOOD"), true},
		{"WOOD", material("PLA"), true},
		{"PC", material("PCCF"), true},
		{"FLEX", material("TPE"), true},
		{"TPU", material("TPE"), true},
		{"PLA", material("TPU"), false},
		{"petg", material("PETG"), true},
		{"PLA", material("Wood"), true},
		{"PETG", Candidate{}, false},
		{"", Candidate{}, false},
	} {
		if got := Matches(c.reported, c.candidate); got != c.want {
			t.Errorf("Matches(%q, %+v) = %v, want %v", c.reported, c.candidate, got, c.want)
		}
	}
}

func TestToolOf(t *testing.T) {
	pla := "PLA"
	cases := []struct {
		name string
		tool connectorapi.Tool
		want Tool
	}{
		{"no material member", connectorapi.Tool{}, Tool{}},
		{"a profile", connectorapi.Tool{Material: &pla, Reported: &connectorapi.ReportedMaterial{Type: "PLA"}},
			Tool{Reports: true, Reported: &pla}},
		{"a tag", connectorapi.Tool{Material: &pla, Reported: &connectorapi.ReportedMaterial{InstanceUUID: "u"}},
			Tool{Reports: true, Reported: &pla, FromNFC: true}},
	}

	for _, c := range cases {
		got := ToolOf(c.tool)
		if got.Reports != c.want.Reports || got.FromNFC != c.want.FromNFC || got.Reported != c.want.Reported {
			t.Errorf("%s: %+v", c.name, got)
		}
	}
}
