package fixture

import (
	"context"
	"fmt"
	"os/exec"

	"github.com/grafana/oats/discovery"
	"github.com/grafana/oats/runner"
	"github.com/grafana/oats/testhelpers/remote"
)

func startK3D(ctx context.Context, plan discovery.Plan) (Handle, Runtime, error) {
	ports, err := allocateK3DPorts()
	if err != nil {
		return nil, Runtime{}, err
	}
	ep := newKubernetesEndpoint(plan, ports)
	if err := ep.Start(ctx); err != nil {
		return nil, Runtime{}, err
	}
	rt := Runtime{
		GrafanaURL:       fmt.Sprintf("http://localhost:%d", ports.GrafanaHTTPPort),
		OTLPHTTP:         fmt.Sprintf("http://localhost:%d", ports.OTLPHTTPPort),
		PyroscopeURL:     fmt.Sprintf("http://localhost:%d", ports.PyroscopeHttpPort),
		CustomCheckEnv:   k3dCheckEnv(runner.Endpoint{AppHost: "localhost", AppPort: plan.Fixture.AppPort}, ports),
		ParallelSafe:     false,
		ParallelDisabled: "k3d fixtures currently use shared clusters and kubectl port-forwards",
	}
	cfg, cfgErr := writeLocalGCXConfig(rt.GrafanaURL)
	if cfgErr != nil {
		_ = ep.Stop(context.Background())
		return nil, Runtime{}, fmt.Errorf("write local gcx config: %w", cfgErr)
	}
	rt.GCXConfig = cfg
	return endpointFixture{ep: ep, cleanup: func() error { return removeIfExists(cfg) }}, rt, nil
}

func k3dCheckEnv(ep runner.Endpoint, ports remote.PortsConfig) []string {
	return []string{
		"OATS_FIXTURE_TYPE=k3d",
		"OATS_APP_URL=" + fmt.Sprintf("http://%s:%d", ep.AppHost, ep.AppPort),
		"OATS_GRAFANA_URL=" + fmt.Sprintf("http://localhost:%d", ports.GrafanaHTTPPort),
		"OATS_OTLP_HTTP=" + fmt.Sprintf("http://localhost:%d", ports.OTLPHTTPPort),
		"OATS_PYROSCOPE_URL=" + fmt.Sprintf("http://localhost:%d", ports.PyroscopeHttpPort),
	}
}

func allocateK3DPorts() (remote.PortsConfig, error) {
	grafanaPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	otlpHTTPPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	lokiPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	promPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	tempoPort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	pyroscopePort, err := findFreePort()
	if err != nil {
		return remote.PortsConfig{}, err
	}
	return remote.PortsConfig{
		GrafanaHTTPPort:    grafanaPort,
		OTLPHTTPPort:       otlpHTTPPort,
		LokiHttpPort:       lokiPort,
		PrometheusHTTPPort: promPort,
		TempoHTTPPort:      tempoPort,
		PyroscopeHttpPort:  pyroscopePort,
	}, nil
}

func readK3DGrafanaToken() (string, error) {
	cmd := exec.Command("kubectl", "exec", "deploy/lgtm", "--", "sh", "-c", "cat /tmp/grafana-sa-token 2>/dev/null || true")
	out, err := cmd.Output()
	if err != nil {
		return "", err
	}
	return string(out), nil
}
