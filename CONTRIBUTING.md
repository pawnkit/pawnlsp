# Contributing

PawnKit is maintained by volunteers, so reviews may take a little time.

Editor bugs are most useful with a short Pawn file, the request being sent, and
the response or diagnostic you expected.

Run these checks before opening a pull request:

```sh
task check
```

Keep language and project behavior in the shared libraries. This repository
owns LSP state, request handling, cancellation, and conversion to protocol
types.
