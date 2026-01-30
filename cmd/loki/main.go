package main

import (
	"os"

	"github.com/norncorp/loki/internal/cli"
)

func main() {
	if err := cli.Execute(); err != nil {
		os.Exit(1)
	}
}
