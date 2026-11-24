// Package store is the module's own database: which spool a person put on a
// printer tool, the last odometer reading of each tool, and the grams counted
// off a spool and not yet written to SpoolSync.
package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"time"

	// The two drivers the database may need. sdk/moduledb imports none.
	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"

	"github.com/prusa3d/prusa-connect/sdk/moduledb"
	"github.com/prusa3d/prusa-connect/sdk/wirerpc"
)

// Assignment is what a person put on one printer tool: a material, and a
// spool of it when one was picked. SpoolID is empty for a material alone.
type Assignment struct {
	PrinterID      string
	Tool           string
	FilamentTypeID string
	SpoolID        string
	AssignedAt     time.Time
	AssignedBy     string
}

// nullIf is the text as a column value, NULL for the empty text.
func nullIf(s string) any {
	if s == "" {
		return nil
	}

	return s
}

// Odometer is the last reading of one tool's odometer.
type Odometer struct {
	// ID is the odometer's id, nil when the printer reports none.
	ID *int64
	// E is the millimetres fed since the last reset.
	E float64
	// Weight is the grams fed, nil when the printer reports none.
	Weight    *float64
	UpdatedAt time.Time
}

// Store reads and writes the tables the migrations make, on the engine the
// operator granted. The statements are the same on both engines: both read $1.
// Only a time differs: SQLite keeps it as text, Postgres as a timestamp.
type Store struct {
	db     *sql.DB
	engine wirerpc.DatabaseEngine
}

// New wraps an open, migrated database.
func New(db *sql.DB, engine wirerpc.DatabaseEngine) *Store {
	return &Store{db: db, engine: engine}
}

// Open opens the granted database and migrates it with the migrations under
// migrations/ in migrations. The caller closes the handle.
func Open(ctx context.Context, grant wirerpc.DatabaseGrant, migrations fs.FS) (*sql.DB, moduledb.Report, error) {
	return moduledb.Open(ctx, grant, moduledb.Schema{Migrations: migrations})
}

// textTime keeps every fraction digit, so SQLite orders the text as time.
const textTime = "2006-01-02T15:04:05.000000000Z07:00"

// at renders a time for one statement.
func (s *Store) at(when time.Time) any {
	if s.engine == wirerpc.DatabasePostgres {
		return when.UTC()
	}

	return when.UTC().Format(textTime)
}

// parseTime reads a time column: a time.Time on Postgres, text on SQLite.
func parseTime(v any) (time.Time, error) {
	switch v := v.(type) {
	case time.Time:
		return v.UTC(), nil
	case string:
		return time.Parse(time.RFC3339Nano, v)
	case []byte:
		return time.Parse(time.RFC3339Nano, string(v))
	}

	return time.Time{}, fmt.Errorf("unexpected time column %T", v)
}

// Assignment answers the spool on one tool, and false for a tool with none.
func (s *Store) Assignment(ctx context.Context, printerID, tool string) (Assignment, bool, error) {
	var (
		a                     Assignment
		at                    any
		filamentType, spoolID sql.NullString
	)

	err := s.db.QueryRowContext(ctx, `SELECT printer_id, tool, filament_type_id, spool_id, assigned_at, assigned_by
		FROM assignments WHERE printer_id = $1 AND tool = $2`, printerID, tool).
		Scan(&a.PrinterID, &a.Tool, &filamentType, &spoolID, &at, &a.AssignedBy)
	if errors.Is(err, sql.ErrNoRows) {
		return Assignment{}, false, nil
	}

	if err != nil {
		return Assignment{}, false, fmt.Errorf("read the assignment of %s %s: %w", printerID, tool, err)
	}

	a.FilamentTypeID, a.SpoolID = filamentType.String, spoolID.String

	if a.AssignedAt, err = parseTime(at); err != nil {
		return Assignment{}, false, fmt.Errorf("read the assignment of %s %s: %w", printerID, tool, err)
	}

	return a, true, nil
}

