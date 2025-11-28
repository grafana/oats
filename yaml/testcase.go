package yaml

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/grafana/oats/model"
	"go.yaml.in/yaml/v3"
)

var oatsFileRegex = regexp.MustCompile(`oats.*\.ya?ml`)

func ReadTestCases(base string) ([]*model.TestCase, string) {
	if base == "" {
		return []*model.TestCase{}, ""
	}

	base = absolutePath(base)

	cases, err := collectTestCases(base, true)
	if err != nil {
		panic(err)
	}

	return cases, base
}

func collectTestCases(base string, evaluateIgnoreFile bool) ([]*model.TestCase, error) {
	var cases []*model.TestCase
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

		if !oatsFileRegex.MatchString(d.Name()) || strings.Contains(d.Name(), "-template.yaml") || strings.Contains(d.Name(), "-template.yml") {
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

func readTestCase(testBase, filePath string) (model.TestCase, error) {
	def, err := readTestCaseDefinition(filePath)
	if err != nil {
		return model.TestCase{}, err
	}

	absoluteFilePath := absolutePath(filePath)
	dir := filepath.Dir(absoluteFilePath)
	name := strings.TrimPrefix(dir, absolutePath(testBase)) + "-" + strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
	sep := string(filepath.Separator)
	name = strings.TrimPrefix(name, sep)
	name = strings.ReplaceAll(name, sep, "-")
	name = "run" + name
	testCase := model.TestCase{
		Path:       absoluteFilePath,
		Name:       name,
		Dir:        dir,
		Definition: def,
	}
	return testCase, nil
}

func readTestCaseDefinition(filePath string) (model.TestCaseDefinition, error) {
	filePath = absolutePath(filePath)
	def := model.TestCaseDefinition{}
	content, err := os.ReadFile(filePath)
	if err != nil {
		return model.TestCaseDefinition{}, err
	}

	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	err = dec.Decode(&def)
	if err != nil {
		return model.TestCaseDefinition{},
			fmt.Errorf("error parsing test case definition %s - see migration notes at https://github.com/grafana/oats/releases/tag/v0.5.0: %w",
				filePath, err)
	}

	for _, s := range def.Include {
		p := includePath(filePath, s)
		other, err := readTestCaseDefinition(p)
		if err != nil {
			return model.TestCaseDefinition{}, err
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
