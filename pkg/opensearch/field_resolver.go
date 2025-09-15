package opensearch

import (
	"embed"
	"fmt"
	"strings"

	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/logger"
	"gopkg.in/yaml.v3"
)

//go:embed schemas/*.yaml
var schemaFS embed.FS

// LoadEmbeddedClickySchema loads a built-in clicky schema by name
func LoadEmbeddedClickySchema(name string) (*api.PrettyObject, error) {
	filename := fmt.Sprintf("schemas/%s.yaml", name)

	data, err := schemaFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("schema %s not found: %w", name, err)
	}

	var schema api.PrettyObject
	if err := yaml.Unmarshal(data, &schema); err != nil {
		return nil, fmt.Errorf("failed to parse schema %s: %w", name, err)
	}

	return &schema, nil
}

// ListEmbeddedClickySchemas returns list of available built-in clicky schemas
func ListEmbeddedClickySchemas() ([]string, error) {
	entries, err := schemaFS.ReadDir("schemas")
	if err != nil {
		return nil, fmt.Errorf("failed to read schemas directory: %w", err)
	}

	var schemas []string
	for _, entry := range entries {
		if !entry.IsDir() && strings.HasSuffix(entry.Name(), ".yaml") {
			name := entry.Name()[:len(entry.Name())-5] // Remove .yaml extension
			schemas = append(schemas, name)
		}
	}

	return schemas, nil
}

// ResolveSchemaFields resolves canonical field names in schema to actual field names from data
func ResolveSchemaFields(schema *api.PrettyObject, availableFields []string) *api.PrettyObject {
	if schema == nil || len(schema.Fields) == 0 {
		return schema
	}

	resolvedSchema := &api.PrettyObject{
		Fields: make([]api.PrettyField, len(schema.Fields)),
	}

	// Copy schema structure and resolve field names
	for i, field := range schema.Fields {
		resolvedField := field // Copy the field

		if field.Type == "array" && field.TableOptions.Fields != nil {
			// This is a table field, resolve the nested fields
			resolvedTableFields := make([]api.PrettyField, len(field.TableOptions.Fields))

			for j, tableField := range field.TableOptions.Fields {
				resolvedTableField := tableField // Copy the table field

				// Resolve the field names using pattern matching to populate aliases
				if resolvedNames := resolveFieldNames(tableField.Name, availableFields); len(resolvedNames) > 0 {
					resolvedTableField.Aliases = resolvedNames
					logger.Tracef("Resolved field '%s' -> aliases: %v\n", tableField.Name, resolvedNames)
				} else {
					logger.Tracef("Could not resolve field '%s'\n", tableField.Name)
				}

				resolvedTableFields[j] = resolvedTableField
			}

			resolvedField.TableOptions.Fields = resolvedTableFields
		}

		resolvedSchema.Fields[i] = resolvedField
	}

	return resolvedSchema
}

// resolveFieldNames resolves a canonical field name to multiple actual field names using pattern matching
func resolveFieldNames(canonicalName string, availableFields []string) []string {
	var matches []string
	seen := make(map[string]bool)

	// First try exact match
	for _, field := range availableFields {
		if strings.ToLower(field) == strings.ToLower(canonicalName) {
			if !seen[field] {
				matches = append(matches, field)
				seen[field] = true
			}
		}
	}

	// Try pattern-based matching using the existing logic
	patternMatches := findFieldsByPattern(availableFields, canonicalName)
	for _, match := range patternMatches {
		if !seen[match] {
			matches = append(matches, match)
			seen[match] = true
		}
	}

	return matches
}

// resolveFieldName resolves a canonical field name to the first actual field name (backward compatibility)
func resolveFieldName(canonicalName string, availableFields []string) string {
	matches := resolveFieldNames(canonicalName, availableFields)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// GetFieldMappingFromSchema extracts field mappings from a resolved schema for filter use
func GetFieldMappingFromSchema(resolvedSchema *api.PrettyObject) *FieldMapping {
	mapping := &FieldMapping{}

	if resolvedSchema == nil || len(resolvedSchema.Fields) == 0 {
		return mapping
	}

	// Find the table field (should be the first one in our schemas)
	var tableFields []api.PrettyField
	for _, field := range resolvedSchema.Fields {
		if field.Type == "array" && field.TableOptions.Fields != nil {
			tableFields = field.TableOptions.Fields
			break
		}
	}

	if tableFields == nil {
		return mapping
	}

	// Map canonical names to resolved field aliases (fallback to original field name if no aliases)
	for _, field := range tableFields {
		canonicalName := getCanonicalFieldName(field.Label, field.Name)
		fieldNames := field.Aliases
		if len(fieldNames) == 0 {
			fieldNames = []string{field.Name}
		}
		switch canonicalName {
		case "namespace":
			mapping.Namespace = fieldNames
		case "pod":
			mapping.Pod = fieldNames
		case "deployment":
			mapping.Deployment = fieldNames
		case "container":
			mapping.Container = fieldNames
		case "service":
			mapping.Service = fieldNames
		case "operation":
			mapping.Operation = fieldNames
		}
	}

	return mapping
}

// getCanonicalFieldName determines the canonical name from field label and name
func getCanonicalFieldName(label, name string) string {
	// Map labels to canonical names
	labelToCanonical := map[string]string{
		"namespace":  "namespace",
		"pod":        "pod",
		"deployment": "deployment",
		"container":  "container",
		"service":    "service",
		"operation":  "operation",
	}

	lowerLabel := strings.ToLower(label)
	if canonical, ok := labelToCanonical[lowerLabel]; ok {
		return canonical
	}

	// Fallback to name-based detection
	lowerName := strings.ToLower(name)
	for canonical, _ := range labelToCanonical {
		if strings.Contains(lowerName, canonical) {
			return canonical
		}
	}

	return ""
}
