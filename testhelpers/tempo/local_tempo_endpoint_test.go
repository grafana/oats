package tempo_test

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/grafana/oats/testhelpers/common"
	"github.com/grafana/oats/testhelpers/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"

	semconv "go.opentelemetry.io/otel/semconv/v1.20.0"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/grafana/oats/testhelpers/tempo"
)

var _ = Describe("provisioning Tempo locally using Docker", Label("integration", "docker", "slow"), func() {
	Describe("LocalEndpoint", func() {
		It("can start a local Tempo endpoint", func() {
			ctx := context.Background()

			sharedNetworkUUID, err := uuid.NewRandom()
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a UUID for a shared container network")

			sharedNetworkName := fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

			_, err = common.ContainerNetwork(sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a shared container network")

			defer common.DestroyContainerNetwork(sharedNetworkName)

			promEndpoint, err := prometheus.NewLocalEndpoint(ctx, sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Prometheus endpoint")

			promAddress, err := promEndpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting the local Prometheus endpoint")

			defer promEndpoint.Stop(ctx)

			endpoint, err := tempo.NewLocalEndpoint(ctx, sharedNetworkName, promAddress)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Tempo endpoint")

			endpointURL, err := endpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting a local Tempo endpoint")

			defer endpoint.Stop(ctx)

			resp, err := http.Get(fmt.Sprintf("http://%s/status", endpointURL.HostEndpoint))
			Expect(err).ToNot(HaveOccurred(), "expected no error getting the status of the local Tempo endpoint")

			defer resp.Body.Close()

			Expect(resp.StatusCode).To(Equal(http.StatusOK), "expected 200 OX from the local Tempo endpoint")

			respBytes, err := io.ReadAll(resp.Body)
			Expect(err).ToNot(HaveOccurred(), "expected no error reading a Tempo status response")

			Expect(string(respBytes)).To(ContainSubstring("tempo, version 2.1.1"), "expected to get the Tempo version from the status endpoint")
		})

		It("provides an OpenTelemetry TraceProvider for sending trace data, and an endpoint for returning a trace by ID", func() {
			ctx := context.Background()

			sharedNetworkUUID, err := uuid.NewRandom()
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a UUID for a shared container network")

			sharedNetworkName := fmt.Sprintf("ginkgoTest%s", sharedNetworkUUID.String())

			_, err = common.ContainerNetwork(sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a shared container network")

			defer common.DestroyContainerNetwork(sharedNetworkName)

			promEndpoint, err := prometheus.NewLocalEndpoint(ctx, sharedNetworkName)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Prometheus endpoint")

			promAddress, err := promEndpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting the local Prometheus endpoint")

			defer promEndpoint.Stop(ctx)

			endpoint, err := tempo.NewLocalEndpoint(ctx, sharedNetworkName, promAddress)
			Expect(err).ToNot(HaveOccurred(), "expected no error creating a local Tempo endpoint")

			_, err = endpoint.Start(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error starting a local Tempo endpoint")

			defer endpoint.Stop(ctx)

			r, err := resource.Merge(
				resource.Default(),
				resource.NewWithAttributes(
					"", // use the SchemaURL from the default resource
					semconv.ServiceName("LocalTempoEndpointTest"),
				),
			)

			Expect(err).ToNot(HaveOccurred(), "expected no error creating an OpenTelemetry resource")

			traceProvider, err := endpoint.TracerProvider(ctx, r)
			Expect(err).ToNot(HaveOccurred(), "expected no error getting a trace provider")

			defer traceProvider.Shutdown(ctx)

			tracer := traceProvider.Tracer("LocalTempoEndpointTestTracer")

			parentCtx, parentSpan := tracer.Start(ctx, "local-tempo-endpoint-test-parent")

			const eventMessage = "taking a little nap"

			// create a closure over the tracer, parent context, and event message
			helloTempo := func() {
				_, childSpan := tracer.Start(parentCtx, "hello-tempo")
				defer childSpan.End()

				childSpan.AddEvent(eventMessage)

				time.Sleep(250 * time.Millisecond)
			}

			helloTempo()
			parentSpan.End()

			parentSpanContext := parentSpan.SpanContext()
			Expect(parentSpanContext.HasTraceID()).To(BeTrue(), "expected the parent local tempo endpoint test span to have a valid TraceID")

			err = traceProvider.ForceFlush(ctx)
			Expect(err).ToNot(HaveOccurred(), "expected no error flushing the trace provider")

			traceID := parentSpanContext.TraceID()

			fetchedTrace, err := endpoint.GetTraceByID(ctx, traceID.String())
			Expect(err).ToNot(HaveOccurred(), "expected no error fetching the exported trace from Tempo")

			Expect(string(fetchedTrace)).ToNot(BeEmpty(), "expected a non empty response from Tempo when getting an exported trace by ID")
			Expect(string(fetchedTrace)).To(ContainSubstring(eventMessage), "expected the event message to be contained in the returned trace")
		})

		XIt("returns the gRPC endpoint for sending traces to", func() {
			Fail("test not written")
		})

		XIt("can be stopped", func() {
			Fail("test not written")
		})

		XIt("tries to respect context cancellation", func() {
			Fail("test not written")
		})

		XIt("starts idempotently", func() {
			Fail("test not written")
		})
	})
})
