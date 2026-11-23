// Package local is the inventory of a standalone module: the spools, filament
// types and storage locations kept in the module's own database, for a module
// with no SpoolSync server. It answers what the SpoolSync client answers, in
// the same types and with the same weight rules (a weight is gross, the
// remaining filament is the gross less the empty spool), so the routes do not
// tell the two apart.
package local

import (
	"cmp"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"

	"github.com/spool-sync/connect/internal/catalogue"
	"github.com/spool-sync/connect/internal/spoolsync"
)

// maxTypePage is the most filament types one page answers, as SpoolSync's.
const maxTypePage = 200

// defaultSpoolWeight is the empty spool of a type that names none, as SpoolSync's.
const defaultSpoolWeight = 200.0

// Inventory reads and writes the standalone tables.
type Inventory struct {
	db     *sql.DB
	engine wirerpc.DatabaseEngine
	log    *slog.Logger
	now    func() time.Time

	// CatalogueURL is where the OpenPrintTag database is downloaded from.
	CatalogueURL string
	// HTTP downloads the catalogue.
	HTTP *http.Client

	mu   sync.Mutex
	sync spoolsync.SyncStatus
}

// New wraps an open, migrated database.
func New(db *sql.DB, engine wirerpc.DatabaseEngine, log *slog.Logger) *Inventory {
	return &Inventory{
		db: db, engine: engine, log: log, now: time.Now,
		CatalogueURL: catalogue.DefaultURL, sync: spoolsync.SyncStatus{State: "idle"},
	}
}

// --- times ---------------------------------------------------------------

const textTime = "2006-01-02T15:04:05.000000000Z07:00"

func (v *Inventory) at(when time.Time) any {
	if v.engine == wirerpc.DatabasePostgres {
		return when.UTC()
	}

	return when.UTC().Format(textTime)
}

func parseTime(raw any) (time.Time, error) {
	switch t := raw.(type) {
	case time.Time:
		return t.UTC(), nil
	case string:
		return time.Parse(time.RFC3339Nano, t)
	case []byte:
		return time.Parse(time.RFC3339Nano, string(t))
	}

	return time.Time{}, fmt.Errorf("unexpected time column %T", raw)
}

func parseNullTime(raw any) (*time.Time, error) {
	if raw == nil {
		return nil, nil
	}

	t, err := parseTime(raw)

	return &t, err
}

func newID() string { return uuid.NewString() }

func nullText(p *string) any {
	if p == nil {
		return nil
	}

	return *p
}

func textOf(s sql.NullString) *string {
	if !s.Valid {
		return nil
	}

	v := s.String

	return &v
}

func floatOf(f sql.NullFloat64) *float64 {
	if !f.Valid {
		return nil
	}

	v := f.Float64

	return &v
}

// refused is an answer SpoolSync would give as a status, such as 409.
func refused(status int, method, what, message string) error {
	return &spoolsync.StatusError{Method: method, Path: what, Status: status, Message: message}
}

// like is a case-insensitive contains pattern, with LIKE's own characters escaped.
func like(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

	return "%" + r.Replace(strings.ToLower(s)) + "%"
}

// --- filament types ------------------------------------------------------

const typeColumns = `id, opt_material_uuid, brand, material, name, color_hex, diameter_mm,
	density_g_cm3, spool_weight_g, net_weight_g, custom, notes`

type scanner interface{ Scan(dest ...any) error }

func scanType(row scanner) (spoolsync.FilamentType, error) {
	var (
		ft                 spoolsync.FilamentType
		opt, colour, notes sql.NullString
		density, net       sql.NullFloat64
		custom             int
	)

	err := row.Scan(&ft.ID, &opt, &ft.Brand, &ft.Material, &ft.Name, &colour, &ft.DiameterMM,
		&density, &ft.SpoolWeightG, &net, &custom, &notes)
	if err != nil {
		return spoolsync.FilamentType{}, err
	}

	ft.OptMaterialUUID, ft.Color, ft.Notes = textOf(opt), textOf(colour), textOf(notes)
	ft.DensityG, ft.NetWeightG = floatOf(density), floatOf(net)
	ft.Custom = custom != 0

	return ft, nil
}

