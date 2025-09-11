package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/samber/lo"
)

// SampleOptions represents options for sample export
type SampleOptions struct {
	Index      string
	From       string
	To         string
	SampleSize int
	OutputDir  string
	Filters    FilterOptions
}

// SampleData represents the structure of exported sample data
type SampleData struct {
	Metadata  SampleMetadata         `json:"metadata"`
	Indices   map[string]IndexSample `json:"indices"`
	CreatedAt time.Time              `json:"created_at"`
}

// SampleMetadata contains information about the sample export
type SampleMetadata struct {
	Host       string    `json:"host"`
	Pattern    string    `json:"pattern"`
	From       string    `json:"from,omitempty"`
	To         string    `json:"to,omitempty"`
	SampleSize int       `json:"sample_size"`
	ExportedAt time.Time `json:"exported_at"`
}

// IndexSample represents sample data from a single index
type IndexSample struct {
	Name      string                   `json:"name"`
	Settings  map[string]interface{}   `json:"settings"`
	Mappings  map[string]interface{}   `json:"mappings"`
	Documents []map[string]interface{} `json:"documents"`
	Count     int                      `json:"count"`
}

// ExportSample exports sample data from OpenSearch indices for testing purposes
func (c *Client) ExportSample(opts SampleOptions) error {
	if c.config.Verbose {
		fmt.Printf("Starting sample export for pattern: %s\n", opts.Index)
		if opts.From != "" || opts.To != "" {
			fmt.Printf("Date range: %s to %s\n", opts.From, opts.To)
		}
	}

	// Resolve indices from pattern
	indices, err := c.ResolveIndices(opts.Index, opts.From, opts.To)
	if err != nil {
		return fmt.Errorf("failed to resolve indices: %w", err)
	}

	if len(indices) == 0 {
		return fmt.Errorf("no indices found matching pattern '%s'", opts.Index)
	}

	if c.config.Verbose {
		fmt.Printf("Found %d indices: %v\n", len(indices), indices)
	}

	// Create output directory
	if err := os.MkdirAll(opts.OutputDir, 0755); err != nil {
		return fmt.Errorf("failed to create output directory '%s': %w", opts.OutputDir, err)
	}

	// Initialize sample data
	sampleData := SampleData{
		Metadata: SampleMetadata{
			Host:       c.config.Host,
			Pattern:    opts.Index,
			From:       opts.From,
			To:         opts.To,
			SampleSize: opts.SampleSize,
			ExportedAt: time.Now(),
		},
		Indices:   make(map[string]IndexSample),
		CreatedAt: time.Now(),
	}

	// Export sample from each index
	for _, indexName := range indices {
		if c.config.Verbose {
			fmt.Printf("Exporting sample from index: %s\n", indexName)
		}

		sample, err := c.exportIndexSample(indexName, opts)
		if err != nil {
			if c.config.Verbose {
				fmt.Printf("Warning: failed to export from index '%s': %v\n", indexName, err)
			}
			continue
		}

		sampleData.Indices[indexName] = *sample
	}

	if len(sampleData.Indices) == 0 {
		return fmt.Errorf("no data could be exported from any indices")
	}

	// Write sample data to file
	outputFile := filepath.Join(opts.OutputDir, "sample-data.json")
	if err := c.writeSampleData(sampleData, outputFile); err != nil {
		return fmt.Errorf("failed to write sample data: %w", err)
	}

	// Create testcontainers import script
	if err := c.createImportScript(opts.OutputDir); err != nil {
		return fmt.Errorf("failed to create import script: %w", err)
	}

	fmt.Printf("Sample data exported successfully to: %s\n", opts.OutputDir)
	fmt.Printf("Exported %d indices with %d total documents\n",
		len(sampleData.Indices),
		lo.SumBy(lo.Values(sampleData.Indices), func(s IndexSample) int { return len(s.Documents) }))

	return nil
}

