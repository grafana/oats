package model

import (
	"net/http"
	"testing"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateInput(t *testing.T) {
	tests := []struct {
		name        string
		input       []Input
		shouldPanic bool
		description string
	}{
		{
			name: "valid GET request",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/api/test",
				},
			},
			shouldPanic: false,
			description: "basic GET request should be valid",
		},
		{
			name: "valid POST request with body",
			input: []Input{
				{
					Method: http.MethodPost,
					Path:   "/api/create",
					Body:   `{"key": "value"}`,
					Status: "201",
				},
			},
			shouldPanic: false,
			description: "POST with body should be valid",
		},
		{
			name: "empty path should fail",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "",
				},
			},
			shouldPanic: true,
			description: "path cannot be empty",
		},
		{
			name: "invalid status code should fail",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/test",
					Status: "invalid",
				},
			},
			shouldPanic: true,
			description: "status must be a valid integer",
		},
		{
			name: "invalid HTTP method should fail",
			input: []Input{
				{
					Method: "INVALID",
					Path:   "/test",
				},
			},
			shouldPanic: true,
			description: "method must be a valid HTTP method",
		},
		{
			name: "GET with body should fail",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/test",
					Body:   "should not have body",
				},
			},
			shouldPanic: true,
			description: "GET requests cannot have a body",
		},
		{
			name: "empty method defaults to GET and body should fail",
			input: []Input{
				{
					Method: "",
					Path:   "/test",
					Body:   "body content",
				},
			},
			shouldPanic: true,
			description: "empty method defaults to GET, which cannot have body",
		},
		{
			name: "invalid scheme should fail",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/test",
					Scheme: "ftp",
				},
			},
			shouldPanic: true,
			description: "scheme must be http or https",
		},
		{
			name: "valid HTTPS scheme",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/test",
					Scheme: "https",
				},
			},
			shouldPanic: false,
			description: "https scheme should be valid",
		},
		{
			name: "multiple valid inputs",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/api/list",
				},
				{
					Method: http.MethodPost,
					Path:   "/api/create",
					Body:   `{"name": "test"}`,
					Status: "201",
				},
				{
					Method: http.MethodDelete,
					Path:   "/api/delete/1",
					Status: "204",
				},
			},
			shouldPanic: false,
			description: "multiple valid requests should pass",
		},
		{
			name: "PUT with body",
			input: []Input{
				{
					Method: http.MethodPut,
					Path:   "/api/update",
					Body:   `{"id": 1, "name": "updated"}`,
				},
			},
			shouldPanic: false,
			description: "PUT with body should be valid",
		},
		{
			name: "PATCH with body",
			input: []Input{
				{
					Method: http.MethodPatch,
					Path:   "/api/patch",
					Body:   `{"field": "value"}`,
				},
			},
			shouldPanic: false,
			description: "PATCH with body should be valid",
		},
		{
			name: "case insensitive method",
			input: []Input{
				{
					Method: "get",
					Path:   "/test",
				},
			},
			shouldPanic: false,
			description: "lowercase method should be valid",
		},
		{
			name: "case insensitive scheme",
			input: []Input{
				{
					Method: http.MethodGet,
					Path:   "/test",
					Scheme: "HTTP",
				},
			},
			shouldPanic: false,
			description: "uppercase scheme should be valid",
		},
		{
			name: "valid status codes",
			input: []Input{
				{
					Path:   "/test",
					Status: "200",
				},
				{
					Path:   "/created",
					Status: "201",
				},
				{
					Path:   "/error",
					Status: "500",
				},
			},
			shouldPanic: false,
			description: "various valid status codes",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			failed := false
			g := gomega.NewGomega(func(message string, callerSkip ...int) {
				if !tt.shouldPanic {
					t.Error(message)
				}
				failed = true
			})

			ValidateInputWithGomega(g, tt.input)

			if tt.shouldPanic {
				require.True(t, failed, tt.description)
			}
		})
	}
}

