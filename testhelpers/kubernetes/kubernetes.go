package kubernetes

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"sync"

	"github.com/grafana/oats/testhelpers/remote"
)

type Kubernetes struct {
	Dir              string   `yaml:"dir"`
	AppService       string   `yaml:"app-service"`
	AppDockerFile    string   `yaml:"app-docker-file"`
	AppDockerContext string   `yaml:"app-docker-context"`
	AppDockerTag     string   `yaml:"app-docker-tag"`
	AppDockerPort    int      `yaml:"app-docker-port"`
	ImportImages     []string `yaml:"import-images"`
}

func NewEndpoint(host string, model *Kubernetes, ports remote.PortsConfig, testName string, dir string) *remote.Endpoint {
	var killList []*os.Process
	run := func(cmd *exec.Cmd, background bool) error {
		slog.Info("running", "command", cmd.String(), "dir", dir)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Dir = dir
		if background {
			err := cmd.Start()
			if err != nil {
				return err
			}
			killList = append(killList, cmd.Process)
			return nil
		}
		return cmd.Run()
	}
	return remote.NewEndpoint(host, ports, func(ctx context.Context) error {
		return start(model, ports, testName, run)
	}, func(ctx context.Context) error {
		for _, p := range killList {
			err := p.Kill()
			if err != nil {
				return err
			}
		}
		return run(exec.Command("k3d", "cluster", "delete", testName), false)
	},
		func(f func(io.ReadCloser, *sync.WaitGroup)) error {
			panic("not implemented for kubernetes")
		},
	)
}

func start(model *Kubernetes, ports remote.PortsConfig, testName string, run func(cmd *exec.Cmd, background bool) error) error {
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
	if len(cluster) > 32 {
		cluster = cluster[(len(cluster))-32:]
	}

	err = run(exec.Command("k3d", "cluster", "list", cluster), false)
	if err == nil {
		slog.Info("cluster already exists - deleting", "name", cluster)
		err = run(exec.Command("k3d", "cluster", "delete", cluster), false)
		if err != nil {
			return err
		}
	}

	err = run(exec.Command("k3d", "cluster", "create", cluster), false)
	if err != nil {
		return err
	}
	importImages := []string{model.AppDockerTag}
	importImages = append(importImages, model.ImportImages...)
	for _, image := range importImages {
		err = run(exec.Command("k3d", "image", "import", "-c", cluster, image), false)
		if err != nil {
			return err
		}
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
	err = portForward(ports.PyroscopeHttpPort, 4040)
	if err != nil {
		return err
	}
	return nil
}
