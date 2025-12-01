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

var yamlFileRegex = regexp.MustCompile(`\.ya?ml$`)

const requiredOatsFileVersion = "2"

func ReadTestCases(input string, evaluateIgnoreFile bool) ([]model.TestCase, error) {
	var cases []model.TestCase

	for _, base := range strings.Split(input, " ") {
		base = absolutePath(base)

		c, err := collectTestCases(base, evaluateIgnoreFile)
		if err != nil {
			return nil, err
		}
		cases = append(cases, c...)
	}

	return cases, nil
}

func collectTestCases(base string, evaluateIgnoreFile bool) ([]model.TestCase, error) {
	var cases []model.TestCase

	if stat, err := os.Stat(base); err != nil {
		return nil, fmt.Errorf("failed to stat path %s: %w", base, err)
	} else if !stat.IsDir() {
		// single file
		return addTestCase(cases, filepath.Dir(base), base)
	}

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

		for _, ignoredDir := range ignored {
			// skip ignored directories
			if strings.HasPrefix(p, ignoredDir) {
				return nil
			}
		}

		if !yamlFileRegex.MatchString(d.Name()) {
			return nil
		}

		cases, err = addTestCase(cases, base, p)
		if err != nil {
			return err
		}
		return nil
	})
	return cases, err
}

func addTestCase(cases []model.TestCase, base string, path string) ([]model.TestCase, error) {
	testCase, err := readTestCase(base, path)
	if err != nil {
		return nil, err
	}
	if testCase == nil {
		return cases, nil
	}
	if testCase.Definition.Matrix != nil {
		for _, matrix := range testCase.Definition.Matrix {
			newCase := *testCase
			newCase.Name = fmt.Sprintf("%s-%s", testCase.Name, matrix.Name)
			newCase.MatrixTestCaseName = matrix.Name
			newCase.Definition.DockerCompose = matrix.DockerCompose
			newCase.Definition.Kubernetes = matrix.Kubernetes
			cases = append(cases, newCase)
		}
	} else {
		cases = append(cases, *testCase)
	}
	return cases, nil
}

func absolutePath(dir string) string {
	abs, err := filepath.Abs(dir)
	if err != nil {
		panic(err)
	}
	return abs
}

func readTestCase(testBase, filePath string) (*model.TestCase, error) {
	def, err := readTestCaseDefinition(filePath, false)
	if def == nil {
		return nil, err
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
		Definition: *def,
	}
	return &testCase, nil
}

func readTestCaseDefinition(filePath string, templateMode bool) (*model.TestCaseDefinition, error) {
	filePath = absolutePath(filePath)
	content, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("failed to read file %s: %w", filePath, err)
	}
	var parsed map[string]interface{}
	err = yaml.Unmarshal(content, &parsed)
	if err != nil {
		return nil, fmt.Errorf("failed to parse file %s: %w", filePath, err)
	}
	fileVersion, ok := parsed["oats-file-version"]
	if !ok {
		// not an oats file
		return nil, nil
	}

	fileVersionStr, ok := fileVersion.(string)
	if !ok {
		return nil, parsingError(filePath, fmt.Errorf("oats-file-version '%v' is not a string", fileVersion))
	}
	if fileVersionStr != requiredOatsFileVersion {
		return nil, parsingError(filePath, fmt.Errorf("unsupported oats-file-version '%s' required version is '%s'",
			fileVersionStr, requiredOatsFileVersion))
	}

	template := parsed["oats-template"] == true
	if templateMode {
		if !template {
			return nil, fmt.Errorf("expected an oats template file %s", filePath)
		}
	} else {
		if template {
			// not a test case definition
			return nil, nil
		}
	}

	def := model.TestCaseDefinition{}
	dec := yaml.NewDecoder(bytes.NewReader(content))
	dec.KnownFields(true)
	err = dec.Decode(&def)
	if err != nil {
		return nil, parsingError(filePath, err)
	}

	for _, s := range def.Include {
		p := includePath(filePath, s)
		other, err := readTestCaseDefinition(p, true)
		if err != nil {
			return nil, err
		}
		if other == nil {
			return nil, fmt.Errorf("included file %s is not a valid oats test case definition", p)
		}
		def.Merge(*other)
	}
	def.Include = []string{}

	return &def, nil
}

func parsingError(filePath string, err error) error {
	return fmt.Errorf("error parsing test case definition %s - see migration notes at https://github.com/grafana/oats/blob/main/CHANGELOG.md - %w",
		filePath, err)
}

func includePath(filePath string, include string) string {
	dir := filepath.Dir(filePath)
	fromSlash := filepath.FromSlash(include)
	return filepath.Join(dir, fromSlash)
}
