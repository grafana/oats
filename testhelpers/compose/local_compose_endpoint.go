package compose

import (
	"context"
	"fmt"
	"github.com/grafana/oats/testhelpers/remote"
	"io"
)

func NewEndpoint(composeFilePath string, logger io.WriteCloser, ports remote.PortsConfig) *remote.Endpoint {
	var compose *Compose
	return remote.NewEndpoint(ports, logger,
		func(ctx context.Context) error {
			var err error

			if composeFilePath == "" {
				return fmt.Errorf("composeFilePath cannot be empty")
			}

			compose, err = ComposeSuite(composeFilePath)
			if err != nil {
				return err
			}
			err = compose.Up()

			return err
		},
		func(ctx context.Context) error {
			return compose.Close()
		})
}
