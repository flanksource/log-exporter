package cmd

import (
	"fmt"
	"os"
	"time"

	"github.com/samber/lo"
	"github.com/spf13/cobra"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/log-exporter/pkg/opensearch"
)

type opensearchFlags struct {
	host          string
	username      string
	password      string
	index         string
	query         string
	queryFile     string
	fields        string
	from          string
	to            string
	limit         int
	output        string
	schema        string
	preset        string
	autoDetect    bool
	k8sNamespace  string
	k8sPod        string
	k8sDeployment string
	k8sContainer  string
	otelService   string
	otelOperation string
	sample        bool
	sampleOutput  string
	sampleSize    int
	scrollSize    int
	scrollTimeout string
	noScroll      bool
}

var osFlags = &opensearchFlags{}

// opensearchCmd represents the opensearch command
var opensearchCmd = &cobra.Command{
	Use:          "opensearch",
	SilenceUsage: true,
	Short:        "Export logs from OpenSearch/Elasticsearch",
	Long: `Export logs from OpenSearch or Elasticsearch clusters with advanced querying capabilities.
Supports Lucene query strings and full OpenSearch JSON Query DSL, dynamic completion for index names
and field names, automatic schema generation, and all clicky output formats.`,
	Example: `  # Basic export with Lucene query string
  log-exporter export opensearch --host https://opensearch.example.com --index "logs-*" --query "level:ERROR"

  # Export with time range
  log-exporter export opensearch --host https://opensearch.example.com --index app-logs --from "now-24h" --to "now"

  # JSON Query DSL - simple match query
  log-exporter export opensearch --index logs --query '{"match": {"level": "ERROR"}}'

  # JSON Query DSL - complex bool query with time range
  log-exporter export opensearch --index logs \
    --query '{"bool": {"must": [{"match": {"service": "api"}}, {"term": {"status": 500}}]}}' \
    --from "now-24h" --to "now"

  # Query from JSON file
  log-exporter export opensearch --index logs --query-file complex-query.json

  # Export specific fields to CSV
  log-exporter export opensearch --index logs --fields "timestamp,message,host,severity" --format csv -o logs.csv

  # Export with field renaming (use colon to specify alias)
  log-exporter export opensearch --index logs --fields "kubernetes.namespace:ns,kubernetes.pod.name:pod,message" --format csv -o logs.csv

  # Use embedded schema for field mapping and formatting
  log-exporter export opensearch --index logs --schema kubernetes --format html -o report.html

  # Use custom schema file for formatting
  log-exporter export opensearch --index logs --schema custom-log-schema.yaml --format html -o report.html

  # Filter Kubernetes logs by namespace, pod, and container
  log-exporter export opensearch --index "filebeat-*" --k8s-namespace "production" --k8s-pod "api-*" --k8s-container "app"

  # Filter OpenTelemetry traces by service and operation
  log-exporter export opensearch --index "jaeger-*" --otel-service "user-service" --otel-operation "GetUser"

  # Combine filters with existing queries
  log-exporter export opensearch --index logs --query "level:ERROR" --k8s-namespace "staging"

  # Export sample data for testing
  log-exporter export opensearch --index "jaeger-span-*" --from "now-7d" --to "now" --sample

  # Export sample data with custom size and output
  log-exporter export opensearch --index "filebeat-*" --from "2024-01-01" --to "2024-01-07" --sample --sample-size 200 --sample-output ./testdata

  # Export large result set with custom scroll settings
  log-exporter export opensearch --index logs --limit 50000 --scroll-size 2000 --scroll-timeout "2m"

  # Export with authentication
  log-exporter export opensearch --host https://opensearch.example.com --username admin --password secret --index logs`,
	RunE: runOpensearchExport,
}