// FilamentType is one filament type by its id.
func (v *Inventory) FilamentType(ctx context.Context, id string) (spoolsync.FilamentType, error) {
	ft, err := scanType(v.db.QueryRowContext(ctx, `SELECT `+typeColumns+` FROM filament_types WHERE id = $1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return spoolsync.FilamentType{}, spoolsync.ErrNotFound
	}

	return ft, err
}

// FilamentTypeByOpt is the filament type with this OpenPrintTag material uuid.
func (v *Inventory) FilamentTypeByOpt(ctx context.Context, materialUUID string) (spoolsync.FilamentType, error) {
	ft, err := scanType(v.db.QueryRowContext(ctx, `SELECT `+typeColumns+` FROM filament_types WHERE opt_material_uuid = $1`,
		strings.ToLower(strings.TrimSpace(materialUUID))))
	if errors.Is(err, sql.ErrNoRows) {
		return spoolsync.FilamentType{}, spoolsync.ErrNotFound
	}

	return ft, err
}

// where builds a WHERE clause of numbered parameters.
type where struct {
	parts []string
	args  []any
}

func (w *where) add(clause string, args ...any) {
	for _, a := range args {
		w.args = append(w.args, a)
		clause = strings.Replace(clause, "?", fmt.Sprintf("$%d", len(w.args)), 1)
	}

	w.parts = append(w.parts, clause)
}

func (w *where) in(column string, ids []string, not bool) {
	marks := make([]string, 0, len(ids))
	for _, id := range ids {
		w.args = append(w.args, id)
		marks = append(marks, fmt.Sprintf("$%d", len(w.args)))
	}

	op := "IN"
	if not {
		op = "NOT IN"
	}

	w.parts = append(w.parts, column+" "+op+" ("+strings.Join(marks, ", ")+")")
}

func (w *where) String() string {
	if len(w.parts) == 0 {
		return ""
	}

	return " WHERE " + strings.Join(w.parts, " AND ")
}

// FilamentTypePage reads one page of filament types, as SpoolSync's paged read.
func (v *Inventory) FilamentTypePage(ctx context.Context, q spoolsync.TypeQuery) (spoolsync.TypePage, error) {
	var w where

	if s := strings.TrimSpace(q.Search); s != "" {
		p := like(s)
		w.add(`(LOWER(name) LIKE ? ESCAPE '\' OR LOWER(brand) LIKE ? ESCAPE '\' OR LOWER(material) LIKE ? ESCAPE '\')`, p, p, p)
	}

	if q.Brand != "" {
		w.add(`LOWER(brand) = ?`, strings.ToLower(q.Brand))
	}

	if q.Material != "" {
		w.add(`LOWER(material) = ?`, strings.ToLower(q.Material))
	}

	switch q.Source {
	case "openprinttag":
		w.add(`opt_material_uuid IS NOT NULL AND custom = 0`)
	case "custom":
		w.add(`custom = 1`)
	case "catalogue":
		w.add(`custom = 0`)
	}

	if len(q.IDs) > 0 {
		w.in("id", q.IDs, false)
	}

	if len(q.Exclude) > 0 {
		w.in("id", q.Exclude, true)
	}

	var total int
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM filament_types`+w.String(), w.args...).Scan(&total); err != nil {
		return spoolsync.TypePage{}, fmt.Errorf("count the filament types: %w", err)
	}

	dir := "ASC"
	if q.Desc {
		dir = "DESC"
	}

	order := "name " + dir + ", brand " + dir
	if q.OrderBy == "brand" || q.OrderBy == "material" {
		order = q.OrderBy + " " + dir + ", name " + dir
	}

	limit := min(max(q.Limit, 1), maxTypePage)
	args := append(slices.Clone(w.args), limit, max(q.Offset, 0))

	rows, err := v.db.QueryContext(ctx, fmt.Sprintf(`SELECT %s FROM filament_types%s ORDER BY %s, id LIMIT $%d OFFSET $%d`,
		typeColumns, w.String(), order, len(args)-1, len(args)), args...)
	if err != nil {
		return spoolsync.TypePage{}, fmt.Errorf("read the filament types: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := spoolsync.TypePage{Total: total, Items: []spoolsync.FilamentType{}}

	for rows.Next() {
		ft, err := scanType(rows)
		if err != nil {
			return spoolsync.TypePage{}, err
		}

		out.Items = append(out.Items, ft)
	}

	return out, rows.Err()
}

// FilamentTypesByID reads these filament types.
func (v *Inventory) FilamentTypesByID(ctx context.Context, ids []string) ([]spoolsync.FilamentType, error) {
	var out []spoolsync.FilamentType

	for start := 0; start < len(ids); start += maxTypePage {
		page, err := v.FilamentTypePage(ctx, spoolsync.TypeQuery{IDs: ids[start:min(start+maxTypePage, len(ids))], Limit: maxTypePage})
		if err != nil {
			return nil, err
		}

		out = append(out, page.Items...)
	}

	return out, nil
}

func (v *Inventory) distinct(ctx context.Context, column string) ([]string, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT DISTINCT `+column+` FROM filament_types WHERE `+column+` <> '' ORDER BY `+column)
	if err != nil {
		return nil, fmt.Errorf("read the %ss: %w", column, err)
	}

	defer func() { _ = rows.Close() }()

	out := []string{}

	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}

		out = append(out, s)
	}

	return out, rows.Err()
}

