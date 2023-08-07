package observability_test

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"

	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/internal/testhelpers/observability"
)

var localEndpoint *observability.LocalEndpoint

var _ = Describe("provisioning a local observability endpoint with Docker", Ordered, Label("docker", "integration", "slow"), func() {
	BeforeAll(func() {
		var ctx context.Context = context.Background()
		var startErr error

		localEndpoint = observability.NewLocalEndpoint()
		startErr = localEndpoint.Start(ctx)
		Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
	})

	AfterAll(func() {
		var ctx context.Context = context.Background()
		var stopErr error

		if localEndpoint != nil {
			stopErr = localEndpoint.Stop(ctx)
			Expect(stopErr).ToNot(HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
	})

	Describe("LocalEndpoint", func() {
		It("provides an OpenTelemetry TraceProvider for sending traces", func() {
			ctx := context.Background()

			r, err := resource.Merge(
				resource.Default(),
				resource.NewWithAttributes(
					"", // use the SchemaURL from the default resource
					semconv.ServiceName("LocalObservabilityEndpointTest"),
				),
			)

			Expect(err).ToNot(HaveOccurred(), "expected no error creating an OpenTelemetry resource")

			traceProvider, err := localEndpoint.TracerProvider(ctx, r)
			Expect(err).ToNot(HaveOccurred(), "expected no error getting a trace provider from the local observability endpoint")

			defer traceProvider.Shutdown(ctx)

			tracer := traceProvider.Tracer("LocalObservabilityEndpointTestTracer")

			parentCtx, parentSpan := tracer.Start(ctx, "local-observability-endpoint-test-parent")

			const eventMessage = "taking a little nickerchen"

			// create a closure over the tracer, parent context, and event message
			helloOtelExample := func() {
				_, childSpan := tracer.Start(parentCtx, "hello-observability-test")
				defer childSpan.End()

				childSpan.AddEvent(eventMessage)

				time.Sleep(250 * time.Millisecond)
			}

			helloOtelExample()
			parentSpan.End()

			parentSpanContext := parentSpan.SpanContext()
			Expect(parentSpanContext.HasTraceID()).To(BeTrue(), "expected the parent local observability  endpoint example span to have a valid TraceID")

			err = traceProvider.ForceFlush(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error flushing the trace provider")

			traceID := parentSpanContext.TraceID()

			fetchedTrace, err := localEndpoint.GetTraceByID(ctx, traceID.String())
			Expect(err).ToNot(HaveOccurred(), "expected no error fetching the exported trace from the local observability endpoint")

			Expect(string(fetchedTrace)).ToNot(BeEmpty(), "expected a non empty response from the local observability endpoint when getting an exported trace by ID")
			Expect(string(fetchedTrace)).To(ContainSubstring(eventMessage), "expected the event message to be contained in the returned trace")
		})

		XIt("provides something to send metrics", func() {
			Fail("test not written")
		})

		XIt("provides something to send logs", func() {
			Fail("test not written")
		})
	})
})
