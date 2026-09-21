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

package qperf

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

const qperfStdout = `tcp_bw:
    bw              =  9.71 Gb/sec
    msg_rate        =  18.5 K/sec
tcp_lat:
    latency         =  12.8 us
    msg_rate        =  78.1 K/sec
`

func TestParseQperfOutput(t *testing.T) {
	out := parseQperfOutput(qperfStdout)

	assert.Equal(t, "9.71 Gb/sec", out["tcp_bw:"]["bw"])
	assert.Equal(t, "18.5 K/sec", out["tcp_bw:"]["msg_rate"])
	assert.Equal(t, "12.8 us", out["tcp_lat:"]["latency"])
	assert.Equal(t, "78.1 K/sec", out["tcp_lat:"]["msg_rate"])
}

// The callers index straight into the map, so a shape change must not read across sections.
func TestParseQperfOutputKeepsSectionsSeparate(t *testing.T) {
	out := parseQperfOutput(qperfStdout)

	assert.NotContains(t, out["tcp_bw:"], "latency")
	assert.NotContains(t, out["tcp_lat:"], "bw")
}

func TestParseQperfOutputOnUnexpectedInput(t *testing.T) {
	// Missing keys read as empty rather than panicking, which is what the ParseFloat guard
	// in runQperfTest relies on.
	out := parseQperfOutput("qperf: cannot connect to remote host\n")

	assert.Equal(t, "", out["tcp_lat:"]["latency"])
	assert.Equal(t, "", out["tcp_bw:"]["bw"])
}

func TestParseQperfOutputOnEmptyInput(t *testing.T) {
	out := parseQperfOutput("")

	assert.Equal(t, "", out["tcp_lat:"]["latency"])
}
