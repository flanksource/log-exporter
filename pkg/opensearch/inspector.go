package opensearch

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// IndexInfo represents information about an OpenSearch index
type IndexInfo struct {
	Type            string   `json:"type"`             // "kubernetes", "jaeger", "generic"
	TimestampField  string   `json:"timestamp_field"`  // "@timestamp", "startTime", "timestamp", etc.
	AvailableFields []string `json:"available_fields"` // All fields in the index
	HasDateField    bool     `json:"has_date_field"`   // Whether any date field was found
	IndexPattern    string   `json:"index_pattern"`    // The pattern used for inspection
}

// InspectIndex analyzes an index to determine its type and timestamp field
func (c *Client) InspectIndex(indexPattern string) (*IndexInfo, error) {
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}

	// Get index mapping
	res, err := client.Indices.GetMapping(
		client.Indices.GetMapping.WithIndex(indexPattern),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping for index %s: %w", indexPattern, err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("failed to get mapping, status: %s", res.Status())
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read mapping response: %w", err)
	}

	var mappingResponse map[string]interface{}
	if err := json.Unmarshal(body, &mappingResponse); err != nil {
		return nil, fmt.Errorf("failed to parse mapping response: %w", err)
	}

	// Extract field information
	fieldTypes := make(map[string]string)
	fieldSet := make(map[string]bool)

	// Extract fields from all matching indices
	for _, indexData := range mappingResponse {
		if indexMap, ok := indexData.(map[string]interface{}); ok {
			if mappings, ok := indexMap["mappings"].(map[string]interface{}); ok {
				extractFieldsFromMapping(mappings, "", fieldSet)
				extractTypesFromProperties(mappings, "", fieldTypes)
			}
		}
	}

	// Convert field set to slice
	var availableFields []string
	for field := range fieldSet {
		availableFields = append(availableFields, field)
	}

	// Build index info
	info := &IndexInfo{
		IndexPattern:    indexPattern,
		AvailableFields: availableFields,
	}

	// Detect log type
	info.Type = c.detectLogType(indexPattern, availableFields)

	// Detect timestamp field
	info.TimestampField, info.HasDateField = c.detectTimestampField(fieldTypes, availableFields, info.Type)

	return info, nil
}

// detectLogType determines the type of logs based on index pattern and available fields
func (c *Client) detectLogType(indexPattern string, availableFields []string) string {
	lowerIndex := strings.ToLower(indexPattern)
	fieldMap := make(map[string]bool)
	for _, field := range availableFields {
		fieldMap[strings.ToLower(field)] = true
	}

	// Check for Kubernetes/Filebeat patterns
	k8sPatterns := []string{"filebeat", "kubernetes", "k8s", "eks", "gke", "aks"}
	k8sFields := []string{"kubernetes.namespace", "kubernetes.pod.name", "kubernetes.container.name"}

	for _, pattern := range k8sPatterns {
		if strings.Contains(lowerIndex, pattern) {
			return "kubernetes"
		}
	}

	// Check for Kubernetes fields in the mapping
	k8sFieldCount := 0
	for _, field := range k8sFields {
		if fieldMap[strings.ToLower(field)] {
			k8sFieldCount++
		}
	}
	if k8sFieldCount >= 2 {
		return "kubernetes"
	}

	// Check for Jaeger patterns
	jaegerPatterns := []string{"jaeger", "span", "trace", "otel", "apm"}
	jaegerFields := []string{"traceid", "spanid", "servicename", "operationname", "starttime"}

	for _, pattern := range jaegerPatterns {
		if strings.Contains(lowerIndex, pattern) {
			return "jaeger"
		}
	}

	// Check for Jaeger fields in the mapping
	jaegerFieldCount := 0
	for _, field := range jaegerFields {
		if fieldMap[strings.ToLower(field)] {
			jaegerFieldCount++
		}
	}
	if jaegerFieldCount >= 3 {
		return "jaeger"
	}

	// Default to generic
	return "generic"
}

