package cmd

import (
	"github.com/spf13/cobra"
)

func init() {
	rootCmd.AddCommand(exportCmd)
	exportCmd.AddCommand(opensearchCmd)
}

// exportCmd represents the export command
var exportCmd = &cobra.Command{
	Use:          "export",
	SilenceUsage: true,
	Short:        "Export logs from various data sources",
	Long: `Export logs from various data sources including OpenSearch, CloudWatch Logs,
Loki, and other log aggregation systems. The export command supports multiple output
formats and provides dynamic shell completion for indexes and fields.`,
	Example: `  # Export from OpenSearch
  log-exporter export opensearch --host https://opensearch.example.com --index "logs-*" --format table

  # Export with time range
  log-exporter export opensearch --index app-logs --from "now-1h" --to "now" --format json

  # Export specific fields to CSV
  log-exporter export opensearch --index logs --fields "timestamp,message,host" --format csv -o logs.csv`,
}
