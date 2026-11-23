package main

import (
	"context"

	"gitlab.honex.life/spoolsync/spool-sync-prusa-connect/internal/spoolsync"
)

// inventory is where the spools, filament types and locations are kept: a
// SpoolSync server (*spoolsync.Client), or the module's own database when it
// runs standalone (*local.Inventory). The routes read and write through it
// alone, so they answer the same either way.
type inventory interface {
	AllSpools(ctx context.Context) ([]spoolsync.Spool, error)
	Get(ctx context.Context, id string) (spoolsync.Spool, error)
	FindByInstanceUUID(ctx context.Context, uuid string) (spoolsync.Spool, error)
	Create(ctx context.Context, n spoolsync.NewSpool) (spoolsync.Spool, error)
	Update(ctx context.Context, id string, u spoolsync.SpoolUpdate) (spoolsync.Spool, error)
	SetWeight(ctx context.Context, spoolID string, grossG float64, source string) error
	DeleteSpool(ctx context.Context, id string) error

	FilamentType(ctx context.Context, id string) (spoolsync.FilamentType, error)
	FilamentTypePage(ctx context.Context, q spoolsync.TypeQuery) (spoolsync.TypePage, error)
	FilamentTypesByID(ctx context.Context, ids []string) ([]spoolsync.FilamentType, error)
	FilamentTypeByOpt(ctx context.Context, materialUUID string) (spoolsync.FilamentType, error)
	FilamentKinds(ctx context.Context) ([]string, error)
	Brands(ctx context.Context) ([]string, error)
	CreateFilamentType(ctx context.Context, n spoolsync.NewFilamentType) (spoolsync.FilamentType, error)
	// UpdateFilamentType takes SpoolSync's field names: brand, material, name,
	// diameter_mm, colorHex, density_g_cm3 and optMaterialUuid.
	UpdateFilamentType(ctx context.Context, id string, body map[string]any) (spoolsync.FilamentType, error)
	DeleteFilamentType(ctx context.Context, id string) error

	StartSync(ctx context.Context) (spoolsync.SyncStatus, error)
	SyncState(ctx context.Context) (spoolsync.SyncStatus, error)

	Locations(ctx context.Context) ([]spoolsync.Location, error)
	CreateLocation(ctx context.Context, name string) (spoolsync.Location, error)
	UpdateLocation(ctx context.Context, id, name string) (spoolsync.Location, error)
	DeleteLocation(ctx context.Context, id string) error

	UsageTrend(ctx context.Context, days int) ([]spoolsync.UsageSeries, error)
}

// mirror is the part of SpoolSync that only a server has: the printers it is
// told about. A standalone module has nobody to tell.
type mirror interface {
	SyncPrinters(ctx context.Context, printers []spoolsync.PrinterReport) ([]spoolsync.SyncResult, error)
}

var _ inventory = (*spoolsync.Client)(nil)

// firstCatalogue starts the OpenPrintTag import on a standalone module that
// holds no catalogue yet, so its first visit can pick a material. A module on
// SpoolSync leaves the import to SpoolSync.
func (s *service) firstCatalogue(ctx context.Context) {
	if _, onServer := s.ss.(mirror); onServer {
		return
	}

	page, err := s.ss.FilamentTypePage(ctx, spoolsync.TypeQuery{Source: "openprinttag", Limit: 1})
	if err != nil || page.Total > 0 {
		return
	}

	if _, err = s.ss.StartSync(ctx); err != nil {
		s.log.Warn("could not start the catalogue import", "error", err)
	}
}
