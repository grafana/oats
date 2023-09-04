package yaml

import (
	"fmt"
	"github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
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
	JmxConfig      string
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
		Expect(err).ToNot(HaveOccurred(), "could not build application jar")
	}

	pattern := c.Dir + "/build/libs/*SNAPSHOT.jar"
	matches, err := filepath.Glob(pattern)
	Expect(err).ToNot(HaveOccurred(), "could not find application jar")
	Expect(matches).To(HaveLen(1))

	file := matches[0]

	if build {
		fileinfo, err := os.Stat(file)
		Expect(err).ToNot(HaveOccurred())
		Expect(fileinfo.ModTime()).To(BeTemporally(">=", t), "application jar was not built")
	}

	return file
}

func imageName(dir string) string {
	content, err := os.ReadFile(path.Join(dir, ".tool-versions"))
	Expect(err).ToNot(HaveOccurred(), "could not read .tool-versions")
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
		JmxConfig:      jmxConfig(c.Dir, c.Definition.DockerCompose.JavaGeneratorParams.OtelJmxConfig),
		Dashboard:      dashboard,
	}
}

func jmxConfig(dir string, jmxConfig string) string {
	if jmxConfig == "" {
		return ""
	}
	p := path.Join(dir, jmxConfig)
	Expect(p).To(BeAnExistingFile(), "jmx config file does not exist")
	return p
}
