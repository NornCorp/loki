package cli

import (
	"fmt"
	"os"

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
	validateCmd.Flags().StringVarP(&validateConfigPath, "config", "c", "", "path to configuration file (required)")
	validateCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(validateCmd)
}

func runValidate(cmd *cobra.Command, args []string) error {
	// Check if config file exists
	if _, err := os.Stat(validateConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s", validateConfigPath)
	}

	// TODO: Implement config validation
	fmt.Printf("Validating configuration file: %s\n", validateConfigPath)
	fmt.Println("Validation functionality will be implemented in Phase 2")

	return nil
}
