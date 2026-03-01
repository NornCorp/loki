package main

import (
	"os"

	"github.com/jumppad-labs/polymorph/internal/cmd"
)

func main() {
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
