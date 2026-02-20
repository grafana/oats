// Package docker provides some helpers to manage docker-compose clusters from the test suites
package compose

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path"
	"strings"
	"sync"

	"github.com/grafana/oats/testhelpers/remote"
)

type Compose struct {
	Command     string
	DefaultArgs []string
	Path        string
	Env         []string
}

func defaultEnv() []string {
	return os.Environ()
}

func Suite(composeFile string) (*Compose, error) {
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
	// networks accumulate over time and can cause issues with the tests
	err := c.runDocker(newCommand("network", "prune", "-f", "--filter", "until=5m").withCompose(false))
	if err != nil {
		return fmt.Errorf("failed to prune docker networks: %w", err)
	}

	return c.runDocker(newCommand("up", "--build", "--detach", "--force-recreate").withBackground(true))
}

func (c *Compose) LogToStdout() error {
	return c.runDocker(newCommand("logs"))
}

func (c *Compose) LogsToConsumer(logConsumer func(io.ReadCloser, *sync.WaitGroup)) error {
	return c.runDocker(newCommand("logs").withLogConsumer(logConsumer))
}

func (c *Compose) Stop() error {
	return c.runDocker(newCommand("stop"))
}

func (c *Compose) Remove() error {
	return c.runDocker(newCommand("rm", "-f"))
}

func (c *Compose) runDocker(cc command) error {
	var cmdArgs []string
	if cc.compose {
		cmdArgs = c.DefaultArgs
		cmdArgs = append(cmdArgs, "-f", c.Path)
	}
	cmdArgs = append(cmdArgs, cc.args...)
	cmd := exec.Command(c.Command, cmdArgs...)
	cmd.Env = c.Env
	if cc.logConsumer != nil {
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		wg := sync.WaitGroup{}
		wg.Add(1)
		go cc.logConsumer(stdout, &wg)

		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start docker command: %w", err)
		}
		wg.Wait()
	} else if cc.background {
		slog.Info("Running", "command", cmd.String(), "dir", c.Path)
		stdout, _ := cmd.StdoutPipe()
		cmd.Stderr = cmd.Stdout
		go func() {
			reader := bufio.NewReader(stdout)
			line, err := reader.ReadString('\n')
			for err == nil {
				slog.Info(line)
				line, err = reader.ReadString('\n')
			}
		}()

		err := cmd.Start()
		if err != nil {
			return fmt.Errorf("failed to start docker command: %w", err)
		}
	} else {
		slog.Info("Running", "command", cmd.String(), "dir", c.Path)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		err := cmd.Run()
		if err != nil {
			return fmt.Errorf("failed to run docker command: %w", err)
		}
	}
	return nil
}

func (c *Compose) Close() error {
	var errs []string
	if err := c.LogToStdout(); err != nil {
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

func NewEndpoint(host string, composeFilePath string, ports remote.PortsConfig) *remote.Endpoint {
	var compose *Compose
	return remote.NewEndpoint(host, ports, func(ctx context.Context) error {
		var err error

		if composeFilePath == "" {
			return fmt.Errorf("composeFilePath cannot be empty")
		}

		compose, err = Suite(composeFilePath)
		if err != nil {
			return err
		}
		err = compose.Up()

		return err
	}, func(ctx context.Context) error {
		return compose.Close()
	},
		func(f func(io.ReadCloser, *sync.WaitGroup)) error {
			return compose.LogsToConsumer(f)
		},
	)
}

type command struct {
	background  bool
	compose     bool
	logConsumer func(io.ReadCloser, *sync.WaitGroup)
	args        []string
}

func newCommand(
	args ...string) command {
	return command{
		args:    args,
		compose: true,
	}
}

func (c command) withBackground(background bool) command {
	c.background = background
	return c
}

func (c command) withCompose(compose bool) command {
	c.compose = compose
	return c
}

func (c command) withLogConsumer(logConsumer func(io.ReadCloser, *sync.WaitGroup)) command {
	c.logConsumer = logConsumer
	return c
}