// detectTimestampField finds the appropriate timestamp field for the log type
func (c *Client) detectTimestampField(fieldTypes map[string]string, availableFields []string, logType string) (string, bool) {
	fieldMap := make(map[string]bool)
	for _, field := range availableFields {
		fieldMap[field] = true
	}

	// Define priority order for timestamp fields based on log type
	var timestampCandidates []string

	switch logType {
	case "kubernetes":
		timestampCandidates = []string{
			"@timestamp",
			"timestamp",
			"time",
			"date",
			"created_at",
		}
	case "jaeger":
		timestampCandidates = []string{
			"startTime",
			"@timestamp",
			"timestamp",
			"time",
			"date",
		}
	default: // generic
		timestampCandidates = []string{
			"@timestamp",
			"timestamp",
			"time",
			"date",
			"created_at",
			"log_time",
			"event_time",
		}
	}

	// Check each candidate
	for _, candidate := range timestampCandidates {
		if fieldMap[candidate] {
			// Verify field type is date if we have type information
			if fieldType, exists := fieldTypes[candidate]; exists {
				if fieldType == "date" {
					return candidate, true
				}
			} else {
				// If no type info, assume it's valid if field exists
				return candidate, true
			}
		}
	}

	// Check for any date field as fallback
	for field, fieldType := range fieldTypes {
		if fieldType == "date" {
			return field, true
		}
	}

	// No date field found, return default based on type
	switch logType {
	case "jaeger":
		return "startTime", false
	default:
		return "@timestamp", false
	}
}

// extractTypesFromProperties recursively extracts field types from OpenSearch mapping
func extractTypesFromProperties(properties map[string]interface{}, prefix string, fieldTypes map[string]string) {
	if props, ok := properties["properties"].(map[string]interface{}); ok {
		for fieldName, fieldMapping := range props {
			fullFieldName := fieldName
			if prefix != "" {
				fullFieldName = prefix + "." + fieldName
			}

			if fieldMap, ok := fieldMapping.(map[string]interface{}); ok {
				if fieldType, ok := fieldMap["type"].(string); ok {
					fieldTypes[fullFieldName] = fieldType
				}

				// Recursively process nested fields
				extractTypesFromProperties(fieldMap, fullFieldName, fieldTypes)
			}
		}
	}
}

// GetRecommendedTimestampField returns the recommended timestamp field for a log type
func GetRecommendedTimestampField(logType string) string {
	switch logType {
	case "kubernetes":
		return "@timestamp"
	case "jaeger":
		return "startTime"
	default:
		return "@timestamp"
	}
}

// ValidateIndexInfo checks if the index info is valid and provides recommendations
func (info *IndexInfo) ValidateIndexInfo() []string {
	var warnings []string

	if !info.HasDateField {
		warnings = append(warnings, fmt.Sprintf(
			"No date field found in index '%s'. Time-based queries may not work correctly.",
			info.IndexPattern))
	}

	if info.TimestampField == "" {
		warnings = append(warnings, "No timestamp field detected. Using default '@timestamp'.")
	}

	if len(info.AvailableFields) == 0 {
		warnings = append(warnings, "No fields found in index mapping. Index may be empty or misconfigured.")
	}

	return warnings
}

// String returns a human-readable representation of the index info
func (info *IndexInfo) String() string {
	var sb strings.Builder

	sb.WriteString(fmt.Sprintf("Index Pattern: %s\n", info.IndexPattern))
	sb.WriteString(fmt.Sprintf("Detected Type: %s\n", info.Type))
	sb.WriteString(fmt.Sprintf("Timestamp Field: %s\n", info.TimestampField))
	sb.WriteString(fmt.Sprintf("Has Date Field: %t\n", info.HasDateField))
	sb.WriteString(fmt.Sprintf("Available Fields: %d\n", len(info.AvailableFields)))

	if len(info.AvailableFields) > 0 && len(info.AvailableFields) <= 20 {
		sb.WriteString("Fields: ")
		sb.WriteString(strings.Join(info.AvailableFields, ", "))
		sb.WriteString("\n")
	}

	warnings := info.ValidateIndexInfo()
	if len(warnings) > 0 {
		sb.WriteString("Warnings:\n")
		for _, warning := range warnings {
			sb.WriteString(fmt.Sprintf("  - %s\n", warning))
		}
	}

	return sb.String()
}
