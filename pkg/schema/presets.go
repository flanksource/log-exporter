package schema

import "github.com/flanksource/clicky/api"

// GetKubernetesSchema returns an optimized schema for Kubernetes/Filebeat logs
func GetKubernetesSchema() *api.PrettyObject {
	return &api.PrettyObject{
		Fields: []api.PrettyField{
			{
				Name:       "@timestamp",
				Type:       "string",
				Format:     "date",
				Style:      "text-gray-500 text-sm font-mono",
				DateFormat: "2006-01-02 15:04:05",
				Label:      "Time",
			},
			{
				Name:  "kubernetes.namespace",
				Type:  "string",
				Style: "bg-purple-100 text-purple-800 px-2 py-1 rounded font-medium text-sm",
				Label: "Namespace",
			},
			{
				Name:  "kubernetes.pod.name",
				Type:  "string",
				Style: "text-blue-600 font-mono text-sm",
				Label: "Pod",
			},
			{
				Name:  "kubernetes.container.name",
				Type:  "string",
				Style: "text-indigo-600 font-medium",
				Label: "Container",
			},
			{
				Name:  "message",
				Type:  "string",
				Style: "text-gray-700",
				Label: "Message",
			},
			{
				Name:  "level",
				Type:  "string",
				Style: "font-bold uppercase text-xs px-2 py-1 rounded-md",
				Label: "Level",
				ColorOptions: map[string]string{
					"red":    "ERROR|FATAL|CRITICAL",
					"yellow": "WARN|WARNING",
					"green":  "INFO|INFORMATION|SUCCESS",
					"blue":   "DEBUG|TRACE",
					"gray":   "UNKNOWN",
				},
			},
			{
				Name:  "kubernetes.node.name",
				Type:  "string",
				Style: "text-gray-600 font-mono text-sm",
				Label: "Node",
			},
			{
				Name:  "kubernetes.deployment.name",
				Type:  "string",
				Style: "text-emerald-600 font-semibold",
				Label: "Deployment",
			},
			{
				Name:  "cloud.provider",
				Type:  "string",
				Style: "text-purple-600 text-sm",
				Label: "Cloud Provider",
			},
			{
				Name:  "cloud.region",
				Type:  "string",
				Style: "text-purple-600 text-sm",
				Label: "Region",
			},
			{
				Name:  "agent.type",
				Type:  "string",
				Style: "text-blue-500 text-sm font-medium",
				Label: "Agent",
			},
		},
	}
}

// GetJaegerSchema returns an optimized schema for Jaeger trace logs
func GetJaegerSchema() *api.PrettyObject {
	return &api.PrettyObject{
		Fields: []api.PrettyField{
			{
				Name:       "startTime",
				Type:       "string",
				Format:     "date",
				Style:      "text-gray-500 text-sm font-mono",
				DateFormat: "15:04:05.000",
				Label:      "Start Time",
			},
			{
				Name:  "traceID",
				Type:  "string",
				Style: "text-purple-600 font-mono text-xs",
				Label: "Trace ID",
			},
			{
				Name:  "spanID",
				Type:  "string",
				Style: "text-blue-600 font-mono text-xs",
				Label: "Span ID",
			},
			{
				Name:  "serviceName",
				Type:  "string",
				Style: "text-blue-700 font-bold",
				Label: "Service",
			},
			{
				Name:  "operationName",
				Type:  "string",
				Style: "text-indigo-600 font-medium",
				Label: "Operation",
			},
			{
				Name:  "duration",
				Type:  "float",
				Style: "text-right font-mono",
				Label: "Duration (μs)",
				ColorOptions: map[string]string{
					"green":  "< 100000",   // < 100ms
					"yellow": "< 500000",   // < 500ms
					"orange": "< 1000000",  // < 1s
					"red":    ">= 1000000", // >= 1s
				},
			},
			{
				Name:  "span.kind",
				Type:  "string",
				Style: "bg-gray-100 text-gray-800 px-2 py-1 rounded text-xs font-medium uppercase",
				Label: "Kind",
				ColorOptions: map[string]string{
					"blue":   "CLIENT",
					"green":  "SERVER",
					"yellow": "PRODUCER",
					"orange": "CONSUMER",
					"purple": "INTERNAL",
				},
			},
			{
				Name:  "error",
				Type:  "boolean",
				Style: "font-bold",
				Label: "Error",
				ColorOptions: map[string]string{
					"red":   "true",
					"green": "false",
				},
			},
			{
				Name:  "http.status_code",
				Type:  "int",
				Style: "font-mono text-center",
				Label: "Status",
				ColorOptions: map[string]string{
					"green":  "200-299",
					"yellow": "300-399",
					"orange": "400-499",
					"red":    "500-599",
				},
			},
			{
				Name:  "http.method",
				Type:  "string",
				Style: "bg-blue-100 text-blue-800 px-2 py-1 rounded text-xs font-bold uppercase",
				Label: "Method",
			},
			{
				Name:  "http.url",
				Type:  "string",
				Style: "text-blue-500 underline font-mono text-sm",
				Label: "URL",
			},
		},
	}
}

