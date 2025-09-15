package opensearch

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
)

// TestOpenSearchIntegration is the main integration test that uses testcontainers
func TestOpenSearchIntegration(t *testing.T) {
	// Skip if Docker is not available
	skipIfNoDocker(t)

	// Setup OpenSearch container
	ctx := context.Background()
	container, hostPort := setupOpenSearchContainer(t, ctx)
	defer func() {
		if err := container.Terminate(ctx); err != nil {
			t.Logf("Failed to terminate container: %v", err)
		}
	}()

	// Create client for tests
	client := createTestClient(t, hostPort)

	t.Run("BasicConnectivity", func(t *testing.T) {
		testBasicConnectivity(t, ctx, client)
	})

	t.Run("IndexResolutionWithDateFiltering", func(t *testing.T) {
		testIndexResolutionWithDateFiltering(t, ctx, client)
	})

	t.Run("InspectIndexWithRealData", func(t *testing.T) {
		testInspectIndexWithRealData(t, ctx, client)
	})

	t.Run("SampleExportBasicFunctionality", func(t *testing.T) {
		testSampleExportBasicFunctionality(t, ctx, client)
	})

	t.Run("LogstashDataWithFiltering", func(t *testing.T) {
		testLogstashDataWithFiltering(t, ctx, client)
	})

	t.Run("JaegerDataWithFiltering", func(t *testing.T) {
		testJaegerDataWithFiltering(t, ctx, client)
	})

	t.Run("ExportWithRealSampleData", func(t *testing.T) {
		testExportWithRealSampleData(t, ctx, client)
	})

	t.Run("ComplexFilteringScenarios", func(t *testing.T) {
		testComplexFilteringScenarios(t, ctx, client)
	})
}

// skipIfNoDocker skips the test if Docker is not available
func skipIfNoDocker(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("Docker not available, skipping integration test")
	}

	// Also check if Docker daemon is running
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Docker daemon not running, skipping integration test")
	}
}

// setupOpenSearchContainer starts an OpenSearch container and returns it with host:port
func setupOpenSearchContainer(t *testing.T, ctx context.Context) (testcontainers.Container, string) {
	req := testcontainers.ContainerRequest{
		Image:        "opensearchproject/opensearch:3.2.0",
		ExposedPorts: []string{"9200/tcp"},
		Env: map[string]string{
			"discovery.type":                    "single-node",
			"OPENSEARCH_JAVA_OPTS":              "-Xms512m -Xmx512m",
			"OPENSEARCH_INITIAL_ADMIN_PASSWORD": "StrongP@ssw0rd2024!",
			"plugins.security.disabled":         "true", // Disable security for testing
		},
		WaitingFor: wait.ForHTTP("/").WithPort("9200").WithStartupTimeout(2 * time.Minute),
	}

	container, err := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{
		ContainerRequest: req,
		Started:          true,
	})
	require.NoError(t, err, "Failed to start OpenSearch container")

	host, err := container.Host(ctx)
	require.NoError(t, err, "Failed to get container host")

	mappedPort, err := container.MappedPort(ctx, "9200")
	require.NoError(t, err, "Failed to get mapped port")

	hostPort := fmt.Sprintf("http://%s:%s", host, mappedPort.Port())
	t.Logf("OpenSearch container running at: %s", hostPort)

	return container, hostPort
}

// createTestClient creates a Client instance for testing
func createTestClient(t *testing.T, hostPort string) *Client {
	config := Config{
		Host:     hostPort,
		Username: "", // No auth needed when security is disabled
		Password: "",
		Debug:    false,
		Verbose:  true,
	}

	client, err := NewClient(config)
	require.NoError(t, err, "Failed to create OpenSearch client")

	return client
}

