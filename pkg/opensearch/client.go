package opensearch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/logger"
	"github.com/samber/lo"
	"sigs.k8s.io/yaml"

	dutyContext "github.com/flanksource/duty/context"
	"github.com/flanksource/duty/logs"
	dutyOS "github.com/flanksource/duty/logs/opensearch"
	"github.com/flanksource/duty/types"

	schemaBuilder "github.com/flanksource/log-exporter/pkg/schema"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
)

type Config struct {
	Host     string
	Username string
	Password string
	Debug    bool
	Verbose  bool
}

type ExportOptions struct {
	Index      string
	Query      string
	Fields     []string
	From       string // Raw time string (will be parsed with datemath support)
	To         string // Raw time string (will be parsed with datemath support)
	Limit      int
	Output     string
	Schema     string
	Preset     string
	AutoDetect bool
	Filters    FilterOptions
	Scroll     ScrollConfig
}

type ImportOptions struct {
	SampleFile string
	BatchSize  int
	Force      bool
}

type ScrollConfig struct {
	Size    int
	Timeout time.Duration
	Enabled bool
}

type Client struct {
	config Config
}

func (c *Client) GetSearcher() (*dutyOS.Searcher, error) {

	// Execute search using duty's OpenSearch implementation
	backend := dutyOS.Backend{
		Address:  c.config.Host,
		Username: lo.ToPtr(types.EnvVar{ValueStatic: c.config.Username}),
		Password: lo.ToPtr(types.EnvVar{ValueStatic: c.config.Password}),
	}

	// Create a duty context implementation
	dutyCtx := dutyContext.New()

	return dutyOS.New(dutyCtx, backend, nil)
}

func (c *Client) GetClient() (*opensearch.Client, error) {

	searcher, err := c.GetSearcher()
	if err != nil {
		return nil, fmt.Errorf("failed to create duty OpenSearch searcher: %w", err)
	}
	return searcher.GetRawClient(), nil

}

func NewClient(config Config) (*Client, error) {

	// Enable debug logging if requested
	if config.Debug {
		// OpenSearch client will log to stdout by default when Logger is set
		// We'll handle debug output through our own verbose flag
	}

	return &Client{
		config: config,
	}, nil
}

