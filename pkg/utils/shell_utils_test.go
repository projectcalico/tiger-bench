// Copyright (c) 2024-2025 Tigera, Inc. All rights reserved.

// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package utils

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShelloutSuccess(t *testing.T) {
	stdout, stderr, err := Shellout("echo Hello, World!", 1)
	require.NoError(t, err)
	require.Equal(t, "Hello, World!\n", stdout)
	require.Equal(t, "", stderr)
}

func TestShelloutFailure(t *testing.T) {
	stdout, stderr, err := Shellout("invalidcommand", 3)
	require.Error(t, err)
	require.Equal(t, "", stdout)
	require.Contains(t, stderr, "invalidcommand: not found")
}
