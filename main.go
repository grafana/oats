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
)

func main() {
	err := run()
	if err != nil {
		fmt.Println(color.RedString(err.Error()))
		os.Exit(1)
	}
}

func run() error {
	args := os.Args[1:]
	if len(args) < 1 {
		return errors.New("you must pass a path to the test case yaml file")
	}

	lgtmVersion := flag.String("lgtm-version", "latest", "version of https://github.com/grafana/docker-otel-lgtm")
	flag.Parse()

	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		panic(message)
	})

	// todo use positional args to get the test case name
	cases, base := yaml.ReadTestCases(args[0])
	if len(cases) == 0 {
		return fmt.Errorf("no cases found in %s", base)
	}
	for _, testCase := range cases {
		slog.Info("test case found", "test", testCase.Name)
	}

	for _, c := range cases {
		c.LgtmVersion = *lgtmVersion
		yaml.RunTestCase(c)
	}
	return nil
}