func (c *Client) Export(opts ExportOptions) error {

	// Step 1: Inspect index to determine type and timestamp field
	indexInfo, err := c.InspectIndex(opts.Index)
	if err != nil {
		if c.config.Verbose {
			logger.Debugf("Warning: Failed to inspect index '%s': %v\n", opts.Index, err)
			logger.Debugf("Falling back to default settings\n")
		}
		// Create fallback index info
		indexInfo = &IndexInfo{
			Type:           "generic",
			TimestampField: "@timestamp",
			HasDateField:   true,
			IndexPattern:   opts.Index,
		}
	}

	if c.config.Verbose {

		warnings := indexInfo.ValidateIndexInfo()
		if len(warnings) > 0 {
			logger.Debugf("  Warnings:\n")
			for _, warning := range warnings {
				logger.Debugf("    - %s\n", warning)
			}
		}
	}

	// Step 2: Parse time range with proper datemath support
	fromTime, toTime, err := ParseDateTimeRange(opts.From, opts.To)
	if err != nil {
		return fmt.Errorf("failed to parse time range: %w", err)
	}

	if c.config.Verbose && (fromTime != "" || toTime != "") {
		logger.Infof("Time range: %s to %s\n", fromTime, toTime)
	}

	// Step 3: Get field mappings for this log type, using embedded clicky schema if specified
	var fieldMapping *FieldMapping
	if opts.Schema != "" {
		if embeddedSchema, err := LoadEmbeddedClickySchema(opts.Schema); err == nil {
			// Resolve schema fields to actual field names
			resolvedSchema := ResolveSchemaFields(embeddedSchema, indexInfo.AvailableFields)
			fieldMapping = GetFieldMappingFromSchema(resolvedSchema)
			if c.config.Verbose {
				logger.Infof("Using embedded clicky schema '%s' for field mapping\n", opts.Schema)
			}
		} else {
			// Fallback to pattern-based mapping
			fieldMapping = GetFieldMappings(indexInfo.Type, indexInfo.AvailableFields)
		}
	} else {
		// Use pattern-based mapping
		fieldMapping = GetFieldMappings(indexInfo.Type, indexInfo.AvailableFields)
	}

	// Debug output for field mappings
	if c.config.Debug {
		logger.Tracef("Field mappings for log type '%s':\n", indexInfo.Type)
		if len(fieldMapping.Namespace) > 0 {
			logger.Tracef("  Namespace: %v\n", fieldMapping.Namespace)
		}
		if len(fieldMapping.Pod) > 0 {
			logger.Tracef("  Pod: %v\n", fieldMapping.Pod)
		}
		if len(fieldMapping.Deployment) > 0 {
			logger.Tracef("  Deployment: %v\n", fieldMapping.Deployment)
		}
		if len(fieldMapping.Container) > 0 {
			logger.Tracef("  Container: %v\n", fieldMapping.Container)
		}
		if len(fieldMapping.Service) > 0 {
			logger.Tracef("  Service: %v\n", fieldMapping.Service)
		}
		if len(fieldMapping.Operation) > 0 {
			logger.Tracef("  Operation: %v\n", fieldMapping.Operation)
		}
	}

	// Step 4: Process filters if any are specified
	var filterConstraints []MultiFieldConstraint
	if opts.Filters.HasActiveFilters() {

		// Build filter constraints
		filterConstraints = BuildMultiFieldConstraints(opts.Filters, fieldMapping)

		// Show warnings for filters that couldn't be applied
		warnings := ValidateFilters(opts.Filters, fieldMapping, indexInfo.Type, indexInfo.AvailableFields)
		if len(warnings) > 0 && (c.config.Verbose || c.config.Debug) {
			logger.Infof("Filter warnings:\n")
			for _, warning := range warnings {
				logger.Infof("  - %s\n", warning)
			}
		}

		if c.config.Verbose && len(filterConstraints) > 0 {
			logger.Infof("Applied filters:\n")
			for _, constraint := range filterConstraints {
				logger.Infof("  - %s: %s (fields: %v)\n", constraint.Name, constraint.Value, constraint.Fields)
			}
		}
	}

	// Step 5: Build OpenSearch query with detected timestamp field and filters
	query, err := c.buildQueryWithFilters(opts, indexInfo.TimestampField, fromTime, toTime, filterConstraints)
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	if c.config.Debug || c.config.Verbose {
		logger.Infof("Executing query: %s\n", query)

		// In debug mode, show formatted JSON query for better readability
		if c.config.Debug {
			c.printFormattedQuery(query)
		}
	}

	searcher, err := c.GetSearcher()
	if err != nil {
		return fmt.Errorf("failed to get searcher: %w", err)
	}

	// Determine if we should use scroll based on limit and scroll configuration
	const scrollThreshold = 10000
	useScroll := opts.Scroll.Enabled && opts.Limit > scrollThreshold

	if c.config.Debug {
		logger.Tracef("Limit: %d, Scroll enabled: %v, Use scroll: %v\n", opts.Limit, opts.Scroll.Enabled, useScroll)
	}

	var result *logs.LogResult
	if useScroll {
		result, err = c.performScrollSearch(searcher, opts, query)
		if err != nil {
			return fmt.Errorf("failed to perform scroll search: %w", err)
		}
	} else {
		// Use regular search for smaller result sets
		request := dutyOS.Request{
			Index: opts.Index,
			Query: query,
			Limit: fmt.Sprintf("%d", opts.Limit),
		}
		result, err = searcher.Search(dutyContext.New(), request)
		if err != nil {
			return fmt.Errorf("failed to search: %w", err)
		}
	}

	if c.config.Verbose {
		logger.Infof("Found %d log entries\n", len(result.Logs))
	}

	// Convert to exportable data structure
	data, err := c.convertLogsToData(result.Logs, opts.Fields, fieldMapping)
	if err != nil {
		return fmt.Errorf("failed to convert logs: %w", err)
	}

	// Generate schema based on priority: embedded clicky schema > custom schema file > preset > auto-detect > mapping-based
	var schema *api.PrettyObject
	if opts.Schema != "" {
		// First try to load as embedded clicky schema
		if embeddedSchema, err := LoadEmbeddedClickySchema(opts.Schema); err == nil {
			// Resolve field names and use the resolved schema
			schema = ResolveSchemaFields(embeddedSchema, indexInfo.AvailableFields)
			if c.config.Verbose {
				logger.Infof("Using embedded clicky schema: %s\n", opts.Schema)
			}
		} else {
			// Try to load as custom schema file
			parser := api.NewStructParser()
			loadedSchema, err := parser.LoadSchemaFromYAML(opts.Schema)
			if err != nil {
				return fmt.Errorf("schema '%s' not found as embedded schema or file: %w", opts.Schema, err)
			}
			schema = loadedSchema
		}
	} else if opts.Preset != "" {
		// Use preset schema (deprecated)
		if c.config.Verbose {
			logger.Infof("Warning: --preset is deprecated, use --schema instead\n")
		}
		schema, err = c.getPresetSchema(opts.Preset)
		if err != nil {
			return fmt.Errorf("failed to get preset schema: %w", err)
		}
	} else if opts.AutoDetect {
		// Auto-detect schema from index pattern
		schema, err = c.autoDetectSchema(opts.Index, opts.Fields)
		yamlData, err := yaml.Marshal(schema)
		os.WriteFile(opts.Index+".schema.yaml", yamlData, 0644)
		if err != nil {
			if c.config.Verbose {
				logger.Infof("Warning: Failed to auto-detect schema, using mapping-based: %v\n", err)
			}
			// Fallback to mapping-based schema
			schema, err = c.buildMappingBasedSchema(opts.Index, opts.Fields, result.Logs)
		}
	} else {
		// Auto-generate schema from OpenSearch mappings
		schema, err = c.buildMappingBasedSchema(opts.Index, opts.Fields, result.Logs)
	}

	if err != nil {
		return fmt.Errorf("failed to generate schema: %w", err)
	}

	// Format using clicky
	return c.formatOutput(data, schema, opts)
}

