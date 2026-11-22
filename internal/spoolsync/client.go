// Package spoolsync is the part of the SpoolSync API this module uses, and the
// shapes it maps SpoolSync's answers to.
//
// SpoolSync weighs a spool gross: the filament and the empty spool, as a scale
// reads it. A Spool here carries both, with the filament left (RemainingG) as
// the number people read.
package spoolsync

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Source is what SpoolSync's weight history records for a reading from here.
const Source = "prusa-connect"

// ErrNotFound is an answer of 404: a spool, filament type or location SpoolSync
// does not hold, such as an OpenPrintTag instance uuid no spool carries.
var ErrNotFound = errors.New("spoolsync: not found")

// maxBody bounds what is read of an answer.
const maxBody = 1 << 20

// StatusError is an answer of SpoolSync that is not 2xx.
type StatusError struct {
	Method, Path string
	Status       int
	// Message is the first line of SpoolSync's answer, cut short.
	Message string
}

func (e *StatusError) Error() string {
	return fmt.Sprintf("spoolsync: %s %s answered %d: %s", e.Method, e.Path, e.Status, e.Message)
}

// Is makes a 404 match ErrNotFound.
func (e *StatusError) Is(target error) bool {
	return target == ErrNotFound && e.Status == http.StatusNotFound
}

// Spool is one spool as this module reads it and shows it.
type Spool struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Brand    string `json:"brand"`
	Material string `json:"material"`
	// Color is the spool's colour as #rrggbb or #rrggbbaa, null when SpoolSync holds none.
	Color *string `json:"color"`
	// RemainingG is the filament left: the gross weight less the empty spool, never below 0.
	RemainingG float64 `json:"remaining_g"`
	GrossG     float64 `json:"gross_g"`
	EmptyG     float64 `json:"empty_g"`
	// InitialG is the filament the spool held when full.
	InitialG float64 `json:"initial_g"`
	// Status is SpoolSync's status: ACTIVE or SPENT.
	Status string `json:"status"`
	// OptInstanceUUID is the OpenPrintTag instance_uuid, null for an untagged spool.
	OptInstanceUUID *string `json:"opt_instance_uuid"`
	// Location is the name of the first storage location the spool is in, null for none.
	Location   *string `json:"location"`
	LocationID *string `json:"location_id"`
	Notes      *string `json:"notes"`
	// FilamentTypeID is the SpoolSync filament type, which is the material here.
	FilamentTypeID string `json:"material_uuid"`
	// Source is "printer" for a spool with an OpenPrintTag instance uuid, else "manual".
	Source string `json:"source"`
	// ArchivedAt is when the spool was archived, null while it is in stock.
	ArchivedAt    *time.Time `json:"archived_at"`
	ArchiveReason *string    `json:"archive_reason"`
	ArchiveNote   *string    `json:"archive_reason_note"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`

	// Filament is the spool's filament type, for the density and diameter of
	// the odometer count. It is not part of the wire shape.
	Filament FilamentType `json:"-"`
}

// FilamentType is a filament type as this module reads and shows it.
type FilamentType struct {
	ID       string  `json:"id"`
	Brand    string  `json:"brand"`
	Material string  `json:"material"`
	Name     string  `json:"name"`
	Color    *string `json:"color"`
	// DiameterMM is the filament's diameter, 1.75 by default in SpoolSync.
	DiameterMM float64 `json:"diameter_mm"`
	// DensityG is the density in g/cm³, null when SpoolSync holds none.
	DensityG *float64 `json:"density_g_cm3"`
	// SpoolWeightG is the weight of the empty spool.
	SpoolWeightG float64 `json:"spool_weight_g"`
	// NetWeightG is the filament a new spool of the type holds: the
	// OpenPrintTag package's nominal weight, null when SpoolSync knows none.
	NetWeightG *float64 `json:"net_weight_g"`
	// OptMaterialUUID is the OpenPrintTag material uuid, null for a custom one.
	OptMaterialUUID *string `json:"opt_material_uuid"`
	// Custom is true for a filament type a person made.
	Custom bool `json:"custom"`
	// ReorderThresholdG is SpoolSync's reorder threshold, null for none.
	ReorderThresholdG *float64 `json:"reorder_threshold_g"`
	Notes             *string  `json:"notes"`
}

