package local

import (
	"archive/zip"
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"spool-sync.com/connect/internal/catalogue"
	"spool-sync.com/connect/internal/spoolsync"
	"spool-sync.com/connect/internal/store"
)

// postgresDSN names a Postgres server the tests may create databases on, as the
// store's tests read it. The Postgres cases are skipped without it.
const postgresDSN = "SPOOLSYNC_TEST_POSTGRES_DSN"

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

	name := fmt.Sprintf("local_test_%d", time.Now().UnixNano())

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
		if err == nil {
			_, _ = drop.ExecContext(context.Background(), "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = drop.Close()
		}
	})

	u.Path = "/" + name

	return wirerpc.DatabaseGrant{Engine: engine, DSN: u.String()}
}

// each runs one body on a fresh inventory of every engine.
func each(t *testing.T, body func(t *testing.T, v *Inventory)) {
	t.Helper()

	for _, engine := range []wirerpc.DatabaseEngine{wirerpc.DatabaseSQLite, wirerpc.DatabasePostgres} {
		t.Run(string(engine), func(t *testing.T) {
			db, _, err := store.Open(t.Context(), grantFor(t, engine), os.DirFS("../.."))
			if err != nil {
				t.Fatalf("open: %v", err)
			}

			t.Cleanup(func() { _ = db.Close() })

			body(t, New(db, engine, slog.New(slog.NewTextHandler(io.Discard, nil))))
		})
	}
}

func ptr[T any](v T) *T { return &v }

func TestSpoolsKeepTheGrossWeightAndAnswerTheFilamentLeft(t *testing.T) {
	each(t, func(t *testing.T, v *Inventory) {
		ctx := t.Context()

		ft, err := v.CreateFilamentType(ctx, spoolsync.NewFilamentType{Brand: "Acme", Material: "PETG", Name: "Blue", SpoolWeightG: 250})
		if err != nil {
			t.Fatal(err)
		}

		if !ft.Custom || ft.SpoolWeightG != 250 || ft.DiameterMM != 1.75 {
			t.Fatalf("the new type is %+v", ft)
		}

		loc, err := v.CreateLocation(ctx, "Shelf")
		if err != nil {
			t.Fatal(err)
		}

		sp, err := v.Create(ctx, spoolsync.NewSpool{FilamentTypeID: ft.ID, InitialWeightG: 1250, LocationID: loc.ID, OptInstanceUUID: "ABC"})
		if err != nil {
			t.Fatal(err)
		}

		if sp.RemainingG != 1000 || sp.InitialG != 1000 || sp.Source != "printer" || sp.Location == nil || *sp.Location != "Shelf" {
			t.Fatalf("the new spool is %+v", sp)
		}

		if _, err = v.Create(ctx, spoolsync.NewSpool{FilamentTypeID: ft.ID, InitialWeightG: 1250, OptInstanceUUID: "abc"}); err == nil {
			t.Fatal("a second spool with the same tag was added")
		}

		if err = v.SetWeight(ctx, sp.ID, 650, "prusa-connect"); err != nil {
			t.Fatal(err)
		}

		if got, _ := v.FindByInstanceUUID(ctx, "abc"); got.RemainingG != 400 {
			t.Fatalf("after a reading of 650 g the spool holds %v g, want 400", got.RemainingG)
		}

		if got, _ := v.Update(ctx, sp.ID, spoolsync.SpoolUpdate{Archive: ptr(true), ArchiveReason: "consumed"}); got.ArchivedAt == nil {
			t.Fatal("archiving left the spool in stock")
		}

		if err = v.DeleteFilamentType(ctx, ft.ID); err == nil {
			t.Fatal("a type with a spool was deleted")
		}

		if err = v.DeleteLocation(ctx, loc.ID); err != nil {
			t.Fatal(err)
		}

		if got, _ := v.Get(ctx, sp.ID); got.LocationID != nil {
			t.Fatal("a deleted location is still the spool's")
		}

		if err = v.DeleteSpool(ctx, sp.ID); err != nil {
			t.Fatal(err)
		}

		if _, err = v.Get(ctx, sp.ID); !errors.Is(err, spoolsync.ErrNotFound) {
			t.Fatalf("a deleted spool answers %v", err)
		}
	})
}

