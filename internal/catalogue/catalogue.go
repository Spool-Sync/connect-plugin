// Package catalogue reads the OpenPrintTag material database into filament
// types, for a module that keeps its inventory itself.
//
// The database is a git repository of YAML files
// (https://github.com/OpenPrintTag/openprinttag-database, MIT). Its ZIP holds
// data/brands, data/materials, data/material-packages and
// data/material-containers. Each FFF material becomes one filament type, keyed
// by its OpenPrintTag material uuid. A file without a uuid gets the one the
// OpenPrintTag specification derives (section 3.2.1).
//
// It is SpoolSync's own importer (backend/src/services/openPrintTagCatalogue.js)
// in Go, so both read the same catalogue into the same rows. SpoolSync's author,
// who wrote that importer, licenses this port under this module's licence.
package catalogue

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/google/uuid"
	"gopkg.in/yaml.v3"
)

// DefaultURL is the database's main branch as a ZIP.
const DefaultURL = "https://github.com/OpenPrintTag/openprinttag-database/archive/refs/heads/main.zip"

// The bounds on what is read of somebody else's file.
const (
	maxZipBytes   = 64 << 20
	maxEntries    = 50_000
	maxFileBytes  = 1 << 20
	maxTotalBytes = 64 << 20
)

// The namespaces the specification derives brand and material uuids in.
var (
	brandNS    = uuid.MustParse("5269dfb7-1559-440a-85be-aba5f3eff2d2")
	materialNS = uuid.MustParse("616fc86d-7d99-4953-96c7-46d2836b9be9")
)

// Material is one filament type the catalogue holds.
type Material struct {
	OptMaterialUUID string
	Brand, Name     string
	// Material is the kind, such as PLA: the abbreviation, else the type.
	Material   string
	ColorHex   *string
	DensityG   *float64
	DiameterMM float64
	// SpoolWeightG is the empty spool, nil when the catalogue names none.
	SpoolWeightG *float64
	// NetWeightG is the filament a new spool holds, nil when unknown.
	NetWeightG *float64
}

// Read reads the database ZIP into materials, and counts the files it skipped.
func Read(b []byte) ([]Material, int, error) {
	if len(b) > maxZipBytes {
		return nil, 0, errors.New("the catalogue is larger than 64 MiB")
	}

	zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
	if err != nil {
		return nil, 0, fmt.Errorf("the catalogue is not a ZIP: %w", err)
	}

	if len(zr.File) > maxEntries {
		return nil, 0, errors.New("the catalogue holds too many files")
	}

	top, ok := topOf(zr.File)
	if !ok {
		return nil, 0, errors.New("the ZIP has no data/manifest.yaml: not the OpenPrintTag database")
	}

	var (
		r     reader
		total int64
	)

	prefix := top + "data/"

	for _, f := range zr.File {
		if f.FileInfo().IsDir() || !strings.HasPrefix(f.Name, prefix) || !strings.HasSuffix(f.Name, ".yaml") {
			continue
		}

		folder, _, _ := strings.Cut(strings.TrimPrefix(f.Name, prefix), "/")
		if !folders[folder] {
			continue
		}

		body, err := readFile(f)
		if err != nil {
			r.skipped++

			continue
		}

		if total += int64(len(body)); total > maxTotalBytes {
			return nil, 0, errors.New("the catalogue unpacks to more than 64 MiB")
		}

		r.add(folder, body)
	}

	out := r.materials()
	if len(r.brands) == 0 || len(out) == 0 {
		return nil, 0, errors.New("the catalogue holds no brands or materials")
	}

	return out, r.skipped, nil
}

// Download fetches the ZIP at url, which must be https.
func Download(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	if !strings.HasPrefix(url, "https://") {
		return nil, errors.New("the catalogue address must be https")
	}

	if client == nil {
		client = &http.Client{Timeout: 5 * time.Minute}
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("the catalogue download failed: %w", err)
	}

	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("the catalogue download answered %d", resp.StatusCode)
	}

	b, err := io.ReadAll(io.LimitReader(resp.Body, maxZipBytes+1))
	if err != nil {
		return nil, fmt.Errorf("the catalogue download failed: %w", err)
	}

	if len(b) > maxZipBytes {
		return nil, errors.New("the catalogue is larger than 64 MiB")
	}

	return b, nil
}

//nolint:gochecknoglobals // the folders read; read-only.
var folders = map[string]bool{"brands": true, "materials": true, "material-packages": true, "material-containers": true}

// topOf is the top directory of a GitHub archive: the one holding data/manifest.yaml.
func topOf(files []*zip.File) (string, bool) {
	for _, f := range files {
		if top, ok := strings.CutSuffix(f.Name, "data/manifest.yaml"); ok && (top == "" || strings.Count(top, "/") == 1 && strings.HasSuffix(top, "/")) {
			return top, true
		}
	}

	return "", false
}

func readFile(f *zip.File) ([]byte, error) {
	if f.UncompressedSize64 > maxFileBytes {
		return nil, errors.New("too large")
	}

	rc, err := f.Open()
	if err != nil {
		return nil, err
	}

	defer func() { _ = rc.Close() }()

	b, err := io.ReadAll(io.LimitReader(rc, maxFileBytes+1))
	if err != nil || len(b) > maxFileBytes {
		return nil, errors.New("unreadable")
	}

	return b, nil
}

type slugRef struct {
	Slug string `yaml:"slug"`
}

