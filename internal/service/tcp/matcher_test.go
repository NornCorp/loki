package tcp

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewMatcher(t *testing.T) {
	m := NewMatcher()
	require.NotNil(t, m)
	require.NotNil(t, m.patterns)
	require.Empty(t, m.patterns)
}

func TestAddPattern(t *testing.T) {
	m := NewMatcher()

	m.AddPattern("PING*", "+PONG\r\n")

	require.Len(t, m.patterns, 1)
	require.Equal(t, "PING*", m.patterns[0].Raw)
	require.Equal(t, "+PONG\r\n", m.patterns[0].Response)
	require.Equal(t, []string{"PING", ""}, m.patterns[0].Parts)
}

func TestSetDefault(t *testing.T) {
	m := NewMatcher()

	m.SetDefault("-ERR unknown\r\n")

	require.Equal(t, "-ERR unknown\r\n", m.default_)
}

func TestMatch_ExactMatch(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("PING", "+PONG\r\n")

	result := m.Match("PING")

	require.Equal(t, "+PONG\r\n", result)
}

func TestMatch_WildcardSuffix(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("PING*", "+PONG\r\n")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "exact match",
			input:    "PING",
			expected: "+PONG\r\n",
		},
		{
			name:     "with trailing data",
			input:    "PING hello",
			expected: "+PONG\r\n",
		},
		{
			name:     "with newline",
			input:    "PING\r\n",
			expected: "+PONG\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMatch_WildcardMiddle(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("GET *", "$5\r\nhello\r\n")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "single argument",
			input:    "GET mykey",
			expected: "$5\r\nhello\r\n",
		},
		{
			name:     "multiple words",
			input:    "GET my long key",
			expected: "$5\r\nhello\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMatch_MultipleWildcards(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("SET * *", "+OK\r\n")

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "two arguments",
			input:    "SET key value",
			expected: "+OK\r\n",
		},
		{
			name:     "complex value",
			input:    "SET mykey my long value with spaces",
			expected: "+OK\r\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.Match(tt.input)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMatch_FirstMatchWins(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("GET *", "first")
	m.AddPattern("GET mykey", "second")

	result := m.Match("GET mykey")

	require.Equal(t, "first", result)
}

func TestMatch_NoMatch_ReturnsDefault(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("PING*", "+PONG\r\n")
	m.SetDefault("-ERR unknown\r\n")

	result := m.Match("UNKNOWN")

	require.Equal(t, "-ERR unknown\r\n", result)
}

func TestMatch_NoMatch_NoDefault(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("PING*", "+PONG\r\n")

	result := m.Match("UNKNOWN")

	require.Equal(t, "", result)
}

func TestMatch_TrimsWhitespace(t *testing.T) {
	m := NewMatcher()
	m.AddPattern("PING", "+PONG\r\n")

	result := m.Match("  PING  ")

	require.Equal(t, "+PONG\r\n", result)
}

func TestMatchPattern_NoWildcard(t *testing.T) {
	m := NewMatcher()
	pattern := &Pattern{
		Raw:   "PING",
		Parts: []string{"PING"},
	}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "exact match",
			input:    "PING",
			expected: true,
		},
		{
			name:     "different command",
			input:    "PONG",
			expected: false,
		},
		{
			name:     "prefix match",
			input:    "PING extra",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.matchPattern(tt.input, pattern)
			require.Equal(t, tt.expected, result)
		})
	}
}

func TestMatchPattern_WildcardPrefix(t *testing.T) {
	m := NewMatcher()
	pattern := &Pattern{
		Raw:   "*PING",
		Parts: []string{"", "PING"},
	}

	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{
			name:     "suffix match",
			input:    "PREFIX PING",
			expected: true,
		},
		{
			name:     "just PING",
			input:    "PING",
			expected: true,
		},
		{
			name:     "no match",
			input:    "PING SUFFIX",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := m.matchPattern(tt.input, pattern)
			require.Equal(t, tt.expected, result)
		})
	}
}