// GetCombinedSchema returns a schema that handles both Kubernetes and Jaeger fields
func GetCombinedSchema() *api.PrettyObject {
	k8sSchema := GetKubernetesSchema()
	jaegerSchema := GetJaegerSchema()

	// Combine both schemas, prioritizing timestamp from Kubernetes
	combined := &api.PrettyObject{
		Fields: []api.PrettyField{},
	}

	// Start with Kubernetes timestamp
	combined.Fields = append(combined.Fields, k8sSchema.Fields[0]) // @timestamp

	// Add key Jaeger fields
	jaegerFields := []string{"traceID", "spanID", "serviceName", "operationName", "duration", "error"}
	for _, jaegerField := range jaegerSchema.Fields {
		for _, fieldName := range jaegerFields {
			if jaegerField.Name == fieldName {
				combined.Fields = append(combined.Fields, jaegerField)
				break
			}
		}
	}

	// Add key Kubernetes fields (skip timestamp since we already added it)
	k8sFields := []string{"kubernetes.namespace", "kubernetes.pod.name", "kubernetes.container.name", "message", "level"}
	for _, k8sField := range k8sSchema.Fields[1:] { // Skip timestamp
		for _, fieldName := range k8sFields {
			if k8sField.Name == fieldName {
				combined.Fields = append(combined.Fields, k8sField)
				break
			}
		}
	}

	return combined
}

// GetPresetSchema returns a preset schema by name
func GetPresetSchema(preset string) (*api.PrettyObject, bool) {
	switch preset {
	case "kubernetes", "k8s", "filebeat":
		return GetKubernetesSchema(), true
	case "jaeger", "traces", "tracing":
		return GetJaegerSchema(), true
	case "combined", "both":
		return GetCombinedSchema(), true
	default:
		return nil, false
	}
}

// GetKubernetesFieldSuggestions returns common Kubernetes field names for completion
func GetKubernetesFieldSuggestions() []string {
	return []string{
		"@timestamp",
		"kubernetes.namespace",
		"kubernetes.pod.name",
		"kubernetes.pod.uid",
		"kubernetes.container.name",
		"kubernetes.container.image",
		"kubernetes.node.name",
		"kubernetes.deployment.name",
		"kubernetes.replicaset.name",
		"kubernetes.daemonset.name",
		"kubernetes.statefulset.name",
		"kubernetes.labels.app",
		"kubernetes.labels.version",
		"kubernetes.annotations.deployment.kubernetes.io/revision",
		"container.id",
		"container.runtime",
		"cloud.provider",
		"cloud.region",
		"cloud.availability_zone",
		"agent.hostname",
		"agent.type",
		"agent.version",
		"input.type",
		"ecs.version",
		"message",
		"level",
		"logger",
		"stream",
	}
}

// GetJaegerFieldSuggestions returns common Jaeger field names for completion
func GetJaegerFieldSuggestions() []string {
	return []string{
		"traceID",
		"spanID",
		"parentSpanID",
		"operationName",
		"serviceName",
		"startTime",
		"duration",
		"span.kind",
		"error",
		"http.status_code",
		"http.method",
		"http.url",
		"http.route",
		"http.user_agent",
		"db.type",
		"db.statement",
		"db.name",
		"db.operation",
		"rpc.service",
		"rpc.method",
		"tags.component",
		"tags.http.method",
		"tags.http.status_code",
		"tags.error",
		"process.tags.hostname",
		"process.tags.ip",
		"process.tags.jaeger.version",
		"references.refType",
		"references.traceID",
		"references.spanID",
		"logs.timestamp",
		"logs.fields.level",
		"logs.fields.event",
	}
}
