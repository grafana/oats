package otelcollector

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/template"

	_ "embed"

	"github.com/google/uuid"
	"github.com/grafana/oats/internal/testhelpers/common"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
	"google.golang.org/grpc"
)

const (
	GRPCContainerPort        = "4317/tcp"
	HealthCheckContainerPort = "13133/tcp"
)

//go:embed otelcol.dockerfile
var otelCollectorDockerfileBytes []byte

//go:embed otelcol-builder-manifest.yaml
var builderManifestBytes []byte

func NewLocalEndpoint(ctx context.Context, networkName string, traceEndpoint *common.LocalEndpointAddress, promEndpoint *common.LocalEndpointAddress) (*LocalEndpoint, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if traceEndpoint == nil {
		return nil, fmt.Errorf("address for sending traces to cannot be nil")
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, err
	}

	err = pool.Client.Ping()
	if err != nil {
		return nil, fmt.Errorf("pinging Docker daemon: %s", err.Error())
	}

	fileExporterOutput, err := os.CreateTemp("", "file-exporter-output")
	if err != nil {
		return nil, err
	}

	fileExporterOutputPath := fileExporterOutput.Name()

	err = fileExporterOutput.Close()
	if err != nil {
		return nil, err
	}

	collectorConfig, err := os.CreateTemp("", "otel-collector-config")
	if err != nil {
		_ = os.Remove(fileExporterOutputPath)

		return nil, err
	}

	configData := &ConfigTemplateData{
		TempoEndpoint:          traceEndpoint.ContainerEndpoint,
		PrometheusEndpoint:     promEndpoint.ContainerEndpoint,
		FileExporterOutputPath: "/etc/file-exporter-output.jsonl",
		HealthCheckPort:        strings.ReplaceAll(HealthCheckContainerPort, common.TCPSuffix, ""),
	}

	configTemplate, err := template.New("otel-collector-config").Parse(ConfigTemplate)
	if err != nil {
		_ = collectorConfig.Close()

		_ = os.Remove(collectorConfig.Name())
		_ = os.Remove(fileExporterOutputPath)

		return nil, err
	}

	err = configTemplate.Execute(collectorConfig, configData)
	if err != nil {
		_ = collectorConfig.Close()

		_ = os.Remove(collectorConfig.Name())
		_ = os.Remove(fileExporterOutputPath)

		return nil, err
	}

	collectorConfigPath := collectorConfig.Name()

	err = collectorConfig.Close()
	if err != nil {
		_ = os.Remove(collectorConfig.Name())
		_ = os.Remove(fileExporterOutputPath)

		return nil, err
	}

	endpoint := &LocalEndpoint{
		mutex:                  &sync.Mutex{},
		networkName:            networkName,
		traceEndpoint:          traceEndpoint,
		prometheusEndpoint:     promEndpoint,
		collectorConfigPath:    collectorConfigPath,
		fileExporterOutputPath: fileExporterOutputPath,
		stopped:                true,
	}

	return endpoint, nil
}

type LocalEndpoint struct {
	mutex   *sync.Mutex
	stopped bool

	container              *dockertest.Resource
	networkName            string
	collectorConfigPath    string
	fileExporterOutputPath string
	traceEndpoint          *common.LocalEndpointAddress
	prometheusEndpoint     *common.LocalEndpointAddress
}

