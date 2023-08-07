package servicegraph_test

import (
	"context"
	"embed"
	"time"

	"github.com/grafana/oats/testkit/internal/harness"
	"github.com/grafana/oats/testkit/internal/util"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/open-telemetry/opentelemetry-collector-contrib/connector/servicegraphconnector"
	"github.com/open-telemetry/opentelemetry-collector-contrib/processor/servicegraphprocessor"
	"go.opentelemetry.io/collector/confmap"
	"go.opentelemetry.io/collector/featuregate"
	"go.opentelemetry.io/collector/pdata/pcommon"
	"go.opentelemetry.io/collector/pdata/pmetric"
	"gopkg.in/yaml.v3"
)

const (
	labelClient = "client"
	labelServer = "server"
)

//go:embed testdata/traces/*.json
var traceFS embed.FS

//go:embed testdata/config_with_peer_attributes.yaml
var componentConfig []byte

// This spec is designed to test the metrics generated by the servicegraph processor.
var _ = Describe("generating service graph metrics", func() {
	var (
		inputStr string
		metrics  []pmetric.Metrics
	)

	// Issue #: https://github.com/grafana/app-o11y/issues/163
	Context("from trace: POST /api/orders", func() {
		JustBeforeEach(func() {
			// create servicegraph connector config
			var data map[string]any
			config := servicegraphprocessor.Config{}
			if err := yaml.Unmarshal(componentConfig, &data); err != nil {
				Fail("error unmarshaling connector config yaml: " + err.Error())
			}
			conf := confmap.NewFromStringMap(data)
			if err := conf.Unmarshal(&config); err != nil {
				Fail("error unmarshaling connector config map: " + err.Error())
			}

			// enable servicegraph.processor.virtualNode feature gate
			if err := featuregate.GlobalRegistry().Set("processor.servicegraph.virtualNode", true); err != nil {
				Fail("error enabling processor.servicegraph.virtualNode feature gate: " + err.Error())
			}

			// create test harness
			h, err := harness.NewTracesToMetrics(GinkgoT(), servicegraphconnector.NewFactory(), &config)
			if err != nil {
				Fail("error constructing test harness: " + err.Error())
			}

			// read trace input
			traces, err := util.ReadTraces(&traceFS, inputStr)
			if err != nil {
				Fail("error reading trace testdata: " + err.Error())
			}

			// consume traces
			ctx := context.Background()
			for _, t := range traces {
				err = h.ConsumeTraces(ctx, t)
				if err != nil {
					Fail("error consuming traces: " + err.Error())
				}
			}

			// TODO: we need to expire metrics for virtualNode pairs to be recorded
			// not a fan of adding this sleep, but it doesn't seem to be long enough.
			time.Sleep(time.Second * 5)
			// should metrics be expired on shutdown? probably
			_ = h.Shutdown(ctx)

			// gather metrics
			metrics = h.Metrics()
			Expect(metrics).NotTo(BeNil())
			Expect(len(metrics)).To(Equal(4))
		})

		Describe("metrics are generated", func() {
			BeforeEach(func() {
				inputStr = "5b584103c6fc5ddf423cb2fb6552d0f0"
			})
			It("should generate traces_service_graph_request_total metric", func() {
				Expect(util.HasMetric(metrics[0], "traces_service_graph_request_total")).To(BeTrue())
			})
			It("should generate traces_service_graph_request_server_seconds metric", func() {
				Expect(util.HasMetric(metrics[0], "traces_service_graph_request_server_seconds")).To(BeTrue())
			})
			It("should generate traces_service_graph_request_client_seconds metric", func() {
				Expect(util.HasMetric(metrics[0], "traces_service_graph_request_client_seconds")).To(BeTrue())
			})
		})

		When("client span to instrumented service", func() {
			BeforeEach(func() {
				inputStr = "5b584103c6fc5ddf423cb2fb6552d0f0"
			})
			It("should create an edge from client=frontend to server=checkout", func() {
				Expect(countEdges(metrics, "frontend", "checkout")).To(BeIdenticalTo(3), "all metrics have edge")
			})
			It("should create an edge from client=frontend to server=fraud-detection", func() {
				Expect(countEdges(metrics, "frontend", "fraud-detection")).To(BeIdenticalTo(3), "all metrics have edge")
			})
			It("should create an edge from client=frontend to server=my_shopping_cart", func() {
				Expect(countEdges(metrics, "frontend", "my_shopping_cart")).To(BeIdenticalTo(3), "all metrics have edge")
			})
		})

		When("client span to uninstrumented service", func() {
			BeforeEach(func() {
				inputStr = "5b584103c6fc5ddf423cb2fb6552d0f0"
			})
			It("should create an edge from client=frontend to server=example.com", func() {
				Expect(countEdges(metrics, "frontend", "example.com")).To(BeIdenticalTo(2),
					"traces_service_graph_request_total and traces_service_graph_request_client_seconds metrics have edge")
			})
		})
	})
})

// countEdges counts the number of metrics that have client and server attributes with values that match the given input
func countEdges(metrics []pmetric.Metrics, client string, server string) int {
	edgeCount := 0
	for i := 0; i < len(metrics); i += 1 {
		m := metrics[i].ResourceMetrics().At(0).ScopeMetrics().At(0).Metrics()
		for j := 0; j < m.Len(); j += 1 {
			if hasEdge(m.At(j), client, server) {
				edgeCount += 1
			}
		}
	}
	return edgeCount
}

// hasEdge returns true if the metric has client and server attributes with values that match the given input
func hasEdge(m pmetric.Metric, client string, server string) bool {
	var attr pcommon.Map
	switch m.Type().String() {
	case pmetric.MetricTypeSum.String():
		attr = m.Sum().DataPoints().At(0).Attributes()
	case pmetric.MetricTypeHistogram.String():
		attr = m.Histogram().DataPoints().At(0).Attributes()
	default:
		Expect(m.Type().String()).ToNot(Equal(m.Type().String()), "did not expect this metric type")
	}

	if c, found := attr.Get(labelClient); found && c.Str() == client {
		s, found := attr.Get(labelServer)
		return found && s.Str() == server
	}
	return false
}
