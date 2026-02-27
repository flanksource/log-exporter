package opensearch

import (
	"fmt"
	"sort"
	"strings"

	"github.com/flanksource/clicky"
	"github.com/flanksource/clicky/api"
	"github.com/flanksource/commons/logger"
)

// FilterOptions holds filter values for Kubernetes and OpenTelemetry
type FilterOptions struct {
	K8sNamespace  string
	K8sPod        string
	K8sDeployment string
	K8sContainer  string
	OtelService   string
	OtelOperation string
}

// FieldMapping represents the actual field names to use for a given log type
// Each field can map to multiple actual field names for broader matching
type FieldMapping struct {
	Namespace  []string
	Pod        []string
	Deployment []string
	Container  []string
	Service    []string
	Operation  []string
}

func (m FieldMapping) Pretty() api.Text {
	t := clicky.Text("")
	if len(m.Namespace) > 0 {
		t = t.NewLine().Append("namespace: ", "text-muted").Append(strings.Join(m.Namespace, ", "))
	}
	if len(m.Pod) > 0 {
		t = t.NewLine().Append("pod: ", "text-muted").Append(strings.Join(m.Pod, ", "))
	}
	if len(m.Deployment) > 0 {
		t = t.NewLine().Append("deployment: ", "text-muted").Append(strings.Join(m.Deployment, ", "))
	}
	if len(m.Container) > 0 {
		t = t.NewLine().Append("container: ", "text-muted").Append(strings.Join(m.Container, ", "))
	}
	return t
}

func normalizeField(field string) string {
	lower := strings.ToLower(field)
	lower = strings.Replace(lower, "_", ".", -1)
	lower = strings.Replace(lower, "@", ".", -1)
	lower = strings.Replace(lower, "-", ".", -1)
	return lower
}

// findFieldsByPattern searches for ALL fields matching a specific pattern
// Looks for fields ending with: [separator]fieldType[separator]name or just [separator]fieldType
// Separators: . _ - @
// Returns multiple matches in priority order (most specific first)
func findFieldsByPattern(availableFields []string, fieldType string) []string {

	// Convert to lowercase for case-insensitive matching
	lowerFields := make([]string, len(availableFields))
	fieldMap := make(map[string]string) // lowercase -> original
	for i, field := range availableFields {
		lower := normalizeField(field)
		lowerFields[i] = lower
		fieldMap[lower] = field
	}

	// Priority patterns to search for (most specific first)
	var patterns []string = []string{fieldType, "." + fieldType + ".name", "." + fieldType, fieldType + "name"}

	var matches []string
	seen := make(map[string]bool) // Avoid duplicates

	// Search through all available fields for each pattern (in priority order)
	for _, pattern := range patterns {
		for _, lowerField := range lowerFields {
			if strings.HasSuffix(lowerField, pattern) {
				originalField := fieldMap[lowerField]
				if !seen[originalField] {
					matches = append(matches, originalField)
					seen[originalField] = true
				}
			}
		}
	}

	// Additional check for prefix matches (e.g., serviceName starts with service)
	for _, lowerField := range lowerFields {
		if strings.HasPrefix(lowerField, fieldType) {
			originalField := fieldMap[lowerField]
			if !seen[originalField] {
				matches = append(matches, originalField)
				seen[originalField] = true
			}
		}
	}

	return matches
}

// findFieldByPattern searches for the first field matching a specific pattern (backward compatibility)
func findFieldByPattern(availableFields []string, fieldType string) string {
	matches := findFieldsByPattern(availableFields, fieldType)
	if len(matches) > 0 {
		return matches[0]
	}
	return ""
}

// GetFieldMappings returns field mappings using pattern-based detection
func GetFieldMappings(logType string, availableFields []string) *FieldMapping {
	fieldMap := make(map[string]bool)
	for _, field := range availableFields {
		fieldMap[strings.ToLower(field)] = true
	}

	mapping := &FieldMapping{}

	// Use pattern-based detection for each field (now supporting multiple matches)
	if namespaceFields := findFieldsByPattern(availableFields, "namespace"); len(namespaceFields) > 0 {
		mapping.Namespace = namespaceFields
	}

	if podFields := findFieldsByPattern(availableFields, "pod"); len(podFields) > 0 {
		mapping.Pod = podFields

	}

	if deploymentFields := findFieldsByPattern(availableFields, "deployment"); len(deploymentFields) > 0 {
		mapping.Deployment = deploymentFields

	}

	if serviceFields := findFieldsByPattern(availableFields, "service"); len(serviceFields) > 0 {
		mapping.Service = serviceFields

	}

	if operationFields := findFieldsByPattern(availableFields, "operation"); len(operationFields) > 0 {
		mapping.Operation = operationFields

	}

	if containerFields := findFieldsByPattern(availableFields, "container"); len(containerFields) > 0 {
		mapping.Container = containerFields

	}

	logger.Infof("Final field mappings: Namespace=%v, Pod=%v, Deployment=%v, Container=%v, Service=%v, Operation=%v\n",
		mapping.Namespace, mapping.Pod, mapping.Deployment, mapping.Container, mapping.Service, mapping.Operation)

	return mapping
}

// FilterConstraint represents a single filter constraint
type FilterConstraint struct {
	Field string
	Value string
}

// MultiFieldConstraint represents a constraint that should match any of multiple fields (OR logic)
type MultiFieldConstraint struct {
	Fields []string
	Value  string
	Name   string // Canonical name for debugging
}

