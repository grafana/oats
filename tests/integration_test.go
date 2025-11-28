package tests

import (
	"os"
	"testing"
	"time"

	"github.com/grafana/oats/model"
	"github.com/grafana/oats/yaml"
	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestIntegration(t *testing.T) {
	key := "INTEGRATION_TESTS"
	if os.Getenv(key) != "true" {
		t.Skipf("skipping integration test; set %s=true to run", key)
	}

	promTest := model.Expected{
		Metrics: []model.ExpectedMetrics{
			{
				PromQL: "histogram_quantile(0.95, sum by(le) (rate(http_server_request_duration_seconds_bucket{http_route=\"/vets.html\"}[5m])))",
				Value:  "> 0",
			},
		},
	}
	tests := []struct {
		name        string
		shouldPanic bool
		expected    model.Expected
	}{
		{
			name:     "prometheus metrics pass",
			expected: promTest,
		},
	}

	failed := ""
	failFast := true
	gomega.RegisterFailHandler(func(message string, callerSkip ...int) {
		if failFast {
			panic(message)
		}
		failed = message
		t.Log("test failed:", message)
	})

	buildDir := yaml.PrepareBuildDir("integration-test")
	c := &model.TestCase{
		Name:      "integration-test",
		OutputDir: buildDir,
		Definition: model.TestCaseDefinition{
			DockerCompose: &model.DockerCompose{Files: []string{
				"docker-compose.oats.yml",
			}},
			Input: []model.Input{{
				Path: "/vets.html",
			}},
			Expected: promTest,
		},
	}
	c.ValidateAndSetVariables(gomega.Default)

	settings := model.Settings{
		Host:     "localhost",
		Timeout:  5 * time.Minute,
		LogLimit: 1000,
		LgtmLogSettings: map[string]bool{
			"ENABLE_LOGS_ALL": false,
		},
		LgtmVersion: "latest",
	}

	r := yaml.NewRunner(c, settings)

	t.Log("start docker compose")

	end, err := r.StartEndpoint()
	assert.NoError(t, err, "expected no error starting an observability endpoint")
	defer end()

	t.Log("finished starting docker compose")

	assert.Empty(t, failed, "expected no failure starting docker compose")

	failFast = false

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c.Name = tt.name
			c.Definition.Expected = tt.expected

			failed = ""
			r.ExecuteChecks()

			if tt.shouldPanic {
				require.NotEmpty(t, failed)
			} else {
				require.Empty(t, failed)
			}
		})
	}
}