// SyncStatus is the last OpenPrintTag import SpoolSync ran.
type SyncStatus struct {
	State      string     `json:"state"`
	StartedAt  *time.Time `json:"started_at"`
	FinishedAt *time.Time `json:"finished_at"`
	Imported   int        `json:"imported"`
	Updated    int        `json:"updated"`
	Skipped    int        `json:"skipped"`
	Error      *string    `json:"error"`
}

// Location is a storage location of SpoolSync.
type Location struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// NewFilamentType is what making a custom filament type sends.
type NewFilamentType struct {
	Brand, Material, Name    string
	Color                    *string
	DiameterMM, SpoolWeightG float64
	DensityG                 *float64
	Notes                    *string
}

// HistoryPoint is one weight reading of a spool.
type HistoryPoint struct {
	ID         string    `json:"id"`
	WeightG    float64   `json:"weight_g"`
	Source     string    `json:"source"`
	RecordedAt time.Time `json:"recorded_at"`
}

// ListQuery is what GET /spools asks for. SpoolSync leaves spent spools out
// unless IncludeSpent is set.
type ListQuery struct {
	Search       string
	IncludeSpent bool
	Page         int
	PageSize     int
}

// SpoolPage is one page of spools. Total counts every spool the query matched.
type SpoolPage struct {
	Items    []Spool
	Total    int
	Page     int
	PageSize int
}

// NewSpool is what creating a spool sends. InitialWeightG is gross.
type NewSpool struct {
	FilamentTypeID  string
	InitialWeightG  float64
	LocationID      string
	OptInstanceUUID string
	Notes           *string
}

// SpoolUpdate is what editing a spool sends. A nil field is left as it is. A
// LocationID of "" clears the location.
type SpoolUpdate struct {
	Status         *string
	LocationID     *string
	CurrentWeightG *float64
	Notes          *string
	// Archive, when set, archives the spool with this reason and note, or
	// unarchives it when Archive is false.
	Archive       *bool
	ArchiveReason string
	ArchiveNote   string
}

// Client calls one SpoolSync server with one API token.
type Client struct {
	base  string
	token string
	http  *http.Client
}

// New returns a client of the server at base, such as https://spools.lan.
// The API is under /api/v1 there.
func New(base, token string, hc *http.Client) (*Client, error) {
	u, err := url.Parse(strings.TrimRight(base, "/"))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("spoolsync: %q is not an http(s) address", base)
	}

	if token == "" {
		return nil, errors.New("spoolsync: no API token")
	}

	if hc == nil {
		hc = &http.Client{Timeout: 15 * time.Second}
	}

	return &Client{base: u.String() + "/api/v1", token: token, http: hc}, nil
}

// List is one page of spools.
func (c *Client) List(ctx context.Context, q ListQuery) (SpoolPage, error) {
	v := url.Values{}
	if q.Search != "" {
		v.Set("search", q.Search)
	}

	if q.IncludeSpent {
		v.Set("includeSpent", "true")
	}

	if q.Page > 0 {
		v.Set("page", strconv.Itoa(q.Page))
	}

	if q.PageSize > 0 {
		v.Set("pageSize", strconv.Itoa(q.PageSize))
	}

	var page struct {
		Items    []wireSpool `json:"items"`
		Total    int         `json:"total"`
		Page     int         `json:"page"`
		PageSize int         `json:"pageSize"`
	}

	if err := c.do(ctx, http.MethodGet, "/spools", v, nil, &page); err != nil {
		return SpoolPage{}, err
	}

	out := SpoolPage{Items: make([]Spool, 0, len(page.Items)), Total: page.Total, Page: page.Page, PageSize: page.PageSize}
	for _, w := range page.Items {
		out.Items = append(out.Items, w.spool())
	}

	return out, nil
}

// Get is one spool by its SpoolSync id.
func (c *Client) Get(ctx context.Context, id string) (Spool, error) {
	var w wireSpool
	if err := c.do(ctx, http.MethodGet, "/spools/"+url.PathEscape(id), nil, nil, &w); err != nil {
		return Spool{}, err
	}

	return w.spool(), nil
}

// FindByInstanceUUID is the spool whose tag has this instance_uuid, or ErrNotFound.
func (c *Client) FindByInstanceUUID(ctx context.Context, uuid string) (Spool, error) {
	var w wireSpool
	if err := c.do(ctx, http.MethodGet, "/spools/by-opt/"+url.PathEscape(uuid), nil, nil, &w); err != nil {
		return Spool{}, err
	}

	return w.spool(), nil
}

