package responses

import (
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestParseTraceDetails(t *testing.T) {
	b, err := os.ReadFile("testdata/trace_by_id.json")
	require.NoError(t, err)
	details, err := ParseTraceDetails(b)
	require.NoError(t, err)
	spans := details.ResourceSpans()
	i := spans.Len()
	require.NotZero(t, i)
	require.NotEmpty(t, details)
	require.Len(t, FindSpans(details, "kafkaTopic publish"), 1)
	require.Len(t, FindSpans(details, "regex:.* publish"), 1)
}
