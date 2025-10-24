package yaml

import (
	"os"
	"testing"

	"github.com/onsi/gomega"
	"github.com/stretchr/testify/require"
)

func TestAssertTraceResponse(t *testing.T) {
	gomega.RegisterTestingT(t)

	file, err := os.ReadFile("testdata/tempo_trace_response.json")
	require.NoError(t, err)

	spans := []ExpectedSpan{
		{
			Name:         "dropped-span",
			ExpectAbsent: true, // This span does not exist in sample set
		},
		{
			Name: "visible-span",
			Attributes: map[string]string{
				"http.method": "GET",
			},
			ExpectAbsent: false,
		},
	}

	r := &runner{
		gomegaInst: gomega.NewGomega(func(message string, callerSkip ...int) {
			t.Error(message)
		}),
	}

	AssertTraceResponse(file, spans, r)
}

func TestAssertTraceResponseFailsIfExists(t *testing.T) {
	gomega.RegisterTestingT(t)

	file, err := os.ReadFile("testdata/tempo_trace_response.json")
	require.NoError(t, err)

	spans := []ExpectedSpan{
		{
			Name: "visible-span",
			Attributes: map[string]string{
				"http.method": "GET",
			},
			ExpectAbsent: true, // this exists, we expect the assertion to fail
		},
	}

	assertionFailed := false
	r := &runner{
		gomegaInst: gomega.NewGomega(func(message string, callerSkip ...int) {
			assertionFailed = true
		}),
	}

	AssertTraceResponse(file, spans, r)

	require.True(t, assertionFailed, "Expected the assertion to fail because visible-span exists but ExpectAbsent was true")
}