func (c *Client) buildQueryWithFilters(opts ExportOptions, timestampField, fromTime, toTime string, filterConstraints []MultiFieldConstraint) (string, error) {
	// Debug output for filter constraints
	if c.config.Debug && len(filterConstraints) > 0 {
		logger.Tracef("Filter constraints being applied:\n")
		for i, constraint := range filterConstraints {
			logger.Tracef("  [%d] Name: %s, Value: %s, Fields: %v\n", i+1, constraint.Name, constraint.Value, constraint.Fields)
		}
	}

	// Check if this is a JSON query
	if IsJSONQuery(opts.Query) {
		if c.config.Verbose {
			logger.Infof("Detected JSON query, processing as OpenSearch Query DSL\n")
		}
		if c.config.Debug {
			logger.Tracef("Original user query: %s\n", opts.Query)
		}

		// Validate the JSON query
		if err := ValidateJSONQuery(opts.Query); err != nil {
			return "", fmt.Errorf("invalid JSON query: %w", err)
		}

		// Start with the user's JSON query
		finalQuery := opts.Query
		var err error

		// Merge time range constraints
		finalQuery, err = MergeJSONWithTimeRange(finalQuery, timestampField, fromTime, toTime)
		if err != nil {
			return "", fmt.Errorf("failed to merge time range into JSON query: %w", err)
		}

		// Add field filtering
		finalQuery, err = AddFieldFilteringToJSON(finalQuery, opts.Fields)
		if err != nil {
			return "", fmt.Errorf("failed to add field filtering to JSON query: %w", err)
		}

		// Add sorting (if not already present)
		finalQuery, err = AddSortingToJSON(finalQuery, timestampField)
		if err != nil {
			return "", fmt.Errorf("failed to add sorting to JSON query: %w", err)
		}

		// Apply filters
		finalQuery, err = AddMultiFieldFiltersToQuery(finalQuery, filterConstraints)
		if err != nil {
			return "", fmt.Errorf("failed to add filters to JSON query: %w", err)
		}

		return finalQuery, nil
	}

	// Handle traditional Lucene query string
	query := map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": []interface{}{},
			},
		},
	}

	// Add user query
	if opts.Query != "" && opts.Query != "*" {
		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"] = append(
			query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"].([]interface{}),
			map[string]interface{}{
				"query_string": map[string]interface{}{
					"query": opts.Query,
				},
			},
		)
	}

	// Add time range if specified
	if fromTime != "" || toTime != "" {
		timeQuery := map[string]interface{}{
			"range": map[string]interface{}{
				timestampField: map[string]interface{}{},
			},
		}

		rangeParams := timeQuery["range"].(map[string]interface{})[timestampField].(map[string]interface{})

		if fromTime != "" {
			if timestampField == "startTimeMillis" {
				// Convert RFC3339 time to epoch milliseconds for startTimeMillis field
				if t, err := time.Parse(time.RFC3339, fromTime); err == nil {
					rangeParams["gte"] = t.UnixMilli()
				} else {
					rangeParams["gte"] = fromTime
				}
			} else {
				rangeParams["gte"] = fromTime
			}
		}

		if toTime != "" {
			if timestampField == "startTimeMillis" {
				// Convert RFC3339 time to epoch milliseconds for startTimeMillis field
				if t, err := time.Parse(time.RFC3339, toTime); err == nil {
					rangeParams["lte"] = t.UnixMilli()
				} else {
					rangeParams["lte"] = toTime
				}
			} else {
				rangeParams["lte"] = toTime
			}
		}

		// Add format specification for Jaeger timestamps (startTimeMillis)
		if timestampField == "startTimeMillis" {
			rangeParams["format"] = "epoch_millis"
		}

		query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"] = append(
			query["query"].(map[string]interface{})["bool"].(map[string]interface{})["must"].([]interface{}),
			timeQuery,
		)
	}

	// Add field filtering
	if len(opts.Fields) > 0 {
		query["_source"] = opts.Fields
	}

	// Add sorting using the detected timestamp field
	sortParams := map[string]interface{}{
		"order": "desc",
	}

	// Add unmapped_type for Jaeger timestamps
	if timestampField == "startTimeMillis" {
		sortParams["unmapped_type"] = "boolean"
	}

	query["sort"] = []interface{}{
		map[string]interface{}{
			timestampField: sortParams,
		},
	}

	// Convert to JSON
	queryBytes, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query: %w", err)
	}

	// Apply filters to the constructed query
	finalQuery := string(queryBytes)
	finalQuery, err = AddMultiFieldFiltersToQuery(finalQuery, filterConstraints)
	if err != nil {
		return "", fmt.Errorf("failed to add filters to Lucene query: %w", err)
	}

	return finalQuery, nil
}