func (e *LocalEndpoint) Start(ctx context.Context) (*common.LocalEndpointAddress, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.container != nil && e.networkName != "" {
		existingContainerNetwork, getExistingNetworkErr := common.ContainerNetwork(e.networkName)
		if getExistingNetworkErr != nil {
			e.container.Close()
			e.container = nil

			return nil, fmt.Errorf("getting container network:% s", getExistingNetworkErr)
		}

		existingContainerNetworkIP := e.container.GetIPInNetwork(existingContainerNetwork)
		if existingContainerNetworkIP == "" {
			return nil, fmt.Errorf("got no IP for Tempo container on the shared container network")
		}

		existingContainerPort := strings.ReplaceAll(GRPCContainerPort, common.TCPSuffix, "")

		existingContainerHostEndpoint := e.container.GetHostPort(GRPCContainerPort)
		if existingContainerHostEndpoint == "" {
			return nil, fmt.Errorf("got no host gRPC OpenTelemetry host endpoint for Tempo container")
		}

		endpointAddress := &common.LocalEndpointAddress{
			HostEndpoint:      existingContainerHostEndpoint,
			ContainerEndpoint: fmt.Sprintf("%s:%s", existingContainerNetworkIP, existingContainerPort),
		}

		return endpointAddress, nil
	}

	resChan := make(chan *dockertest.Resource)
	errsChan := make(chan error)

	var funcConfig struct {
		traceEndpointAddress   string
		configPath             string
		fileExporterOutputPath string
		networkName            string
	}

	funcConfig.traceEndpointAddress = e.traceEndpoint.HostEndpoint
	funcConfig.configPath = e.collectorConfigPath
	funcConfig.fileExporterOutputPath = e.fileExporterOutputPath
	funcConfig.networkName = e.networkName

	go func() {
		if ctx.Err() != nil {
			// expect this error to be delivered in the select
			return
		}

		pool, createClientErr := dockertest.NewPool("")
		if createClientErr != nil {
			errsChan <- fmt.Errorf("creating a Docker client: %s", createClientErr)
			return
		}

		pingErr := pool.Client.Ping()
		if pingErr != nil {
			errsChan <- fmt.Errorf("pinging Docker daemon: %s", pingErr.Error())
			return
		}

		conn, dialTraceEndpointErr := grpc.Dial(funcConfig.traceEndpointAddress, grpc.WithInsecure())
		if dialTraceEndpointErr != nil {
			errsChan <- fmt.Errorf("dialing trace endpoint: %s which is assumed to be gRPC: %s", funcConfig.traceEndpointAddress, dialTraceEndpointErr)
			return
		}

		buildDir, createBuildDirErr := os.MkdirTemp("", "opentelemetry-collector-build")
		if createBuildDirErr != nil {
			errsChan <- fmt.Errorf("creating build context for OpenTelemetry Collector: %s", createBuildDirErr)
			return
		}

		defer os.RemoveAll(buildDir)

		dockerfilePath := filepath.Join(buildDir, "Dockerfile")
		builderManifestPath := filepath.Join(buildDir, "otelcol-builder-manifest.yaml")

		writeDockerfileErr := os.WriteFile(dockerfilePath, otelCollectorDockerfileBytes, 0666)
		if writeDockerfileErr != nil {
			errsChan <- writeDockerfileErr
			return
		}

		writeBuilderManifestErr := os.WriteFile(builderManifestPath, builderManifestBytes, 0666)
		if writeBuilderManifestErr != nil {
			errsChan <- writeBuilderManifestErr
			return
		}

		traceEndpointCloseConnErr := conn.Close()
		if traceEndpointCloseConnErr != nil {
			errsChan <- traceEndpointCloseConnErr
			return
		}

		network, err := common.ContainerNetwork(funcConfig.networkName)
		if err != nil {
			errsChan <- fmt.Errorf("getting container network:% s", err)
			return
		}

		hostGRPCContainerPort, getPortErr := common.HostTCPPort()
		if getPortErr != nil {
			errsChan <- getPortErr
			return
		}

		hostHealthCheckContainerPort, getPortErr := common.HostTCPPort()
		if getPortErr != nil {
			errsChan <- getPortErr
			return
		}

		nameUUID, uuidErr := uuid.NewRandom()
		if uuidErr != nil {
			errsChan <- uuidErr
			return
		}

		containerName := fmt.Sprintf("otelcol-ginkgo%s", nameUUID.String())

		options := &dockertest.RunOptions{
			Name:     containerName,
			Cmd:      []string{"/otelcol", "--config=/etc/otel-collector.yaml"}, // attempting to specify the command manually causes otelcol to fail (╯°□°)╯︵ ┻━┻
			Networks: []*dockertest.Network{network},

			Mounts: []string{
				fmt.Sprintf("%s:/etc/otel-collector.yaml:z", funcConfig.configPath),
				fmt.Sprintf("%s:/etc/file-exporter-output.jsonl:z", funcConfig.fileExporterOutputPath), // we'd like this file to be available from the host after creating the volume
			},

			ExposedPorts: []string{
				GRPCContainerPort,
				HealthCheckContainerPort,
			},

			// to update, look at the upstream DockerFile: https://github.com/open-telemetry/opentelemetry-collector-releases/blob/main/distributions/otelcol/Dockerfile
			PortBindings: map[docker.Port][]docker.PortBinding{
				GRPCContainerPort:        []docker.PortBinding{{HostPort: hostGRPCContainerPort}},
				HealthCheckContainerPort: []docker.PortBinding{{HostPort: hostHealthCheckContainerPort}},
			},
		}

		resource, runErr := pool.BuildAndRunWithOptions(dockerfilePath, options)
		if runErr != nil {
			errsChan <- fmt.Errorf("running container: %s", runErr.Error())
			return
		}

		connectionErr := pool.Retry(func() error {
			url := fmt.Sprintf("http://localhost:%s/health/status", resource.GetPort(HealthCheckContainerPort))

			resp, getErr := http.Get(url)
			if getErr != nil {
				return getErr
			}

			if resp.StatusCode != http.StatusOK {
				return fmt.Errorf("expected HTTP status 200, but got: %d", resp.StatusCode)
			}

			defer resp.Body.Close()

			return nil
		})

		if ctx.Err() != nil {
			resource.Close()
			// do not send the context error through the channel because we're going to check if the context is closed
			return
		}

		if connectionErr != nil {
			resource.Close()
			errsChan <- connectionErr
			return
		}

		resChan <- resource
		return
	}()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()

		case containerResource := <-resChan:
			containerNetwork, err := common.ContainerNetwork(e.networkName)
			if err != nil {
				containerResource.Close()
				return nil, fmt.Errorf("getting container network:% s", err)
			}

			containerNetworkIP := containerResource.GetIPInNetwork(containerNetwork)
			if containerNetworkIP == "" {
				containerResource.Close()
				return nil, fmt.Errorf("got no IP for Tempo container on the container network")
			}

			containerPort := strings.ReplaceAll(GRPCContainerPort, common.TCPSuffix, "")

			hostEndpoint := containerResource.GetHostPort(GRPCContainerPort)
			if hostEndpoint == "" {
				return nil, fmt.Errorf("got no host gRPC OpenTelemetry host endpoint for Tempo container")
			}

			endpointAddress := &common.LocalEndpointAddress{
				HostEndpoint:      hostEndpoint,
				ContainerEndpoint: fmt.Sprintf("%s:%s", containerNetworkIP, containerPort),
			}

			e.container = containerResource
			e.stopped = false

			return endpointAddress, nil

		case startErr := <-errsChan:
			return nil, startErr
		}
	}
}