// Create adds a spool. SpoolSync takes the gross weight, which the caller
// works out from the filament type's empty spool.
func (c *Client) Create(ctx context.Context, n NewSpool) (Spool, error) {
	body := map[string]any{
		"filamentTypeId":  n.FilamentTypeID,
		"initialWeight_g": n.InitialWeightG,
	}

	if n.LocationID != "" {
		body["storageLocationId"] = n.LocationID
	}

	if n.Notes != nil {
		body["notes"] = *n.Notes
	}

	if n.OptInstanceUUID != "" {
		body["optInstanceUuid"] = n.OptInstanceUUID
	}

	var w wireSpool
	if err := c.do(ctx, http.MethodPost, "/spools", nil, body, &w); err != nil {
		return Spool{}, err
	}

	return w.spool(), nil
}

// Update edits a spool and answers it as it is then.
func (c *Client) Update(ctx context.Context, id string, u SpoolUpdate) (Spool, error) {
	body := map[string]any{}

	if u.Status != nil {
		body["status"] = *u.Status
	}

	if u.LocationID != nil {
		if *u.LocationID == "" {
			body["storageLocationId"] = nil
		} else {
			body["storageLocationId"] = *u.LocationID
		}
	}

	if u.CurrentWeightG != nil {
		body["currentWeight_g"] = *u.CurrentWeightG
	}

	if u.Notes != nil {
		body["notes"] = *u.Notes
	}

	if u.Archive != nil {
		if *u.Archive {
			body["archivedAt"] = time.Now().UTC().Format(time.RFC3339)
			body["archiveReason"] = u.ArchiveReason
			body["archiveNote"] = u.ArchiveNote
		} else {
			body["archivedAt"] = nil
			body["archiveReason"] = nil
			body["archiveNote"] = nil
		}
	}

	var w wireSpool
	if err := c.do(ctx, http.MethodPut, "/spools/"+url.PathEscape(id), nil, body, &w); err != nil {
		return Spool{}, err
	}

	return w.spool(), nil
}

// SetWeight sets the gross weight of a spool, recorded as a reading of source.
func (c *Client) SetWeight(ctx context.Context, spoolID string, grossG float64, source string) error {
	body := map[string]any{"weight_g": grossG, "source": source}

	return c.do(ctx, http.MethodPut, "/spools/"+url.PathEscape(spoolID)+"/update-weight", nil, body, nil)
}

// History is the weight readings of a spool, oldest first, over the last 90 days.
func (c *Client) History(ctx context.Context, id string) ([]HistoryPoint, error) {
	var wire []struct {
		ID         string    `json:"id"`
		WeightG    float64   `json:"weight_g"`
		Source     string    `json:"source"`
		RecordedAt time.Time `json:"recordedAt"`
	}

	if err := c.do(ctx, http.MethodGet, "/spools/"+url.PathEscape(id)+"/history", nil, nil, &wire); err != nil {
		return nil, err
	}

	out := make([]HistoryPoint, 0, len(wire))
	for _, h := range wire {
		out = append(out, HistoryPoint{ID: h.ID, WeightG: h.WeightG, Source: h.Source, RecordedAt: h.RecordedAt})
	}

	return out, nil
}

// FilamentType is one filament type by its id.
func (c *Client) FilamentType(ctx context.Context, id string) (FilamentType, error) {
	var w wireFilamentType
	if err := c.do(ctx, http.MethodGet, "/filament-types/"+url.PathEscape(id), nil, nil, &w); err != nil {
		return FilamentType{}, err
	}

	return w.filamentType(), nil
}

// TypeQuery is one page of filament types, as GET /filament-types/page reads it.
// Every field is optional.
type TypeQuery struct {
	Search, Brand, Material string
	// Source is "openprinttag", "custom" or "catalogue".
	Source string
	// IDs keeps only these types; Exclude leaves these out.
	IDs, Exclude []string
	// OrderBy is "name", "brand" or "material".
	OrderBy       string
	Desc          bool
	Offset, Limit int
}

// TypePage is one page of filament types and how many matched in all.
type TypePage struct {
	Items []FilamentType
	Total int
}

