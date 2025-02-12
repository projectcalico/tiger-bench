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

package stats

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestAverage(t *testing.T) {
	tests := []struct {
		name     string
		input    []float64
		expected float64
		err      bool
	}{
		{
			name:     "average of positive numbers",
			input:    []float64{1, 2, 3, 4, 5},
			expected: 3,
			err:      false,
		},
		{
			name:     "average of negative numbers",
			input:    []float64{-1, -2, -3, -4, -5},
			expected: -3,
			err:      false,
		},
		{
			name:     "average of mixed numbers",
			input:    []float64{-1, 2, -3, 4, -5},
			expected: -0.6,
			err:      false,
		},
		{
			name:     "average of empty slice",
			input:    []float64{},
			expected: 0,
			err:      true,
		},
		{
			name:     "average of single element",
			input:    []float64{42},
			expected: 42,
			err:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := average(tt.input)
			if tt.err {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.InDelta(t, tt.expected, result, 0.0001)
			}
		})
	}
}
func TestSummarizeResults(t *testing.T) {
	tests := []struct {
		name     string
		input    []float64
		expected ResultSummary
		err      bool
	}{
		{
			name:  "summarize positive numbers",
			input: []float64{1, 2, 3, 4, 5},
			expected: ResultSummary{
				Min:     1,
				Max:     5,
				Average: 3,
				P50:     3,
				P75:     4,
				P90:     5,
				P99:     5,
			},
			err: false,
		},
		{
			name:  "summarize negative numbers",
			input: []float64{-1, -2, -3, -4, -5},
			expected: ResultSummary{
				Min:     -5,
				Max:     -1,
				Average: -3,
				P50:     -3,
				P75:     -2,
				P90:     -1,
				P99:     -1,
			},
			err: false,
		},
		{
			name:  "summarize mixed numbers",
			input: []float64{-1, 2, -3, 4, -5},
			expected: ResultSummary{
				Min:     -5,
				Max:     4,
				Average: -0.6,
				P50:     -1,
				P75:     2,
				P90:     4,
				P99:     4,
			},
			err: false,
		},
		{
			name:  "summarize empty slice",
			input: []float64{},
			expected: ResultSummary{
				Min:     0,
				Max:     0,
				Average: 0,
				P50:     0,
				P75:     0,
				P90:     0,
				P99:     0,
			},
			err: true,
		},
		{
			name:  "summarize single element",
			input: []float64{42},
			expected: ResultSummary{
				Min:     42,
				Max:     42,
				Average: 42,
				P50:     42,
				P75:     42,
				P90:     42,
				P99:     42,
			},
			err: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, recderr := SummarizeResults(tt.input)
			if tt.err {
				assert.Error(t, recderr)
			} else {
				assert.NoError(t, recderr)
			}
			assert.InDelta(t, tt.expected.Min, result.Min, 0.0001)
			assert.InDelta(t, tt.expected.Max, result.Max, 0.0001)
			assert.InDelta(t, tt.expected.Average, result.Average, 0.0001)
			assert.InDelta(t, tt.expected.P50, result.P50, 0.0001)
			assert.InDelta(t, tt.expected.P75, result.P75, 0.0001)
			assert.InDelta(t, tt.expected.P90, result.P90, 0.0001)
			assert.InDelta(t, tt.expected.P99, result.P99, 0.0001)
		})
	}
}
