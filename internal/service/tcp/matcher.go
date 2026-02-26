package tcp

import (
	"strings"
)

// Pattern represents a compiled TCP pattern
type Pattern struct {
	Raw      string // Original pattern
	Parts    []string // Pattern parts (split on *)
	Response string // Response to send
}

// Matcher handles pattern matching for TCP data
type Matcher struct {
	patterns []*Pattern
	default_  string // Default response when no pattern matches
}

// NewMatcher creates a new pattern matcher
func NewMatcher() *Matcher {
	return &Matcher{
		patterns: make([]*Pattern, 0),
	}
}

// AddPattern adds a pattern to the matcher
func (m *Matcher) AddPattern(pattern, response string) {
	parts := strings.Split(pattern, "*")
	m.patterns = append(m.patterns, &Pattern{
		Raw:      pattern,
		Parts:    parts,
		Response: response,
	})
}

// SetDefault sets the default response
func (m *Matcher) SetDefault(response string) {
	m.default_ = response
}

// Match attempts to match incoming data against patterns
// Returns the response to send, or empty string if no match
func (m *Matcher) Match(data string) string {
	// Normalize: trim whitespace
	data = strings.TrimSpace(data)

	// Try each pattern in order
	for _, pattern := range m.patterns {
		if m.matchPattern(data, pattern) {
			return pattern.Response
		}
	}

	// No pattern matched, return default
	return m.default_
}

// matchPattern checks if data matches a pattern
func (m *Matcher) matchPattern(data string, pattern *Pattern) bool {
	parts := pattern.Parts

	// If no wildcards, must be exact match
	if len(parts) == 1 {
		return data == parts[0]
	}

	// Must start with first part
	if !strings.HasPrefix(data, parts[0]) {
		return false
	}

	// Must end with last part
	if !strings.HasSuffix(data, parts[len(parts)-1]) {
		return false
	}

	// Check middle parts in order
	pos := len(parts[0])
	for i := 1; i < len(parts)-1; i++ {
		part := parts[i]
		if part == "" {
			continue // consecutive wildcards
		}

		idx := strings.Index(data[pos:], part)
		if idx == -1 {
			return false
		}
		pos += idx + len(part)
	}

	return true
}