// FilamentTypePage reads one page of filament types. After a catalogue import
// SpoolSync holds some 16,500, so nothing reads them all.
func (c *Client) FilamentTypePage(ctx context.Context, q TypeQuery) (TypePage, error) {
	v := url.Values{}
	set := func(k, val string) {
		if val != "" {
			v.Set(k, val)
		}
	}
	set("search", q.Search)
	set("brand", q.Brand)
	set("material", q.Material)
	set("source", q.Source)
	set("ids", strings.Join(q.IDs, ","))
	set("exclude", strings.Join(q.Exclude, ","))
	set("orderBy", q.OrderBy)

	if q.Desc {
		v.Set("desc", "true")
	}

	v.Set("offset", strconv.Itoa(max(q.Offset, 0)))
	v.Set("limit", strconv.Itoa(min(max(q.Limit, 1), maxTypePage)))

	var wire struct {
		Items []wireFilamentType `json:"items"`
		Total int                `json:"total"`
	}

	if err := c.do(ctx, http.MethodGet, "/filament-types/page", v, nil, &wire); err != nil {
		return TypePage{}, err
	}

	out := TypePage{Total: wire.Total, Items: make([]FilamentType, 0, len(wire.Items))}
	for _, w := range wire.Items {
		out.Items = append(out.Items, w.filamentType())
	}

	return out, nil
}

// maxTypePage is the most filament types SpoolSync answers on one page.
const maxTypePage = 200

// FilamentTypesByID reads these filament types, a page of them at a time.
func (c *Client) FilamentTypesByID(ctx context.Context, ids []string) ([]FilamentType, error) {
	var out []FilamentType

	for start := 0; start < len(ids); start += maxTypePage {
		chunk := ids[start:min(start+maxTypePage, len(ids))]

		page, err := c.FilamentTypePage(ctx, TypeQuery{IDs: chunk, Limit: maxTypePage})
		if err != nil {
			return nil, err
		}

		out = append(out, page.Items...)
	}

	return out, nil
}

// FilamentTypeByOpt is the filament type with this OpenPrintTag material uuid,
// or ErrNotFound.
func (c *Client) FilamentTypeByOpt(ctx context.Context, materialUUID string) (FilamentType, error) {
	var w wireFilamentType
	if err := c.do(ctx, http.MethodGet, "/filament-types/by-opt/"+url.PathEscape(materialUUID), nil, nil, &w); err != nil {
		return FilamentType{}, err
	}

	return w.filamentType(), nil
}

// FilamentKinds is every distinct material SpoolSync's types name, such as PLA.
func (c *Client) FilamentKinds(ctx context.Context) ([]string, error) {
	var out []string

	err := c.do(ctx, http.MethodGet, "/filament-types/kinds", nil, nil, &out)

	return out, err
}

// AllSpools reads every spool SpoolSync holds, spent ones included, a page at a time.
func (c *Client) AllSpools(ctx context.Context) ([]Spool, error) {
	var out []Spool

	for page := 1; ; page++ {
		p, err := c.List(ctx, ListQuery{IncludeSpent: true, Page: page, PageSize: 200})
		if err != nil {
			return nil, err
		}

		out = append(out, p.Items...)
		if len(out) >= p.Total || len(p.Items) == 0 {
			return out, nil
		}
	}
}

// DeleteSpool takes a spool out of SpoolSync.
func (c *Client) DeleteSpool(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/spools/"+url.PathEscape(id), nil, nil, nil)
}

// FilamentTypeByID is one filament type with the fields the module shows.
func (c *Client) FilamentTypeByID(ctx context.Context, id string) (FilamentType, error) {
	return c.FilamentType(ctx, id)
}

// CreateFilamentType makes a custom filament type.
func (c *Client) CreateFilamentType(ctx context.Context, n NewFilamentType) (FilamentType, error) {
	body := map[string]any{
		"brand": n.Brand, "material": n.Material, "name": n.Name,
		"diameter_mm": n.DiameterMM, "spoolWeight_g": n.SpoolWeightG,
	}

	if n.Color != nil {
		body["colorHex"] = *n.Color
	}

	if n.DensityG != nil {
		body["density_g_cm3"] = *n.DensityG
	}

	if n.Notes != nil {
		body["notes"] = *n.Notes
	}

	var w wireFilamentType
	if err := c.do(ctx, http.MethodPost, "/filament-types", nil, body, &w); err != nil {
		return FilamentType{}, err
	}

	return w.filamentType(), nil
}

// UpdateFilamentType edits a custom filament type with the fields given.
func (c *Client) UpdateFilamentType(ctx context.Context, id string, body map[string]any) (FilamentType, error) {
	var w wireFilamentType
	if err := c.do(ctx, http.MethodPut, "/filament-types/"+url.PathEscape(id), nil, body, &w); err != nil {
		return FilamentType{}, err
	}

	return w.filamentType(), nil
}

