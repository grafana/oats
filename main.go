package main

import (
	"errors"
	"flag"
	"fmt"
	"github.com/fatih/color"
	"github.com/grafana/oats/yaml"
	"github.com/onsi/gomega"
	"log/slog"
	"os"
	"time"
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(color.RedString(err.Error()))
		os.Exit(1)
	}
}

func run() error {
	lgtmVersion := flag.String("lgtm-version", "latest", "version of https://github.com/grafana/docker-otel-lgtm")
	timeout := flag.Duration("timeout", 30*time.Second, "timeout for the test case")
	manualDebug := flag.Bool("manual-debug", false, "debug mode")
	flag.Parse()

	if flag.NArg() != 1 {
		return errors.New("you must pass a path to the test case yaml file")
	}

	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		panic(message)
	})

	cases, base := yaml.ReadTestCases(flag.Arg(0))
	if len(cases) == 0 {
		return fmt.Errorf("no cases found in %s", base)
	}
	for _, testCase := range cases {
		slog.Info("test case found", "test", testCase.Name)
	}

	for _, c := range cases {
		c.LgtmVersion = *lgtmVersion
		c.Timeout = *timeout
		c.ManualDebug = *manualDebug
		yaml.RunTestCase(c)
	}

	slog.Info("all test cases passed")
	return nil
}