func (c *Client) convertLogsToData(logLines []*logs.LogLine, fields []string, fieldMapping *FieldMapping) (interface{}, error) {
	var data []map[string]interface{}

	for _, logLine := range logLines {
		entry := make(map[string]interface{})

		// Use labels as primary data source (contains raw OpenSearch fields)
		// This preserves the original field names from OpenSearch
		if logLine.Labels != nil {
			for k, v := range logLine.Labels {
				// Parse epoch millisecond timestamps and store as readable timestamp
				if strings.HasSuffix(strings.ToLower(k), "millis") {
					// Try parsing as millisecond epoch and convert to timestamp
					if parsedTime := api.ParseEpoch(v); parsedTime != nil {
						entry["timestamp"] = parsedTime.Format(time.RFC3339)
					}
				}
				entry[k] = v
			}
		}

		// Add core fields only if not already present from labels
		// and only if no specific fields are requested (preserve raw data when fields are specified)
		if len(fields) == 0 {
			if _, exists := entry["id"]; !exists {
				entry["id"] = logLine.ID
			}
			if _, hasTimestamp := entry["@timestamp"]; !hasTimestamp {
				if _, hasTimestampAlt := entry["timestamp"]; !hasTimestampAlt {
					entry["timestamp"] = logLine.FirstObserved.Format(time.RFC3339)
				}
			}
			if _, exists := entry["message"]; !exists {
				entry["message"] = logLine.Message
			}
			if logLine.Severity != "" {
				if _, exists := entry["severity"]; !exists {
					entry["severity"] = logLine.Severity
				}
			}
			if logLine.Source != "" {
				if _, exists := entry["source"]; !exists {
					entry["source"] = logLine.Source
				}
			}
			if logLine.Host != "" {
				if _, exists := entry["host"]; !exists {
					entry["host"] = logLine.Host
				}
			}
			if logLine.Count > 1 {
				if _, exists := entry["count"]; !exists {
					entry["count"] = logLine.Count
				}
			}
		}

		// Add canonical field names if field mapping is provided
		if fieldMapping != nil {
			// Add namespace canonical field
			if len(fieldMapping.Namespace) > 0 {
				for _, field := range fieldMapping.Namespace {
					if value, exists := entry[field]; exists && value != nil && value != "" {
						entry["namespace"] = value
						break
					}
				}
			}

			// Add pod canonical field
			if len(fieldMapping.Pod) > 0 {
				for _, field := range fieldMapping.Pod {
					if value, exists := entry[field]; exists && value != nil && value != "" {
						entry["pod"] = value
						break
					}
				}
			}

			// Add deployment canonical field
			if len(fieldMapping.Deployment) > 0 {
				for _, field := range fieldMapping.Deployment {
					if value, exists := entry[field]; exists && value != nil && value != "" {
						entry["deployment"] = value
						break
					}
				}
			}

			// Add container canonical field
			if len(fieldMapping.Container) > 0 {
				for _, field := range fieldMapping.Container {
					if value, exists := entry[field]; exists && value != nil && value != "" {
						entry["container"] = value
						break
					}
				}
			}

			// Add service canonical field
			if len(fieldMapping.Service) > 0 {
				for _, field := range fieldMapping.Service {
					if value, exists := entry[field]; exists && value != nil && value != "" {
						entry["service"] = value
						break
					}
				}
			}

			// Add operation canonical field
			if len(fieldMapping.Operation) > 0 {
				for _, field := range fieldMapping.Operation {
					if value, exists := entry[field]; exists && value != nil && value != "" {
						entry["operation"] = value
						break
					}
				}
			}
		}

		// Filter fields if specified
		if len(fields) > 0 {
			filteredEntry := make(map[string]interface{})
			for _, field := range fields {
				if value, exists := entry[field]; exists {
					filteredEntry[field] = value
				}
			}
			entry = filteredEntry
		}

		data = append(data, entry)
	}

	return data, nil
}

