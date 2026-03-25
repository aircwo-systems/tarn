package main

import (
	"fmt"
	"os"

	"github.com/aircwo-systems/tarn/internal/cli"
)

func main() {
	root := cli.NewRootCmd()
	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
