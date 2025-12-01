package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/yaml"
	"github.com/onsi/gomega"
)

func main() {
	err := run()
	if err != nil {
		panic(err)
	}
}

func run() error {
	settings, err := parseSettings()
	if err != nil {
		return err
	}

	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		panic(message)
	})

	if flag.NArg() < 1 {
		return errors.New("you must pass at least one path to a test case yaml file or directory")
	}

	inputs := flag.Args()
	cases, err := yaml.ReadTestCases(inputs, true)
	if err != nil {
		return fmt.Errorf("failed to read test cases: %w", err)
	}

	if len(cases) == 0 {
		return fmt.Errorf("no cases found in %s", strings.Join(inputs, ", "))
	}
	for _, testCase := range cases {
		slog.Info("test case found", "test", testCase.Name)
	}
	for _, c := range cases {
		yaml.RunTestCase(&c, settings)
	}

	slog.Info("all test cases passed")
	return nil
}

func parseSettings() (model.Settings, error) {
	host := flag.String("host", "localhost", "host to run the test cases against")
	lgtmVersion := flag.String("lgtm-version", "latest", "version of https://github.com/grafana/docker-otel-lgtm")

	logAll := flag.Bool("lgtm-log-all", false, "enable logging for all LGTM components")
	logGrafana := flag.Bool("lgtm-log-grafana", false, "enable logging for Grafana")
	logPrometheus := flag.Bool("lgtm-log-prometheus", false, "enable logging for Prometheus")
	logLoki := flag.Bool("lgtm-log-loki", false, "enable logging for Loki")
	logTempo := flag.Bool("lgtm-log-tempo", false, "enable logging for Tempo")
	logPyroscope := flag.Bool("lgtm-log-pyroscope", false, "enable logging for Pyroscope")
	logCollector := flag.Bool("lgtm-log-collector", false, "enable logging for OTel Collector")

	timeout := flag.Duration("timeout", 30*time.Second, "timeout for each test case")
	absentTimeout := flag.Duration("absent-timeout", 10*time.Second, "timeout for tests that assert absence")
	manualDebug := flag.Bool("manual-debug", false, "debug mode")
	logLimit := flag.Int("log-limit", 1000, "maximum log output length per log entry")
	flag.Parse()

	logSettings := make(map[string]bool)
	logSettings["ENABLE_LOGS_ALL"] = *logAll
	logSettings["ENABLE_LOGS_GRAFANA"] = *logGrafana
	logSettings["ENABLE_LOGS_PROMETHEUS"] = *logPrometheus
	logSettings["ENABLE_LOGS_LOKI"] = *logLoki
	logSettings["ENABLE_LOGS_TEMPO"] = *logTempo
	logSettings["ENABLE_LOGS_PYROSCOPE"] = *logPyroscope
	logSettings["ENABLE_LOGS_OTELCOL"] = *logCollector

	return model.Settings{
		Host:            *host,
		Timeout:         *timeout,
		AbsentTimeout:   *absentTimeout,
		LgtmVersion:     *lgtmVersion,
		LgtmLogSettings: logSettings,
		ManualDebug:     *manualDebug,
		LogLimit:        *logLimit,
	}, nil
}
