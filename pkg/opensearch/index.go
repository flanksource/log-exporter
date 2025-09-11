package opensearch

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/timberio/go-datemath"
)

// IndexMetadata represents metadata information about an OpenSearch index
type IndexMetadata struct {
	Name     string
	UUID     string
	Settings map[string]interface{}
	Mappings map[string]interface{}
}

// ResolveIndices resolves index patterns to concrete indices, optionally filtering by date range
func (c *Client) ResolveIndices(indexPattern string, from, to string) ([]string, error) {
	// If no wildcards, return as-is
	if !strings.Contains(indexPattern, "*") {
		return []string{indexPattern}, nil
	}

	// Get all indices
	allIndices, err := c.getAllIndices()
	if err != nil {
		return nil, fmt.Errorf("failed to get indices: %w", err)
	}

	// Filter by pattern
	pattern := strings.ReplaceAll(indexPattern, "*", ".*")
	regex, err := regexp.Compile("^" + pattern + "$")
	if err != nil {
		return nil, fmt.Errorf("invalid index pattern '%s': %w", indexPattern, err)
	}

	var matchingIndices []string
	for _, index := range allIndices {
		if regex.MatchString(index) {
			matchingIndices = append(matchingIndices, index)
		}
	}

	// If no date filtering requested, return all matches
	if from == "" && to == "" {
		sort.Strings(matchingIndices)
		return matchingIndices, nil
	}

	// Parse date range
	fromTime, toTime, err := c.parseDateRange(from, to)
	if err != nil {
		return nil, fmt.Errorf("failed to parse date range: %w", err)
	}

	// Filter indices by date range
	filtered, err := c.filterIndicesByDateRange(matchingIndices, fromTime, toTime)
	if err != nil {
		return nil, fmt.Errorf("failed to filter indices by date: %w", err)
	}

	sort.Strings(filtered)
	return filtered, nil
}

// GetIndexMetadata retrieves detailed information about an index including mappings and settings
func (c *Client) GetIndexMetadata(indexName string) (*IndexMetadata, error) {
	// Get OpenSearch client
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}

	// Get index settings and mappings
	resp, err := client.Indices.Get([]string{indexName})
	if err != nil {
		return nil, fmt.Errorf("failed to get index info for '%s': %w", indexName, err)
	}
	defer resp.Body.Close()

	if resp.IsError() {
		return nil, fmt.Errorf("failed to get index info for '%s': %s", indexName, resp.String())
	}

	var indexData map[string]struct {
		Settings map[string]interface{} `json:"settings"`
		Mappings map[string]interface{} `json:"mappings"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&indexData); err != nil {
		return nil, fmt.Errorf("failed to decode index info response: %w", err)
	}

	info, exists := indexData[indexName]
	if !exists {
		return nil, fmt.Errorf("index '%s' not found in response", indexName)
	}

	// Extract UUID from settings
	uuid := ""
	if settings, ok := info.Settings["index"].(map[string]interface{}); ok {
		if u, ok := settings["uuid"].(string); ok {
			uuid = u
		}
	}

	return &IndexMetadata{
		Name:     indexName,
		UUID:     uuid,
		Settings: info.Settings,
		Mappings: info.Mappings,
	}, nil
}

// getAllIndices retrieves all index names from the cluster
func (c *Client) getAllIndices() ([]string, error) {
	// Get OpenSearch client
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}

	resp, err := client.Cat.Indices(
		client.Cat.Indices.WithFormat("json"),
		client.Cat.Indices.WithH("index"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w", err)
	}
	defer resp.Body.Close()

	if resp.IsError() {
		return nil, fmt.Errorf("failed to list indices: %s", resp.String())
	}

	var indices []struct {
		Index string `json:"index"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&indices); err != nil {
		return nil, fmt.Errorf("failed to decode indices response: %w", err)
	}

	var indexNames []string
	for _, idx := range indices {
		// Skip system indices
		if !strings.HasPrefix(idx.Index, ".") {
			indexNames = append(indexNames, idx.Index)
		}
	}

	return indexNames, nil
}