// DeleteFilamentType takes a filament type out of SpoolSync.
func (c *Client) DeleteFilamentType(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/filament-types/"+url.PathEscape(id), nil, nil, nil)
}

// Brands lists the brands of the filament types SpoolSync holds, by name.
func (c *Client) Brands(ctx context.Context) ([]string, error) {
	var out []string

	err := c.do(ctx, http.MethodGet, "/filament-types/brands", nil, nil, &out)

	return out, err
}

// StartSync starts the OpenPrintTag import, and answers the state it left.
func (c *Client) StartSync(ctx context.Context) (SyncStatus, error) {
	var st SyncStatus
	err := c.do(ctx, http.MethodPost, "/filament-types/sync-openprinttag", nil, map[string]any{}, &st)

	return st, err
}

// SyncState is the state of the last OpenPrintTag import.
func (c *Client) SyncState(ctx context.Context) (SyncStatus, error) {
	var st SyncStatus
	err := c.do(ctx, http.MethodGet, "/filament-types/sync-openprinttag", nil, nil, &st)

	return st, err
}

// CreateLocation makes a storage location. The token needs storage:create for it.
// UpdateLocation renames a storage location.
func (c *Client) UpdateLocation(ctx context.Context, id, name string) (Location, error) {
	var wire struct {
		ID   string `json:"storageLocationId"`
		Name string `json:"name"`
	}

	if err := c.do(ctx, http.MethodPut, "/storage-locations/"+url.PathEscape(id), nil, map[string]any{"name": name}, &wire); err != nil {
		return Location{}, err
	}

	return Location{ID: wire.ID, Name: wire.Name}, nil
}

// DeleteLocation removes a storage location; its spools keep none.
func (c *Client) DeleteLocation(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/storage-locations/"+url.PathEscape(id), nil, nil, nil)
}

// FilamentTypes searches the filament types SpoolSync holds, at most 50 of them.
func (c *Client) FilamentTypes(ctx context.Context, search string) ([]FilamentType, error) {
	var wire []wireFilamentType

	v := url.Values{}
	if search != "" {
		v.Set("q", search)
	}

	if err := c.do(ctx, http.MethodGet, "/filament-types/search", v, nil, &wire); err != nil {
		return nil, err
	}

	out := make([]FilamentType, 0, len(wire))
	for _, w := range wire {
		out = append(out, w.filamentType())
	}

	return out, nil
}

// Locations lists the storage locations. The token needs storage:view for it.
func (c *Client) Locations(ctx context.Context) ([]Location, error) {
	var wire []struct {
		ID   string `json:"storageLocationId"`
		Name string `json:"name"`
	}

	if err := c.do(ctx, http.MethodGet, "/storage-locations", nil, nil, &wire); err != nil {
		return nil, err
	}

	out := make([]Location, 0, len(wire))
	for _, l := range wire {
		out = append(out, Location{ID: l.ID, Name: l.Name})
	}

	return out, nil
}

// CreateLocation makes a storage location. The token needs storage:create for it.
func (c *Client) CreateLocation(ctx context.Context, name string) (Location, error) {
	var wire struct {
		ID   string `json:"storageLocationId"`
		Name string `json:"name"`
	}

	if err := c.do(ctx, http.MethodPost, "/storage-locations", nil, map[string]any{"name": name}, &wire); err != nil {
		return Location{}, err
	}

	return Location{ID: wire.ID, Name: wire.Name}, nil
}

// wireSpool is a spool as SpoolSync answers it, with its filament type and
// storage locations included.
type wireSpool struct {
	SpoolID         string              `json:"spoolId"`
	FilamentTypeID  string              `json:"filamentTypeId"`
	InitialWeightG  float64             `json:"initialWeight_g"`
	CurrentWeightG  float64             `json:"currentWeight_g"`
	Status          string              `json:"status"`
	OptInstanceUUID *string             `json:"optInstanceUuid"`
	Notes           *string             `json:"notes"`
	ArchivedAt      *time.Time          `json:"archivedAt"`
	ArchiveReason   *string             `json:"archiveReason"`
	ArchiveNote     *string             `json:"archiveNote"`
	CreatedAt       time.Time           `json:"createdAt"`
	UpdatedAt       time.Time           `json:"updatedAt"`
	FilamentType    wireFilamentType    `json:"filamentType"`
	Storage         []wireSpoolLocation `json:"storageLocations"`
}

