package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"github.com/prusa3d/prusa-connect/sdk/routesapi"
	"gopkg.in/yaml.v3"
)

// TestStatusBeforeConfigure answers that nothing is configured yet.
func TestStatusBeforeConfigure(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&module{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))

	if recorder.Code != http.StatusOK {
		t.Fatalf("GET /status answered %d, want %d", recorder.Code, http.StatusOK)
	}

	var body struct {
		Configured bool `json:"configured"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
		t.Fatalf("the answer is not JSON: %v", err)
	}

	if body.Configured {
		t.Fatal("an unconfigured module says it is configured")
	}
}

// TestAPathThisModuleDoesNotServeIsRefusedByCode reads the code, not the sentence.
func TestAPathThisModuleDoesNotServeIsRefusedByCode(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&service{}).routes().ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nothing-here", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("an unserved path answered %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}

	if problem.Code != NotFound.Name {
		t.Fatalf("the refusal carries %q, want %q", problem.Code, NotFound.Name)
	}
}

// TestAnUnconfiguredModuleRefusesEveryOtherRoute holds the refusal before settings arrive.
func TestAnUnconfiguredModuleRefusesEveryOtherRoute(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&module{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/spools", nil))

	if recorder.Code != NotConfigured.Status || !strings.Contains(recorder.Body.String(), NotConfigured.Name) {
		t.Fatalf("GET /spools answered %d %s", recorder.Code, recorder.Body.String())
	}
}

// TestTheManifestParses holds module.yaml to the SDK's own reader, which is
// what `prusa-connect module add` reads it with.
func TestTheManifestParses(t *testing.T) {
	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	held, err := manifest.Parse("module.yaml", document, func(b []byte, into any) error { return yaml.Unmarshal(b, into) })
	if err != nil {
		t.Fatalf("module.yaml does not parse: %v", err)
	}

	if held.Name != name {
		t.Fatalf("module.yaml names %q, the code %q", held.Name, name)
	}

	// The host refuses a child whose version is not its manifest's. A release
	// build stamps version from the archive's manifest; this default is what a
	// plain go build answers, so it has to be module.yaml's own.
	if string(held.Version) != version {
		t.Fatalf("module.yaml says version %q, main.go defaults to %q", held.Version, version)
	}
}

// TestNoSettingIsReadFromTheEnvironment keeps every setting in the manifest.
func TestNoSettingIsReadFromTheEnvironment(t *testing.T) {
	for _, file := range []string{"main.go", "service.go", "routes.go", "printers.go"} {
		source, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}

		if strings.Contains(string(source), "os.Getenv") {
			t.Fatalf("%s reads an environment variable. Declare the setting in module.yaml instead", file)
		}
	}
}

// TestTheManifestDeclaresEverySettingTheCodeReads holds Settings' keys to module.yaml's options.
func TestTheManifestDeclaresEverySettingTheCodeReads(t *testing.T) {
	document, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatal(err)
	}

	var m struct {
		Options []struct {
			Key string `yaml:"key"`
		} `yaml:"options"`
	}

	if err = yaml.Unmarshal(document, &m); err != nil {
		t.Fatal(err)
	}

	var declared []string
	for _, o := range m.Options {
		declared = append(declared, o.Key)
	}

	for _, want := range []string{"spoolsync_url", "spoolsync_token", "max_spools"} {
		if !slices.Contains(declared, want) {
			t.Fatalf("module.yaml does not declare %s; it declares %v", want, declared)
		}
	}
}

// TestTheOpenAPIFragmentParses holds openapi.json to the SDK's own reader.
func TestTheOpenAPIFragmentParses(t *testing.T) {
	if _, err := routesapi.ParseFragment(openAPI); err != nil {
		t.Fatalf("openapi.json does not parse: %v", err)
	}
}

// TestTheOpenAPIDescribesEveryRoute holds openapi.json to the routes the module serves.
func TestTheOpenAPIDescribesEveryRoute(t *testing.T) {
	var doc struct {
		Paths map[string]map[string]json.RawMessage `json:"paths"`
	}

	if err := json.Unmarshal(openAPI, &doc); err != nil {
		t.Fatal(err)
	}

	described := map[string]bool{}

	for path, ops := range doc.Paths {
		for method := range ops {
			described[strings.ToUpper(method)+" "+path] = true
		}
	}

	for _, route := range served {
		if !described[route] {
			t.Errorf("openapi.json does not describe %s", route)
		}
	}

	if len(described) != len(served) {
		t.Errorf("openapi.json describes %d operations and the module serves %d", len(described), len(served))
	}

}

// TestTheRefusalEnumHoldsEveryCode holds the codes in faults.go to the Refusal enum of openapi.json.
func TestTheRefusalEnumHoldsEveryCode(t *testing.T) {
	var doc struct {
		Components struct {
			Schemas struct {
				Refusal struct {
					Properties struct {
						Code struct {
							Enum []string `json:"enum"`
						} `json:"code"`
					} `json:"properties"`
				} `json:"Refusal"`
			} `json:"schemas"`
		} `json:"components"`
	}

	if err := json.Unmarshal(openAPI, &doc); err != nil {
		t.Fatal(err)
	}

	enum := doc.Components.Schemas.Refusal.Properties.Code.Enum

	for _, c := range allCodes {
		if !slices.Contains(enum, c.Name) {
			t.Errorf("the Refusal enum lacks %s", c.Name)
		}
	}

	if len(enum) != len(allCodes) {
		t.Errorf("the Refusal enum holds %d codes and faults.go %d", len(enum), len(allCodes))
	}
}

// served is every route the module mounts, as openapi.json names it. A route added here
// and not in routes() is caught by the openapi test; one added to routes() and not here
// is caught by TestEveryRouteIsListed.
//
//nolint:gochecknoglobals // the list of routes; read-only.
var served = []string{
	"GET /status",
	"GET /catalogue", "POST /catalogue/download", "GET /catalogue/brands", "GET /types",
	"GET /materials", "GET /materials/{uuid}", "GET /materials/{uuid}/spools",
	"POST /custom-materials", "PATCH /custom-materials/{uuid}", "DELETE /custom-materials/{uuid}",
	"PUT /favourites/{uuid}", "DELETE /favourites/{uuid}",
	"GET /locations", "POST /locations", "PATCH /locations/{id}", "DELETE /locations/{id}",
	"GET /list-filters", "PUT /list-filters",
	"GET /spools", "POST /spools", "PATCH /spools/{id}", "DELETE /spools/{id}", "POST /spools/delete",
	"POST /spools/{id}/merge",
	"GET /printers", "GET /printers/tools", "GET /printers/{device}/tools",
	"PUT /printers/{device}/tools/{tool}", "DELETE /printers/{device}/tools/{tool}",
	"POST /printers/{device}/tools/{tool}/spool",
}
