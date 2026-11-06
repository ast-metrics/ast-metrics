// Command cribelo tells which files of a change are worth a human reader.
// The release binary is built from github.com/cribelo/cribelo; this one is
// for local development inside the AST Metrics module.
package main

import (
	"os"

	"github.com/ast-metrics/ast-metrics/cribelo"
)

func main() {
	os.Exit(cribelo.Run(os.Args, ""))
}
