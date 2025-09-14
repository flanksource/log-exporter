package opensearch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/flanksource/commons/logger"
	opensearch "github.com/opensearch-project/opensearch-go/v2"
)

type ImportData struct {
	Metadata map[string]interface{}     `json:"metadata,omitempty"`
	Indices  map[string]IndexDefinition `json:"indices,omitempty"`
	Data     []map[string]interface{}   `json:"data,omitempty"`
	// For simple array files like traces.json
	Documents []map[string]interface{} `json:"-"`
}

type IndexDefinition struct {
	Name      string                   `json:"name"`
	Settings  map[string]interface{}   `json:"settings"`
	Mappings  map[string]interface{}   `json:"mappings"`
	Documents []map[string]interface{} `json:"documents,omitempty"`
}

func (c *Client) ImportSamples(opts ImportOptions) error {
	if c.config.Verbose {
		logger.Infof("Loading sample data from: %s\n", opts.SampleFile)
	}

	// Load sample file
	importData, err := c.loadSampleFile(opts.SampleFile)
	if err != nil {
		return fmt.Errorf("failed to load sample file: %w", err)
	}

	// Get OpenSearch client
	osClient, err := c.GetClient()
	if err != nil {
		return fmt.Errorf("failed to get OpenSearch client: %w", err)
	}

	ctx := context.Background()

	// Handle structured sample files with indices
	if len(importData.Indices) > 0 {
		for indexName, indexDef := range importData.Indices {
			if c.config.Verbose {
				logger.Infof("Processing index: %s\n", indexName)
			}

			// Clean index definition to exclude unwanted attributes
			cleanedIndexDef := c.cleanIndexDefinition(indexDef)

			// Create index with mappings
			err := c.createIndexWithMappings(ctx, osClient, indexName, cleanedIndexDef, opts.Force)
			if err != nil {
				return fmt.Errorf("failed to create index %s: %w", indexName, err)
			}

			// Import documents if present in this index
			if len(indexDef.Documents) > 0 {
				if c.config.Verbose {
					logger.Infof("Importing %d documents into %s\n", len(indexDef.Documents), indexName)
				}
				err := c.bulkImport(ctx, osClient, indexName, indexDef.Documents, opts.BatchSize)
				if err != nil {
					return fmt.Errorf("failed to import documents into %s: %w", indexName, err)
				}
			}
		}
	}

	// Import data if present
	if len(importData.Data) > 0 {
		// For structured files, import data into all indices
		for indexName := range importData.Indices {
			if c.config.Verbose {
				logger.Infof("Importing %d documents into %s\n", len(importData.Data), indexName)
			}
			err := c.bulkImport(ctx, osClient, indexName, importData.Data, opts.BatchSize)
			if err != nil {
				return fmt.Errorf("failed to import data into %s: %w", indexName, err)
			}
		}
	} else if len(importData.Documents) > 0 {
		// For simple array files, create index based on filename and import data
		indexName := c.generateIndexNameFromFile(opts.SampleFile)

		if c.config.Verbose {
			logger.Infof("Creating index %s for data-only file\n", indexName)
		}

		// Auto-generate mapping from data
		mapping, err := c.GenerateMappingFromData(importData.Documents)
		if err != nil {
			return fmt.Errorf("failed to generate mapping: %w", err)
		}

		// Create index definition
		indexDef := IndexDefinition{
			Name: indexName,
			Settings: map[string]interface{}{
				"index": map[string]interface{}{
					"number_of_shards":   1,
					"number_of_replicas": 0,
				},
			},
			Mappings: mapping,
		}

		// Create index
		err = c.createIndexWithMappings(ctx, osClient, indexName, indexDef, opts.Force)
		if err != nil {
			return fmt.Errorf("failed to create index %s: %w", indexName, err)
		}

		// Import data
		if c.config.Verbose {
			logger.Infof("Importing %d documents into %s\n", len(importData.Documents), indexName)
		}
		err = c.bulkImport(ctx, osClient, indexName, importData.Documents, opts.BatchSize)
		if err != nil {
			return fmt.Errorf("failed to import data into %s: %w", indexName, err)
		}
	}

	if c.config.Verbose {
		logger.Infof("Import completed successfully\n")
	}
	return nil
}

