package yaml

import (
	"path/filepath"
	"testing"

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
	cases, err := collectTestCases("testdata", false)
	require.NoError(t, err)
	require.Len(t, cases, 5)
	require.Equal(t, "runfoo-expect-absent.oats", cases[0].Name)
	require.Equal(t, "runfoo-input.oats", cases[1].Name)
	require.Equal(t, "runfoo-more-oats", cases[2].Name)
	require.Equal(t, "runfoo-oats", cases[3].Name)
	require.Equal(t, "run-oats-merged", cases[4].Name)
}
