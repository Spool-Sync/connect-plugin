package spoolsync

import (
	"os"
	"testing"
)

// TestLive drives every call against a real SpoolSync. Scratch only: runs when
// SPOOLSYNC_LIVE_URL and SPOOLSYNC_LIVE_TOKEN are set.
func TestLive(t *testing.T) {
	base, token := os.Getenv("SPOOLSYNC_LIVE_URL"), os.Getenv("SPOOLSYNC_LIVE_TOKEN")
	if base == "" {
		t.Skip("no live SpoolSync")
	}

	c, err := New(base, token, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()

	types, err := c.FilamentTypes(ctx, "Galaxy Black")
	if err != nil || len(types) == 0 {
		t.Fatalf("search: %d %v", len(types), err)
	}
	ft := types[0]
	t.Logf("type %+v", ft)

	if _, err = c.FilamentTypeByID(ctx, ft.ID); err != nil {
		t.Fatalf("type by id: %v", err)
	}

	brands, err := c.Brands(ctx)
	t.Logf("brands %d err %v", len(brands), err)

	loc, err := c.CreateLocation(ctx, "Shelf Z")
	if err != nil {
		t.Fatalf("create location: %v", err)
	}
	t.Logf("location %+v", loc)
	if _, err = c.UpdateLocation(ctx, loc.ID, "Shelf Y"); err != nil {
		t.Fatalf("rename location: %v", err)
	}
	locs, err := c.Locations(ctx)
	t.Logf("locations %+v err %v", locs, err)

	sp, err := c.Create(ctx, NewSpool{FilamentTypeID: ft.ID, InitialWeightG: 1200, LocationID: loc.ID,
		OptInstanceUUID: "bf63e92d-9ca5-53d7-9fab-ffdd0240c585"})
	if err != nil {
		t.Fatalf("create spool: %v", err)
	}
	t.Logf("spool %+v", sp)

	got, err := c.FindByInstanceUUID(ctx, "bf63e92d-9ca5-53d7-9fab-ffdd0240c585")
	if err != nil || got.ID != sp.ID {
		t.Fatalf("by-opt: %+v %v", got, err)
	}

	if err = c.SetWeight(ctx, sp.ID, 1012.7, "prusa-connect"); err != nil {
		t.Fatalf("set weight: %v", err)
	}
	notes := "from the live test"
	yes, no := true, false
	upd, err := c.Update(ctx, sp.ID, SpoolUpdate{Notes: &notes, Archive: &yes, ArchiveReason: "used_up", ArchiveNote: "done"})
	if err != nil {
		t.Fatalf("archive: %v", err)
	}
	t.Logf("archived %+v", upd)
	upd, err = c.Update(ctx, sp.ID, SpoolUpdate{Archive: &no})
	if err != nil {
		t.Fatalf("unarchive: %v", err)
	}
	t.Logf("unarchived %+v", upd)

	hist, err := c.History(ctx, sp.ID)
	t.Logf("history %+v err %v", hist, err)

	page, err := c.List(ctx, ListQuery{PageSize: 5})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	t.Logf("list total %d items %d", page.Total, len(page.Items))

	all, err := c.AllSpools(ctx)
	t.Logf("all %d err %v", len(all), err)

	st, err := c.SyncState(ctx)
	t.Logf("sync %+v err %v", st, err)

	custom := "#ff00ff"
	nft, err := c.CreateFilamentType(ctx, NewFilamentType{Brand: "Acme", Material: "PLA", Name: "Magenta test", Color: &custom, DiameterMM: 1.75, SpoolWeightG: 200})
	if err != nil {
		t.Fatalf("create type: %v", err)
	}
	if _, err = c.UpdateFilamentType(ctx, nft.ID, map[string]any{"name": "Magenta test 2"}); err != nil {
		t.Fatalf("update type: %v", err)
	}

	if err = c.DeleteSpool(ctx, sp.ID); err != nil {
		t.Fatalf("delete spool: %v", err)
	}
	if err = c.DeleteFilamentType(ctx, nft.ID); err != nil {
		t.Fatalf("delete type: %v", err)
	}
	if err = c.DeleteLocation(ctx, loc.ID); err != nil {
		t.Fatalf("delete location: %v", err)
	}
}
