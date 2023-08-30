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

func AssertMetrics(g Gomega, expected Expected, otelComposeEndpoint *compose.ComposeEndpoint, verbose bool, c TestCase) {
	for _, metric := range expected.Metrics {
		assertProm(g, otelComposeEndpoint, verbose, metric.PromQL, metric.Value)
	}
	for _, dashboard := range expected.Dashboards {
		assertDashboard(g, otelComposeEndpoint, verbose, dashboard, &c.Dashboard.Content)
	}
}

func assertDashboard(g Gomega, endpoint *compose.ComposeEndpoint, verbose bool, want ExpectedDashboard, dashboard *lint.Dashboard) {
	wantPanelValues := map[string]string{}
	for _, panel := range want.Panels {
		wantPanelValues[panel.Title] = panel.Value
	}

	for _, panel := range dashboard.Panels {
		ginkgo.It(fmt.Sprintf("panel '%s'", panel.Title), func() {
			wantValue := wantPanelValues[panel.Title]
			if wantValue == "" {
				return
			}
			wantPanelValues[panel.Title] = ""
			g.Expect(panel.Targets).To(HaveLen(1))

			assertProm(g, endpoint, verbose, replaceVariables(panel.Targets[0].Expr), wantValue)
		})
	}

	for panel, expected := range wantPanelValues {
		g.Expect(expected).To(BeEmpty(), "panel '%s' not found", panel)
	}
}

func replaceVariables(promQL string) string {
	for _, variable := range promQlVariables {
		promQL = strings.ReplaceAll(promQL, variable, ".*")
	}
	return promQL
}

func assertProm(g Gomega, endpoint *compose.ComposeEndpoint, verbose bool, promQL string, value string) {
	ginkgo.It(fmt.Sprintf("should have %s in prometheus", promQL), func() {

		ctx := context.Background()
		logger := endpoint.Logger()
		b, err := endpoint.RunPromQL(ctx, promQL)
		if verbose {
			_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)
		}
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(b)).Should(BeNumerically(">", 0), "expected prometheus response to be non-empty")

		pr, err := responses.ParseQueryOutput(b)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(len(pr)).Should(BeNumerically(">", 0), "expected prometheus results to be non-empty")
		_, _ = fmt.Fprintf(logger, "prom response %v err=%v\n", string(b), err)

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
	})
}