// FilamentKinds is every distinct material, such as PLA.
func (v *Inventory) FilamentKinds(ctx context.Context) ([]string, error) {
	return v.distinct(ctx, "material")
}

// Brands is every distinct brand.
func (v *Inventory) Brands(ctx context.Context) ([]string, error) { return v.distinct(ctx, "brand") }

// CreateFilamentType makes a custom filament type.
func (v *Inventory) CreateFilamentType(ctx context.Context, n spoolsync.NewFilamentType) (spoolsync.FilamentType, error) {
	id, now := newID(), v.at(v.now())
	diameter := cmp.Or(n.DiameterMM, 1.75)
	empty := cmp.Or(n.SpoolWeightG, defaultSpoolWeight)

	var opt any
	if n.OptMaterialUUID != nil {
		opt = strings.ToLower(*n.OptMaterialUUID)
	}

	_, err := v.db.ExecContext(ctx, `INSERT INTO filament_types (id, opt_material_uuid, brand, material, name, color_hex,
		diameter_mm, density_g_cm3, spool_weight_g, custom, notes, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, 1, $10, $11, $11)`,
		id, opt, n.Brand, n.Material, n.Name, nullText(n.Color), diameter, n.DensityG, empty, nullText(n.Notes), now)
	if err != nil {
		return spoolsync.FilamentType{}, fmt.Errorf("make the filament type: %w", err)
	}

	return v.FilamentType(ctx, id)
}

