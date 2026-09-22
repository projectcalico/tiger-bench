// Copyright (c) 2025 Tigera, Inc. All rights reserved.

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
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSleepCtxSleeps(t *testing.T) {
	start := time.Now()
	require.NoError(t, SleepCtx(context.Background(), 20*time.Millisecond))
	assert.GreaterOrEqual(t, time.Since(start), 20*time.Millisecond)
}

func TestSleepCtxReturnsOnCancellation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := SleepCtx(ctx, time.Hour)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	assert.Less(t, time.Since(start), 5*time.Second)
}

// Pacing loops compute a delay that goes negative once they are behind schedule.
func TestSleepCtxWithNonPositiveDuration(t *testing.T) {
	start := time.Now()
	require.NoError(t, SleepCtx(context.Background(), -1*time.Second))
	assert.Less(t, time.Since(start), time.Second)
}

func TestSleepCtxNonPositiveStillReportsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	assert.ErrorIs(t, SleepCtx(ctx, 0), context.Canceled)
}
