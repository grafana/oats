package yaml

import (
	"context"

	"github.com/grafana/oats/testhelpers/tempo/responses"
	"github.com/onsi/gomega"
	"go.opentelemetry.io/collector/pdata/pcommon"
)

func AssertTempo(r *runner, t ExpectedTraces) {
	ctx := context.Background()

	b, err := r.endpoint.SearchTempo(ctx, t.TraceQL)
	r.LogQueryResult("traceQL query %v response %v err=%v\n", t.TraceQL, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	res, err := responses.ParseTempoSearchResult(b)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(res.Traces).ToNot(gomega.BeEmpty())

	assertTrace(r, res.Traces[0], t.Spans)
}

func AssertTempoAbsent(r *runner, t ExpectedTraces) {
	ctx := context.Background()

	b, err := r.endpoint.SearchTempo(ctx, t.TraceQL)
	r.LogQueryResult("traceQL query %v response %v err=%v\n", t.TraceQL, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	res, err := responses.ParseTempoSearchResult(b)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	// For ExpectAbsent, not finding traces is success (the spans are indeed absent)
	if len(res.Traces) == 0 {
		r.LogQueryResult("no traces found matching %s - absent spans confirmed\n", t.TraceQL)
		return
	}

	// If traces are found, verify that the expected absent spans are not within them
	for _, trace := range res.Traces {
		assertTrace(r, trace, t.Spans)
	}
}

func AssertTraceResponse(b []byte, wantSpans []ExpectedSpan, r *runner) {
	g := r.gomegaInst
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	td, err := responses.ParseTraceDetails(b)
	g.Expect(err).ToNot(gomega.HaveOccurred())

	for _, wantSpan := range wantSpans {
		spans, atts := responses.FindSpansWithAttributes(td, wantSpan.Name)

		if wantSpan.ExpectAbsent {
			g.Expect(spans).To(gomega.BeEmpty(), "expected span %s to be absent, but found %d instance(s)", wantSpan.Name, len(spans))
			continue
		}

		if wantSpan.AllowDups {
			g.Expect(len(spans)).Should(gomega.BeNumerically(">", 0), "we should find at least one span with the name %s", wantSpan.Name)
		} else {
			g.Expect(spans).To(gomega.HaveLen(1), "we should find a single span with the name %s", wantSpan.Name)
		}

		for k, v := range wantSpan.Attributes {
			for k, v := range spans[0].Attributes().AsRaw() {
				atts[k] = v
			}
			m := pcommon.NewMap()
			err = m.FromRaw(atts)
			g.Expect(err).ToNot(gomega.HaveOccurred(), "we should be able to convert the map to a pdata.Map")
			err := responses.MatchTraceAttribute(m, pcommon.ValueTypeStr, k, v)
			g.Expect(err).ToNot(gomega.HaveOccurred(), "span attribute should match")
		}
	}
}

func assertTrace(r *runner, tr responses.Trace, wantSpans []ExpectedSpan) {
	ctx := context.Background()

	b, err := r.endpoint.GetTraceByID(ctx, tr.TraceID)
	r.LogQueryResult("traceQL traceID %v response %v err=%v\n", tr.TraceID, string(b), err)

	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred(), "we should find the trace by traceID")
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	AssertTraceResponse(b, wantSpans, r)
}
