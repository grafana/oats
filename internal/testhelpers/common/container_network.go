package common

import (
	"sync"

	"github.com/ory/dockertest/v3"
)

var networks map[string]*dockertest.Network
var mutex *sync.Mutex

func init() {
	networks = map[string]*dockertest.Network{}
	mutex = &sync.Mutex{}
}

func ContainerNetwork(networkName string) (*dockertest.Network, error) {
	mutex.Lock()
	defer mutex.Unlock()

	existingNetwork := networks[networkName]
	if existingNetwork != nil {
		return existingNetwork, nil
	}

	pool, err := dockertest.NewPool("")
	if err != nil {
		return nil, err
	}

	network, err := pool.CreateNetwork(networkName)
	if err != nil {
		return nil, err
	}

	networks[networkName] = network

	return network, nil
}

func DestroyContainerNetwork(networkName string) error {
	mutex.Lock()
	defer mutex.Unlock()

	existingNetwork := networks[networkName]
	if existingNetwork == nil {
		return nil
	}

	err := existingNetwork.Close()
	if err != nil {
		return err
	}

	delete(networks, networkName)

	return nil
}