func init() {
	// Add import subcommand
	opensearchCmd.AddCommand(opensearchImportCmd)

	flags := opensearchCmd.Flags()

	// Connection flags
	opensearchCmd.PersistentFlags().StringVar(&osFlags.host, "host", lo.CoalesceOrEmpty(os.Getenv("OPENSEARCH_HOST"), "http://localhost:9200"), "OpenSearch host URL read from OPENSEARCH_HOST env variable if not set")
	opensearchCmd.PersistentFlags().StringVarP(&osFlags.username, "username", "u", os.Getenv("OPENSEARCH_USERNAME"), "Username for authentication read from OPENSEARCH_USERNAME env variable if not set")
	opensearchCmd.PersistentFlags().StringVarP(&osFlags.password, "password", "p", os.Getenv("OPENSEARCH_PASSWORD"), "Password for authentication read from OPENSEARCH_PASSWORD env variable if not set")

	// Query flags
	flags.StringVarP(&osFlags.index, "index", "i", "", "Index name or pattern (required)")
	flags.StringVarP(&osFlags.query, "query", "q", "*", "Lucene query string or OpenSearch JSON Query DSL")
	flags.StringVar(&osFlags.queryFile, "query-file", "", "Read query from JSON file (alternative to --query)")
	flags.StringVar(&osFlags.fields, "fields", "", "Comma-separated list of fields to include. Use 'field:alias' to rename fields (e.g., 'kubernetes.namespace:ns,message')")
	flags.StringVar(&osFlags.from, "from", "", "Start time (e.g., 'now-24h', 'now-7d/d', '2023-01-01T00:00:00Z')")
	flags.StringVar(&osFlags.to, "to", "", "End time (e.g., 'now', 'now/d', '2023-01-02T00:00:00Z')")
	flags.IntVarP(&osFlags.limit, "limit", "l", 500, "Maximum number of records to export")

	// Output flags
	flags.StringVarP(&osFlags.output, "output", "o", "", "Output file path")
	flags.StringVar(&osFlags.schema, "schema", "", "Use embedded schema (kubernetes, otel, otel.http, otel.db, syslog) or custom schema file")
	flags.StringVar(&osFlags.preset, "preset", "", "Use preset schema (kubernetes, jaeger, combined) - deprecated, use --schema instead")
	flags.BoolVar(&osFlags.autoDetect, "auto-detect", true, "Automatically detect log type from index pattern")

	// Filter flags
	flags.StringVar(&osFlags.k8sNamespace, "k8s-namespace", "", "Filter by Kubernetes namespace")
	flags.StringVar(&osFlags.k8sPod, "k8s-pod", "", "Filter by Kubernetes pod name")
	flags.StringVar(&osFlags.k8sDeployment, "k8s-deployment", "", "Filter by Kubernetes deployment name")
	flags.StringVar(&osFlags.k8sContainer, "k8s-container", "", "Filter by Kubernetes container name")
	flags.StringVar(&osFlags.otelService, "otel-service", "", "Filter by OpenTelemetry service name")
	flags.StringVar(&osFlags.otelOperation, "otel-operation", "", "Filter by OpenTelemetry operation name")

	// Sample flags
	flags.BoolVar(&osFlags.sample, "sample", false, "Export sample data for testing (ignores format and output settings)")
	flags.StringVar(&osFlags.sampleOutput, "sample-output", "./opensearch-sample", "Output directory for sample data")
	flags.IntVar(&osFlags.sampleSize, "sample-size", 100, "Number of sample records to export per index")

	// Scroll flags
	flags.IntVar(&osFlags.scrollSize, "scroll-size", 1000, "Number of documents to fetch per scroll batch")
	flags.StringVar(&osFlags.scrollTimeout, "scroll-timeout", "1m", "Keep-alive time for scroll context (e.g., '1m', '30s')")
	flags.BoolVar(&osFlags.noScroll, "no-scroll", false, "Disable scroll API even for large result sets")

	// Mark required flags
	opensearchCmd.MarkFlagRequired("index")

	// Add dynamic completion
	opensearchCmd.RegisterFlagCompletionFunc("index", completeIndexNames)
	opensearchCmd.RegisterFlagCompletionFunc("fields", completeFieldNames)
	opensearchCmd.RegisterFlagCompletionFunc("format", completeFormats)
	opensearchCmd.RegisterFlagCompletionFunc("preset", completePresets)
	opensearchCmd.RegisterFlagCompletionFunc("schema", completeSchemas)
}

func runOpensearchExport(cmd *cobra.Command, args []string) error {
	// Check if sample mode is enabled
	if osFlags.sample {
		return runSampleExport(cmd, args)
	}

	// Handle query file vs query flag
	query := osFlags.query
	if osFlags.queryFile != "" {
		if osFlags.query != "*" {
			return fmt.Errorf("cannot specify both --query and --query-file flags")
		}

		queryBytes, err := os.ReadFile(osFlags.queryFile)
		if err != nil {
			return fmt.Errorf("failed to read query file '%s': %w", osFlags.queryFile, err)
		}

		query = string(queryBytes)

		if IsVerbose() {
			logger.Infof("Read query from file: %s\n", osFlags.queryFile)
		}
	}

	if IsVerbose() {
		logger.Infof("Connecting to OpenSearch at: %s\n", osFlags.host)
		logger.Infof("Exporting from index: %s\n", osFlags.index)
		if query != "*" {
			logger.Infof("Query: %s\n", query)
		}
	}

	// Create OpenSearch client
	config := opensearch.Config{
		Host:     osFlags.host,
		Username: osFlags.username,
		Password: osFlags.password,
		Debug:    IsDebug(),
		Verbose:  IsVerbose(),
	}

	client, err := opensearch.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Parse scroll timeout
	var scrollTimeout time.Duration
	if osFlags.scrollTimeout != "" {
		var err error
		scrollTimeout, err = time.ParseDuration(osFlags.scrollTimeout)
		if err != nil {
			return fmt.Errorf("invalid scroll timeout '%s': %w", osFlags.scrollTimeout, err)
		}
	}

	// Parse fields with potential aliases
	fieldsWithAliases := opensearch.ParseFields(osFlags.fields)

	// Build export options
	exportOpts := opensearch.ExportOptions{
		Index:        osFlags.index,
		Query:        query,
		Fields:       fieldsWithAliases.Fields,
		FieldAliases: fieldsWithAliases.Aliases,
		From:         osFlags.from, // Pass raw string, will be parsed with datemath support
		To:           osFlags.to,   // Pass raw string, will be parsed with datemath support
		Limit:        osFlags.limit,
		Output:       osFlags.output,
		Schema:       osFlags.schema,
		AutoDetect:   osFlags.autoDetect,
		Filters: opensearch.FilterOptions{
			K8sNamespace:  osFlags.k8sNamespace,
			K8sPod:        osFlags.k8sPod,
			K8sDeployment: osFlags.k8sDeployment,
			K8sContainer:  osFlags.k8sContainer,
			OtelService:   osFlags.otelService,
			OtelOperation: osFlags.otelOperation,
		},
		Scroll: opensearch.ScrollConfig{
			Size:    osFlags.scrollSize,
			Timeout: scrollTimeout,
			Enabled: !osFlags.noScroll,
		},
	}

	// Perform export
	return client.Export(exportOpts)
}

