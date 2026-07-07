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
	"strings"
	"sync"

	"github.com/grafana/oats/testhelpers/remote"
)

type Compose struct {
	Command     string
	DefaultArgs []string
	Paths       []string
	Env         []string
}

var dockerPruneMu sync.Mutex

func defaultEnv() []string {
	return os.Environ()
}

func Suite(composeFile string) (*Compose, error) {
	return SuiteFiles([]string{composeFile}, nil)
}

func SuiteFiles(composeFiles []string, env []string) (*Compose, error) {
	command := "docker"
	defaultArgs := []string{"compose"}
	for _, file := range composeFiles {
		defaultArgs = append(defaultArgs, "-f", file)
	}

	if len(composeFiles) == 0 {
		return nil, fmt.Errorf("at least one compose file is required")
	}
	mergedEnv := defaultEnv()
	mergedEnv = append(mergedEnv, env...)
	return &Compose{
		Command:     command,
		DefaultArgs: defaultArgs,
		Paths:       composeFiles,
		Env:         mergedEnv,
	}, nil
}

func (c *Compose) Up() error {
	// networks accumulate over time and can cause issues with the tests
	dockerPruneMu.Lock()
	err := c.runDocker(newCommand("network", "prune", "-f", "--filter", "until=5m").withCompose(false))
	dockerPruneMu.Unlock()
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
		cmdArgs = append([]string(nil), c.DefaultArgs...)
	}
	cmdArgs = append(cmdArgs, cc.args...)
	cmd := exec.Command(c.Command, cmdArgs...)
	cmd.Env = c.Env
	if cc.logConsumer != nil {
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to open docker stdout pipe: %w", err)
		}
		cmd.Stderr = cmd.Stdout
		// Start before spawning the consumer: if Start fails the write end of
		// the pipe never opens, so a consumer started earlier would block on
		// the read forever and leak.
		if err := cmd.Start(); err != nil {
			return fmt.Errorf("failed to start docker command: %w", err)
		}
		wg := sync.WaitGroup{}
		wg.Add(1)
		go cc.logConsumer(stdout, &wg)
		wg.Wait()
		if err := cmd.Wait(); err != nil {
			return fmt.Errorf("failed to run docker command: %w", err)
		}
	} else if cc.background {
		slog.Info("Running", "command", cmd.String(), "compose_files", c.Paths)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return fmt.Errorf("failed to open docker stdout pipe: %w", err)
		}
		cmd.Stderr = cmd.Stdout
		// Start the command before spawning the reader: if Start fails the
		// write end of the pipe never opens, and a reader started earlier would
		// block forever on ReadString and leak.
		if err := cmd.Start(); err != nil {
			_ = stdout.Close()
			return fmt.Errorf("failed to start docker command: %w", err)
		}
		wg := sync.WaitGroup{}
		wg.Add(1)
		go func() {
			defer wg.Done()
			reader := bufio.NewReader(stdout)
			line, err := reader.ReadString('\n')
			for err == nil {
				slog.Info(line)
				line, err = reader.ReadString('\n')
			}
		}()

		err = cmd.Wait()
		wg.Wait()
		if err != nil {
			return fmt.Errorf("failed to run docker command: %w", err)
		}
	} else {
		slog.Info("Running", "command", cmd.String(), "compose_files", c.Paths)
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
