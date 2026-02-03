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

package dnsperf

import (
	"fmt"
	"reflect"
	testing "testing"

	"github.com/projectcalico/tiger-bench/pkg/stats"
	"github.com/stretchr/testify/require"
	v3 "github.com/tigera/api/pkg/apis/projectcalico/v3"
	"github.com/tigera/api/pkg/lib/numorstring"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestProcessTCPDumpOutput(t *testing.T) {
	// Test case 1: Valid input, no duplicates
	input := `
	   reading from file dump.cap, link-type EN10MB (Ethernet), snapshot length 262144
	   14:37:23.379170 IP 10.128.0.146.32341 > 173.194.195.106.80: Flags [S], seq 3596708413, win 64390, options [mss 1370,sackOK,TS val 254092413 ecr 0,nop,wscale 7], length 0
	   14:37:23.380515 IP 173.194.195.106.80 > 10.128.0.146.32341: Flags [S.], seq 3064651401, ack 3596708414, win 65535, options [mss 1420,sackOK,TS val 277045536 ecr 254092413,nop,wscale 8], length 0
	   14:37:35.080487 IP 10.128.0.146.59524 > 23.52.42.155.80: Flags [S], seq 4198782584, win 64390, options [mss 1370,sackOK,TS val 2180913612 ecr 0,nop,wscale 7], length 0
	   14:37:35.091962 IP 23.52.42.155.80 > 10.128.0.146.59524: Flags [S.], seq 2562236066, ack 4198782585, win 65160, options [mss 1460,sackOK,TS val 3020826720 ecr 2180913612,nop,wscale 7], length 0
	   14:37:48.371104 IP 10.128.0.146.65255 > 69.147.65.251.80: Flags [S], seq 2052195526, win 64390, options [mss 1370,sackOK,TS val 678355302 ecr 0,nop,wscale 7], length 0
	   14:37:48.383808 IP 69.147.65.251.80 > 10.128.0.146.65255: Flags [S.], seq 3179189632, ack 2052195527, win 28960, options [mss 1460,sackOK,TS val 3847925973 ecr 678355302,nop,wscale 13], length 0
	   14:38:00.691097 IP 10.128.0.146.31488 > 40.89.244.232.80: Flags [S], seq 3481780927, win 64390, options [mss 1370,sackOK,TS val 924816907 ecr 0,nop,wscale 7], length 0
	   14:38:00.710978 IP 40.89.244.232.80 > 10.128.0.146.31488: Flags [S.], seq 464898114, ack 3481780928, win 7240, options [mss 1440,sackOK,TS val 1247241486 ecr 924816907,nop,wscale 0], length 0
	   14:38:11.869710 IP 10.128.0.146.23282 > 108.156.127.185.80: Flags [S], seq 1185736244, win 64390, options [mss 1370,sackOK,TS val 3603287483 ecr 0,nop,wscale 7], length 0
	   14:38:11.881347 IP 108.156.127.185.80 > 10.128.0.146.23282: Flags [S.], seq 3369658766, ack 1185736245, win 65535, options [mss 1440,sackOK,TS val 2377397182 ecr 3603287483,nop,wscale 9], length 0
	`
	expectedsyn := 0
	expectedsynack := 0
	resultsyn, resultsynack, err := processTCPDumpOutput(input)
	require.NoError(t, err)
	if resultsyn != expectedsyn {
		panic("Unexpected number of duplicate SYN packets")
	}
	if resultsynack != expectedsynack {
		panic("Unexpected number of duplicate SYNACK packets")
	}

	// Test case 2: Valid input, 2 duplicates
	input = `
	   reading from file dump.cap, link-type EN10MB (Ethernet), snapshot length 262144
	   14:37:23.379170 IP 10.128.0.146.32341 > 173.194.195.106.80: Flags [S], seq 3596708413, win 64390, options [mss 1370,sackOK,TS val 254092413 ecr 0,nop,wscale 7], length 0
	   14:37:23.379175 IP 10.128.0.146.32341 > 173.194.195.106.80: Flags [S], seq 3596708413, win 64390, options [mss 1370,sackOK,TS val 254092413 ecr 0,nop,wscale 7], length 0
	   14:37:23.380515 IP 173.194.195.106.80 > 10.128.0.146.32341: Flags [S.], seq 3064651401, ack 3596708414, win 65535, options [mss 1420,sackOK,TS val 277045536 ecr 254092413,nop,wscale 8], length 0
	   14:37:35.080487 IP 10.128.0.146.59524 > 23.52.42.155.80: Flags [S], seq 4198782584, win 64390, options [mss 1370,sackOK,TS val 2180913612 ecr 0,nop,wscale 7], length 0
	   14:37:35.091962 IP 23.52.42.155.80 > 10.128.0.146.59524: Flags [S.], seq 2562236066, ack 4198782585, win 65160, options [mss 1460,sackOK,TS val 3020826720 ecr 2180913612,nop,wscale 7], length 0
	   14:37:48.371104 IP 10.128.0.146.65255 > 69.147.65.251.80: Flags [S], seq 2052195526, win 64390, options [mss 1370,sackOK,TS val 678355302 ecr 0,nop,wscale 7], length 0
	   14:37:48.383808 IP 69.147.65.251.80 > 10.128.0.146.65255: Flags [S.], seq 3179189632, ack 2052195527, win 28960, options [mss 1460,sackOK,TS val 3847925973 ecr 678355302,nop,wscale 13], length 0
	   14:38:00.691097 IP 10.128.0.146.31488 > 40.89.244.232.80: Flags [S], seq 3481780927, win 64390, options [mss 1370,sackOK,TS val 924816907 ecr 0,nop,wscale 7], length 0
	   14:38:00.691197 IP 10.128.0.146.31488 > 40.89.244.232.80: Flags [S], seq 3481780927, win 64390, options [mss 1370,sackOK,TS val 924816907 ecr 0,nop,wscale 7], length 0
	   14:38:00.710978 IP 40.89.244.232.80 > 10.128.0.146.31488: Flags [S.], seq 464898114, ack 3481780928, win 7240, options [mss 1440,sackOK,TS val 1247241486 ecr 924816907,nop,wscale 0], length 0
	   14:38:11.869710 IP 10.128.0.146.23282 > 108.156.127.185.80: Flags [S], seq 1185736244, win 64390, options [mss 1370,sackOK,TS val 3603287483 ecr 0,nop,wscale 7], length 0
	   14:38:11.881347 IP 108.156.127.185.80 > 10.128.0.146.23282: Flags [S.], seq 3369658766, ack 1185736245, win 65535, options [mss 1440,sackOK,TS val 2377397182 ecr 3603287483,nop,wscale 9], length 0
	`
	expectedsyn = 2
	expectedsynack = 0
	resultsyn, resultsynack, err = processTCPDumpOutput(input)
	require.NoError(t, err)
	if resultsyn != expectedsyn {
		panic("Unexpected number of duplicate SYN packets")
	}
	if resultsynack != expectedsynack {
		panic("Unexpected number of duplicate SYNACK packets")
	}

	// Test case 2: Empty input
	input = ""
	expectedsyn = 0
	expectedsynack = 0
	resultsyn, resultsynack, err = processTCPDumpOutput(input)
	require.NoError(t, err)
	if resultsyn != expectedsyn {
		panic("Unexpected number of duplicate SYN packets")
	}
	if resultsynack != expectedsynack {
		panic("Unexpected number of duplicate SYNACK packets")
	}

	// Test case 3: Invalid input
	input = "Invalid input"
	expectedsyn = 0
	expectedsynack = 0
	resultsyn, resultsynack, err = processTCPDumpOutput(input)
	require.NoError(t, err)
	if resultsyn != expectedsyn {
		panic("Unexpected number of duplicate SYN packets")
	}
	if resultsynack != expectedsynack {
		panic("Unexpected number of duplicate SYNACK packets")
	}

	input = `
reading from file dump.cap, link-type EN10MB (Ethernet), snapshot length 262144
11:41:13.942820 IP 196.217.178.241.54478 > 10.128.0.22.80: Flags [S], seq 4036182372, win 64240, options [mss 1452,nop,wscale 8,nop,nop,sackOK], length 0
11:41:14.759117 IP 196.217.178.241.54478 > 10.128.0.22.80: Flags [S], seq 4036182372, win 64240, options [mss 1452,nop,wscale 8,nop,nop,sackOK], length 0
11:42:23.380343 IP 143.198.87.112.56531 > 10.128.0.22.80: Flags [S], seq 1781524233, win 1025, options [mss 1460], length 0
11:42:48.702862 IP 10.128.0.22.57837 > 151.101.195.52.80: Flags [S], seq 3626693963, win 64390, options [mss 1370,sackOK,TS val 1723262889 ecr 0,nop,wscale 7], length 0
11:42:48.716104 IP 151.101.195.52.80 > 10.128.0.22.57837: Flags [S.], seq 3252147713, ack 3626693964, win 65535, options [mss 1460,sackOK,TS val 2498440407 ecr 1723262889,nop,wscale 9], length 0
11:42:49.680904 IP 10.128.0.22.7513 > 151.101.1.55.80: Flags [S], seq 3507394897, win 64390, options [mss 1370,sackOK,TS val 638132362 ecr 0,nop,wscale 7], length 0
11:42:49.691800 IP 151.101.1.55.80 > 10.128.0.22.7513: Flags [S.], seq 635899245, ack 3507394898, win 65535, options [mss 1460,sackOK,TS val 1253690585 ecr 638132362,nop,wscale 9], length 0
11:42:50.234831 IP 10.128.0.22.49277 > 23.203.41.94.80: Flags [S], seq 1186860793, win 64390, options [mss 1370,sackOK,TS val 1503262126 ecr 0,nop,wscale 7], length 0
11:42:50.247269 IP 23.203.41.94.80 > 10.128.0.22.49277: Flags [S.], seq 624203251, ack 1186860794, win 65160, options [mss 1460,sackOK,TS val 2045541474 ecr 1503262126,nop,wscale 7], length 0
11:42:50.677733 IP 10.128.0.22.9403 > 78.46.152.34.80: Flags [S], seq 632924936, win 64390, options [mss 1370,sackOK,TS val 4211946349 ecr 0,nop,wscale 7], length 0
11:42:50.793201 IP 78.46.152.34.80 > 10.128.0.22.9403: Flags [S.], seq 333063793, ack 632924937, win 64240, options [mss 1460,nop,nop,sackOK,nop,wscale 7], length 0
11:42:52.176611 IP 10.128.0.22.19477 > 18.160.225.69.80: Flags [S], seq 1659726236, win 64390, options [mss 1370,sackOK,TS val 2111353744 ecr 0,nop,wscale 7], length 0
11:42:52.190120 IP 18.160.225.69.80 > 10.128.0.22.19477: Flags [S.], seq 3843458644, ack 1659726237, win 65535, options [mss 1440,sackOK,TS val 3463925541 ecr 2111353744,nop,wscale 9], length 0
11:42:53.646208 IP 10.128.0.22.62826 > 151.101.66.133.80: Flags [S], seq 1028235414, win 64390, options [mss 1370,sackOK,TS val 621475182 ecr 0,nop,wscale 7], length 0
11:42:53.656582 IP 151.101.66.133.80 > 10.128.0.22.62826: Flags [S.], seq 1839462556, ack 1028235415, win 65535, options [mss 1460,sackOK,TS val 2324812128 ecr 621475182,nop,wscale 9], length 0
11:42:56.781051 IP 10.128.0.22.19175 > 98.137.27.7.80: Flags [S], seq 2177179698, win 64390, options [mss 1370,sackOK,TS val 947783941 ecr 0,nop,wscale 7], length 0
11:42:56.836528 IP 98.137.27.7.80 > 10.128.0.22.19175: Flags [S.], seq 3933502144, ack 2177179699, win 28960, options [mss 1460,sackOK,TS val 883248384 ecr 947783941,nop,wscale 7], length 0
11:42:58.362868 IP 10.128.0.22.5072 > 199.232.194.154.80: Flags [S], seq 569239281, win 64390, options [mss 1370,sackOK,TS val 1793408701 ecr 0,nop,wscale 7], length 0
11:42:58.376072 IP 199.232.194.154.80 > 10.128.0.22.5072: Flags [S.], seq 4226520763, ack 569239282, win 65535, options [mss 1460,sackOK,TS val 1260628829 ecr 1793408701,nop,wscale 9], length 0
11:42:58.547049 IP 10.128.0.22.39954 > 151.101.2.114.80: Flags [S], seq 151758019, win 64390, options [mss 1370,sackOK,TS val 1936532937 ecr 0,nop,wscale 7], length 0
11:42:58.557265 IP 151.101.2.114.80 > 10.128.0.22.39954: Flags [S.], seq 382373960, ack 151758020, win 65535, options [mss 1460,sackOK,TS val 691010519 ecr 1936532937,nop,wscale 9], length 0
11:43:01.182325 IP 10.128.0.22.20076 > 151.101.66.187.80: Flags [S], seq 1106428168, win 64390, options [mss 1370,sackOK,TS val 736287773 ecr 0,nop,wscale 7], length 0
11:43:01.192712 IP 151.101.66.187.80 > 10.128.0.22.20076: Flags [S.], seq 3494241240, ack 1106428169, win 65535, options [mss 1460,sackOK,TS val 1514627479 ecr 736287773,nop,wscale 9], length 0
11:43:05.843068 IP 10.128.0.22.38504 > 23.66.127.152.80: Flags [S], seq 3957937804, win 64390, options [mss 1370,sackOK,TS val 1226774101 ecr 0,nop,wscale 7], length 0
11:43:05.855485 IP 23.66.127.152.80 > 10.128.0.22.38504: Flags [S.], seq 3958728816, ack 3957937805, win 65160, options [mss 1460,sackOK,TS val 1622953621 ecr 1226774101,nop,wscale 7], length 0
11:43:05.892871 IP 10.128.0.22.59396 > 69.147.65.251.80: Flags [S], seq 602386872, win 64390, options [mss 1370,sackOK,TS val 3769857884 ecr 0,nop,wscale 7], length 0
11:43:05.905696 IP 69.147.65.251.80 > 10.128.0.22.59396: Flags [S.], seq 608302250, ack 602386873, win 28960, options [mss 1460,sackOK,TS val 3263577201 ecr 3769857884,nop,wscale 13], length 0
11:43:14.702060 IP 10.128.0.22.44437 > 151.101.195.52.80: Flags [S], seq 2029649516, win 64390, options [mss 1370,sackOK,TS val 1723288888 ecr 0,nop,wscale 7], length 0
11:43:14.714701 IP 151.101.195.52.80 > 10.128.0.22.44437: Flags [S.], seq 546376759, ack 2029649517, win 65535, options [mss 1460,sackOK,TS val 297315516 ecr 1723288888,nop,wscale 9], length 0
11:43:16.234088 IP 10.128.0.22.60602 > 23.203.41.94.80: Flags [S], seq 1697555601, win 64390, options [mss 1370,sackOK,TS val 1503288125 ecr 0,nop,wscale 7], length 0
11:43:16.246406 IP 23.203.41.94.80 > 10.128.0.22.60602: Flags [S.], seq 2300202215, ack 1697555602, win 65160, options [mss 1460,sackOK,TS val 2593006053 ecr 1503288125,nop,wscale 7], length 0
11:43:20.687906 IP 10.128.0.22.15065 > 151.101.194.133.80: Flags [S], seq 548810804, win 64390, options [mss 1370,sackOK,TS val 1769912564 ecr 0,nop,wscale 7], length 0
11:43:20.698521 IP 151.101.194.133.80 > 10.128.0.22.15065: Flags [S.], seq 3749398670, ack 548810805, win 65535, options [mss 1460,sackOK,TS val 3336984784 ecr 1769912564,nop,wscale 9], length 0
11:43:22.416371 IP 10.128.0.22.62958 > 199.232.198.154.80: Flags [S], seq 1933764786, win 64390, options [mss 1370,sackOK,TS val 2671087335 ecr 0,nop,wscale 7], length 0
11:43:22.428453 IP 199.232.198.154.80 > 10.128.0.22.62958: Flags [S.], seq 1138886851, ack 1933764787, win 65535, options [mss 1460,sackOK,TS val 3655503108 ecr 2671087335,nop,wscale 9], length 0
11:43:23.701140 IP 10.128.0.22.40126 > 98.137.27.7.80: Flags [S], seq 2038897137, win 64390, options [mss 1370,sackOK,TS val 947810861 ecr 0,nop,wscale 7], length 0
11:43:23.757268 IP 98.137.27.7.80 > 10.128.0.22.40126: Flags [S.], seq 1490468389, ack 2038897138, win 28960, options [mss 1460,sackOK,TS val 896587499 ecr 947810861,nop,wscale 7], length 0
11:43:25.240382 IP 10.128.0.22.4967 > 151.101.130.114.80: Flags [S], seq 2175720625, win 64390, options [mss 1370,sackOK,TS val 2176065735 ecr 0,nop,wscale 7], length 0
11:43:25.250658 IP 151.101.130.114.80 > 10.128.0.22.4967: Flags [S.], seq 3734999677, ack 2175720626, win 65535, options [mss 1460,sackOK,TS val 482520881 ecr 2176065735,nop,wscale 9], length 0
11:43:27.738969 IP 10.128.0.22.26658 > 151.101.65.91.80: Flags [S], seq 596992891, win 64390, options [mss 1370,sackOK,TS val 2038058186 ecr 0,nop,wscale 7], length 0
11:43:27.749219 IP 151.101.65.91.80 > 10.128.0.22.26658: Flags [S.], seq 3723949804, ack 596992892, win 65535, options [mss 1460,sackOK,TS val 409504363 ecr 2038058186,nop,wscale 9], length 0
11:43:28.387930 IP 10.128.0.22.2108 > 151.101.194.187.80: Flags [S], seq 649977275, win 64390, options [mss 1370,sackOK,TS val 1594540040 ecr 0,nop,wscale 7], length 0
11:43:28.398812 IP 151.101.194.187.80 > 10.128.0.22.2108: Flags [S.], seq 409831421, ack 649977276, win 65535, options [mss 1460,sackOK,TS val 4207388797 ecr 1594540040,nop,wscale 9], length 0
11:43:28.909226 IP 10.128.0.22.44858 > 169.254.169.254.80: Flags [S], seq 3442020643, win 65320, options [mss 1420,sackOK,TS val 3256745690 ecr 0,nop,wscale 7], length 0
11:43:28.909485 IP 169.254.169.254.80 > 10.128.0.22.44858: Flags [S.], seq 4249204480, ack 3442020644, win 65535, options [mss 1420,eol], length 0
11:43:32.878639 IP 10.128.0.22.4569 > 69.147.65.252.80: Flags [S], seq 2649503375, win 64390, options [mss 1370,sackOK,TS val 2041561617 ecr 0,nop,wscale 7], length 0
11:43:32.890490 IP 69.147.65.252.80 > 10.128.0.22.4569: Flags [S.], seq 837570892, ack 2649503376, win 28960, options [mss 1460,sackOK,TS val 3268277594 ecr 2041561617,nop,wscale 13], length 0
11:43:33.647588 IP 10.128.0.22.46031 > 74.125.201.103.80: Flags [S], seq 2213701291, win 64390, options [mss 1370,sackOK,TS val 1529915764 ecr 0,nop,wscale 7], length 0
11:43:33.648975 IP 74.125.201.103.80 > 10.128.0.22.46031: Flags [S.], seq 4089587581, ack 2213701292, win 65535, options [mss 1420,sackOK,TS val 911218743 ecr 1529915764,nop,wscale 8], length 0
11:43:33.769939 IP 10.128.0.22.14700 > 74.125.201.147.80: Flags [S], seq 1931862711, win 64390, options [mss 1370,sackOK,TS val 1565134974 ecr 0,nop,wscale 7], length 0
11:43:33.771208 IP 74.125.201.147.80 > 10.128.0.22.14700: Flags [S.], seq 814894850, ack 1931862712, win 65535, options [mss 1420,sackOK,TS val 2072085591 ecr 1565134974,nop,wscale 8], length 0
11:43:34.244133 IP 10.128.0.22.5877 > 23.66.127.149.80: Flags [S], seq 1528840969, win 64390, options [mss 1370,sackOK,TS val 980718341 ecr 0,nop,wscale 7], length 0
11:43:34.256529 IP 23.66.127.149.80 > 10.128.0.22.5877: Flags [S.], seq 2107320225, ack 1528840970, win 65160, options [mss 1460,sackOK,TS val 3805931379 ecr 980718341,nop,wscale 7], length 0
11:43:34.740992 IP 10.128.0.22.63016 > 23.66.127.150.80: Flags [S], seq 3413509553, win 64390, options [mss 1370,sackOK,TS val 3801231735 ecr 0,nop,wscale 7], length 0
11:43:34.752087 IP 23.66.127.150.80 > 10.128.0.22.63016: Flags [S.], seq 1337008465, ack 3413509554, win 65160, options [mss 1460,sackOK,TS val 2010997278 ecr 3801231735,nop,wscale 7], length 0
11:43:34.919363 IP 10.128.0.22.63407 > 40.89.244.232.80: Flags [S], seq 4038606340, win 64390, options [mss 1370,sackOK,TS val 2040040551 ecr 0,nop,wscale 7], length 0
11:43:34.940272 IP 40.89.244.232.80 > 10.128.0.22.63407: Flags [S.], seq 1604591821, ack 4038606341, win 7240, options [mss 1440,sackOK,TS val 1228419111 ecr 2040040551,nop,wscale 0], length 0
11:43:34.987576 IP 10.128.0.22.36991 > 23.66.127.144.80: Flags [S], seq 1504923640, win 64390, options [mss 1370,sackOK,TS val 4011342736 ecr 0,nop,wscale 7], length 0
11:43:34.997903 IP 23.66.127.144.80 > 10.128.0.22.36991: Flags [S.], seq 3417578038, ack 1504923641, win 65160, options [mss 1460,sackOK,TS val 1319627288 ecr 4011342736,nop,wscale 7], length 0
11:43:35.111166 IP 10.128.0.22.29803 > 23.66.127.148.80: Flags [S], seq 603153337, win 64390, options [mss 1370,sackOK,TS val 3263574933 ecr 0,nop,wscale 7], length 0
11:43:35.121359 IP 23.66.127.148.80 > 10.128.0.22.29803: Flags [S.], seq 4166255960, ack 603153338, win 65160, options [mss 1460,sackOK,TS val 2302021333 ecr 3263574933,nop,wscale 7], length 0
11:43:35.173932 IP 10.128.0.22.49299 > 23.66.127.145.80: Flags [S], seq 3387000293, win 64390, options [mss 1370,sackOK,TS val 1213068232 ecr 0,nop,wscale 7], length 0
11:43:35.184859 IP 23.66.127.145.80 > 10.128.0.22.49299: Flags [S.], seq 1600296763, ack 3387000294, win 65160, options [mss 1460,sackOK,TS val 2926009861 ecr 1213068232,nop,wscale 7], length 0
11:43:35.204618 IP 10.128.0.22.41351 > 23.66.127.151.80: Flags [S], seq 295544965, win 64390, options [mss 1370,sackOK,TS val 3719955926 ecr 0,nop,wscale 7], length 0
11:43:35.215039 IP 23.66.127.151.80 > 10.128.0.22.41351: Flags [S.], seq 2188966382, ack 295544966, win 65160, options [mss 1460,sackOK,TS val 1849460510 ecr 3719955926,nop,wscale 7], length 0
11:43:35.220161 IP 10.128.0.22.46382 > 23.66.127.147.80: Flags [S], seq 2412821990, win 64390, options [mss 1370,sackOK,TS val 2656986948 ecr 0,nop,wscale 7], length 0
11:43:35.230990 IP 23.66.127.147.80 > 10.128.0.22.46382: Flags [S.], seq 2873931244, ack 2412821991, win 65160, options [mss 1460,sackOK,TS val 1261095796 ecr 2656986948,nop,wscale 7], length 0
11:43:35.772192 IP 10.128.0.22.43910 > 3.167.165.162.80: Flags [S], seq 4055567412, win 64390, options [mss 1370,sackOK,TS val 1186836224 ecr 0,nop,wscale 7], length 0
11:43:35.784320 IP 3.167.165.162.80 > 10.128.0.22.43910: Flags [S.], seq 2118553797, ack 4055567413, win 65535, options [mss 1440,sackOK,TS val 588601280 ecr 1186836224,nop,wscale 9], length 0
11:43:38.266846 IP 10.128.0.22.53285 > 104.88.206.87.80: Flags [S], seq 2624804934, win 64390, options [mss 1370,sackOK,TS val 2130669782 ecr 0,nop,wscale 7], length 0
11:43:38.280029 IP 104.88.206.87.80 > 10.128.0.22.53285: Flags [S.], seq 3972830284, ack 2624804935, win 65160, options [mss 1460,sackOK,TS val 2945658346 ecr 2130669782,nop,wscale 7], length 0
11:43:38.945682 IP 10.128.0.22.34211 > 162.159.153.4.80: Flags [S], seq 2207324960, win 64390, options [mss 1370,sackOK,TS val 3657014106 ecr 0,nop,wscale 7], length 0
11:43:38.959259 IP 162.159.153.4.80 > 10.128.0.22.34211: Flags [S.], seq 2233584525, ack 2207324961, win 65535, options [mss 1400,sackOK,TS val 3965064634 ecr 3657014106,nop,wscale 13], length 0
11:43:40.435098 IP 10.128.0.22.56054 > 151.101.65.55.80: Flags [S], seq 2244485109, win 64390, options [mss 1370,sackOK,TS val 144339639 ecr 0,nop,wscale 7], length 0
11:43:40.447791 IP 151.101.65.55.80 > 10.128.0.22.56054: Flags [S.], seq 3159209447, ack 2244485110, win 65535, options [mss 1460,sackOK,TS val 971197673 ecr 144339639,nop,wscale 9], length 0
11:43:43.436275 IP 10.128.0.22.14763 > 23.203.41.94.80: Flags [S], seq 1768646439, win 64390, options [mss 1370,sackOK,TS val 1503315327 ecr 0,nop,wscale 7], length 0
11:43:43.448757 IP 23.203.41.94.80 > 10.128.0.22.14763: Flags [S.], seq 2304888401, ack 1768646440, win 65160, options [mss 1460,sackOK,TS val 2593033255 ecr 1503315327,nop,wscale 7], length 0
11:43:43.459333 IP 23.203.41.94.80 > 10.128.0.22.14763: Flags [S.], seq 2304888401, ack 1768646440, win 65160, options [mss 1460,sackOK,TS val 2593033266 ecr 1503315327,nop,wscale 7], length 0
11:43:44.762972 IP 10.128.0.22.13930 > 78.46.152.34.80: Flags [S], seq 964435538, win 64390, options [mss 1370,sackOK,TS val 4212000435 ecr 0,nop,wscale 7], length 0
11:43:44.879071 IP 78.46.152.34.80 > 10.128.0.22.13930: Flags [S.], seq 325449789, ack 964435539, win 64240, options [mss 1460,nop,nop,sackOK,nop,wscale 7], length 0
11:43:46.212239 IP 10.128.0.22.41528 > 18.238.176.4.80: Flags [S], seq 984969598, win 64390, options [mss 1370,sackOK,TS val 2472418706 ecr 0,nop,wscale 7], length 0
11:43:46.225496 IP 18.238.176.4.80 > 10.128.0.22.41528: Flags [S.], seq 375382471, ack 984969599, win 65535, options [mss 1440,sackOK,TS val 3979778723 ecr 2472418706,nop,wscale 9], length 0
11:43:49.432041 IP 10.128.0.22.58907 > 199.232.194.154.80: Flags [S], seq 2504072383, win 64390, options [mss 1370,sackOK,TS val 1793459770 ecr 0,nop,wscale 7], length 0
11:43:49.444870 IP 199.232.194.154.80 > 10.128.0.22.58907: Flags [S.], seq 2839520545, ack 2504072384, win 65535, options [mss 1460,sackOK,TS val 3884099637 ecr 1793459770,nop,wscale 9], length 0
11:43:50.927190 IP 10.128.0.22.2530 > 98.137.27.7.80: Flags [S], seq 218764002, win 64390, options [mss 1370,sackOK,TS val 947838087 ecr 0,nop,wscale 7], length 0
11:43:50.980699 IP 98.137.27.7.80 > 10.128.0.22.2530: Flags [S.], seq 792715430, ack 218764003, win 28960, options [mss 1460,sackOK,TS val 878331696 ecr 947838087,nop,wscale 7], length 0
11:43:52.229493 IP 10.128.0.22.52041 > 151.101.194.114.80: Flags [S], seq 2157436379, win 64390, options [mss 1370,sackOK,TS val 1190396621 ecr 0,nop,wscale 7], length 0
11:43:52.241707 IP 151.101.194.114.80 > 10.128.0.22.52041: Flags [S.], seq 117375665, ack 2157436380, win 65535, options [mss 1460,sackOK,TS val 578616183 ecr 1190396621,nop,wscale 9], length 0
11:43:55.601018 IP 10.128.0.22.8424 > 151.101.194.187.80: Flags [S], seq 3821756616, win 64390, options [mss 1370,sackOK,TS val 1594567253 ecr 0,nop,wscale 7], length 0
11:43:55.612038 IP 151.101.194.187.80 > 10.128.0.22.8424: Flags [S.], seq 1819710255, ack 3821756617, win 65535, options [mss 1460,sackOK,TS val 204201694 ecr 1594567253,nop,wscale 9], length 0
11:43:56.747355 IP 10.128.0.22.13878 > 74.125.201.103.80: Flags [S], seq 811431121, win 64390, options [mss 1370,sackOK,TS val 1529938864 ecr 0,nop,wscale 7], length 0
11:43:56.748680 IP 74.125.201.103.80 > 10.128.0.22.13878: Flags [S.], seq 3339022698, ack 811431122, win 65535, options [mss 1420,sackOK,TS val 3858003184 ecr 1529938864,nop,wscale 8], length 0
11:44:00.009562 IP 10.128.0.22.26988 > 69.147.65.252.80: Flags [S], seq 2434746002, win 64390, options [mss 1370,sackOK,TS val 2041588748 ecr 0,nop,wscale 7], length 0
11:44:00.021765 IP 69.147.65.252.80 > 10.128.0.22.26988: Flags [S.], seq 1288366144, ack 2434746003, win 28960, options [mss 1460,sackOK,TS val 167766216 ecr 2041588748,nop,wscale 13], length 0
11:44:01.514783 IP 10.128.0.22.17621 > 40.89.244.232.80: Flags [S], seq 1194488308, win 64390, options [mss 1370,sackOK,TS val 2040067146 ecr 0,nop,wscale 7], length 0
11:44:01.538895 IP 40.89.244.232.80 > 10.128.0.22.17621: Flags [S.], seq 2744964825, ack 1194488309, win 7240, options [mss 1440,sackOK,TS val 1564017461 ecr 2040067146,nop,wscale 0], length 0
11:44:05.803641 IP 10.128.0.22.64360 > 162.159.152.4.80: Flags [S], seq 3788934059, win 64390, options [mss 1370,sackOK,TS val 3939455759 ecr 0,nop,wscale 7], length 0
11:44:05.817166 IP 162.159.152.4.80 > 10.128.0.22.64360: Flags [S.], seq 1252157225, ack 3788934060, win 65535, options [mss 1400,sackOK,TS val 4263404188 ecr 3939455759,nop,wscale 13], length 0
11:44:07.275576 IP 10.128.0.22.50515 > 151.101.193.55.80: Flags [S], seq 247127646, win 64390, options [mss 1370,sackOK,TS val 4090476931 ecr 0,nop,wscale 7], length 0
11:44:07.287427 IP 151.101.193.55.80 > 10.128.0.22.50515: Flags [S.], seq 3194651310, ack 247127647, win 65535, options [mss 1460,sackOK,TS val 4087585611 ecr 4090476931,nop,wscale 9], length 0
11:44:07.870923 IP 10.128.0.22.17134 > 23.33.22.150.80: Flags [S], seq 3168267905, win 64390, options [mss 1370,sackOK,TS val 3009492977 ecr 0,nop,wscale 7], length 0
11:44:07.883410 IP 23.33.22.150.80 > 10.128.0.22.17134: Flags [S.], seq 1036416259, ack 3168267906, win 65160, options [mss 1460,sackOK,TS val 4263007337 ecr 3009492977,nop,wscale 7], length 0
11:44:08.117485 IP 10.128.0.22.25415 > 23.33.22.154.80: Flags [S], seq 3659835182, win 64390, options [mss 1370,sackOK,TS val 1711278240 ecr 0,nop,wscale 7], length 0
11:44:08.127786 IP 23.33.22.154.80 > 10.128.0.22.25415: Flags [S.], seq 604211212, ack 3659835183, win 65160, options [mss 1460,sackOK,TS val 2324473048 ecr 1711278240,nop,wscale 7], length 0
11:44:08.241101 IP 10.128.0.22.7439 > 23.33.22.145.80: Flags [S], seq 1468856288, win 64390, options [mss 1370,sackOK,TS val 3488083494 ecr 0,nop,wscale 7], length 0
11:44:08.252117 IP 23.33.22.145.80 > 10.128.0.22.7439: Flags [S.], seq 2881864843, ack 1468856289, win 65160, options [mss 1460,sackOK,TS val 3184194593 ecr 3488083494,nop,wscale 7], length 0
11:44:08.302676 IP 10.128.0.22.5579 > 23.33.22.143.80: Flags [S], seq 4013481743, win 64390, options [mss 1370,sackOK,TS val 2096100284 ecr 0,nop,wscale 7], length 0
11:44:08.313738 IP 23.33.22.143.80 > 10.128.0.22.5579: Flags [S.], seq 1553420802, ack 4013481744, win 65160, options [mss 1460,sackOK,TS val 3490687748 ecr 2096100284,nop,wscale 7], length 0
11:44:08.333200 IP 10.128.0.22.42100 > 23.33.22.151.80: Flags [S], seq 203839216, win 64390, options [mss 1370,sackOK,TS val 3497283225 ecr 0,nop,wscale 7], length 0
11:44:08.343456 IP 23.33.22.151.80 > 10.128.0.22.42100: Flags [S.], seq 4084526676, ack 203839217, win 65160, options [mss 1460,sackOK,TS val 2009103704 ecr 3497283225,nop,wscale 7], length 0
11:44:08.348562 IP 10.128.0.22.27652 > 23.33.22.149.80: Flags [S], seq 3961955205, win 64390, options [mss 1370,sackOK,TS val 3700003794 ecr 0,nop,wscale 7], length 0
11:44:08.358800 IP 23.33.22.149.80 > 10.128.0.22.27652: Flags [S.], seq 3141837192, ack 3961955206, win 65160, options [mss 1460,sackOK,TS val 3579485439 ecr 3700003794,nop,wscale 7], length 0
11:44:08.782530 IP 10.128.0.22.29297 > 151.101.195.52.80: Flags [S], seq 3747408810, win 64390, options [mss 1370,sackOK,TS val 1723342968 ecr 0,nop,wscale 7], length 0
11:44:08.792938 IP 151.101.195.52.80 > 10.128.0.22.29297: Flags [S.], seq 165104154, ack 3747408811, win 65535, options [mss 1460,sackOK,TS val 990255126 ecr 1723342968,nop,wscale 9], length 0
11:44:13.371017 IP 10.128.0.22.29229 > 78.46.152.34.80: Flags [S], seq 1171361313, win 64390, options [mss 1370,sackOK,TS val 4212029043 ecr 0,nop,wscale 7], length 0
11:44:13.486257 IP 78.46.152.34.80 > 10.128.0.22.29229: Flags [S.], seq 1568346580, ack 1171361314, win 64240, options [mss 1460,nop,nop,sackOK,nop,wscale 7], length 0
11:44:13.530913 IP 10.128.0.22.1366 > 23.203.41.94.80: Flags [S], seq 2827531377, win 64390, options [mss 1370,sackOK,TS val 1503345422 ecr 0,nop,wscale 7], length 0
11:44:13.543115 IP 23.203.41.94.80 > 10.128.0.22.1366: Flags [S.], seq 2285304985, ack 2827531378, win 65160, options [mss 1460,sackOK,TS val 2593063350 ecr 1503345422,nop,wscale 7], length 0
11:44:14.299032 IP 10.128.0.22.49377 > 18.160.225.37.80: Flags [S], seq 2289962867, win 64390, options [mss 1370,sackOK,TS val 2055327857 ecr 0,nop,wscale 7], length 0
11:44:14.312473 IP 18.160.225.37.80 > 10.128.0.22.49377: Flags [S.], seq 1557542362, ack 2289962868, win 65535, options [mss 1440,sackOK,TS val 2528977775 ecr 2055327857,nop,wscale 9], length 0
`
	expectedsyn = 1
	expectedsynack = 1
	resultsyn, resultsynack, err = processTCPDumpOutput(input)
	require.NoError(t, err)
	if resultsyn != expectedsyn {
		panic("Unexpected number of duplicate SYN packets")
	}
	if resultsynack != expectedsynack {
		panic("Unexpected number of duplicate SYNACK packets")
	}

}

func TestProcessResults(t *testing.T) {
	// Test case 1: Empty input
	var rawResults []CurlResult
	expected := Results{
		LookupTime:      stats.ResultSummary{},
		ConnectTime:     stats.ResultSummary{},
		DuplicateSYN:    0,
		FailedCurls:     0,
		SuccessfulCurls: 0,
	}

	result := processResults(rawResults)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Test case 1 failed: Expected %+v, but got %+v", expected, result)
	}

	// Test case 2: Non-empty input
	rawResults = []CurlResult{
		{
			Target:      "www.google.com",
			LookupTime:  0.123,
			ConnectTime: 0.456,
			Success:     true,
		},
		{
			Target:      "www.bing.com",
			LookupTime:  0.234,
			ConnectTime: 0.567,
			Success:     true,
		},
		{
			Target:      "www.yahoo.com",
			LookupTime:  0.345,
			ConnectTime: 0.678,
			Success:     true,
		},
	}
	expected = Results{
		LookupTime: stats.ResultSummary{
			Min:           0.123,
			Max:           0.345,
			Average:       0.23399999999999999,
			P50:           0.234,
			P75:           0.345,
			P90:           0.345,
			P99:           0.345,
			NumDataPoints: 3,
		},
		ConnectTime: stats.ResultSummary{
			Min:           0.456,
			Max:           0.678,
			Average:       0.5670000000000001,
			P50:           0.567,
			P75:           0.678,
			P90:           0.678,
			P99:           0.678,
			NumDataPoints: 3,
		},
		DuplicateSYN:    0,
		DuplicateSYNACK: 0,
		FailedCurls:     0,
		SuccessfulCurls: 3,
	}

	result = processResults(rawResults)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Test case 2 failed: Expected %v, but got %v", expected, result)
	}

	// Test case 3: Non-empty input, some fails
	rawResults = []CurlResult{
		{
			Target:      "www.google.com",
			LookupTime:  0.123,
			ConnectTime: 0.456,
			Success:     false,
		},
		{
			Target:      "www.bing.com",
			LookupTime:  0.234,
			ConnectTime: 0.567,
			Success:     true,
		},
		{
			Target:      "www.yahoo.com",
			LookupTime:  0.345,
			ConnectTime: 0.678,
			Success:     false,
		},
	}
	expected = Results{
		LookupTime: stats.ResultSummary{
			Min:           0.234,
			Max:           0.234,
			Average:       0.234,
			P50:           0.234,
			P75:           0.234,
			P90:           0.234,
			P99:           0.234,
			NumDataPoints: 1,
		},
		ConnectTime: stats.ResultSummary{
			Min:           0.567,
			Max:           0.567,
			Average:       0.567,
			P50:           0.567,
			P75:           0.567,
			P90:           0.567,
			P99:           0.567,
			NumDataPoints: 1,
		},
		DuplicateSYN:    0,
		DuplicateSYNACK: 0,
		FailedCurls:     2,
		SuccessfulCurls: 1,
	}

	result = processResults(rawResults)
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Test case 3 failed: Expected %v, but got %v", expected, result)
	}
}

