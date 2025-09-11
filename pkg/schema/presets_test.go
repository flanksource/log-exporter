package schema

import (
	"testing"

	"github.com/flanksource/clicky/api"
)

func TestGetKubernetesSchema(t *testing.T) {
	schema := GetKubernetesSchema()

	if schema == nil {
		t.Fatal("GetKubernetesSchema() returned nil")
	}

	expectedFields := map[string]bool{
		"@timestamp":                 true,
		"kubernetes.namespace":       true,
		"kubernetes.pod.name":        true,
		"kubernetes.container.name":  true,
		"message":                    true,
		"level":                      true,
		"kubernetes.node.name":       true,
		"kubernetes.deployment.name": true,
		"cloud.provider":             true,
		"cloud.region":               true,
		"agent.type":                 true,
	}

	if len(schema.Fields) != len(expectedFields) {
		t.Errorf("Expected %d fields, got %d", len(expectedFields), len(schema.Fields))
	}

	foundFields := make(map[string]bool)
	for _, field := range schema.Fields {
		foundFields[field.Name] = true

		// Verify field has required properties
		if field.Name == "" {
			t.Error("Field missing name")
		}
		if field.Type == "" {
			t.Error("Field missing type")
		}
		if field.Style == "" {
			t.Error("Field missing style")
		}
	}

	// Check specific field properties
	for _, field := range schema.Fields {
		switch field.Name {
		case "@timestamp":
			if field.Format != "date" {
				t.Errorf("Expected @timestamp to have date format, got %s", field.Format)
			}
		case "level":
			if field.ColorOptions == nil || len(field.ColorOptions) == 0 {
				t.Error("Expected level field to have color options")
			}
			expectedColors := []string{"red", "yellow", "green", "blue", "gray"}
			for _, color := range expectedColors {
				if _, exists := field.ColorOptions[color]; !exists {
					t.Errorf("Expected level field to have %s color option", color)
				}
			}
		case "kubernetes.namespace":
			if field.Label != "Namespace" {
				t.Errorf("Expected namespace field to have label 'Namespace', got %s", field.Label)
			}
		}
	}
}

func TestGetJaegerSchema(t *testing.T) {
	schema := GetJaegerSchema()

	if schema == nil {
		t.Fatal("GetJaegerSchema() returned nil")
	}

	expectedFields := map[string]bool{
		"startTime":        true,
		"traceID":          true,
		"spanID":           true,
		"serviceName":      true,
		"operationName":    true,
		"duration":         true,
		"span.kind":        true,
		"error":            true,
		"http.status_code": true,
		"http.method":      true,
		"http.url":         true,
	}

	if len(schema.Fields) != len(expectedFields) {
		t.Errorf("Expected %d fields, got %d", len(expectedFields), len(schema.Fields))
	}

	// Check specific Jaeger field properties
	for _, field := range schema.Fields {
		switch field.Name {
		case "duration":
			if field.Type != "float" {
				t.Errorf("Expected duration to be float type, got %s", field.Type)
			}
			if field.ColorOptions == nil || len(field.ColorOptions) == 0 {
				t.Error("Expected duration field to have color options for performance thresholds")
			}
		case "error":
			if field.Type != "boolean" {
				t.Errorf("Expected error to be boolean type, got %s", field.Type)
			}
		case "http.status_code":
			if field.Type != "int" {
				t.Errorf("Expected http.status_code to be int type, got %s", field.Type)
			}
		case "startTime":
			if field.Format != "date" {
				t.Errorf("Expected startTime to have date format, got %s", field.Format)
			}
		}
	}
}