func TestTheCatalogueTurnsATagsStandInIntoItsMaterial(t *testing.T) {
	each(t, func(t *testing.T, v *Inventory) {
		ctx := t.Context()
		uuid := "8f0e1a7c-3b9d-4c2e-9f61-2d5a7b8c9e10"

		stand, err := v.CreateFilamentType(ctx, spoolsync.NewFilamentType{OptMaterialUUID: &uuid, Brand: "?", Material: "PLA", Name: "PLA"})
		if err != nil {
			t.Fatal(err)
		}

		imported, updated, err := v.Upsert(ctx, []catalogue.Material{
			{OptMaterialUUID: uuid, Brand: "Prusament", Name: "PLA Galaxy Black", Material: "PLA", DiameterMM: 1.75, NetWeightG: ptr(1000.0)},
			{OptMaterialUUID: "0a1b2c3d-0000-4000-8000-000000000001", Brand: "Prusament", Name: "PETG Orange", Material: "PETG", DiameterMM: 1.75, SpoolWeightG: ptr(193.0)},
		})
		if err != nil {
			t.Fatal(err)
		}

		if imported != 1 || updated != 1 {
			t.Fatalf("the import made %d and updated %d, want 1 and 1", imported, updated)
		}

		got, _ := v.FilamentType(ctx, stand.ID)
		if got.Custom || got.Brand != "Prusament" || got.NetWeightG == nil || *got.NetWeightG != 1000 {
			t.Fatalf("the stand-in is now %+v", got)
		}

		page, _ := v.FilamentTypePage(ctx, spoolsync.TypeQuery{Source: "openprinttag", Search: "orange", Limit: 10})
		if page.Total != 1 || page.Items[0].SpoolWeightG != 193 {
			t.Fatalf("searching the catalogue for orange answered %+v", page)
		}

		kinds, _ := v.FilamentKinds(ctx)
		if len(kinds) != 2 || kinds[0] != "PETG" {
			t.Fatalf("the kinds are %v", kinds)
		}
	})
}

func TestTheUsageTrendTakesEachSpoolsLastReadingOfTheDay(t *testing.T) {
	each(t, func(t *testing.T, v *Inventory) {
		ctx := t.Context()
		day := time.Date(2026, 11, 20, 9, 0, 0, 0, time.UTC)
		v.now = func() time.Time { return day }

		ft, _ := v.CreateFilamentType(ctx, spoolsync.NewFilamentType{Brand: "Acme", Material: "PLA", Name: "Red", SpoolWeightG: 200})
		sp, _ := v.Create(ctx, spoolsync.NewSpool{FilamentTypeID: ft.ID, InitialWeightG: 1200})

		day = day.Add(time.Hour)
		_ = v.SetWeight(ctx, sp.ID, 1100, "prusa-connect")
		day = day.Add(24 * time.Hour)
		_ = v.SetWeight(ctx, sp.ID, 900, "prusa-connect")

		trend, err := v.UsageTrend(ctx, 30)
		if err != nil {
			t.Fatal(err)
		}

		if len(trend) != 1 || len(trend[0].Data) != 2 || trend[0].Data[0].TotalG != 900 || trend[0].Data[1].TotalG != 700 {
			t.Fatalf("the trend is %+v", trend)
		}
	})
}

func TestReadDerivesTheUUIDsTheSpecificationDoes(t *testing.T) {
	var buf bytes.Buffer

	zw := zip.NewWriter(&buf)
	file := func(name, body string) {
		w, _ := zw.Create("openprinttag-database-main/" + name)
		_, _ = w.Write([]byte(body))
	}
	file("data/manifest.yaml", "version: 1\n")
	file("data/brands/prusament.yaml", "slug: prusament\nname: Prusament\nuuid: ae5ff34e-298e-50c9-8f77-92a97fb30b09\n")
	file("data/materials/prusament/pla-black.yaml",
		"slug: pla-black\nbrand: {slug: prusament}\nname: PLA Galaxy Black\nclass: FFF\ntype: PLA\n"+
			"primary_color: {color_rgba: '#3D3E3DFF'}\nproperties: {density: 1.24}\n")
	file("data/materials/prusament/resin.yaml", "slug: resin\nbrand: {slug: prusament}\nname: Tough\nclass: SLA\n")
	file("data/material-packages/prusament/pla-black-1kg.yaml",
		"material: {slug: pla-black}\ncontainer: {slug: spool-1kg}\nnominal_netto_full_weight: 1000\nfilament_diameter: 1750\n")
	file("data/material-containers/spool-1kg.yaml", "slug: spool-1kg\nempty_weight: 193\n")
	_ = zw.Close()

	rows, skipped, err := catalogue.Read(buf.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	if len(rows) != 1 || skipped != 0 {
		t.Fatalf("read %d materials and skipped %d, want 1 and 0 (resin is no filament)", len(rows), skipped)
	}

	m := rows[0]
	// UUIDv5 of the brand's uuid bytes and the name, in the material namespace:
	// what SpoolSync's openPrintTagUuid.js derives for the same file.
	if m.OptMaterialUUID != "6e774110-9aa4-5ab2-a269-456918dad9b1" {
		t.Fatalf("the material uuid is %q", m.OptMaterialUUID)
	}

	if m.Brand != "Prusament" || m.Material != "PLA" || *m.ColorHex != "#3d3e3d" || *m.SpoolWeightG != 193 ||
		*m.NetWeightG != 1000 || m.DiameterMM != 1.75 || *m.DensityG != 1.24 {
		t.Fatalf("the material read is %+v", m)
	}
}
