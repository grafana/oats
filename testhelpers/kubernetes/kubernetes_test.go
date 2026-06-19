package kubernetes

import (
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/grafana/oats/testhelpers/remote"
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
		"fg: kubectl wait --timeout=5m --for=condition=ready pod -l app=lgtm",
		"bg: kubectl port-forward service/dice 18080:8080",
		"bg: kubectl port-forward service/lgtm 13100:3100",
		"bg: kubectl port-forward service/lgtm 19090:9090",
		"bg: kubectl port-forward service/lgtm 13200:3200",
		"bg: kubectl port-forward service/lgtm 14040:4040",
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("unexpected command sequence:\n got: %#v\nwant: %#v", calls, want)
	}
}