func TestGetCombinedSchema(t *testing.T) {
	schema := GetCombinedSchema()

	if schema == nil {
		t.Fatal("GetCombinedSchema() returned nil")
	}

	// Should have fields from both Kubernetes and Jaeger schemas
	expectedKubernetesFields := []string{"@timestamp", "kubernetes.namespace", "kubernetes.pod.name", "kubernetes.container.name", "message", "level"}
	expectedJaegerFields := []string{"traceID", "spanID", "serviceName", "operationName", "duration", "error"}

	fieldNames := make([]string, len(schema.Fields))
	for i, field := range schema.Fields {
		fieldNames[i] = field.Name
	}

	// Check that key fields from both schemas are present
	for _, expectedField := range expectedKubernetesFields {
		found := false
		for _, fieldName := range fieldNames {
			if fieldName == expectedField {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected combined schema to contain Kubernetes field: %s", expectedField)
		}
	}

	for _, expectedField := range expectedJaegerFields {
		found := false
		for _, fieldName := range fieldNames {
			if fieldName == expectedField {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("Expected combined schema to contain Jaeger field: %s", expectedField)
		}
	}

	// First field should be timestamp from Kubernetes
	if len(schema.Fields) > 0 && schema.Fields[0].Name != "@timestamp" {
		t.Errorf("Expected combined schema to start with @timestamp, got %s", schema.Fields[0].Name)
	}
}

func TestGetPresetSchema(t *testing.T) {
	testCases := []struct {
		preset   string
		expected bool
		name     string
	}{
		{"kubernetes", true, "kubernetes preset"},
		{"k8s", true, "k8s alias"},
		{"filebeat", true, "filebeat alias"},
		{"jaeger", true, "jaeger preset"},
		{"traces", true, "traces alias"},
		{"tracing", true, "tracing alias"},
		{"combined", true, "combined preset"},
		{"both", true, "both alias"},
		{"unknown", false, "unknown preset"},
		{"", false, "empty preset"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			schema, found := GetPresetSchema(tc.preset)

			if found != tc.expected {
				t.Errorf("Expected GetPresetSchema(%s) found=%v, got found=%v", tc.preset, tc.expected, found)
			}

			if tc.expected {
				if schema == nil {
					t.Errorf("Expected valid schema for preset %s, got nil", tc.preset)
				}
				if len(schema.Fields) == 0 {
					t.Errorf("Expected schema for preset %s to have fields", tc.preset)
				}
			} else {
				if schema != nil {
					t.Errorf("Expected nil schema for unknown preset %s, got valid schema", tc.preset)
				}
			}
		})
	}
}

func TestGetKubernetesFieldSuggestions(t *testing.T) {
	suggestions := GetKubernetesFieldSuggestions()

	if len(suggestions) == 0 {
		t.Fatal("GetKubernetesFieldSuggestions() returned empty slice")
	}

	expectedFields := []string{
		"@timestamp",
		"kubernetes.namespace",
		"kubernetes.pod.name",
		"kubernetes.container.name",
		"kubernetes.node.name",
		"message",
		"level",
		"cloud.provider",
		"agent.type",
	}

	suggestionMap := make(map[string]bool)
	for _, suggestion := range suggestions {
		suggestionMap[suggestion] = true
	}

	for _, expected := range expectedFields {
		if !suggestionMap[expected] {
			t.Errorf("Expected Kubernetes field suggestions to contain: %s", expected)
		}
	}
}

func TestGetJaegerFieldSuggestions(t *testing.T) {
	suggestions := GetJaegerFieldSuggestions()

	if len(suggestions) == 0 {
		t.Fatal("GetJaegerFieldSuggestions() returned empty slice")
	}

	expectedFields := []string{
		"traceID",
		"spanID",
		"serviceName",
		"operationName",
		"duration",
		"span.kind",
		"error",
		"http.status_code",
		"http.method",
		"http.url",
	}

	suggestionMap := make(map[string]bool)
	for _, suggestion := range suggestions {
		suggestionMap[suggestion] = true
	}

	for _, expected := range expectedFields {
		if !suggestionMap[expected] {
			t.Errorf("Expected Jaeger field suggestions to contain: %s", expected)
		}
	}
}

// Test that all schemas have valid structure
func TestSchemaStructure(t *testing.T) {
	schemas := map[string]*api.PrettyObject{
		"kubernetes": GetKubernetesSchema(),
		"jaeger":     GetJaegerSchema(),
		"combined":   GetCombinedSchema(),
	}

	for name, schema := range schemas {
		t.Run(name, func(t *testing.T) {
			if schema == nil {
				t.Fatalf("Schema %s is nil", name)
			}

			if len(schema.Fields) == 0 {
				t.Errorf("Schema %s has no fields", name)
			}

			for i, field := range schema.Fields {
				if field.Name == "" {
					t.Errorf("Schema %s field %d has empty name", name, i)
				}

				if field.Type == "" {
					t.Errorf("Schema %s field %s has empty type", name, field.Name)
				}

				// Type should be one of the valid types
				validTypes := map[string]bool{
					"string":  true,
					"int":     true,
					"float":   true,
					"boolean": true,
					"struct":  true,
					"array":   true,
				}

				if !validTypes[field.Type] {
					t.Errorf("Schema %s field %s has invalid type: %s", name, field.Name, field.Type)
				}
			}
		})
	}
}