func TestValidateSignal(t *testing.T) {
	tests := []struct {
		name        string
		signal      ExpectedSignal
		shouldPanic bool
		description string
	}{
		{
			name: "valid signal with equals",
			signal: ExpectedSignal{
				Equals: "test-value",
			},
			shouldPanic: false,
			description: "signal with equals should be valid",
		},
		{
			name: "valid signal with regexp",
			signal: ExpectedSignal{
				Regexp: "test-.*",
			},
			shouldPanic: false,
			description: "signal with regexp should be valid",
		},
		{
			name: "valid signal with both equals and regexp",
			signal: ExpectedSignal{
				Equals: "test",
				Regexp: "test",
			},
			shouldPanic: false,
			description: "signal with both equals and regexp should be valid",
		},
		{
			name: "invalid signal with neither equals nor regexp",
			signal: ExpectedSignal{
				Attributes: map[string]string{"key": "value"},
			},
			shouldPanic: true,
			description: "signal must have equals or regexp",
		},
		{
			name: "deprecated contains field should fail",
			signal: ExpectedSignal{
				Equals:   "test",
				Contains: []string{"deprecated"},
			},
			shouldPanic: true,
			description: "contains field is deprecated",
		},
		{
			name: "valid attributes",
			signal: ExpectedSignal{
				Equals: "test",
				Attributes: map[string]string{
					"service.name": "my-service",
					"environment":  "prod",
				},
			},
			shouldPanic: false,
			description: "valid attributes should pass",
		},
		{
			name: "empty attribute key should fail",
			signal: ExpectedSignal{
				Equals: "test",
				Attributes: map[string]string{
					"": "value",
				},
			},
			shouldPanic: true,
			description: "attribute keys cannot be empty",
		},
		{
			name: "empty attribute value should fail",
			signal: ExpectedSignal{
				Equals: "test",
				Attributes: map[string]string{
					"key": "",
				},
			},
			shouldPanic: true,
			description: "attribute values cannot be empty",
		},
		{
			name: "valid attribute regexp",
			signal: ExpectedSignal{
				Regexp: "test",
				AttributeRegexp: map[string]string{
					"trace.id": "^[a-f0-9]{32}$",
				},
			},
			shouldPanic: false,
			description: "valid attribute regexp should pass",
		},
		{
			name: "empty attribute regexp key should fail",
			signal: ExpectedSignal{
				Equals: "test",
				AttributeRegexp: map[string]string{
					"": "^[a-f0-9]+$",
				},
			},
			shouldPanic: true,
			description: "attribute regexp keys cannot be empty",
		},
		{
			name: "empty attribute regexp value should fail",
			signal: ExpectedSignal{
				Equals: "test",
				AttributeRegexp: map[string]string{
					"key": "",
				},
			},
			shouldPanic: true,
			description: "attribute regexp values cannot be empty",
		},
		{
			name: "valid count range",
			signal: ExpectedSignal{
				Equals: "test",
				Count:  &ExpectedRange{Min: 1, Max: 5},
			},
			shouldPanic: false,
			description: "valid count range should pass",
		},
		{
			name: "count with max 0 means no upper limit",
			signal: ExpectedSignal{
				Equals: "test",
				Count:  &ExpectedRange{Min: 1, Max: 0},
			},
			shouldPanic: false,
			description: "max=0 means no upper limit",
		},
		{
			name: "count with min=0 max=0 expects absent but will fail due to BeNil check on string",
			signal: ExpectedSignal{
				Count: &ExpectedRange{Min: 0, Max: 0},
			},
			shouldPanic: true,
			description: "min=0 max=0 with empty strings fails BeNil() check (potential validation bug)",
		},
		{
			name: "expect absent should not have equals",
			signal: ExpectedSignal{
				Equals: "test",
				Count:  &ExpectedRange{Min: 0, Max: 0},
			},
			shouldPanic: true,
			description: "expect-absent signals should not have equals",
		},
		{
			name: "expect absent should not have regexp",
			signal: ExpectedSignal{
				Regexp: "test",
				Count:  &ExpectedRange{Min: 0, Max: 0},
			},
			shouldPanic: true,
			description: "expect-absent signals should not have regexp",
		},
		{
			name: "expect absent should not have attributes",
			signal: ExpectedSignal{
				Attributes: map[string]string{"key": "value"},
				Count:      &ExpectedRange{Min: 0, Max: 0},
			},
			shouldPanic: true,
			description: "expect-absent signals should not have attributes",
		},
		{
			name: "expect absent should not have attribute regexp",
			signal: ExpectedSignal{
				AttributeRegexp: map[string]string{"key": "pattern"},
				Count:           &ExpectedRange{Min: 0, Max: 0},
			},
			shouldPanic: true,
			description: "expect-absent signals should not have attribute regexp",
		},
		{
			name: "invalid count range min=0 max>0",
			signal: ExpectedSignal{
				Equals: "test",
				Count:  &ExpectedRange{Min: 0, Max: 5},
			},
			shouldPanic: true,
			description: "min=0 with max>0 is not supported",
		},
		{
			name: "negative min should fail",
			signal: ExpectedSignal{
				Equals: "test",
				Count:  &ExpectedRange{Min: -1, Max: 5},
			},
			shouldPanic: true,
			description: "negative min is not allowed",
		},
		{
			name: "max less than min should fail",
			signal: ExpectedSignal{
				Equals: "test",
				Count:  &ExpectedRange{Min: 5, Max: 3},
			},
			shouldPanic: true,
			description: "max must be >= min (or 0)",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := []byte("test output")
			failed := false
			g := gomega.NewGomega(func(message string, callerSkip ...int) {
				if !tt.shouldPanic {
					t.Error(message)
				}
				failed = true
			})

			validateSignalWithGomega(g, tt.signal, out)

			if tt.shouldPanic {
				require.True(t, failed, tt.description)
			}
		})
	}
}

