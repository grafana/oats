package yaml

import (
	"path/filepath"
	"testing"

	"github.com/grafana/oats/model"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestReadTestCaseDefinition(t *testing.T) {
	def, err := readTestCaseDefinition("testdata/foo/oats.yaml")
	require.NoError(t, err)
	merged, err := readTestCaseDefinition("testdata/oats-merged.yaml")
	require.NoError(t, err)
	require.Equal(t, merged, def)
}

func TestReadTestCase(t *testing.T) {
	tc, err := readTestCase("testdata", "testdata/foo/oats.yaml")
	require.NoError(t, err)
	require.Equal(t, "runfoo-oats", tc.Name)
	require.Equal(t, absolutePath("testdata/foo"), tc.Dir)
}

func TestIncludePath(t *testing.T) {
	require.Equal(t,
		filepath.FromSlash("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/oats-non-reactive.yaml"),
		includePath("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/spring-boot-non-reactive-2.7/oats.yaml", "../oats-non-reactive.yaml"))
}

func TestInputDefinitionsAreCorrect(t *testing.T) {
	def, err := readTestCaseDefinition("testdata/foo/input.oats.yaml")
	require.NoError(t, err)
	require.Len(t, def.Input, 3)
	item := def.Input[0]
	require.Equal(t, "/stock", item.Path)
	require.Equal(t, "", item.Scheme)
	require.Equal(t, "", item.Host)
	require.Equal(t, "", item.Method)
	require.Empty(t, item.Headers)
	require.Equal(t, "", item.Body)
	require.Equal(t, "", item.Status)
	item = def.Input[1]
	require.Equal(t, "/buy", item.Path)
	require.Equal(t, "", item.Scheme)
	require.Equal(t, "", item.Host)
	require.Equal(t, "POST", item.Method)
	require.Len(t, item.Headers, 2)
	require.Equal(t, "Bearer user-token", item.Headers["Authorization"])
	require.Equal(t, "application/json", item.Headers["Content-Type"])
	require.Equal(t, "{\"id\": \"42\", \"quantity\": 10}", item.Body)
	require.Equal(t, "201", item.Status)
	item = def.Input[2]
	require.Equal(t, "/delist/42", item.Path)
	require.Equal(t, "https", item.Scheme)
	require.Equal(t, "127.0.0.1", item.Host)
	require.Equal(t, "DELETE", item.Method)
	require.Len(t, item.Headers, 1)
	require.Equal(t, "Bearer admin-token", item.Headers["Authorization"])
	require.Equal(t, "", item.Body)
	require.Equal(t, "204", item.Status)
}

func TestCollectTestCases(t *testing.T) {
	testCases := []struct {
		name               string
		basePath           string
		evaluateIgnoreFile bool
		expectedCount      int
		expectedNames      []string
		notExpectedNames   []string
	}{
		{
			name:               "without ignore file evaluation",
			basePath:           "testdata",
			evaluateIgnoreFile: false,
			expectedCount:      7, // includes matrix expansions (2) and ignored file (1)
			expectedNames: []string{
				"runfoo-expect-absent.oats",
				"runfoo-input.oats",
				"runfoo-more-oats",
				"runfoo-oats",
				"run-oats-merged",
				"run-matrix-test.oats-docker", // matrix expansion
				"run-matrix-test.oats-k8s",    // matrix expansion
			},
		},
		{
			name:               "with ignore file evaluation",
			basePath:           "testdata",
			evaluateIgnoreFile: true,
			expectedCount:      6, // excludes ignored directory
			expectedNames: []string{
				"runfoo-expect-absent.oats",
				"runfoo-input.oats",
				"runfoo-more-oats",
				"runfoo-oats",
				"run-oats-merged",
				"run-matrix-test.oats-docker", // matrix expansion
				"run-matrix-test.oats-k8s",    // matrix expansion
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cases, err := collectTestCases(tc.basePath, tc.evaluateIgnoreFile)
			require.NoError(t, err)
			require.Len(t, cases, tc.expectedCount, "expected %d test cases", tc.expectedCount)

			// Collect all case names for easier assertion
			actualNames := make([]string, len(cases))
			for i, c := range cases {
				actualNames[i] = c.Name
			}

			// Check that all expected names are present
			require.ElementsMatch(t, actualNames, tc.expectedNames, "expected test case %s to be present", tc.expectedNames)
		})
	}
}

func TestCollectTestCasesWithMatrix(t *testing.T) {
	cases, err := collectTestCases("testdata", false)
	require.NoError(t, err)

	// Find the matrix test cases
	var matrixCases []*model.TestCase
	for _, c := range cases {
		if c.Name == "run-matrix-test.oats-docker" || c.Name == "run-matrix-test.oats-k8s" {
			matrixCases = append(matrixCases, c)
		}
	}

	require.Len(t, matrixCases, 2, "expected 2 matrix-expanded test cases")

	// Verify docker case
	dockerCase := matrixCases[0]
	if dockerCase.Name != "run-matrix-test.oats-docker" {
		dockerCase = matrixCases[1]
	}
	require.Equal(t, "run-matrix-test.oats-docker", dockerCase.Name)
	require.Equal(t, "docker", dockerCase.MatrixTestCaseName)
	require.NotNil(t, dockerCase.Definition.DockerCompose)
	require.Contains(t, dockerCase.Definition.DockerCompose.Files, "docker-compose-template.yaml")
	require.Nil(t, dockerCase.Definition.Kubernetes)

	// Verify k8s case
	k8sCase := matrixCases[1]
	if k8sCase.Name != "run-matrix-test.oats-k8s" {
		k8sCase = matrixCases[0]
	}
	require.Equal(t, "run-matrix-test.oats-k8s", k8sCase.Name)
	require.Equal(t, "k8s", k8sCase.MatrixTestCaseName)
	require.NotNil(t, k8sCase.Definition.Kubernetes)
	require.Nil(t, k8sCase.Definition.DockerCompose)
}

func TestTestCasesAreValid(t *testing.T) {
	cases, err := collectTestCases("testdata", false)
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
