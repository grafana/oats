package yaml

import (
	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/onsi/gomega"
	. "github.com/onsi/gomega"
)

func AssertLoki(g gomega.Gomega, endpoint *compose.ComposeEndpoint, queryLogger QueryLogger, logQl string, contains string) {
	b, err := endpoint.SearchLoki(logQl)
	queryLogger.LogQueryResult("logQL query %v response %v err=%v\n", logQl, string(b), err)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(b)).Should(BeNumerically(">", 0), "expected loki response to be non-empty")

	g.Expect(string(b)).To(ContainSubstring(contains))
}