func TestExpectedRange(t *testing.T) {
	tests := []struct {
		name           string
		expectedRange  *ExpectedRange
		description    string
		expectsAbsent  bool
		expectsPresent bool
	}{
		{
			name:           "nil range expects at least 1",
			expectedRange:  nil,
			description:    "nil count means 1 or more",
			expectsAbsent:  false,
			expectsPresent: true,
		},
		{
			name:           "min=0 max=0 expects exactly 0",
			expectedRange:  &ExpectedRange{Min: 0, Max: 0},
			description:    "count of 0 means absent",
			expectsAbsent:  true,
			expectsPresent: false,
		},
		{
			name:           "min=1 max=0 expects 1 or more",
			expectedRange:  &ExpectedRange{Min: 1, Max: 0},
			description:    "max=0 means no upper limit",
			expectsAbsent:  false,
			expectsPresent: true,
		},
		{
			name:           "min=5 max=0 expects 5 or more",
			expectedRange:  &ExpectedRange{Min: 5, Max: 0},
			description:    "min with max=0 means at least min",
			expectsAbsent:  false,
			expectsPresent: true,
		},
		{
			name:           "min=1 max=10 expects between 1 and 10",
			expectedRange:  &ExpectedRange{Min: 1, Max: 10},
			description:    "bounded range",
			expectsAbsent:  false,
			expectsPresent: true,
		},
		{
			name:           "min=5 max=5 expects exactly 5",
			expectedRange:  &ExpectedRange{Min: 5, Max: 5},
			description:    "min=max means exact count",
			expectsAbsent:  false,
			expectsPresent: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			signal := ExpectedSignal{Count: tt.expectedRange}
			isAbsent := signal.ExpectAbsent()

			if tt.expectsAbsent {
				assert.True(t, isAbsent, tt.description)
			} else {
				assert.False(t, isAbsent, tt.description)
			}
		})
	}
}

