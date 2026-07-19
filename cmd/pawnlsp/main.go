package main

import (
	"fmt"
	"os"

	"github.com/pawnkit/pawnlsp/lsp"
)

func main() {
	if err := lsp.Run(os.Stdin, os.Stdout); err != nil {
		_, _ = fmt.Fprintln(os.Stderr, "pawnlsp:", err)
		os.Exit(1)
	}
}
