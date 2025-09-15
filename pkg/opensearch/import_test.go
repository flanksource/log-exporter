package opensearch

import (
	"testing"
)

func TestGenerateMappingFromData(t *testing.T) {
	client := &Client{}

	testCases := []struct {
		name     string
		input    []map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "should exclude provided_name, uid, and version from mapping",
			input: []map[string]interface{}{
				{
					"id":            "test-id",
					"provided_name": "test-name",
					"uid":           "test-uid",
					"version":       "1.0",
					"message":       "test message",
					"timestamp":     "2023-01-01T00:00:00Z",
				},
			},
			expected: map[string]interface{}{
				"properties": map[string]interface{}{
					"id":        map[string]interface{}{"type": "keyword"},
					"message":   map[string]interface{}{"type": "text", "fields": map[string]interface{}{"keyword": map[string]interface{}{"type": "keyword", "ignore_above": 256}}},
					"timestamp": map[string]interface{}{"type": "date"},
				},
			},
		},
		{
			name: "should include all other fields in mapping",
			input: []map[string]interface{}{
				{
					"field1": "value1",
					"field2": 123,
					"field3": true,
					"field4": 123.45,
				},
			},
			expected: map[string]interface{}{
				"properties": map[string]interface{}{
					"field1": map[string]interface{}{"type": "keyword"},
					"field2": map[string]interface{}{"type": "long"},
					"field3": map[string]interface{}{"type": "boolean"},
					"field4": map[string]interface{}{"type": "double"},
				},
			},
		},
		{
			name: "should handle documents with only filtered attributes",
			input: []map[string]interface{}{
				{
					"provided_name": "test",
					"uid":           "test-uid",
					"version":       "1.0",
				},
			},
			expected: map[string]interface{}{
				"properties": map[string]interface{}{},
			},
		},
		{
			name:  "should handle empty documents",
			input: []map[string]interface{}{},
			expected: map[string]interface{}{
				"properties": map[string]interface{}{},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := client.GenerateMappingFromData(tc.input)
			if err != nil {
				t.Fatalf("Unexpected error: %v", err)
			}

			properties, ok := result["properties"].(map[string]interface{})
			if !ok {
				t.Fatalf("Expected 'properties' key in result")
			}

			expectedProperties := tc.expected["properties"].(map[string]interface{})

			// Check that expected properties are present
			for key, expectedValue := range expectedProperties {
				actualValue, exists := properties[key]
				if !exists {
					t.Errorf("Expected property %s to be present in mapping", key)
					continue
				}

				expectedMap := expectedValue.(map[string]interface{})
				actualMap := actualValue.(map[string]interface{})

				// Check type
				if expectedMap["type"] != actualMap["type"] {
					t.Errorf("For property %s, expected type %v, got %v", key, expectedMap["type"], actualMap["type"])
				}
			}

			// Check that filtered fields are not present
			filteredKeys := []string{"provided_name", "uid", "version"}
			for _, filteredKey := range filteredKeys {
				if _, exists := properties[filteredKey]; exists {
					t.Errorf("Filtered property %s should not be present in mapping", filteredKey)
				}
			}

			// Check that no extra properties are present (beyond expected)
			for key := range properties {
				if _, expected := expectedProperties[key]; !expected {
					t.Errorf("Unexpected property %s found in mapping", key)
				}
			}
		})
	}
}

