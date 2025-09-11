package opensearch

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestIsJSONQuery(t *testing.T) {
	tests := []struct {
		name     string
		query    string
		expected bool
	}{
		{
			name:     "empty string",
			query:    "",
			expected: false,
		},
		{
			name:     "wildcard query",
			query:    "*",
			expected: false,
		},
		{
			name:     "lucene query string",
			query:    "level:ERROR",
			expected: false,
		},
		{
			name:     "simple JSON match query",
			query:    `{"match": {"level": "ERROR"}}`,
			expected: true,
		},
		{
			name:     "complex bool query",
			query:    `{"bool": {"must": [{"match": {"service": "api"}}, {"term": {"status": 500}}]}}`,
			expected: true,
		},
		{
			name:     "complete query with aggregations",
			query:    `{"query": {"match_all": {}}, "aggs": {"status_codes": {"terms": {"field": "status"}}}}`,
			expected: true,
		},
		{
			name:     "invalid JSON",
			query:    `{"match": {"level": "ERROR"}`,
			expected: false,
		},
		{
			name:     "query starting with { but not JSON",
			query:    "{this is not json}",
			expected: false,
		},
		{
			name:     "whitespace padded JSON",
			query:    `  {"match": {"level": "ERROR"}}  `,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := IsJSONQuery(tt.query)
			if result != tt.expected {
				t.Errorf("IsJSONQuery(%q) = %v, want %v", tt.query, result, tt.expected)
			}
		})
	}
}

func TestValidateJSONQuery(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		expectError bool
	}{
		{
			name:        "valid match query",
			query:       `{"match": {"level": "ERROR"}}`,
			expectError: false,
		},
		{
			name:        "valid bool query",
			query:       `{"bool": {"must": [{"match": {"service": "api"}}]}}`,
			expectError: false,
		},
		{
			name:        "valid complete query",
			query:       `{"query": {"match": {"level": "ERROR"}}}`,
			expectError: false,
		},
		{
			name:        "valid aggregation query",
			query:       `{"aggs": {"status_codes": {"terms": {"field": "status"}}}}`,
			expectError: false,
		},
		{
			name:        "valid range query",
			query:       `{"range": {"timestamp": {"gte": "now-1h"}}}`,
			expectError: false,
		},
		{
			name:        "valid term query",
			query:       `{"term": {"status": "active"}}`,
			expectError: false,
		},
		{
			name:        "valid query_string query",
			query:       `{"query_string": {"query": "level:ERROR AND service:api"}}`,
			expectError: false,
		},
		{
			name:        "invalid JSON",
			query:       `{"match": "invalid"}`,
			expectError: false, // This is valid JSON, just not a good query structure
		},
		{
			name:        "completely invalid JSON",
			query:       `{invalid json}`,
			expectError: true,
		},
		{
			name:        "unrecognized query type",
			query:       `{"unknown_query_type": {"field": "value"}}`,
			expectError: true,
		},
		{
			name:        "empty object",
			query:       `{}`,
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateJSONQuery(tt.query)
			if tt.expectError && err == nil {
				t.Errorf("ValidateJSONQuery(%q) expected error but got none", tt.query)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateJSONQuery(%q) unexpected error: %v", tt.query, err)
			}
		})
	}
}

