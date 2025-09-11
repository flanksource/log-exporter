package opensearch

import (
	"strings"
	"testing"
)

// TestParseFields is already covered in client_test.go, so we'll focus on completion-specific tests

func TestGetFieldSuggestionsByIndexPattern(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name         string
		indexPattern string
		expected     []string
	}{
		{
			name:         "kubernetes pattern - filebeat",
			indexPattern: "filebeat-2024",
			expected: []string{
				"@timestamp",
				"kubernetes.namespace",
				"kubernetes.pod.name",
				"kubernetes.container.name",
				"kubernetes.node.name",
				"message",
				"level",
				"cloud.provider",
				"agent.type",
			},
		},
		{
			name:         "kubernetes pattern - k8s",
			indexPattern: "k8s-logs-prod",
			expected: []string{
				"@timestamp",
				"kubernetes.namespace",
				"kubernetes.pod.name",
				"kubernetes.container.name",
				"message",
				"level",
			},
		},
		{
			name:         "jaeger pattern - jaeger",
			indexPattern: "jaeger-span-2024",
			expected: []string{
				"traceID",
				"spanID",
				"serviceName",
				"operationName",
				"startTime",
				"duration",
				"span.kind",
				"error",
			},
		},
		{
			name:         "jaeger pattern - trace",
			indexPattern: "trace-data",
			expected: []string{
				"traceID",
				"spanID",
				"serviceName",
				"operationName",
				"duration",
			},
		},
		{
			name:         "generic pattern",
			indexPattern: "application-logs",
			expected: []string{
				"@timestamp",
				"timestamp",
				"message",
				"level",
				"severity",
				"host",
				"source",
				"application",
				"user_id",
				"trace_id",
				"span_id",
			},
		},
		{
			name:         "empty pattern",
			indexPattern: "",
			expected: []string{
				"@timestamp",
				"message",
				"level",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := client.getFieldSuggestionsByIndexPattern(tt.indexPattern)

			// Check that all expected fields are present
			gotMap := make(map[string]bool)
			for _, field := range got {
				gotMap[field] = true
			}

			for _, expected := range tt.expected {
				if !gotMap[expected] {
					t.Errorf("Expected field %s not found in suggestions for pattern %s", expected, tt.indexPattern)
				}
			}
		})
	}
}

func TestKubernetesPatternDetection(t *testing.T) {
	client := &Client{}

	kubernetesPatterns := []string{
		"filebeat-2024-01",
		"kubernetes-logs",
		"k8s-prod-logs",
		"eks-cluster-logs",
		"gke-logs-staging",
		"aks-production",
		"FILEBEAT-DEV", // Test case insensitivity
		"logs-k8s-app",
	}

	for _, pattern := range kubernetesPatterns {
		t.Run(pattern, func(t *testing.T) {
			suggestions := client.getFieldSuggestionsByIndexPattern(pattern)

			// Should contain Kubernetes-specific fields
			expectedK8sFields := []string{
				"kubernetes.namespace",
				"kubernetes.pod.name",
				"kubernetes.container.name",
				"kubernetes.node.name",
			}

			suggestionMap := make(map[string]bool)
			for _, suggestion := range suggestions {
				suggestionMap[suggestion] = true
			}

			for _, k8sField := range expectedK8sFields {
				if !suggestionMap[k8sField] {
					t.Errorf("Expected Kubernetes field %s not found for pattern %s", k8sField, pattern)
				}
			}
		})
	}
}

func TestJaegerPatternDetection(t *testing.T) {
	client := &Client{}

	jaegerPatterns := []string{
		"jaeger-span-2024",
		"jaeger-traces",
		"span-data",
		"trace-logs",
		"otel-traces",
		"apm-data",
		"JAEGER-PROD", // Test case insensitivity
		"logs-span-app",
	}

	for _, pattern := range jaegerPatterns {
		t.Run(pattern, func(t *testing.T) {
			suggestions := client.getFieldSuggestionsByIndexPattern(pattern)

			// Should contain Jaeger-specific fields
			expectedJaegerFields := []string{
				"traceID",
				"spanID",
				"serviceName",
				"operationName",
				"duration",
				"span.kind",
			}

			suggestionMap := make(map[string]bool)
			for _, suggestion := range suggestions {
				suggestionMap[suggestion] = true
			}

			for _, jaegerField := range expectedJaegerFields {
				if !suggestionMap[jaegerField] {
					t.Errorf("Expected Jaeger field %s not found for pattern %s", jaegerField, pattern)
				}
			}
		})
	}
}