func runSampleExport(cmd *cobra.Command, args []string) error {
	if IsVerbose() {
		logger.Infof("Starting sample export from: %s\n", osFlags.host)
		logger.Infof("Index pattern: %s\n", osFlags.index)
		logger.Infof("Sample size: %d per index\n", osFlags.sampleSize)
		logger.Infof("Output directory: %s\n", osFlags.sampleOutput)
	}

	// Create OpenSearch client
	config := opensearch.Config{
		Host:     osFlags.host,
		Username: osFlags.username,
		Password: osFlags.password,
		Debug:    IsDebug(),
		Verbose:  IsVerbose(),
	}

	client, err := opensearch.NewClient(config)
	if err != nil {
		return fmt.Errorf("failed to create OpenSearch client: %w", err)
	}

	// Build sample options
	sampleOpts := opensearch.SampleOptions{
		Index:      osFlags.index,
		From:       osFlags.from,
		To:         osFlags.to,
		SampleSize: osFlags.sampleSize,
		OutputDir:  osFlags.sampleOutput,
		Filters: opensearch.FilterOptions{
			K8sNamespace:  osFlags.k8sNamespace,
			K8sPod:        osFlags.k8sPod,
			K8sDeployment: osFlags.k8sDeployment,
			K8sContainer:  osFlags.k8sContainer,
			OtelService:   osFlags.otelService,
			OtelOperation: osFlags.otelOperation,
		},
	}

	// Perform sample export
	return client.ExportSample(sampleOpts)
}

// parseTime is no longer needed - time parsing is handled in the opensearch package with full datemath support

// Completion functions
func completeIndexNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Create a temporary client for completion
	config := opensearch.Config{
		Host:     osFlags.host,
		Username: osFlags.username,
		Password: osFlags.password,
		Debug:    false,
		Verbose:  false,
	}

	client, err := opensearch.NewClient(config)
	if err != nil {
		// Return fallback options if connection fails
		return []string{
			"logs-*",
			"logstash-*",
			"filebeat-*",
			"application-*",
			"system-*",
		}, cobra.ShellCompDirectiveDefault
	}

	indices, err := client.GetIndexCompletion(toComplete)
	if err != nil {
		return []string{}, cobra.ShellCompDirectiveDefault
	}

	return indices, cobra.ShellCompDirectiveDefault
}

func completeFieldNames(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// Create a temporary client for completion
	config := opensearch.Config{
		Host:     osFlags.host,
		Username: osFlags.username,
		Password: osFlags.password,
		Debug:    false,
		Verbose:  false,
	}

	client, err := opensearch.NewClient(config)
	if err != nil {
		// Return fallback options if connection fails
		return []string{
			"@timestamp",
			"timestamp",
			"message",
			"level",
			"severity",
			"host",
			"source",
		}, cobra.ShellCompDirectiveDefault
	}

	fields, err := client.GetFieldCompletion(osFlags.index, toComplete)
	if err != nil {
		return []string{}, cobra.ShellCompDirectiveDefault
	}

	return fields, cobra.ShellCompDirectiveDefault
}

func completeFormats(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"table",
		"json",
		"yaml",
		"csv",
		"html",
		"pdf",
		"markdown",
	}, cobra.ShellCompDirectiveDefault
}

func completePresets(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	return []string{
		"kubernetes",
		"k8s",
		"filebeat",
		"jaeger",
		"traces",
		"tracing",
		"combined",
		"both",
	}, cobra.ShellCompDirectiveDefault
}

func completeSchemas(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	// First try to get embedded clicky schemas
	schemas, err := opensearch.ListEmbeddedClickySchemas()
	if err == nil && len(schemas) > 0 {
		return schemas, cobra.ShellCompDirectiveDefault
	}

	// Fallback to hardcoded list if embedded schemas can't be loaded
	return []string{
		"kubernetes",
		"otel",
		"otel.http",
		"otel.db",
		"syslog",
	}, cobra.ShellCompDirectiveDefault
}