func TestMergeJSONWithTimeRange(t *testing.T) {
	tests := []struct {
		name           string
		jsonQuery      string
		timestampField string
		fromTime       string
		toTime         string
		expectError    bool
		validateResult func(string) bool
	}{
		{
			name:           "no time range",
			jsonQuery:      `{"match": {"level": "ERROR"}}`,
			timestampField: "@timestamp",
			fromTime:       "",
			toTime:         "",
			expectError:    false,
			validateResult: func(result string) bool {
				return result == `{"match": {"level": "ERROR"}}`
			},
		},
		{
			name:           "simple query with time range",
			jsonQuery:      `{"match": {"level": "ERROR"}}`,
			timestampField: "@timestamp",
			fromTime:       "now-1h",
			toTime:         "now",
			expectError:    false,
			validateResult: func(result string) bool {
				var query map[string]interface{}
				json.Unmarshal([]byte(result), &query)

				// Should have query.bool.must with both original query and time range
				if queryClause, ok := query["query"].(map[string]interface{}); ok {
					if boolClause, ok := queryClause["bool"].(map[string]interface{}); ok {
						if mustClause, ok := boolClause["must"].([]interface{}); ok {
							return len(mustClause) == 2
						}
					}
				}
				return false
			},
		},
		{
			name:           "complete query with time range",
			jsonQuery:      `{"query": {"match": {"level": "ERROR"}}}`,
			timestampField: "@timestamp",
			fromTime:       "now-1h",
			toTime:         "",
			expectError:    false,
			validateResult: func(result string) bool {
				var query map[string]interface{}
				json.Unmarshal([]byte(result), &query)

				// Should have modified the existing query structure
				return strings.Contains(result, "range") && strings.Contains(result, "@timestamp")
			},
		},
		{
			name:           "bool query with time range",
			jsonQuery:      `{"query": {"bool": {"must": [{"match": {"service": "api"}}]}}}`,
			timestampField: "@timestamp",
			fromTime:       "now-24h",
			toTime:         "now",
			expectError:    false,
			validateResult: func(result string) bool {
				var query map[string]interface{}
				json.Unmarshal([]byte(result), &query)

				// Should add time range to existing must clause
				if queryClause, ok := query["query"].(map[string]interface{}); ok {
					if boolClause, ok := queryClause["bool"].(map[string]interface{}); ok {
						if mustClause, ok := boolClause["must"].([]interface{}); ok {
							return len(mustClause) == 2 // original + time range
						}
					}
				}
				return false
			},
		},
		{
			name:           "invalid JSON",
			jsonQuery:      `{invalid json}`,
			timestampField: "@timestamp",
			fromTime:       "now-1h",
			toTime:         "",
			expectError:    true,
			validateResult: func(result string) bool {
				return true // Not used when expectError is true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := MergeJSONWithTimeRange(tt.jsonQuery, tt.timestampField, tt.fromTime, tt.toTime)

			if tt.expectError && err == nil {
				t.Errorf("MergeJSONWithTimeRange() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("MergeJSONWithTimeRange() unexpected error: %v", err)
			}

			if !tt.expectError && !tt.validateResult(result) {
				t.Errorf("MergeJSONWithTimeRange() result validation failed. Result: %s", result)
			}
		})
	}
}

func TestAddFieldFilteringToJSON(t *testing.T) {
	tests := []struct {
		name        string
		jsonQuery   string
		fields      []string
		expectError bool
	}{
		{
			name:        "no fields",
			jsonQuery:   `{"match": {"level": "ERROR"}}`,
			fields:      []string{},
			expectError: false,
		},
		{
			name:        "add fields to simple query",
			jsonQuery:   `{"match": {"level": "ERROR"}}`,
			fields:      []string{"timestamp", "message", "level"},
			expectError: false,
		},
		{
			name:        "add fields to complex query",
			jsonQuery:   `{"query": {"bool": {"must": [{"match": {"service": "api"}}]}}}`,
			fields:      []string{"@timestamp", "service", "status"},
			expectError: false,
		},
		{
			name:        "invalid JSON",
			jsonQuery:   `{invalid json}`,
			fields:      []string{"field1"},
			expectError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AddFieldFilteringToJSON(tt.jsonQuery, tt.fields)

			if tt.expectError && err == nil {
				t.Errorf("AddFieldFilteringToJSON() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("AddFieldFilteringToJSON() unexpected error: %v", err)
			}

			if !tt.expectError {
				if len(tt.fields) == 0 {
					// Should return original query unchanged
					if result != tt.jsonQuery {
						t.Errorf("AddFieldFilteringToJSON() should return original query when no fields specified")
					}
				} else {
					// Should contain _source field
					if !strings.Contains(result, "_source") {
						t.Errorf("AddFieldFilteringToJSON() result should contain _source field")
					}
				}
			}
		})
	}
}

func TestAddSortingToJSON(t *testing.T) {
	tests := []struct {
		name           string
		jsonQuery      string
		timestampField string
		expectError    bool
	}{
		{
			name:           "add sorting to simple query",
			jsonQuery:      `{"match": {"level": "ERROR"}}`,
			timestampField: "@timestamp",
			expectError:    false,
		},
		{
			name:           "don't override existing sorting",
			jsonQuery:      `{"match": {"level": "ERROR"}, "sort": [{"custom_field": {"order": "asc"}}]}`,
			timestampField: "@timestamp",
			expectError:    false,
		},
		{
			name:           "add sorting to complex query",
			jsonQuery:      `{"query": {"bool": {"must": [{"match": {"service": "api"}}]}}}`,
			timestampField: "startTime",
			expectError:    false,
		},
		{
			name:           "invalid JSON",
			jsonQuery:      `{invalid json}`,
			timestampField: "@timestamp",
			expectError:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AddSortingToJSON(tt.jsonQuery, tt.timestampField)

			if tt.expectError && err == nil {
				t.Errorf("AddSortingToJSON() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("AddSortingToJSON() unexpected error: %v", err)
			}

			if !tt.expectError {
				// Should contain sort field
				if !strings.Contains(result, "sort") {
					t.Errorf("AddSortingToJSON() result should contain sort field")
				}

				// For queries that don't already have sorting, check if timestamp field was added
				if !strings.Contains(tt.jsonQuery, "sort") {
					if !strings.Contains(result, tt.timestampField) {
						t.Errorf("AddSortingToJSON() result should contain timestamp field %s", tt.timestampField)
					}
				}
			}
		})
	}
}

func TestComplexJSONQueryScenarios(t *testing.T) {
	tests := []struct {
		name           string
		jsonQuery      string
		timestampField string
		fromTime       string
		toTime         string
		fields         []string
		expectValid    bool
	}{
		{
			name:           "aggregation query with time range and field filtering",
			jsonQuery:      `{"aggs": {"status_codes": {"terms": {"field": "status"}}}}`,
			timestampField: "@timestamp",
			fromTime:       "now-24h",
			toTime:         "now",
			fields:         []string{"status", "timestamp"},
			expectValid:    true,
		},
		{
			name:           "complex bool query with all features",
			jsonQuery:      `{"bool": {"must": [{"match": {"service": "api"}}, {"term": {"environment": "prod"}}], "must_not": [{"term": {"status": "healthy"}}]}}`,
			timestampField: "startTime",
			fromTime:       "now-1h",
			toTime:         "",
			fields:         []string{"service", "environment", "status", "startTime"},
			expectValid:    true,
		},
		{
			name:           "nested query with time range",
			jsonQuery:      `{"nested": {"path": "logs", "query": {"match": {"logs.level": "ERROR"}}}}`,
			timestampField: "@timestamp",
			fromTime:       "now-6h",
			toTime:         "now",
			fields:         []string{"@timestamp", "logs.level", "logs.message"},
			expectValid:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test the complete workflow
			if !IsJSONQuery(tt.jsonQuery) {
				t.Errorf("IsJSONQuery() should detect query as JSON")
			}

			if err := ValidateJSONQuery(tt.jsonQuery); err != nil {
				t.Errorf("ValidateJSONQuery() failed: %v", err)
			}

			// Merge time range
			result, err := MergeJSONWithTimeRange(tt.jsonQuery, tt.timestampField, tt.fromTime, tt.toTime)
			if err != nil {
				t.Errorf("MergeJSONWithTimeRange() failed: %v", err)
			}

			// Add field filtering
			result, err = AddFieldFilteringToJSON(result, tt.fields)
			if err != nil {
				t.Errorf("AddFieldFilteringToJSON() failed: %v", err)
			}

			// Add sorting
			result, err = AddSortingToJSON(result, tt.timestampField)
			if err != nil {
				t.Errorf("AddSortingToJSON() failed: %v", err)
			}

			// Validate final result is valid JSON
			var finalQuery map[string]interface{}
			if err := json.Unmarshal([]byte(result), &finalQuery); err != nil {
				t.Errorf("Final result is not valid JSON: %v", err)
			}

			// Should contain expected elements
			if len(tt.fields) > 0 && !strings.Contains(result, "_source") {
				t.Errorf("Final result should contain _source field")
			}

			if !strings.Contains(result, "sort") {
				t.Errorf("Final result should contain sort field")
			}

			if tt.fromTime != "" || tt.toTime != "" {
				if !strings.Contains(result, "range") {
					t.Errorf("Final result should contain range query for time filtering")
				}
			}
		})
	}
}

