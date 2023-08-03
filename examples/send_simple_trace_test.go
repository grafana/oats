package examples_test

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/common"
	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/otelcollector"
	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/tempo"
	"go.opentelemetry.io/otel/sdk/resource"

	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var localTempoEndpoint *tempo.LocalEndpoint
var localCollectorEndpoint *otelcollector.LocalEndpoint
var sharedNetworkName string

var _ = Describe("sending a simple trace to Tempo through the OpenTelemetry Collector", Ordered, Serial, func() {
	BeforeAll(func() {
		ctx := context.Background()

		sharedNetworkUUID, err := uuid.NewRandom()
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a new UUID for the shared container network name")

		sharedNetworkName = fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

		_, err = common.ContainerNetwork(sharedNetworkName)
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a shared container network")

		localTempoEndpoint, err = tempo.NewLocalEndpoint(ctx, sharedNetworkName)
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Tempo endpoint")

		_, err = localTempoEndpoint.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "expected no error starting the local Tempo endpoint")

		traceEndpoint, err := localTempoEndpoint.TraceEndpoint(ctx)
		Expect(err).ToNot(HaveOccurred(), "expected no error getting the Tempo trace ingestion endpoint")

		localCollectorEndpoint, err = otelcollector.NewLocalEndpoint(ctx, sharedNetworkName, traceEndpoint)
		Expect(err).ToNot(HaveOccurred(), "expected no error creating a local OpenTelemetry collector endpoint")

		_, err = localCollectorEndpoint.Start(ctx)
		Expect(err).ToNot(HaveOccurred(), "expected no error starting the local OpenTelemetry collector endpoint")
	})

	AfterAll(func() {
		var cleanupErr error
		var ctx context.Context = context.Background()

		if localCollectorEndpoint != nil {
			cleanupErr = localCollectorEndpoint.Stop(ctx)
			Expect(cleanupErr).ToNot(HaveOccurred(), "expected no error stopping the local OpenTelemetry Collector endpoint")
		}

		if localTempoEndpoint != nil {
			cleanupErr = localTempoEndpoint.Stop(ctx)
			Expect(cleanupErr).ToNot(HaveOccurred(), "expected no error stopping the local Tempo endpoint")
		}

		if sharedNetworkName != "" {
			cleanupErr = common.DestroyContainerNetwork(sharedNetworkName)
			Expect(cleanupErr).ToNot(HaveOccurred(), "expected no error destroying the shared container network")
		}
	})

	It("is possible to send traces to Tempo through the OpenTelemetry Collector", func() {
		ctx := context.Background()

		r, err := resource.Merge(
			resource.Default(),
			resource.NewWithAttributes(
				"", // use the SchemaURL from the default resource
				semconv.ServiceName("LocalOTelCollectorEndpointExample"),
			),
		)

		Expect(err).ToNot(HaveOccurred(), "expected no error creating an OpenTelemetry resource")

		traceProvider, err := localCollectorEndpoint.TracerProvider(ctx, r)
		Expect(err).ToNot(HaveOccurred(), "expected no error getting a trace provider")

		defer traceProvider.Shutdown(ctx)

		tracer := traceProvider.Tracer("LocalOTelCollectorEndpointExampleTracer")

		parentCtx, parentSpan := tracer.Start(ctx, "local-otel-collector-endpoint-example-parent")

		const eventMessage = "taking a little siesta"

		// create a closure over the tracer, parent context, and event message
		helloOtelExample := func() {
			_, childSpan := tracer.Start(parentCtx, "hello-otel-example")
			defer childSpan.End()

			childSpan.AddEvent(eventMessage)

			time.Sleep(250 * time.Millisecond)
		}

		helloOtelExample()
		parentSpan.End()

		parentSpanContext := parentSpan.SpanContext()
		Expect(parentSpanContext.HasTraceID()).To(BeTrue(), "expected the parent local OpenTelemetry Collector endpoint example span to have a valid TraceID")

		err = traceProvider.ForceFlush(ctx)
		Expect(err).ToNot(HaveOccurred(), "expected no error flushing the trace provider")

		traceID := parentSpanContext.TraceID()

		fetchedTrace, err := localTempoEndpoint.GetTraceByID(ctx, traceID.String())
		Expect(err).ToNot(HaveOccurred(), "expected no error fetching the exported trace from Tempo")

		Expect(string(fetchedTrace)).ToNot(BeEmpty(), "expected a non empty response from Tempo when getting an exported trace by ID")
		Expect(string(fetchedTrace)).To(ContainSubstring(eventMessage), "expected the event message to be contained in the returned trace")
	})
})
