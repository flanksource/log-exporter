package opensearch

import (
	"strings"
	"testing"
	"time"
)

func TestParseDateTimeWithDateMath(t *testing.T) {
	// Test current time for reference
	now := time.Now()

	tests := []struct {
		name        string
		input       string
		expectError bool
		validate    func(*time.Time) bool
	}{
		{
			name:        "empty string",
			input:       "",
			expectError: false,
			validate:    func(t *time.Time) bool { return t == nil },
		},
		{
			name:        "now expression",
			input:       "now",
			expectError: false,
			validate: func(t *time.Time) bool {
				return t != nil && t.Unix() >= now.Unix()-5 && t.Unix() <= now.Unix()+5
			},
		},
		{
			name:        "now minus 24 hours",
			input:       "now-24h",
			expectError: false,
			validate: func(t *time.Time) bool {
				expected := now.Add(-24 * time.Hour)
				return t != nil && abs(t.Unix()-expected.Unix()) < 5
			},
		},
		{
			name:        "now minus 7 days",
			input:       "now-7d",
			expectError: false,
			validate: func(t *time.Time) bool {
				expected := now.Add(-7 * 24 * time.Hour)
				return t != nil && abs(t.Unix()-expected.Unix()) < 5
			},
		},
		{
			name:        "RFC3339 format",
			input:       "2023-01-01T00:00:00Z",
			expectError: false,
			validate: func(t *time.Time) bool {
				expected := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
				return t != nil && t.Equal(expected)
			},
		},
		{
			name:        "ISO date format",
			input:       "2023-01-01",
			expectError: false,
			validate: func(t *time.Time) bool {
				expected := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
				return t != nil && t.Equal(expected)
			},
		},
		{
			name:        "Unix timestamp",
			input:       "1640995200", // 2022-01-01 00:00:00 UTC
			expectError: false,
			validate: func(t *time.Time) bool {
				expected := time.Unix(1640995200, 0)
				return t != nil && t.Equal(expected)
			},
		},
		{
			name:        "Unix timestamp in milliseconds",
			input:       "1640995200000",
			expectError: false,
			validate: func(t *time.Time) bool {
				expected := time.Unix(1640995200, 0)
				return t != nil && t.Equal(expected)
			},
		},
		{
			name:        "invalid format",
			input:       "invalid-time",
			expectError: true,
			validate:    func(t *time.Time) bool { return t == nil },
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := ParseDateTime(tt.input)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for input '%s', but got none", tt.input)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for input '%s': %v", tt.input, err)
				}
			}

			if !tt.validate(result) {
				t.Errorf("Validation failed for input '%s', result: %v", tt.input, result)
			}
		})
	}
}

func TestParseDateTimeRange(t *testing.T) {
	tests := []struct {
		name        string
		from        string
		to          string
		expectError bool
	}{
		{
			name:        "valid range",
			from:        "2023-01-01",
			to:          "2023-01-02",
			expectError: false,
		},
		{
			name:        "datemath range",
			from:        "now-24h",
			to:          "now",
			expectError: false,
		},
		{
			name:        "empty range",
			from:        "",
			to:          "",
			expectError: false,
		},
		{
			name:        "only from",
			from:        "now-1h",
			to:          "",
			expectError: false,
		},
		{
			name:        "only to",
			from:        "",
			to:          "now",
			expectError: false,
		},
		{
			name:        "invalid from",
			from:        "invalid",
			to:          "now",
			expectError: true,
		},
		{
			name:        "invalid to",
			from:        "now-1h",
			to:          "invalid",
			expectError: true,
		},
		{
			name:        "from after to",
			from:        "2023-01-02",
			to:          "2023-01-01",
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			from, to, err := ParseDateTimeRange(tt.from, tt.to)

			if tt.expectError {
				if err == nil {
					t.Errorf("Expected error for range '%s' to '%s', but got none", tt.from, tt.to)
				}
			} else {
				if err != nil {
					t.Errorf("Unexpected error for range '%s' to '%s': %v", tt.from, tt.to, err)
				}

				// Validate that if strings were provided, we got formatted output
				if tt.from != "" && from == "" {
					t.Errorf("Expected non-empty 'from' result for input '%s'", tt.from)
				}
				if tt.to != "" && to == "" {
					t.Errorf("Expected non-empty 'to' result for input '%s'", tt.to)
				}
			}
		})
	}
}

func TestFormatForOpenSearch(t *testing.T) {
	tests := []struct {
		name     string
		input    *time.Time
		expected string
	}{
		{
			name:     "nil time",
			input:    nil,
			expected: "",
		},
		{
			name:     "valid time",
			input:    func() *time.Time { t := time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC); return &t }(),
			expected: "2023-01-01T12:00:00Z",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FormatForOpenSearch(tt.input)
			if result != tt.expected {
				t.Errorf("FormatForOpenSearch() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestGetSupportedFormats(t *testing.T) {
	formats := GetSupportedFormats()

	if len(formats) == 0 {
		t.Error("GetSupportedFormats() returned empty slice")
	}

	// Check that some expected formats are present
	foundDatemath := false
	foundAbsolute := false

	for _, format := range formats {
		if strings.Contains(format, "now") {
			foundDatemath = true
		}
		if strings.Contains(format, "2006-01-02") {
			foundAbsolute = true
		}
	}

	if !foundDatemath {
		t.Error("GetSupportedFormats() should include datemath examples")
	}
	if !foundAbsolute {
		t.Error("GetSupportedFormats() should include absolute time examples")
	}
}

// Helper function to calculate absolute difference
func abs(x int64) int64 {
	if x < 0 {
		return -x
	}
	return x
}
