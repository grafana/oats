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
			name:        "prometheus metrics pass",
			shouldPanic: false,
			expected:    promTest,
		},
		{
			name:        "multiple metrics pass",
			shouldPanic: false,
			expected: model.Expected{
				Metrics: []model.ExpectedMetrics{
					{
						PromQL: "histogram_quantile(0.95, sum by(le) (rate(http_server_request_duration_seconds_bucket{http_route=\"/vets.html\"}[5m])))",
						Value:  "> 0",
					},
					{
						PromQL: "rate(http_server_request_duration_seconds_count{http_route=\"/vets.html\"}[5m])",
						Value:  "> 0",
					},
				},
			},
		},
		{
			name:        "logs check pass",
			shouldPanic: false,
			expected: model.Expected{
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{service_name=\"spring-pet-clinic\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 1, Max: 1000},
						},
					},
				},
			},
		},
		{
			name:        "traces check pass",
			shouldPanic: false,
			expected: model.Expected{
				Traces: []model.ExpectedTraces{
					{
						TraceQL: "{name=\"GET /vets.html\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 1, Max: 100},
						},
					},
				},
			},
		},
		{
			name:        "composed checks with metrics and logs",
			shouldPanic: false,
			expected: model.Expected{
				Metrics: []model.ExpectedMetrics{
					{
						PromQL: "histogram_quantile(0.95, sum by(le) (rate(http_server_request_duration_seconds_bucket{http_route=\"/vets.html\"}[5m])))",
						Value:  "> 0",
					},
				},
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{service_name=\"spring-pet-clinic\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 1, Max: 1000},
						},
					},
				},
			},
		},
		{
			name:        "successfully check for absence - non-existent logs",
			shouldPanic: false,
			expected: model.Expected{
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{job=\"nonexistent-job\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 0, Max: 0},
						},
					},
				},
			},
		},
		{
			name:        "successfully check for absence - non-existent traces",
			shouldPanic: false,
			expected: model.Expected{
				Traces: []model.ExpectedTraces{
					{
						TraceQL: "{name=\"/nonexistent-endpoint\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 0, Max: 0},
						},
					},
				},
			},
		},
		{
			name:        "successfully check for absence - non-existent service logs",
			shouldPanic: false,
			expected: model.Expected{
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{service_name=\"service-that-does-not-exist\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 0, Max: 0},
						},
					},
				},
			},
		},
		{
			name:        "successfully check for absence - traces with specific attribute",
			shouldPanic: false,
			expected: model.Expected{
				Traces: []model.ExpectedTraces{
					{
						TraceQL: "{.http.status_code=999}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 0, Max: 0},
						},
					},
				},
			},
		},
		{
			name:        "should fail - checking for absence when logs exist",
			shouldPanic: true,
			expected: model.Expected{
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{service_name=\"spring-pet-clinic\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 0, Max: 0},
						},
					},
				},
			},
		},
		{
			name:        "should fail - checking for absence when traces exist",
			shouldPanic: true,
			expected: model.Expected{
				Traces: []model.ExpectedTraces{
					{
						TraceQL: "{name=\"GET /vets.html\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 0, Max: 0},
						},
					},
				},
			},
		},
		{
			name:        "should fail - metric value doesn't match condition",
			shouldPanic: true,
			expected: model.Expected{
				Metrics: []model.ExpectedMetrics{
					{
						PromQL: "histogram_quantile(0.95, sum by(le) (rate(http_server_request_duration_seconds_bucket{http_route=\"/vets.html\"}[5m])))",
						Value:  "< 0",
					},
				},
			},
		},
		{
			name:        "should fail - looking for non-existent logs",
			shouldPanic: true,
			expected: model.Expected{
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{job=\"nonexistent-job\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 1, Max: 1000},
						},
					},
				},
			},
		},
		{
			name:        "should fail - looking for non-existent traces",
			shouldPanic: true,
			expected: model.Expected{
				Traces: []model.ExpectedTraces{
					{
						TraceQL: "{name=\"/nonexistent-endpoint\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 1, Max: 100},
						},
					},
				},
			},
		},
		{
			name:        "should fail - count range too restrictive",
			shouldPanic: true,
			expected: model.Expected{
				Logs: []model.ExpectedLogs{
					{
						LogQL: "{service_name=\"spring-pet-clinic\"}",
						Signal: model.ExpectedSignal{
							Count: &model.ExpectedRange{Min: 10000, Max: 20000},
						},
					},
				},
			},
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

	checkTimeout := 2 * time.Second
	startupTimeout := 2 * time.Minute

	settings := model.Settings{
		Host:           "localhost",
		Timeout:        startupTimeout,
		PresentTimeout: startupTimeout, // first test needs more time to allow docker compose to start
		AbsentTimeout:  checkTimeout,
		LogLimit:       1000,
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

			// docker compose is already started, so speed up the checks
			r.Settings.PresentTimeout = checkTimeout

			if tt.shouldPanic {
				require.NotEmpty(t, failed)
			} else {
				require.Empty(t, failed)
			}
		})
	}
}