// UpdateFilamentType edits a filament type with SpoolSync's field names.
func (v *Inventory) UpdateFilamentType(ctx context.Context, id string, body map[string]any) (spoolsync.FilamentType, error) {
	columns := map[string]string{
		"brand": "brand", "material": "material", "name": "name", "diameter_mm": "diameter_mm",
		"colorHex": "color_hex", "density_g_cm3": "density_g_cm3", "optMaterialUuid": "opt_material_uuid",
		"spoolWeight_g": "spool_weight_g", "notes": "notes",
	}

	var (
		sets []string
		args []any
	)

	for _, key := range slices.Sorted(func(yield func(string) bool) {
		for k := range body {
			if !yield(k) {
				return
			}
		}
	}) {
		col, ok := columns[key]
		if !ok {
			continue
		}

		val := body[key]
		if p, ok := val.(*string); ok {
			val = nullText(p)
		} else if p, ok := val.(*float64); ok {
			if p == nil {
				val = nil
			} else {
				val = *p
			}
		}

		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	if _, err := v.FilamentType(ctx, id); err != nil {
		return spoolsync.FilamentType{}, err
	}

	args = append(args, v.at(v.now()), id)
	sets = append(sets, fmt.Sprintf("updated_at = $%d", len(args)-1))

	if _, err := v.db.ExecContext(ctx, `UPDATE filament_types SET `+strings.Join(sets, ", ")+
		fmt.Sprintf(` WHERE id = $%d`, len(args)), args...); err != nil {
		return spoolsync.FilamentType{}, fmt.Errorf("edit the filament type: %w", err)
	}

	return v.FilamentType(ctx, id)
}

// DeleteFilamentType takes a filament type out, refused while a spool is of it.
func (v *Inventory) DeleteFilamentType(ctx context.Context, id string) error {
	var used int
	if err := v.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM spools WHERE filament_type_id = $1`, id).Scan(&used); err != nil {
		return err
	}

	if used > 0 {
		return refused(http.StatusConflict, http.MethodDelete, "/filament-types/"+id, "spools are of this filament type")
	}

	res, err := v.db.ExecContext(ctx, `DELETE FROM filament_types WHERE id = $1`, id)
	if err != nil {
		return err
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return spoolsync.ErrNotFound
	}

	return nil
}

// --- spools --------------------------------------------------------------

const spoolColumns = `s.id, s.filament_type_id, s.initial_weight_g, s.current_weight_g, s.status, s.opt_instance_uuid,
	s.notes, s.archived_at, s.archive_reason, s.archive_note, s.created_at, s.updated_at, s.location_id, l.name, ` +
	`t.id, t.opt_material_uuid, t.brand, t.material, t.name, t.color_hex, t.diameter_mm,
	t.density_g_cm3, t.spool_weight_g, t.net_weight_g, t.custom, t.notes`

const spoolFrom = ` FROM spools s JOIN filament_types t ON t.id = s.filament_type_id LEFT JOIN locations l ON l.id = s.location_id`

func scanSpool(row scanner) (spoolsync.Spool, error) {
	var (
		sp                                  spoolsync.Spool
		initial, current                    float64
		opt, notes, reason, note, loc, name sql.NullString
		archived, created, updated          any
		ft                                  spoolsync.FilamentType
		topt, colour, tnotes                sql.NullString
		density, net                        sql.NullFloat64
		custom                              int
	)

	err := row.Scan(&sp.ID, &sp.FilamentTypeID, &initial, &current, &sp.Status, &opt, &notes, &archived, &reason, &note,
		&created, &updated, &loc, &name,
		&ft.ID, &topt, &ft.Brand, &ft.Material, &ft.Name, &colour, &ft.DiameterMM, &density, &ft.SpoolWeightG, &net,
		&custom, &tnotes)
	if err != nil {
		return spoolsync.Spool{}, err
	}

	ft.OptMaterialUUID, ft.Color, ft.Notes = textOf(topt), textOf(colour), textOf(tnotes)
	ft.DensityG, ft.NetWeightG, ft.Custom = floatOf(density), floatOf(net), custom != 0

	if sp.ArchivedAt, err = parseNullTime(archived); err != nil {
		return spoolsync.Spool{}, err
	}

	if sp.CreatedAt, err = parseTime(created); err != nil {
		return spoolsync.Spool{}, err
	}

	if sp.UpdatedAt, err = parseTime(updated); err != nil {
		return spoolsync.Spool{}, err
	}

	empty := ft.SpoolWeightG
	sp.Name = strings.TrimSpace(ft.Brand + " " + ft.Material)
	sp.Brand, sp.Material, sp.Color = ft.Brand, ft.Material, ft.Color
	sp.GrossG, sp.EmptyG = current, empty
	sp.RemainingG = math.Max(0, current-empty)
	sp.InitialG = math.Max(0, initial-empty)
	sp.OptInstanceUUID, sp.Notes = textOf(opt), textOf(notes)
	sp.ArchiveReason, sp.ArchiveNote = textOf(reason), textOf(note)
	sp.LocationID, sp.Location = textOf(loc), textOf(name)
	sp.Filament = ft

	sp.Source = "manual"
	if sp.OptInstanceUUID != nil && *sp.OptInstanceUUID != "" {
		sp.Source = "printer"
	}

	return sp, nil
}

func (v *Inventory) oneSpool(ctx context.Context, clause string, arg any) (spoolsync.Spool, error) {
	sp, err := scanSpool(v.db.QueryRowContext(ctx, `SELECT `+spoolColumns+spoolFrom+` WHERE `+clause, arg))
	if errors.Is(err, sql.ErrNoRows) {
		return spoolsync.Spool{}, spoolsync.ErrNotFound
	}

	return sp, err
}

// Get is one spool by its id.
func (v *Inventory) Get(ctx context.Context, id string) (spoolsync.Spool, error) {
	return v.oneSpool(ctx, `s.id = $1`, id)
}

// FindByInstanceUUID is the spool whose tag has this instance uuid.
func (v *Inventory) FindByInstanceUUID(ctx context.Context, uuid string) (spoolsync.Spool, error) {
	return v.oneSpool(ctx, `s.opt_instance_uuid = $1`, strings.ToLower(strings.TrimSpace(uuid)))
}

// AllSpools reads every spool, spent and archived ones included, oldest first.
func (v *Inventory) AllSpools(ctx context.Context) ([]spoolsync.Spool, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT `+spoolColumns+spoolFrom+` ORDER BY s.created_at, s.id`)
	if err != nil {
		return nil, fmt.Errorf("read the spools: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []spoolsync.Spool

	for rows.Next() {
		sp, err := scanSpool(rows)
		if err != nil {
			return nil, err
		}

		out = append(out, sp)
	}

	return out, rows.Err()
}

func (v *Inventory) record(ctx context.Context, tx *sql.Tx, spoolID string, gross float64, source string) error {
	_, err := tx.ExecContext(ctx, `INSERT INTO weight_history (id, spool_id, weight_g, source, recorded_at)
		VALUES ($1, $2, $3, $4, $5)`, newID(), spoolID, gross, source, v.at(v.now()))

	return err
}

// Create adds a spool of the gross weight given, and records it as its first reading.
func (v *Inventory) Create(ctx context.Context, n spoolsync.NewSpool) (spoolsync.Spool, error) {
	if _, err := v.FilamentType(ctx, n.FilamentTypeID); err != nil {
		return spoolsync.Spool{}, err
	}

	id, now := newID(), v.at(v.now())

	var opt, loc any
	if n.OptInstanceUUID != "" {
		opt = strings.ToLower(n.OptInstanceUUID)
	}

	if n.LocationID != "" {
		loc = n.LocationID
	}

	err := v.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `INSERT INTO spools (id, filament_type_id, initial_weight_g, current_weight_g,
			status, opt_instance_uuid, location_id, notes, created_at, updated_at)
			VALUES ($1, $2, $3, $3, 'ACTIVE', $4, $5, $6, $7, $7)`,
			id, n.FilamentTypeID, n.InitialWeightG, opt, loc, nullText(n.Notes), now); err != nil {
			return err
		}

		return v.record(ctx, tx, id, n.InitialWeightG, "manual")
	})
	if err != nil {
		if opt != nil && isUnique(err) {
			return spoolsync.Spool{}, refused(http.StatusConflict, http.MethodPost, "/spools", "a spool carries this tag already")
		}

		return spoolsync.Spool{}, fmt.Errorf("add the spool: %w", err)
	}

	return v.Get(ctx, id)
}