func (c *Client) loadSampleFile(filePath string) (*ImportData, error) {
	data, err := readFileContents(filePath)
	if err != nil {
		return nil, err
	}

	var importData ImportData

	// Try to parse as structured sample file first
	err = json.Unmarshal(data, &importData)
	if err == nil && len(importData.Indices) > 0 {
		// Successfully parsed as structured file
		return &importData, nil
	}

	// Try to parse as simple array
	var documents []map[string]interface{}
	err = json.Unmarshal(data, &documents)
	if err != nil {
		return nil, fmt.Errorf("failed to parse sample file as JSON: %w", err)
	}

	importData.Documents = documents
	return &importData, nil
}

func (c *Client) createIndexWithMappings(ctx context.Context, osClient *opensearch.Client, indexName string, indexDef IndexDefinition, force bool) error {
	// Check if index exists
	res, err := osClient.Indices.Exists(
		[]string{indexName},
	)
	if err != nil {
		return fmt.Errorf("failed to check if index exists: %w", err)
	}

	indexExists := res.StatusCode == 200

	if indexExists {
		if force {
			if c.config.Verbose {
				logger.Infof("Deleting existing index: %s\n", indexName)
			}
			// Delete existing index
			res, err := osClient.Indices.Delete(
				[]string{indexName},
			)
			if err != nil {
				return fmt.Errorf("failed to delete existing index: %w", err)
			}
			if res.IsError() {
				return fmt.Errorf("failed to delete existing index: %s", res.String())
			}
		} else {
			if c.config.Verbose {
				logger.Infof("Index %s already exists, skipping creation\n", indexName)
			}
			return nil
		}
	}

	// Create index with settings and mappings
	createReq := map[string]interface{}{
		"settings": indexDef.Settings,
		"mappings": indexDef.Mappings,
	}

	reqBody, err := json.Marshal(createReq)
	if err != nil {
		return fmt.Errorf("failed to marshal create request: %w", err)
	}

	if c.config.Debug {
		logger.Infof("DEBUG: Creating index %s with definition:\n%s\n", indexName, string(reqBody))
	}

	res, err = osClient.Indices.Create(
		indexName,
		osClient.Indices.Create.WithBody(bytes.NewReader(reqBody)),
	)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}

	if res.IsError() {
		bodyBytes, _ := io.ReadAll(res.Body)
		return fmt.Errorf("failed to create index: %s - %s", res.String(), string(bodyBytes))
	}

	if c.config.Verbose {
		logger.Infof("Successfully created index: %s\n", indexName)
	}
	return nil
}