func TestMakeDNSPolicy(t *testing.T) {
	namespace := "test-namespace"
	name := "test-policy"
	targetURL := "http://www.example.com"
	targetDomain := "www.example.com"
	var testdomains = []string{
		targetDomain,
	}
	orderOne := float64(1)
	udp := numorstring.ProtocolFromString("UDP")

	expected := v3.NetworkPolicy{
		ObjectMeta: metav1.ObjectMeta{
			Name:      fmt.Sprintf("default.%s", name),
			Namespace: namespace,
		},
		Spec: v3.NetworkPolicySpec{
			Order:    &orderOne,
			Tier:     "default",
			Selector: "dep == 'dnsperf'",
			Types:    []v3.PolicyType{"Egress"},
			Egress: []v3.Rule{
				{
					Action:   v3.Allow,
					Protocol: &udp,
					Destination: v3.EntityRule{
						Ports: []numorstring.Port{
							numorstring.SinglePort(53),
							numorstring.NamedPort("dns"),
						},
					},
				},
				{
					Action: "Allow",
					Destination: v3.EntityRule{
						Domains: testdomains,
					},
				},
			},
		},
	}

	// Test case 1: One domain
	numDomains := 1
	result, err := MakeDNSPolicy(namespace, name, numDomains, targetURL)
	if err != nil {
		t.Errorf("MakeDNSPolicy() returned an error: %v", err)
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MakeDNSPolicy() failed, expected: %v, got: %v", expected, result)
	}

	// Test case 2: Zero domains
	numDomains = 0
	result, err = MakeDNSPolicy(namespace, name, numDomains, targetURL)
	if err != nil {
		t.Errorf("MakeDNSPolicy() returned an error: %v", err)
	}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("MakeDNSPolicy() failed, expected: %v, got: %v", expected, result)
	}

	// Test case 2: 50 domains
	numDomains = 50
	result, err = MakeDNSPolicy(namespace, name, numDomains, targetURL)
	if err != nil {
		t.Errorf("MakeDNSPolicy() returned an error: %v", err)
	}
	if len(result.Spec.Egress[1].Destination.Domains) != 50 {
		t.Errorf("MakeDNSPolicy() failed, expected: %v, got: %v", numDomains, len(result.Spec.Egress[1].Destination.Domains))
	}

	// Test case 3: IPv4 in targetURL - expect an error
	numDomains = 1
	result, err = MakeDNSPolicy(namespace, name, numDomains, "http://192.168.1.1")
	if err == nil {
		t.Errorf("MakeDNSPolicy() should return an error for IPv4 address")
	}

	// Test case 4: IPv6 in targetURL - expect an error
	result, err = MakeDNSPolicy(namespace, name, numDomains, "http://[::1]")
	if err == nil {
		t.Errorf("MakeDNSPolicy() should return an error for IPv6 address")
	}

	// Test case 5: IPv6 full address in targetURL - expect an error
	result, err = MakeDNSPolicy(namespace, name, numDomains, "http://[2001:db8::1]")
	if err == nil {
		t.Errorf("MakeDNSPolicy() should return an error for IPv6 address")
	}

	// Test case 6: Empty targetURL - expect an error
	result, err = MakeDNSPolicy(namespace, name, numDomains, "")
	if err == nil {
		t.Errorf("MakeDNSPolicy() should return an error for empty targetURL")
	}
}
