package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestVersionFlag(t *testing.T) {
	t.Parallel()

	var output bytes.Buffer
	if err := run([]string{"--version"}, strings.NewReader(""), &output); err != nil {
		t.Fatalf("run: %v", err)
	}
	if strings.TrimSpace(output.String()) == "" {
		t.Fatal("version output is empty")
	}
}