func (c *Client) generateSchema(logLines []*logs.LogLine, fields []string) (*api.PrettyObject, error) {
	logSchema := &api.PrettyObject{
		Fields: []api.PrettyField{},
	}

	// Determine which fields to include
	fieldsToInclude := fields
	if len(fieldsToInclude) == 0 {
		// Auto-detect fields from first few log entries
		fieldSet := make(map[string]bool)
		for i, logLine := range logLines {
			if i >= 10 { // Sample first 10 entries
				break
			}

			fieldSet["timestamp"] = true
			fieldSet["message"] = true
			if logLine.Severity != "" {
				fieldSet["severity"] = true
			}
			if logLine.Source != "" {
				fieldSet["source"] = true
			}
			if logLine.Host != "" {
				fieldSet["host"] = true
			}

			for k := range logLine.Labels {
				fieldSet[k] = true
			}
		}

		for field := range fieldSet {
			fieldsToInclude = append(fieldsToInclude, field)
		}
	}

	// Generate field definitions
	for _, fieldName := range fieldsToInclude {
		field := c.generateFieldSchema(fieldName)
		logSchema.Fields = append(logSchema.Fields, field)
	}

	return logSchema, nil
}

func (c *Client) generateFieldSchema(fieldName string) api.PrettyField {
	field := api.PrettyField{
		Name: fieldName,
		Type: "string",
	}

	// Apply styling and formatting based on field name
	switch {
	case fieldName == "timestamp" || fieldName == "@timestamp":
		field.Format = "date"
		field.Style = "text-gray-500 text-sm"

	case fieldName == "severity" || fieldName == "level":
		field.Style = "font-bold uppercase text-sm px-2 py-1 rounded"
		field.ColorOptions = map[string]string{
			"red":    "ERROR|FATAL|CRITICAL",
			"yellow": "WARN|WARNING",
			"green":  "INFO|INFORMATION",
			"gray":   "DEBUG|TRACE",
		}

	case fieldName == "message":
		field.Style = "text-gray-700"
		field.Type = "string"

	case fieldName == "host" || fieldName == "hostname":
		field.Style = "text-blue-600 font-medium"

	case fieldName == "source" || fieldName == "application":
		field.Style = "text-indigo-600 font-medium"

	case strings.Contains(fieldName, "id"):
		field.Style = "text-gray-500 font-mono text-xs"

	case strings.Contains(fieldName, "count"):
		field.Type = "int"
		field.Style = "text-center font-medium"
		field.ColorOptions = map[string]string{
			"red":    "> 100",
			"yellow": "> 10",
			"green":  "<= 10",
		}

	default:
		field.Style = "text-gray-600"
	}

	return field
}

