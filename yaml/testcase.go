package yaml

import (
	"gopkg.in/yaml.v3"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func ReadTestCases() ([]TestCase, string) {
	var cases []TestCase

	base := TestCaseBashPath()
	if base != "" {
		base = absolutePath(base)
		timeout := os.Getenv("TESTCASE_TIMEOUT")
		if timeout == "" {
			timeout = "30s"
		}
		duration, err := time.ParseDuration(timeout)
		if err != nil {
			panic(err)
		}

		err = filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.Name() != "oats.yaml" {
				return nil
			}
			testCase, err := readTestCase(base, p, duration)
			if err != nil {
				return err
			}
			cases = append(cases, testCase)
			return nil
		})
		if err != nil {
			panic(err)
		}
	}
	return cases, base
}

func absolutePath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		panic(err)
	}
	return abs
}

func readTestCase(testBase, filePath string, duration time.Duration) (TestCase, error) {
	def, err := readTestCaseDefinition(filePath)
	if err != nil {
		return TestCase{}, err
	}

	dir := path.Dir(absolutePath(filePath))
	name := strings.TrimPrefix(dir, absolutePath(testBase))
	sep := string(filepath.Separator)
	name = strings.TrimPrefix(name, sep)
	name = strings.ReplaceAll(name, sep, "-")
	testCase := TestCase{
		Name:       name,
		Dir:        dir,
		Definition: def,
		Timeout:    duration,
	}
	return testCase, nil
}

func readTestCaseDefinition(filePath string) (TestCaseDefinition, error) {
	def := TestCaseDefinition{}
	content, err := os.ReadFile(absolutePath(filePath))
	if err != nil {
		return TestCaseDefinition{}, err
	}

	err = yaml.Unmarshal(content, &def)
	if err != nil {
		return TestCaseDefinition{}, err
	}

	for _, s := range def.Include {
		p := path.Join(path.Dir(filePath), s)
		other, err := readTestCaseDefinition(p)
		if err != nil {
			return TestCaseDefinition{}, err
		}
		def.Merge(other)
	}
	def.Include = []string{}

	return def, nil
}

func TestCaseBashPath() string {
	return os.Getenv("TESTCASE_BASE_PATH")
}

func AssumeNoYamlTest(t *testing.T) {
	if TestCaseBashPath() != "" {
		t.Skip("skipping because we run yaml tests")
	}
}