func isUnique(err error) bool {
	s := strings.ToLower(err.Error())

	return strings.Contains(s, "unique") || strings.Contains(s, "duplicate key")
}

func (v *Inventory) inTx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := v.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}

	if err = fn(tx); err != nil {
		_ = tx.Rollback()

		return err
	}

	return tx.Commit()
}

// Update edits a spool and answers it as it is then. A new weight is recorded.
func (v *Inventory) Update(ctx context.Context, id string, u spoolsync.SpoolUpdate) (spoolsync.Spool, error) {
	if _, err := v.Get(ctx, id); err != nil {
		return spoolsync.Spool{}, err
	}

	var (
		sets []string
		args []any
	)

	set := func(col string, val any) {
		args = append(args, val)
		sets = append(sets, fmt.Sprintf("%s = $%d", col, len(args)))
	}

	if u.FilamentTypeID != nil {
		if _, err := v.FilamentType(ctx, *u.FilamentTypeID); err != nil {
			return spoolsync.Spool{}, err
		}

		set("filament_type_id", *u.FilamentTypeID)
	}

	if u.Status != nil {
		set("status", *u.Status)
	}

	if u.LocationID != nil {
		if *u.LocationID == "" {
			set("location_id", nil)
		} else {
			set("location_id", *u.LocationID)
		}
	}

	if u.CurrentWeightG != nil {
		set("current_weight_g", *u.CurrentWeightG)
	}

	if u.Notes != nil {
		set("notes", *u.Notes)
	}

	if u.Archive != nil {
		if *u.Archive {
			set("archived_at", v.at(v.now()))
			set("archive_reason", u.ArchiveReason)
			set("archive_note", u.ArchiveNote)
		} else {
			set("archived_at", nil)
			set("archive_reason", nil)
			set("archive_note", nil)
		}
	}

	set("updated_at", v.at(v.now()))
	args = append(args, id)

	err := v.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE spools SET `+strings.Join(sets, ", ")+fmt.Sprintf(` WHERE id = $%d`, len(args)), args...); err != nil {
			return err
		}

		if u.CurrentWeightG != nil {
			return v.record(ctx, tx, id, *u.CurrentWeightG, "manual")
		}

		return nil
	})
	if err != nil {
		return spoolsync.Spool{}, fmt.Errorf("edit the spool: %w", err)
	}

	return v.Get(ctx, id)
}

// SetWeight sets the gross weight of a spool, recorded as a reading of source.
func (v *Inventory) SetWeight(ctx context.Context, spoolID string, grossG float64, source string) error {
	return v.inTx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `UPDATE spools SET current_weight_g = $1, updated_at = $2 WHERE id = $3`,
			grossG, v.at(v.now()), spoolID)
		if err != nil {
			return err
		}

		if n, _ := res.RowsAffected(); n == 0 {
			return spoolsync.ErrNotFound
		}

		return v.record(ctx, tx, spoolID, grossG, source)
	})
}

// DeleteSpool takes a spool and its readings out.
func (v *Inventory) DeleteSpool(ctx context.Context, id string) error {
	return v.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM weight_history WHERE spool_id = $1`, id); err != nil {
			return err
		}

		res, err := tx.ExecContext(ctx, `DELETE FROM spools WHERE id = $1`, id)
		if err != nil {
			return err
		}

		if n, _ := res.RowsAffected(); n == 0 {
			return spoolsync.ErrNotFound
		}

		return nil
	})
}

