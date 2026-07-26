# Editor performance

Run the large-file edit benchmarks with:

```sh
go test -run '^$' \
  -bench 'Benchmark(Full|Incremental)DidChangeTo(Analysis|Diagnostics)50K$' \
  -benchmem -benchtime=1x -count=3 ./lsp
```

The fixture is generated in memory. It contains about 50,000 lines and 940 KB
of functions, local variables, references, and control flow. Setup runs the
initial diagnostics, then each measured iteration changes one character and
waits for the next result.

## July 26 baseline

Reference system:

- AMD Ryzen 7 5800X3D, 8 cores and 16 threads
- Linux amd64
- Go 1.26.5

| Version | Change | Time | Allocated | Allocations |
|---|---|---:|---:|---:|
| pawn-analysis v0.1.17 | full document | 1.92–1.95 s | 461 MB | 1.17 million |
| pawn-analysis v0.1.19 | full document | 658–672 ms | 466 MB | 1.17 million |
| pawn-analysis v0.1.19 | incremental range | 667–700 ms | 467 MB | 1.17 million |

One-pass scaling with pawn-analysis v0.4.0 and pawnlint v1.2.0:

| Lines | Time | Allocated | Allocations |
|---:|---:|---:|---:|
| 10,000 | 226–234 ms | 113 MB | 270,000 |
| 25,000 | 321–326 ms | 198 MB | 526,000 |
| 50,000 | 498–543 ms | 327 MB | 927,000 |
| 100,000 | 913–945 ms | 589 MB | 1.73 million |

These figures include the 150 ms typing debounce. They are a diagnostic
baseline, not the target: the 50,000-line case still needs to fall below
300 ms.

With pawn-analysis v0.4.1 and pawnlint v1.2.1, the 50,000-line fixture reaches
editor analysis in 352–414 ms and full lint diagnostics in 520–560 ms.
Hover, completion, navigation, and document symbols can use the earlier result.
The client refreshes diagnostics when full lint finishes.

Pawn-analysis v0.4.3 reduces preprocessor allocation growth. The isolated
analysis-ready benchmark allocates about 254 MB, while the full diagnostic path
allocates about 305 MB.

Owned analysis snapshots remove one full-file copy from each editor revision.
This is visible in allocations but does not remove the parser and lint work
that still dominates the benchmark.

Range-based edits update line starts from the changed span instead of scanning
the whole document again. Analysis still needs a contiguous source buffer.

Tag checks are reused for unchanged larger functions. Small functions keep the
one-pass path because caching them costs more memory than it saves.

The first CPU profile found repeated linear scans by declaration and reference
span. Span indexes removed that bottleneck. The remaining edit still rebuilds
the parser, preprocessor, semantic model, control-flow graphs, and lint model,
which explains why incremental transport alone has little effect.

Workspace indexing uses a separate path. Pawn-analysis v0.1.20 reuses its
prepared syntax and symbols when it completes workspace semantics instead of
parsing every closed file twice.

Rapid edits keep one active diagnostic run and replace a single pending run.
Cancelled or superseded versions cannot publish results.

An unchanged 50,000-line diagnostic pull takes about 0.6 µs and allocates
432 bytes. The server reuses the document line index and returns an unchanged
result ID after the first pull.

Create profiles with:

```sh
go test -run '^$' \
  -bench '^BenchmarkIncrementalDidChangeToDiagnostics50K$' \
  -benchtime=1x -cpuprofile cpu.pprof -memprofile heap.pprof ./lsp
go tool pprof -top cpu.pprof
go tool pprof -top -alloc_space heap.pprof
```

Do not commit profile files. Record the command, hardware, and useful findings
when a change moves a bottleneck.

For one run, stage timings can be written to the server log with:

```sh
PAWNKIT_ANALYSIS_TRACE=1 pawnlsp
```

The trace includes the document version, duration, cancellation state, and
reuse count. Leave it disabled during normal editing.