func (e *LocalEndpoint) Stop(ctx context.Context) error {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return ctx.Err()
	}

	if e.stopped {
		return nil
	}

	if e.container != nil {
		containerCloseErr := e.container.Close()
		if containerCloseErr != nil {
			return containerCloseErr
		}
	}

	err := os.Remove(e.fileExporterOutputPath)
	if err != nil {
		return err
	}

	err = os.Remove(e.collectorConfigPath)
	if err != nil {
		return err
	}

	e.container = nil
	e.stopped = true

	return nil
}

func (e *LocalEndpoint) TracerProvider(ctx context.Context, r *resource.Resource) (*trace.TracerProvider, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.stopped {
		return nil, fmt.Errorf("refusing to return OpenTelemetry TracerProvider for stopped endpoint")
	}

	if e.container == nil {
		return nil, fmt.Errorf("cannot return OpenTelemetry TracerProvider with nil OpenTelemetry Collector container")
	}

	containerPort := e.container.GetPort(GRPCContainerPort)
	if containerPort == "" {
		return nil, fmt.Errorf("got no container gRPC port for the OpenTelemetry Collector container")
	}

	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithInsecure(), otlptracegrpc.WithEndpoint(fmt.Sprintf("localhost:%s", containerPort)))
	if err != nil {
		return nil, err
	}

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	traceProvider := trace.NewTracerProvider(
		trace.WithBatcher(exporter),
		trace.WithResource(r),
	)

	return traceProvider, nil
}

func (e *LocalEndpoint) OTLPEndpoint(ctx context.Context) (*common.LocalEndpointAddress, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.stopped {
		return nil, fmt.Errorf("refusing to return OTLP endpoint for stopped endpoint")
	}

	if e.container == nil {
		return nil, fmt.Errorf("cannot return trace endpoint with nil OpenTelemetry Collector container")
	}

	containerNetwork, err := common.ContainerNetwork(e.networkName)
	if err != nil {
		return nil, fmt.Errorf("getting container network:% s", err)
	}

	containerNetworkIP := e.container.GetIPInNetwork(containerNetwork)
	if containerNetworkIP == "" {
		return nil, fmt.Errorf("got no IP for Tempo container on the shared container network")
	}

	containerPort := e.container.GetPort(GRPCContainerPort)
	if containerPort == "" {
		return nil, fmt.Errorf("got no container gRPC port for the OpenTelemetry Collector container")
	}

	hostEndpoint := e.container.GetHostPort(GRPCContainerPort)
	if hostEndpoint == "" {
		return nil, fmt.Errorf("got no host gRPC OpenTelemetry host endpoint for OpenTelemetry Collector container")
	}

	endpointAddress := &common.LocalEndpointAddress{
		HostEndpoint:      hostEndpoint,
		ContainerEndpoint: fmt.Sprintf("%s:%s", containerNetworkIP, containerPort),
	}

	return endpointAddress, nil
}
