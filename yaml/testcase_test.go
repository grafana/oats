package yaml

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestReadTestCaseDefinition(t *testing.T) {
	AssumeNoYamlTest(t)

	def, err := readTestCaseDefinition("testdata/foo/oats.yaml")
	require.NoError(t, err)
	merged, err := readTestCaseDefinition("testdata/oats-merged.yaml")
	require.NoError(t, err)
	require.Equal(t, merged, def)
}

func TestReadTestCase(t *testing.T) {
	AssumeNoYamlTest(t)

	tc, err := readTestCase("testdata", "testdata/foo/oats.yaml", 0)
	require.NoError(t, err)
	require.Equal(t, "foo", tc.Name)
	require.Equal(t, absolutePath("testdata/foo"), tc.Dir)
}
