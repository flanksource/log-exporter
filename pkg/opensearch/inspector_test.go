package opensearch

import (
	"strings"
	"testing"
)

func TestDetectLogType(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name            string
		indexPattern    string
		availableFields []string
		expected        string
	}{
		{
			name:            "kubernetes index pattern",
			indexPattern:    "filebeat-2024-01",
			availableFields: []string{"@timestamp", "message", "level"},
			expected:        "kubernetes",
		},
		{
			name:            "kubernetes fields",
			indexPattern:    "app-logs",
			availableFields: []string{"@timestamp", "kubernetes.namespace", "kubernetes.pod.name", "kubernetes.container.name"},
			expected:        "kubernetes",
		},
		{
			name:            "jaeger index pattern",
			indexPattern:    "jaeger-span-2024",
			availableFields: []string{"startTime", "message"},
			expected:        "jaeger",
		},
		{
			name:            "jaeger fields",
			indexPattern:    "app-traces",
			availableFields: []string{"startTime", "traceID", "spanID", "serviceName", "operationName"},
			expected:        "jaeger",
		},
		{
			name:            "generic logs",
			indexPattern:    "application-logs",
			availableFields: []string{"@timestamp", "message", "level", "host"},
			expected:        "generic",
		},
		{
			name:            "empty fields",
			indexPattern:    "unknown-logs",
			availableFields: []string{},
			expected:        "generic",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := client.detectLogType(tt.indexPattern, tt.availableFields)
			if result != tt.expected {
				t.Errorf("detectLogType() = %s, want %s", result, tt.expected)
			}
		})
	}
}

func TestDetectTimestampField(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name            string
		fieldTypes      map[string]string
		availableFields []string
		logType         string
		expectedField   string
		expectedHasDate bool
	}{
		{
			name: "kubernetes with @timestamp",
			fieldTypes: map[string]string{
				"@timestamp": "date",
				"message":    "text",
			},
			availableFields: []string{"@timestamp", "message", "level"},
			logType:         "kubernetes",
			expectedField:   "@timestamp",
			expectedHasDate: true,
		},
		{
			name: "jaeger with startTimeMillis",
			fieldTypes: map[string]string{
				"startTimeMillis": "date",
				"traceID":         "keyword",
			},
			availableFields: []string{"startTimeMillis", "traceID", "spanID"},
			logType:         "jaeger",
			expectedField:   "startTimeMillis",
			expectedHasDate: true,
		},
		{
			name: "jaeger with startTime",
			fieldTypes: map[string]string{
				"startTime": "date",
				"traceID":   "keyword",
			},
			availableFields: []string{"startTime", "traceID", "spanID"},
			logType:         "jaeger",
			expectedField:   "startTime",
			expectedHasDate: true,
		},
		{
			name: "generic with timestamp",
			fieldTypes: map[string]string{
				"timestamp": "date",
				"message":   "text",
			},
			availableFields: []string{"timestamp", "message", "level"},
			logType:         "generic",
			expectedField:   "timestamp",
			expectedHasDate: true,
		},
		{
			name:            "no date field",
			fieldTypes:      map[string]string{"message": "text"},
			availableFields: []string{"message", "level"},
			logType:         "generic",
			expectedField:   "@timestamp",
			expectedHasDate: false,
		},
		{
			name:            "jaeger fallback",
			fieldTypes:      map[string]string{"message": "text"},
			availableFields: []string{"message", "level"},
			logType:         "jaeger",
			expectedField:   "startTimeMillis",
			expectedHasDate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, hasDate := client.detectTimestampField(tt.fieldTypes, tt.availableFields, tt.logType)

			if field != tt.expectedField {
				t.Errorf("detectTimestampField() field = %s, want %s", field, tt.expectedField)
			}

			if hasDate != tt.expectedHasDate {
				t.Errorf("detectTimestampField() hasDate = %t, want %t", hasDate, tt.expectedHasDate)
			}
		})
	}
}

