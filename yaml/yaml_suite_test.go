package yaml_test

import (
	"github.com/grafana/oats/yaml"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"testing"
)

func TestYaml(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Yaml Suite")
}

var _ = Describe("test case", Ordered, ContinueOnFailure, Label("docker", "integration", "slow"), func() {
	Describe("yaml test case", func() {
		cases, base := yaml.ReadTestCases()
		if base != "" {
			It("should have at least one test case", func() {
				Expect(cases).ToNot(BeEmpty(), "expected at least one test case in %s", base)
			})
		}

		for _, c := range cases {
			Describe(c.Name, func() {
				yaml.RunTestCase(c)
			})
		}
	})
})
