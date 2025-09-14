package opensearch

import (
	"fmt"
	"strings"
)

// FilterOptions holds filter values for Kubernetes and OpenTelemetry
type FilterOptions struct {
	K8sNamespace  string
	K8sPod        string
	K8sDeployment string
	OtelService   string
	OtelOperation string
}

// FieldMapping represents the actual field names to use for a given log type
type FieldMapping struct {
	Namespace  string
	Pod        string
	Deployment string
	Service    string
	Operation  string
}

// GetFieldMappings returns the correct field names based on log type and available fields
func GetFieldMappings(logType string, availableFields []string) *FieldMapping {
	fieldMap := make(map[string]bool)
	for _, field := range availableFields {
		fieldMap[strings.ToLower(field)] = true
	}

	mapping := &FieldMapping{}

	switch logType {
	case "kubernetes":
		// Try Kubernetes-specific field formats first (including underscore notation)
		if fieldMap["kubernetes_namespace_name"] {
			mapping.Namespace = "kubernetes_namespace_name"
		} else if fieldMap["kubernetes.namespace"] {
			mapping.Namespace = "kubernetes.namespace"
		} else if fieldMap["kubernetes.namespace.name"] {
			mapping.Namespace = "kubernetes.namespace.name"
		} else if fieldMap["namespace"] {
			mapping.Namespace = "namespace"
		}

		if fieldMap["kubernetes_pod_name"] {
			mapping.Pod = "kubernetes_pod_name"
		} else if fieldMap["kubernetes.pod.name"] {
			mapping.Pod = "kubernetes.pod.name"
		} else if fieldMap["kubernetes.pod"] {
			mapping.Pod = "kubernetes.pod"
		} else if fieldMap["pod.name"] {
			mapping.Pod = "pod.name"
		} else if fieldMap["pod"] {
			mapping.Pod = "pod"
		}

		if fieldMap["kubernetes_deployment_name"] {
			mapping.Deployment = "kubernetes_deployment_name"
		} else if fieldMap["kubernetes.deployment.name"] {
			mapping.Deployment = "kubernetes.deployment.name"
		} else if fieldMap["kubernetes.labels.app"] {
			mapping.Deployment = "kubernetes.labels.app"
		} else if fieldMap["deployment.name"] {
			mapping.Deployment = "deployment.name"
		} else if fieldMap["deployment"] {
			mapping.Deployment = "deployment"
		}

		// For Kubernetes logs that might also have OTEL data
		if fieldMap["servicename"] {
			mapping.Service = "serviceName"
		} else if fieldMap["service.name"] {
			mapping.Service = "service.name"
		}

		if fieldMap["operationname"] {
			mapping.Operation = "operationName"
		} else if fieldMap["operation.name"] {
			mapping.Operation = "operation.name"
		}

	case "jaeger":
		// Jaeger/OTEL typically uses nested field names
		if fieldMap["process.servicename"] {
			mapping.Service = "process.serviceName"
		} else if fieldMap["servicename"] {
			mapping.Service = "serviceName"
		} else if fieldMap["service.name"] {
			mapping.Service = "service.name"
		} else if fieldMap["service"] {
			mapping.Service = "service"
		}

		if fieldMap["operationname"] {
			mapping.Operation = "operationName"
		} else if fieldMap["operation.name"] {
			mapping.Operation = "operation.name"
		} else if fieldMap["operation"] {
			mapping.Operation = "operation"
		}

		// Some Jaeger indices might have Kubernetes context
		if fieldMap["kubernetes.namespace"] {
			mapping.Namespace = "kubernetes.namespace"
		} else if fieldMap["namespace"] {
			mapping.Namespace = "namespace"
		}

		if fieldMap["kubernetes.pod.name"] {
			mapping.Pod = "kubernetes.pod.name"
		} else if fieldMap["pod.name"] {
			mapping.Pod = "pod.name"
		} else if fieldMap["pod"] {
			mapping.Pod = "pod"
		}

	default: // generic
		// Try common field names (including underscore notation)
		if fieldMap["kubernetes_namespace_name"] {
			mapping.Namespace = "kubernetes_namespace_name"
		} else if fieldMap["namespace"] {
			mapping.Namespace = "namespace"
		} else if fieldMap["kubernetes.namespace"] {
			mapping.Namespace = "kubernetes.namespace"
		}

		if fieldMap["kubernetes_pod_name"] {
			mapping.Pod = "kubernetes_pod_name"
		} else if fieldMap["pod"] {
			mapping.Pod = "pod"
		} else if fieldMap["pod.name"] {
			mapping.Pod = "pod.name"
		} else if fieldMap["kubernetes.pod.name"] {
			mapping.Pod = "kubernetes.pod.name"
		}

		if fieldMap["kubernetes_deployment_name"] {
			mapping.Deployment = "kubernetes_deployment_name"
		} else if fieldMap["deployment"] {
			mapping.Deployment = "deployment"
		} else if fieldMap["kubernetes.deployment.name"] {
			mapping.Deployment = "kubernetes.deployment.name"
		}

		if fieldMap["service"] {
			mapping.Service = "service"
		} else if fieldMap["servicename"] {
			mapping.Service = "serviceName"
		} else if fieldMap["service.name"] {
			mapping.Service = "service.name"
		}

		if fieldMap["operation"] {
			mapping.Operation = "operation"
		} else if fieldMap["operationname"] {
			mapping.Operation = "operationName"
		} else if fieldMap["operation.name"] {
			mapping.Operation = "operation.name"
		}
	}

	return mapping
}

