package yaml

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"
)

var oatsFileRegex = regexp.MustCompile("oats.*\\.yaml")

func ReadTestCases() ([]*TestCase, string) {
	base := TestCaseBasePath()
	if base == "" {
		return []*TestCase{}, ""
	}

	base = absolutePath(base)
	timeout := os.Getenv("TESTCASE_TIMEOUT")
	if timeout == "" {
		timeout = "30s"
	}
	duration, err := time.ParseDuration(timeout)
	if err != nil {
		panic(err)
	}

	cases, err := collectTestCases(base, duration, true)
	if err != nil {
		panic(err)
	}

	return cases, base
}

func collectTestCases(base string, duration time.Duration, evaluateIgnoreFile bool) ([]*TestCase, error) {
	var cases []*TestCase
	var ignored []string
	err := filepath.WalkDir(base, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if evaluateIgnoreFile {
			if d.IsDir() {
				if _, err := os.Stat(filepath.Join(p, ".oatsignore")); errors.Is(err, os.ErrNotExist) {
					// ignore file does not exist
				} else {
					// ignore file exists
					println("ignoring", p)
					ignored = append(ignored, p)
					return nil
				}
			}
		}

		if !oatsFileRegex.MatchString(d.Name()) || strings.Contains(d.Name(), "-template.yaml") {
			return nil
		}

		for _, i := range ignored {
			if strings.HasPrefix(p, i) {
				return nil
			}
		}

		if evaluateIgnoreFile {
			println("adding", p)
		}

		testCase, err := readTestCase(base, p, duration)
		if err != nil {
			return err
		}
		cases = append(cases, &testCase)
		return nil
	})
	return cases, err
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

	dir := filepath.Dir(absolutePath(filePath))
	name := strings.TrimPrefix(dir, absolutePath(testBase)) + "-" + strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	sep := string(filepath.Separator)
	name = strings.TrimPrefix(name, sep)
	name = strings.ReplaceAll(name, sep, "-")
	name = "run" + name
	testCase := TestCase{
		Name:       name,
		Dir:        dir,
		Definition: def,
		Timeout:    duration,
	}
	return testCase, nil
}

func readTestCaseDefinition(filePath string) (TestCaseDefinition, error) {
	filePath = absolutePath(filePath)
	def := TestCaseDefinition{}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return TestCaseDefinition{}, err
	}

	err = yaml.Unmarshal(content, &def)
	if err != nil {
		return TestCaseDefinition{}, err
	}

	for _, s := range def.Include {
		p := includePath(filePath, s)
		other, err := readTestCaseDefinition(p)
		if err != nil {
			return TestCaseDefinition{}, err
		}
		def.Merge(other)
	}
	def.Include = []string{}

	return def, nil
}

func includePath(filePath string, include string) string {
	dir := filepath.Dir(filePath)
	fromSlash := filepath.FromSlash(include)
	return filepath.Join(dir, fromSlash)
}

func TestCaseBasePath() string {
	return os.Getenv("TESTCASE_BASE_PATH")
}

func AssumeNoYamlTest(t *testing.T) {
	if TestCaseBasePath() != "" {
		t.Skip("skipping because we run yaml tests")
	}
}
