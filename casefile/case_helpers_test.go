package casefile

import (
	"os"
	"reflect"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestFixtureConfigHelpers(t *testing.T) {
	tests := []struct {
		name             string
		fixture          FixtureConfig
		wantKind         string
		wantManagedApp   bool
		wantRelativePath bool
	}{
		{name: "empty"},
		{
			name: "compose managed app",
			fixture: FixtureConfig{Compose: &ComposeFixture{
				File:       "compose.yml",
				AppService: "app",
				AppPort:    8080,
			}},
			wantKind:         "compose",
			wantManagedApp:   true,
			wantRelativePath: true,
		},
		{
			name: "compose files",
			fixture: FixtureConfig{Compose: &ComposeFixture{
				Files: []string{"base.yml", "override.yml"},
			}},
			wantKind:         "compose",
			wantRelativePath: true,
		},
		{
			name:             "k3d paths",
			fixture:          FixtureConfig{K3D: &K3DFixture{K8sDir: "k8s"}},
			wantKind:         "k3d",
			wantRelativePath: true,
		},
		{
			name:     "remote",
			fixture:  FixtureConfig{Remote: &RemoteFixture{Endpoint: "remote"}},
			wantKind: "remote",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.fixture.Kind(); got != tt.wantKind {
				t.Errorf("Kind() = %q, want %q", got, tt.wantKind)
			}
			if got := tt.fixture.HasManagedApp(); got != tt.wantManagedApp {
				t.Errorf("HasManagedApp() = %v, want %v", got, tt.wantManagedApp)
			}
			if got := tt.fixture.UsesRelativePaths(); got != tt.wantRelativePath {
				t.Errorf("UsesRelativePaths() = %v, want %v", got, tt.wantRelativePath)
			}
		})
	}
}

func TestLoadSetsSourcePath(t *testing.T) {
	path := t.TempDir() + "/case.yaml"
	if err := os.WriteFile(path, []byte(`
name: loaded case
expected:
  logs:
    - logql: '{service_name="app"}'
      contains: ready
`), 0o600); err != nil {
		t.Fatal(err)
	}

	c, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.SourcePath != path {
		t.Fatalf("SourcePath = %q, want %q", c.SourcePath, path)
	}

	if _, err := Load(path + ".missing"); err == nil || !strings.Contains(err.Error(), "casefile load") {
		t.Fatalf("Load missing file error = %v", err)
	}
}

func TestMarshalYAMLHelpers(t *testing.T) {
	stringCases := []struct {
		name string
		in   StringList
		want any
	}{
		{name: "empty", in: StringList{}, want: nil},
		{name: "scalar", in: StringList{"one"}, want: "one"},
		{name: "sequence", in: StringList{"one", "two"}, want: []string{"one", "two"}},
	}
	for _, tt := range stringCases {
		t.Run("strings/"+tt.name, func(t *testing.T) {
			got, err := tt.in.MarshalYAML()
			if err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("MarshalYAML() = %#v, want %#v", got, tt.want)
			}
		})
	}

	attrs, err := (AttributeMatchers{{Key: "service.name"}, {Key: "job", Value: stringPtr("oats")}}).MarshalYAML()
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := yaml.Marshal(attrs)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(encoded), "service.name") || !strings.Contains(string(encoded), "oats") {
		t.Errorf("marshaled attributes = %s", encoded)
	}

	if got, err := (AttributeMatchers{}).MarshalYAML(); err != nil || got != nil {
		t.Fatalf("empty attributes MarshalYAML() = %#v, %v", got, err)
	}
}

func stringPtr(s string) *string { return &s }
