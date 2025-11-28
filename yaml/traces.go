package yaml

import (
	"context"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/testhelpers/tempo/responses"
	"github.com/onsi/gomega"
)

func AssertTempo(r *runner, t model.ExpectedTraces) {
	ctx := context.Background()

	b, err := r.endpoint.SearchTempo(ctx, t.TraceQL)
	r.LogQueryResult("traceQL query %v response %v err=%v\n", t.TraceQL, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())

	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	res, err := responses.ParseTempoSearchResult(b)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(res.Traces).ToNot(gomega.BeEmpty())

	assertTrace(r, res.Traces[0], t, len(res.Traces))
}

func assertTrace(r *runner, tr responses.Trace, wantTraces model.ExpectedTraces, count int) {
	ctx := context.Background()

	b, err := r.endpoint.GetTraceByID(ctx, tr.TraceID)
	r.LogQueryResult("traceQL traceID %v response %v err=%v\n", tr.TraceID, string(b), err)

	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred(), "we should find the trace by traceID")
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0))

	td, err := responses.ParseTraceDetails(b)
	g.Expect(err).ToNot(gomega.HaveOccurred(), "we should be able to parse the GET trace by traceID API output")

	name, atts := responses.FindSpans(td, wantTraces.Signal)
	r.LogQueryResult("found span name '%v' attributes %v for traceID %v\n", name, atts, tr.TraceID)

	if name == "" && !wantTraces.Signal.ExpectAbsent() {
		g.Expect(name).ToNot(gomega.BeEmpty(), "no spans matching the signal were found")
	}
	assertSignal(g, wantTraces.Signal, count, name, atts)
}
