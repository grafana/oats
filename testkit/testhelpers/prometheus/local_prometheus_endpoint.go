package prometheus

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"os/user"
	"strings"
	"sync"
	"text/template"

	_ "embed"

	"github.com/grafana/oats/internal/testhelpers/common"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

const (
	ContainerPort = "9090/tcp"
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

	promDataDir, err := os.MkdirTemp("", "prom-data")
	if err != nil {
		return nil, err
	}

	promConfigFile, err := os.CreateTemp("", "prom-config")
	if err != nil {
		return nil, err
	}

	configData := &ConfigTemplateData{
		ContainerPort: strings.ReplaceAll(ContainerPort, common.TCPSuffix, ""),
	}

	configTemplate, err := template.New("prom-config").Parse(ConfigTemplate)
	if err != nil {
		_ = promConfigFile.Close()
		_ = os.Remove(promConfigFile.Name())

		return nil, err
	}

	err = configTemplate.Execute(promConfigFile, configData)
	if err != nil {
		_ = promConfigFile.Close()
		_ = os.Remove(promConfigFile.Name())

		return nil, err
	}

	promConfigPath := promConfigFile.Name()

	err = promConfigFile.Close()
	if err != nil {
		_ = os.Remove(promConfigFile.Name())

		return nil, err
	}

	endpoint := &LocalEndpoint{
		mutex:         &sync.Mutex{},
		networkName:   networkName,
		configPath:    promConfigPath,
		dataDir:       promDataDir,
		stopped:       true,
	}

	return endpoint, nil
}

type LocalEndpoint struct {
	mutex *sync.Mutex

	container   *dockertest.Resource
	networkName string

	tempoEndpoint *common.LocalEndpointAddress

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
			return nil, fmt.Errorf("got no IP for Prometheus container on the container network")
		}

		existingContainerPort := strings.ReplaceAll(ContainerPort, common.TCPSuffix, "")

		existingContainerHostEndpoint := e.container.GetHostPort(ContainerPort)
		if existingContainerHostEndpoint == "" {
			return nil, fmt.Errorf("got no host endpoint for Prometheus container")
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
			return
		}

		promHostPort, getPortErr := common.HostTCPPort()
		if getPortErr != nil {
			errsChan <- getPortErr
			return
		}

		options := &dockertest.RunOptions{
			Repository: "prom/prometheus",
			Tag:        "latest",
			Cmd: []string{
				"--config.file=/etc/prometheus.yaml",
				"--storage.tsdb.path=/tmp/prometheus",
				"--web.enable-admin-api",
				"--web.enable-remote-write-receiver",
				"--enable-feature=exemplar-storage",
			},

			User:     fmt.Sprintf("%s:%s", currentUser.Uid, currentUser.Gid),
			Networks: []*dockertest.Network{network},

			Mounts: []string{
				fmt.Sprintf("%s:/etc/prometheus.yaml:z", funcConfig.configPath),
				fmt.Sprintf("%s:/tmp/prometheus:z", funcConfig.dataDir),
			},

			ExposedPorts: []string{
				ContainerPort,
			},

			PortBindings: map[docker.Port][]docker.PortBinding{
				ContainerPort: []docker.PortBinding{{HostPort: promHostPort}},
			},
		}

		resource, runErr := pool.RunWithOptions(options)
		if runErr != nil {
			errsChan <- fmt.Errorf("running container: %s", runErr.Error())
			return
		}

		connectionErr := pool.Retry(func() error {
			url := fmt.Sprintf("http://localhost:%s/-/healthy", resource.GetPort(ContainerPort))

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

			containerPort := strings.ReplaceAll(ContainerPort, common.TCPSuffix, "")

			hostEndpoint := containerResource.GetHostPort(ContainerPort)
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

func (e *LocalEndpoint) PromClient(ctx context.Context) (any, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()

	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

	return nil, nil
}
