package kubernetes

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/grafana/oats/testhelpers/remote"
	"github.com/stretchr/testify/require"
)

func TestClusterName_TruncatesFromEnd(t *testing.T) {
	in := "this-is-a-very-long-suite-name-that-exceeds-thirty-two-chars"
	got := clusterName(in)
	if len(got) != 32 {
		t.Fatalf("cluster length = %d, want 32 (%q)", len(got), got)
	}
	if !strings.HasSuffix(in, got) {
		t.Fatalf("expected %q to be the suffix of %q", got, in)
	}
}

func TestClusterName_NormalizesRFC1123(t *testing.T) {
	if got := clusterName("logging k8s probe"); got != "logging-k8s-probe" {
		t.Fatalf("clusterName() = %q, want %q", got, "logging-k8s-probe")
	}
	if got := clusterName("!!!"); got != "oats" {
		t.Fatalf("clusterName() fallback = %q, want %q", got, "oats")
	}
}

func TestStart_DefaultDockerContextAndCommandSequence(t *testing.T) {
	model := &Kubernetes{
		Dir:           "k8s",
		AppService:    "dice",
		AppDockerFile: "Dockerfile",
		AppDockerTag:  "dice:test",
		AppDockerPort: 18080,
		ImportImages:  []string{"busybox:latest"},
	}
	ports := remote.PortsConfig{
		GrafanaHTTPPort:    13000,
		OTLPHTTPPort:       14318,
		PrometheusHTTPPort: 19090,
		LokiHttpPort:       13100,
		TempoHTTPPort:      13200,
		PyroscopeHttpPort:  14040,
	}

	var calls []string
	run := func(cmd *exec.Cmd, background bool) error {
		mode := "fg"
		if background {
			mode = "bg"
		}
		calls = append(calls, mode+": "+strings.Join(cmd.Args, " "))
		return nil
	}

	if err := start(model, ports, "smoke-suite", run); err != nil {
		t.Fatalf("start: %v", err)
	}
	if model.AppDockerContext != "." {
		t.Fatalf("expected default docker context '.', got %q", model.AppDockerContext)
	}

	want := []string{
		"fg: docker build -f Dockerfile -t dice:test .",
		"fg: k3d cluster list smoke-suite",
		"fg: k3d cluster delete smoke-suite",
		"fg: k3d cluster create smoke-suite",
		"fg: k3d image import -c smoke-suite dice:test",
		"fg: k3d image import -c smoke-suite busybox:latest",
		"fg: kubectl apply -f k8s",
		"fg: kubectl wait --timeout=5m --for=condition=available deployment/lgtm",
		"bg: kubectl port-forward service/dice 18080:8080",
		"bg: kubectl port-forward service/lgtm 13100:3100",
		"bg: kubectl port-forward service/lgtm 13000:3000",
		"bg: kubectl port-forward service/lgtm 14318:4318",
		"bg: kubectl port-forward service/lgtm 19090:9090",
		"bg: kubectl port-forward service/lgtm 13200:3200",
		"bg: kubectl port-forward service/lgtm 14040:4040",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected command sequence:\n got: %#v\nwant: %#v", calls, want)
	}
}

func TestStartWaitsForLgtmDeploymentAvailability(t *testing.T) {
	t.Parallel()

	model := &Kubernetes{
		Dir:              "k8s",
		AppService:       "dice",
		AppDockerFile:    "Dockerfile",
		AppDockerContext: ".",
		AppDockerTag:     "dice:1.1-SNAPSHOT",
		AppDockerPort:    8080,
	}
	ports := remote.PortsConfig{
		GrafanaHTTPPort:    3000,
		OTLPHTTPPort:       4318,
		LokiHttpPort:       3100,
		PrometheusHTTPPort: 9090,
		TempoHTTPPort:      3200,
		PyroscopeHttpPort:  4040,
	}

	var commands [][]string
	run := func(cmd *exec.Cmd, background bool) error {
		commands = append(commands, append([]string(nil), cmd.Args...))
		return nil
	}

	err := start(model, ports, "run-oats", run)
	require.NoError(t, err)

	require.Contains(t, commands, []string{
		"kubectl",
		"wait",
		"--timeout=5m",
		"--for=condition=available",
		"deployment/lgtm",
	})
}

func TestStart_FallsBackToLegacyGrafanaAndOTLPPorts(t *testing.T) {
	t.Parallel()

	model := &Kubernetes{
		Dir:              "k8s",
		AppService:       "dice",
		AppDockerFile:    "Dockerfile",
		AppDockerContext: ".",
		AppDockerTag:     "dice:test",
		AppDockerPort:    18080,
	}
	ports := remote.PortsConfig{
		LokiHttpPort:       3100,
		PrometheusHTTPPort: 9090,
		TempoHTTPPort:      3200,
		PyroscopeHttpPort:  4040,
	}

	var calls []string
	run := func(cmd *exec.Cmd, background bool) error {
		mode := "fg"
		if background {
			mode = "bg"
		}
		calls = append(calls, mode+": "+strings.Join(cmd.Args, " "))
		return nil
	}

	if err := start(model, ports, "legacy-ports", run); err != nil {
		t.Fatalf("start: %v", err)
	}
	if !contains(calls, "bg: kubectl port-forward service/lgtm 3000:3000") {
		t.Fatalf("expected Grafana legacy fallback port-forward, got %#v", calls)
	}
	if !contains(calls, "bg: kubectl port-forward service/lgtm 4318:4318") {
		t.Fatalf("expected OTLP legacy fallback port-forward, got %#v", calls)
	}
}

func contains(items []string, want string) bool {
	for _, item := range items {
		if item == want {
			return true
		}
	}
	return false
}