// BuildFilterConstraints converts filter options to field constraints
func BuildFilterConstraints(filters FilterOptions, mapping *FieldMapping) []FilterConstraint {
	// Convert MultiFieldConstraints to FilterConstraints (backward compatibility)
	multiConstraints := BuildMultiFieldConstraints(filters, mapping)
	var constraints []FilterConstraint

	for _, mc := range multiConstraints {
		// For now, just use the first field for backward compatibility
		if len(mc.Fields) > 0 {
			constraints = append(constraints, FilterConstraint{
				Field: mc.Fields[0],
				Value: mc.Value,
			})
		}
	}

	return constraints
}

// BuildMultiFieldConstraints converts filter options to multi-field constraints
func BuildMultiFieldConstraints(filters FilterOptions, mapping *FieldMapping) []MultiFieldConstraint {
	var constraints []MultiFieldConstraint

	if filters.K8sNamespace != "" && len(mapping.Namespace) > 0 {
		constraints = append(constraints, MultiFieldConstraint{
			Fields: mapping.Namespace,
			Value:  filters.K8sNamespace,
			Name:   "namespace",
		})
	}

	if filters.K8sPod != "" && len(mapping.Pod) > 0 {
		constraints = append(constraints, MultiFieldConstraint{
			Fields: mapping.Pod,
			Value:  filters.K8sPod,
			Name:   "pod",
		})
	}

	if filters.K8sDeployment != "" && len(mapping.Deployment) > 0 {
		constraints = append(constraints, MultiFieldConstraint{
			Fields: mapping.Deployment,
			Value:  filters.K8sDeployment,
			Name:   "deployment",
		})
	}

	if filters.K8sContainer != "" && len(mapping.Container) > 0 {
		constraints = append(constraints, MultiFieldConstraint{
			Fields: mapping.Container,
			Value:  filters.K8sContainer,
			Name:   "container",
		})
	}

	if filters.OtelService != "" && len(mapping.Service) > 0 {
		constraints = append(constraints, MultiFieldConstraint{
			Fields: mapping.Service,
			Value:  filters.OtelService,
			Name:   "service",
		})
	}

	if filters.OtelOperation != "" && len(mapping.Operation) > 0 {
		constraints = append(constraints, MultiFieldConstraint{
			Fields: mapping.Operation,
			Value:  filters.OtelOperation,
			Name:   "operation",
		})
	}

	return constraints
}

// ValidateFilters checks if filters can be applied and provides warnings
func ValidateFilters(filters FilterOptions, mapping *FieldMapping, logType string, availableFields []string) []string {
	var warnings []string

	if filters.K8sNamespace != "" && len(mapping.Namespace) == 0 {
		suggestions := findSimilarFields(availableFields, []string{"namespace"})
		warning := fmt.Sprintf("Kubernetes namespace filter '%s' cannot be applied - no suitable namespace field found in %s logs", filters.K8sNamespace, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.K8sPod != "" && len(mapping.Pod) == 0 {
		suggestions := findSimilarFields(availableFields, []string{"pod"})
		warning := fmt.Sprintf("Kubernetes pod filter '%s' cannot be applied - no suitable pod field found in %s logs", filters.K8sPod, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.K8sDeployment != "" && len(mapping.Deployment) == 0 {
		suggestions := findSimilarFields(availableFields, []string{"deployment", "app", "k8s", "kubernetes"})
		warning := fmt.Sprintf("Kubernetes deployment filter '%s' cannot be applied - no suitable deployment field found in %s logs", filters.K8sDeployment, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.K8sContainer != "" && len(mapping.Container) == 0 {
		suggestions := findSimilarFields(availableFields, []string{"container", "k8s", "kubernetes"})
		warning := fmt.Sprintf("Kubernetes container filter '%s' cannot be applied - no suitable container field found in %s logs", filters.K8sContainer, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.OtelService != "" && len(mapping.Service) == 0 {
		suggestions := findSimilarFields(availableFields, []string{"service"})
		warning := fmt.Sprintf("OTEL service filter '%s' cannot be applied - no suitable service field found in %s logs", filters.OtelService, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.OtelOperation != "" && len(mapping.Operation) == 0 {
		suggestions := findSimilarFields(availableFields, []string{"operation"})
		warning := fmt.Sprintf("OTEL operation filter '%s' cannot be applied - no suitable operation field found in %s logs", filters.OtelOperation, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	return warnings
}

// findSimilarFields finds fields that contain any of the given keywords
func findSimilarFields(availableFields []string, keywords []string) []string {
	var suggestions []string
	seen := make(map[string]bool)

	for _, field := range availableFields {
		lowerField := strings.ToLower(field)
		for _, keyword := range keywords {
			if strings.Contains(lowerField, strings.ToLower(keyword)) && !seen[field] {
				suggestions = append(suggestions, field)
				seen[field] = true
				break
			}
		}
	}
	sort.Strings(suggestions)

	// Limit to max 5 suggestions to keep output manageable
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	return suggestions
}

// HasActiveFilters returns true if any filters are specified
func (f FilterOptions) HasActiveFilters() bool {
	return f.K8sNamespace != "" || f.K8sPod != "" || f.K8sDeployment != "" || f.K8sContainer != "" ||
		f.OtelService != "" || f.OtelOperation != ""
}
