package local

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/spool-sync/connect-plugin/internal/catalogue"
	"github.com/spool-sync/connect-plugin/internal/spoolsync"
)

// importTimeout bounds one catalogue import: the download and the writes.
const importTimeout = 15 * time.Minute

// importBatch is how many catalogue rows one transaction writes.
const importBatch = 500

// SyncState is the state of the last catalogue import, as SpoolSync's.
func (v *Inventory) SyncState(context.Context) (spoolsync.SyncStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	return v.sync, nil
}

// StartSync starts the OpenPrintTag import in the background, and answers the
// state it left. A second call while one runs answers the running one.
func (v *Inventory) StartSync(context.Context) (spoolsync.SyncStatus, error) {
	v.mu.Lock()
	defer v.mu.Unlock()

	if v.sync.State == "running" {
		return v.sync, nil
	}

	started := v.now().UTC()
	v.sync = spoolsync.SyncStatus{State: "running", StartedAt: &started}

	go v.runImport()

	return v.sync, nil
}

func (v *Inventory) runImport() {
	ctx, cancel := context.WithTimeout(context.Background(), importTimeout)
	defer cancel()

	imported, updated, skipped, err := v.importCatalogue(ctx)

	v.mu.Lock()
	defer v.mu.Unlock()

	finished := v.now().UTC()
	v.sync.FinishedAt = &finished

	if err != nil {
		msg := err.Error()
		v.sync.State, v.sync.Error = "failed", &msg
		v.log.Warn("the catalogue import failed", "error", err)

		return
	}

	v.sync.State, v.sync.Imported, v.sync.Updated, v.sync.Skipped = "idle", imported, updated, skipped
	v.log.Info("the catalogue was imported", "new", imported, "updated", updated, "skipped", skipped)
}

func (v *Inventory) importCatalogue(ctx context.Context) (imported, updated, skipped int, err error) {
	zip, err := catalogue.Download(ctx, v.HTTP, v.CatalogueURL)
	if err != nil {
		return 0, 0, 0, err
	}

	rows, skipped, err := catalogue.Read(zip)
	if err != nil {
		return 0, 0, 0, err
	}

	imported, updated, err = v.Upsert(ctx, rows)

	return imported, updated, skipped, err
}

// Upsert writes the catalogue's materials by their OpenPrintTag uuid: a new
// one is added, a held one updated, and a custom type that carries the uuid
// (one made from a tag before the catalogue held it) becomes the catalogue's.
func (v *Inventory) Upsert(ctx context.Context, rows []catalogue.Material) (imported, updated int, err error) {
	held := map[string]bool{}

	list, err := v.db.QueryContext(ctx, `SELECT opt_material_uuid FROM filament_types WHERE opt_material_uuid IS NOT NULL`)
	if err != nil {
		return 0, 0, fmt.Errorf("read the catalogue held: %w", err)
	}

	for list.Next() {
		var u string
		if err = list.Scan(&u); err != nil {
			_ = list.Close()

			return 0, 0, err
		}

		held[u] = true
	}

	_ = list.Close()

	for start := 0; start < len(rows); start += importBatch {
		batch := rows[start:min(start+importBatch, len(rows))]

		err = v.inTx(ctx, func(tx *sql.Tx) error {
			now := v.at(v.now())

			for _, m := range batch {
				if held[m.OptMaterialUUID] {
					if _, err := tx.ExecContext(ctx, `UPDATE filament_types SET brand = $1, material = $2, name = $3,
						color_hex = $4, diameter_mm = $5, density_g_cm3 = $6,
						spool_weight_g = COALESCE($7, spool_weight_g), net_weight_g = $8, custom = 0, updated_at = $9
						WHERE opt_material_uuid = $10`,
						m.Brand, m.Material, m.Name, nullText(m.ColorHex), m.DiameterMM, m.DensityG,
						m.SpoolWeightG, m.NetWeightG, now, m.OptMaterialUUID); err != nil {
						return err
					}

					updated++

					continue
				}

				empty := defaultSpoolWeight
				if m.SpoolWeightG != nil {
					empty = *m.SpoolWeightG
				}

				if _, err := tx.ExecContext(ctx, `INSERT INTO filament_types (id, opt_material_uuid, brand, material, name,
					color_hex, diameter_mm, density_g_cm3, spool_weight_g, net_weight_g, custom, created_at, updated_at)
					VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, 0, $11, $11)`,
					newID(), m.OptMaterialUUID, m.Brand, m.Material, m.Name, nullText(m.ColorHex), m.DiameterMM,
					m.DensityG, empty, m.NetWeightG, now); err != nil {
					return err
				}

				held[m.OptMaterialUUID] = true
				imported++
			}

			return nil
		})
		if err != nil {
			return imported, updated, fmt.Errorf("write the catalogue: %w", err)
		}
	}

	return imported, updated, nil
}