func (c *Client) formatOutput(data interface{}, schema *api.PrettyObject, opts ExportOptions) error {

	// Wrap slice data in a container for clicky's ParseDataWithSchema
	// which expects a struct or single map, not a slice
	var processableData interface{}
	if dataSlice, ok := data.([]map[string]interface{}); ok {
		// Wrap the slice in a container object
		processableData = map[string]interface{}{
			"logs": dataSlice,
		}
	} else {
		// Use data as-is if it's already a single object
		processableData = data
	}

	// Use clicky's schema-aware formatting
	parser := api.NewStructParser()
	prettyData, err := parser.ParseDataWithSchema(processableData, schema)
	if err != nil {
		return fmt.Errorf("failed to parse data with schema: %w", err)
	}

	// Determine format based on file extension if available, otherwise use clicky's format
	format := clicky.Flags.FormatOptions.ResolveFormat()
	if opts.Output != "" && strings.HasSuffix(strings.ToLower(opts.Output), ".json") {
		format = "json"
	}

	if format == "json" {
		// Use standard JSON encoder without HTML escaping for clean output
		var buf bytes.Buffer
		encoder := json.NewEncoder(&buf)
		encoder.SetEscapeHTML(false)
		encoder.SetIndent("", "  ")

		if err := encoder.Encode(data); err != nil {
			return fmt.Errorf("failed to marshal JSON: %w", err)
		}

		return os.WriteFile(opts.Output, buf.Bytes(), 0644)
	} else if format == "yaml" || (opts.Output != "" && strings.HasSuffix(strings.ToLower(opts.Output), ".yaml")) {
		yamlData, err := yaml.Marshal(data)
		if err != nil {
			return fmt.Errorf("failed to marshal YAML: %w", err)
		}
		return os.WriteFile(opts.Output, yamlData, 0644)
	}

	err = clicky.FormatToFile(prettyData, clicky.Flags.FormatOptions, opts.Output)
	if err != nil {
		return fmt.Errorf("failed to format data: %w", err)
	}

	return nil
}

