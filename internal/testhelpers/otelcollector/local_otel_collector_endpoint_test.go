package otelcollector_test

import (
	"context"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/common"
	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/tempo"
	"go.opentelemetry.io/otel/sdk/resource"
	"google.golang.org/grpc"

	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/opentelemetry-acceptance-tests/internal/testhelpers/otelcollector"
)

var _ = Describe("provisioning an OpenTelemetry Collector locally using Docker", Label("integration", "docker", "slow"), func() {
	Describe("LocalEndpoint", func() {
		It("can start a local OpenTelemetry Collector endpoint", func() {
			ctx := context.Background()

			sharedNetworkUUID, err := uuid.NewRandom()
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a UUID for a shared container network")

			sharedNetworkName := fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

			_, err = common.ContainerNetwork(sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a shared container network")

			defer common.DestroyContainerNetwork(sharedNetworkName)

			tempoEndpoint, err := tempo.NewLocalEndpoint(ctx, sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Tempo endpoint")

			_, err = tempoEndpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting a local Tempo endpoint")

			defer tempoEndpoint.Stop(ctx)

			traceEndpoint, err := tempoEndpoint.TraceEndpoint(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error getting the endpoint for sending traces to Tempo")

			collectorEndpoint, err := otelcollector.NewLocalEndpoint(ctx, sharedNetworkName, traceEndpoint)
			Expect(err).ToNot(HaveOccurred(), "expeccted no error creating a local OpenTelemetry Collector endpoint")

			collectorAddress, err := collectorEndpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting a local OpenTelemetry Collector endpoint")

			defer collectorEndpoint.Stop(ctx)

			conn, err := grpc.Dial(collectorAddress.HostEndpoint, grpc.WithInsecure())
			Expect(err).ToNot(HaveOccurred(), "expected no error connecting to the gRPC endpoint of the OpenTelemetry Collector")

			err = conn.Close()
			Expect(err).ToNot(HaveOccurred(), "expected no error closing the gRPC connection to the OpenTelemetry Collector")
		})

		It("provides an OpenTelemetry TracerProvider for sending trace data", func() {
			ctx := context.Background()

			sharedNetworkUUID, err := uuid.NewRandom()
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a UUID for a shared container network")

			sharedNetworkName := fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

			_, err = common.ContainerNetwork(sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a shared container network")

			defer common.DestroyContainerNetwork(sharedNetworkName)

			tempoEndpoint, err := tempo.NewLocalEndpoint(ctx, sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Tempo endpoint")

			_, err = tempoEndpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting a local Tempo endpoint")

			defer tempoEndpoint.Stop(ctx)

			traceEndpoint, err := tempoEndpoint.TraceEndpoint(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error getting the endpoint for sending traces to Tempo")

			collectorEndpoint, err := otelcollector.NewLocalEndpoint(ctx, sharedNetworkName, traceEndpoint)
			Expect(err).ToNot(HaveOccurred(), "expeccted no error creating a local OpenTelemetry Collector endpoint")

			_, err = collectorEndpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting a local OpenTelemetry Collector endpoint")

			defer collectorEndpoint.Stop(ctx)

			r, err := resource.Merge(
				resource.Default(),
				resource.NewWithAttributes(
					"", // use the SchemaURL from the default resource
					semconv.ServiceName("LocalOTelCollectorEndpointTest"),
				),
			)

			Expect(err).ToNot(HaveOccurred(), "expected no error creating an OpenTelemetry resource")

			traceProvider, err := collectorEndpoint.TracerProvider(ctx, r)
			Expect(err).ToNot(HaveOccurred(), "expected no error getting a trace provider")

			defer traceProvider.Shutdown(ctx)

			tracer := traceProvider.Tracer("LocalOTelCollectorEndpointTestTracer")

			parentCtx, parentSpan := tracer.Start(ctx, "local-otel-collector-endpoint-test-parent")

			const eventMessage = "taking a little siesta"

			// create a closure over the tracer, parent context, and event message
			helloOtel := func() {
				_, childSpan := tracer.Start(parentCtx, "hello-otel")
				defer childSpan.End()

				childSpan.AddEvent(eventMessage)

				time.Sleep(250 * time.Millisecond)
			}

			helloOtel()
			parentSpan.End()

			parentSpanContext := parentSpan.SpanContext()
			Expect(parentSpanContext.HasTraceID()).To(BeTrue(), "expected the parent local OpenTelemetry Collector endpoint test span to have a valid TraceID")

			err = traceProvider.ForceFlush(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error flushing the trace provider")

			traceID := parentSpanContext.TraceID()

			fetchedTrace, err := tempoEndpoint.GetTraceByID(ctx, traceID.String())
			if err != nil {
				fmt.Println("there was an error fetching the trace from Tempo, you have 60s to investigate.....")
				time.Sleep(60 * time.Second)
			}

			Expect(err).ToNot(HaveOccurred(), "expected no error fetching the exported trace from Tempo")

			Expect(string(fetchedTrace)).ToNot(BeEmpty(), "expected a non empty response from Tempo when getting an exported trace by ID")
			Expect(string(fetchedTrace)).To(ContainSubstring(eventMessage), "expected the event message to be contained in the returned trace")
		})

		XIt("can be stopped", func() {
			Fail("test not written")
		})

		XIt("tries to respect context cancellation", func() {
			Fail("test not written")
		})
	})
})
