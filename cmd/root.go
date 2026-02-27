package cmd

import (
	"os"
	"path/filepath"

	"github.com/flanksource/clicky"
	"github.com/flanksource/commons/logger"
	"github.com/spf13/cobra"
)

var (
	cfgFile string
)

// rootCmd represents the base command when called without any subcommands
var rootCmd = &cobra.Command{
	Use:   "log-exporter",
	Short: "Export logs from various data sources into different formats",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		clicky.Flags.UseFlags()
	},
	Long: `A CLI tool for exporting logs from various data sources (OpenSearch, CloudWatch, Loki, etc.)
and formatting them using clicky's powerful formatting capabilities.

Features:
- Export from OpenSearch with dynamic index/field completion
- Multiple output formats (table, json, yaml, csv, html, pdf, markdown)
- Auto-generated schemas with customizable styling
- Time range queries and advanced filtering
- Shell completion for indexes and fields`,
	Example: `  # Export OpenSearch logs as a table
  log-exporter export opensearch --host https://opensearch.example.com --index "logs-*" --query "level:ERROR"

  # Export to CSV with specific fields
  log-exporter export opensearch --index app-logs --fields "timestamp,message,host" --format csv -o logs.csv

  # Use custom schema for formatting
  log-exporter export opensearch --index logs --schema my-schema.yaml --format html -o report.html`,
}

// Execute adds all child commands to the root command and sets flags appropriately.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)

	clicky.BindAllFlags(rootCmd.PersistentFlags())

	// Global flags
	rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.log-exporter.yaml)")

	// Add completion command
	rootCmd.AddCommand(completionCmd)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		// viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := os.UserHomeDir()
		cobra.CheckErr(err)

		// Search config in home directory with name ".log-exporter" (without extension).
		configPath := filepath.Join(home, ".log-exporter.yaml")
		if _, err := os.Stat(configPath); err == nil {
			cfgFile = configPath
		}
	}

	// If a config file is found, read it in.
	if cfgFile != "" {
		logger.Infof("Using config file: %s\n", cfgFile)
	}
}

// completionCmd represents the completion command
var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate completion script",
	Long: `To load completions:

Bash:

  $ source <(log-exporter completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ log-exporter completion bash > /etc/bash_completion.d/log-exporter
  # macOS:
  $ log-exporter completion bash > $(brew --prefix)/etc/bash_completion.d/log-exporter

Zsh:

  # If shell completion is not already enabled in your environment,
  # you will need to enable it.  You can execute the following once:

  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ log-exporter completion zsh > "${fpath[1]}/_log-exporter"

  # You will need to start a new shell for this setup to take effect.

fish:

  $ log-exporter completion fish | source

  # To load completions for each session, execute once:
  $ log-exporter completion fish > ~/.config/fish/completions/log-exporter.fish

PowerShell:

  PS> log-exporter completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> log-exporter completion powershell > log-exporter.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.ExactValidArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		switch args[0] {
		case "bash":
			cmd.Root().GenBashCompletion(os.Stdout)
		case "zsh":
			cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
	},
}

// Global flag accessors
func IsVerbose() bool {
	return clicky.Flags.LevelCount > 0
}

func IsDebug() bool {
	return clicky.Flags.LevelCount > 1
}

func GetConfigFile() string {
	return cfgFile
}