// exportIndexSample exports sample data from a single index
func (c *Client) exportIndexSample(indexName string, opts SampleOptions) (*IndexSample, error) {
	// Get index metadata
	indexMetadata, err := c.GetIndexMetadata(indexName)
	if err != nil {
		return nil, fmt.Errorf("failed to get index metadata: %w", err)
	}

	// Build query with filters
	query := "*"
	var filterConstraints []FilterConstraint

	if len(opts.Filters.K8sNamespace) > 0 || len(opts.Filters.K8sPod) > 0 ||
		len(opts.Filters.K8sDeployment) > 0 || len(opts.Filters.OtelService) > 0 ||
		len(opts.Filters.OtelOperation) > 0 {

		// Get index info to get available fields
		indexInfo, err := c.InspectIndex(indexName)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect index: %w", err)
		}

		// Use existing field mappings
		mapping := GetFieldMappings(indexInfo.Type, indexInfo.AvailableFields)
		filterConstraints = BuildFilterConstraints(opts.Filters, mapping)
	}

	// Add date range to query if specified
	if opts.From != "" || opts.To != "" {
		query, err = c.buildDateRangeQuery(opts.From, opts.To)
		if err != nil {
			return nil, fmt.Errorf("failed to build date range query: %w", err)
		}
	}

	// Add filters to query
	if len(filterConstraints) > 0 {
		query, err = AddFiltersToQuery(query, filterConstraints)
		if err != nil {
			return nil, fmt.Errorf("failed to add filters to query: %w", err)
		}
	}

	// Search for sample documents
	searchReq := map[string]interface{}{
		"size": opts.SampleSize,
		"sort": []map[string]interface{}{
			{"@timestamp": map[string]string{"order": "desc"}},
		},
	}

	// Add query if not wildcard
	if query != "*" {
		var queryObj interface{}
		if err := json.Unmarshal([]byte(query), &queryObj); err != nil {
			// Treat as Lucene query string
			searchReq["query"] = map[string]interface{}{
				"query_string": map[string]interface{}{
					"query": query,
				},
			}
		} else {
			// Treat as JSON query
			searchReq["query"] = queryObj
		}
	}

	// Execute search
	searchBody, err := json.Marshal(searchReq)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal search request: %w", err)
	}

	// Get OpenSearch client
	client, err := c.GetClient()
	if err != nil {
		return nil, fmt.Errorf("failed to get OpenSearch client: %w", err)
	}

	resp, err := client.Search(
		client.Search.WithContext(context.Background()),
		client.Search.WithIndex(indexName),
		client.Search.WithBody(strings.NewReader(string(searchBody))),
	)
	if err != nil {
		return nil, fmt.Errorf("search failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.IsError() {
		return nil, fmt.Errorf("search error: %s", resp.String())
	}

	var searchResult struct {
		Hits struct {
			Total struct {
				Value int `json:"value"`
			} `json:"total"`
			Hits []struct {
				Source map[string]interface{} `json:"_source"`
			} `json:"hits"`
		} `json:"hits"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&searchResult); err != nil {
		return nil, fmt.Errorf("failed to decode search response: %w", err)
	}

	// Extract documents
	var documents []map[string]interface{}
	for _, hit := range searchResult.Hits.Hits {
		documents = append(documents, hit.Source)
	}

	return &IndexSample{
		Name:      indexName,
		Settings:  indexMetadata.Settings,
		Mappings:  indexMetadata.Mappings,
		Documents: documents,
		Count:     searchResult.Hits.Total.Value,
	}, nil
}

// buildDateRangeQuery builds a query with date range constraints
func (c *Client) buildDateRangeQuery(from, to string) (string, error) {
	if from == "" && to == "" {
		return "*", nil
	}

	rangeQuery := map[string]interface{}{
		"range": map[string]interface{}{
			"@timestamp": map[string]interface{}{},
		},
	}

	timestampRange := rangeQuery["range"].(map[string]interface{})["@timestamp"].(map[string]interface{})

	if from != "" {
		timestampRange["gte"] = from
	}
	if to != "" {
		timestampRange["lte"] = to
	}

	queryBytes, err := json.Marshal(rangeQuery)
	if err != nil {
		return "", fmt.Errorf("failed to marshal date range query: %w", err)
	}

	return string(queryBytes), nil
}

// writeSampleData writes the sample data to a JSON file
func (c *Client) writeSampleData(data SampleData, outputFile string) error {
	file, err := os.Create(outputFile)
	if err != nil {
		return fmt.Errorf("failed to create output file: %w", err)
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")

	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("failed to encode sample data: %w", err)
	}

	return nil
}

// createImportScript creates a Go script for importing sample data into testcontainers
func (c *Client) createImportScript(outputDir string) error {
	scriptContent := `package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/opensearch-project/opensearch-go/v2"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/opensearch"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run import.go <sample-data.json>")
		os.Exit(1)
	}

	sampleFile := os.Args[1]
	
	// Load sample data
	data, err := loadSampleData(sampleFile)
	if err != nil {
		fmt.Printf("Failed to load sample data: %v\n", err)
		os.Exit(1)
	}

	// Start OpenSearch container
	ctx := context.Background()
	container, err := opensearch.Run(ctx,
		"opensearchproject/opensearch:2.11.0",
		opensearch.WithUsername("admin"),
		opensearch.WithPassword("admin"),
		testcontainers.WithEnv(map[string]string{
			"discovery.type": "single-node",
			"OPENSEARCH_JAVA_OPTS": "-Xms1g -Xmx1g",
		}),
	)
	if err != nil {
		fmt.Printf("Failed to start OpenSearch container: %v\n", err)
		os.Exit(1)
	}
	defer container.Terminate(ctx)

	// Get connection details
	host, err := container.Host(ctx)
	if err != nil {
		fmt.Printf("Failed to get container host: %v\n", err)
		os.Exit(1)
	}

	port, err := container.MappedPort(ctx, "9200")
	if err != nil {
		fmt.Printf("Failed to get container port: %v\n", err)
		os.Exit(1)
	}

	// Create OpenSearch client
	client, err := opensearch.NewClient(opensearch.Config{
		Addresses: []string{fmt.Sprintf("http://%s:%s", host, port.Port())},
		Username:  "admin",
		Password:  "admin",
	})
	if err != nil {
		fmt.Printf("Failed to create OpenSearch client: %v\n", err)
		os.Exit(1)
	}

	// Import data
	fmt.Printf("Importing sample data into OpenSearch at %s:%s\n", host, port.Port())
	
	for indexName, indexSample := range data.Indices {
		if err := importIndexSample(ctx, client, indexName, indexSample); err != nil {
			fmt.Printf("Failed to import index %s: %v\n", indexName, err)
			continue
		}
		fmt.Printf("Imported %d documents to index %s\n", len(indexSample.Documents), indexName)
	}

	fmt.Printf("Import completed. OpenSearch is running at http://%s:%s\n", host, port.Port())
	fmt.Println("Press Enter to stop the container...")
	fmt.Scanln()
}

