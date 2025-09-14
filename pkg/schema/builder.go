package schema

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/flanksource/clicky/api"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
)

type Builder struct {
	client *opensearch.Client
}

func NewBuilder(client *opensearch.Client) *Builder {
	return &Builder{client: client}
}

// BuildSchemaFromMapping creates a clicky schema from OpenSearch field mappings
func (b *Builder) BuildSchemaFromMapping(indexPattern string, fields []string) (*api.PrettyObject, error) {
	mapping, err := b.getIndexMapping(indexPattern)
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping: %w", err)
	}

	fieldTypes := extractFieldTypes(mapping)

	schema := &api.PrettyObject{
		Fields: []api.PrettyField{},
	}

	// Determine which fields to include
	fieldsToInclude := fields
	if len(fieldsToInclude) == 0 {
		// Include all mapped fields
		for field := range fieldTypes {
			fieldsToInclude = append(fieldsToInclude, field)
		}
	}

	// Generate field definitions based on mapping types
	for _, fieldName := range fieldsToInclude {
		esType := fieldTypes[fieldName]
		field := b.buildFieldFromMapping(fieldName, esType)
		schema.Fields = append(schema.Fields, field)
	}

	return schema, nil
}

func (b *Builder) getIndexMapping(indexPattern string) (map[string]interface{}, error) {
	res, err := b.client.Indices.GetMapping(
		b.client.Indices.GetMapping.WithIndex(indexPattern),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get mapping: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("mapping request failed with status: %s", res.Status())
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var mapping map[string]interface{}
	if err := json.Unmarshal(body, &mapping); err != nil {
		return nil, fmt.Errorf("failed to unmarshal mapping: %w", err)
	}

	return mapping, nil
}

func extractFieldTypes(mapping map[string]interface{}) map[string]string {
	fieldTypes := make(map[string]string)

	// Extract field types from all indices in the mapping
	for _, indexData := range mapping {
		if indexMap, ok := indexData.(map[string]interface{}); ok {
			if mappings, ok := indexMap["mappings"].(map[string]interface{}); ok {
				extractTypesFromProperties(mappings, "", fieldTypes)
			}
		}
	}

	return fieldTypes
}

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

func (b *Builder) buildFieldFromMapping(fieldName, esType string) api.PrettyField {
	field := api.PrettyField{
		Name: fieldName,
		Type: mapElasticsearchType(esType),
	}

	// Check for Kubernetes/Filebeat fields first
	if strings.HasPrefix(fieldName, "kubernetes.") {
		return b.buildKubernetesFieldFromMapping(fieldName, esType)
	}

	// Check for Jaeger trace fields
	if b.isJaegerField(fieldName) {
		return b.buildJaegerFieldFromMapping(fieldName, esType)
	}

	// Apply field-specific styling and formatting
	switch {
	case fieldName == "timestamp" || fieldName == "@timestamp":
		field.Format = "date"
		field.Style = "text-gray-500 text-sm font-mono"
		field.DateFormat = "2006-01-02 15:04:05"

	case fieldName == "severity" || fieldName == "level" || strings.Contains(fieldName, "level"):
		field.Style = "font-bold uppercase text-xs px-2 py-1 rounded-md"
		field.ColorOptions = map[string]string{
			"red":    "ERROR|FATAL|CRITICAL",
			"yellow": "WARN|WARNING",
			"green":  "INFO|INFORMATION|SUCCESS",
			"blue":   "DEBUG|TRACE",
			"gray":   "UNKNOWN",
		}

	case fieldName == "message" || fieldName == "msg":
		field.Style = "text-gray-700"
		if esType == "text" {
			field.Type = "string"
		}

	case fieldName == "host" || fieldName == "hostname" || strings.Contains(fieldName, "host"):
		field.Style = "text-blue-600 font-medium"

	case fieldName == "source" || fieldName == "application" || strings.Contains(fieldName, "app"):
		field.Style = "text-indigo-600 font-medium"

	case strings.Contains(fieldName, "id") || strings.Contains(fieldName, "uuid"):
		field.Style = "text-gray-500 font-mono text-xs"

	case strings.Contains(fieldName, "count") || strings.Contains(fieldName, "number"):
		if field.Type == "int" {
			field.Style = "text-center font-medium"
			field.ColorOptions = map[string]string{
				"red":    "> 1000",
				"yellow": "> 100",
				"green":  "<= 100",
			}
		}

	case strings.Contains(fieldName, "duration") || strings.Contains(fieldName, "time"):
		if field.Type == "int" || field.Type == "float" {
			field.Style = "text-center font-mono text-sm"
			field.ColorOptions = map[string]string{
				"red":    "> 5000",
				"yellow": "> 1000",
				"green":  "<= 1000",
			}
		}

	case strings.Contains(fieldName, "status"):
		field.Style = "font-semibold uppercase text-sm px-2 py-1 rounded"
		field.ColorOptions = map[string]string{
			"green":  "SUCCESS|OK|COMPLETED|ACTIVE",
			"yellow": "PENDING|PROCESSING|WARNING",
			"red":    "ERROR|FAILED|INACTIVE|CRITICAL",
			"blue":   "RUNNING|IN_PROGRESS",
		}

	case strings.Contains(fieldName, "error") || strings.Contains(fieldName, "exception"):
		field.Style = "text-red-600 font-medium"

	case strings.Contains(fieldName, "url") || strings.Contains(fieldName, "uri"):
		field.Style = "text-blue-500 underline font-mono text-sm"

	case strings.Contains(fieldName, "ip") || strings.Contains(fieldName, "address"):
		field.Style = "text-purple-600 font-mono text-sm"

	case esType == "date":
		field.Format = "date"
		field.Style = "text-gray-500 text-sm"
		field.DateFormat = "2006-01-02 15:04:05"

	case esType == "boolean":
		field.Style = "font-semibold"
		field.ColorOptions = map[string]string{
			"green": "true",
			"gray":  "false",
		}

	default:
		field.Style = "text-gray-600"
	}

	return field
}

// buildKubernetesFieldFromMapping creates a field schema optimized for Kubernetes/Filebeat fields
func (b *Builder) buildKubernetesFieldFromMapping(fieldName, esType string) api.PrettyField {
	field := api.PrettyField{
		Name: fieldName,
		Type: mapElasticsearchType(esType),
	}

	switch {
	case strings.Contains(fieldName, "namespace"):
		field.Style = "bg-purple-100 text-purple-800 px-2 py-1 rounded font-medium text-sm"
		field.Label = "Namespace"

	case strings.Contains(fieldName, "pod.name"):
		field.Style = "text-blue-600 font-mono text-sm"
		field.Label = "Pod"

	case strings.Contains(fieldName, "pod.uid"):
		field.Style = "text-gray-400 font-mono text-xs"
		field.Label = "Pod UID"

	case strings.Contains(fieldName, "container.name"):
		field.Style = "text-indigo-600 font-medium"
		field.Label = "Container"

	case strings.Contains(fieldName, "container.image"):
		field.Style = "text-cyan-600 font-mono text-sm"
		field.Label = "Image"

	case strings.Contains(fieldName, "node.name"):
		field.Style = "text-gray-600 font-mono text-sm"
		field.Label = "Node"

	case strings.Contains(fieldName, "deployment.name"):
		field.Style = "text-emerald-600 font-semibold"
		field.Label = "Deployment"

	case strings.HasPrefix(fieldName, "kubernetes.labels."):
		field.Style = "text-amber-600 text-xs"
		field.Label = strings.TrimPrefix(fieldName, "kubernetes.labels.")

	case strings.HasPrefix(fieldName, "kubernetes.annotations."):
		field.Style = "text-slate-500 text-xs"
		field.Label = strings.TrimPrefix(fieldName, "kubernetes.annotations.")

	case strings.Contains(fieldName, "container.id"):
		field.Style = "text-gray-400 font-mono text-xs"
		field.Label = "Container ID"

	case strings.Contains(fieldName, "container.runtime"):
		field.Style = "text-blue-500 text-sm"
		field.Label = "Runtime"

	case strings.HasPrefix(fieldName, "cloud."):
		field.Style = "text-purple-600 text-sm"
		cloudField := strings.TrimPrefix(fieldName, "cloud.")
		field.Label = strings.Title(strings.ReplaceAll(cloudField, "_", " "))

	case strings.Contains(fieldName, "agent.hostname"):
		field.Style = "text-gray-600 font-mono text-sm"
		field.Label = "Agent Host"

	case strings.Contains(fieldName, "agent.type"):
		field.Style = "text-blue-500 text-sm font-medium"
		field.Label = "Agent"

	case strings.Contains(fieldName, "agent.version"):
		field.Style = "text-gray-500 text-xs"
		field.Label = "Version"

	case fieldName == "input.type":
		field.Style = "text-green-600 text-sm"
		field.Label = "Input Type"

	case fieldName == "ecs.version":
		field.Style = "text-gray-500 text-xs"
		field.Label = "ECS Version"

	default:
		// Generic Kubernetes field styling
		field.Style = "text-blue-600"
		// Try to create a readable label from the field name
		parts := strings.Split(strings.TrimPrefix(fieldName, "kubernetes."), ".")
		if len(parts) > 0 {
			field.Label = strings.Title(strings.ReplaceAll(parts[len(parts)-1], "_", " "))
		}
	}

	return field
}

// isJaegerField checks if a field name belongs to Jaeger trace data
func (b *Builder) isJaegerField(fieldName string) bool {
	jaegerFields := []string{
		"traceID", "spanID", "parentSpanID", "operationName", "serviceName",
		"startTimeMillis", "startTime", "duration", "span.kind", "error",
	}

	for _, jf := range jaegerFields {
		if strings.Contains(fieldName, jf) {
			return true
		}
	}

	// Check for common Jaeger field prefixes
	jaegerPrefixes := []string{
		"tags.", "process.tags.", "logs[", "references[", "http.", "db.", "rpc.",
	}

	for _, prefix := range jaegerPrefixes {
		if strings.HasPrefix(fieldName, prefix) {
			return true
		}
	}

	return false
}

// buildJaegerFieldFromMapping creates a field schema optimized for Jaeger trace fields
func (b *Builder) buildJaegerFieldFromMapping(fieldName, esType string) api.PrettyField {
	field := api.PrettyField{
		Name: fieldName,
		Type: mapElasticsearchType(esType),
	}

	switch {
	case strings.Contains(fieldName, "traceID"):
		field.Style = "text-purple-600 font-mono text-xs"
		field.Label = "Trace ID"

	case strings.Contains(fieldName, "spanID"):
		field.Style = "text-blue-600 font-mono text-xs"
		field.Label = "Span ID"

	case strings.Contains(fieldName, "parentSpanID"):
		field.Style = "text-gray-500 font-mono text-xs"
		field.Label = "Parent ID"

	case strings.Contains(fieldName, "serviceName"):
		field.Style = "text-blue-700 font-bold"
		field.Label = "Service"

	case strings.Contains(fieldName, "operationName"):
		field.Style = "text-indigo-600 font-medium"
		field.Label = "Operation"

	case fieldName == "duration":
		field.Type = "float"
		field.Style = "text-right font-mono"
		field.Label = "Duration (μs)"
		field.ColorOptions = map[string]string{
			"green":  "< 100000",   // < 100ms
			"yellow": "< 500000",   // < 500ms
			"orange": "< 1000000",  // < 1s
			"red":    ">= 1000000", // >= 1s
		}

	case fieldName == "startTimeMillis" || fieldName == "startTime":
		field.Format = "date"
		field.Style = "text-gray-500 text-sm font-mono"
		field.DateFormat = "15:04:05.000"
		field.Label = "Start Time"

	case strings.Contains(fieldName, "span.kind"):
		field.Style = "bg-gray-100 text-gray-800 px-2 py-1 rounded text-xs font-medium uppercase"
		field.Label = "Kind"
		field.ColorOptions = map[string]string{
			"blue":   "CLIENT",
			"green":  "SERVER",
			"yellow": "PRODUCER",
			"orange": "CONSUMER",
			"purple": "INTERNAL",
		}

	case fieldName == "error" || strings.Contains(fieldName, "error"):
		field.Type = "boolean"
		field.Style = "font-bold"
		field.Label = "Error"
		field.ColorOptions = map[string]string{
			"red":   "true",
			"green": "false",
		}

	case strings.Contains(fieldName, "http.status_code"):
		field.Type = "int"
		field.Style = "font-mono text-center"
		field.Label = "Status"
		field.ColorOptions = map[string]string{
			"green":  "200-299",
			"yellow": "300-399",
			"orange": "400-499",
			"red":    "500-599",
		}

	case strings.Contains(fieldName, "http.method"):
		field.Style = "bg-blue-100 text-blue-800 px-2 py-1 rounded text-xs font-bold uppercase"
		field.Label = "Method"

	case strings.Contains(fieldName, "http.url"):
		field.Style = "text-blue-500 underline font-mono text-sm"
		field.Label = "URL"

	case strings.HasPrefix(fieldName, "tags."):
		field.Style = "text-amber-600 text-sm"
		field.Label = strings.TrimPrefix(fieldName, "tags.")

	case strings.HasPrefix(fieldName, "process.tags."):
		field.Style = "text-green-600 text-sm"
		field.Label = strings.TrimPrefix(fieldName, "process.tags.")

	case strings.Contains(fieldName, "db.statement"):
		field.Style = "text-gray-700 font-mono text-sm"
		field.Label = "DB Query"

	case strings.Contains(fieldName, "db.type"):
		field.Style = "text-purple-600 text-sm font-medium"
		field.Label = "DB Type"

	case strings.HasPrefix(fieldName, "rpc."):
		field.Style = "text-cyan-600 text-sm"
		rpcField := strings.TrimPrefix(fieldName, "rpc.")
		field.Label = "RPC " + strings.Title(strings.ReplaceAll(rpcField, "_", " "))

	default:
		// Generic Jaeger field styling
		field.Style = "text-gray-600"
		// Create readable label
		field.Label = strings.Title(strings.ReplaceAll(fieldName, "_", " "))
	}

	return field
}

func mapElasticsearchType(esType string) string {
	switch esType {
	case "text", "keyword":
		return "string"
	case "long", "integer", "short", "byte":
		return "int"
	case "double", "float", "half_float", "scaled_float":
		return "float"
	case "boolean":
		return "boolean"
	case "date":
		return "string" // We'll format it as date
	case "object":
		return "struct"
	case "nested":
		return "array"
	default:
		return "string"
	}
}