// Assignments answers every assignment, ordered by printer and tool.
func (s *Store) Assignments(ctx context.Context) ([]Assignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT printer_id, tool, filament_type_id, spool_id, assigned_at, assigned_by
		FROM assignments ORDER BY printer_id, tool`)
	if err != nil {
		return nil, fmt.Errorf("list the assignments: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []Assignment

	for rows.Next() {
		var (
			a                     Assignment
			at                    any
			filamentType, spoolID sql.NullString
		)

		if err = rows.Scan(&a.PrinterID, &a.Tool, &filamentType, &spoolID, &at, &a.AssignedBy); err != nil {
			return nil, fmt.Errorf("list the assignments: %w", err)
		}

		a.FilamentTypeID, a.SpoolID = filamentType.String, spoolID.String

		if a.AssignedAt, err = parseTime(at); err != nil {
			return nil, fmt.Errorf("list the assignments: %w", err)
		}

		out = append(out, a)
	}

	return out, rows.Err()
}

// SetAssignment puts the spool on the tool, replacing the one on it.
func (s *Store) SetAssignment(ctx context.Context, a Assignment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO assignments (printer_id, tool, filament_type_id, spool_id, assigned_at, assigned_by)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (printer_id, tool) DO UPDATE SET filament_type_id = excluded.filament_type_id,
			spool_id = excluded.spool_id, assigned_at = excluded.assigned_at, assigned_by = excluded.assigned_by`,
		a.PrinterID, a.Tool, nullIf(a.FilamentTypeID), nullIf(a.SpoolID), s.at(a.AssignedAt), a.AssignedBy)
	if err != nil {
		return fmt.Errorf("assign the spool to %s %s: %w", a.PrinterID, a.Tool, err)
	}

	return nil
}

// DeleteAssignment takes the spool off the tool, and reports whether one was on it.
func (s *Store) DeleteAssignment(ctx context.Context, printerID, tool string) (bool, error) {
	res, err := s.db.ExecContext(ctx, `DELETE FROM assignments WHERE printer_id = $1 AND tool = $2`, printerID, tool)
	if err != nil {
		return false, fmt.Errorf("unassign %s %s: %w", printerID, tool, err)
	}

	n, err := res.RowsAffected()

	return n > 0, err
}

// Odometer answers the last reading of a tool's odometer, and false for none.
func (s *Store) Odometer(ctx context.Context, printerID, tool string) (Odometer, bool, error) {
	var (
		id     sql.NullInt64
		weight sql.NullFloat64
		e      float64
		at     any
	)

	err := s.db.QueryRowContext(ctx, `SELECT odometer_id, e_mm, weight_g, updated_at FROM odometers
		WHERE printer_id = $1 AND tool = $2`, printerID, tool).Scan(&id, &e, &weight, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return Odometer{}, false, nil
	}

	if err != nil {
		return Odometer{}, false, fmt.Errorf("read the odometer of %s %s: %w", printerID, tool, err)
	}

	o := Odometer{E: e}
	if id.Valid {
		v := id.Int64
		o.ID = &v
	}

	if weight.Valid {
		v := weight.Float64
		o.Weight = &v
	}

	if o.UpdatedAt, err = parseTime(at); err != nil {
		return Odometer{}, false, fmt.Errorf("read the odometer of %s %s: %w", printerID, tool, err)
	}

	return o, true, nil
}

// SetOdometer keeps a reading of a tool's odometer.
func (s *Store) SetOdometer(ctx context.Context, printerID, tool string, o Odometer, now time.Time) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO odometers (printer_id, tool, odometer_id, e_mm, weight_g, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (printer_id, tool) DO UPDATE SET odometer_id = excluded.odometer_id,
			e_mm = excluded.e_mm, weight_g = excluded.weight_g, updated_at = excluded.updated_at`,
		printerID, tool, o.ID, o.E, o.Weight, s.at(now))
	if err != nil {
		return fmt.Errorf("keep the odometer of %s %s: %w", printerID, tool, err)
	}

	return nil
}

// DeleteOdometer forgets a tool's reading, so the next one starts a count.
func (s *Store) DeleteOdometer(ctx context.Context, printerID, tool string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM odometers WHERE printer_id = $1 AND tool = $2`, printerID, tool)
	if err != nil {
		return fmt.Errorf("forget the odometer of %s %s: %w", printerID, tool, err)
	}

	return nil
}

