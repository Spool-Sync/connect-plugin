package store

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// postgresDSN names a Postgres server the tests may create databases on. The
// Postgres tests are skipped without it.
const postgresDSN = "SPOOLSYNC_TEST_POSTGRES_DSN"

// engines are the engines every store test runs on.
var engines = []wirerpc.DatabaseEngine{wirerpc.DatabaseSQLite, wirerpc.DatabasePostgres}

// grantFor makes a fresh, empty database of one engine: a SQLite file in the
// test's directory, or a new Postgres database on the server the environment
// names, dropped when the test ends.
func grantFor(t *testing.T, engine wirerpc.DatabaseEngine) wirerpc.DatabaseGrant {
	t.Helper()

	if engine == wirerpc.DatabaseSQLite {
		return wirerpc.DatabaseGrant{Engine: engine, DSN: filepath.Join(t.TempDir(), "module.db")}
	}

	admin := os.Getenv(postgresDSN)
	if admin == "" {
		t.Skipf("%s is empty", postgresDSN)
	}

	u, err := url.Parse(admin)
	if err != nil {
		t.Fatal(err)
	}

	name := fmt.Sprintf("store_test_%d", time.Now().UnixNano())

	server, err := sql.Open("postgres", admin)
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = server.Close() }()

	if _, err = server.ExecContext(t.Context(), "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create %s: %v", name, err)
	}

	t.Cleanup(func() {
		drop, err := sql.Open("postgres", admin)
		if err != nil {
			return
		}

		defer func() { _ = drop.Close() }()

		_, _ = drop.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
	})

	u.Path = "/" + name

	return wirerpc.DatabaseGrant{Engine: engine, DSN: u.String()}
}

// open migrates a fresh database of one engine.
func open(t *testing.T, engine wirerpc.DatabaseEngine) (*Store, *sql.DB) {
	t.Helper()

	db, _, err := Open(t.Context(), grantFor(t, engine), os.DirFS("../.."))
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

	return New(db, engine), db
}

// each runs one test body on every engine.
func each(t *testing.T, body func(t *testing.T, s *Store)) {
	t.Helper()

	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			s, _ := open(t, engine)
			body(t, s)
		})
	}
}

func TestAssignmentRoundTripReplaceAndDelete(t *testing.T) {
	each(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		if _, ok, err := s.Assignment(ctx, "p1", "0"); err != nil || ok {
			t.Fatalf("an empty store answered %v %v", ok, err)
		}

		at := time.Date(2026, 11, 20, 9, 30, 0, 123456000, time.UTC)
		if err := s.SetAssignment(ctx, Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedAt: at, AssignedBy: "user:alpha"}); err != nil {
			t.Fatal(err)
		}

		got, ok, err := s.Assignment(ctx, "p1", "0")
		if err != nil || !ok {
			t.Fatalf("read: %v %v", ok, err)
		}

		if got.SpoolID != "s1" || got.AssignedBy != "user:alpha" || !got.AssignedAt.Equal(at) {
			t.Fatalf("read %+v, want the spool, the person and %v", got, at)
		}

		// A second assignment to the same tool replaces the first.
		if err = s.SetAssignment(ctx, Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s2", AssignedAt: at, AssignedBy: "user:beta"}); err != nil {
			t.Fatal(err)
		}

		if err = s.SetAssignment(ctx, Assignment{PrinterID: "p1", Tool: "0.3", SpoolID: "s3", AssignedAt: at, AssignedBy: "user:beta"}); err != nil {
			t.Fatal(err)
		}

		all, err := s.Assignments(ctx)
		if err != nil || len(all) != 2 || all[0].SpoolID != "s2" || all[1].Tool != "0.3" {
			t.Fatalf("list %v %+v", err, all)
		}

		if ok, err = s.DeleteAssignment(ctx, "p1", "0"); err != nil || !ok {
			t.Fatalf("delete: %v %v", ok, err)
		}

		if ok, err = s.DeleteAssignment(ctx, "p1", "0"); err != nil || ok {
			t.Fatalf("a second delete answered %v %v, want false", ok, err)
		}
	})
}

