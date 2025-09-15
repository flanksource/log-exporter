package opensearch

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flanksource/commons/logger"
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

	if c.config.Verbose {
		logger.Tracef(" Detecting log type for index pattern: %s\n", indexPattern)
		logger.Tracef(" Available fields count: %d\n", len(availableFields))
		if c.config.Debug {
			logger.Tracef(" Available fields: %v\n", availableFields)
		}
	}

	// Check for Kubernetes/Filebeat patterns
	k8sPatterns := []string{"filebeat", "kubernetes", "k8s", "eks", "gke", "aks"}
	k8sFields := []string{"kubernetes.namespace", "kubernetes.pod.name", "kubernetes.container.name"}

	var matchedK8sPatterns []string
	for _, pattern := range k8sPatterns {
		if strings.Contains(lowerIndex, pattern) {
			matchedK8sPatterns = append(matchedK8sPatterns, pattern)
		}
	}

	if len(matchedK8sPatterns) > 0 {
		if c.config.Verbose {
			logger.Tracef(" Matched Kubernetes patterns in index name: %v\n", matchedK8sPatterns)
			logger.Tracef(" Detected log type: kubernetes (by pattern match)\n")
		}
		return "kubernetes"
	}

	// Check for Kubernetes fields in the mapping
	k8sFieldCount := 0
	var foundK8sFields []string
	for _, field := range k8sFields {
		if fieldMap[strings.ToLower(field)] {
			k8sFieldCount++
			foundK8sFields = append(foundK8sFields, field)
		}
	}

	if c.config.Verbose {
		logger.Tracef(" Kubernetes field detection: found %d/%d required fields\n", k8sFieldCount, 2)
		if len(foundK8sFields) > 0 {
			logger.Tracef(" Found Kubernetes fields: %v\n", foundK8sFields)
		}
	}

	if k8sFieldCount >= 2 {
		if c.config.Verbose {
			logger.Tracef(" Detected log type: kubernetes (by field presence)\n")
		}
		return "kubernetes"
	}

	// Check for Jaeger patterns
	jaegerPatterns := []string{"jaeger", "span", "trace", "otel", "apm"}
	jaegerFields := []string{"traceid", "spanid", "servicename", "operationname", "starttime"}

	var matchedJaegerPatterns []string
	for _, pattern := range jaegerPatterns {
		if strings.Contains(lowerIndex, pattern) {
			matchedJaegerPatterns = append(matchedJaegerPatterns, pattern)
		}
	}

	if len(matchedJaegerPatterns) > 0 {
		if c.config.Verbose {
			logger.Tracef(" Matched Jaeger/OpenTelemetry patterns in index name: %v\n", matchedJaegerPatterns)
			logger.Tracef(" Detected log type: jaeger (by pattern match)\n")
		}
		return "jaeger"
	}

	// Check for Jaeger fields in the mapping
	jaegerFieldCount := 0
	var foundJaegerFields []string
	for _, field := range jaegerFields {
		if fieldMap[strings.ToLower(field)] {
			jaegerFieldCount++
			foundJaegerFields = append(foundJaegerFields, field)
		}
	}

	if c.config.Verbose {
		logger.Tracef(" Jaeger/OpenTelemetry field detection: found %d/%d required fields\n", jaegerFieldCount, 3)
		if len(foundJaegerFields) > 0 {
			logger.Tracef(" Found Jaeger/OpenTelemetry fields: %v\n", foundJaegerFields)
		}
	}

	if jaegerFieldCount >= 3 {
		if c.config.Verbose {
			logger.Tracef(" Detected log type: jaeger (by field presence)\n")
		}
		return "jaeger"
	}

	// Default to generic
	if c.config.Verbose {
		logger.Tracef(" No specific patterns or fields matched, defaulting to: generic\n")
	}
	return "generic"
}

// detectTimestampField finds the appropriate timestamp field for the log type
func (c *Client) detectTimestampField(fieldTypes map[string]string, availableFields []string, logType string) (string, bool) {
	fieldMap := make(map[string]bool)
	for _, field := range availableFields {
		fieldMap[field] = true
	}

	if c.config.Verbose {
		logger.Tracef(" Detecting timestamp field for log type: %s\n", logType)
		var dateFields []string
		for field, fieldType := range fieldTypes {
			if fieldType == "date" {
				dateFields = append(dateFields, field)
			}
		}
		logger.Tracef(" Available date fields: %v\n", dateFields)
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
			"startTimeMillis",
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

	if c.config.Verbose {
		logger.Tracef(" Timestamp candidates for %s: %v\n", logType, timestampCandidates)
	}

	// Check each candidate
	for _, candidate := range timestampCandidates {
		if fieldMap[candidate] {
			// Verify field type is date if we have type information
			if fieldType, exists := fieldTypes[candidate]; exists {
				if c.config.Verbose {
					logger.Tracef(" Checking candidate '%s': field type is '%s'\n", candidate, fieldType)
				}
				if fieldType == "date" {
					if c.config.Verbose {
						logger.Tracef(" Selected timestamp field: %s (verified date type)\n", candidate)
					}
					return candidate, true
				}
			} else {
				// If no type info, assume it's valid if field exists
				if c.config.Verbose {
					logger.Tracef(" Selected timestamp field: %s (field exists, no type info)\n", candidate)
				}
				return candidate, true
			}
		} else {
			if c.config.Debug {
				logger.Tracef(" Candidate '%s' not found in available fields\n", candidate)
			}
		}
	}

	if c.config.Verbose {
		logger.Tracef(" No priority timestamp candidates found, checking fallback date fields\n")
	}

	// Check for any date field as fallback
	for field, fieldType := range fieldTypes {
		if fieldType == "date" {
			if c.config.Verbose {
				logger.Tracef(" Using fallback date field: %s\n", field)
			}
			return field, true
		}
	}

	// No date field found, return default based on type
	var defaultField string
	switch logType {
	case "jaeger":
		defaultField = "startTimeMillis"
	default:
		defaultField = "@timestamp"
	}

	if c.config.Verbose {
		logger.Tracef(" No timestamp field found, using default: %s (not verified to exist)\n", defaultField)
	}
	return defaultField, false
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
		return "startTimeMillis"
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
