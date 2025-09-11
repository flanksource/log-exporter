package opensearch

import (
	"encoding/json"
	"testing"
)

func TestBuildDateRangeQuery(t *testing.T) {
	client := &Client{}

	tests := []struct {
		name    string
		from    string
		to      string
		want    string
		wantErr bool
	}{
		{
			name: "both from and to",
			from: "2024-01-01",
			to:   "2024-01-31",
			want: `{"range":{"@timestamp":{"gte":"2024-01-01","lte":"2024-01-31"}}}`,
		},
		{
			name: "only from",
			from: "2024-01-01",
			to:   "",
			want: `{"range":{"@timestamp":{"gte":"2024-01-01"}}}`,
		},
		{
			name: "only to",
			from: "",
			to:   "2024-01-31",
			want: `{"range":{"@timestamp":{"lte":"2024-01-31"}}}`,
		},
		{
			name: "neither from nor to",
			from: "",
			to:   "",
			want: "*",
		},
		{
			name: "date math expressions",
			from: "now-7d",
			to:   "now",
			want: `{"range":{"@timestamp":{"gte":"now-7d","lte":"now"}}}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := client.buildDateRangeQuery(tt.from, tt.to)

			if tt.wantErr {
				if err == nil {
					t.Errorf("buildDateRangeQuery() expected error, got none")
				}
				return
			}

			if err != nil {
				t.Errorf("buildDateRangeQuery() error = %v", err)
				return
			}

			if tt.want == "*" {
				if got != "*" {
					t.Errorf("buildDateRangeQuery() = %v, want %v", got, tt.want)
				}
				return
			}

			// Parse both JSON strings to compare structure
			var gotObj, wantObj map[string]interface{}

			if err := json.Unmarshal([]byte(got), &gotObj); err != nil {
				t.Errorf("buildDateRangeQuery() returned invalid JSON: %v", err)
				return
			}

			if err := json.Unmarshal([]byte(tt.want), &wantObj); err != nil {
				t.Errorf("Test case has invalid JSON: %v", err)
				return
			}

			// Compare the structure
			if !equalMaps(gotObj, wantObj) {
				t.Errorf("buildDateRangeQuery() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestSampleOptions(t *testing.T) {
	tests := []struct {
		name string
		opts SampleOptions
		want bool // Just test that the struct is valid
	}{
		{
			name: "basic options",
			opts: SampleOptions{
				Index:      "logs-*",
				SampleSize: 100,
				OutputDir:  "/tmp/sample",
			},
			want: true,
		},
		{
			name: "options with date range",
			opts: SampleOptions{
				Index:      "filebeat-*",
				From:       "now-7d",
				To:         "now",
				SampleSize: 50,
				OutputDir:  "/tmp/sample",
			},
			want: true,
		},
		{
			name: "options with filters",
			opts: SampleOptions{
				Index:      "jaeger-*",
				SampleSize: 200,
				OutputDir:  "/tmp/sample",
				Filters: FilterOptions{
					K8sNamespace: "production",
					OtelService:  "api-service",
				},
			},
			want: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Test that the struct is properly constructed
			if tt.opts.Index == "" {
				t.Errorf("SampleOptions.Index should not be empty")
			}
			if tt.opts.SampleSize <= 0 {
				t.Errorf("SampleOptions.SampleSize should be positive")
			}
			if tt.opts.OutputDir == "" {
				t.Errorf("SampleOptions.OutputDir should not be empty")
			}
		})
	}
}

func TestSampleDataStructure(t *testing.T) {
	// Test that the sample data structure can be marshaled/unmarshaled
	sampleData := SampleData{
		Metadata: SampleMetadata{
			Host:       "http://localhost:9200",
			Pattern:    "logs-*",
			From:       "now-7d",
			To:         "now",
			SampleSize: 100,
		},
		Indices: map[string]IndexSample{
			"logs-2024.01.15": {
				Name: "logs-2024.01.15",
				Settings: map[string]interface{}{
					"index": map[string]interface{}{
						"number_of_shards": 1,
					},
				},
				Mappings: map[string]interface{}{
					"properties": map[string]interface{}{
						"@timestamp": map[string]interface{}{
							"type": "date",
						},
						"message": map[string]interface{}{
							"type": "text",
						},
					},
				},
				Documents: []map[string]interface{}{
					{
						"@timestamp": "2024-01-15T10:00:00Z",
						"message":    "Test log message",
						"level":      "INFO",
					},
				},
				Count: 1,
			},
		},
	}

	// Test JSON marshaling
	jsonBytes, err := json.Marshal(sampleData)
	if err != nil {
		t.Errorf("Failed to marshal SampleData: %v", err)
		return
	}

	// Test JSON unmarshaling
	var unmarshaled SampleData
	if err := json.Unmarshal(jsonBytes, &unmarshaled); err != nil {
		t.Errorf("Failed to unmarshal SampleData: %v", err)
		return
	}

	// Verify structure
	if unmarshaled.Metadata.Host != sampleData.Metadata.Host {
		t.Errorf("Host mismatch after marshal/unmarshal")
	}

	if len(unmarshaled.Indices) != len(sampleData.Indices) {
		t.Errorf("Indices count mismatch after marshal/unmarshal")
	}

	indexSample, exists := unmarshaled.Indices["logs-2024.01.15"]
	if !exists {
		t.Errorf("Index sample not found after marshal/unmarshal")
		return
	}

	if len(indexSample.Documents) != 1 {
		t.Errorf("Document count mismatch after marshal/unmarshal")
	}

	if indexSample.Documents[0]["message"] != "Test log message" {
		t.Errorf("Document content mismatch after marshal/unmarshal")
	}
}

// Helper function to compare maps deeply
func equalMaps(a, b map[string]interface{}) bool {
	if len(a) != len(b) {
		return false
	}

	for key, valueA := range a {
		valueB, exists := b[key]
		if !exists {
			return false
		}

		switch vA := valueA.(type) {
		case map[string]interface{}:
			vB, ok := valueB.(map[string]interface{})
			if !ok || !equalMaps(vA, vB) {
				return false
			}
		case string:
			vB, ok := valueB.(string)
			if !ok || vA != vB {
				return false
			}
		default:
			if valueA != valueB {
				return false
			}
		}
	}

	return true
}
