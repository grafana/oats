package legacyyaml

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestReadTestCaseDefinition(t *testing.T) {
	def, err := readTestCaseDefinition("testdata/valid-tests/oats.yaml", false)
	require.NoError(t, err)
	merged, err := readTestCaseDefinition("testdata/oats-merged.yaml", false)
	require.NoError(t, err)
	require.Equal(t, merged, def)
}

func TestReadTestCase(t *testing.T) {
	tc, err := readTestCase("testdata", "testdata/valid-tests/oats.yaml")
	require.NoError(t, err)
	require.Equal(t, "runvalid-tests-oats", tc.Name)
	expectedDir, err := absolutePath("testdata/valid-tests")
	require.NoError(t, err)
	require.Equal(t, expectedDir, tc.Dir)
}

func TestIncludePath(t *testing.T) {
	require.Equal(t,
		filepath.FromSlash("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/oats-non-reactive.yaml"),
		includePath("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/spring-boot-non-reactive-2.7/oats.yaml", "../oats-non-reactive.yaml"))
}

func TestInputDefinitionsAreCorrect(t *testing.T) {
	def, err := readTestCaseDefinition("testdata/valid-tests/input.oats.yaml", false)
	require.NoError(t, err)

	expected := &model.TestCaseDefinition{
		Input: []model.Input{
			{
				Path: "/stock",
			},
			{
				Path:   "/buy",
				Method: "POST",
				Headers: map[string]string{
					"Authorization": "Bearer user-token",
					"Content-Type":  "application/json",
				},
				Body:   `{"id": "42", "quantity": 10}`,
				Status: "201",
			},
			{
				Path:   "/delist/42",
				Scheme: "https",
				Host:   "127.0.0.1",
				Method: "DELETE",
				Headers: map[string]string{
					"Authorization": "Bearer admin-token",
				},
				Status: "204",
			},
		},
	}

	require.Equal(t, expected.Input, def.Input)
}
func TestInputDefinitionsInvalidFiles(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		errorMsg string
	}{
		{
			name:     "malformed yaml",
			filePath: "testdata/invalid-tests/malformed-yaml.yaml",
			errorMsg: "failed to parse file \".*/legacyyaml/testdata/invalid-tests/malformed-yaml.yaml\": yaml: mapping values are not allowed in this context",
		},
		{
			name:     "outdated file version",
			filePath: "testdata/invalid-tests/outdated-version.yaml",
			errorMsg: "error parsing test case definition .*/legacyyaml/testdata/invalid-tests/outdated-version.yaml - " +
				"see migration notes at https://github.com/grafana/oats/blob/main/UPGRADING.md - unsupported oats-schema-version '1' required version is '2'",
		},
		{
			name:     "file version is not a number",
			filePath: "testdata/invalid-tests/version-not-int.yaml",
			errorMsg: "error parsing test case definition .*/legacyyaml/testdata/invalid-tests/version-not-int.yaml - " +
				"see migration notes at https://github.com/grafana/oats/blob/main/UPGRADING.md - oats-schema-version '1' is not a number",
		},
		{
			name:     "unknown field",
			filePath: "testdata/invalid-tests/unknown-field.yaml",
			errorMsg: "error parsing test case definition .*/legacyyaml/testdata/invalid-tests/unknown-field.yaml - " +
				"see migration notes at https://github.com/grafana/oats/blob/main/UPGRADING.md - yaml: unmarshal errors:\n" +
				".*line 5: field spans not found in type model.ExpectedTraces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readTestCaseDefinition(tt.filePath, false)
			require.NotNil(t, err)
			require.Regexp(t, tt.errorMsg, err.Error())
		})
	}
}

