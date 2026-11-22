// The codes this module refuses a caller with.
//
// A refusal a person or a client can see is a code of this table, never a bare
// English sentence. A client branches on the code; the title is English for a
// person and never varies, and the detail says what went wrong this time. The
// shape is Materials' own, so a caller of one ports to the other.
//
// Add a code here and to the Refusal enum of openapi.json; a test holds the two
// together.
package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

// Code is one error this module answers with.
type Code struct {
	// Name is the identifier, such as spool-sync/NOT_FOUND.
	Name string
	// Status is the HTTP status this refusal answers with.
	Status int
	// Title is the English phrase for this code. It never varies.
	Title string
	// Retryable says whether sending the same request again could work.
	Retryable bool
}

// The codes this module answers with.
//
//nolint:gochecknoglobals // the code table; a const cannot hold a struct.
var (
	// InvalidQuery reports a query parameter or a body this module cannot read.
	InvalidQuery = Code{Name: "spool-sync/INVALID_QUERY", Status: http.StatusBadRequest, Title: "the request could not be read"}
	// NotFound reports a spool, a material, a printer or a tool the caller cannot see.
	NotFound = Code{Name: "spool-sync/NOT_FOUND", Status: http.StatusNotFound, Title: "there is nothing here with that id"}
	// MaterialInvalid reports a custom material without a name or a known class, or a text over its bound.
	MaterialInvalid = Code{Name: "spool-sync/MATERIAL_INVALID", Status: http.StatusUnprocessableEntity, Title: "the material was not accepted"}
	// MaterialInUse reports a delete of a custom material a spool is of.
	MaterialInUse = Code{Name: "spool-sync/MATERIAL_IN_USE", Status: http.StatusConflict, Title: "a spool in stock is still of this material"}
	// SpoolInvalid reports a spool with a weight or a quantity it cannot have.
	SpoolInvalid = Code{Name: "spool-sync/SPOOL_INVALID", Status: http.StatusUnprocessableEntity, Title: "the spool was not accepted"}
	// LocationInvalid reports a location name that is empty, more than one line or too long.
	LocationInvalid = Code{Name: "spool-sync/LOCATION_INVALID", Status: http.StatusUnprocessableEntity, Title: "the location was not accepted"}
	// LocationTaken reports a rename to a name another location has.
	LocationTaken = Code{Name: "spool-sync/LOCATION_TAKEN", Status: http.StatusConflict, Title: "another location already has this name"}
	// LocationsFull reports a new location past the most the module keeps.
	LocationsFull = Code{Name: "spool-sync/LOCATIONS_FULL", Status: http.StatusConflict, Title: "the module already keeps as many locations as it may"}
	// ShelfFull reports an add that would pass max_spools.
	ShelfFull = Code{Name: "spool-sync/SHELF_FULL", Status: http.StatusConflict, Title: "the module already holds as many spools as it may"}
	// ImportRunning reports a catalogue import asked for while one runs.
	ImportRunning = Code{Name: "spool-sync/IMPORT_RUNNING", Status: http.StatusConflict, Title: "the catalogue is already being downloaded"}
	// NoPrincipal reports a request the host attributed to nobody.
	NoPrincipal = Code{Name: "spool-sync/NO_PRINCIPAL", Status: http.StatusForbidden, Title: "the request carries no signed-in person"}
	// NotOperator reports a change to a printer tool by a person who may not operate that printer.
	NotOperator = Code{Name: "spool-sync/NOT_OPERATOR", Status: http.StatusForbidden, Title: "you may not operate this printer"}
	// AccessUnknown reports a change when the host gave no answer on whether the person may operate the printer.
	AccessUnknown = Code{Name: "spool-sync/ACCESS_UNKNOWN", Status: http.StatusServiceUnavailable, Title: "the server could not say whether you may operate this printer", Retryable: true}
	// ToolRefused reports a change the rules refuse in the printer's state or for what it reports.
	ToolRefused = Code{Name: "spool-sync/TOOL_REFUSED", Status: http.StatusConflict, Title: "the tool cannot be changed now"}
	// NotReported reports an NFC add on a tool that reads no tag.
	NotReported = Code{Name: "spool-sync/NOT_REPORTED", Status: http.StatusUnprocessableEntity, Title: "the printer reports no NFC tag on this tool"}
	// WeightNeeded reports a tag that holds no full weight added without one: the person is asked for the spool's weight.
	WeightNeeded = Code{Name: "spool-sync/WEIGHT_NEEDED", Status: http.StatusUnprocessableEntity, Title: "the tag holds no weight; give the spool's"}
	// MergeRefused reports a merge that is not a manual spool into a printer spool of the same material.
	MergeRefused = Code{Name: "spool-sync/MERGE_REFUSED", Status: http.StatusConflict, Title: "these two spools cannot be merged"}
	// SpoolAssigned reports a change to a spool that is on a printer tool, which only unassigning allows.
	SpoolAssigned = Code{Name: "spool-sync/SPOOL_ASSIGNED", Status: http.StatusConflict, Title: "the spool is on a printer tool"}
	// SpoolArchived reports a spool that is archived, which cannot go on a tool.
	SpoolArchived = Code{Name: "spool-sync/SPOOL_ARCHIVED", Status: http.StatusConflict, Title: "the spool is archived"}
	// SpoolOnAnotherTool reports a spool that is already on another tool.
	SpoolOnAnotherTool = Code{Name: "spool-sync/SPOOL_ON_ANOTHER_TOOL", Status: http.StatusConflict, Title: "the spool is on another printer tool"}
	// StoreFailed reports a failure of the module's own database. The detail is fixed: the cause goes to the log.
	StoreFailed = Code{Name: "spool-sync/STORE_FAILED", Status: http.StatusInternalServerError, Title: "the module could not read or write its database", Retryable: true}
	// SpoolSyncUnavailable reports SpoolSync that could not be reached or failed inside. Added to Materials' codes.
	SpoolSyncUnavailable = Code{Name: "spool-sync/SPOOLSYNC_UNAVAILABLE", Status: http.StatusBadGateway, Title: "SpoolSync could not be reached", Retryable: true}
	// SpoolSyncRefused reports SpoolSync that answered a request with a refusal the module passes on. Added to Materials' codes.
	SpoolSyncRefused = Code{Name: "spool-sync/SPOOLSYNC_REFUSED", Status: http.StatusBadGateway, Title: "SpoolSync refused the request"}
	// NotConfigured reports a request before the module has its settings and database. Added to Materials' codes.
	NotConfigured = Code{Name: "spool-sync/NOT_CONFIGURED", Status: http.StatusServiceUnavailable, Title: "the module is not configured yet", Retryable: true}
)

