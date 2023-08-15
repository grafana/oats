package compose_test

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"github.com/grafana/oats/internal/testhelpers/compose"
)

var composeEndpoint *compose.ComposeEndpoint

var _ = Describe("provisioning a local observability endpoint with Docker", Ordered, Label("docker", "integration", "slow"), func() {
	BeforeAll(func() {
		var ctx context.Context = context.Background()
		var startErr error

		composeEndpoint = compose.NewEndpoint("", ".", []string{}, compose.PortsConfig{TracesGRPCPort: 4017, TempoHTTPPort: 3200})
		startErr = composeEndpoint.Start(ctx)
		Expect(startErr).ToNot(HaveOccurred(), "expected no error starting a local observability endpoint")
	})

	AfterAll(func() {
		var ctx context.Context = context.Background()
		var stopErr error

		if composeEndpoint != nil {
			stopErr = composeEndpoint.Stop(ctx)
			Expect(stopErr).ToNot(HaveOccurred(), "expected no error stopping the local observability endpoint")
		}
	})

	Describe("observability.LocalEndpoint", func() {
		It("provides an OpenTelemetry TraceProvider for sending traces", func() {
			ctx := context.Background()

			r, err := resource.Merge(
				resource.Default(),
				resource.NewWithAttributes(
					"", // use the SchemaURL from the default resource
					semconv.ServiceName("LocalObservabilityEndpointExample"),
				),
			)

			Expect(err).ToNot(HaveOccurred(), "expected no error creating an OpenTelemetry resource")

			traceProvider, err := composeEndpoint.TracerProvider(ctx, r)
			Expect(err).ToNot(HaveOccurred(), "expected no error getting a tracer provider from the local observability endpoint")

			Expect(traceProvider).ToNot(Equal(nil), "expected non-nil traceProvider")

			defer traceProvider.Shutdown(ctx)

			tracer := traceProvider.Tracer("LocalObservabilityEndpointExampleTracer")

			parentCtx, parentSpan := tracer.Start(ctx, "local-observability-endpoint-example-parent")

			const eventMessage = "taking a little siesta"

			// create a closure over the tracer, parent context, and event message
			helloOtelExample := func() {
				_, childSpan := tracer.Start(parentCtx, "hello-observability-example")
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

			fmt.Println("Looking to see if we stored traceID " + traceID.String())

			var fetchedTrace []byte

			Eventually(ctx, func() error {
				fetchedTrace, err = composeEndpoint.GetTraceByID(ctx, traceID.String())
				return err
			}).WithTimeout(30 * time.Second).Should(BeNil())

			Expect(err).ToNot(HaveOccurred(), "expected no error fetching the exported trace from the local observability endpoint")

			Expect(string(fetchedTrace)).ToNot(BeEmpty(), "expected a non empty response from the local observability endpoint when getting an exported trace by ID")
			Expect(string(fetchedTrace)).To(ContainSubstring(eventMessage), "expected the event message to be contained in the returned trace")

		})
	})
})
