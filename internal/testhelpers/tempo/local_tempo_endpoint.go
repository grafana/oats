package tempo

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"strings"
	"sync"

	_ "embed"

	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/common"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.opentelemetry.io/otel/sdk/trace"
)

//go:embed tempo.yaml
var defaultTempoConfig []byte

const (
	HTTPContainerPort     = "3200/tcp"
	GRPCContainerPort     = "9095/tcp"
	GRPCOtelContainerPort = "4317/tcp"
	HTTPOtelContainerPort = "4318/tcp"
)

func NewLocalEndpoint(ctx context.Context, networkName string) (*LocalEndpoint, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, err
	}

	err = pool.Client.Ping()
	if err != nil {
		return nil, fmt.Errorf("pinging Docker daemon: %s", err.Error())
	}

	tempoDataDir, err := os.MkdirTemp("", "tempo-data")
	if err != nil {
		return nil, err
	}

	tempoConfig, err := os.CreateTemp("", "tempo-config")
	if err != nil {
		os.RemoveAll(tempoDataDir)

		return nil, err
	}

	defaultTempoConfigReader := bytes.NewReader(defaultTempoConfig)

	_, err = io.Copy(tempoConfig, defaultTempoConfigReader)
	if err != nil {
		tempoConfig.Close()
		os.RemoveAll(tempoDataDir)
		os.Remove(tempoConfig.Name())

		return nil, err
	}

	configPath := tempoConfig.Name()

	err = tempoConfig.Close()
	if err != nil {
		os.RemoveAll(tempoDataDir)
		os.Remove(tempoConfig.Name())

		return nil, err
	}

	endpoint := &LocalEndpoint{
		mutex:       &sync.Mutex{},
		dataDir:     tempoDataDir,
		configPath:  configPath,
		networkName: networkName,
		stopped:     true,
	}

	return endpoint, nil
}

type LocalEndpoint struct {
	mutex *sync.Mutex

	container   *dockertest.Resource
	networkName string

	configPath string
	dataDir    string

	stopped bool
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
			return nil, fmt.Errorf("getting container network:% s", getExistingNetworkErr)
		}

		existingContainerNetworkIP := e.container.GetIPInNetwork(existingContainerNetwork)
		if existingContainerNetworkIP == "" {
			return nil, fmt.Errorf("got no IP for Tempo container on the container network")
		}

		existingContainerPort := strings.ReplaceAll(HTTPContainerPort, common.TCPSuffix, "")

		existingContainerHostEndpoint := e.container.GetHostPort(HTTPContainerPort)
		if existingContainerHostEndpoint == "" {
			return nil, fmt.Errorf("got no host endpoint for HTTP Tempo API of container")
		}

		existingEndpointAddress := &common.LocalEndpointAddress{
			HostEndpoint:      existingContainerHostEndpoint,
			ContainerEndpoint: fmt.Sprintf("%s:%s", existingContainerNetworkIP, existingContainerPort),
		}

		return existingEndpointAddress, nil
	}

	resChan := make(chan *dockertest.Resource)
	errsChan := make(chan error)

	var funcConfig struct {
		configPath  string
		networkName string
		dataDir     string
	}

	funcConfig.configPath = e.configPath
	funcConfig.dataDir = e.dataDir
	funcConfig.networkName = e.networkName

	go func(parentCtx context.Context) {
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
			errsChan <- fmt.Errorf("connecting to Docker daemon: %s", pingErr)
			return
		}

		network, getNetworkErr := common.ContainerNetwork(funcConfig.networkName)
		if getNetworkErr != nil {
			errsChan <- fmt.Errorf("getting container network: %s", getNetworkErr)
			return
		}

		currentUser, getCurrentUserErr := user.Current()
		if getCurrentUserErr != nil {
			errsChan <- fmt.Errorf("getting current user: %s", getCurrentUserErr)
		}

		options := &dockertest.RunOptions{
			Repository: "grafana/tempo",
			Tag:        "2.1.1",
			Cmd:        []string{"/tempo", "-config.file=/etc/tempo.yaml"},
			Env:        []string{"PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"},
			User:       fmt.Sprintf("%s:%s", currentUser.Uid, currentUser.Gid),
			Networks:   []*dockertest.Network{network},

			Mounts: []string{
				fmt.Sprintf("%s:/etc/tempo.yaml:z", funcConfig.configPath),
				fmt.Sprintf("%s:/tmp/tempo:z", funcConfig.dataDir),
			},

			// to update, look at the upstream compose file: https://github.com/grafana/tempo/blob/main/example/docker-compose/local/docker-compose.yaml
			PortBindings: map[docker.Port][]docker.PortBinding{
				HTTPContainerPort:     []docker.PortBinding{{HostPort: ""}}, // the empty for the host port mapping will result in a random port being chosen
				GRPCContainerPort:     []docker.PortBinding{{HostPort: ""}},
				HTTPOtelContainerPort: []docker.PortBinding{{HostPort: ""}},
				GRPCOtelContainerPort: []docker.PortBinding{{HostPort: ""}},
			},
		}

		resource, runErr := pool.RunWithOptions(options)
		if runErr != nil {
			errsChan <- fmt.Errorf("running container: %s", runErr.Error())
			return
		}

		connectionErr := pool.Retry(func() error {
			url := fmt.Sprintf("http://localhost:%s/ready", resource.GetPort(HTTPContainerPort))

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
	}(ctx)

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
				return nil, fmt.Errorf("got no IP for Tempo container on the shared container network")
			}

			containerPort := strings.ReplaceAll(HTTPContainerPort, common.TCPSuffix, "")

			hostEndpoint := containerResource.GetHostPort(HTTPContainerPort)
			if hostEndpoint == "" {
				return nil, fmt.Errorf("got no host endpoint for HTTP Tempo API of container")
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

	err := os.RemoveAll(e.dataDir)
	if err != nil {
		return err
	}

	err = os.Remove(e.configPath)
	if err != nil {
		return err
	}

	e.container = nil
	e.stopped = true

	return nil
}

