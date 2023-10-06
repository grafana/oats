package yaml

import (
	"context"

	"github.com/grafana/oats/testhelpers/compose"
	"github.com/grafana/oats/testhelpers/tempo/responses"
	. "github.com/onsi/gomega"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

func AssertTempo(g Gomega, endpoint *compose.ComposeEndpoint, queryLogger QueryLogger, traceQL string, spans []ExpectedSpan) {
	ctx := context.Background()

	b, err := endpoint.SearchTempo(ctx, traceQL)
	queryLogger.LogQueryResult("traceQL query %v response %v err=%v\n", traceQL, string(b), err)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(b)).Should(BeNumerically(">", 0))

	r, err := responses.ParseTempoSearchResult(b)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(r.Traces).ToNot(BeEmpty())

	assertTrace(g, endpoint, r.Traces[0], spans, queryLogger)
}

func assertTrace(g Gomega, endpoint *compose.ComposeEndpoint, tr responses.Trace, wantSpans []ExpectedSpan, queryLogger QueryLogger) {
	ctx := context.Background()

	b, err := endpoint.GetTraceByID(ctx, tr.TraceID)
	queryLogger.LogQueryResult("traceQL traceID %v response %v err=%v\n", tr.TraceID, string(b), err)

	g.Expect(err).ToNot(HaveOccurred(), "we should find the trace by traceID")
	g.Expect(len(b)).Should(BeNumerically(">", 0))

	td, err := responses.ParseTraceDetails(b)
	g.Expect(err).ToNot(HaveOccurred(), "we should be able to parse the GET trace by traceID API output")

	for _, wantSpan := range wantSpans {
		spans, atts := responses.FindSpansWithAttributes(td, wantSpan.Name)
		g.Expect(spans).To(HaveLen(1), "we should find a single span with the name %s", wantSpan.Name)

		for k, v := range wantSpan.Attributes {
			for k, v := range spans[0].Attributes().AsRaw() {
				atts[k] = v
			}
			m := pcommon.NewMap()
			err = m.FromRaw(atts)
			g.Expect(err).ToNot(HaveOccurred(), "we should be able to convert the map to a pdata.Map")
			err := responses.MatchTraceAttribute(m, pcommon.ValueTypeStr, k, v)
			g.Expect(err).ToNot(HaveOccurred(), "span attribute should match")
		}
	}
}