func TestCollectTestCases(t *testing.T) {
	testCases := []struct {
		name               string
		input              []string
		evaluateIgnoreFile bool
		expectedNames      []string
	}{
		{
			name:               "without ignore file evaluation",
			input:              []string{"testdata/valid-tests"},
			evaluateIgnoreFile: false,
			expectedNames: []string{
				"run-expect-absent.oats",
				"run-input.oats",
				"run-more-oats",
				"run-oats",
				"run-matrix-test.oats-docker",       // matrix expansion
				"run-matrix-test.oats-k8s",          // matrix expansion
				"runignored-should-not-appear.oats", // included when not evaluating ignore
			},
		},
		{
			name:               "with ignore file evaluation",
			input:              []string{"testdata/valid-tests"},
			evaluateIgnoreFile: true,
			expectedNames: []string{
				"run-expect-absent.oats",
				"run-input.oats",
				"run-more-oats",
				"run-oats",
				"run-matrix-test.oats-docker", // matrix expansion
				"run-matrix-test.oats-k8s",    // matrix expansion
			},
		},
		{
			name:               "2 explicit files",
			input:              []string{"testdata/valid-tests/oats.yaml", "testdata/valid-tests/more-oats.yml"},
			evaluateIgnoreFile: true,
			expectedNames: []string{
				"run-oats",
				"run-more-oats",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cases, err := ReadTestCases(tc.input, tc.evaluateIgnoreFile)
			require.NoError(t, err)

			// Collect all case names for easier assertion
			actualNames := make([]string, len(cases))
			for i, c := range cases {
				actualNames[i] = c.Name
			}

			// Check that all expected names are present
			require.ElementsMatch(t, tc.expectedNames, actualNames)
			require.Len(t, actualNames, len(tc.expectedNames))
		})
	}
}

func TestTestCasesAreValid(t *testing.T) {
	cases, err := ReadTestCases([]string{"testdata/valid-tests"}, false)
	require.NoError(t, err)
	require.NotEmpty(t, cases)
	for _, c := range cases {
		require.NotEqual(t, nil, c.Definition)
		require.NotEmpty(t, c.Definition.Input)
		model.ValidateInput(gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		}), c.Definition.Input)
	}
}

func TestLegacyQueryHelpers(t *testing.T) {
	if got := replaceVariables("up{job=\"$job\",instance=\"$instance\"}"); got != `up{job=".*",instance=".*"}` {
		t.Fatalf("replaceVariables = %q", got)
	}

	r := &Runner{testCase: &model.TestCase{MatrixTestCaseName: "linux"}}
	for condition, want := range map[string]bool{
		"":          true,
		"linux":     true,
		"windows":   false,
		"linux|mac": true,
	} {
		if got := r.MatchesMatrixCondition(condition, "query"); got != want {
			t.Errorf("MatchesMatrixCondition(%q) = %v, want %v", condition, got, want)
		}
	}
	r.testCase.MatrixTestCaseName = ""
	if !r.MatchesMatrixCondition("linux", "query") {
		t.Fatal("matrix condition should be ignored outside a matrix case")
	}
}

func TestLegacySignalAssertionsAgainstHTTP(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/loki/api/v1/query_range":
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"result":[{"stream":{"service":"api"},"values":[["1","ready"]]}]}}`)
		case "/api/v1/query":
			_, _ = fmt.Fprint(w, `{"status":"success","data":{"resultType":"vector","result":[{"metric":{"job":"oats"},"value":[1,"2"]}]}}`)
		case "/pyroscope/render":
			_, _ = fmt.Fprint(w, `{"flamebearer":{"names":["main","worker"]}}`)
		case "/api/search":
			_, _ = fmt.Fprint(w, `{"traces":[]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	parsed, err := url.Parse(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal(err)
	}

	r := &Runner{
		endpoint: remote.NewEndpoint(parsed.Hostname(), remote.PortsConfig{
			PrometheusHTTPPort: port,
			LokiHTTPPort:       port,
			TempoHTTPPort:      port,
			PyroscopeHTTPPort:  port,
		}, nil, nil, nil),
		gomegaInst: gomega.NewGomega(func(message string, _ ...int) { t.Error(message) }),
	}

	AssertLoki(r, model.ExpectedLogs{LogQL: "{service=\"api\"}", Signal: model.ExpectedSignal{
		NameEquals: "ready",
		Count:      &model.ExpectedRange{Min: 1, Max: 1},
		Attributes: map[string]string{"service": "api"},
	}})
	AssertProm(r, "$job", ">= 1")
	AssertPyroscope(r, model.ExpectedProfiles{Query: "process_cpu", Flamebearers: model.Flamebearers{NameRegexp: "work"}})
	AssertTempo(r, model.ExpectedTraces{TraceQL: "{ .service.name = \"missing\" }", Signal: model.ExpectedSignal{
		Count: &model.ExpectedRange{Min: 0, Max: 0},
	}})
}