func TestGetRecommendedTimestampField(t *testing.T) {
	tests := []struct {
		logType  string
		expected string
	}{
		{"kubernetes", "@timestamp"},
		{"jaeger", "startTimeMillis"},
		{"generic", "@timestamp"},
		{"unknown", "@timestamp"},
	}

	for _, tt := range tests {
		t.Run(tt.logType, func(t *testing.T) {
			result := GetRecommendedTimestampField(tt.logType)
			if result != tt.expected {
				t.Errorf("GetRecommendedTimestampField(%s) = %s, want %s", tt.logType, result, tt.expected)
			}
		})
	}
}

func TestIndexInfoValidation(t *testing.T) {
	tests := []struct {
		name           string
		info           IndexInfo
		expectWarnings int
	}{
		{
			name: "valid index",
			info: IndexInfo{
				Type:            "kubernetes",
				TimestampField:  "@timestamp",
				AvailableFields: []string{"@timestamp", "message", "level"},
				HasDateField:    true,
				IndexPattern:    "logs-*",
			},
			expectWarnings: 0,
		},
		{
			name: "no date field",
			info: IndexInfo{
				Type:            "generic",
				TimestampField:  "@timestamp",
				AvailableFields: []string{"message", "level"},
				HasDateField:    false,
				IndexPattern:    "logs-*",
			},
			expectWarnings: 1,
		},
		{
			name: "no timestamp field",
			info: IndexInfo{
				Type:            "generic",
				TimestampField:  "",
				AvailableFields: []string{"message", "level"},
				HasDateField:    true,
				IndexPattern:    "logs-*",
			},
			expectWarnings: 1,
		},
		{
			name: "no fields",
			info: IndexInfo{
				Type:            "generic",
				TimestampField:  "@timestamp",
				AvailableFields: []string{},
				HasDateField:    true,
				IndexPattern:    "logs-*",
			},
			expectWarnings: 1,
		},
		{
			name: "multiple issues",
			info: IndexInfo{
				Type:            "generic",
				TimestampField:  "",
				AvailableFields: []string{},
				HasDateField:    false,
				IndexPattern:    "logs-*",
			},
			expectWarnings: 3,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			warnings := tt.info.ValidateIndexInfo()

			if len(warnings) != tt.expectWarnings {
				t.Errorf("ValidateIndexInfo() returned %d warnings, want %d", len(warnings), tt.expectWarnings)
				for i, warning := range warnings {
					t.Logf("Warning %d: %s", i+1, warning)
				}
			}
		})
	}
}

func TestIndexInfoString(t *testing.T) {
	info := IndexInfo{
		Type:            "kubernetes",
		TimestampField:  "@timestamp",
		AvailableFields: []string{"@timestamp", "message", "level"},
		HasDateField:    true,
		IndexPattern:    "filebeat-*",
	}

	result := info.String()

	// Check that key information is present
	expectedStrings := []string{
		"filebeat-*",
		"kubernetes",
		"@timestamp",
		"true",
		"3",
	}

	for _, expected := range expectedStrings {
		if !strings.Contains(result, expected) {
			t.Errorf("String() output missing expected string '%s'", expected)
		}
	}
}

func TestExtractTypesFromProperties(t *testing.T) {
	properties := map[string]interface{}{
		"properties": map[string]interface{}{
			"@timestamp": map[string]interface{}{
				"type": "date",
			},
			"message": map[string]interface{}{
				"type": "text",
			},
			"kubernetes": map[string]interface{}{
				"properties": map[string]interface{}{
					"namespace": map[string]interface{}{
						"type": "keyword",
					},
					"pod": map[string]interface{}{
						"properties": map[string]interface{}{
							"name": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
				},
			},
		},
	}

	fieldTypes := make(map[string]string)
	extractTypesFromProperties(properties, "", fieldTypes)

	expected := map[string]string{
		"@timestamp":           "date",
		"message":              "text",
		"kubernetes.namespace": "keyword",
		"kubernetes.pod.name":  "keyword",
	}

	if len(fieldTypes) != len(expected) {
		t.Errorf("Expected %d field types, got %d", len(expected), len(fieldTypes))
	}

	for field, expectedType := range expected {
		if actualType, exists := fieldTypes[field]; !exists {
			t.Errorf("Expected field %s not found", field)
		} else if actualType != expectedType {
			t.Errorf("Field %s: expected type %s, got %s", field, expectedType, actualType)
		}
	}
}
