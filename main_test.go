package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/prusa3d/prusa-connect/sdk/manifest"
	"gopkg.in/yaml.v3"
)

// TestStatusBeforeConfigure answers that nothing is configured yet.
func TestStatusBeforeConfigure(t *testing.T) {
	recorder := httptest.NewRecorder()
	routes(&running{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/status", nil))

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
	routes(&running{}).ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, "/nothing-here", nil))

	if recorder.Code != http.StatusNotFound {
		t.Fatalf("an unserved path answered %d, want %d", recorder.Code, http.StatusNotFound)
	}

	var problem struct {
		Code string `json:"code"`
	}

	if err := json.Unmarshal(recorder.Body.Bytes(), &problem); err != nil {
		t.Fatalf("the refusal is not JSON: %v", err)
	}

	if want := RouteNotFound.Code().String(); problem.Code != want {
		t.Fatalf("the refusal carries %q, want %q", problem.Code, want)
	}
}

// TestTheManifestPublishesTheCodesDefault holds module.yaml and this code to one default.
func TestTheManifestPublishesTheCodesDefault(t *testing.T) {
	manifest, err := os.ReadFile("module.yaml")
	if err != nil {
		t.Fatalf("read module.yaml: %v", err)
	}

	declared := `default: "` + defaultMaterials + `"`
	if !strings.Contains(string(manifest), declared) {
		t.Fatalf("module.yaml does not declare %s for materials_module", declared)
	}
}

// TestNoSettingIsReadFromTheEnvironment keeps every setting in the manifest.
func TestNoSettingIsReadFromTheEnvironment(t *testing.T) {
	source, err := os.ReadFile("main.go")
	if err != nil {
		t.Fatalf("read main.go: %v", err)
	}

	if strings.Contains(string(source), "os.Getenv") {
		t.Fatal("main.go reads an environment variable. Declare the setting in module.yaml instead")
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