type wireSpoolLocation struct {
	StorageLocation struct {
		ID   string `json:"storageLocationId"`
		Name string `json:"name"`
	} `json:"storageLocation"`
}

type wireFilamentType struct {
	ID                string   `json:"filamentTypeId"`
	Name              string   `json:"name"`
	Brand             string   `json:"brand"`
	Color             *string  `json:"color"`
	ColorHex          *string  `json:"colorHex"`
	Material          string   `json:"material"`
	DiameterMM        float64  `json:"diameter_mm"`
	DensityG          *float64 `json:"density_g_cm3"`
	SpoolWeightG      float64  `json:"spoolWeight_g"`
	NetWeightG        *float64 `json:"netWeight_g"`
	OptMaterialUUID   *string  `json:"optMaterialUuid"`
	Custom            bool     `json:"custom"`
	ReorderThresholdG *float64 `json:"reorderThreshold_g"`
	Notes             *string  `json:"notes"`
}

func (w wireFilamentType) filamentType() FilamentType {
	return FilamentType{
		ID: w.ID, Brand: w.Brand, Material: w.Material, Name: w.Name, Color: w.ColorHex,
		DiameterMM: w.DiameterMM, DensityG: w.DensityG, SpoolWeightG: w.SpoolWeightG, NetWeightG: w.NetWeightG,
		OptMaterialUUID: w.OptMaterialUUID, Custom: w.Custom, ReorderThresholdG: w.ReorderThresholdG, Notes: w.Notes,
	}
}

func (w wireSpool) spool() Spool {
	ft := w.FilamentType.filamentType()
	empty := ft.SpoolWeightG

	var location, locationID *string
	if len(w.Storage) > 0 && w.Storage[0].StorageLocation.ID != "" {
		name, id := w.Storage[0].StorageLocation.Name, w.Storage[0].StorageLocation.ID
		location, locationID = &name, &id
	}

	source := "manual"
	if w.OptInstanceUUID != nil && *w.OptInstanceUUID != "" {
		source = "printer"
	}

	parts := []string{ft.Brand, ft.Material}
	if w.FilamentType.Color != nil && *w.FilamentType.Color != "" {
		parts = append(parts, *w.FilamentType.Color)
	}

	var name strings.Builder

	for _, p := range parts {
		if p == "" {
			continue
		}

		if name.Len() > 0 {
			name.WriteByte(' ')
		}

		name.WriteString(p)
	}

	return Spool{
		ID:              w.SpoolID,
		Name:            name.String(),
		Brand:           ft.Brand,
		Material:        ft.Material,
		Color:           ft.Color,
		RemainingG:      math.Max(0, w.CurrentWeightG-empty),
		GrossG:          w.CurrentWeightG,
		EmptyG:          empty,
		InitialG:        math.Max(0, w.InitialWeightG-empty),
		Status:          w.Status,
		OptInstanceUUID: w.OptInstanceUUID,
		Location:        location,
		LocationID:      locationID,
		Notes:           w.Notes,
		FilamentTypeID:  w.FilamentTypeID,
		Source:          source,
		ArchivedAt:      w.ArchivedAt,
		ArchiveReason:   w.ArchiveReason,
		ArchiveNote:     w.ArchiveNote,
		CreatedAt:       w.CreatedAt,
		UpdatedAt:       w.UpdatedAt,
		Filament:        ft,
	}
}

func (c *Client) do(ctx context.Context, method, path string, query url.Values, in, out any) error {
	var reader io.Reader

	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("spoolsync: encode: %w", err)
		}

		reader = bytes.NewReader(b)
	}

	target := c.base + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}

	req, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return fmt.Errorf("spoolsync: %w", err)
	}

	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")

	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("spoolsync: %s %s: %w", method, path, err)
	}

	defer func() { _ = resp.Body.Close() }()

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return fmt.Errorf("spoolsync: read %s %s: %w", method, path, err)
	}

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return &StatusError{Method: method, Path: path, Status: resp.StatusCode, Message: firstLine(b)}
	}

	if out == nil {
		return nil
	}

	if err = json.Unmarshal(b, out); err != nil {
		return fmt.Errorf("spoolsync: decode %s %s: %w", method, path, err)
	}

	return nil
}

func firstLine(b []byte) string {
	s, _, _ := strings.Cut(strings.TrimSpace(string(b)), "\n")
	if len(s) > 200 {
		s = s[:200]
	}

	return s
}
