# pawnlsp

An LSP server for SA-MP/open.mp Pawn. It consumes shared PawnKit libraries for project discovery, analysis, API metadata, diagnostics, and fixes.

## Install

```sh
go install github.com/pawnkit/pawnlsp/cmd/pawnlsp@latest
```

## Use

Configure your editor's Pawn language client to start `pawnlsp`
over standard input and output.

The server provides:

- diagnostics for open documents and indexed project files
- safe quick fixes
- documented completion, hover, signature help, and semantic highlighting
- document and workspace symbols, definitions, hover, references, and rename
- document highlights, folding, and structural selection
- whole-document, range, and format-on-type formatting through pawnfmt
- parameter-name inlay hints
- Pawn project and `pawnlint.toml` discovery

The editor must send `file://` document URIs.

## Contributing

Editor bug reports and focused protocol fixes are welcome. See
[CONTRIBUTING.md](CONTRIBUTING.md).
