package main

import (
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"time"

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
	host := flag.String("host", "localhost", "host to run the test cases against")
	lgtmVersion := flag.String("lgtm-version", "latest", "version of https://github.com/grafana/docker-otel-lgtm")

	logAll := flag.Bool("lgtm-log-all", false, "enable logging for all LGTM components")
	logGrafana := flag.Bool("lgtm-log-grafana", false, "enable logging for Grafana")
	logPrometheus := flag.Bool("lgtm-log-prometheus", false, "enable logging for Prometheus")
	logLoki := flag.Bool("lgtm-log-loki", false, "enable logging for Loki")
	logTempo := flag.Bool("lgtm-log-tempo", false, "enable logging for Tempo")
	logPyroscope := flag.Bool("lgtm-log-pyroscope", false, "enable logging for Pyroscope")
	logCollector := flag.Bool("lgtm-log-collector", false, "enable logging for OTel Collector")

	timeout := flag.Duration("timeout", 30*time.Second, "timeout for the test case")
	manualDebug := flag.Bool("manual-debug", false, "debug mode")
	flag.Parse()

	if flag.NArg() != 1 {
		return errors.New("you must pass a path to the test case yaml file")
	}

	logSettings := make(map[string]bool)
	logSettings["ENABLE_LOGS_ALL"] = *logAll
	logSettings["ENABLE_LOGS_GRAFANA"] = *logGrafana
	logSettings["ENABLE_LOGS_PROMETHEUS"] = *logPrometheus
	logSettings["ENABLE_LOGS_LOKI"] = *logLoki
	logSettings["ENABLE_LOGS_TEMPO"] = *logTempo
	logSettings["ENABLE_LOGS_PYROSCOPE"] = *logPyroscope
	logSettings["ENABLE_LOGS_OTELCOL"] = *logCollector

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
		c.Host = *host
		c.LgtmVersion = *lgtmVersion
		c.LgtmLogSettings = logSettings
		c.Timeout = *timeout
		c.ManualDebug = *manualDebug
		yaml.RunTestCase(c)
	}

	slog.Info("all test cases passed")
	return nil
}