type yBrand struct {
	UUID string `yaml:"uuid"`
	Slug string `yaml:"slug"`
	Name string `yaml:"name"`
}

type yMaterial struct {
	UUID         string  `yaml:"uuid"`
	Slug         string  `yaml:"slug"`
	Brand        slugRef `yaml:"brand"`
	Name         string  `yaml:"name"`
	Class        string  `yaml:"class"`
	Type         string  `yaml:"type"`
	Abbreviation string  `yaml:"abbreviation"`
	PrimaryColor struct {
		ColorRGBA string `yaml:"color_rgba"`
	} `yaml:"primary_color"`
	Properties struct {
		Density *float64 `yaml:"density"`
	} `yaml:"properties"`
}

type yPackage struct {
	Material  slugRef  `yaml:"material"`
	Container slugRef  `yaml:"container"`
	NetWeight *float64 `yaml:"nominal_netto_full_weight"`
	Diameter  *float64 `yaml:"filament_diameter"`
}

type yContainer struct {
	Slug        string   `yaml:"slug"`
	EmptyWeight *float64 `yaml:"empty_weight"`
}

// reader collects the files of the four folders.
type reader struct {
	brands     []yBrand
	materialsY []yMaterial
	packages   []yPackage
	containers []yContainer
	skipped    int
}

func (r *reader) add(folder string, body []byte) {
	var err error

	switch folder {
	case "brands":
		var y yBrand
		if err = yaml.Unmarshal(body, &y); err == nil {
			r.brands = append(r.brands, y)
		}
	case "materials":
		var y yMaterial
		if err = yaml.Unmarshal(body, &y); err == nil {
			// Resin (class SLA) is no filament: the inventory holds spools.
			if c := strings.TrimSpace(y.Class); c == "" || c == "FFF" {
				r.materialsY = append(r.materialsY, y)
			}
		}
	case "material-packages":
		var y yPackage
		if err = yaml.Unmarshal(body, &y); err == nil {
			r.packages = append(r.packages, y)
		}
	default:
		var y yContainer
		if err = yaml.Unmarshal(body, &y); err == nil {
			r.containers = append(r.containers, y)
		}
	}

	if err != nil {
		r.skipped++
	}
}

var hexColour = regexp.MustCompile(`(?i)^#([0-9a-f]{6})([0-9a-f]{2})?$`)

// hexOf is #rrggbb from an OpenPrintTag color_rgba (#rrggbbaa or #rrggbb), or nil.
func hexOf(rgba string) *string {
	m := hexColour.FindStringSubmatch(strings.TrimSpace(rgba))
	if m == nil {
		return nil
	}

	h := "#" + strings.ToLower(m[1])

	return &h
}

// parsed is a uuid in canonical form, or false for text that is not one.
func parsed(s string) (uuid.UUID, bool) {
	s = strings.TrimSpace(s)
	if len(s) != 36 {
		return uuid.UUID{}, false
	}

	u, err := uuid.Parse(s)

	return u, err == nil
}

func (r *reader) materials() []Material {
	type brand struct {
		id   uuid.UUID
		name string
	}

	brands := map[string]brand{}

	for _, b := range r.brands {
		id, ok := parsed(b.UUID)
		if !ok {
			name := strings.TrimSpace(b.Name)
			if name == "" {
				r.skipped++

				continue
			}

			id = uuid.NewSHA1(brandNS, []byte(name))
		}

		brands[strings.TrimSpace(b.Slug)] = brand{id: id, name: strings.TrimSpace(b.Name)}
	}

	containers := map[string]yContainer{}
	for _, c := range r.containers {
		containers[strings.TrimSpace(c.Slug)] = c
	}

	packages := map[string]yPackage{}

	for _, p := range r.packages {
		slug := strings.TrimSpace(p.Material.Slug)
		if _, seen := packages[slug]; slug != "" && !seen {
			packages[slug] = p
		}
	}

	out := make([]Material, 0, len(r.materialsY))

	for _, m := range r.materialsY {
		name := strings.TrimSpace(m.Name)
		b, hasBrand := brands[strings.TrimSpace(m.Brand.Slug)]

		id, ok := parsed(m.UUID)
		if !ok && hasBrand && name != "" {
			id, ok = uuid.NewSHA1(materialNS, append(b.id[:], []byte(name)...)), true
		}

		if !ok || name == "" {
			r.skipped++

			continue
		}

		row := Material{
			OptMaterialUUID: id.String(),
			Brand:           firstOf(b.name, strings.TrimSpace(m.Brand.Slug), "Unknown"),
			Name:            name,
			Material:        firstOf(strings.TrimSpace(m.Abbreviation), strings.TrimSpace(m.Type), "Other"),
			ColorHex:        hexOf(m.PrimaryColor.ColorRGBA),
			DensityG:        m.Properties.Density,
			DiameterMM:      1.75,
		}

		if p, ok := packages[strings.TrimSpace(m.Slug)]; ok {
			row.NetWeightG = p.NetWeight
			if p.Diameter != nil && *p.Diameter > 0 && *p.Diameter == float64(int(*p.Diameter)) {
				row.DiameterMM = *p.Diameter / 1000
			}

			if c, ok := containers[strings.TrimSpace(p.Container.Slug)]; ok {
				row.SpoolWeightG = c.EmptyWeight
			}
		}

		out = append(out, row)
	}

	return out
}

func firstOf(texts ...string) string {
	for _, t := range texts {
		if t != "" {
			return t
		}
	}

	return ""
}
