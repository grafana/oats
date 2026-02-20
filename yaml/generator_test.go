package yaml

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildComposeArgs(t *testing.T) {
	t.Run("without user files", func(t *testing.T) {
		got := buildComposeArgs([]string{"/tmp/base.yml"}, nil)
		require.Equal(t, []string{
			"compose",
			"-f", "/tmp/base.yml",
			"config",
		}, got)
	})

	t.Run("with user files", func(t *testing.T) {
		got := buildComposeArgs(
			[]string{"/tmp/base.yml", "/home/user/project/docker-compose-generated.yml"},
			[]string{"/home/user/project/docker-compose.yml"},
		)
		require.Equal(t, []string{
			"compose",
			"--project-directory", "/home/user/project",
			"-f", "/tmp/base.yml",
			"-f", "/home/user/project/docker-compose-generated.yml",
			"config",
		}, got)
	})
}

func TestJoinDockerComposeFiles(t *testing.T) {
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
