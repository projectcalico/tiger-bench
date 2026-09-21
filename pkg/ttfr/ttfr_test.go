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

package ttfr

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummarizeResultsPerIteration(t *testing.T) {
	summaries, err := SummarizeResults([]*Results{
		{TTFR: []float64{1, 2, 3}},
		{TTFR: []float64{10, 20}},
	})
	require.NoError(t, err)
	require.Len(t, summaries, 2)

	assert.Equal(t, float64(1), summaries[0].TTFRSummary.Min)
	assert.Equal(t, float64(3), summaries[0].TTFRSummary.Max)
	assert.Equal(t, float64(2), summaries[0].TTFRSummary.Average)
	assert.Equal(t, 3, summaries[0].TTFRSummary.NumDataPoints)
	assert.Equal(t, "seconds", summaries[0].TTFRSummary.Unit)

	assert.Equal(t, float64(15), summaries[1].TTFRSummary.Average)
	assert.Equal(t, 2, summaries[1].TTFRSummary.NumDataPoints)
}

func TestSummarizeResultsWithNoIterations(t *testing.T) {
	_, err := SummarizeResults(nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no results to summarize")
}

// An iteration that recorded nothing fails the whole summary rather than reporting a zero.
func TestSummarizeResultsWithEmptyIteration(t *testing.T) {
	_, err := SummarizeResults([]*Results{{TTFR: []float64{1, 2}}, {TTFR: nil}})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no results to summarize")
}