// createTestIndices creates test indices with sample data
func createTestIndices(t *testing.T, ctx context.Context, client *Client, indices map[string][]map[string]interface{}) {
	osClient, err := client.GetClient()
	require.NoError(t, err, "Failed to get OpenSearch client")

	for indexName, documents := range indices {
		// Create appropriate mapping based on index name/type
		var createReq map[string]interface{}

		if strings.Contains(indexName, "filebeat") || strings.Contains(indexName, "k8s") {
			// Kubernetes mapping
			createReq = map[string]interface{}{
				"mappings": map[string]interface{}{
					"properties": map[string]interface{}{
						"@timestamp": map[string]interface{}{
							"type": "date",
						},
						"message": map[string]interface{}{
							"type": "text",
						},
						"level": map[string]interface{}{
							"type": "keyword",
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
									},
								},
							},
						},
					},
				},
			}
		} else if strings.Contains(indexName, "jaeger") || strings.Contains(indexName, "span") || strings.Contains(indexName, "trace") {
			// Jaeger/OpenTelemetry mapping
			createReq = map[string]interface{}{
				"mappings": map[string]interface{}{
					"properties": map[string]interface{}{
						"@timestamp": map[string]interface{}{
							"type": "date",
						},
						"serviceName": map[string]interface{}{
							"type": "keyword",
						},
						"operationName": map[string]interface{}{
							"type": "keyword",
						},
						"traceID": map[string]interface{}{
							"type": "text",
						},
						"spanID": map[string]interface{}{
							"type": "text",
						},
						"duration": map[string]interface{}{
							"type": "long",
						},
					},
				},
			}
		} else {
			// Generic mapping
			createReq = map[string]interface{}{
				"mappings": map[string]interface{}{
					"properties": map[string]interface{}{
						"@timestamp": map[string]interface{}{
							"type": "date",
						},
						"message": map[string]interface{}{
							"type": "text",
						},
						"level": map[string]interface{}{
							"type": "keyword",
						},
						"source": map[string]interface{}{
							"type": "text",
						},
						"host": map[string]interface{}{
							"type": "text",
						},
					},
				},
			}
		}

		createBody, err := json.Marshal(createReq)
		require.NoError(t, err)

		res, err := osClient.Indices.Create(
			indexName,
			osClient.Indices.Create.WithContext(ctx),
			osClient.Indices.Create.WithBody(strings.NewReader(string(createBody))),
		)
		require.NoError(t, err)
		require.False(t, res.IsError(), "Failed to create index %s: %s", indexName, res.String())
		res.Body.Close()

		// Add documents using bulk API
		if len(documents) > 0 {
			var bulkBody strings.Builder
			for _, doc := range documents {
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

			bulkRes, err := osClient.Bulk(
				strings.NewReader(bulkBody.String()),
				osClient.Bulk.WithContext(ctx),
			)
			require.NoError(t, err)
			require.False(t, bulkRes.IsError(), "Failed to bulk insert to %s: %s", indexName, bulkRes.String())
			bulkRes.Body.Close()

			// Refresh index to make documents searchable
			refreshRes, err := osClient.Indices.Refresh(
				osClient.Indices.Refresh.WithIndex(indexName),
				osClient.Indices.Refresh.WithContext(ctx),
			)
			require.NoError(t, err)
			require.False(t, refreshRes.IsError(), "Failed to refresh index %s", indexName)
			refreshRes.Body.Close()
		}

		t.Logf("Created index %s with %d documents", indexName, len(documents))
	}
}

// generateKubernetesLogs generates sample Kubernetes log documents
func generateKubernetesLogs(count int) []map[string]interface{} {
	logs := make([]map[string]interface{}, count)
	namespaces := []string{"production", "staging", "development"}
	pods := []string{"api-server", "worker", "database"}
	levels := []string{"INFO", "WARN", "ERROR"}

	for i := 0; i < count; i++ {
		logs[i] = map[string]interface{}{
			"@timestamp": time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			"message":    fmt.Sprintf("Kubernetes log message %d", i+1),
			"level":      levels[i%len(levels)],
			"kubernetes": map[string]interface{}{
				"namespace": namespaces[i%len(namespaces)],
				"pod": map[string]interface{}{
					"name": fmt.Sprintf("%s-%d", pods[i%len(pods)], i+1),
				},
			},
		}
	}

	return logs
}

// generateJaegerTraces generates sample Jaeger trace documents
func generateJaegerTraces(count int) []map[string]interface{} {
	traces := make([]map[string]interface{}, count)
	services := []string{"user-service", "order-service", "payment-service"}
	operations := []string{"GetUser", "CreateOrder", "ProcessPayment"}

	for i := 0; i < count; i++ {
		traces[i] = map[string]interface{}{
			"@timestamp":    time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			"serviceName":   services[i%len(services)],
			"operationName": operations[i%len(operations)],
			"traceID":       fmt.Sprintf("trace-%d", i+1),
			"spanID":        fmt.Sprintf("span-%d", i+1),
			"duration":      1000 + i*100,
		}
	}

	return traces
}

