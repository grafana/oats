package yaml

import (
	"context"
	"fmt"
	"github.com/grafana/dashboard-linter/lint"
	"github.com/grafana/oats/internal/testhelpers/compose"
	"github.com/grafana/oats/internal/testhelpers/prometheus/responses"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"strconv"
	"strings"
)

var promQlVariables = []string{"$job", "$instance", "$pod", "$namespace", "$container"}

type DashboardAssert struct {
	want ExpectedDashboard
}

func NewDashboardAssert(d ExpectedDashboard) *DashboardAssert {
	a := DashboardAssert{
		want: d,
	}
	return &a
}

func (a *DashboardAssert) AssertDashboard(g Gomega, endpoint *compose.ComposeEndpoint, queryLogger QueryLogger,
	panelIndex int, dashboard *lint.Dashboard) {
	p := a.want.Panels[panelIndex]
	wantTitle := p.Title
	wantValue := p.Value

	for _, panel := range dashboard.Panels {
		if panel.Title == wantTitle {
			g.Expect(panel.Targets).To(HaveLen(1))

			AssertProm(g, endpoint, queryLogger, panel.Targets[0].Expr, wantValue)
			return
		}
	}
	ginkgo.Fail(fmt.Sprintf("panel '%s' not found", wantTitle))
}

func replaceVariables(promQL string) string {
	for _, variable := range promQlVariables {
		promQL = strings.ReplaceAll(promQL, variable, ".*")
	}
	return promQL
}

func AssertProm(g Gomega, endpoint *compose.ComposeEndpoint, queryLogger QueryLogger, promQL string, value string) {
	promQL = replaceVariables(promQL)
	ctx := context.Background()
	b, err := endpoint.RunPromQL(ctx, promQL)
	queryLogger.LogQueryResult("promQL query %v response %v err=%v\n", promQL, string(b), err)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(b)).Should(BeNumerically(">", 0), "expected prometheus response to be non-empty")

	pr, err := responses.ParseQueryOutput(b)
	g.Expect(err).ToNot(HaveOccurred())
	g.Expect(len(pr)).Should(BeNumerically(">", 0), "expected prometheus results to be non-empty")

	s := strings.Split(value, " ")
	comp := s[0]
	val, err := strconv.ParseFloat(s[1], 64)
	if err != nil {
		g.Expect(err).ToNot(HaveOccurred())
	}
	got, err := strconv.ParseFloat(pr[0].Value[1].(string), 64)
	if err != nil {
		g.Expect(err).ToNot(HaveOccurred())
	}

	g.Expect(got).Should(BeNumerically(comp, val), "expected %s %f, got %f", comp, val, got)
}
