package yaml

import (
	"path/filepath"
	"testing"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestReadTestCaseDefinition(t *testing.T) {
	def, err := readTestCaseDefinition("testdata/valid-tests/oats.yaml", false)
	require.NoError(t, err)
	merged, err := readTestCaseDefinition("testdata/oats-merged.yaml", false)
	require.NoError(t, err)
	require.Equal(t, merged, def)
}

func TestReadTestCase(t *testing.T) {
	tc, err := readTestCase("testdata", "testdata/valid-tests/oats.yaml")
	require.NoError(t, err)
	require.Equal(t, "runvalid-tests-oats", tc.Name)
	require.Equal(t, absolutePath("testdata/valid-tests"), tc.Dir)
}

func TestIncludePath(t *testing.T) {
	require.Equal(t,
		filepath.FromSlash("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/oats-non-reactive.yaml"),
		includePath("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/spring-boot-non-reactive-2.7/oats.yaml", "../oats-non-reactive.yaml"))
}

func TestInputDefinitionsAreCorrect(t *testing.T) {
	def, err := readTestCaseDefinition("testdata/valid-tests/input.oats.yaml", false)
	require.NoError(t, err)

	expected := &model.TestCaseDefinition{
		Input: []model.Input{
			{
				Path: "/stock",
			},
			{
				Path:   "/buy",
				Method: "POST",
				Headers: map[string]string{
					"Authorization": "Bearer user-token",
					"Content-Type":  "application/json",
				},
				Body:   `{"id": "42", "quantity": 10}`,
				Status: "201",
			},
			{
				Path:   "/delist/42",
				Scheme: "https",
				Host:   "127.0.0.1",
				Method: "DELETE",
				Headers: map[string]string{
					"Authorization": "Bearer admin-token",
				},
				Status: "204",
			},
		},
	}

	require.Equal(t, expected.Input, def.Input)
}
func TestInputDefinitionsInvalidFiles(t *testing.T) {
	tests := []struct {
		name     string
		filePath string
		errorMsg string
	}{
		{
			name:     "malformed yaml",
			filePath: "testdata/invalid-tests/malformed-yaml.yaml",
			errorMsg: "failed to parse file .*/yaml/testdata/invalid-tests/malformed-yaml.yaml: yaml: mapping values are not allowed in this context",
		},
		{
			name:     "outdated file version",
			filePath: "testdata/invalid-tests/outdated-version.yaml",
			errorMsg: "error parsing test case definition .*/yaml/testdata/invalid-tests/outdated-version.yaml - " +
				"see migration notes at https://github.com/grafana/oats/blob/main/CHANGELOG.md - unsupported oats-schema-version '1.000000' required version is '2'",
		},
		{
			name:     "file version is not a number",
			filePath: "testdata/invalid-tests/version-not-int.yaml",
			errorMsg: "error parsing test case definition .*/yaml/testdata/invalid-tests/version-not-int.yaml - " +
				"see migration notes at https://github.com/grafana/oats/blob/main/CHANGELOG.md - oats-schema-version '1' is not a number",
		},
		{
			name:     "unknown field",
			filePath: "testdata/invalid-tests/unknown-field.yaml",
			errorMsg: "error parsing test case definition .*/yaml/testdata/invalid-tests/unknown-field.yaml - " +
				"see migration notes at https://github.com/grafana/oats/blob/main/CHANGELOG.md - yaml: unmarshal errors:\n" +
				".*line 5: field spans not found in type model.ExpectedTraces",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := readTestCaseDefinition(tt.filePath, false)
			require.NotNil(t, err)
			require.Regexp(t, tt.errorMsg, err.Error())
		})
	}
}

func TestCollectTestCases(t *testing.T) {
	testCases := []struct {
		name               string
		input              []string
		evaluateIgnoreFile bool
		expectedNames      []string
	}{
		{
			name:               "without ignore file evaluation",
			input:              []string{"testdata/valid-tests"},
			evaluateIgnoreFile: false,
			expectedNames: []string{
				"run-expect-absent.oats",
				"run-input.oats",
				"run-more-oats",
				"run-oats",
				"run-matrix-test.oats-docker",       // matrix expansion
				"run-matrix-test.oats-k8s",          // matrix expansion
				"runignored-should-not-appear.oats", // included when not evaluating ignore
			},
		},
		{
			name:               "with ignore file evaluation",
			input:              []string{"testdata/valid-tests"},
			evaluateIgnoreFile: true,
			expectedNames: []string{
				"run-expect-absent.oats",
				"run-input.oats",
				"run-more-oats",
				"run-oats",
				"run-matrix-test.oats-docker", // matrix expansion
				"run-matrix-test.oats-k8s",    // matrix expansion
			},
		},
		{
			name:               "2 explicit files",
			input:              []string{"testdata/valid-tests/oats.yaml", "testdata/valid-tests/more-oats.yml"},
			evaluateIgnoreFile: true,
			expectedNames: []string{
				"run-oats",
				"run-more-oats",
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cases, err := ReadTestCases(tc.input, tc.evaluateIgnoreFile)
			require.NoError(t, err)

			// Collect all case names for easier assertion
			actualNames := make([]string, len(cases))
			for i, c := range cases {
				actualNames[i] = c.Name
			}

			// Check that all expected names are present
			require.ElementsMatch(t, tc.expectedNames, actualNames)
			require.Len(t, actualNames, len(tc.expectedNames))
		})
	}
}

func TestTestCasesAreValid(t *testing.T) {
	cases, err := ReadTestCases([]string{"testdata/valid-tests"}, false)
	require.NoError(t, err)
	require.NotEmpty(t, cases)
	for _, c := range cases {
		require.NotEqual(t, nil, c.Definition)
		require.NotEmpty(t, c.Definition.Input)
		model.ValidateInput(gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		}), c.Definition.Input)
	}
}
