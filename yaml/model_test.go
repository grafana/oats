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
