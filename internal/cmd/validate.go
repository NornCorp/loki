package cmd

import (
	"fmt"
	"os"

	"github.com/norncorp/loki/internal/config/parser"
	"github.com/spf13/cobra"
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate a Loki configuration file",
	Long:  `Validate a Loki configuration file for syntax and semantic errors.`,
	RunE:  runValidate,
}

var validateConfigPath string

func init() {
	validateCmd.Flags().StringVarP(&validateConfigPath, "config", "c", "", "path to configuration file or directory (required)")
	validateCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(validateConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s", validateConfigPath)
	}

	cfg, err := parser.ParseFile(validateConfigPath)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	if err := parser.Validate(cfg); err != nil {
		return fmt.Errorf("invalid config: %w", err)
	}

	fmt.Printf("Configuration file %s is valid.\n", validateConfigPath)
	return nil
}
