package kubernetes

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"
	"sync"
	"unicode"

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
	cluster := clusterName(testName)
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
		var errs []error
		for _, p := range killList {
			if err := p.Kill(); err != nil {
				errs = append(errs, err)
			}
		}
		if err := run(exec.Command("k3d", "cluster", "delete", cluster), false); err != nil {
			errs = append(errs, err)
		}
		return errors.Join(errs...)
	},
		func(f func(io.ReadCloser, *sync.WaitGroup)) error {
			return fmt.Errorf("compose log reading is not implemented for kubernetes fixtures")
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

	cluster := clusterName(testName)

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
	err = run(
		exec.Command(
			"kubectl",
			"wait",
			"--timeout=5m",
			"--for=condition=available",
			"deployment/lgtm",
		),
		false,
	)
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
	if ports.GrafanaHTTPPort != 0 {
		err = portForward(ports.GrafanaHTTPPort, 3000)
		if err != nil {
			return err
		}
	}
	if ports.OTLPHTTPPort != 0 {
		err = portForward(ports.OTLPHTTPPort, 4318)
		if err != nil {
			return err
		}
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

func clusterName(testName string) string {
	var b strings.Builder
	lastDash := false
	for _, r := range strings.ToLower(testName) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
			lastDash = false
		case r == '-' || r == '_' || unicode.IsSpace(r):
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		default:
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	cluster := strings.Trim(b.String(), "-")
	if cluster == "" {
		cluster = "oats"
	}
	if len(cluster) > 32 {
		cluster = strings.Trim(cluster[len(cluster)-32:], "-")
		if cluster == "" {
			cluster = "oats"
		}
	}
	return cluster
}