// getPresetSchema returns a preset schema by name
func (c *Client) getPresetSchema(preset string) (*api.PrettyObject, error) {
	schema, found := schemaBuilder.GetPresetSchema(preset)
	if !found {
		return nil, fmt.Errorf("unknown preset schema: %s", preset)
	}
	return c.wrapSchemaInContainer(schema), nil
}

// autoDetectSchema attempts to detect the appropriate schema from index patterns
func (c *Client) autoDetectSchema(indexPattern string, fields []string) (*api.PrettyObject, error) {
	// Convert to lowercase for matching
	lowerIndex := strings.ToLower(indexPattern)

	// Check for Kubernetes/Filebeat patterns
	k8sPatterns := []string{"filebeat", "kubernetes", "k8s", "eks", "gke", "aks"}
	for _, pattern := range k8sPatterns {
		if strings.Contains(lowerIndex, pattern) {
			if c.config.Verbose {
				logger.Infof("Auto-detected Kubernetes logs from index pattern: %s\n", indexPattern)
			}
			return c.wrapSchemaInContainer(schemaBuilder.GetKubernetesSchema()), nil
		}
	}

	// Check for Jaeger patterns
	jaegerPatterns := []string{"jaeger", "span", "trace", "otel", "apm"}
	for _, pattern := range jaegerPatterns {
		if strings.Contains(lowerIndex, pattern) {
			if c.config.Verbose {
				logger.Infof("Auto-detected Jaeger traces from index pattern: %s\n", indexPattern)
			}
			return c.wrapSchemaInContainer(schemaBuilder.GetJaegerSchema()), nil
		}
	}

	// If we can't auto-detect, return error to fall back to mapping-based
	return nil, fmt.Errorf("could not auto-detect schema from index pattern: %s", indexPattern)
}

// buildMappingBasedSchema builds schema from OpenSearch mappings with fallback
func (c *Client) buildMappingBasedSchema(indexPattern string, fields []string, logLines []*logs.LogLine) (*api.PrettyObject, error) {
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}
	builder := schemaBuilder.NewBuilder(client)
	logSchema, err := builder.BuildSchemaFromMapping(indexPattern, fields)
	if err != nil {
		// Fallback to basic schema generation from log data
		if c.config.Verbose {
			logger.Infof("Warning: Failed to build schema from mapping, using fallback: %v\n", err)
		}
		logSchema, err = c.generateSchema(logLines, fields)
		if err != nil {
			return nil, fmt.Errorf("failed to generate fallback schema: %w", err)
		}
	}

	// Wrap the log schema in a container to match the data structure
	return c.wrapSchemaInContainer(logSchema), nil
}

// wrapSchemaInContainer wraps a log entry schema in a container structure
// to match the data format: { "logs": [...] }
func (c *Client) wrapSchemaInContainer(logSchema *api.PrettyObject) *api.PrettyObject {
	return &api.PrettyObject{
		Fields: []api.PrettyField{
			{
				Name:   "logs",
				Label:  "Logs",
				Type:   "array",
				Format: "table",
				TableOptions: api.PrettyTable{
					Fields: logSchema.Fields,
				},
			},
		},
	}
}

