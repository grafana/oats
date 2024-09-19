package yaml

import (
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
)

func TestAssertLokiResponse(t *testing.T) {
	file, err := os.ReadFile("testdata/loki_response.json")
	require.NoError(t, err)
	logs := ExpectedLogs{
		Contains: []string{"Anonymous player is rolling the dice"},
		Attributes: map[string]string{
			"scope_name":        "com.grafana.example.RollController",
			"service_name":      "dice",
			"service_namespace": "shop",
			"severity_number":   "9",
			"severity_text":     "INFO",
		},
		AttributeRegexp: map[string]string{
			"span_id":  ".*",
			"trace_id": ".*",
		},
	}
	AssertLokiResponse(gomega.NewGomega(func(message string, callerSkip ...int) {
		t.Error(message)
	}), file, logs, nil)
}