type SampleData struct {
	Indices map[string]IndexSample ` + "`json:\"indices\"`" + `
}

type IndexSample struct {
	Settings  map[string]interface{}   ` + "`json:\"settings\"`" + `
	Mappings  map[string]interface{}   ` + "`json:\"mappings\"`" + `
	Documents []map[string]interface{} ` + "`json:\"documents\"`" + `
}

func loadSampleData(filename string) (*SampleData, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var data SampleData
	if err := json.NewDecoder(file).Decode(&data); err != nil {
		return nil, err
	}

	return &data, nil
}

func importIndexSample(ctx context.Context, client *opensearch.Client, indexName string, sample IndexSample) error {
	// Create index with mappings and settings
	createReq := map[string]interface{}{
		"settings": sample.Settings,
		"mappings": sample.Mappings,
	}

	createBody, err := json.Marshal(createReq)
	if err != nil {
		return fmt.Errorf("failed to marshal create request: %w", err)
	}

	resp, err := client.Indices.Create(
		indexName,
		client.Indices.Create.WithBody(strings.NewReader(string(createBody))),
	)
	if err != nil {
		return fmt.Errorf("failed to create index: %w", err)
	}
	defer resp.Body.Close()

	if resp.IsError() && !strings.Contains(resp.String(), "already_exists") {
		return fmt.Errorf("failed to create index: %s", resp.String())
	}

	// Bulk import documents
	if len(sample.Documents) > 0 {
		var bulkBody strings.Builder
		for _, doc := range sample.Documents {
			// Index action
			action := map[string]interface{}{
				"index": map[string]interface{}{
					"_index": indexName,
				},
			}
			actionBytes, _ := json.Marshal(action)
			bulkBody.Write(actionBytes)
			bulkBody.WriteByte('\n')

			// Document
			docBytes, _ := json.Marshal(doc)
			bulkBody.Write(docBytes)
			bulkBody.WriteByte('\n')
		}

		bulkResp, err := client.Bulk(strings.NewReader(bulkBody.String()))
		if err != nil {
			return fmt.Errorf("bulk import failed: %w", err)
		}
		defer bulkResp.Body.Close()

		if bulkResp.IsError() {
			return fmt.Errorf("bulk import error: %s", bulkResp.String())
		}
	}

	return nil
}
`

	scriptFile := filepath.Join(outputDir, "import.go")
	if err := os.WriteFile(scriptFile, []byte(scriptContent), 0644); err != nil {
		return fmt.Errorf("failed to write import script: %w", err)
	}

	// Create go.mod for the import script
	modContent := `module opensearch-import

go 1.21

require (
	github.com/opensearch-project/opensearch-go/v2 v2.3.0
	github.com/testcontainers/testcontainers-go v0.24.1
	github.com/testcontainers/testcontainers-go/modules/opensearch v0.24.1
)
`

	modFile := filepath.Join(outputDir, "go.mod")
	if err := os.WriteFile(modFile, []byte(modContent), 0644); err != nil {
		return fmt.Errorf("failed to write go.mod: %w", err)
	}

	return nil
}