// ForgetPrinter takes every assignment and odometer reading of a printer away.
func (s *Store) ForgetPrinter(ctx context.Context, printerID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	for _, table := range []string{"assignments", "odometers", "printer_jobs"} {
		if _, err = tx.ExecContext(ctx, `DELETE FROM `+table+` WHERE printer_id = $1`, printerID); err != nil {
			return fmt.Errorf("forget printer %s: %w", printerID, err)
		}
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// Printers answers every printer that holds an assignment or an odometer reading.
func (s *Store) Printers(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT printer_id FROM assignments
		UNION SELECT printer_id FROM odometers ORDER BY printer_id`)
	if err != nil {
		return nil, fmt.Errorf("list the printers with state: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list the printers with state: %w", err)
		}

		out = append(out, id)
	}

	return out, rows.Err()
}

// AddPending counts grams more off a spool that SpoolSync does not hold yet.
func (s *Store) AddPending(ctx context.Context, spoolID string, grams float64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO pending (spool_id, grams) VALUES ($1, $2)
		ON CONFLICT (spool_id) DO UPDATE SET grams = pending.grams + excluded.grams`, spoolID, grams)
	if err != nil {
		return fmt.Errorf("count the filament of spool %s: %w", spoolID, err)
	}

	return nil
}

// Pending answers the grams counted off a spool and not yet written, 0 for none.
func (s *Store) Pending(ctx context.Context, spoolID string) (float64, error) {
	var grams float64

	err := s.db.QueryRowContext(ctx, `SELECT grams FROM pending WHERE spool_id = $1`, spoolID).Scan(&grams)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}

	if err != nil {
		return 0, fmt.Errorf("read the pending grams of spool %s: %w", spoolID, err)
	}

	return grams, nil
}

// PendingSpools answers every spool with grams counted and not yet written.
func (s *Store) PendingSpools(ctx context.Context) (map[string]float64, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT spool_id, grams FROM pending WHERE grams > 0`)
	if err != nil {
		return nil, fmt.Errorf("list the pending grams: %w", err)
	}

	defer func() { _ = rows.Close() }()

	out := map[string]float64{}

	for rows.Next() {
		var (
			id    string
			grams float64
		)

		if err = rows.Scan(&id, &grams); err != nil {
			return nil, fmt.Errorf("list the pending grams: %w", err)
		}

		out[id] = grams
	}

	return out, rows.Err()
}

// TakePending writes grams off a spool's pending count, and drops the row
// once nothing is left.
func (s *Store) TakePending(ctx context.Context, spoolID string, grams float64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}

	defer func() { _ = tx.Rollback() }()

	if _, err = tx.ExecContext(ctx, `UPDATE pending SET grams = grams - $1 WHERE spool_id = $2`, grams, spoolID); err != nil {
		return fmt.Errorf("take the pending grams of spool %s: %w", spoolID, err)
	}

	if _, err = tx.ExecContext(ctx, `DELETE FROM pending WHERE spool_id = $1 AND grams <= 0`, spoolID); err != nil {
		return fmt.Errorf("take the pending grams of spool %s: %w", spoolID, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}

	return nil
}

// ClearPending drops the grams counted off a spool that were not written.
func (s *Store) ClearPending(ctx context.Context, spoolID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM pending WHERE spool_id = $1`, spoolID)
	if err != nil {
		return fmt.Errorf("clear the pending grams of spool %s: %w", spoolID, err)
	}

	return nil
}