func TestCleanIndexDefinition(t *testing.T) {
	client := &Client{}

	testCases := []struct {
		name     string
		input    IndexDefinition
		expected IndexDefinition
	}{
		{
			name: "should clean settings and mappings",
			input: IndexDefinition{
				Name: "test-index",
				Settings: map[string]interface{}{
					"index": map[string]interface{}{
						"number_of_shards":   1,
						"number_of_replicas": 0,
						"provided_name":      "test-index",
						"uid":                "test-uid",
						"uuid":               "test-uuid",
						"version": map[string]interface{}{
							"created": "136347827",
						},
					},
				},
				Mappings: map[string]interface{}{
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type": "keyword",
						},
						"provided_name": map[string]interface{}{
							"type": "keyword",
						},
						"uid": map[string]interface{}{
							"type": "keyword",
						},
						"version": map[string]interface{}{
							"type": "keyword",
						},
						"message": map[string]interface{}{
							"type": "text",
						},
					},
				},
				Documents: []map[string]interface{}{
					{
						"id":            "test-1",
						"provided_name": "should-be-preserved-in-original",
						"uid":           "test-uid",
						"version":       "1.0",
						"message":       "test message",
					},
				},
			},
			expected: IndexDefinition{
				Name: "test-index",
				Settings: map[string]interface{}{
					"index": map[string]interface{}{
						"number_of_shards":   1,
						"number_of_replicas": 0,
					},
				},
				Mappings: map[string]interface{}{
					"properties": map[string]interface{}{
						"id": map[string]interface{}{
							"type": "keyword",
						},
						"message": map[string]interface{}{
							"type": "text",
						},
					},
				},
				Documents: []map[string]interface{}{
					{
						"id":            "test-1",
						"provided_name": "should-be-preserved-in-original",
						"uid":           "test-uid",
						"version":       "1.0",
						"message":       "test message",
					},
				},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := client.cleanIndexDefinition(tc.input)

			// Check name
			if result.Name != tc.expected.Name {
				t.Errorf("Expected name %s, got %s", tc.expected.Name, result.Name)
			}

			// Check settings
			indexSettings := result.Settings["index"].(map[string]interface{})
			expectedIndexSettings := tc.expected.Settings["index"].(map[string]interface{})

			for key, expectedValue := range expectedIndexSettings {
				if actualValue, exists := indexSettings[key]; !exists || actualValue != expectedValue {
					t.Errorf("For setting %s, expected %v, got %v", key, expectedValue, actualValue)
				}
			}

			// Verify filtered settings are not present
			filteredSettingsKeys := []string{"provided_name", "uid", "uuid", "version"}
			for _, filteredKey := range filteredSettingsKeys {
				if _, exists := indexSettings[filteredKey]; exists {
					t.Errorf("Filtered setting %s should not be present", filteredKey)
				}
			}

			// Check mappings
			properties := result.Mappings["properties"].(map[string]interface{})
			expectedProperties := tc.expected.Mappings["properties"].(map[string]interface{})

			for key, expectedValue := range expectedProperties {
				if actualValue, exists := properties[key]; !exists {
					t.Errorf("Expected property %s to be present", key)
				} else {
					expectedMap := expectedValue.(map[string]interface{})
					actualMap := actualValue.(map[string]interface{})
					if expectedMap["type"] != actualMap["type"] {
						t.Errorf("For property %s, expected type %v, got %v", key, expectedMap["type"], actualMap["type"])
					}
				}
			}

			// Verify filtered mappings are not present
			filteredMappingKeys := []string{"provided_name", "uid", "version"}
			for _, filteredKey := range filteredMappingKeys {
				if _, exists := properties[filteredKey]; exists {
					t.Errorf("Filtered property %s should not be present in mapping", filteredKey)
				}
			}

			// Check that documents are preserved (filtering happens during preprocessing)
			if len(tc.expected.Documents) != len(result.Documents) {
				t.Errorf("Expected %d documents, got %d", len(tc.expected.Documents), len(result.Documents))
			} else {
				for i, expectedDoc := range tc.expected.Documents {
					if i < len(result.Documents) {
						actualDoc := result.Documents[i]
						for key, expectedValue := range expectedDoc {
							if actualValue, exists := actualDoc[key]; !exists || actualValue != expectedValue {
								t.Errorf("For document %d, key %s, expected %v, got %v", i, key, expectedValue, actualValue)
							}
						}
					}
				}
			}
		})
	}
}

func TestPreprocessDocument(t *testing.T) {
	client := &Client{}

	testCases := []struct {
		name     string
		input    map[string]interface{}
		expected map[string]interface{}
	}{
		{
			name: "should remove provided_name, uid, and version attributes",
			input: map[string]interface{}{
				"id":            "test-id",
				"provided_name": "test-name",
				"uid":           "test-uid",
				"version":       "1.0",
				"message":       "test message",
				"timestamp":     "2023-01-01T00:00:00Z",
			},
			expected: map[string]interface{}{
				"id":        "test-id",
				"message":   "test message",
				"timestamp": "2023-01-01T00:00:00Z",
			},
		},
		{
			name: "should process @json fields and remove filtered attributes",
			input: map[string]interface{}{
				"data@json":     `{"key": "value"}`,
				"provided_name": "should-be-removed",
				"uid":           "should-be-removed",
				"version":       "should-be-removed",
				"normal_field":  "should-be-kept",
			},
			expected: map[string]interface{}{
				"data@json":    map[string]interface{}{"key": "value"},
				"normal_field": "should-be-kept",
			},
		},
		{
			name: "should keep other attributes unchanged",
			input: map[string]interface{}{
				"field1": "value1",
				"field2": 123,
				"field3": true,
			},
			expected: map[string]interface{}{
				"field1": "value1",
				"field2": 123,
				"field3": true,
			},
		},
		{
			name:     "should handle empty document",
			input:    map[string]interface{}{},
			expected: map[string]interface{}{},
		},
		{
			name: "should handle document with only filtered attributes",
			input: map[string]interface{}{
				"provided_name": "test",
				"uid":           "test-uid",
				"version":       "1.0",
			},
			expected: map[string]interface{}{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := client.PreprocessDocument(tc.input)

			// Check that expected keys are present
			for key, expectedValue := range tc.expected {
				actualValue, exists := result[key]
				if !exists {
					t.Errorf("Expected key %s to be present in result", key)
					continue
				}

				// For JSON objects, do a deep comparison
				if expectedMap, ok := expectedValue.(map[string]interface{}); ok {
					if actualMap, ok := actualValue.(map[string]interface{}); ok {
						for innerKey, innerExpected := range expectedMap {
							if actualInner, exists := actualMap[innerKey]; !exists || actualInner != innerExpected {
								t.Errorf("For key %s.%s, expected %v, got %v", key, innerKey, innerExpected, actualInner)
							}
						}
					} else {
						t.Errorf("For key %s, expected map[string]interface{}, got %T", key, actualValue)
					}
				} else if actualValue != expectedValue {
					t.Errorf("For key %s, expected %v, got %v", key, expectedValue, actualValue)
				}
			}

			// Check that no extra keys are present
			for key := range result {
				if _, expected := tc.expected[key]; !expected {
					t.Errorf("Unexpected key %s found in result", key)
				}
			}

			// Verify filtered attributes are not present
			filteredKeys := []string{"provided_name", "uid", "version"}
			for _, filteredKey := range filteredKeys {
				if _, exists := result[filteredKey]; exists {
					t.Errorf("Filtered key %s should not be present in result", filteredKey)
				}
			}
		})
	}
}
