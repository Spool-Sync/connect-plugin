package main

import (
	"database/sql"
	"log/slog"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/spool-sync/connect-plugin/internal/local"
)

// standaloneRigs makes newRig keep the inventory in the module's own database.
//
//nolint:gochecknoglobals // a test switch, set only by TestStandalone, which runs its cases one by one.
var standaloneRigs bool

// seededLocal is a standalone inventory holding the fake SpoolSync's one filament type.
func seededLocal(t *testing.T, db *sql.DB, log *slog.Logger) *local.Inventory {
	t.Helper()

	if _, err := db.ExecContext(t.Context(), `INSERT INTO filament_types (id, opt_material_uuid, brand, material, name,
		color_hex, diameter_mm, spool_weight_g, custom, created_at, updated_at)
		VALUES ('ft1', 'opt-pla', 'Prusament', 'PLA', 'Prusament PLA', '#3d3e3d', 1.75, 200, 0,
		'2026-11-21T10:00:00.000000000Z', '2026-11-21T10:00:00.000000000Z')`); err != nil {
		t.Fatal(err)
	}

	return local.New(db, wirerpc.DatabaseSQLite, log)
}

// TestStandalone runs the route cases that do not reach into the fake SpoolSync
// again, with the inventory in the module's own database: a standalone module
// answers what one on SpoolSync answers.
func TestStandalone(t *testing.T) {
	cases := map[string]func(*testing.T){
		"the rules refuse a change while printing": TestTheRulesRefuseAChangeWhilePrinting,
		"locations":                        TestLocationsRules,
		"favourites and list filters":      TestFavouritesAndListFilters,
		"add reported needs a tag":         TestAddReportedNeedsATag,
		"list printers":                    TestListPrintersAnswersWhatThePersonMayObserve,
		"no person":                        TestARequestWithNoPersonIsRefused,
		"printer tools":                    TestPrinterToolsSayWhetherTheCallerMayOperate,
		"add reported asks for the weight": TestAddReportedAsksForTheWeightATagLacks,
		"the overview":                     TestTheOverviewTotalsTheStockByMaterialAndType,
		"the jobs":                         TestTheJobsAnswerTheFilamentEachPrintTook,
		"spools round trip":                testSpoolsRoundTrip,
	}

	standaloneRigs = true
	t.Cleanup(func() { standaloneRigs = false })

	for name, fn := range cases {
		t.Run(name, fn)
	}
}

// testSpoolsRoundTrip adds, edits, archives and deletes spools through the routes.
func testSpoolsRoundTrip(t *testing.T) {
	r := newRig(t, printerWith("IDLE", nil))

	if rec := r.call(t, "POST", "/spools", `{"material_uuid":"ft1","remaining_weight_g":800,"quantity":2}`, "user:alpha"); rec.Code != 201 {
		t.Fatalf("adding answered %d: %s", rec.Code, rec.Body.String())
	}

	spools := decode(t, r.call(t, "GET", "/spools", "", "user:alpha"))["spools"].([]any)
	if len(spools) != 2 {
		t.Fatalf("the list holds %d spools, want 2", len(spools))
	}

	first := spools[0].(map[string]any)
	if first["remaining_weight_g"] != 800.0 {
		t.Fatalf("a new spool holds %v g, want 800", first["remaining_weight_g"])
	}

	id := first["id"].(string)

	if rec := r.call(t, "PATCH", "/spools/"+id, `{"remaining_weight_g":500}`, "user:alpha"); rec.Code != 200 ||
		decode(t, rec)["remaining_weight_g"] != 500.0 {
		t.Fatalf("editing answered %d: %s", rec.Code, rec.Body.String())
	}

	if rec := r.call(t, "PATCH", "/spools/"+id, `{"archived":true,"archive_reason":"consumed"}`, "user:alpha"); rec.Code != 200 ||
		decode(t, rec)["archived_at"] == nil {
		t.Fatalf("archiving answered %d: %s", rec.Code, rec.Body.String())
	}

	if o := decode(t, r.call(t, "GET", "/overview", "", "user:alpha")); o["spools"] != 1.0 {
		t.Fatalf("the overview counts %v spools in stock after one was archived, want 1", o["spools"])
	}

	if rec := r.call(t, "DELETE", "/spools/"+id, "", "user:alpha"); rec.Code != 204 {
		t.Fatalf("deleting answered %d: %s", rec.Code, rec.Body.String())
	}

	if rec := r.call(t, "GET", "/status", "", "user:alpha"); decode(t, rec)["standalone"] != true {
		t.Fatalf("the status does not say standalone: %s", rec.Body.String())
	}
}
