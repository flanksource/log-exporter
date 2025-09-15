package opensearch

import (
	"reflect"
	"testing"
)

func TestGetFieldMappings(t *testing.T) {
	tests := []struct {
		name            string
		logType         string
		availableFields []string
		expectedMapping *FieldMapping
	}{
		{
			name:    "kubernetes logs with standard fields",
			logType: "kubernetes",
			availableFields: []string{
				"kubernetes.namespace",
				"tags.kubernetes@namespace",
				"kubernetes.pod.name",
				"kubernetes.deployment.name",
				"serviceName",
				"operationName",
			},
			expectedMapping: &FieldMapping{
				Namespace:  []string{"kubernetes.namespace", "tags.kubernetes@namespace"},
				Pod:        []string{"kubernetes.pod.name"},
				Deployment: []string{"kubernetes.deployment.name"},
				Service:    []string{"serviceName"},
				Operation:  []string{"operationName"},
			},
		},
		{
			name:    "jaeger logs with standard fields",
			logType: "jaeger",
			availableFields: []string{
				"serviceName",
				"operationName",
				"kubernetes.namespace",
				"kubernetes.pod.name",
			},
			expectedMapping: &FieldMapping{
				Service:   []string{"serviceName"},
				Operation: []string{"operationName"},
				Namespace: []string{"kubernetes.namespace"},
				Pod:       []string{"kubernetes.pod.name"},
			},
		},
		{
			name:    "generic logs with mixed field names",
			logType: "generic",
			availableFields: []string{
				"namespace",
				"pod",
				"service",
				"kubernetes.namespace",
				"tags.kubernetes@namespace",
				"operation",
			},
			expectedMapping: &FieldMapping{
				Namespace: []string{"namespace", "kubernetes.namespace", "tags.kubernetes@namespace"},
				Pod:       []string{"pod"},
				Service:   []string{"service"},
				Operation: []string{"operation"},
			},
		},
		{
			name:    "kubernetes logs with alternative field names",
			logType: "kubernetes",
			availableFields: []string{
				"kubernetes.pod",
				"namespace",
				"service.name",
			},
			expectedMapping: &FieldMapping{
				Namespace:  []string{"namespace"},
				Pod:        []string{"kubernetes.pod"},
				Deployment: nil,
				Service:    []string{"service.name"},
			},
		},
		{
			name:            "empty fields",
			logType:         "kubernetes",
			availableFields: []string{},
			expectedMapping: &FieldMapping{},
		},
		{
			name:    "kubernetes logs with underscore field names (new format)",
			logType: "kubernetes",
			availableFields: []string{
				"kubernetes_namespace_name",
				"kubernetes_pod_name",
				"kubernetes_deployment_name",
				"serviceName",
				"operationName",
			},
			expectedMapping: &FieldMapping{
				Namespace:  []string{"kubernetes_namespace_name"},
				Pod:        []string{"kubernetes_pod_name"},
				Deployment: []string{"kubernetes_deployment_name"},
				Service:    []string{"serviceName"},
				Operation:  []string{"operationName"},
			},
		},
		{
			name:    "process fields with serviceName",
			logType: "jaeger",
			availableFields: []string{
				"process.serviceName",
				"process.tag.k8s@namespace@name",
				"process.tag.k8s@pod@name",
				"operationName",
				"traceID",
			},
			expectedMapping: &FieldMapping{
				Service:   []string{"process.serviceName"},
				Operation: []string{"operationName"},
				Namespace: []string{"process.tag.k8s@namespace@name"},
				Pod:       []string{"process.tag.k8s@pod@name"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetFieldMappings(tt.logType, tt.availableFields)

			if !reflect.DeepEqual(result.Namespace, tt.expectedMapping.Namespace) {
				t.Errorf("Namespace: got %v, want %v", result.Namespace, tt.expectedMapping.Namespace)
			}
			if !reflect.DeepEqual(result.Pod, tt.expectedMapping.Pod) {
				t.Errorf("Pod: got %v, want %v", result.Pod, tt.expectedMapping.Pod)
			}
			if !reflect.DeepEqual(result.Deployment, tt.expectedMapping.Deployment) {
				t.Errorf("Deployment: got %v, want %v", result.Deployment, tt.expectedMapping.Deployment)
			}
			if !reflect.DeepEqual(result.Service, tt.expectedMapping.Service) {
				t.Errorf("Service: got %v, want %v", result.Service, tt.expectedMapping.Service)
			}
			if !reflect.DeepEqual(result.Operation, tt.expectedMapping.Operation) {
				t.Errorf("Operation: got %v, want %v", result.Operation, tt.expectedMapping.Operation)
			}
		})
	}
}

func TestBuildFilterConstraints(t *testing.T) {
	tests := []struct {
		name                string
		filters             FilterOptions
		mapping             *FieldMapping
		expectedConstraints []FilterConstraint
	}{
		{
			name: "all kubernetes filters with mapping",
			filters: FilterOptions{
				K8sNamespace:  "production",
				K8sPod:        "api-pod",
				K8sDeployment: "api-deployment",
			},
			mapping: &FieldMapping{
				Namespace:  []string{"kubernetes.namespace"},
				Pod:        []string{"kubernetes.pod.name"},
				Deployment: []string{"kubernetes.deployment.name"},
			},
			expectedConstraints: []FilterConstraint{
				{Field: "kubernetes.namespace", Value: "production"},
				{Field: "kubernetes.pod.name", Value: "api-pod"},
				{Field: "kubernetes.deployment.name", Value: "api-deployment"},
			},
		},
		{
			name: "all otel filters with mapping",
			filters: FilterOptions{
				OtelService:   "user-service",
				OtelOperation: "GetUser",
			},
			mapping: &FieldMapping{
				Service:   []string{"serviceName"},
				Operation: []string{"operationName"},
			},
			expectedConstraints: []FilterConstraint{
				{Field: "serviceName", Value: "user-service"},
				{Field: "operationName", Value: "GetUser"},
			},
		},
		{
			name: "filters without corresponding field mappings",
			filters: FilterOptions{
				K8sNamespace: "production",
				OtelService:  "user-service",
			},
			mapping: &FieldMapping{
				// No namespace or service mappings
				Pod: []string{"kubernetes.pod.name"},
			},
			expectedConstraints: []FilterConstraint{}, // No constraints because no fields mapped
		},
		{
			name: "mixed filters with partial mapping",
			filters: FilterOptions{
				K8sNamespace:  "staging",
				K8sPod:        "web-pod",
				OtelService:   "auth-service",
				OtelOperation: "Validate",
			},
			mapping: &FieldMapping{
				Namespace: []string{"kubernetes.namespace"},
				Operation: []string{"operationName"},
				// No pod or service mappings
			},
			expectedConstraints: []FilterConstraint{
				{Field: "kubernetes.namespace", Value: "staging"},
				{Field: "operationName", Value: "Validate"},
			},
		},
		{
			name:                "empty filters",
			filters:             FilterOptions{},
			mapping:             &FieldMapping{},
			expectedConstraints: []FilterConstraint{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := BuildFilterConstraints(tt.filters, tt.mapping)

			if len(result) != len(tt.expectedConstraints) {
				t.Errorf("Expected %d constraints, got %d", len(tt.expectedConstraints), len(result))
				return
			}

			for i, expected := range tt.expectedConstraints {
				if i >= len(result) {
					t.Errorf("Missing constraint %d: %+v", i, expected)
					continue
				}

				if result[i].Field != expected.Field {
					t.Errorf("Constraint %d field: got %q, want %q", i, result[i].Field, expected.Field)
				}
				if result[i].Value != expected.Value {
					t.Errorf("Constraint %d value: got %q, want %q", i, result[i].Value, expected.Value)
				}
			}
		})
	}
}

func TestValidateFilters(t *testing.T) {
	tests := []struct {
		name             string
		filters          FilterOptions
		mapping          *FieldMapping
		logType          string
		availableFields  []string
		expectedWarnings []string
	}{
		{
			name: "all filters have mappings",
			filters: FilterOptions{
				K8sNamespace: "production",
				OtelService:  "user-service",
			},
			mapping: &FieldMapping{
				Namespace: []string{"kubernetes.namespace"},
				Service:   []string{"serviceName"},
			},
			logType:          "kubernetes",
			availableFields:  []string{"kubernetes.namespace", "serviceName"},
			expectedWarnings: []string{},
		},
		{
			name: "some filters missing mappings",
			filters: FilterOptions{
				K8sNamespace:  "production",
				K8sPod:        "api-pod",
				OtelService:   "user-service",
				OtelOperation: "GetUser",
			},
			mapping: &FieldMapping{
				Namespace: []string{"kubernetes.namespace"},
				// Missing: Pod, Service, Operation mappings
			},
			logType: "kubernetes",
			availableFields: []string{
				"kubernetes.namespace", "kubernetes_pod_name", "some_service_field", "trace_operation",
			},
			expectedWarnings: []string{
				"Kubernetes pod filter 'api-pod' cannot be applied - no suitable pod field found in kubernetes logs. Available similar fields: kubernetes_pod_name",
				"OTEL service filter 'user-service' cannot be applied - no suitable service field found in kubernetes logs. Available similar fields: some_service_field",
				"OTEL operation filter 'GetUser' cannot be applied - no suitable operation field found in kubernetes logs. Available similar fields: trace_operation",
			},
		},
		{
			name:             "no filters specified",
			filters:          FilterOptions{},
			mapping:          &FieldMapping{},
			logType:          "generic",
			availableFields:  []string{},
			expectedWarnings: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ValidateFilters(tt.filters, tt.mapping, tt.logType, tt.availableFields)

			if len(result) != len(tt.expectedWarnings) {
				t.Errorf("Expected %d warnings, got %d", len(tt.expectedWarnings), len(result))
				t.Errorf("Expected: %v", tt.expectedWarnings)
				t.Errorf("Got: %v", result)
				return
			}

			for i, expected := range tt.expectedWarnings {
				if i >= len(result) {
					t.Errorf("Missing warning %d: %s", i, expected)
					continue
				}

				if result[i] != expected {
					t.Errorf("Warning %d: got %q, want %q", i, result[i], expected)
				}
			}
		})
	}
}

func TestFilterOptionsHasActiveFilters(t *testing.T) {
	tests := []struct {
		name     string
		filters  FilterOptions
		expected bool
	}{
		{
			name:     "no filters",
			filters:  FilterOptions{},
			expected: false,
		},
		{
			name: "only k8s namespace",
			filters: FilterOptions{
				K8sNamespace: "production",
			},
			expected: true,
		},
		{
			name: "only otel service",
			filters: FilterOptions{
				OtelService: "api-service",
			},
			expected: true,
		},
		{
			name: "multiple filters",
			filters: FilterOptions{
				K8sNamespace:  "staging",
				K8sPod:        "web-pod",
				OtelOperation: "CreateUser",
			},
			expected: true,
		},
		{
			name: "all filters",
			filters: FilterOptions{
				K8sNamespace:  "production",
				K8sPod:        "api-pod",
				K8sDeployment: "api-deployment",
				OtelService:   "user-service",
				OtelOperation: "GetUser",
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := tt.filters.HasActiveFilters()
			if result != tt.expected {
				t.Errorf("HasActiveFilters() = %v, want %v", result, tt.expected)
			}
		})
	}
}
