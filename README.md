# Log Exporter CLI

A powerful CLI tool for exporting logs from various data sources (OpenSearch, CloudWatch, Loki, etc.) and formatting them using [clicky's](https://github.com/flanksource/clicky) advanced formatting capabilities.

## Features

- **OpenSearch/Elasticsearch Support**: Export logs with advanced querying
- **JSON Query DSL**: Full support for OpenSearch/Elasticsearch JSON Query DSL alongside Lucene queries
- **Kubernetes/Filebeat Logs**: Specialized schema and field detection for Kubernetes logs
- **Jaeger Trace Support**: Optimized formatting for distributed tracing data
- **Preset Schemas**: Built-in support for common log types (kubernetes, jaeger, combined)
- **Auto-Detection**: Automatically detects log types from index patterns
- **Dynamic Shell Completion**: Auto-complete index names and field names with context awareness
- **Multiple Output Formats**: table, json, yaml, csv, html, pdf, markdown
- **Schema-Driven Formatting**: Auto-generate schemas from OpenSearch mappings
- **Time Range Queries**: Support for datemath expressions
- **Field Filtering**: Export specific fields only
- **Styled Output**: Rich formatting with colors and styling

## Installation

```bash
go build -o log-exporter .
```

## Usage

### Basic OpenSearch Export

```bash
# Export recent logs as a formatted table
./log-exporter export opensearch --host https://opensearch.example.com --index "logs-*" --query "level:ERROR"

# Export with time range
./log-exporter export opensearch --index app-logs --from "now-24h" --to "now" --format table

# Export specific fields to CSV
./log-exporter export opensearch --index logs --fields "timestamp,message,host,severity" --format csv -o logs.csv
```

### Kubernetes/Filebeat Logs

```bash
# Export Kubernetes logs with preset schema and auto-detection
./log-exporter export opensearch --index "filebeat-*" --preset kubernetes --format table

# Auto-detect Kubernetes logs from index pattern
./log-exporter export opensearch --index "k8s-logs-*" --auto-detect --query "kubernetes.namespace:production"

# Export specific Kubernetes fields to HTML report
./log-exporter export opensearch \
  --index "filebeat-*" \
  --fields "timestamp,kubernetes.namespace,kubernetes.pod.name,kubernetes.container.name,message,level" \
  --preset kubernetes \
  --format html -o k8s-report.html
```

### Jaeger Trace Logs

```bash
# Export Jaeger traces with performance analysis
./log-exporter export opensearch --index "jaeger-*" --preset jaeger --query "duration:>1000000"

# Auto-detect Jaeger traces and export errors
./log-exporter export opensearch --index "jaeger-span-*" --auto-detect --query "error:true"

# Export trace details to JSON
./log-exporter export opensearch \
  --index "jaeger-*" \
  --fields "startTime,traceID,spanID,serviceName,operationName,duration,error" \
  --preset jaeger \
  --format json -o traces.json
```

### JSON Query DSL Support

The tool supports both Lucene query strings and full OpenSearch JSON Query DSL for advanced querying:

```bash
# Simple JSON match query
./log-exporter export opensearch --index logs --query '{"match": {"level": "ERROR"}}'

# Complex bool query with multiple conditions
./log-exporter export opensearch --index logs \
  --query '{"bool": {"must": [{"match": {"service": "api"}}, {"term": {"status": 500}}]}}' \
  --from "now-24h" --to "now"

# Range query with field filtering
./log-exporter export opensearch --index logs \
  --query '{"range": {"duration": {"gte": 1000}}}' \
  --fields "timestamp,service,duration,message"

# Aggregation query for analytics
./log-exporter export opensearch --index logs \
  --query '{"aggs": {"status_codes": {"terms": {"field": "status"}}, "avg_duration": {"avg": {"field": "duration"}}}}'

# Query from file for complex queries
./log-exporter export opensearch --index logs --query-file complex-query.json

# Nested query for structured logs
./log-exporter export opensearch --index logs \
  --query '{"nested": {"path": "error", "query": {"match": {"error.type": "TimeoutException"}}}}'

# Function score query for custom relevance
./log-exporter export opensearch --index logs \
  --query '{"function_score": {"query": {"match": {"message": "error"}}, "boost": "5", "random_score": {}}}'
```

**JSON Query Features:**
- **Automatic Detection**: Queries starting with `{` are detected as JSON automatically
- **Time Range Integration**: `--from` and `--to` flags work seamlessly with JSON queries
- **Field Filtering**: `--fields` flag applies to JSON queries just like Lucene queries
- **Validation**: Invalid JSON queries are caught with helpful error messages
- **File Support**: Use `--query-file` for complex queries stored in files

### Combined Kubernetes + Jaeger

```bash
# Use combined schema for environments with both log types
./log-exporter export opensearch \
  --index "logs-*" \
  --preset combined \
  --query "kubernetes.namespace:production OR serviceName:user-service"

# JSON query for combined logs with precise matching
./log-exporter export opensearch \
  --index "logs-*" \
  --preset combined \
  --query '{"bool": {"should": [{"match": {"kubernetes.namespace": "production"}}, {"match": {"serviceName": "user-service"}}]}}'
```

### Custom Schema Formatting

```bash
# Use custom clicky schema for advanced formatting
./log-exporter export opensearch --index logs --schema custom-log-schema.yaml --format html -o report.html
```

### Authentication

```bash
# With basic auth
./log-exporter export opensearch --host https://opensearch.example.com --username admin --password secret --index logs
```

## Shell Completion

Enable dynamic completion for indexes and fields:

### Bash
```bash
source <(./log-exporter completion bash)
# Or install permanently:
./log-exporter completion bash > /etc/bash_completion.d/log-exporter
```

### Zsh
```bash
source <(./log-exporter completion zsh)
# Or install permanently:
./log-exporter completion zsh > "${fpath[1]}/_log-exporter"
```

### Fish
```bash
./log-exporter completion fish | source
# Or install permanently:
./log-exporter completion fish > ~/.config/fish/completions/log-exporter.fish
```

## Command Reference

### Global Flags

- `--config string`: Config file (default: `$HOME/.log-exporter.yaml`)
- `--verbose, -v`: Verbose output
- `--debug`: Debug output

### OpenSearch Export Flags

**Connection:**
- `--host string`: OpenSearch host URL (default: `http://localhost:9200`)
- `--username, -u string`: Username for authentication
- `--password, -p string`: Password for authentication

**Query:**
- `--index, -i string`: Index name or pattern (required)
- `--query, -q string`: Lucene query string or OpenSearch JSON Query DSL (default: `*`)
- `--query-file string`: Read query from JSON file (alternative to --query)
- `--fields string`: Comma-separated list of fields to include
- `--from string`: Start time (e.g., `now-24h`, `2023-01-01T00:00:00Z`)
- `--to string`: End time (e.g., `now`, `2023-01-02T00:00:00Z`)
- `--limit, -l int`: Maximum number of records (default: 500)

**Output:**
- `--format, -f string`: Output format (default: `table`)
  - `table`: Formatted table
  - `json`: JSON output
  - `yaml`: YAML output
  - `csv`: CSV format
  - `html`: HTML report
  - `pdf`: PDF document
  - `markdown`: Markdown format
- `--output, -o string`: Output file path (default: stdout)
- `--schema string`: Custom clicky schema file
- `--preset string`: Use preset schema (kubernetes, jaeger, combined)
- `--auto-detect`: Automatically detect log type from index pattern

## Output Formats

### Table Format
Rich terminal table with colors and styling based on field types and values.

### JSON Format
```json
[
  {
    "timestamp": "2023-01-01T10:00:00Z",
    "message": "Application started",
    "level": "INFO",
    "host": "web-01"
  }
]
```

### CSV Format
```csv
timestamp,message,level,host
2023-01-01T10:00:00Z,Application started,INFO,web-01
```

### HTML/PDF Formats
Styled reports with tables, colors, and formatting suitable for sharing.

## Schema-Driven Formatting

The CLI automatically generates [clicky schemas](https://github.com/flanksource/clicky) from OpenSearch field mappings, applying intelligent styling:

### Preset Schemas

- **kubernetes**: Optimized for Kubernetes/Filebeat logs
  - Namespace badges, pod names, container identifiers
  - Cloud provider metadata styling
  - Log level color coding
- **jaeger**: Optimized for distributed tracing
  - Trace/span ID formatting
  - Performance color coding (duration thresholds)
  - HTTP status code styling
  - Service and operation highlighting
- **combined**: Hybrid schema for mixed log environments

### Auto-Detection

The tool automatically detects log types from index patterns:

- **Kubernetes**: `filebeat`, `kubernetes`, `k8s`, `eks`, `gke`, `aks`
- **Jaeger**: `jaeger`, `span`, `trace`, `otel`, `apm`

### Field-Specific Styling

- **Timestamps**: Formatted dates with gray styling
- **Log levels**: Color-coded badges (ERROR=red, WARN=yellow, INFO=green, DEBUG=blue)
- **Kubernetes fields**: Specialized styling for namespaces, pods, containers
- **Jaeger fields**: Trace IDs, durations with performance thresholds
- **Hosts**: Blue links
- **Messages**: Clean text formatting
- **IDs**: Monospace font
- **Counts**: Numeric formatting with color-coded thresholds

You can override the auto-generated schema with a custom YAML file:

```yaml
fields:
  - name: "@timestamp"
    type: "string"
    format: "date" 
    style: "text-gray-500 text-sm"
    
  - name: "level"
    type: "string"
    style: "font-bold uppercase px-2 py-1 rounded"
    color_options:
      red: "ERROR|FATAL"
      yellow: "WARN"
      green: "INFO"
      blue: "DEBUG"
```

## Testing

```bash
# Run all tests
go test ./pkg/...

# Test with mock data (no OpenSearch required)
go run hack/test_opensearch.go

# Build and run basic functionality test
go build -o log-exporter .
./log-exporter --help
```

## Dependencies

- [Cobra](https://github.com/spf13/cobra): CLI framework with completion support
- [Clicky](https://github.com/flanksource/clicky): Powerful data formatting library
- [Flanksource Duty](https://github.com/flanksource/duty): Log structures and OpenSearch integration
- [OpenSearch Go Client](https://github.com/opensearch-project/opensearch-go): Official OpenSearch client

## Development

The project structure follows Go best practices:

```
log-exporter/
├── cmd/                 # CLI commands and flags
├── pkg/
│   ├── opensearch/     # OpenSearch client and completion
│   └── schema/         # Schema generation from mappings
├── hack/               # Development and testing scripts  
├── main.go            # Entry point
└── README.md
```

## Advanced Examples

### Daily Error Report for Kubernetes

```bash
# Generate a comprehensive error report for the last 24 hours
./log-exporter export opensearch \
  --host https://opensearch.example.com \
  --index "filebeat-*" \
  --query "level:ERROR AND kubernetes.namespace:(production OR staging)" \
  --from "now-24h" \
  --fields "timestamp,kubernetes.namespace,kubernetes.pod.name,kubernetes.container.name,message,level" \
  --preset kubernetes \
  --format html -o daily-errors.html
```

### Performance Analysis with Jaeger

```bash
# Find slow operations (>500ms) in the last hour
./log-exporter export opensearch \
  --index "jaeger-span-*" \
  --query "duration:>500000" \
  --from "now-1h" \
  --fields "serviceName,operationName,duration,http.status_code,error,traceID" \
  --preset jaeger \
  --format csv -o slow-operations.csv
```

### Multi-Service Investigation

```bash
# Investigate a specific trace across services
./log-exporter export opensearch \
  --index "logs-*" \
  --query "traceID:abc123def456 OR (kubernetes.labels.app:user-service AND level:(ERROR OR WARN))" \
  --preset combined \
  --format markdown -o investigation.md
```

### Kubernetes Resource Usage Analysis

```bash
# Export logs with resource constraints and OOMKilled events
./log-exporter export opensearch \
  --index "k8s-events-*" \
  --query "message:(OOMKilled OR FailedScheduling OR ResourceQuota)" \
  --fields "timestamp,kubernetes.namespace,kubernetes.pod.name,message,reason" \
  --preset kubernetes \
  --format table
```

### Distributed Tracing Error Analysis

```bash
# Find all failed traces and related logs
./log-exporter export opensearch \
  --index "jaeger-*" \
  --query "error:true AND http.status_code:>=400" \
  --from "now-6h" \
  --fields "startTime,serviceName,operationName,http.method,http.url,http.status_code,duration" \
  --preset jaeger \
  --format json -o failed-requests.json
```

### Security Audit Log Export

```bash
# Export authentication and authorization events
./log-exporter export opensearch \
  --index "audit-*" \
  --query "kubernetes.verb:(create OR delete OR patch) AND kubernetes.objectRef.resource:(secrets OR configmaps)" \
  --fields "timestamp,kubernetes.user.username,kubernetes.verb,kubernetes.objectRef.name,kubernetes.objectRef.namespace" \
  --preset kubernetes \
  --format pdf -o security-audit.pdf
```

## Configuration File

Create `~/.log-exporter.yaml` for default settings:

```yaml
# Default OpenSearch connection
opensearch:
  host: https://opensearch.example.com
  username: admin
  # Use environment variables for sensitive data
  # password: ${OPENSEARCH_PASSWORD}

# Global defaults
defaults:
  format: table
  limit: 1000
  preset: kubernetes
  
# Named connections for different environments
connections:
  production:
    host: https://prod-opensearch.example.com
    username: readonly
  staging:
    host: https://staging-opensearch.example.com
    username: readonly
```

## Future Enhancements

- CloudWatch Logs integration
- Loki support  
- Kubernetes logs via kubectl
- Query templates and saved queries
- Named connection profiles
- Pagination for large result sets
- Real-time log streaming
- Log aggregation and statistics