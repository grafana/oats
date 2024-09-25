// Package docker provides some helpers to manage docker-compose clusters from the test suites
package compose

import (
	"context"
	"errors"
	"fmt"
	"github.com/grafana/oats/testhelpers/remote"
	"github.com/onsi/ginkgo/v2"
	"io"
	"os"
	"os/exec"
	"path"
	"strings"
)

type Compose struct {
	Command     string
	DefaultArgs []string
	Path        string
	Logger      io.WriteCloser
	Env         []string
}

func defaultEnv() []string {
	return os.Environ()
}

func ComposeSuite(composeFile string, logger io.WriteCloser) (*Compose, error) {
	command := "docker"
	defaultArgs := []string{"compose"}

	return &Compose{
		Command:     command,
		DefaultArgs: defaultArgs,
		Path:        path.Join(composeFile),
		Env:         defaultEnv(),
		Logger:      logger,
	}, nil
}

func (c *Compose) Up() error {
	//networks accumulate over time and can cause issues with the tests
	configuration, _ := ginkgo.GinkgoConfiguration()
	if configuration.ParallelProcess == 1 {
		//don't do this in parallel, it can cause issues
		err := c.runDocker(false, "network", "prune", "-f", "--filter", "until=5m")
		if err != nil {
			return fmt.Errorf("failed to prune docker networks: %w", err)
		}
	}

	return c.command("up", "--build", "--detach", "--force-recreate")
}

func (c *Compose) Logs() error {
	return c.command("logs")
}

func (c *Compose) Stop() error {
	return c.command("stop")
}

func (c *Compose) Remove() error {
	return c.command("rm", "-f")
}

func (c *Compose) command(args ...string) error {
	return c.runDocker(true, args...)
}

func (c *Compose) runDocker(composeCommand bool, args ...string) error {
	var cmdArgs []string
	if composeCommand {
		cmdArgs = c.DefaultArgs
		cmdArgs = append(cmdArgs, "-f", c.Path)
	}
	cmdArgs = append(cmdArgs, args...)
	cmd := exec.Command(c.Command, cmdArgs...)
	cmd.Env = c.Env
	if c.Logger != nil {
		cmd.Stdout = c.Logger
		cmd.Stderr = c.Logger
		fmt.Fprintf(c.Logger, "Running: docker %s\n", cmd.String())
	}

	return cmd.Run()
}

func (c *Compose) Close() error {
	var errs []string
	if err := c.Logs(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := c.Stop(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := c.Remove(); err != nil {
		errs = append(errs, err.Error())
	}
	if err := c.Logger.Close(); err != nil {
		errs = append(errs, err.Error())
	}
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, " / "))
}

func NewEndpoint(composeFilePath string, ports remote.PortsConfig, logger io.WriteCloser) *remote.Endpoint {
	var compose *Compose
	return remote.NewEndpoint(ports, func(ctx context.Context) error {
		var err error

		if composeFilePath == "" {
			return fmt.Errorf("composeFilePath cannot be empty")
		}

		compose, err = ComposeSuite(composeFilePath, logger)
		if err != nil {
			return err
		}
		err = compose.Up()

		return err
	}, func(ctx context.Context) error {
		return compose.Close()
	})
}
