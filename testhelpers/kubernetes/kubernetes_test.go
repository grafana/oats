package kubernetes

import (
	"os/exec"
	"testing"

	"github.com/grafana/oats/testhelpers/remote"
	"github.com/stretchr/testify/require"
)

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
