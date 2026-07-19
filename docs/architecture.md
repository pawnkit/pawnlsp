# Architecture

The protocol layer stores open document snapshots and delegates language work
to PawnKit libraries.

```text
LSP request
  -> pawn-project discovery
  -> pawn-analysis
  -> pawn-api name context
  -> pawnlint diagnostics and fixes
  -> UTF-16 protocol conversion
```

Documents use immutable, versioned snapshots. New edits cancel older analysis,
and stale results are discarded. Navigation waits for the current snapshot.
Background workspace indexing is not implemented yet.

Document formatting delegates to pawnfmt's public library API.
