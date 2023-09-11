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

var _ = Describe("test case", Label("docker", "integration", "slow"), func() {
	cases, base := yaml.ReadTestCases()
	if base != "" {
		It("should have at least one test case", func() {
			Expect(cases).ToNot(BeEmpty(), "expected at least one test case in %s", base)
		})
	}

	configuration, _ := GinkgoConfiguration()
	if configuration.ParallelTotal > 1 {
		ports := yaml.NewPortAllocator(len(cases))
		for _, c := range cases {
			// Ports have to be allocated before we start executing in parallel to avoid taking the same port.
			// Even though it sounds unlikely, it happens quite often.
			c.PortConfig = ports.AllocatePorts()
		}
	}

	for _, c := range cases {
		Describe(c.Name, Ordered, func() {
			yaml.RunTestCase(c)
		})
	}
})
