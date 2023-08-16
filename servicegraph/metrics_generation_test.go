package servicegraph_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"time"

	impl "github.com/grafana/oats/internal/testhelpers/observability"
	"github.com/grafana/oats/observability"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"
)

var localEndpoint observability.Endpoint

var _ = Describe("service graph metrics generation", Ordered, func() {
	BeforeAll(func() {
		var ctx context.Context = context.Background()

		localEndpoint = impl.NewLocalEndpoint()
		Expect(localEndpoint.Start(ctx)).To(Succeed(), "expected no error starting a local observability endpoint")
	})

	AfterAll(func() {
		var ctx context.Context = context.Background()

		if localEndpoint != nil {
			Expect(localEndpoint.Stop(ctx)).To(Succeed(), "expected no error stopping the local observability endpoint")
		}
	})

	It("traces HTTP requests", func() {
		ctx := context.Background()

		ts := httptest.NewServer(sampleHandler(localEndpoint))
		defer ts.Close()

		_, err := otelhttp.Get(ctx, ts.URL)
		Expect(err).ToNot(HaveOccurred(), "no http error")

		// TODO: move this to the endpoint
		promClient, err := api.NewClient(api.Config{
			Address:      localEndpoint.PromAddress(),
			Client:       &http.Client{},
			RoundTripper: nil,
		})
		Expect(err).ToNot(HaveOccurred(), "expected no error creating prometheus client")

		v1api := v1.NewAPI(promClient)

		r := v1.Range{
			Start: time.Time{},
			End:   time.Time{},
			Step:  0,
		}

		value, warnings, err := v1api.QueryRange(ctx, "traces_service_graph_request_total{}", r, v1.WithTimeout(50*time.Millisecond))
		Expect(err).ToNot(HaveOccurred(), "prometheus query error")

		println(value.String())
		for f := range warnings {
			println(f)
		}

	})
})

func sampleHandler(e observability.Endpoint) http.Handler {
	ctx := context.Background()

	r, err := resource.Merge(
		resource.Default(),
		resource.NewWithAttributes(
			"", // use the SchemaURL from the default resource
			semconv.ServiceName("sample"),
		),
	)
	if err != nil {
		Fail(err.Error())
	}

	tp, err := localEndpoint.TracerProvider(ctx, r)
	if err != nil {
		Fail(err.Error())
	}

	hf := func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}

	return otelhttp.NewHandler(http.HandlerFunc(hf), "server", otelhttp.WithTracerProvider(tp))
}