func (c *Client) bulkImport(ctx context.Context, osClient *opensearch.Client, indexName string, documents []map[string]interface{}, batchSize int) error {
	total := len(documents)
	imported := 0

	for i := 0; i < total; i += batchSize {
		end := i + batchSize
		if end > total {
			end = total
		}

		batch := documents[i:end]

		// Preprocess documents to handle @json/@input fields
		processedBatch := make([]map[string]interface{}, len(batch))
		for j, doc := range batch {
			processedBatch[j] = c.PreprocessDocument(doc)
		}

		// Build bulk request body
		var bulkBody strings.Builder
		for _, doc := range processedBatch {
			// Add action line
			action := map[string]interface{}{
				"index": map[string]interface{}{
					"_index": indexName,
				},
			}
			actionJSON, _ := json.Marshal(action)
			bulkBody.Write(actionJSON)
			bulkBody.WriteString("\n")

			// Add document line
			docJSON, _ := json.Marshal(doc)
			bulkBody.Write(docJSON)
			bulkBody.WriteString("\n")
		}

		// Execute bulk request
		res, err := osClient.Bulk(
			strings.NewReader(bulkBody.String()),
		)
		if err != nil {
			return fmt.Errorf("bulk import failed: %w", err)
		}

		if res.IsError() {
			bodyBytes, _ := io.ReadAll(res.Body)
			return fmt.Errorf("bulk import failed: %s - %s", res.String(), string(bodyBytes))
		}

		// Parse response to check for errors
		var bulkResponse map[string]interface{}
		if err := json.NewDecoder(res.Body).Decode(&bulkResponse); err == nil {
			if errors, hasErrors := bulkResponse["errors"].(bool); hasErrors && errors {
				if c.config.Debug {
					logger.Infof("DEBUG: Some documents failed to import in this batch\n")
				}
			}
		}

		imported += len(batch)
		if c.config.Verbose {
			logger.Infof("Progress: %d/%d documents imported\n", imported, total)
		}

		// Small delay between batches to avoid overwhelming the cluster
		if i+batchSize < total {
			time.Sleep(100 * time.Millisecond)
		}
	}

	if c.config.Verbose {
		logger.Infof("Successfully imported %d documents into %s\n", imported, indexName)
	}
	return nil
}

func (c *Client) PreprocessDocument(doc map[string]interface{}) map[string]interface{} {
	processed := make(map[string]interface{})

	for key, value := range doc {
		// Skip provided_name, uid, and version attributes
		if key == "provided_name" || key == "uid" || key == "version" {
			continue
		}
		
		// Check if field name ends with @json or @input
		if strings.HasSuffix(key, "@json") || strings.HasSuffix(key, "@input") {
			// Only attempt to unmarshal string values
			if strValue, ok := value.(string); ok {
				// Attempt to unmarshal the JSON string
				var jsonValue interface{}
				if err := json.Unmarshal([]byte(strValue), &jsonValue); err == nil {
					// Successfully unmarshalled, replace the value
					processed[key] = jsonValue
					continue
				}
			}
		}
		// Keep original value if not processed
		processed[key] = value
	}

	return processed
}

func (c *Client) cleanIndexDefinition(indexDef IndexDefinition) IndexDefinition {
	cleaned := IndexDefinition{
		Name:      indexDef.Name,
		Settings:  c.cleanSettings(indexDef.Settings),
		Mappings:  c.cleanMappings(indexDef.Mappings),
		Documents: indexDef.Documents, // Preserve documents, filtering happens during preprocessing
	}
	return cleaned
}

func (c *Client) cleanSettings(settings map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})
	for key, value := range settings {
		if key == "index" {
			if indexSettings, ok := value.(map[string]interface{}); ok {
				cleanedIndex := make(map[string]interface{})
				for indexKey, indexValue := range indexSettings {
					// Skip provided_name, uid, uuid, and version from index settings
					if indexKey == "provided_name" || indexKey == "uid" || indexKey == "uuid" || indexKey == "version" || indexKey == "creation_date" {
						continue
					}
					cleanedIndex[indexKey] = indexValue
				}
				cleaned[key] = cleanedIndex
			} else {
				cleaned[key] = value
			}
		} else {
			cleaned[key] = value
		}
	}
	return cleaned
}

func (c *Client) cleanMappings(mappings map[string]interface{}) map[string]interface{} {
	cleaned := make(map[string]interface{})
	for key, value := range mappings {
		if key == "properties" {
			if properties, ok := value.(map[string]interface{}); ok {
				cleanedProperties := make(map[string]interface{})
				for propKey, propValue := range properties {
					// Skip provided_name, uid, and version from mapping properties
					if propKey == "provided_name" || propKey == "uid" || propKey == "version" {
						continue
					}
					cleanedProperties[propKey] = propValue
				}
				cleaned[key] = cleanedProperties
			} else {
				cleaned[key] = value
			}
		} else {
			cleaned[key] = value
		}
	}
	return cleaned
}

