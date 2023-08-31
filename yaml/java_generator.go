package yaml

import (
	"fmt"
	"github.com/onsi/ginkgo/v2"
	"github.com/onsi/gomega"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type javaTemplateVars struct {
	Image          string
	JavaAgent      string
	ApplicationJar string
	Dashboard      string
}

func (c *TestCase) applicationJar() string {
	t := time.Now()
	build := os.Getenv("TESTCASE_SKIP_BUILD") != "true"
	if build {
		println("building application jar in " + c.Dir)
		// create a new app.jar - only needed for local testing - maybe add an option to skip this in CI
		cmd := exec.Command("../../../gradlew", "clean", "build")
		cmd.Dir = c.Dir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stdout

		err := cmd.Run()
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
	}

	pattern := c.Dir + "/build/libs/*SNAPSHOT.jar"
	matches, err := filepath.Glob(pattern)
	gomega.Expect(err).ToNot(gomega.HaveOccurred())
	gomega.Expect(matches).To(gomega.HaveLen(1))

	file := matches[0]

	if build {
		fileinfo, err := os.Stat(file)
		gomega.Expect(err).ToNot(gomega.HaveOccurred())
		gomega.Expect(fileinfo.ModTime()).To(gomega.BeTemporally(">=", t), "application jar was not built")
	}

	return file
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
