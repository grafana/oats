package kubernetes

import (
	"context"
	"fmt"
	"github.com/grafana/oats/testhelpers/remote"
	"io"
	"os/exec"
)

type Kubernetes struct {
	Dir              string `yaml:"dir"`
	AppService       string `yaml:"app-service"`
	AppDockerFile    string `yaml:"app-docker-file"`
	AppDockerContext string `yaml:"app-docker-context"`
	AppDockerTag     string `yaml:"app-docker-tag"`
	AppDockerPort    int    `yaml:"app-docker-port"`
}

func NewEndpoint(model *Kubernetes, ports remote.PortsConfig, logger io.WriteCloser, testName string, dir string) *remote.Endpoint {
	run := func(cmd *exec.Cmd, background bool) error {
		_, _ = fmt.Fprintf(logger, "Running: %s\n", cmd.String())
		cmd.Stdout = logger
		cmd.Stderr = logger
		cmd.Dir = dir
		if background {
			err := cmd.Start()
			if err != nil {
				return err
			}
			return cmd.Process.Release()
		}
		return cmd.Run()
	}
	return remote.NewEndpoint(ports, func(ctx context.Context) error {
		return start(model, ports, testName, run, logger)
	}, func(ctx context.Context) error {
		return run(exec.Command("k3d", "cluster", "delete", testName), false)
	})
}

func start(model *Kubernetes, ports remote.PortsConfig, testName string, run func(cmd *exec.Cmd, background bool) error, logger io.WriteCloser) error {
	portForward := func(localPort int, remotePort int) error {
		cmd := exec.Command("kubectl", "port-forward", "service/lgtm", fmt.Sprintf("%d:%d", localPort, remotePort))
		return run(cmd, true)
	}

	if model.AppDockerContext == "" {
		model.AppDockerContext = "."
	}

	err := run(exec.Command("docker", "build", "-f", model.AppDockerFile, "-t", model.AppDockerTag, model.AppDockerContext), false)
	if err != nil {
		return err
	}

	cluster := testName
	err = run(exec.Command("k3d", "cluster", "create", cluster), false)
	if err != nil {
		_, _ = fmt.Fprintf(logger, "Failed to create cluster (it probably already exists) %s: %v\n", cluster, err)
	}

	err = run(exec.Command("k3d", "image", "import", "-c", cluster, model.AppDockerTag), false)
	if err != nil {
		return err
	}
	err = run(exec.Command("kubectl", "apply", "-f", model.Dir), false)
	if err != nil {
		return err
	}
	err = run(exec.Command("kubectl", "wait", "--timeout=5m", "--for=condition=ready", "pod", "-l", "app=lgtm"), false)
	if err != nil {
		return err
	}
	err = run(exec.Command("kubectl", "port-forward", "service/"+model.AppService, fmt.Sprintf("%d:8080", model.AppDockerPort)), true)
	if err != nil {
		return err
	}

	err = portForward(ports.LokiHttpPort, 3100)
	if err != nil {
		return err
	}
	err = portForward(ports.PrometheusHTTPPort, 9090)
	if err != nil {
		return err
	}
	err = portForward(ports.TempoHTTPPort, 3200)
	if err != nil {
		return err
	}
	return nil
}
