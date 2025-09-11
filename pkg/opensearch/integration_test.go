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
		Image:        "opensearchproject/opensearch:2.11.0",
		ExposedPorts: []string{"9200/tcp"},
		Env: map[string]string{
			"discovery.type":                    "single-node",
			"OPENSEARCH_JAVA_OPTS":              "-Xms512m -Xmx512m",
			"OPENSEARCH_INITIAL_ADMIN_PASSWORD": "admin123",
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
