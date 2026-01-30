package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "loki",
	Short: "Loki - Fake service simulator",
	Long: `Loki is a fake service simulator for creating realistic microservice architectures.
It supports HTTP, PostgreSQL, TCP, and other protocols with configurable behaviors.`,
}

// Execute runs the root command
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.SetOut(os.Stdout)
	rootCmd.SetErr(os.Stderr)
}

// Fatal prints an error message and exits
func Fatal(msg string, args ...interface{}) {
	fmt.Fprintf(os.Stderr, "Error: "+msg+"\n", args...)
	os.Exit(1)
}
