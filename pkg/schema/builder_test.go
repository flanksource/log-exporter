package schema

import (
	"testing"
)

func TestMapElasticsearchType(t *testing.T) {
	tests := []struct {
		name   string
		esType string
		want   string
	}{
		{"text", "text", "string"},
		{"keyword", "keyword", "string"},
		{"long", "long", "int"},
		{"integer", "integer", "int"},
		{"double", "double", "float"},
		{"float", "float", "float"},
		{"boolean", "boolean", "boolean"},
		{"date", "date", "string"},
		{"object", "object", "struct"},
		{"nested", "nested", "array"},
		{"unknown", "unknown_type", "string"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := mapElasticsearchType(tt.esType)
			if got != tt.want {
				t.Errorf("mapElasticsearchType() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildFieldFromMapping(t *testing.T) {
	builder := &Builder{}

	tests := []struct {
		name      string
		fieldName string
		esType    string
		wantType  string
		wantStyle string
	}{
		{
			name:      "timestamp field",
			fieldName: "@timestamp",
			esType:    "date",
			wantType:  "string",
			wantStyle: "text-gray-500 text-sm font-mono",
		},
		{
			name:      "text field",
			fieldName: "message",
			esType:    "text",
			wantType:  "string",
			wantStyle: "text-gray-700",
		},
		{
			name:      "keyword field",
			fieldName: "severity",
			esType:    "keyword",
			wantType:  "string",
			wantStyle: "font-bold uppercase text-xs px-2 py-1 rounded-md",
		},
		{
			name:      "integer field",
			fieldName: "count",
			esType:    "integer",
			wantType:  "int",
			wantStyle: "text-center font-medium",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.buildFieldFromMapping(tt.fieldName, tt.esType)

			if got.Name != tt.fieldName {
				t.Errorf("Name = %v, want %v", got.Name, tt.fieldName)
			}
			if got.Type != tt.wantType {
				t.Errorf("Type = %v, want %v", got.Type, tt.wantType)
			}
			if got.Style != tt.wantStyle {
				t.Errorf("Style = %v, want %v", got.Style, tt.wantStyle)
			}
		})
	}
}

func TestExtractFieldTypes(t *testing.T) {
	mapping := map[string]interface{}{
		"test-index": map[string]interface{}{
			"mappings": map[string]interface{}{
				"properties": map[string]interface{}{
					"@timestamp": map[string]interface{}{
						"type": "date",
					},
					"message": map[string]interface{}{
						"type": "text",
					},
					"severity": map[string]interface{}{
						"type": "keyword",
					},
					"nested_field": map[string]interface{}{
						"type": "object",
						"properties": map[string]interface{}{
							"sub_field": map[string]interface{}{
								"type": "keyword",
							},
						},
					},
				},
			},
		},
	}

	fieldTypes := extractFieldTypes(mapping)

	tests := []struct {
		field    string
		wantType string
	}{
		{"@timestamp", "date"},
		{"message", "text"},
		{"severity", "keyword"},
		{"nested_field.sub_field", "keyword"},
	}

	for _, tt := range tests {
		t.Run(tt.field, func(t *testing.T) {
			got, exists := fieldTypes[tt.field]
			if !exists {
				t.Errorf("Field %s not found in extracted types", tt.field)
				return
			}
			if got != tt.wantType {
				t.Errorf("Field %s type = %v, want %v", tt.field, got, tt.wantType)
			}
		})
	}
}

func TestIsJaegerField(t *testing.T) {
	builder := &Builder{}

	tests := []struct {
		name      string
		fieldName string
		expected  bool
	}{
		// Jaeger core fields
		{"trace ID", "traceID", true},
		{"span ID", "spanID", true},
		{"parent span ID", "parentSpanID", true},
		{"operation name", "operationName", true},
		{"service name", "serviceName", true},
		{"start time", "startTime", true},
		{"duration", "duration", true},
		{"span kind", "span.kind", true},
		{"error", "error", true},

		// HTTP fields
		{"http status", "http.status_code", true},
		{"http method", "http.method", true},
		{"http url", "http.url", true},

		// Database fields
		{"db type", "db.type", true},
		{"db statement", "db.statement", true},

		// Tag fields
		{"tags component", "tags.component", true},
		{"process tags", "process.tags.hostname", true},
		{"logs fields", "logs[0].timestamp", true},
		{"references", "references[0].traceID", true},

		// RPC fields
		{"rpc service", "rpc.service", true},
		{"rpc method", "rpc.method", true},

		// Non-Jaeger fields
		{"kubernetes namespace", "kubernetes.namespace", false},
		{"message", "message", false},
		{"level", "level", false},
		{"host", "host", false},
		{"timestamp", "@timestamp", false},
		{"application", "application", false},
		{"user id", "user_id", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := builder.isJaegerField(tt.fieldName)
			if got != tt.expected {
				t.Errorf("isJaegerField(%s) = %v, want %v", tt.fieldName, got, tt.expected)
			}
		})
	}
}

func TestBuildKubernetesFieldFromMapping(t *testing.T) {
	builder := &Builder{}

	tests := []struct {
		name          string
		fieldName     string
		esType        string
		expectedLabel string
	}{
		{"namespace", "kubernetes.namespace", "keyword", "Namespace"},
		{"pod name", "kubernetes.pod.name", "keyword", "Pod"},
		{"container name", "kubernetes.container.name", "keyword", "Container"},
		{"node name", "kubernetes.node.name", "keyword", "Node"},
		{"deployment", "kubernetes.deployment.name", "keyword", "Deployment"},
		{"label", "kubernetes.labels.app", "keyword", "app"},
		{"annotation", "kubernetes.annotations.deployment.kubernetes.io/revision", "keyword", "deployment.kubernetes.io/revision"},
		{"cloud provider", "cloud.provider", "keyword", "Provider"},
		{"agent type", "agent.type", "keyword", "Agent"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := builder.buildKubernetesFieldFromMapping(tt.fieldName, tt.esType)

			if field.Name != tt.fieldName {
				t.Errorf("Name = %v, want %v", field.Name, tt.fieldName)
			}

			if field.Label != tt.expectedLabel {
				t.Errorf("Label = %v, want %v", field.Label, tt.expectedLabel)
			}

			if field.Style == "" {
				t.Error("Style should not be empty")
			}
		})
	}
}

func TestBuildJaegerFieldFromMapping(t *testing.T) {
	builder := &Builder{}

	tests := []struct {
		name          string
		fieldName     string
		esType        string
		expectedLabel string
		expectedType  string
		hasColors     bool
	}{
		{"trace ID", "traceID", "keyword", "Trace ID", "string", false},
		{"span ID", "spanID", "keyword", "Span ID", "string", false},
		{"service name", "serviceName", "keyword", "Service", "string", false},
		{"duration", "duration", "long", "Duration (μs)", "float", true},
		{"error", "error", "boolean", "Error", "boolean", true},
		{"http status", "http.status_code", "integer", "Status", "int", true},
		{"span kind", "span.kind", "keyword", "Kind", "string", true},
		{"tag", "tags.component", "keyword", "component", "string", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field := builder.buildJaegerFieldFromMapping(tt.fieldName, tt.esType)

			if field.Name != tt.fieldName {
				t.Errorf("Name = %v, want %v", field.Name, tt.fieldName)
			}

			if field.Label != tt.expectedLabel {
				t.Errorf("Label = %v, want %v", field.Label, tt.expectedLabel)
			}

			if field.Type != tt.expectedType {
				t.Errorf("Type = %v, want %v", field.Type, tt.expectedType)
			}

			if tt.hasColors && (field.ColorOptions == nil || len(field.ColorOptions) == 0) {
				t.Errorf("Expected field %s to have color options", tt.fieldName)
			}

			if field.Style == "" {
				t.Error("Style should not be empty")
			}
		})
	}
}
