package yaml

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestJoinDockerComposeFiles(t *testing.T) {
	a, err := os.ReadFile("testdata/docker-compose-a.yaml")
	require.NoError(t, err)
	b, err := os.ReadFile("testdata/docker-compose-b.yaml")
	require.NoError(t, err)
	want, err := os.ReadFile("testdata/docker-compose-expected.yaml")
	require.NoError(t, err)
	c, err := joinComposeFiles(a, b)
	require.NoError(t, err)
	require.Equal(t, string(want), string(c))
}
