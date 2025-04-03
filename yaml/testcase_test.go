package yaml

import (
	"github.com/stretchr/testify/require"
	"path/filepath"
	"testing"
)

func TestReadTestCaseDefinition(t *testing.T) {

	def, err := readTestCaseDefinition("testdata/foo/oats.yaml")
	require.NoError(t, err)
	merged, err := readTestCaseDefinition("testdata/oats-merged.yaml")
	require.NoError(t, err)
	require.Equal(t, merged, def)
}

func TestReadTestCase(t *testing.T) {

	tc, err := readTestCase("testdata", "testdata/foo/oats.yaml", 0)
	require.NoError(t, err)
	require.Equal(t, "runfoo-oats", tc.Name)
	require.Equal(t, absolutePath("testdata/foo"), tc.Dir)
}

func TestIncludePath(t *testing.T) {

	require.Equal(t,
		filepath.FromSlash("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/oats-non-reactive.yaml"),
		includePath("/home/gregor/source/grafana-opentelemetry-java/examples/jdbc/spring-boot-non-reactive-2.7/oats.yaml", "../oats-non-reactive.yaml"))
}

func TestCollectTestCases(t *testing.T) {
	cases, err := collectTestCases("testdata", 0, false)
	require.NoError(t, err)
	require.Len(t, cases, 2)
	require.Equal(t, "runfoo-oats", cases[0].Name)
	require.Equal(t, "run-oats-merged", cases[1].Name)
}