// problem is the RFC 9457 document a refusal is answered with.
type problem struct {
	Type      string         `json:"type"`
	Status    int            `json:"status"`
	Code      string         `json:"code"`
	Title     string         `json:"title"`
	Detail    string         `json:"detail"`
	Params    map[string]any `json:"params"`
	Retryable bool           `json:"retryable"`
}

// document builds the problem for a code.
func document(code Code, detail string, params map[string]any) problem {
	if params == nil {
		params = map[string]any{}
	}

	return problem{
		Type: "about:blank", Status: code.Status, Code: code.Name, Title: code.Title,
		Detail: detail, Params: params, Retryable: code.Retryable,
	}
}

// refuse writes one refusal as problem+json.
func refuse(w http.ResponseWriter, code Code, detail string, params map[string]any) {
	if detail == "" {
		detail = code.Title
	}

	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(code.Status)

	_ = json.NewEncoder(w).Encode(document(code, detail, params))
}

// refusef is refuse with a formatted detail.
func refusef(w http.ResponseWriter, code Code, params map[string]any, format string, args ...any) {
	refuse(w, code, fmt.Sprintf(format, args...), params)
}

// allCodes is every code above, for the tests that hold the table to openapi.json.
//
//nolint:gochecknoglobals // the list the tests read; read-only.
var allCodes = []Code{
	InvalidQuery, NotFound, MaterialInvalid, MaterialInUse, SpoolInvalid, LocationInvalid, LocationTaken,
	LocationsFull, ShelfFull, ImportRunning, NoPrincipal, NotOperator, AccessUnknown, ToolRefused,
	NotReported, WeightNeeded, MergeRefused, SpoolAssigned, SpoolArchived, SpoolOnAnotherTool, StoreFailed,
	SpoolSyncUnavailable, SpoolSyncRefused, NotConfigured,
}
