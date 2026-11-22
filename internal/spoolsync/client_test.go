package spoolsync

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

// spoolJSON is a spool as SpoolSync answers GET /spools/:id: the filament type
// and the storage locations included.
const spoolJSON = `{
	"spoolId":"s1","filamentTypeId":"ft1","initialWeight_g":1400,"currentWeight_g":1100,
	"status":"ACTIVE","optInstanceUuid":"bf63e92d-9ca5-53d7-9fab-ffdd0240c585",
	"updatedAt":"2026-11-20T10:00:00.000Z",
	"filamentType":{"filamentTypeId":"ft1","name":"Prusament PLA","brand":"Prusament","material":"PLA",
		"color":"Galaxy Black","colorHex":"#3d3e3dff","diameter_mm":1.75,"density_g_cm3":1.24,"spoolWeight_g":200},
	"storageLocations":[{"storageLocation":{"storageLocationId":"l1","name":"Shelf A"}}]
}`

// fakeSpoolSync answers the routes this module calls, and checks the token.
func fakeSpoolSync(t *testing.T, seen *[]string) *httptest.Server {
	t.Helper()

	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ss_test" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		key := r.Method + " " + r.URL.Path
		*seen = append(*seen, key+"?"+r.URL.RawQuery)

		switch key {
		case "GET /api/v1/spools/by-opt/bf63e92d-9ca5-53d7-9fab-ffdd0240c585",
			"GET /api/v1/spools/s1":
			_, _ = w.Write([]byte(spoolJSON))
		case "GET /api/v1/spools/by-opt/00000000-0000-0000-0000-000000000000",
			"GET /api/v1/spools/gone":
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte(`{"message":"no"}`))
		case "GET /api/v1/spools":
			_, _ = w.Write([]byte(`{"items":[` + spoolJSON + `],"total":41,"page":2,"pageSize":25}`))
		case "PUT /api/v1/spools/s1/update-weight":
			_, _ = w.Write([]byte(`{}`))
		case "PUT /api/v1/spools/s1":
			_, _ = w.Write([]byte(spoolJSON))
		case "POST /api/v1/spools":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(spoolJSON))
		case "GET /api/v1/spools/s1/history":
			_, _ = w.Write([]byte(`[{"id":"h1","weight_g":1100,"source":"manual","recordedAt":"2026-11-20T10:00:00.000Z"}]`))
		case "GET /api/v1/filament-types/ft1":
			_, _ = w.Write([]byte(`{"filamentTypeId":"ft1","name":"Prusament PLA","brand":"Prusament","material":"PLA","colorHex":null,"diameter_mm":2.85,"density_g_cm3":null,"spoolWeight_g":250}`))
		case "GET /api/v1/filament-types/search":
			_, _ = w.Write([]byte(`[{"filamentTypeId":"ft1","name":"Prusament PLA","brand":"Prusament","material":"PLA","colorHex":null,"diameter_mm":1.75,"density_g_cm3":null,"spoolWeight_g":200}]`))
		case "GET /api/v1/storage-locations":
			_, _ = w.Write([]byte(`[{"storageLocationId":"l1","name":"Shelf A"}]`))
		case "POST /api/v1/storage-locations":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"storageLocationId":"l2","name":"Dryer"}`))
		default:
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"message":"no route ` + key + `"}`))
		}
	}))
}

