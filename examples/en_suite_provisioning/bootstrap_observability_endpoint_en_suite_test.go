package en_suite_provisioning_test

import (
	"context"
	"time"

	"go.opentelemetry.io/otel/sdk/resource"

	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("setting up the local observability endpoint inside the Ginkgo suite bootstrapping", func() {
	It("hides the setup and teardown of the local observability endpoint", func() {
		if localEndpoint == nil {
			Skip("the local observability endpoint is nil?! bye!")
		}

		ctx := context.Background()

		r, err := resource.Merge(
			resource.Default(),
			resource.NewWithAttributes(
				"", // use the SchemaURL from the default resource
				semconv.ServiceName("LocalObservabilityEndpointExample"),
			),
		)

		Expect(err).ToNot(HaveOccurred(), "expected no error creating an OpenTelemetry resource")

		traceProvider, err := localEndpoint.TracerProvider(ctx, r)
		Expect(err).ToNot(HaveOccurred(), "expected no error getting a tracer provider from the local observability endpoint")

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

		fetchedTrace, err := localEndpoint.GetTraceByID(ctx, traceID.String())
		Expect(err).ToNot(HaveOccurred(), "expected no error fetching the exported trace from the local observability endpoint")

		Expect(string(fetchedTrace)).ToNot(BeEmpty(), "expected a non empty response from the local observability endpoint when getting an exported trace by ID")
		Expect(string(fetchedTrace)).To(ContainSubstring(eventMessage), "expected the event message to be contained in the returned trace")
	})
})
