package legacyyaml

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/onsi/gomega"
)

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