// parseDateRange parses from/to strings using datemath
func (c *Client) parseDateRange(from, to string) (*time.Time, *time.Time, error) {
	var fromTime, toTime *time.Time

	if from != "" {
		expr, err := datemath.Parse(from)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid 'from' date '%s': %w", from, err)
		}
		parsed := expr.Time()
		fromTime = &parsed
	}

	if to != "" {
		expr, err := datemath.Parse(to)
		if err != nil {
			return nil, nil, fmt.Errorf("invalid 'to' date '%s': %w", to, err)
		}
		parsed := expr.Time()
		toTime = &parsed
	}

	return fromTime, toTime, nil
}

// filterIndicesByDateRange filters indices based on their date patterns
func (c *Client) filterIndicesByDateRange(indices []string, fromTime, toTime *time.Time) ([]string, error) {
	var filtered []string

	for _, index := range indices {
		include, err := c.indexInDateRange(index, fromTime, toTime)
		if err != nil {
			if c.config.Verbose {
				fmt.Printf("Warning: could not determine date for index '%s': %v\n", index, err)
			}
			// Include indices we can't parse dates for
			filtered = append(filtered, index)
			continue
		}

		if include {
			filtered = append(filtered, index)
		}
	}

	return filtered, nil
}

// indexInDateRange determines if an index falls within the specified date range
func (c *Client) indexInDateRange(indexName string, fromTime, toTime *time.Time) (bool, error) {
	// Common date patterns in index names
	datePatterns := []string{
		"2006.01.02", // logstash-2024.01.15
		"2006-01-02", // filebeat-2024-01-15
		"2006.01",    // logs-2024.01
		"2006-01",    // logs-2024-01
		"20060102",   // logs20240115
	}

	// Extract potential date strings from index name
	for _, pattern := range datePatterns {
		if indexDate := c.extractDateFromIndex(indexName, pattern); indexDate != nil {
			// Check if date falls within range
			if fromTime != nil && indexDate.Before(*fromTime) {
				return false, nil
			}
			if toTime != nil && indexDate.After(*toTime) {
				return false, nil
			}
			return true, nil
		}
	}

	// If we can't extract a date, assume it's in range
	return true, fmt.Errorf("no date pattern found in index name")
}

// extractDateFromIndex tries to extract a date from an index name using the given pattern
func (c *Client) extractDateFromIndex(indexName, datePattern string) *time.Time {
	// Create regex pattern from Go time format
	regexPattern := datePattern
	regexPattern = strings.ReplaceAll(regexPattern, "2006", `(\d{4})`)
	regexPattern = strings.ReplaceAll(regexPattern, "01", `(\d{2})`)
	regexPattern = strings.ReplaceAll(regexPattern, "02", `(\d{2})`)

	regex := regexp.MustCompile(regexPattern)
	matches := regex.FindStringSubmatch(indexName)

	if len(matches) == 0 {
		return nil
	}

	// Try to parse the matched portion as a date
	for _, match := range matches[1:] { // Skip the full match
		if len(match) >= 4 { // Must be at least YYYY
			// Find the full date string in the original index name
			startIdx := strings.Index(indexName, match)
			if startIdx == -1 {
				continue
			}

			// Extract potential date string
			for _, pattern := range []string{"2006.01.02", "2006-01-02", "2006.01", "2006-01", "20060102"} {
				dateStr := c.extractDateString(indexName, startIdx, pattern)
				if dateStr != "" {
					if parsed, err := time.Parse(pattern, dateStr); err == nil {
						return &parsed
					}
				}
			}
		}
	}

	return nil
}

// extractDateString extracts a date string from index name starting at a given position
func (c *Client) extractDateString(indexName string, startIdx int, pattern string) string {
	if startIdx+len(pattern) > len(indexName) {
		return ""
	}

	candidate := indexName[startIdx : startIdx+len(pattern)]

	// Validate the candidate matches the pattern structure
	switch pattern {
	case "2006.01.02":
		if regexp.MustCompile(`^\d{4}\.\d{2}\.\d{2}$`).MatchString(candidate) {
			return candidate
		}
	case "2006-01-02":
		if regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`).MatchString(candidate) {
			return candidate
		}
	case "2006.01":
		if regexp.MustCompile(`^\d{4}\.\d{2}$`).MatchString(candidate) {
			return candidate
		}
	case "2006-01":
		if regexp.MustCompile(`^\d{4}-\d{2}$`).MatchString(candidate) {
			return candidate
		}
	case "20060102":
		if regexp.MustCompile(`^\d{8}$`).MatchString(candidate) {
			return candidate
		}
	}

	return ""
}
