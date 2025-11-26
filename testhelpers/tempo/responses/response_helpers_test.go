package responses

import (
	"os"
	"testing"

	"github.com/grafana/oats/model"
	"github.com/stretchr/testify/require"
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
	findSpans, _ := FindSpans(details, model.ExpectedSignal{
		Equals: "kafkaTopic publish",
	})
	require.Len(t, findSpans, 1)
	findSpans, _ = FindSpans(details, model.ExpectedSignal{
		Regexp: "kafkaTopic publish",
	})
	require.Len(t, findSpans, 1)
}
