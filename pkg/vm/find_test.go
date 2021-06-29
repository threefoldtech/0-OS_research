package vm

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestProcessGetParam(t *testing.T) {
	require := require.New(t)

	ps := Process{
		Pid: 0,
		Args: []string{
			"exe",
			"--multiple", "value1", "value2",
			"--no-input",
			"--one-input", "value3",
		},
	}

	_, ok := ps.GetParam("--not-existing")
	require.False(ok)

	values, ok := ps.GetParam("--multiple")
	require.True(ok)
	require.Len(values, 2)
	require.EqualValues([]string{"value1", "value2"}, values)

	values, ok = ps.GetParam("--one-input")
	require.True(ok)
	require.Len(values, 1)
	require.EqualValues([]string{"value3"}, values)

	values, ok = ps.GetParam("--no-input")
	require.True(ok)
	require.Len(values, 0)

}
