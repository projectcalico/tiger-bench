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
	"fmt"
	"math/rand"
	"net/url"
	"strings"
)

// SanitizeString removes newlines, carriage returns, and periods from a string
func SanitizeString(s string) string {
	s = strings.ReplaceAll(s, "\n", "")
	s = strings.ReplaceAll(s, "\r", "")
	s = strings.ReplaceAll(s, ".", "-")
	return s
}

// RandomString generates a random string of length n
func RandomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// ExtractDomainFromURL extracts the domain/hostname from a URL string
// It uses net/url for reliable parsing of URLs with various formats
func ExtractDomainFromURL(urlStr string) (string, error) {
	// If it doesn't have a scheme, fail early
	if !strings.Contains(urlStr, "://") {
		return "", fmt.Errorf("invalid URL: missing scheme (http:// or https://)")
	}

	u, err := url.Parse(urlStr)
	if err != nil {
		return "", fmt.Errorf("failed to parse URL: %w", err)
	}

	// Hostname() returns the hostname without port (and without brackets for IPv6)
	hostname := u.Hostname()
	if hostname == "" {
		return "", fmt.Errorf("failed to extract hostname from URL")
	}

	return hostname, nil
}