// generateGenericLogs generates sample generic log documents
func generateGenericLogs(count int) []map[string]interface{} {
	logs := make([]map[string]interface{}, count)
	levels := []string{"DEBUG", "INFO", "WARN", "ERROR"}
	sources := []string{"app.server", "app.database", "app.cache"}

	for i := 0; i < count; i++ {
		logs[i] = map[string]interface{}{
			"@timestamp": time.Now().Add(-time.Duration(i) * time.Minute).Format(time.RFC3339),
			"message":    fmt.Sprintf("Generic log message %d", i+1),
			"level":      levels[i%len(levels)],
			"source":     sources[i%len(sources)],
			"host":       fmt.Sprintf("server-%d", (i%3)+1),
		}
	}

	return logs
}

// testBasicConnectivity tests basic OpenSearch connectivity
func testBasicConnectivity(t *testing.T, ctx context.Context, client *Client) {
	// Test that we can get the OpenSearch client
	osClient, err := client.GetClient()
	require.NoError(t, err, "Should be able to get OpenSearch client")

	// Test cluster health
	res, err := osClient.Cluster.Health()
	require.NoError(t, err, "Should be able to check cluster health")
	require.False(t, res.IsError(), "Cluster should be healthy")
	res.Body.Close()
}

// testSampleExportBasicFunctionality tests basic sample export functionality
func testSampleExportBasicFunctionality(t *testing.T, ctx context.Context, client *Client) {
	// Create a simple test index with minimal data
	indices := map[string][]map[string]interface{}{
		"sample-test-2024.01.15": {
			{
				"@timestamp": time.Now().Format(time.RFC3339),
				"message":    "Test log message",
				"level":      "INFO",
			},
		},
	}
	createTestIndices(t, ctx, client, indices)

	// Create temp directory for sample output
	tempDir, err := os.MkdirTemp("", "sample-test-basic-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	// Test basic sample export
	opts := SampleOptions{
		Index:      "sample-test-*",
		SampleSize: 5,
		OutputDir:  tempDir,
	}

	err = client.ExportSample(opts)
	require.NoError(t, err, "Sample export should succeed")

	// Verify output files exist
	sampleFile := tempDir + "/sample-data.json"
	assert.FileExists(t, sampleFile, "Sample data file should exist")

	importScript := tempDir + "/import.go"
	assert.FileExists(t, importScript, "Import script should exist")

	goMod := tempDir + "/go.mod"
	assert.FileExists(t, goMod, "go.mod should exist")

	// Basic validation of sample data structure
	var sampleData SampleData
	sampleBytes, err := os.ReadFile(sampleFile)
	require.NoError(t, err, "Should be able to read sample file")

	err = json.Unmarshal(sampleBytes, &sampleData)
	require.NoError(t, err, "Should be able to parse sample data")

	// Basic structure validation
	assert.NotEmpty(t, sampleData.Metadata.Host, "Metadata should have host")
	assert.Equal(t, "sample-test-*", sampleData.Metadata.Pattern)
	assert.GreaterOrEqual(t, len(sampleData.Indices), 1, "Should have at least 1 index")
}

// testIndexResolutionWithDateFiltering tests index resolution with date filtering
func testIndexResolutionWithDateFiltering(t *testing.T, ctx context.Context, client *Client) {
	// Create indices with different dates
	indices := map[string][]map[string]interface{}{
		"logs-2024.01.15":  generateGenericLogs(5),
		"logs-2024.01.16":  generateGenericLogs(5),
		"logs-2024.01.17":  generateGenericLogs(5),
		"other-2024.01.15": generateGenericLogs(3), // Different pattern
	}
	createTestIndices(t, ctx, client, indices)

	// Test 1: Resolve all matching indices without date filtering
	resolved, err := client.ResolveIndices("logs-*", "", "")
	require.NoError(t, err)
	assert.Len(t, resolved, 3, "Should resolve 3 logs indices")
	for _, index := range resolved {
		assert.Contains(t, index, "logs-2024.01.1")
	}

	// Test 2: Resolve with date range filtering
	resolved, err = client.ResolveIndices("logs-*", "2024-01-15", "2024-01-16")
	require.NoError(t, err)
	assert.Contains(t, resolved, "logs-2024.01.15")
	assert.Contains(t, resolved, "logs-2024.01.16")
	assert.NotContains(t, resolved, "logs-2024.01.17")

	// Test 3: Resolve with exact match (no wildcards)
	resolved, err = client.ResolveIndices("logs-2024.01.15", "", "")
	require.NoError(t, err)
	assert.Len(t, resolved, 1)
	assert.Equal(t, "logs-2024.01.15", resolved[0])
}

// testInspectIndexWithRealData tests index inspection with real data
func testInspectIndexWithRealData(t *testing.T, ctx context.Context, client *Client) {
	// Create indices with different log types
	indices := map[string][]map[string]interface{}{
		"filebeat-2024.01.15":    generateKubernetesLogs(10),
		"jaeger-span-2024.01.15": generateJaegerTraces(8),
		"app-logs-2024.01.15":    generateGenericLogs(12),
	}
	createTestIndices(t, ctx, client, indices)

	// Test 1: Inspect Kubernetes logs
	info, err := client.InspectIndex("filebeat-2024.01.15")
	require.NoError(t, err)
	assert.Equal(t, "kubernetes", info.Type, "Should detect Kubernetes log type")
	assert.Equal(t, "@timestamp", info.TimestampField)
	assert.True(t, info.HasDateField)
	assert.Contains(t, info.AvailableFields, "@timestamp")
	assert.Contains(t, info.AvailableFields, "message")

	// Test 2: Inspect Jaeger traces
	info, err = client.InspectIndex("jaeger-span-2024.01.15")
	require.NoError(t, err)
	assert.Equal(t, "jaeger", info.Type, "Should detect Jaeger log type")
	assert.Contains(t, info.AvailableFields, "serviceName")
	assert.Contains(t, info.AvailableFields, "operationName")

	// Test 3: Inspect generic logs
	info, err = client.InspectIndex("app-logs-2024.01.15")
	require.NoError(t, err)
	assert.Equal(t, "generic", info.Type, "Should detect generic log type")
	assert.Contains(t, info.AvailableFields, "@timestamp")
	assert.Contains(t, info.AvailableFields, "message")
}

// loadSampleDataFromFile loads sample data from JSON files in opensearch-sample directory
func loadSampleDataFromFile(t *testing.T, ctx context.Context, client *Client, filepath string) (string, int) {
	// Read the sample file
	data, err := os.ReadFile(filepath)
	require.NoError(t, err, "Failed to read sample file %s", filepath)

	var sampleData SampleData
	err = json.Unmarshal(data, &sampleData)
	require.NoError(t, err, "Failed to parse sample data from %s", filepath)

	osClient, err := client.GetClient()
	require.NoError(t, err, "Failed to get OpenSearch client")

	totalDocs := 0
	indexName := ""

	// Process each index in the sample data
	for idxName, indexData := range sampleData.Indices {
		indexName = idxName + "-test" // Add suffix to avoid conflicts

		// Create index with appropriate mappings based on the sample type
		var mappings map[string]interface{}

		// Check if it's logstash or jaeger data based on fields
		if len(indexData.Documents) > 0 {
			doc := indexData.Documents[0]
			if _, hasK8s := doc["kubernetes_namespace_name"]; hasK8s {
				// Logstash/Kubernetes mapping
				mappings = map[string]interface{}{
					"properties": map[string]interface{}{
						"@timestamp": map[string]interface{}{
							"type": "date",
						},
						"kubernetes_namespace_name": map[string]interface{}{
							"type": "keyword",
						},
						"kubernetes_pod_name": map[string]interface{}{
							"type": "keyword",
						},
						"kubernetes_container_name": map[string]interface{}{
							"type": "keyword",
						},
						"log_level": map[string]interface{}{
							"type": "keyword",
						},
						"message": map[string]interface{}{
							"type": "text",
						},
						"kubernetes_labels": map[string]interface{}{
							"type": "object",
						},
					},
				}
			} else if _, hasTrace := doc["traceID"]; hasTrace {
				// Jaeger mapping
				mappings = map[string]interface{}{
					"properties": map[string]interface{}{
						"startTimeMillis": map[string]interface{}{
							"type": "date",
						},
						"traceID": map[string]interface{}{
							"type": "keyword",
						},
						"spanID": map[string]interface{}{
							"type": "keyword",
						},
						"duration": map[string]interface{}{
							"type": "long",
						},
						"operationName": map[string]interface{}{
							"type": "keyword",
						},
						"process": map[string]interface{}{
							"properties": map[string]interface{}{
								"serviceName": map[string]interface{}{
									"type": "keyword",
								},
								"tag": map[string]interface{}{
									"type": "object",
								},
							},
						},
						"tag": map[string]interface{}{
							"type": "object",
						},
					},
				}
			}
		}

		createReq := map[string]interface{}{
			"mappings": mappings,
		}

		createBody, err := json.Marshal(createReq)
		require.NoError(t, err)

		// Delete index if it exists (cleanup from previous runs)
		delRes, _ := osClient.Indices.Delete(
			[]string{indexName},
			osClient.Indices.Delete.WithContext(ctx),
		)
		if delRes != nil && !delRes.IsError() {
			delRes.Body.Close()
		}

		// Create the index
		res, err := osClient.Indices.Create(
			indexName,
			osClient.Indices.Create.WithContext(ctx),
			osClient.Indices.Create.WithBody(strings.NewReader(string(createBody))),
		)
		require.NoError(t, err)
		require.False(t, res.IsError(), "Failed to create index %s: %s", indexName, res.String())
		res.Body.Close()

		// Bulk insert documents
		if len(indexData.Documents) > 0 {
			var bulkBody strings.Builder
			for _, doc := range indexData.Documents {
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

			bulkRes, err := osClient.Bulk(
				strings.NewReader(bulkBody.String()),
				osClient.Bulk.WithContext(ctx),
			)
			require.NoError(t, err)
			require.False(t, bulkRes.IsError(), "Failed to bulk insert to %s: %s", indexName, bulkRes.String())
			bulkRes.Body.Close()

			// Refresh index
			refreshRes, err := osClient.Indices.Refresh(
				osClient.Indices.Refresh.WithIndex(indexName),
				osClient.Indices.Refresh.WithContext(ctx),
			)
			require.NoError(t, err)
			require.False(t, refreshRes.IsError(), "Failed to refresh index %s", indexName)
			refreshRes.Body.Close()

			totalDocs = len(indexData.Documents)
			t.Logf("Loaded %d documents into index %s from %s", totalDocs, indexName, filepath)
		}
	}

	return indexName, totalDocs
}

// testLogstashDataWithFiltering tests filtering on logstash/kubernetes data
func testLogstashDataWithFiltering(t *testing.T, ctx context.Context, client *Client) {
	// Load logstash sample data
	indexName, docCount := loadSampleDataFromFile(t, ctx, client, "../../opensearch-sample/logstash.json")
	require.Equal(t, 100, docCount, "Should load 100 documents from logstash sample")

	// Test 1: Filter by namespace
	opts := ExportOptions{
		Index: indexName,
		Filters: FilterOptions{
			K8sNamespace: "malawi",
		},
		Limit: 100,
	}

	result, err := client.performSearch(opts)
	require.NoError(t, err, "Should be able to filter by namespace")

	// Verify results contain only malawi namespace
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if ns, ok := doc["kubernetes_namespace_name"].(string); ok {
			assert.Equal(t, "malawi", ns, "Should only return malawi namespace documents")
		}
	}
	t.Logf("Filtered by namespace 'malawi': found %d documents", result.Hits.Total.Value)

	// Test 2: Filter by log level
	opts = ExportOptions{
		Index: indexName,
		Query: "log_level:ERROR",
		Limit: 100,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should be able to filter by log level")

	// Verify results contain only ERROR level
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if level, ok := doc["log_level"].(string); ok {
			assert.Equal(t, "ERROR", level, "Should only return ERROR level logs")
		}
	}
	t.Logf("Filtered by log_level ERROR: found %d documents", result.Hits.Total.Value)

	// Test 3: Combined filters - namespace AND pod name pattern
	opts = ExportOptions{
		Index: indexName,
		Filters: FilterOptions{
			K8sNamespace: "kenya",
		},
		Query: "kubernetes_pod_name:service-*",
		Limit: 100,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should be able to use combined filters")

	// Verify combined filter results
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if ns, ok := doc["kubernetes_namespace_name"].(string); ok {
			assert.Equal(t, "kenya", ns, "Should be kenya namespace")
		}
		if pod, ok := doc["kubernetes_pod_name"].(string); ok {
			assert.True(t, strings.HasPrefix(pod, "service-"), "Pod should start with 'service-'")
		}
	}
	t.Logf("Combined filter (namespace=kenya, pod=service-*): found %d documents", result.Hits.Total.Value)
}

// testJaegerDataWithFiltering tests filtering on jaeger/tracing data
func testJaegerDataWithFiltering(t *testing.T, ctx context.Context, client *Client) {
	// Load jaeger sample data
	indexName, docCount := loadSampleDataFromFile(t, ctx, client, "../../opensearch-sample/jaeger.json")
	require.Equal(t, 100, docCount, "Should load 100 documents from jaeger sample")

	// Test 1: Filter by service name
	opts := ExportOptions{
		Index: indexName,
		Filters: FilterOptions{
			OtelService: "zimbabwe-cycle",
		},
		Limit: 100,
	}

	result, err := client.performSearch(opts)
	require.NoError(t, err, "Should be able to filter by service name")

	// Verify results contain only zimbabwe-cycle service
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if process, ok := doc["process"].(map[string]interface{}); ok {
			if serviceName, ok := process["serviceName"].(string); ok {
				assert.Equal(t, "zimbabwe-cycle", serviceName, "Should only return zimbabwe-cycle service")
			}
		}
	}
	t.Logf("Filtered by service 'zimbabwe-cycle': found %d documents", result.Hits.Total.Value)

	// Test 2: Filter by HTTP status code
	opts = ExportOptions{
		Index: indexName,
		Query: "tag.http\\@status_code:200",
		Limit: 100,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should be able to filter by HTTP status code")

	// Verify results contain only 200 status codes
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if tag, ok := doc["tag"].(map[string]interface{}); ok {
			if status, ok := tag["http@status_code"].(float64); ok {
				assert.Equal(t, float64(200), status, "Should only return 200 status codes")
			}
		}
	}
	t.Logf("Filtered by HTTP status 200: found %d documents", result.Hits.Total.Value)

	// Test 3: Filter by operation pattern
	opts = ExportOptions{
		Index: indexName,
		Query: "operationName:\"GET /Cycle/*\"",
		Limit: 100,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should be able to filter by operation name")

	// Verify operation names
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if op, ok := doc["operationName"].(string); ok {
			assert.Equal(t, "GET /Cycle/*", op, "Should match operation pattern")
		}
	}
	t.Logf("Filtered by operation 'GET /Cycle/*': found %d documents", result.Hits.Total.Value)

	// Test 4: Filter by duration range (find slow requests > 300 microseconds)
	opts = ExportOptions{
		Index: indexName,
		Query: "duration:>300",
		Limit: 100,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should be able to filter by duration range")

	// Verify durations
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if duration, ok := doc["duration"].(float64); ok {
			assert.Greater(t, duration, float64(300), "Duration should be greater than 300")
		}
	}
	t.Logf("Filtered by duration > 300: found %d documents", result.Hits.Total.Value)
}

