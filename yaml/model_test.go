package yaml

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTestCasesAreValid(t *testing.T) {
	cases, err := collectTestCases("testdata", false)
	require.NoError(t, err)
	require.NotEmpty(t, cases)
	for _, c := range cases {
		require.NotEqual(t, nil, c.Definition)
		require.NotEmpty(t, c.Definition.Input)
		validateInput(c.Definition.Input)
	}
}

func TestAllSpansExpectAbsent(t *testing.T) {
	tests := []struct {
		name     string
		traces   ExpectedTraces
		expected bool
	}{
		{
			name: "no spans",
			traces: ExpectedTraces{
				TraceQL: "{.service.name = \"test\"}",
				Spans:   []ExpectedSpan{},
			},
			expected: false,
		},
		{
			name: "only normal spans",
			traces: ExpectedTraces{
				TraceQL: "{.service.name = \"test\"}",
				Spans: []ExpectedSpan{
					{Name: "span1", ExpectAbsent: false},
					{Name: "span2", ExpectAbsent: false},
				},
			},
			expected: false,
		},
		{
			name: "only absent spans",
			traces: ExpectedTraces{
				TraceQL: "{.service.name = \"test\"}",
				Spans: []ExpectedSpan{
					{Name: "span1", ExpectAbsent: true},
					{Name: "span2", ExpectAbsent: true},
				},
			},
			expected: true,
		},
		{
			name: "mixed spans",
			traces: ExpectedTraces{
				TraceQL: "{.service.name = \"test\"}",
				Spans: []ExpectedSpan{
					{Name: "span1", ExpectAbsent: false},
					{Name: "span2", ExpectAbsent: true},
					{Name: "span3", ExpectAbsent: false},
				},
			},
			expected: false,
		},
		{
			name: "single absent span",
			traces: ExpectedTraces{
				TraceQL: "{.service.name = \"test\"}",
				Spans: []ExpectedSpan{
					{Name: "span1", ExpectAbsent: true},
				},
			},
			expected: true,
		},
		{
			name: "single normal span",
			traces: ExpectedTraces{
				TraceQL: "{.service.name = \"test\"}",
				Spans: []ExpectedSpan{
					{Name: "span1", ExpectAbsent: false},
				},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.traces.AllSpansExpectAbsent()
			require.Equal(t, tt.expected, result)
		})
	}
}
