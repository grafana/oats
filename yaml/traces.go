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

	// If ALL spans have expect-absent, we expect NO traces to be found
	if t.AllSpansExpectAbsent() {
		res, err := responses.ParseTempoSearchResult(b)
		if err != nil || len(res.Traces) == 0 {
			// No traces found - this is expected, the spans are absent
			return
		}
		// Traces were found - this is a failure
		g.Expect(res.Traces).To(gomega.BeEmpty(),
			"expected no traces matching TraceQL query %q (all spans should be absent), but found %d trace(s)",
			t.TraceQL, len(res.Traces))
		return
	}

	// Otherwise, we expect traces to be found
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	res, err := responses.ParseTempoSearchResult(b)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(res.Traces).ToNot(gomega.BeEmpty())

	assertTrace(r, res.Traces[0], t.Spans)
}

func assertTrace(r *runner, tr responses.Trace, wantSpans []ExpectedSpan) {
	ctx := context.Background()

	b, err := r.endpoint.GetTraceByID(ctx, tr.TraceID)
	r.LogQueryResult("traceQL traceID %v response %v err=%v\n", tr.TraceID, string(b), err)

	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred(), "we should find the trace by traceID")
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	td, err := responses.ParseTraceDetails(b)
	g.Expect(err).ToNot(gomega.HaveOccurred(), "we should be able to parse the GET trace by traceID API output")

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
