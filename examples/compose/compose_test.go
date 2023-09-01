package compose_test

import (
	"context"
	"path"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.17.0"

	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/requests"
	"github.com/grafana/oats/internal/testhelpers/tempo/responses"
)

var _ = Describe("provisioning a local observability endpoint with Docker", Ordered, Label("docker", "integration", "slow"), func() {
	var composeEndpoint *compose.ComposeEndpoint

	BeforeAll(func() {
		var ctx context.Context = context.Background()
		var startErr error

		composeEndpoint = compose.NewEndpoint(
			path.Join(".", "docker-compose-traces.yml"),
			path.Join(".", "test-suite-traces.log"),
			[]string{},
			compose.PortsConfig{TracesGRPCPort: 4017, TempoHTTPPort: 3200},
		)
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

	// Traces generated by auto-instrumentation
	Describe("observability.LocalEndpoint", func() {
		It("can create traces with auto-instrumentation", func() {
			ctx := context.Background()

			// Run repeated /smoke APIs to ensure data is flowing from the application to Tempo
			Eventually(ctx, func(g Gomega) {
				err := requests.DoHTTPGet("http://localhost:8080/smoke", 200)
				g.Expect(err).ToNot(HaveOccurred())

				b, err := composeEndpoint.SearchTags(ctx, map[string]string{"http.method": "GET"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(b)).Should(BeNumerically(">", 0))

				sr, err := responses.ParseTempoResult(b)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(sr.Traces)).Should(BeNumerically(">", 0))
			}).WithTimeout(30*time.Second).Should(Succeed(), "calling /smoke for 30 seconds should cause traces in Tempo")

			// Make a single /create-trace event, no need to loop, we know data is flowing by now
			err := requests.DoHTTPGet("http://localhost:8080/create-trace?delay=30&response=200", 200)
			Expect(err).ShouldNot(HaveOccurred())

			var sr responses.TempoResult

			// Loop until we find a /create-trace trace
			Eventually(ctx, func(g Gomega) {
				b, err := composeEndpoint.SearchTags(ctx, map[string]string{"http.target": "/create-trace"})
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(b)).Should(BeNumerically(">", 0))

				sr, err = responses.ParseTempoResult(b)

				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(len(sr.Traces)).Should(BeNumerically(">", 0))
			}).WithTimeout(30*time.Second).Should(Succeed(), "we should see a single /create-trace trace in Tempo after 30 seconds")

			Expect(sr).NotTo(BeNil())
			Expect(len(sr.Traces)).Should(BeNumerically(">=", 1))

			tr := sr.Traces[0]

			traceBytes, err := composeEndpoint.GetTraceByID(ctx, tr.TraceID)
			Expect(err).ToNot(HaveOccurred(), "we should find the traceID from the search tags")
			Expect(len(traceBytes)).Should(BeNumerically(">", 0))

			td, err := responses.ParseTraceDetails(traceBytes)
			Expect(err).ToNot(HaveOccurred(), "we should be able to parse the GET trace by traceID API output")
			Expect(len(td.Batches)).Should(Equal(1))

			batch := td.Batches[0]
			Expect(batch.Resource).ToNot(BeNil())
			err = responses.AttributesMatch(
				batch.Resource.Attributes,
				[]responses.AttributeMatch{
					{Type: "string", Key: "service.namespace", Value: "integration-test"},
					{Type: "string", Key: "service.name", Value: "testserver"},
					{Type: "string", Key: "telemetry.sdk.language", Value: "go"},
				},
			)
			Expect(err).ToNot(HaveOccurred(), "we should be able to match the trace attributes")

			parents := batch.FindSpansByName("GET /create-trace")
			Expect(len(parents)).Should(Equal(1))
			parent := parents[0]

			Expect(parent.Kind).To(Equal("SPAN_KIND_SERVER"))
			Expect(responses.TimeIsIncreasing(parent)).ToNot(HaveOccurred(), "the parent span duration must be a positive value")

			err = responses.AttributesMatch(
				parent.Attributes,
				[]responses.AttributeMatch{
					{Type: "string", Key: "http.target", Value: "/create-trace"},
					{Type: "string", Key: "http.route", Value: "/create-trace"},
					{Type: "string", Key: "http.method", Value: "GET"},
					{Type: "int", Key: "net.host.port", Value: "8080"},
					{Type: "int", Key: "http.status_code", Value: "200"},
					{Type: "int", Key: "http.request_content_length", Value: "0"},
				},
			)
			Expect(err).ToNot(HaveOccurred(), "parent attributes must match")

			err = responses.AttributesExist(
				parent.Attributes,
				[]responses.AttributeMatch{
					{Type: "string", Key: "net.host.name"},
					{Type: "string", Key: "net.sock.peer.addr"},
				},
			)
			Expect(err).ToNot(HaveOccurred(), "parent host and peer attributes must exist")

			children := batch.ChildrenOf(parent.SpanId)
			Expect(len(children)).Should(Equal(2))

			inQueue := false
			processing := false
			for _, c := range children {
				Expect(c.Kind).To(Equal("SPAN_KIND_INTERNAL"))
				Expect(parent.SpanId).To(Equal(c.ParentSpanId))
				Expect(responses.TimeIsIncreasing(c)).ToNot(HaveOccurred(), "the child span duration must be a positive value")
				Expect(c.SpanId).ToNot(Equal(c.ParentSpanId))
				Expect(parent.TraceId).To(Equal(c.TraceId))
				Expect(c.Attributes).To(BeNil()) // internal spans don't have the attributes set

				if c.Name == "in queue" {
					inQueue = true
				}

				if c.Name == "processing" {
					processing = true
				}
			}

			Expect(inQueue).To(BeTrue(), "we must find an 'in queue' internal span")
			Expect(processing).To(BeTrue(), "we must find a 'processing' internal span")
		})
	})

	// Here only to ensure we can actually directly use the TracerProvider
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
