package opensearch

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/timberio/go-datemath"
)

// ParseDateTime handles various datetime formats including datemath expressions
func ParseDateTime(timeStr string) (*time.Time, error) {
	if timeStr == "" {
		return nil, nil
	}

	// Handle datemath expressions using the go-datemath library
	if strings.HasPrefix(timeStr, "now") || strings.Contains(timeStr, "/") {
		parsedTime, err := datemath.ParseAndEvaluate(timeStr, datemath.WithNow(time.Now()))
		if err != nil {
			return nil, fmt.Errorf("failed to parse datemath expression '%s': %w", timeStr, err)
		}
		return &parsedTime, nil
	}

	// Handle Unix timestamps (seconds)
	if val, err := strconv.ParseInt(timeStr, 10, 64); err == nil {
		if val > 1000000000 && val < 10000000000 { // Valid Unix timestamp range
			t := time.Unix(val, 0)
			return &t, nil
		}
	}

	// Handle Unix timestamps (milliseconds)
	if val, err := strconv.ParseInt(timeStr, 10, 64); err == nil {
		if val > 1000000000000 && val < 10000000000000 { // Valid Unix timestamp in ms
			t := time.Unix(val/1000, (val%1000)*1000000)
			return &t, nil
		}
	}

	// Try parsing as RFC3339
	if t, err := time.Parse(time.RFC3339, timeStr); err == nil {
		return &t, nil
	}

	// Try parsing as RFC3339 without timezone
	if t, err := time.Parse("2006-01-02T15:04:05", timeStr); err == nil {
		return &t, nil
	}

	// Try parsing as ISO date
	if t, err := time.Parse("2006-01-02", timeStr); err == nil {
		return &t, nil
	}

	// Try parsing as date with time (common log format)
	if t, err := time.Parse("2006-01-02 15:04:05", timeStr); err == nil {
		return &t, nil
	}

	// Try parsing as date with time and timezone
	if t, err := time.Parse("2006-01-02 15:04:05 MST", timeStr); err == nil {
		return &t, nil
	}

	return nil, fmt.Errorf("unable to parse time string '%s': unsupported format", timeStr)
}

// FormatForOpenSearch formats a time for use in OpenSearch queries
func FormatForOpenSearch(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format(time.RFC3339)
}

// ParseDateTimeRange parses from and to time strings and returns formatted times for OpenSearch
func ParseDateTimeRange(fromStr, toStr string) (from, to string, err error) {
	var fromTime, toTime *time.Time

	if fromStr != "" {
		fromTime, err = ParseDateTime(fromStr)
		if err != nil {
			return "", "", fmt.Errorf("invalid 'from' time: %w", err)
		}
	}

	if toStr != "" {
		toTime, err = ParseDateTime(toStr)
		if err != nil {
			return "", "", fmt.Errorf("invalid 'to' time: %w", err)
		}
	}

	// Validate time range
	if fromTime != nil && toTime != nil && fromTime.After(*toTime) {
		return "", "", fmt.Errorf("'from' time (%s) cannot be after 'to' time (%s)",
			fromTime.Format(time.RFC3339), toTime.Format(time.RFC3339))
	}

	return FormatForOpenSearch(fromTime), FormatForOpenSearch(toTime), nil
}

// GetSupportedFormats returns a list of supported datetime formats for help text
func GetSupportedFormats() []string {
	return []string{
		"Datemath expressions:",
		"  now",
		"  now-24h",
		"  now-7d",
		"  now-1M",
		"  now/d (rounded to start of day)",
		"  now-1d/d (yesterday start of day)",
		"",
		"Absolute formats:",
		"  2006-01-02",
		"  2006-01-02T15:04:05",
		"  2006-01-02T15:04:05Z",
		"  2006-01-02 15:04:05",
		"  1640995200 (Unix timestamp)",
		"  1640995200000 (Unix timestamp in ms)",
	}
}
