package opensearch

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// ListIndices retrieves all available indices from OpenSearch
func (c *Client) ListIndices() ([]string, error) {
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}

	res, err := client.Cat.Indices(
		client.Cat.Indices.WithFormat("json"),
		client.Cat.Indices.WithH("index"),
		client.Cat.Indices.WithS("index"),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to list indices: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("failed to list indices, status: %s", res.Status())
	}

	body, err := io.ReadAll(res.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	var indices []map[string]interface{}
	if err := json.Unmarshal(body, &indices); err != nil {
		return nil, fmt.Errorf("failed to parse indices response: %w", err)
	}

	var indexNames []string
	for _, index := range indices {
		if name, ok := index["index"].(string); ok {
			// Filter out system indices that start with dot
			if !strings.HasPrefix(name, ".") {
				indexNames = append(indexNames, name)
			}
		}
	}

	return indexNames, nil
}

// ListFields retrieves field mappings for a specific index
func (c *Client) ListFields(indexPattern string) ([]string, error) {
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}
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

	fieldSet := make(map[string]bool)

	// Extract fields from all matching indices
	for _, indexData := range mappingResponse {
		if indexMap, ok := indexData.(map[string]interface{}); ok {
			if mappings, ok := indexMap["mappings"].(map[string]interface{}); ok {
				extractFieldsFromMapping(mappings, "", fieldSet)
			}
		}
	}

	var fields []string
	for field := range fieldSet {
		fields = append(fields, field)
	}

	return fields, nil
}

// extractFieldsFromMapping recursively extracts field names from OpenSearch mapping
func extractFieldsFromMapping(mapping map[string]interface{}, prefix string, fieldSet map[string]bool) {
	if properties, ok := mapping["properties"].(map[string]interface{}); ok {
		for fieldName, fieldMapping := range properties {
			fullFieldName := fieldName
			if prefix != "" {
				fullFieldName = prefix + "." + fieldName
			}

			fieldSet[fullFieldName] = true

			// Recursively extract nested fields
			if fieldMap, ok := fieldMapping.(map[string]interface{}); ok {
				extractFieldsFromMapping(fieldMap, fullFieldName, fieldSet)
			}
		}
	}
}

// GetIndexCompletion provides index name completion with caching
func (c *Client) GetIndexCompletion(toComplete string) ([]string, error) {
	indices, err := c.ListIndices()
	if err != nil {
		return nil, err
	}

	var matches []string
	for _, index := range indices {
		if strings.Contains(index, toComplete) || toComplete == "" {
			matches = append(matches, index)
		}
	}

	// Add common pattern suggestions
	if toComplete == "" || strings.Contains("logs-*", toComplete) {
		matches = append(matches, "logs-*")
	}
	if toComplete == "" || strings.Contains("logstash-*", toComplete) {
		matches = append(matches, "logstash-*")
	}

	return matches, nil
}

// GetFieldCompletion provides field name completion for a specific index
func (c *Client) GetFieldCompletion(indexPattern, toComplete string) ([]string, error) {
	if indexPattern == "" {
		// Return common log fields if no index specified
		return []string{
			"@timestamp",
			"timestamp",
			"message",
			"level",
			"severity",
			"host",
			"hostname",
			"source",
			"application",
			"trace_id",
			"span_id",
			"user_id",
		}, nil
	}

	// Try to get fields from mapping first
	fields, err := c.ListFields(indexPattern)
	if err != nil {
		// If mapping fails, return log-type specific suggestions based on index pattern
		fields = c.getFieldSuggestionsByIndexPattern(indexPattern)
	}

	var matches []string
	for _, field := range fields {
		if strings.Contains(field, toComplete) || toComplete == "" {
			matches = append(matches, field)
		}
	}

	return matches, nil
}

// getFieldSuggestionsByIndexPattern returns appropriate field suggestions based on index pattern
func (c *Client) getFieldSuggestionsByIndexPattern(indexPattern string) []string {
	lowerIndex := strings.ToLower(indexPattern)

	// Check for Kubernetes/Filebeat patterns
	k8sPatterns := []string{"filebeat", "kubernetes", "k8s", "eks", "gke", "aks"}
	for _, pattern := range k8sPatterns {
		if strings.Contains(lowerIndex, pattern) {
			return []string{
				"@timestamp",
				"kubernetes.namespace",
				"kubernetes.pod.name",
				"kubernetes.pod.uid",
				"kubernetes.container.name",
				"kubernetes.container.image",
				"kubernetes.node.name",
				"kubernetes.deployment.name",
				"kubernetes.replicaset.name",
				"kubernetes.daemonset.name",
				"kubernetes.statefulset.name",
				"kubernetes.labels.app",
				"kubernetes.labels.version",
				"kubernetes.annotations.deployment.kubernetes.io/revision",
				"container.id",
				"container.runtime",
				"cloud.provider",
				"cloud.region",
				"cloud.availability_zone",
				"agent.hostname",
				"agent.type",
				"agent.version",
				"input.type",
				"ecs.version",
				"message",
				"level",
				"logger",
				"stream",
			}
		}
	}

	// Check for Jaeger patterns
	jaegerPatterns := []string{"jaeger", "span", "trace", "otel", "apm"}
	for _, pattern := range jaegerPatterns {
		if strings.Contains(lowerIndex, pattern) {
			return []string{
				"traceID",
				"spanID",
				"parentSpanID",
				"operationName",
				"serviceName",
				"startTimeMillis",
				"startTime",
				"duration",
				"span.kind",
				"error",
				"http.status_code",
				"http.method",
				"http.url",
				"http.route",
				"http.user_agent",
				"db.type",
				"db.statement",
				"db.name",
				"db.operation",
				"rpc.service",
				"rpc.method",
				"tags.component",
				"tags.http.method",
				"tags.http.status_code",
				"tags.error",
				"process.tags.hostname",
				"process.tags.ip",
				"process.tags.jaeger.version",
				"references.refType",
				"references.traceID",
				"references.spanID",
				"logs.timestamp",
				"logs.fields.level",
				"logs.fields.event",
			}
		}
	}

	// Default common fields
	return []string{
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
	}
}