func (e *LocalEndpoint) TraceEndpoint(ctx context.Context) (*common.LocalEndpointAddress, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.stopped {
		return nil, fmt.Errorf("refusing to return trace endpoint for stopped endpoint")
	}

	if e.container == nil {
		return nil, fmt.Errorf("cannot return trace endpoint with nil Tempo container")
	}

	existingContainerNetwork, getExistingNetworkErr := common.ContainerNetwork(e.networkName)
	if getExistingNetworkErr != nil {
		return nil, fmt.Errorf("getting container network:% s", getExistingNetworkErr)
	}

	existingContainerNetworkIP := e.container.GetIPInNetwork(existingContainerNetwork)
	if existingContainerNetworkIP == "" {
		return nil, fmt.Errorf("got no IP for Tempo container on the shared container network")
	}

	existingContainerPort := strings.ReplaceAll(GRPCOtelContainerPort, common.TCPSuffix, "")

	existingContainerHostEndpoint := e.container.GetHostPort(GRPCOtelContainerPort)
	if existingContainerHostEndpoint == "" {
		return nil, fmt.Errorf("got no host gRPC OpenTelemetry host endpoint for Tempo container")
	}

	endpointAddress := &common.LocalEndpointAddress{
		HostEndpoint:      existingContainerHostEndpoint,
		ContainerEndpoint: fmt.Sprintf("%s:%s", existingContainerNetworkIP, existingContainerPort),
	}

	return endpointAddress, nil
}

func (e *LocalEndpoint) TracerProvider(ctx context.Context, r *resource.Resource) (*trace.TracerProvider, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.stopped {
		return nil, fmt.Errorf("refusing to return TracerProvider for stopped endpoint")
	}

	if e.container == nil {
		return nil, fmt.Errorf("cannot return TraceProvider with nil Tempo container")
	}

	containerPort := e.container.GetPort(GRPCOtelContainerPort)
	if containerPort == "" {
		return nil, fmt.Errorf("got no container gRPC OTel port for Tempo")
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

func (e *LocalEndpoint) GetTraceByID(ctx context.Context, id string) ([]byte, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	if e.stopped {
		return nil, fmt.Errorf("cannot get trace from stopped endpoint")
	}

	if e.container == nil {
		return nil, fmt.Errorf("cannot get trace with nil Tempo container")
	}

	containerPort := e.container.GetPort(HTTPContainerPort)
	if containerPort == "" {
		return nil, fmt.Errorf("got no container HTTP API port for Tempo")
	}

	url := fmt.Sprintf("http://localhost:%s/api/traces/%s", containerPort, id)

	resp, getErr := http.Get(url)
	if getErr != nil {
		return nil, getErr
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("expected HTTP status 200, but got: %d", resp.StatusCode)
	}

	defer resp.Body.Close()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return respBytes, nil
}