// Favourites answers the filament types a person starred, sorted.
func (s *Store) Favourites(ctx context.Context, person string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT filament_type_id FROM favourites WHERE person = $1
		ORDER BY filament_type_id`, person)
	if err != nil {
		return nil, fmt.Errorf("list the favourites: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []string

	for rows.Next() {
		var id string
		if err = rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("list the favourites: %w", err)
		}

		out = append(out, id)
	}

	return out, rows.Err()
}

// SetFavourite stars or unstars a filament type for a person. Starring one twice changes nothing.
func (s *Store) SetFavourite(ctx context.Context, person, filamentTypeID string, on bool) error {
	var err error
	if on {
		_, err = s.db.ExecContext(ctx, `INSERT INTO favourites (person, filament_type_id) VALUES ($1, $2)
			ON CONFLICT (person, filament_type_id) DO NOTHING`, person, filamentTypeID)
	} else {
		_, err = s.db.ExecContext(ctx, `DELETE FROM favourites WHERE person = $1 AND filament_type_id = $2`,
			person, filamentTypeID)
	}

	if err != nil {
		return fmt.Errorf("keep the favourite of %s: %w", person, err)
	}

	return nil
}

// ListFilters answers the list filters a person kept, and false for none.
func (s *Store) ListFilters(ctx context.Context, person string) (string, bool, error) {
	var filters string

	err := s.db.QueryRowContext(ctx, `SELECT filters FROM list_filters WHERE person = $1`, person).Scan(&filters)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("read the list filters of %s: %w", person, err)
	}

	return filters, true, nil
}

// SetListFilters keeps the list filters of a person, as the JSON text given.
func (s *Store) SetListFilters(ctx context.Context, person, filters string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO list_filters (person, filters) VALUES ($1, $2)
		ON CONFLICT (person) DO UPDATE SET filters = excluded.filters`, person, filters)
	if err != nil {
		return fmt.Errorf("keep the list filters of %s: %w", person, err)
	}

	return nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// StartJob holds the print running on a printer, replacing any before it.
func (s *Store) StartJob(ctx context.Context, printerID, jobID string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO printer_jobs (printer_id, job_id) VALUES ($1, $2)
		ON CONFLICT (printer_id) DO UPDATE SET job_id = excluded.job_id`, printerID, jobID)
	if err != nil {
		return fmt.Errorf("hold the print of %s: %w", printerID, err)
	}

	return nil
}

// EndJob lets go of a printer's print, when the one held is jobID.
func (s *Store) EndJob(ctx context.Context, printerID, jobID string) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM printer_jobs WHERE printer_id = $1 AND job_id = $2`,
		printerID, jobID); err != nil {
		return fmt.Errorf("end the print of %s: %w", printerID, err)
	}

	return nil
}

// RunningJob answers the print a printer runs, and false for none.
func (s *Store) RunningJob(ctx context.Context, printerID string) (string, bool, error) {
	var job string

	err := s.db.QueryRowContext(ctx, `SELECT job_id FROM printer_jobs WHERE printer_id = $1`, printerID).Scan(&job)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}

	if err != nil {
		return "", false, fmt.Errorf("read the print of %s: %w", printerID, err)
	}

	return job, true, nil
}

// AddJobUsage counts grams more that a print took off a spool on a tool.
func (s *Store) AddJobUsage(ctx context.Context, jobID, printerID, tool, spoolID string, grams float64) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO job_usage (job_id, printer_id, tool, spool_id, grams)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (job_id, tool, spool_id) DO UPDATE SET grams = job_usage.grams + excluded.grams`,
		jobID, printerID, tool, spoolID, grams)
	if err != nil {
		return fmt.Errorf("count the filament of print %s: %w", jobID, err)
	}

	return nil
}

// JobUse is the grams one print took off one spool on one tool.
type JobUse struct {
	JobID, PrinterID, Tool, SpoolID string
	Grams                           float64
}

// JobUsage answers what the prints took, tool by tool and spool by spool, for
// the prints named. A print with no row took nothing this module counted.
func (s *Store) JobUsage(ctx context.Context, jobIDs []string) ([]JobUse, error) {
	if len(jobIDs) == 0 {
		return nil, nil
	}

	marks := make([]string, len(jobIDs))
	args := make([]any, len(jobIDs))

	for i, id := range jobIDs {
		marks[i] = fmt.Sprintf("$%d", i+1)
		args[i] = id
	}

	rows, err := s.db.QueryContext(ctx, `SELECT job_id, printer_id, tool, spool_id, grams FROM job_usage
		WHERE job_id IN (`+strings.Join(marks, ", ")+`) ORDER BY job_id, tool, spool_id`, args...)
	if err != nil {
		return nil, fmt.Errorf("read the filament of the prints: %w", err)
	}

	defer func() { _ = rows.Close() }()

	var out []JobUse

	for rows.Next() {
		var u JobUse
		if err := rows.Scan(&u.JobID, &u.PrinterID, &u.Tool, &u.SpoolID, &u.Grams); err != nil {
			return nil, fmt.Errorf("read the filament of the prints: %w", err)
		}

		out = append(out, u)
	}

	return out, rows.Err()
}
