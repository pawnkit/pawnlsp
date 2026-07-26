package main

import (
	"fmt"
	"io"
	"log"
	"os"

	analysis "github.com/pawnkit/pawn-analysis"
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
	options := lsp.RunOptions{}
	if os.Getenv("PAWNKIT_ANALYSIS_TRACE") != "" {
		logger := log.New(os.Stderr, "pawnlsp: analysis: ", 0)
		options.AnalysisTrace = func(uri string, version int, event analysis.TraceEvent) {
			logger.Printf("%s@%d %s %s reused=%d cancelled=%t", uri, version, event.Stage, event.Duration, event.Reused, event.Cancelled)
		}
	}
	return lsp.RunWithOptions(input, output, options)
}
