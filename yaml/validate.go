package yaml

import (
	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
)

func assertSignal(g gomega.Gomega, s model.ExpectedSignal, count int, name string, atts map[string]string) {
	if s.Count != nil {
		assertCount(g, s.Count, count)
	}
	assertName(g, s, name)
	assertAttributes(g, s, atts)
}

func assertCount(g gomega.Gomega, expectedRange *model.ExpectedRange, got int) {
	g.Expect(got).Should(gomega.BeNumerically(">=", expectedRange.Min),
		"expected count to be at least %d, got %d", expectedRange.Min, got)
	if expectedRange.Max != 0 {
		g.Expect(got).Should(gomega.BeNumerically("<=", expectedRange.Max),
			"expected count to be at most %d, got %d", expectedRange.Max, got)
	}
}

func assertName(g gomega.Gomega, s model.ExpectedSignal, name string) {
	if len(s.Equals) > 0 {
		g.Expect(name).To(gomega.Equal(s.Equals))
	}
	if len(s.Regexp) > 0 {
		g.Expect(name).To(gomega.MatchRegexp(s.Regexp))
	}
}

func assertAttributes(g gomega.Gomega, l model.ExpectedSignal, attributes map[string]string) {
	for k, v := range l.Attributes {
		g.Expect(attributes).To(gomega.HaveKeyWithValue(k, v))
	}
	for k, v := range l.AttributeRegexp {
		g.Expect(attributes).To(gomega.HaveKey(k))
		g.Expect(attributes[k]).To(gomega.MatchRegexp(v))
	}
	if l.NoExtraAttributes {
		var allowedKeys []string
		for k := range l.Attributes {
			allowedKeys = append(allowedKeys, k)
		}
		for k := range l.AttributeRegexp {
			allowedKeys = append(allowedKeys, k)
		}
		var keys []string
		for k := range attributes {
			keys = append(keys, k)
		}
		g.Expect(keys).To(gomega.ConsistOf(allowedKeys))
	}
}