func TestAssertPyroscopeResponseRejectsMalformedJSON(t *testing.T) {
	failed := false
	r := &Runner{gomegaInst: gomega.NewGomega(func(string, ...int) { failed = true })}
	assertPyroscopeResponse([]byte("not json"), model.ExpectedProfiles{}, r)
	if !failed {
		t.Fatal("malformed Pyroscope response did not fail")
	}
}

func TestLegacyPublicHelpers(t *testing.T) {
	def, err := LoadTestCaseDefinition("testdata/valid-tests/oats.yaml")
	if err != nil || def == nil {
		t.Fatalf("LoadTestCaseDefinition = %#v, %v", def, err)
	}

	dir := PrepareBuildDir("coverage-helper")
	if !strings.HasSuffix(dir, "build/coverage-helper") {
		t.Fatalf("PrepareBuildDir = %q", dir)
	}
	if err := os.RemoveAll(dir); err != nil {
		t.Fatal(err)
	}
}

func TestLegacyRunnerPollingHelpers(t *testing.T) {
	r := NewRunner(&model.TestCase{
		Name: "polling helper",
		Definition: model.TestCaseDefinition{
			Interval: time.Millisecond,
		},
	}, model.Settings{
		Timeout:        100 * time.Millisecond,
		PresentTimeout: 10 * time.Millisecond,
		AbsentTimeout:  10 * time.Millisecond,
		LogLimit:       8,
	})
	r.deadline = time.Now().Add(time.Second)
	r.eventually(func() {})
	r.consistently(func() {})
	r.assertSignal(model.ExpectedSignal{}, "query", func() {}, func() {})

	r.Verbose = true
	r.LogQueryResult("this message is deliberately longer than the limit")
	r.gomegaInst = gomega.NewGomega(func(message string, _ ...int) { t.Error(message) })
	r.assertCustomCheck(model.CustomCheck{Script: "true"})
}

func TestCreateDockerComposeFileWithFakeDocker(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("test uses a POSIX executable")
	}

	bin := t.TempDir()
	docker := filepath.Join(bin, "docker")
	if err := os.WriteFile(docker, []byte("#!/bin/sh\nprintf 'services: {}\\n'\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))

	outputDir := t.TempDir()
	r := &Runner{
		testCase: &model.TestCase{
			OutputDir: outputDir,
			PortConfig: &model.PortConfig{
				ApplicationPort:    8080,
				GrafanaHTTPPort:    3000,
				PrometheusHTTPPort: 9090,
				LokiHTTPPort:       3100,
				TempoHTTPPort:      3200,
				PyroscopeHttpPort:  4040,
			},
			Definition: model.TestCaseDefinition{DockerCompose: &model.DockerCompose{}},
		},
		Settings: model.Settings{LgtmVersion: "latest"},
	}
	gomega.RegisterTestingT(t)
	path := CreateDockerComposeFile(r)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("generated compose file: %v", err)
	}
	if string(data) != "services: {}\n" {
		t.Fatalf("generated compose = %q", data)
	}
}