func TestOdometerKeepsTheReadingAndItsWeight(t *testing.T) {
	each(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		id := int64(7)
		weight := 12.5

		if err := s.SetOdometer(ctx, "p1", "0", Odometer{ID: &id, E: 1500.25, Weight: &weight}, time.Now()); err != nil {
			t.Fatal(err)
		}

		if err := s.SetOdometer(ctx, "p1", "1", Odometer{E: 40}, time.Now()); err != nil {
			t.Fatal(err)
		}

		o, ok, err := s.Odometer(ctx, "p1", "0")
		if err != nil || !ok || o.ID == nil || *o.ID != 7 || o.E != 1500.25 || o.Weight == nil || *o.Weight != 12.5 {
			t.Fatalf("read %+v %v %v", o, ok, err)
		}

		bare, ok, err := s.Odometer(ctx, "p1", "1")
		if err != nil || !ok || bare.ID != nil || bare.Weight != nil || bare.E != 40 {
			t.Fatalf("a reading with no id or weight read %+v %v %v", bare, ok, err)
		}

		if err = s.DeleteOdometer(ctx, "p1", "0"); err != nil {
			t.Fatal(err)
		}

		if _, ok, _ = s.Odometer(ctx, "p1", "0"); ok {
			t.Fatal("a deleted reading is still there")
		}
	})
}

func TestPendingAddsTakesAndClears(t *testing.T) {
	each(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		for _, g := range []float64{0.4, 0.7, 2.25} {
			if err := s.AddPending(ctx, "s1", g); err != nil {
				t.Fatal(err)
			}
		}

		if err := s.AddPending(ctx, "s2", 0.5); err != nil {
			t.Fatal(err)
		}

		if g, _ := s.Pending(ctx, "s1"); g < 3.349 || g > 3.351 {
			t.Fatalf("pending s1 is %v, want 3.35", g)
		}

		if g, _ := s.Pending(ctx, "none"); g != 0 {
			t.Fatalf("pending of a spool with none is %v", g)
		}

		if err := s.TakePending(ctx, "s1", 3); err != nil {
			t.Fatal(err)
		}

		if g, _ := s.Pending(ctx, "s1"); g < 0.349 || g > 0.351 {
			t.Fatalf("after taking 3 g, pending s1 is %v, want 0.35", g)
		}

		spools, err := s.PendingSpools(ctx)
		if err != nil || len(spools) != 2 {
			t.Fatalf("pending spools %v %v", spools, err)
		}

		// Taking everything drops the row, so a spool with nothing counted is not listed.
		if err = s.TakePending(ctx, "s2", 0.5); err != nil {
			t.Fatal(err)
		}

		if spools, _ = s.PendingSpools(ctx); len(spools) != 1 {
			t.Fatalf("a spool with nothing left is still listed: %v", spools)
		}

		if err = s.ClearPending(ctx, "s1"); err != nil {
			t.Fatal(err)
		}

		if spools, _ = s.PendingSpools(ctx); len(spools) != 0 {
			t.Fatalf("after clearing, pending spools are %v", spools)
		}
	})
}