func TestAddFiltersToQuery(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		constraints []FilterConstraint
		expectError bool
		validate    func(string) bool
	}{
		{
			name:        "no constraints",
			query:       `{"match": {"level": "ERROR"}}`,
			constraints: []FilterConstraint{},
			expectError: false,
			validate: func(result string) bool {
				return result == `{"match": {"level": "ERROR"}}`
			},
		},
		{
			name:  "add filters to simple JSON query",
			query: `{"match": {"level": "ERROR"}}`,
			constraints: []FilterConstraint{
				{Field: "kubernetes.namespace", Value: "production"},
				{Field: "kubernetes.pod.name", Value: "api-pod"},
			},
			expectError: false,
			validate: func(result string) bool {
				return strings.Contains(result, "kubernetes.namespace") &&
					strings.Contains(result, "production") &&
					strings.Contains(result, "kubernetes.pod.name") &&
					strings.Contains(result, "api-pod") &&
					strings.Contains(result, "bool") &&
					strings.Contains(result, "must")
			},
		},
		{
			name:  "add filters to complete JSON query",
			query: `{"query": {"match": {"level": "ERROR"}}}`,
			constraints: []FilterConstraint{
				{Field: "serviceName", Value: "user-service"},
			},
			expectError: false,
			validate: func(result string) bool {
				return strings.Contains(result, "serviceName") &&
					strings.Contains(result, "user-service") &&
					strings.Contains(result, "bool") &&
					strings.Contains(result, "must")
			},
		},
		{
			name:  "add filters to existing bool query",
			query: `{"query": {"bool": {"must": [{"match": {"service": "api"}}]}}}`,
			constraints: []FilterConstraint{
				{Field: "kubernetes.namespace", Value: "staging"},
			},
			expectError: false,
			validate: func(result string) bool {
				var query map[string]interface{}
				json.Unmarshal([]byte(result), &query)

				queryClause := query["query"].(map[string]interface{})
				boolClause := queryClause["bool"].(map[string]interface{})
				mustClause := boolClause["must"].([]interface{})

				// Should have original query + filter
				return len(mustClause) >= 2
			},
		},
		{
			name:  "add filters to Lucene query",
			query: "level:ERROR",
			constraints: []FilterConstraint{
				{Field: "kubernetes.namespace", Value: "production"},
				{Field: "serviceName", Value: "api-service"},
			},
			expectError: false,
			validate: func(result string) bool {
				return strings.Contains(result, "(level:ERROR)") &&
					strings.Contains(result, "kubernetes.namespace:production") &&
					strings.Contains(result, "serviceName:\"api-service\"") &&
					strings.Contains(result, "AND")
			},
		},
		{
			name:  "add filters to wildcard query",
			query: "*",
			constraints: []FilterConstraint{
				{Field: "namespace", Value: "test"},
			},
			expectError: false,
			validate: func(result string) bool {
				return result == "namespace:test"
			},
		},
		{
			name:  "escape special characters in filter values",
			query: "*",
			constraints: []FilterConstraint{
				{Field: "message", Value: "Error: failed to connect"},
				{Field: "path", Value: "/app/data/logs"},
			},
			expectError: false,
			validate: func(result string) bool {
				return strings.Contains(result, `message:"Error: failed to connect"`) &&
					strings.Contains(result, `path:"/app/data/logs"`)
			},
		},
		{
			name:        "invalid JSON gets treated as Lucene",
			query:       `{invalid json}`,
			constraints: []FilterConstraint{{Field: "test", Value: "value"}},
			expectError: false,
			validate: func(result string) bool {
				return strings.Contains(result, "({invalid json})") &&
					strings.Contains(result, "test:value")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AddFiltersToQuery(tt.query, tt.constraints)

			if tt.expectError && err == nil {
				t.Errorf("AddFiltersToQuery() expected error but got none")
			}
			if !tt.expectError && err != nil {
				t.Errorf("AddFiltersToQuery() unexpected error: %v", err)
			}

			if !tt.expectError && !tt.validate(result) {
				t.Errorf("AddFiltersToQuery() result validation failed. Result: %s", result)
			}
		})
	}
}

func TestEscapeQueryValue(t *testing.T) {
	tests := []struct {
		name     string
		value    string
		expected string
	}{
		{
			name:     "simple value",
			value:    "production",
			expected: "production",
		},
		{
			name:     "value with spaces",
			value:    "my namespace",
			expected: `"my namespace"`,
		},
		{
			name:     "value with special characters",
			value:    "Error: failed to connect",
			expected: `"Error: failed to connect"`,
		},
		{
			name:     "value with quotes",
			value:    `Message "failed"`,
			expected: `"Message \"failed\""`,
		},
		{
			name:     "value with path characters",
			value:    "/app/data/logs",
			expected: `"/app/data/logs"`,
		},
		{
			name:     "value with Lucene operators",
			value:    "level:ERROR AND status:500",
			expected: `"level:ERROR AND status:500"`,
		},
		{
			name:     "empty value",
			value:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := escapeQueryValue(tt.value)
			if result != tt.expected {
				t.Errorf("escapeQueryValue(%q) = %q, want %q", tt.value, result, tt.expected)
			}
		})
	}
}
