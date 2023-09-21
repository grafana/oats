package yaml

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestJoinDockerComposeFiles(t *testing.T) {
	AssumeNoYamlTest(t)

	template, err := os.ReadFile("testdata/docker-compose-template.yaml")
	require.NoError(t, err)
	add, err := os.ReadFile("testdata/docker-compose-addition.yaml")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/docker-compose-expected.yaml")
	require.NoError(t, err)
	c, err := joinComposeFiles(template, add)
	require.NoError(t, err)
	require.YAMLEq(t, string(want), string(c))
}
