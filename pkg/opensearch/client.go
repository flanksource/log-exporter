package opensearch

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/clicky/formatters"
	"github.com/samber/lo"

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
	Format     string
	Output     string
	Schema     string
	Preset     string
	AutoDetect bool
	Filters    FilterOptions
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
			fmt.Printf("Warning: Failed to inspect index '%s': %v\n", opts.Index, err)
			fmt.Printf("Falling back to default settings\n")
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
		fmt.Printf("Index inspection results:\n")
		fmt.Printf("  Type: %s\n", indexInfo.Type)
		fmt.Printf("  Timestamp field: %s\n", indexInfo.TimestampField)
		fmt.Printf("  Available fields: %d\n", len(indexInfo.AvailableFields))

		warnings := indexInfo.ValidateIndexInfo()
		if len(warnings) > 0 {
			fmt.Printf("  Warnings:\n")
			for _, warning := range warnings {
				fmt.Printf("    - %s\n", warning)
			}
		}
	}

	// Step 2: Parse time range with proper datemath support
	fromTime, toTime, err := ParseDateTimeRange(opts.From, opts.To)
	if err != nil {
		return fmt.Errorf("failed to parse time range: %w", err)
	}

	if c.config.Verbose && (fromTime != "" || toTime != "") {
		fmt.Printf("Time range: %s to %s\n", fromTime, toTime)
	}

	// Step 3: Process filters if any are specified
	var filterConstraints []FilterConstraint
	if opts.Filters.HasActiveFilters() {
		// Get field mappings for this log type
		fieldMapping := GetFieldMappings(indexInfo.Type, indexInfo.AvailableFields)

		// Debug output for field mappings
		if c.config.Debug {
			fmt.Printf("DEBUG: Field mappings for log type '%s':\n", indexInfo.Type)
			if fieldMapping.Namespace != "" {
				fmt.Printf("DEBUG:   Namespace: %s\n", fieldMapping.Namespace)
			}
			if fieldMapping.Pod != "" {
				fmt.Printf("DEBUG:   Pod: %s\n", fieldMapping.Pod)
			}
			if fieldMapping.Deployment != "" {
				fmt.Printf("DEBUG:   Deployment: %s\n", fieldMapping.Deployment)
			}
			if fieldMapping.Service != "" {
				fmt.Printf("DEBUG:   Service: %s\n", fieldMapping.Service)
			}
			if fieldMapping.Operation != "" {
				fmt.Printf("DEBUG:   Operation: %s\n", fieldMapping.Operation)
			}
		}

		// Build filter constraints
		filterConstraints = BuildFilterConstraints(opts.Filters, fieldMapping)

		// Show warnings for filters that couldn't be applied
		warnings := ValidateFilters(opts.Filters, fieldMapping, indexInfo.Type, indexInfo.AvailableFields)
		if len(warnings) > 0 && (c.config.Verbose || c.config.Debug) {
			fmt.Printf("Filter warnings:\n")
			for _, warning := range warnings {
				fmt.Printf("  - %s\n", warning)
			}
		}

		if c.config.Verbose && len(filterConstraints) > 0 {
			fmt.Printf("Applied filters:\n")
			for _, constraint := range filterConstraints {
				fmt.Printf("  - %s: %s\n", constraint.Field, constraint.Value)
			}
		}
	}

	// Step 4: Build OpenSearch query with detected timestamp field and filters
	query, err := c.buildQueryWithFilters(opts, indexInfo.TimestampField, fromTime, toTime, filterConstraints)
	if err != nil {
		return fmt.Errorf("failed to build query: %w", err)
	}

	if c.config.Debug || c.config.Verbose {
		fmt.Printf("Executing query: %s\n", query)

		// In debug mode, show formatted JSON query for better readability
		if c.config.Debug {
			c.printFormattedQuery(query)
		}
	}

	request := dutyOS.Request{
		Index: opts.Index,
		Query: query,
		Limit: fmt.Sprintf("%d", opts.Limit),
	}
	searcher, err := c.GetSearcher()
	if err != nil {
		return fmt.Errorf("failed to get searcher: %w", err)
	}

	result, err := searcher.Search(dutyContext.New(), request)
	if err != nil {
		return fmt.Errorf("failed to search: %w", err)
	}

	if c.config.Verbose {
		fmt.Printf("Found %d log entries\n", len(result.Logs))
	}

	// Convert to exportable data structure
	data, err := c.convertLogsToData(result.Logs, opts.Fields)
	if err != nil {
		return fmt.Errorf("failed to convert logs: %w", err)
	}

	// Generate schema based on priority: custom schema > preset > auto-detect > mapping-based
	var schema *api.PrettyObject
	if opts.Schema != "" {
		// Load custom schema file
		parser := api.NewStructParser()
		loadedSchema, err := parser.LoadSchemaFromYAML(opts.Schema)
		if err != nil {
			return fmt.Errorf("failed to load schema: %w", err)
		}
		schema = loadedSchema
	} else if opts.Preset != "" {
		// Use preset schema
		schema, err = c.getPresetSchema(opts.Preset)
		if err != nil {
			return fmt.Errorf("failed to get preset schema: %w", err)
		}
	} else if opts.AutoDetect {
		// Auto-detect schema from index pattern
		schema, err = c.autoDetectSchema(opts.Index, opts.Fields)
		if err != nil {
			if c.config.Verbose {
				fmt.Printf("Warning: Failed to auto-detect schema, using mapping-based: %v\n", err)
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

func (c *Client) buildQueryWithFilters(opts ExportOptions, timestampField, fromTime, toTime string, filterConstraints []FilterConstraint) (string, error) {
	// Debug output for filter constraints
	if c.config.Debug && len(filterConstraints) > 0 {
		fmt.Printf("DEBUG: Filter constraints being applied:\n")
		for i, constraint := range filterConstraints {
			fmt.Printf("DEBUG:   [%d] Field: %s, Value: %s\n", i+1, constraint.Field, constraint.Value)
		}
	}

	// Check if this is a JSON query
	if IsJSONQuery(opts.Query) {
		if c.config.Verbose {
			fmt.Printf("Detected JSON query, processing as OpenSearch Query DSL\n")
		}
		if c.config.Debug {
			fmt.Printf("DEBUG: Original user query: %s\n", opts.Query)
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
		finalQuery, err = AddFiltersToQuery(finalQuery, filterConstraints)
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

		if fromTime != "" {
			timeQuery["range"].(map[string]interface{})[timestampField].(map[string]interface{})["gte"] = fromTime
		}

		if toTime != "" {
			timeQuery["range"].(map[string]interface{})[timestampField].(map[string]interface{})["lte"] = toTime
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
	query["sort"] = []interface{}{
		map[string]interface{}{
			timestampField: map[string]interface{}{
				"order": "desc",
			},
		},
	}

	// Convert to JSON
	queryBytes, err := json.Marshal(query)
	if err != nil {
		return "", fmt.Errorf("failed to marshal query: %w", err)
	}

	// Apply filters to the constructed query
	finalQuery := string(queryBytes)
	finalQuery, err = AddFiltersToQuery(finalQuery, filterConstraints)
	if err != nil {
		return "", fmt.Errorf("failed to add filters to Lucene query: %w", err)
	}

	return finalQuery, nil
}

func (c *Client) convertLogsToData(logLines []*logs.LogLine, fields []string) (interface{}, error) {
	var data []map[string]interface{}

	for _, logLine := range logLines {
		entry := make(map[string]interface{})

		// Always include core fields
		entry["id"] = logLine.ID
		entry["timestamp"] = logLine.FirstObserved.Format(time.RFC3339)
		entry["message"] = logLine.Message

		if logLine.Severity != "" {
			entry["severity"] = logLine.Severity
		}
		if logLine.Source != "" {
			entry["source"] = logLine.Source
		}
		if logLine.Host != "" {
			entry["host"] = logLine.Host
		}
		if logLine.Count > 1 {
			entry["count"] = logLine.Count
		}

		// Add labels
		if logLine.Labels != nil {
			for k, v := range logLine.Labels {
				entry[k] = v
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
	formatOpts := formatters.FormatOptions{
		Format:  opts.Format,
		Output:  opts.Output,
		Schema:  schema,
		Verbose: c.config.Verbose,
	}

	manager := formatters.NewFormatManager()

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

	output, err := manager.FormatWithSchema(prettyData, formatOpts)
	if err != nil {
		return fmt.Errorf("failed to format data: %w", err)
	}

	// Output result
	if opts.Output != "" {
		// Write to file (clicky handles this internally)
		if c.config.Verbose {
			fmt.Printf("Output written to %s\n", opts.Output)
		}
	} else {
		// Print to stdout
		fmt.Print(output)
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
				fmt.Printf("Auto-detected Kubernetes logs from index pattern: %s\n", indexPattern)
			}
			return c.wrapSchemaInContainer(schemaBuilder.GetKubernetesSchema()), nil
		}
	}

	// Check for Jaeger patterns
	jaegerPatterns := []string{"jaeger", "span", "trace", "otel", "apm"}
	for _, pattern := range jaegerPatterns {
		if strings.Contains(lowerIndex, pattern) {
			if c.config.Verbose {
				fmt.Printf("Auto-detected Jaeger traces from index pattern: %s\n", indexPattern)
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
			fmt.Printf("Warning: Failed to build schema from mapping, using fallback: %v\n", err)
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
		fmt.Printf("DEBUG: Empty query\n")
		return
	}

	// Try to parse as JSON for pretty printing
	var jsonQuery map[string]interface{}
	if err := json.Unmarshal([]byte(query), &jsonQuery); err != nil {
		// If it's not JSON (e.g., Lucene query), just print as-is
		fmt.Printf("DEBUG: Query (Lucene): %s\n", query)
		return
	}

	// Pretty print the JSON
	prettyBytes, err := json.MarshalIndent(jsonQuery, "DEBUG: ", "  ")
	if err != nil {
		fmt.Printf("DEBUG: Query (raw): %s\n", query)
		return
	}

	fmt.Printf("DEBUG: Query (formatted JSON):\n%s\n", string(prettyBytes))
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
