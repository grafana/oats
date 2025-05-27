package yaml

import (
	"errors"
	"fmt"
	"gopkg.in/yaml.v3"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var oatsFileRegex = regexp.MustCompile(`oats.*\.yaml`)

func ReadTestCases(base string) ([]*TestCase, string) {
	if base == "" {
		return []*TestCase{}, ""
	}

	base = absolutePath(base)

	cases, err := collectTestCases(base, true)
	if err != nil {
		panic(err)
	}

	return cases, base
}

func collectTestCases(base string, evaluateIgnoreFile bool) ([]*TestCase, error) {
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
					slog.Info("ignoring", "path", p)
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

		testCase, err := readTestCase(base, p)
		if err != nil {
			return err
		}
		if testCase.Definition.Matrix != nil {
			for _, matrix := range testCase.Definition.Matrix {
				newCase := testCase
				newCase.Definition = testCase.Definition
				newCase.Definition.DockerCompose = matrix.DockerCompose
				newCase.Definition.Kubernetes = matrix.Kubernetes
				newCase.Name = fmt.Sprintf("%s-%s", testCase.Name, matrix.Name)
				newCase.MatrixTestCaseName = matrix.Name
				cases = append(cases, &newCase)
			}
			return nil
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

func readTestCase(testBase, filePath string) (TestCase, error) {
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