// FilterConstraint represents a single filter constraint
type FilterConstraint struct {
	Field string
	Value string
}

// BuildFilterConstraints converts filter options to field constraints
func BuildFilterConstraints(filters FilterOptions, mapping *FieldMapping) []FilterConstraint {
	var constraints []FilterConstraint

	if filters.K8sNamespace != "" && mapping.Namespace != "" {
		constraints = append(constraints, FilterConstraint{
			Field: mapping.Namespace,
			Value: filters.K8sNamespace,
		})
	}

	if filters.K8sPod != "" && mapping.Pod != "" {
		constraints = append(constraints, FilterConstraint{
			Field: mapping.Pod,
			Value: filters.K8sPod,
		})
	}

	if filters.K8sDeployment != "" && mapping.Deployment != "" {
		constraints = append(constraints, FilterConstraint{
			Field: mapping.Deployment,
			Value: filters.K8sDeployment,
		})
	}

	if filters.OtelService != "" && mapping.Service != "" {
		constraints = append(constraints, FilterConstraint{
			Field: mapping.Service,
			Value: filters.OtelService,
		})
	}

	if filters.OtelOperation != "" && mapping.Operation != "" {
		constraints = append(constraints, FilterConstraint{
			Field: mapping.Operation,
			Value: filters.OtelOperation,
		})
	}

	return constraints
}

// ValidateFilters checks if filters can be applied and provides warnings
func ValidateFilters(filters FilterOptions, mapping *FieldMapping, logType string, availableFields []string) []string {
	var warnings []string

	if filters.K8sNamespace != "" && mapping.Namespace == "" {
		suggestions := findSimilarFields(availableFields, []string{"namespace"})
		warning := fmt.Sprintf("Kubernetes namespace filter '%s' cannot be applied - no suitable namespace field found in %s logs", filters.K8sNamespace, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.K8sPod != "" && mapping.Pod == "" {
		suggestions := findSimilarFields(availableFields, []string{"pod"})
		warning := fmt.Sprintf("Kubernetes pod filter '%s' cannot be applied - no suitable pod field found in %s logs", filters.K8sPod, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.K8sDeployment != "" && mapping.Deployment == "" {
		suggestions := findSimilarFields(availableFields, []string{"deployment", "app", "k8s", "kubernetes"})
		warning := fmt.Sprintf("Kubernetes deployment filter '%s' cannot be applied - no suitable deployment field found in %s logs", filters.K8sDeployment, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.OtelService != "" && mapping.Service == "" {
		suggestions := findSimilarFields(availableFields, []string{"service"})
		warning := fmt.Sprintf("OTEL service filter '%s' cannot be applied - no suitable service field found in %s logs", filters.OtelService, logType)
		if len(suggestions) > 0 {
			warning += fmt.Sprintf(". Available similar fields: %s", strings.Join(suggestions, ", "))
		}
		warnings = append(warnings, warning)
	}

	if filters.OtelOperation != "" && mapping.Operation == "" {
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

	// Limit to max 5 suggestions to keep output manageable
	if len(suggestions) > 5 {
		suggestions = suggestions[:5]
	}

	return suggestions
}

// HasActiveFilters returns true if any filters are specified
func (f FilterOptions) HasActiveFilters() bool {
	return f.K8sNamespace != "" || f.K8sPod != "" || f.K8sDeployment != "" ||
		f.OtelService != "" || f.OtelOperation != ""
}
