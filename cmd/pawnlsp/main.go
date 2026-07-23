package main

import (
	"fmt"
	"io"
	"os"

	"github.com/pawnkit/pawnlsp/lsp"
)

var version = "dev"

func main() {
	if err := run(os.Args[1:], os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "pawnlsp:", err)
		os.Exit(1)
	}
}

func run(args []string, input io.Reader, output io.Writer) error {
	if len(args) == 1 && (args[0] == "--version" || args[0] == "-V") {
		_, err := fmt.Fprintln(output, version)
		return err
	}
	return lsp.Run(input, output)
}