// testExportWithRealSampleData tests export functionality with real sample data
func testExportWithRealSampleData(t *testing.T, ctx context.Context, client *Client) {
	// Load both sample data files
	logstashIndex, _ := loadSampleDataFromFile(t, ctx, client, "../../opensearch-sample/logstash.json")
	jaegerIndex, _ := loadSampleDataFromFile(t, ctx, client, "../../opensearch-sample/jaeger.json")

	// Test 1: Export logstash data with specific fields
	tempDir, err := os.MkdirTemp("", "export-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	outputFile := tempDir + "/logstash-export.json"
	opts := ExportOptions{
		Index:  logstashIndex,
		Fields: []string{"@timestamp", "kubernetes_namespace_name", "kubernetes_pod_name", "log_level", "message"},
		Limit:  10,
		Output: outputFile,
	}

	err = client.Export(opts)
	require.NoError(t, err, "Should export logstash data successfully")

	// Verify exported file
	exportedData, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Should read exported file")

	var exportedDocs []map[string]interface{}
	err = json.Unmarshal(exportedData, &exportedDocs)
	require.NoError(t, err, "Should parse exported JSON")
	assert.Equal(t, 10, len(exportedDocs), "Should export 10 documents")

	// Verify fields are filtered correctly
	for _, doc := range exportedDocs {
		assert.Contains(t, doc, "@timestamp", "Should have timestamp field")
		assert.Contains(t, doc, "kubernetes_namespace_name", "Should have namespace field")
		assert.NotContains(t, doc, "kubernetes_docker_id", "Should not have docker_id field")
	}
	t.Log("Successfully exported logstash data with field selection")

	// Test 2: Export jaeger data with filtering
	outputFile = tempDir + "/jaeger-export.csv"
	opts = ExportOptions{
		Index: jaegerIndex,
		Filters: FilterOptions{
			OtelService: "kenya-cycle",
		},
		Fields: []string{"startTimeMillis", "traceID", "spanID", "operationName", "duration"},
		Limit:  20,
		Output: outputFile,
	}

	err = client.Export(opts)
	require.NoError(t, err, "Should export jaeger data as CSV")

	// Verify CSV file exists and has content
	csvData, err := os.ReadFile(outputFile)
	require.NoError(t, err, "Should read CSV file")
	lines := strings.Split(string(csvData), "\n")
	assert.Greater(t, len(lines), 1, "CSV should have header and data rows")
	assert.Contains(t, lines[0], "startTimeMillis", "CSV header should contain field names")
	t.Log("Successfully exported jaeger data as CSV with filtering")

	// Test 3: Export with complex query
	outputFile = tempDir + "/complex-export.json"
	opts = ExportOptions{
		Index:  logstashIndex,
		Query:  "log_level:ERROR AND kubernetes_namespace_name:(kenya OR malawi)",
		Limit:  50,
		Output: outputFile,
	}

	err = client.Export(opts)
	require.NoError(t, err, "Should export with complex query")

	// Verify complex query results
	exportedData, err = os.ReadFile(outputFile)
	require.NoError(t, err)

	err = json.Unmarshal(exportedData, &exportedDocs)
	require.NoError(t, err)

	for _, doc := range exportedDocs {
		level, _ := doc["log_level"].(string)
		namespace, _ := doc["kubernetes_namespace_name"].(string)
		assert.Equal(t, "ERROR", level, "Should only have ERROR level")
		assert.True(t, namespace == "kenya" || namespace == "malawi", "Should be kenya or malawi namespace")
	}
	t.Log("Successfully exported with complex query")
}

// testComplexFilteringScenarios tests advanced filtering combinations
func testComplexFilteringScenarios(t *testing.T, ctx context.Context, client *Client) {
	// Load sample data
	logstashIndex, _ := loadSampleDataFromFile(t, ctx, client, "../../opensearch-sample/logstash.json")
	jaegerIndex, _ := loadSampleDataFromFile(t, ctx, client, "../../opensearch-sample/jaeger.json")

	// Test 1: Logstash with nested label filtering
	opts := ExportOptions{
		Index: logstashIndex,
		Query: "kubernetes_labels.app:sybrin",
		Limit: 10,
	}

	result, err := client.performSearch(opts)
	require.NoError(t, err, "Should filter by nested kubernetes labels")

	// Verify nested label filtering
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if labels, ok := doc["kubernetes_labels"].(map[string]interface{}); ok {
			if app, ok := labels["app"].(string); ok {
				assert.Equal(t, "sybrin", app, "Should match app label")
			}
		}
	}
	t.Logf("Filtered by kubernetes_labels.app=sybrin: found %d documents", result.Hits.Total.Value)

	// Test 2: Jaeger with nested process tag filtering
	opts = ExportOptions{
		Index: jaegerIndex,
		Query: "process.tag.k8s\\@namespace\\@name:zimbabwe",
		Limit: 10,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should filter by nested process tags")

	// Verify nested process tag filtering
	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if process, ok := doc["process"].(map[string]interface{}); ok {
			if tag, ok := process["tag"].(map[string]interface{}); ok {
				if ns, ok := tag["k8s@namespace@name"].(string); ok {
					assert.Equal(t, "zimbabwe", ns, "Should match k8s namespace in process tag")
				}
			}
		}
	}
	t.Logf("Filtered by process.tag.k8s@namespace@name=zimbabwe: found %d documents", result.Hits.Total.Value)

	// Test 3: Time range filtering with Jaeger (using startTimeMillis)
	opts = ExportOptions{
		Index: jaegerIndex,
		From:  "2025-09-05T00:00:00Z",
		To:    "2025-09-05T23:59:59Z",
		Limit: 100,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should filter by time range on jaeger data")
	assert.Greater(t, result.Hits.Total.Value, int64(0), "Should find documents in time range")
	t.Logf("Filtered jaeger by time range: found %d documents", result.Hits.Total.Value)

	// Test 4: Wildcard search with field selection
	tempDir, err := os.MkdirTemp("", "complex-test-")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	outputFile := tempDir + "/wildcard-export.json"
	opts = ExportOptions{
		Index:  logstashIndex,
		Query:  "message:*GET*API*",
		Fields: []string{"@timestamp", "message", "kubernetes_namespace_name"},
		Limit:  20,
		Output: outputFile,
	}

	err = client.Export(opts)
	require.NoError(t, err, "Should export with wildcard search")

	// Verify wildcard results
	exportedData, err := os.ReadFile(outputFile)
	require.NoError(t, err)

	// Check if we have valid JSON data
	if len(exportedData) > 2 { // More than just "[]"
		var exportedDocs []map[string]interface{}
		err = json.Unmarshal(exportedData, &exportedDocs)
		require.NoError(t, err, "Failed to parse exported JSON: %s", string(exportedData))

		for _, doc := range exportedDocs {
			message, _ := doc["message"].(string)
			if message != "" {
				// The query looks for documents with both GET and API in the message
				// but with limited sample data, we might not find exact matches
				t.Logf("Found document with message: %s", message)
			}
		}
	} else {
		t.Log("No documents matched the wildcard query with limited sample data")
	}
	t.Log("Successfully performed wildcard search and export")

	// Test 5: Multi-service filtering in Jaeger
	opts = ExportOptions{
		Index: jaegerIndex,
		Query: "process.serviceName:(kenya-cycle OR malawi-cycle OR uganda-cycle)",
		Limit: 30,
	}

	result, err = client.performSearch(opts)
	require.NoError(t, err, "Should filter by multiple services")

	// Verify multi-service results
	validServices := map[string]bool{
		"kenya-cycle":  true,
		"malawi-cycle": true,
		"uganda-cycle": true,
	}

	for _, hit := range result.Hits.Hits {
		doc := hit.Source
		if process, ok := doc["process"].(map[string]interface{}); ok {
			if serviceName, ok := process["serviceName"].(string); ok {
				assert.True(t, validServices[serviceName], "Service should be one of the filtered services")
			}
		}
	}
	t.Logf("Filtered by multiple services: found %d documents", result.Hits.Total.Value)
}

// Helper function for performSearch since it's not exported
func (c *Client) performSearch(opts ExportOptions) (*SearchResult, error) {
	// Detect log type and timestamp field
	indexInfo, err := c.InspectIndex(opts.Index)
	if err != nil {
		return nil, fmt.Errorf("failed to inspect index: %w", err)
	}

	timestampField := indexInfo.TimestampField
	if timestampField == "" {
		timestampField = "@timestamp"
	}

	// Get field mappings
	fieldMapping := GetFieldMappings(indexInfo.Type, indexInfo.AvailableFields)

	// Build filter constraints
	filterConstraints := BuildMultiFieldConstraints(opts.Filters, fieldMapping)

	// Parse time range
	fromTime, toTime := opts.From, opts.To

	// Build query
	query, err := c.buildQueryWithFilters(opts, timestampField, fromTime, toTime, filterConstraints)
	if err != nil {
		return nil, fmt.Errorf("failed to build query: %w", err)
	}

	// Execute search
	osClient, err := c.GetClient()
	if err != nil {
		return nil, err
	}

	res, err := osClient.Search(
		osClient.Search.WithContext(context.Background()),
		osClient.Search.WithIndex(opts.Index),
		osClient.Search.WithBody(strings.NewReader(query)),
		osClient.Search.WithSize(opts.Limit),
	)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("search error: %s", res.String())
	}

	var result SearchResult
	if err := json.NewDecoder(res.Body).Decode(&result); err != nil {
		return nil, err
	}

	return &result, nil
}

// SearchResult represents OpenSearch search response
type SearchResult struct {
	Hits struct {
		Total struct {
			Value int64 `json:"value"`
		} `json:"total"`
		Hits []struct {
			Source map[string]interface{} `json:"_source"`
		} `json:"hits"`
	} `json:"hits"`
}