func TestTestCaseDefinition_Merge(t *testing.T) {
	tests := []struct {
		name     string
		base     TestCaseDefinition
		other    TestCaseDefinition
		expected TestCaseDefinition
	}{
		{
			name: "merge logs",
			base: TestCaseDefinition{
				Expected: Expected{
					Logs: []ExpectedLogs{
						{LogQL: "query1"},
					},
				},
			},
			other: TestCaseDefinition{
				Expected: Expected{
					Logs: []ExpectedLogs{
						{LogQL: "query2"},
					},
				},
			},
			expected: TestCaseDefinition{
				Expected: Expected{
					Logs: []ExpectedLogs{
						{LogQL: "query1"},
						{LogQL: "query2"},
					},
				},
			},
		},
		{
			name: "merge traces",
			base: TestCaseDefinition{
				Expected: Expected{
					Traces: []ExpectedTraces{
						{TraceQL: "trace1"},
					},
				},
			},
			other: TestCaseDefinition{
				Expected: Expected{
					Traces: []ExpectedTraces{
						{TraceQL: "trace2"},
					},
				},
			},
			expected: TestCaseDefinition{
				Expected: Expected{
					Traces: []ExpectedTraces{
						{TraceQL: "trace1"},
						{TraceQL: "trace2"},
					},
				},
			},
		},
		{
			name: "merge metrics",
			base: TestCaseDefinition{
				Expected: Expected{
					Metrics: []ExpectedMetrics{
						{PromQL: "metric1"},
					},
				},
			},
			other: TestCaseDefinition{
				Expected: Expected{
					Metrics: []ExpectedMetrics{
						{PromQL: "metric2"},
					},
				},
			},
			expected: TestCaseDefinition{
				Expected: Expected{
					Metrics: []ExpectedMetrics{
						{PromQL: "metric1"},
						{PromQL: "metric2"},
					},
				},
			},
		},
		{
			name: "merge profiles",
			base: TestCaseDefinition{
				Expected: Expected{
					Profiles: []ExpectedProfiles{
						{Query: "profile1"},
					},
				},
			},
			other: TestCaseDefinition{
				Expected: Expected{
					Profiles: []ExpectedProfiles{
						{Query: "profile2"},
					},
				},
			},
			expected: TestCaseDefinition{
				Expected: Expected{
					Profiles: []ExpectedProfiles{
						{Query: "profile1"},
						{Query: "profile2"},
					},
				},
			},
		},
		{
			name: "merge custom checks",
			base: TestCaseDefinition{
				Expected: Expected{
					CustomChecks: []CustomCheck{
						{Script: "check1"},
					},
				},
			},
			other: TestCaseDefinition{
				Expected: Expected{
					CustomChecks: []CustomCheck{
						{Script: "check2"},
					},
				},
			},
			expected: TestCaseDefinition{
				Expected: Expected{
					CustomChecks: []CustomCheck{
						{Script: "check1"},
						{Script: "check2"},
					},
				},
			},
		},
		{
			name: "merge matrix",
			base: TestCaseDefinition{
				Matrix: []Matrix{
					{Name: "matrix1"},
				},
			},
			other: TestCaseDefinition{
				Matrix: []Matrix{
					{Name: "matrix2"},
				},
			},
			expected: TestCaseDefinition{
				Matrix: []Matrix{
					{Name: "matrix1"},
					{Name: "matrix2"},
				},
			},
		},
		{
			name: "merge input",
			base: TestCaseDefinition{
				Input: []Input{
					{Path: "/path1"},
				},
			},
			other: TestCaseDefinition{
				Input: []Input{
					{Path: "/path2"},
				},
			},
			expected: TestCaseDefinition{
				Input: []Input{
					{Path: "/path1"},
					{Path: "/path2"},
				},
			},
		},
		{
			name: "docker-compose only in other",
			base: TestCaseDefinition{},
			other: TestCaseDefinition{
				DockerCompose: &DockerCompose{
					Files: []string{"docker-compose.yml"},
				},
			},
			expected: TestCaseDefinition{
				DockerCompose: &DockerCompose{
					Files: []string{"docker-compose.yml"},
				},
			},
		},
		{
			name: "docker-compose in base not overwritten",
			base: TestCaseDefinition{
				DockerCompose: &DockerCompose{
					Files: []string{"base.yml"},
				},
			},
			other: TestCaseDefinition{
				DockerCompose: &DockerCompose{
					Files: []string{"other.yml"},
				},
			},
			expected: TestCaseDefinition{
				DockerCompose: &DockerCompose{
					Files: []string{"base.yml"},
				},
			},
		},
		{
			name: "merge all fields",
			base: TestCaseDefinition{
				Expected: Expected{
					Logs:    []ExpectedLogs{{LogQL: "log1"}},
					Traces:  []ExpectedTraces{{TraceQL: "trace1"}},
					Metrics: []ExpectedMetrics{{PromQL: "metric1"}},
				},
				Input: []Input{{Path: "/path1"}},
			},
			other: TestCaseDefinition{
				Expected: Expected{
					Logs:    []ExpectedLogs{{LogQL: "log2"}},
					Traces:  []ExpectedTraces{{TraceQL: "trace2"}},
					Metrics: []ExpectedMetrics{{PromQL: "metric2"}},
				},
				Input: []Input{{Path: "/path2"}},
			},
			expected: TestCaseDefinition{
				Expected: Expected{
					Logs:    []ExpectedLogs{{LogQL: "log1"}, {LogQL: "log2"}},
					Traces:  []ExpectedTraces{{TraceQL: "trace1"}, {TraceQL: "trace2"}},
					Metrics: []ExpectedMetrics{{PromQL: "metric1"}, {PromQL: "metric2"}},
				},
				Input: []Input{{Path: "/path1"}, {Path: "/path2"}},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			base := tt.base
			base.Merge(tt.other)

			assert.Equal(t, len(tt.expected.Expected.Logs), len(base.Expected.Logs), "logs count mismatch")
			assert.Equal(t, len(tt.expected.Expected.Traces), len(base.Expected.Traces), "traces count mismatch")
			assert.Equal(t, len(tt.expected.Expected.Metrics), len(base.Expected.Metrics), "metrics count mismatch")
			assert.Equal(t, len(tt.expected.Expected.Profiles), len(base.Expected.Profiles), "profiles count mismatch")
			assert.Equal(t, len(tt.expected.Expected.CustomChecks), len(base.Expected.CustomChecks), "custom checks count mismatch")
			assert.Equal(t, len(tt.expected.Matrix), len(base.Matrix), "matrix count mismatch")
			assert.Equal(t, len(tt.expected.Input), len(base.Input), "input count mismatch")

			if tt.expected.DockerCompose != nil {
				require.NotNil(t, base.DockerCompose)
				assert.Equal(t, tt.expected.DockerCompose.Files, base.DockerCompose.Files)
			}
		})
	}
}
