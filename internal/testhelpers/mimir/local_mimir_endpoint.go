package mimir

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/grafana/oats/internal/testhelpers/common"
	"github.com/ory/dockertest/v3"
	"github.com/ory/dockertest/v3/docker"
)

const (
  ContainerPort = "8080/tcp"
	ContainerPortHTTP = "9009/tcp"
)

type LocalEndpoint struct {
  mutex *sync.Mutex

  container *dockertest.Resource
  networkName string
  configPath string
}

func NewLocalEndpoint(ctx context.Context, networkName string) (*LocalEndpoint, error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}

  mimirConfigFile, err := os.CreateTemp("", "mimir-config")
  if err != nil {
    return nil, err
  }

  _, err = mimirConfigFile.Write([]byte(ConfigTemplate))
  if err != nil {
    return nil, err
  }

  err = mimirConfigFile.Close()
  if err != nil {
    _ = os.Remove(mimirConfigFile.Name())
    return nil, err
  }

  endpoint := &LocalEndpoint{
    mutex:       &sync.Mutex{},
  	container:   &dockertest.Resource{},
  	networkName: networkName,
    configPath:  mimirConfigFile.Name(), 
  }

  return endpoint, nil
}

func (e *LocalEndpoint) Start(ctx context.Context) (*common.LocalEndpointAddress, error) {
	e.mutex.Lock()
	defer e.mutex.Unlock()
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

  network, err := common.ContainerNetwork(e.networkName)
  if err != nil {
    return nil, fmt.Errorf("failed to get container network: %s", err.Error())
  }

  hostPort, err := common.HostTCPPort()
  if err != nil {
    return nil, fmt.Errorf("host port error: %s", err.Error())
  }

  options := &dockertest.RunOptions{
  	Repository:   "grafana/mimir",
  	Tag:          "latest",
    Cmd:          []string{
      "-config.file=/etc/mimir-config/mimir.yaml",
      "-config.expand-env=true",
    },
    Mounts:       []string{
      e.configPath+":/etc/mimir-config/mimir.yaml",
    },
    ExposedPorts: []string{
      ContainerPortHTTP,
    },
  	Networks:     []*dockertest.Network{network},
    PortBindings: map[docker.Port][]docker.PortBinding{
      ContainerPortHTTP: {{HostPort: hostPort}},
    },
  }

  resource, err := pool.RunWithOptions(options)
  if err != nil {
    return nil, fmt.Errorf("running container: %s", err.Error())
  }

  if err = pool.Retry(func() error {
    url := fmt.Sprintf("http://localhost:%s/ready", resource.GetPort(ContainerPortHTTP))
    resp, err := http.Get(url)
    if err != nil {
      return err
    }
    
    if resp.StatusCode != http.StatusOK {
      return fmt.Errorf("expected HTTP status 200, but got: %d", resp.StatusCode)
    }

    defer resp.Body.Close()

    return nil
  }); err != nil {
    return nil, err
  }

  hostEndpoint := resource.GetHostPort(ContainerPortHTTP)
  containerNetwork, err := common.ContainerNetwork(e.networkName)
  if err != nil {
    return nil, err
  }
  containerNetworkIP := resource.GetIPInNetwork(containerNetwork)
  containerPort := strings.ReplaceAll(ContainerPortHTTP, common.TCPSuffix, "")
   
  e.container = resource

  return &common.LocalEndpointAddress{
  	HostEndpoint:      hostEndpoint,
    ContainerEndpoint: containerNetworkIP+":"+containerPort,
  }, nil
}

func (e *LocalEndpoint) Stop(ctx context.Context) error {
  return nil
}
