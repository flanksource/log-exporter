package cmd

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/flanksource/commons/logger"
	"github.com/flanksource/log-exporter/pkg/opensearch"
)

type importFlags struct {
	samples string

	batchSize int
	force     bool
}

var importOsFlags = &importFlags{}

// opensearchImportCmd represents the import command for opensearch
var opensearchImportCmd = &cobra.Command{
	Use:          "import",
	SilenceUsage: true,
	Short:        "Import sample data with mappings into OpenSearch",
	Long: `Import sample data and index mappings into OpenSearch from JSON files.
Supports both structured sample files with mappings (like jaeger.json, logstash.json)
and raw data files (like traces.json). Automatically creates indices with proper
mappings and imports the data.`,
	Example: `  # Import logstash sample with mappings
  log-exporter import opensearch --samples opensearch-sample/logstash.json

  # Import jaeger sample with authentication
  log-exporter import opensearch --samples opensearch-sample/jaeger.json \
    --host https://opensearch.example.com --username admin --password secret

  # Import traces.json (data only, auto-generate mappings)
  log-exporter import opensearch --samples traces.json --force

  # Import with custom batch size
  log-exporter import opensearch --samples large-dataset.json --batch-size 5000`,
	RunE: runOpensearchImport,
}

func init() {
	rootCmd.AddCommand(opensearchImportCmd)
	flags := opensearchImportCmd.Flags()

	// Input flags
	flags.StringVar(&importOsFlags.samples, "samples", "", "Path to sample data file (required)")

	// Import flags
	flags.IntVar(&importOsFlags.batchSize, "batch-size", 1000, "Number of documents per batch")
	flags.BoolVar(&importOsFlags.force, "force", false, "Delete index if it exists before importing")

	// Mark required flags
	opensearchImportCmd.MarkFlagRequired("samples")
}

func runOpensearchImport(cmd *cobra.Command, args []string) error {
	if IsVerbose() {
		logger.Infof("Starting OpenSearch import from: %s\n", importOsFlags.samples)
		logger.Infof("Target host: %s\n", osFlags.host)
		logger.Infof("Batch size: %d\n", importOsFlags.batchSize)
		if importOsFlags.force {
			logger.Infof("Force mode: will delete existing indices\n")
		}
	}

	// Check if sample file exists
	if _, err := os.Stat(importOsFlags.samples); os.IsNotExist(err) {
		return fmt.Errorf("sample file does not exist: %s", importOsFlags.samples)
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

	// Build import options
	importOpts := opensearch.ImportOptions{
		SampleFile: importOsFlags.samples,
		BatchSize:  importOsFlags.batchSize,
		Force:      importOsFlags.force,
	}

	// Perform import
	return client.ImportSamples(importOpts)
}
