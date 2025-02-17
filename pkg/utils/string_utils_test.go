package utils

import (
	"testing"
	"unicode"
)

func TestSanitizeString(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"Hello\nWorld", "HelloWorld"},
		{"Hello\rWorld", "HelloWorld"},
		{"Hello.World", "Hello-World"},
		{"Hello\n\r.World", "Hello-World"},
		{"NoSpecialChars", "NoSpecialChars"},
	}

	for _, test := range tests {
		result := SanitizeString(test.input)
		if result != test.expected {
			t.Errorf("SanitizeString(%q) = %q; want %q", test.input, result, test.expected)
		}
	}
}

func TestRandomString(t *testing.T) {
	length := 10
	result := RandomString(length)
	if len(result) != length {
		t.Errorf("RandomString(%d) = %q; length = %d; want %d", length, result, len(result), length)
	}

	for _, char := range result {
		if !unicode.IsLetter(char) && !unicode.IsDigit(char) {
			t.Errorf("RandomString(%d) contains invalid character: %q", length, char)
		}
	}
}
