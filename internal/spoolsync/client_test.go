package spoolsync

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFindAndSetWeight(t *testing.T) {
	var put map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer ss_test" {
			w.WriteHeader(http.StatusUnauthorized)

			return
		}

		switch r.Method + " " + r.URL.Path {
		case "GET /api/v1/spools/by-opt/bf63e92d-9ca5-53d7-9fab-ffdd0240c585":
			_, _ = w.Write([]byte(`{"spoolId":"s1","currentWeight_g":1100,"filamentType":{"spoolWeight_g":200}}`))
		case "PUT /api/v1/spools/s1/update-weight":
			_ = json.NewDecoder(r.Body).Decode(&put)
			_, _ = w.Write([]byte(`{}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c, err := New(srv.URL+"/", "ss_test", srv.Client())
	if err != nil {
		t.Fatal(err)
	}

	sp, err := c.FindByInstanceUUID(t.Context(), "bf63e92d-9ca5-53d7-9fab-ffdd0240c585")
	if err != nil {
		t.Fatal(err)
	}

	if sp.ID != "s1" || sp.CurrentWeightG != 1100 || sp.FilamentType.SpoolWeightG != 200 {
		t.Fatalf("read %+v", sp)
	}

	if err = c.SetWeight(t.Context(), "s1", 950.5); err != nil {
		t.Fatal(err)
	}

	if put["weight_g"] != 950.5 || put["source"] != Source {
		t.Fatalf("sent %v", put)
	}

	if _, err = c.FindByInstanceUUID(t.Context(), "00000000-0000-0000-0000-000000000000"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("an unknown uuid answered %v, want ErrNotFound", err)
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
