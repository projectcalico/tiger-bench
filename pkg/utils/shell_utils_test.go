package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShelloutSuccess(t *testing.T) {
	stdout, stderr, err := Shellout("echo Hello, World!")
	require.NoError(t, err)
	require.Equal(t, "Hello, World!\n", stdout)
	require.Equal(t, "", stderr)
}

func TestShelloutFailure(t *testing.T) {
	stdout, stderr, err := Shellout("invalidcommand")
	require.Error(t, err)
	require.Equal(t, "", stdout)
	require.Contains(t, stderr, "command not found")
}
