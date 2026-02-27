package cli

import (
	"fmt"
	"os"

	"github.com/norncorp/loki/internal/cligen"
	"github.com/norncorp/loki/internal/config"
	"github.com/spf13/cobra"
)

var cliCmd = &cobra.Command{
	Use:   "cli",
	Short: "Run a CLI defined in an HCL config file",
	Long: `Run a CLI defined in an HCL configuration file.

Use -- to separate loki flags from CLI arguments:
  loki cli -c config.hcl -- <args>

Example:
  loki cli -c examples/mimir-cli.hcl -- kv get mysecret
  loki cli -c examples/mimir-cli.hcl -- --help`,
	RunE:         runCLI,
	Args:         cobra.ArbitraryArgs,
	SilenceUsage: true,
}

var cliConfigPath string

func init() {
	cliCmd.Flags().StringVarP(&cliConfigPath, "config", "c", "", "path to CLI configuration file (required)")
	cliCmd.MarkFlagRequired("config")
	rootCmd.AddCommand(cliCmd)
}

func runCLI(cmd *cobra.Command, args []string) error {
	if _, err := os.Stat(cliConfigPath); os.IsNotExist(err) {
		return fmt.Errorf("configuration file not found: %s", cliConfigPath)
	}

	cfg, err := config.ParseFile(cliConfigPath)
	if err != nil {
		return fmt.Errorf("failed to parse config: %w", err)
	}

	if err := config.ValidateCLI(cfg); err != nil {
		return fmt.Errorf("invalid CLI config: %w", err)
	}

	builtCmd := cligen.BuildCommand(cfg.CLI)
	builtCmd.SetArgs(args)
	return builtCmd.ExecuteContext(cmd.Context())
}