// --- locations -----------------------------------------------------------

// Locations lists the storage locations by name.
func (v *Inventory) Locations(ctx context.Context) ([]spoolsync.Location, error) {
	rows, err := v.db.QueryContext(ctx, `SELECT id, name FROM locations ORDER BY name, id`)
	if err != nil {
		return nil, fmt.Errorf("read the locations: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := []spoolsync.Location{}

	for rows.Next() {
		var l spoolsync.Location
		if err := rows.Scan(&l.ID, &l.Name); err != nil {
			return nil, err
		}

		out = append(out, l)
	}

	return out, rows.Err()
}

// CreateLocation makes a storage location.
func (v *Inventory) CreateLocation(ctx context.Context, name string) (spoolsync.Location, error) {
	l := spoolsync.Location{ID: newID(), Name: name}
	if _, err := v.db.ExecContext(ctx, `INSERT INTO locations (id, name) VALUES ($1, $2)`, l.ID, l.Name); err != nil {
		return spoolsync.Location{}, fmt.Errorf("make the location: %w", err)
	}

	return l, nil
}

// UpdateLocation renames a storage location.
func (v *Inventory) UpdateLocation(ctx context.Context, id, name string) (spoolsync.Location, error) {
	res, err := v.db.ExecContext(ctx, `UPDATE locations SET name = $1 WHERE id = $2`, name, id)
	if err != nil {
		return spoolsync.Location{}, fmt.Errorf("rename the location: %w", err)
	}

	if n, _ := res.RowsAffected(); n == 0 {
		return spoolsync.Location{}, spoolsync.ErrNotFound
	}

	return spoolsync.Location{ID: id, Name: name}, nil
}

// DeleteLocation removes a storage location; its spools keep none.
func (v *Inventory) DeleteLocation(ctx context.Context, id string) error {
	return v.inTx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE spools SET location_id = NULL WHERE location_id = $1`, id); err != nil {
			return err
		}

		res, err := tx.ExecContext(ctx, `DELETE FROM locations WHERE id = $1`, id)
		if err != nil {
			return err
		}

		if n, _ := res.RowsAffected(); n == 0 {
			return spoolsync.ErrNotFound
		}

		return nil
	})
}

// --- usage ---------------------------------------------------------------

// UsageTrend is the filament on each filament type's spools, a point a day,
// over the last days: each spool's last reading of the day, less its empty
// spool, summed by type.
func (v *Inventory) UsageTrend(ctx context.Context, days int) ([]spoolsync.UsageSeries, error) {
	since := v.now().AddDate(0, 0, -days)

	rows, err := v.db.QueryContext(ctx, `SELECT h.spool_id, h.weight_g, h.recorded_at, t.id, t.brand, t.name, t.color_hex, t.spool_weight_g
		FROM weight_history h JOIN spools s ON s.id = h.spool_id JOIN filament_types t ON t.id = s.filament_type_id
		WHERE h.recorded_at >= $1 ORDER BY h.recorded_at`, v.at(since))
	if err != nil {
		return nil, fmt.Errorf("read the weight history: %w", err)
	}

	defer func() { _ = rows.Close() }()

	type key struct{ typeID, day, spool string }

	last := map[key]float64{}
	series := map[string]*spoolsync.UsageSeries{}

	for rows.Next() {
		var (
			spool, typeID, brand, name string
			weight, empty              float64
			at                         any
			colour                     sql.NullString
		)

		if err := rows.Scan(&spool, &weight, &at, &typeID, &brand, &name, &colour, &empty); err != nil {
			return nil, err
		}

		when, err := parseTime(at)
		if err != nil {
			return nil, err
		}

		if series[typeID] == nil {
			series[typeID] = &spoolsync.UsageSeries{FilamentTypeID: typeID, Label: strings.TrimSpace(brand + " " + name), ColorHex: textOf(colour)}
		}

		// Rows come oldest first, so the last one written is the day's last.
		last[key{typeID, when.UTC().Format(time.DateOnly), spool}] = math.Max(0, weight-empty)
	}

	if err := rows.Err(); err != nil {
		return nil, err
	}

	byDay := map[string]map[string]float64{}
	for k, g := range last {
		if byDay[k.typeID] == nil {
			byDay[k.typeID] = map[string]float64{}
		}

		byDay[k.typeID][k.day] += g
	}

	out := make([]spoolsync.UsageSeries, 0, len(series))

	for id, s := range series {
		for _, day := range slices.Sorted(func(yield func(string) bool) {
			for d := range byDay[id] {
				if !yield(d) {
					return
				}
			}
		}) {
			s.Data = append(s.Data, spoolsync.UsagePoint{Date: day, TotalG: math.Round(byDay[id][day])})
		}

		out = append(out, *s)
	}

	slices.SortFunc(out, func(a, b spoolsync.UsageSeries) int { return cmp.Compare(a.Label, b.Label) })

	return out, nil
}