// printFormattedQuery prints the query in a formatted JSON structure for debugging
func (c *Client) printFormattedQuery(query string) {
	if query == "" {
		logger.Tracef("Empty query\n")
		return
	}

	// Try to parse as JSON for pretty printing
	var jsonQuery map[string]interface{}
	if err := json.Unmarshal([]byte(query), &jsonQuery); err != nil {
		// If it's not JSON (e.g., Lucene query), just print as-is
		logger.Tracef("Query (Lucene): %s\n", query)
		return
	}

	// Pretty print the JSON
	prettyBytes, err := json.MarshalIndent(jsonQuery, " ", "  ")
	if err != nil {
		logger.Tracef("Query (raw): %s\n", query)
		return
	}

	logger.Tracef("Query (formatted JSON):\n%s\n", string(prettyBytes))
}

// performScrollSearch executes a scroll search for large result sets
func (c *Client) performScrollSearch(searcher *dutyOS.Searcher, opts ExportOptions, query string) (*logs.LogResult, error) {
	ctx := dutyContext.New()

	// Set up scroll options with defaults
	scrollSize := opts.Scroll.Size
	if scrollSize <= 0 {
		scrollSize = 1000
	}

	scrollTimeout := opts.Scroll.Timeout
	if scrollTimeout == 0 {
		scrollTimeout = time.Minute
	}

	if c.config.Debug {
		logger.Tracef("Starting scroll search with size=%d, timeout=%v\n", scrollSize, scrollTimeout)
	}

	// Create scroll request
	scrollReq := dutyOS.ScrollRequest{
		Request: dutyOS.Request{
			Index: opts.Index,
			Query: query,
		},
		Scroll: dutyOS.ScrollOptions{
			Size:    scrollSize,
			Timeout: scrollTimeout,
			Enabled: true,
		},
	}

	// Initialize the scroll
	initialResult, scrollID, err := searcher.SearchWithScroll(ctx, scrollReq)
	if err != nil {
		return nil, fmt.Errorf("failed to initialize scroll: %w", err)
	}

	// Ensure we clean up the scroll context
	defer func() {
		if scrollID != "" {
			if err := searcher.ClearScroll(ctx, scrollID); err != nil && c.config.Verbose {
				logger.Infof("Warning: Failed to clear scroll: %v\n", err)
			}
		}
	}()

	if c.config.Debug {
		logger.Tracef("Initial scroll returned %d documents, scroll_id: %s\n", len(initialResult.Logs), scrollID[:20]+"...")
	}

	// Collect all results
	allLogs := initialResult.Logs
	totalFetched := len(allLogs)

	// Continue scrolling until we have enough results or no more data
	for scrollID != "" && totalFetched < opts.Limit {
		if c.config.Verbose {
			logger.Infof("Scroll progress: %d/%d documents fetched\n", totalFetched, opts.Limit)
		}

		nextResult, nextScrollID, err := searcher.ScrollNext(ctx, scrollID, scrollTimeout)
		if err != nil {
			return nil, fmt.Errorf("failed to continue scroll: %w", err)
		}

		// No more results
		if len(nextResult.Logs) == 0 {
			break
		}

		if c.config.Debug {
			logger.Tracef("Scroll next returned %d documents\n", len(nextResult.Logs))
		}

		// Add results, but respect the limit
		remaining := opts.Limit - totalFetched
		if len(nextResult.Logs) > remaining {
			allLogs = append(allLogs, nextResult.Logs[:remaining]...)
			totalFetched = opts.Limit
			break
		} else {
			allLogs = append(allLogs, nextResult.Logs...)
			totalFetched += len(nextResult.Logs)
		}

		scrollID = nextScrollID
	}

	if c.config.Verbose {
		logger.Infof("Scroll completed. Total documents fetched: %d\n", totalFetched)
	}

	// Return combined result
	return &logs.LogResult{
		Logs: allLogs,
	}, nil
}

// ParseFields splits a comma-separated field list into a slice
func ParseFields(fieldsStr string) []string {
	if fieldsStr == "" {
		return nil
	}

	fields := strings.Split(fieldsStr, ",")
	for i, field := range fields {
		fields[i] = strings.TrimSpace(field)
	}
	return fields
}
