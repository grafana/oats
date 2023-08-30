package yaml

import (
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
)

type javaTemplateVars struct {
	Image          string
	JavaAgent      string
	ApplicationJar string
	Dashboard      string
}

func (c *TestCase) applicationJar() string {
	pattern := c.Dir + "/build/libs/*SNAPSHOT.jar"
	matches, err := filepath.Glob(pattern)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(matches).To(gomega.HaveLen(1))

	return matches[0]
}

func imageName(dir string) string {
	content, err := os.ReadFile(path.Join(dir, ".tool-versions"))
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	for _, line := range strings.Split(string(content), "\n") {
		if strings.HasPrefix(line, "java ") {
			// find major version in java temurin-8.0.372+7 using regex
			major := regexp.MustCompile("java temurin-(\\d+).*").FindStringSubmatch(line)[1]
			return fmt.Sprintf("eclipse-temurin:%s-jre", major)
		}
	}
	ginkgo.Fail("no java version found")
	return ""
}

func (c *TestCase) javaTemplateVars(dashboard string) (string, any) {
	projectDir := strings.Split(c.Dir, "examples/")[0]

	return "./docker-compose-java-template.yml", javaTemplateVars{
		Image:          imageName(c.Dir),
		JavaAgent:      path.Join(projectDir, "agent/build/libs/grafana-opentelemetry-java.jar"),
		ApplicationJar: c.applicationJar(),
		Dashboard:      dashboard,
	}
}