func newClient(t *testing.T, srv *httptest.Server) *Client {
	t.Helper()

	c, err := New(srv.URL+"/", "ss_test", srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	return c
}

// A spool maps to the plan's shape: remaining is gross less the empty spool.
func TestGetMapsASpoolToThePlanShape(t *testing.T) {
	var seen []string

	srv := fakeSpoolSync(t, &seen)
	defer srv.Close()

	c := newClient(t, srv)

	sp, err := c.FindByInstanceUUID(t.Context(), "bf63e92d-9ca5-53d7-9fab-ffdd0240c585")
	if err != nil {
		t.Fatal(err)
	}

	if sp.ID != "s1" || sp.Name != "Prusament PLA Galaxy Black" || sp.Brand != "Prusament" || sp.Material != "PLA" {
		t.Fatalf("name and brand: %+v", sp)
	}

	if sp.GrossG != 1100 || sp.EmptyG != 200 || sp.RemainingG != 900 || sp.InitialG != 1200 {
		t.Fatalf("weights: gross %v empty %v remaining %v initial %v", sp.GrossG, sp.EmptyG, sp.RemainingG, sp.InitialG)
	}

	if sp.Color == nil || *sp.Color != "#3d3e3dff" {
		t.Fatalf("colour %v", sp.Color)
	}

	if sp.OptInstanceUUID == nil || *sp.OptInstanceUUID != "bf63e92d-9ca5-53d7-9fab-ffdd0240c585" {
		t.Fatalf("instance uuid %v", sp.OptInstanceUUID)
	}

	if sp.Location == nil || *sp.Location != "Shelf A" {
		t.Fatalf("location %v", sp.Location)
	}

	if sp.Filament.DiameterMM != 1.75 || sp.Filament.DensityG == nil || *sp.Filament.DensityG != 1.24 {
		t.Fatalf("filament %+v", sp.Filament)
	}
}

// Remaining never goes below 0, however light the spool reads.
func TestRemainingIsFlooredAtZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"spoolId":"s1","initialWeight_g":1000,"currentWeight_g":150,"status":"SPENT",
			"filamentType":{"spoolWeight_g":200,"brand":"B","material":"PLA"},"storageLocations":[]}`))
	}))
	defer srv.Close()

	sp, err := newClient(t, srv).Get(t.Context(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	if sp.RemainingG != 0 || sp.InitialG != 800 || sp.Location != nil || sp.Color != nil {
		t.Fatalf("got %+v", sp)
	}
}

func TestSetWeightSendsTheGrossAndTheSource(t *testing.T) {
	var seen []string

	srv := fakeSpoolSync(t, &seen)
	defer srv.Close()

	var sent map[string]any

	inner := srv.Config.Handler
	srv.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/api/v1/spools/s1/update-weight" {
			_ = json.NewDecoder(r.Body).Decode(&sent)
		}

		inner.ServeHTTP(w, r)
	})

	if err := newClient(t, srv).SetWeight(t.Context(), "s1", 1012, Source); err != nil {
		t.Fatal(err)
	}

	if sent["weight_g"] != 1012.0 || sent["source"] != "prusa-connect" {
		t.Fatalf("sent %v", sent)
	}
}

func TestUnknownUUIDIsNotFoundAndOtherRefusalsCarryTheStatus(t *testing.T) {
	var seen []string

	srv := fakeSpoolSync(t, &seen)
	defer srv.Close()

	c := newClient(t, srv)

	if _, err := c.FindByInstanceUUID(t.Context(), "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown uuid answered %v, want ErrNotFound", err)
	}

	_, err := c.Get(t.Context(), "nope")

	var status *StatusError
	if !errors.As(err, &status) || status.Status != http.StatusBadRequest {
		t.Fatalf("an unknown route answered %v, want a status error of 400", err)
	}
}

func TestListSendsTheQueryAndMapsThePage(t *testing.T) {
	var seen []string

	srv := fakeSpoolSync(t, &seen)
	defer srv.Close()

	page, err := newClient(t, srv).List(t.Context(), ListQuery{Search: "galaxy", IncludeSpent: true, Page: 2, PageSize: 25})
	if err != nil {
		t.Fatal(err)
	}

	if page.Total != 41 || page.Page != 2 || page.PageSize != 25 || len(page.Items) != 1 || page.Items[0].ID != "s1" {
		t.Fatalf("page %+v", page)
	}

	got, query, _ := strings.Cut(seen[len(seen)-1], "?")
	if got != "GET /api/v1/spools" {
		t.Fatalf("asked %q", got)
	}

	v, _ := url.ParseQuery(query)
	if v.Get("search") != "galaxy" || v.Get("includeSpent") != "true" || v.Get("page") != "2" || v.Get("pageSize") != "25" {
		t.Fatalf("query %q", query)
	}
}

func TestCreateUpdateAndHistory(t *testing.T) {
	var seen []string

	srv := fakeSpoolSync(t, &seen)
	defer srv.Close()

	c := newClient(t, srv)

	if _, err := c.Create(t.Context(), NewSpool{FilamentTypeID: "ft1", InitialWeightG: 1400, LocationID: "l1"}); err != nil {
		t.Fatal(err)
	}

	status := "SPENT"
	if _, err := c.Update(t.Context(), "s1", SpoolUpdate{Status: &status}); err != nil {
		t.Fatal(err)
	}

	hist, err := c.History(t.Context(), "s1")
	if err != nil {
		t.Fatal(err)
	}

	if len(hist) != 1 || hist[0].WeightG != 1100 || hist[0].Source != "manual" {
		t.Fatalf("history %+v", hist)
	}
}

// The body of an update names only the fields it changes, and an empty
// location clears the link with null.
func TestUpdateSendsOnlyTheChangedFields(t *testing.T) {
	var sent []map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)

		var m map[string]any
		_ = json.Unmarshal(b, &m)
		sent = append(sent, m)
		_, _ = w.Write([]byte(spoolJSON))
	}))
	defer srv.Close()

	c := newClient(t, srv)
	empty := ""

	if _, err := c.Update(t.Context(), "s1", SpoolUpdate{LocationID: &empty}); err != nil {
		t.Fatal(err)
	}

	if len(sent) != 1 || len(sent[0]) != 1 {
		t.Fatalf("sent %v", sent)
	}

	if v, ok := sent[0]["storageLocationId"]; !ok || v != nil {
		t.Fatalf("the location is %v, want null", sent[0])
	}
}

func TestFilamentTypesAndLocations(t *testing.T) {
	var seen []string

	srv := fakeSpoolSync(t, &seen)
	defer srv.Close()

	c := newClient(t, srv)

	ft, err := c.FilamentType(t.Context(), "ft1")
	if err != nil {
		t.Fatal(err)
	}

	if ft.SpoolWeightG != 250 || ft.DiameterMM != 2.85 || ft.DensityG != nil || ft.Color != nil || ft.NetWeightG != nil {
		t.Fatalf("filament type %+v", ft)
	}

	types, err := c.FilamentTypes(t.Context(), "prusa")
	if err != nil || len(types) != 1 || types[0].Brand != "Prusament" {
		t.Fatalf("search %v %+v", err, types)
	}

	locs, err := c.Locations(t.Context())
	if err != nil || len(locs) != 1 || locs[0].Name != "Shelf A" {
		t.Fatalf("locations %v %+v", err, locs)
	}

	made, err := c.CreateLocation(t.Context(), "Dryer")
	if err != nil || made.ID != "l2" {
		t.Fatalf("create location %v %+v", err, made)
	}
}

func TestNewRefusesABadAddressOrNoToken(t *testing.T) {
	for _, tc := range []struct{ base, token string }{
		{"spools.lan", "ss_x"}, {"ftp://spools.lan", "ss_x"}, {"https://spools.lan", ""},
	} {
		if _, err := New(tc.base, tc.token, nil); err == nil {
			t.Fatalf("New(%q, %q) was accepted", tc.base, tc.token)
		}
	}
}
