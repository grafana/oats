package yaml

import (
	"context"
	"strconv"
	"strings"

	"github.com/grafana/oats/testhelpers/prometheus/responses"
	"github.com/onsi/gomega"
)

var promQlVariables = []string{"$job", "$instance", "$pod", "$namespace", "$container"}

func replaceVariables(promQL string) string {
	for _, variable := range promQlVariables {
		promQL = strings.ReplaceAll(promQL, variable, ".*")
	}
	return promQL
}

func AssertProm(r *runner, promQL string, value string) {
	promQL = replaceVariables(promQL)
	ctx := context.Background()
	b, err := r.endpoint.RunPromQL(ctx, promQL)
	r.queryLogger.LogQueryResult("promQL query %v response %v err=%v\n", promQL, string(b), err)
	g := r.gomegaInst
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(b)).Should(gomega.BeNumerically(">", 0), "expected prometheus response to be non-empty")

	pr, err := responses.ParseQueryOutput(b)
	g.Expect(err).ToNot(gomega.HaveOccurred())
	g.Expect(len(pr)).Should(gomega.BeNumerically(">", 0), "expected prometheus results to be non-empty")

	s := strings.Split(value, " ")
	comp := s[0]
	val, err := strconv.ParseFloat(s[1], 64)
	if err != nil {
		g.Expect(err).ToNot(gomega.HaveOccurred())
	}
	got, err := strconv.ParseFloat(pr[0].Value[1].(string), 64)
	if err != nil {
		g.Expect(err).ToNot(gomega.HaveOccurred())
	}

	g.Expect(got).Should(gomega.BeNumerically(comp, val), "expected %s %f, got %f", comp, val, got)
}