func (c *Client) GenerateMappingFromData(documents []map[string]interface{}) (map[string]interface{}, error) {
	if len(documents) == 0 {
		return map[string]interface{}{"properties": map[string]interface{}{}}, nil
	}

	fieldTypes := make(map[string]string)

	// Sample first few documents to determine field types
	sampleSize := len(documents)
	if sampleSize > 10 {
		sampleSize = 10
	}

	for i := 0; i < sampleSize; i++ {
		doc := documents[i]
		for key, value := range doc {
			// Skip provided_name, uid, and version attributes from mapping
			if key == "provided_name" || key == "uid" || key == "version" {
				continue
			}
			if _, exists := fieldTypes[key]; !exists {
				fieldTypes[key] = c.inferFieldType(key, value)
			}
		}
	}

	// Build mapping properties
	properties := make(map[string]interface{})
	for field, fieldType := range fieldTypes {
		switch fieldType {
		case "date":
			if field == "startTimeMillis" {
				properties[field] = map[string]interface{}{
					"type":   "date",
					"format": "epoch_millis",
				}
			} else {
				properties[field] = map[string]interface{}{
					"type": "date",
				}
			}
		case "object":
			properties[field] = map[string]interface{}{
				"type": "object",
			}
		case "keyword":
			properties[field] = map[string]interface{}{
				"type": "keyword",
			}
		case "text":
			properties[field] = map[string]interface{}{
				"type": "text",
				"fields": map[string]interface{}{
					"keyword": map[string]interface{}{
						"type":         "keyword",
						"ignore_above": 256,
					},
				},
			}
		case "long":
			properties[field] = map[string]interface{}{
				"type": "long",
			}
		case "double":
			properties[field] = map[string]interface{}{
				"type": "double",
			}
		case "boolean":
			properties[field] = map[string]interface{}{
				"type": "boolean",
			}
		}
	}

	return map[string]interface{}{
		"properties": properties,
	}, nil
}

func (c *Client) inferFieldType(fieldName string, value interface{}) string {
	// Check field name patterns first
	if strings.Contains(strings.ToLower(fieldName), "time") ||
		strings.Contains(strings.ToLower(fieldName), "timestamp") ||
		fieldName == "@timestamp" {
		return "date"
	}

	// Check for JSON fields
	if strings.HasSuffix(fieldName, "@json") || strings.HasSuffix(fieldName, "@input") {
		return "object"
	}

	// Check field name patterns for keywords
	if strings.Contains(strings.ToLower(fieldName), "id") ||
		strings.Contains(strings.ToLower(fieldName), "name") ||
		strings.HasSuffix(fieldName, "Name") {
		return "keyword"
	}

	// Infer from value type
	switch v := value.(type) {
	case string:
		// Long strings are better as text, short ones as keywords
		if len(v) > 50 || strings.Contains(v, " ") {
			return "text"
		}
		return "keyword"
	case int, int64:
		return "long"
	case float64:
		return "double"
	case bool:
		return "boolean"
	case map[string]interface{}:
		return "object"
	default:
		return "keyword"
	}
}

func (c *Client) generateIndexNameFromFile(filePath string) string {
	// Extract filename without extension
	parts := strings.Split(filePath, "/")
	filename := parts[len(parts)-1]

	// Remove extension
	if dotIndex := strings.LastIndex(filename, "."); dotIndex != -1 {
		filename = filename[:dotIndex]
	}

	// Make it a valid index name
	indexName := strings.ToLower(filename)
	indexName = strings.ReplaceAll(indexName, "_", "-")

	// Add timestamp to make it unique
	timestamp := time.Now().Format("2006-01-02")
	return fmt.Sprintf("%s-%s", indexName, timestamp)
}

func readFileContents(filePath string) ([]byte, error) {
	// This would be implemented to read file contents
	// For now, using a simple implementation
	file, err := os.Open(filePath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	return io.ReadAll(file)
}
