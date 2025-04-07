// Package docker provides some helpers to manage docker-compose clusters from the test suites
package compose

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"github.com/grafana/oats/testhelpers/remote"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"
)

type Compose struct {
	Command     string
	DefaultArgs []string
	Path        string
	LogConsumer func(io.ReadCloser, *sync.WaitGroup)
	Env         []string
}

func defaultEnv() []string {
	return os.Environ()
}

func ComposeSuite(composeFile string) (*Compose, error) {
	command := "docker"
	defaultArgs := []string{"compose"}

	return &Compose{
		Command:     command,
		DefaultArgs: defaultArgs,
		Path:        path.Join(composeFile),
		Env:         defaultEnv(),
	}, nil
}

func (c *Compose) Up() error {
	//networks accumulate over time and can cause issues with the tests
	err := c.runDocker(false, "network", "prune", "-f", "--filter", "until=5m")
	if err != nil {
		return fmt.Errorf("failed to prune docker networks: %w", err)
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
	if c.LogConsumer != nil {
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		wg := sync.WaitGroup{}
		wg.Add(1)
		go c.LogConsumer(stdout, &wg)

		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start docker command: %w", err)
		}
		wg.Wait()
		return nil
	} else {
		slog.Info("Running", "command", cmd.String(), "dir", c.Path)
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		go func() {
			reader := bufio.NewReader(stdout)
			line, err := reader.ReadString('\n')
			for err == nil {
				line, err = reader.ReadString('\n')
				if err == nil {
					slog.Info(line)
				}
			}
		}()

		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start docker command: %w", err)
		}
		return nil
	}
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
	if len(errs) == 0 {
		return nil
	}
	return errors.New(strings.Join(errs, " / "))
}

func NewEndpoint(composeFilePath string, ports remote.PortsConfig) *remote.Endpoint {
	var compose *Compose
	return remote.NewEndpoint(ports, func(ctx context.Context) error {
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
	}, func(ctx context.Context) error {
		return compose.Close()
	},
		func(f func(io.ReadCloser, *sync.WaitGroup)) error {
			compose.LogConsumer = f
			err := compose.Logs()
			if err != nil {
				return err
			}
			compose.LogConsumer = nil
			return nil
		},
	)
}