func TestForgetPrinterTakesItsStateAway(t *testing.T) {
	each(t, func(t *testing.T, s *Store) {
		ctx := context.Background()
		now := time.Now()

		_ = s.SetAssignment(ctx, Assignment{PrinterID: "p1", Tool: "0", SpoolID: "s1", AssignedAt: now, AssignedBy: "user:a"})
		_ = s.SetAssignment(ctx, Assignment{PrinterID: "p2", Tool: "0", SpoolID: "s2", AssignedAt: now, AssignedBy: "user:a"})
		_ = s.SetOdometer(ctx, "p3", "0", Odometer{E: 1}, now)

		printers, err := s.Printers(ctx)
		if err != nil || len(printers) != 3 {
			t.Fatalf("printers with state %v %v", printers, err)
		}

		if err = s.ForgetPrinter(ctx, "p1"); err != nil {
			t.Fatal(err)
		}

		if _, ok, _ := s.Assignment(ctx, "p1", "0"); ok {
			t.Fatal("a forgotten printer kept its assignment")
		}

		if _, ok, _ := s.Assignment(ctx, "p2", "0"); !ok {
			t.Fatal("another printer lost its assignment")
		}
	})
}

// What is written survives the database being closed and opened again.
func TestStateSurvivesARestart(t *testing.T) {
	for _, engine := range engines {
		t.Run(string(engine), func(t *testing.T) {
			grant := grantFor(t, engine)
			ctx := context.Background()

			db, _, err := Open(t.Context(), grant, os.DirFS("../.."))
			if err != nil {
				t.Fatal(err)
			}

			if err = New(db, engine).AddPending(ctx, "s1", 4.5); err != nil {
				t.Fatal(err)
			}

			_ = db.Close()

			db, _, err = Open(t.Context(), grant, os.DirFS("../.."))
			if err != nil {
				t.Fatal(err)
			}

			defer func() { _ = db.Close() }()

			if g, _ := New(db, engine).Pending(ctx, "s1"); g != 4.5 {
				t.Fatalf("after a restart pending is %v, want 4.5", g)
			}
		})
	}
}

func TestFavouritesAreThePersonsOwnAndListFiltersAreKept(t *testing.T) {
	each(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		for _, id := range []string{"ft2", "ft1"} {
			if err := s.SetFavourite(ctx, "user:alpha", id, true); err != nil {
				t.Fatal(err)
			}
		}

		// Starring twice changes nothing.
		if err := s.SetFavourite(ctx, "user:alpha", "ft1", true); err != nil {
			t.Fatal(err)
		}

		if got, _ := s.Favourites(ctx, "user:alpha"); len(got) != 2 || got[0] != "ft1" {
			t.Fatalf("favourites %v, want ft1 and ft2 sorted", got)
		}

		if got, _ := s.Favourites(ctx, "user:beta"); len(got) != 0 {
			t.Fatalf("another person sees %v", got)
		}

		if err := s.SetFavourite(ctx, "user:alpha", "ft1", false); err != nil {
			t.Fatal(err)
		}

		if got, _ := s.Favourites(ctx, "user:alpha"); len(got) != 1 {
			t.Fatalf("after unstar, favourites %v", got)
		}

		if _, ok, _ := s.ListFilters(ctx, "user:alpha"); ok {
			t.Fatal("filters answered before any were kept")
		}

		if err := s.SetListFilters(ctx, "user:alpha", `{"q":"a"}`); err != nil {
			t.Fatal(err)
		}

		if err := s.SetListFilters(ctx, "user:alpha", `{"q":"b"}`); err != nil {
			t.Fatal(err)
		}

		if got, ok, _ := s.ListFilters(ctx, "user:alpha"); !ok || got != `{"q":"b"}` {
			t.Fatalf("filters %q, want the last kept", got)
		}
	})
}

func TestAMaterialAloneHasNoSpool(t *testing.T) {
	each(t, func(t *testing.T, s *Store) {
		ctx := context.Background()

		if err := s.SetAssignment(ctx, Assignment{PrinterID: "p1", Tool: "2", FilamentTypeID: "ft1", AssignedAt: time.Now(), AssignedBy: "user:a"}); err != nil {
			t.Fatal(err)
		}

		got, ok, err := s.Assignment(ctx, "p1", "2")
		if err != nil || !ok || got.FilamentTypeID != "ft1" || got.SpoolID != "" {
			t.Fatalf("read %+v %v %v", got, ok, err)
		}
	})
}
