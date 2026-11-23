// Copied from extensions/materials/internal/assign/rules.go in the prusa-connect
// repository, by Prusa Research a.s., licensed under
// OCL v1.1 + Micro v1 + NoSaaS v1 + SWAtt v1 (https://github.com/prusa3d/prusa-connect), as this module is.
// Unchanged apart from this note.
// Package assign holds the rules for when a person may change the material
// on a printer tool.
package assign

import (
	"slices"
	"strings"

	"github.com/prusa3d/prusa-connect/sdk/connectorapi"
)

// State is a printer state as the rules group it.
type State int

// The groups of printer states.
const (
	// Offline is a printer that is not connected. Its report may be old, so
	// a change goes through.
	Offline State = iota
	Printing
	// Busy is a printer that may change its filament by itself.
	Busy
	// Standby is a printer that is idle or done with a job.
	Standby
	// Other is a state the rules do not trust, such as ATTENTION or ERROR.
	Other
)

// StateOf groups a printer's state for the rules.
func StateOf(connected bool, state string) State {
	if !connected {
		return Offline
	}

	switch state {
	case "PRINTING", "PAUSED":
		return Printing
	case "BUSY", "MANIPULATING":
		return Busy
	case "IDLE", "READY", "FINISHED", "STOPPED":
		return Standby
	}

	return Other
}

// Reason says why a change is refused. Allowed is no refusal.
type Reason string

// The reasons.
const (
	Allowed       Reason = ""
	WhilePrinting Reason = "printing"
	// WhileBusy also covers a state the rules do not trust.
	WhileBusy Reason = "busy"
	// ReportedLoaded is a tool that reports a material the candidate does not match.
	ReportedLoaded Reason = "reported-loaded"
	// ReportedEmpty is a reporting tool with nothing loaded.
	ReportedEmpty Reason = "reported-empty"
	// ReadOffTag is a tool whose spool the printer read off its NFC tag.
	ReadOffTag Reason = "nfc"
)

// Tool is what the rules need to know of one printer tool.
type Tool struct {
	// Reports is true when the printer reports the material of this tool.
	Reports bool
	// Reported is the abbreviation the printer reports. Nil is nothing
	// reported, and an empty string is a tool reported empty.
	Reported *string
	// FromNFC is true when the printer read the tool's spool off its NFC tag.
	FromNFC bool
}

// ToolOf reads what the rules need out of one tool of a printer's tree. A
// tool reports its material when the tree carries the material member, and
// its spool came off an NFC tag when the report carries an instance uuid.
func ToolOf(t connectorapi.Tool) Tool {
	return Tool{
		Reports:  t.Material != nil,
		Reported: t.Material,
		FromNFC:  t.Reported != nil && t.Reported.InstanceUUID != "",
	}
}

// Candidate is the material a person wants to assign.
type Candidate struct {
	Abbreviation, Type string
}

// aliases are groups of materials a printer reports by one name: it
// reports PLA for WOOD and PC for PCCF.
//
//nolint:gochecknoglobals // a closed table read at run time, never written.
var aliases = [][]string{
	{"PC", "PCCF"},
	{"PLA", "WOOD"},
	{"FLEX", "TPU", "TPE"},
}

// knownTypes are the FFF material types of the architecture spec, with the
// names in aliases. Anything else a printer reports is a name a person gave
// the filament.
//
//nolint:gochecknoglobals // a closed table read at run time, never written.
var knownTypes = strings.Fields("PLA PETG TPU ABS ASA PC PCTG PP PA6 PA11 PA12 PA612 PA66 CPE TPE HIPS " +
	"PHA PET PEI PBT PVB PVA PEKK PEEK BVOH TPC PPS PPSU PVC PEBA PVDF PPA " +
	"PCL PES PMMA POM PPE PS PSU TPI SBS OBC EVA PCCF WOOD FLEX")

// isCustom is true for a reported name that is no known type. Case does not
// count here: "petg" is PETG.
func isCustom(reported string) bool {
	return !slices.Contains(knownTypes, strings.ToUpper(reported))
}

// same is true for a reported name and a requested one that are equal or in
// one alias group. Case does not count: the catalogue's
// Woodfill is "Wood" for the alias WOOD. An empty requested name matches
// nothing.
func same(reported, requested string) bool {
	if requested == "" {
		return false
	}

	reported, requested = strings.ToUpper(reported), strings.ToUpper(requested)
	if reported == requested {
		return true
	}

	return slices.ContainsFunc(aliases, func(group []string) bool {
		return slices.Contains(group, reported) && slices.Contains(group, requested)
	})
}

// Matches is true when the reported name is the candidate's abbreviation or
// its type. A PLA+ spool of type PLA matches a reported PLA.
func Matches(reported string, c Candidate) bool {
	return same(reported, c.Abbreviation) || same(reported, c.Type)
}

// refusedState is the refusal the printer's state alone gives, for an assign
// and an unassign alike.
func refusedState(s State, t Tool) Reason {
	switch {
	case s == Printing:
		return WhilePrinting
	case s == Offline, s == Standby, s == Busy && !t.Reports:
		return Allowed
	}

	return WhileBusy
}

// MayAssign says whether a person may put candidate on the tool.
func MayAssign(s State, t Tool, c Candidate) Reason {
	if r := refusedState(s, t); r != Allowed {
		return r
	}

	// A material read off an NFC tag is not changed by hand.
	if t.FromNFC {
		return ReadOffTag
	}

	if s == Offline || !t.Reports {
		return Allowed
	}

	if t.Reported == nil || *t.Reported == "" {
		return ReportedEmpty
	}

	if isCustom(*t.Reported) || Matches(*t.Reported, c) {
		return Allowed
	}

	return ReportedLoaded
}

// MayUnassign says whether a person may take the material off the tool. On a
// reporting printer the reported material stays.
func MayUnassign(s State, t Tool) Reason {
	return refusedState(s, t)
}
