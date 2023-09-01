package yaml

import (
	"context"
	"fmt"
	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/tempo/responses"
	. "github.com/onsi/gomega"
)

func AssertTempo(g Gomega, endpoint *compose.ComposeEndpoint, verbose bool, traceQL string, spans []ExpectedSpan) {
	ctx := context.Background()

	b, err := endpoint.SearchTempo(ctx, traceQL)
	if verbose {
		_, _ = fmt.Printf("traceQL query %v response %v err=%v\n", traceQL, string(b), err)
	}
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(b)).Should(BeNumerically(">", 0))

	r, err := responses.ParseTempoResult(b)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(r.Traces).ToNot(BeEmpty())

	assertTrace(g, endpoint, r.Traces[0], spans, verbose)
}

func assertTrace(g Gomega, endpoint *compose.ComposeEndpoint, tr responses.Trace, wantSpans []ExpectedSpan, verbose bool) {
	ctx := context.Background()

	b, err := endpoint.GetTraceByID(ctx, tr.TraceID)
	if verbose {
		_, _ = fmt.Printf("traceQL traceID %v response %v err=%v\n", tr.TraceID, string(b), err)
	}

	g.Expect(err).ToNot(HaveOccurred(), "we should find the trace by traceID")
	g.Expect(len(b)).Should(BeNumerically(">", 0))

	td, err := responses.ParseTraceDetails(b)
	g.Expect(err).ToNot(HaveOccurred(), "we should be able to parse the GET trace by traceID API output")
	g.Expect(td.Batches).ToNot(BeEmpty())

	for _, wantSpan := range wantSpans {
		span := findSpan(td, wantSpan)
		g.Expect(span).ToNot(BeNil(), "we should find a single span with the name %s", wantSpan.Name)

		for k, v := range wantSpan.Attributes {
			err := responses.MatchTraceAttribute(span.Attributes, "string", k, v)
			g.Expect(err).ToNot(HaveOccurred(), "span attribute should match")
		}
	}
}

func findSpan(td responses.TraceDetails, wantSpan ExpectedSpan) *responses.Span {
	for _, batch := range td.Batches {
		spans := batch.FindSpansByName(wantSpan.Name)
		if len(spans) > 0 {
			return &spans[0]
		}
	}
	return nil
}