func TestExtractFieldsFromMapping(t *testing.T) {
	// Test the helper function with a sample mapping structure
	mapping := map[string]interface{}{
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
							"uid": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
					"labels": map[string]interface{}{
						"properties": map[string]interface{}{
							"app": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
				},
			},
		},
	}

	fieldSet := make(map[string]bool)
	extractFieldsFromMapping(mapping, "", fieldSet)

	expectedFields := []string{
		"@timestamp",
		"message",
		"kubernetes.namespace",
		"kubernetes.pod.name",
		"kubernetes.pod.uid",
		"kubernetes.labels.app",
	}

	// The function also extracts intermediate object names, so we check that all expected fields are present
	// but allow for additional fields (like "kubernetes", "kubernetes.pod", "kubernetes.labels")
	if len(fieldSet) < len(expectedFields) {
		t.Errorf("Expected at least %d fields, got %d", len(expectedFields), len(fieldSet))
	}

	for _, expected := range expectedFields {
		if !fieldSet[expected] {
			t.Errorf("Expected field %s not found in extracted fields", expected)
		}
	}
}

func TestFieldFiltering(t *testing.T) {
	// Test field filtering logic similar to GetFieldCompletion
	allFields := []string{
		"@timestamp",
		"message",
		"level",
		"kubernetes.namespace",
		"kubernetes.pod.name",
		"application.name",
		"trace.id",
		"user.id",
	}

	tests := []struct {
		name       string
		toComplete string
		expected   []string
	}{
		{
			name:       "empty filter",
			toComplete: "",
			expected:   allFields, // All fields should match
		},
		{
			name:       "timestamp filter",
			toComplete: "timestamp",
			expected:   []string{"@timestamp"},
		},
		{
			name:       "kubernetes filter",
			toComplete: "kubernetes",
			expected:   []string{"kubernetes.namespace", "kubernetes.pod.name"},
		},
		{
			name:       "id filter",
			toComplete: "id",
			expected:   []string{"trace.id", "user.id"},
		},
		{
			name:       "partial match",
			toComplete: "app",
			expected:   []string{"application.name"},
		},
		{
			name:       "no match",
			toComplete: "nonexistent",
			expected:   []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var matches []string
			for _, field := range allFields {
				if strings.Contains(field, tt.toComplete) || tt.toComplete == "" {
					matches = append(matches, field)
				}
			}

			if len(matches) != len(tt.expected) {
				t.Errorf("Expected %d matches, got %d", len(tt.expected), len(matches))
				return
			}

			matchMap := make(map[string]bool)
			for _, match := range matches {
				matchMap[match] = true
			}

			for _, expected := range tt.expected {
				if !matchMap[expected] {
					t.Errorf("Expected match %s not found", expected)
				}
			}
		})
	}
}

// Test edge cases and error conditions
func TestEdgeCases(t *testing.T) {
	client := &Client{}

	t.Run("case insensitive pattern matching", func(t *testing.T) {
		patterns := map[string]string{
			"FILEBEAT-PROD": "kubernetes",
			"filebeat-prod": "kubernetes",
			"JAEGER-SPAN":   "jaeger",
			"jaeger-span":   "jaeger",
		}

		for pattern, expectedType := range patterns {
			suggestions := client.getFieldSuggestionsByIndexPattern(pattern)

			var hasExpectedFields bool
			if expectedType == "kubernetes" {
				for _, suggestion := range suggestions {
					if strings.HasPrefix(suggestion, "kubernetes.") {
						hasExpectedFields = true
						break
					}
				}
			} else if expectedType == "jaeger" {
				for _, suggestion := range suggestions {
					if suggestion == "traceID" || suggestion == "spanID" {
						hasExpectedFields = true
						break
					}
				}
			}

			if !hasExpectedFields {
				t.Errorf("Pattern %s should detect %s fields", pattern, expectedType)
			}
		}
	})

	t.Run("multiple pattern matches", func(t *testing.T) {
		// Index pattern that could match both kubernetes and jaeger patterns
		pattern := "k8s-jaeger-logs"
		suggestions := client.getFieldSuggestionsByIndexPattern(pattern)

		// Should prioritize the first match (kubernetes in this case)
		hasKubernetesFields := false
		for _, suggestion := range suggestions {
			if strings.HasPrefix(suggestion, "kubernetes.") {
				hasKubernetesFields = true
				break
			}
		}

		if !hasKubernetesFields {
			t.Error("Expected kubernetes fields to be detected for mixed pattern")
		}
	})

	t.Run("field parsing with special characters", func(t *testing.T) {
		input := "@timestamp,kubernetes.labels.app/version,http.status_code"
		fields := ParseFields(input)

		expected := []string{"@timestamp", "kubernetes.labels.app/version", "http.status_code"}

		if len(fields) != len(expected) {
			t.Errorf("Expected %d fields, got %d", len(expected), len(fields))
		}

		for i, field := range fields {
			if field != expected[i] {
				t.Errorf("Field[%d] = %s, want %s", i, field, expected[i])
			}
		}
	})
}
