# Editor performance

Run the large-file edit benchmarks with:

```sh
go test -run '^$' \
  -bench 'Benchmark(Full|Incremental)DidChangeToDiagnostics50K$' \
  -benchmem -benchtime=1x -count=3 ./lsp
```

The fixture is generated in memory. It contains about 50,000 lines and 940 KB
of functions, local variables, references, and control flow. Each iteration
changes one character and waits for diagnostics.

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

The first CPU profile found repeated linear scans by declaration and reference
span. Span indexes removed that bottleneck. The remaining edit still rebuilds
the parser, preprocessor, semantic model, control-flow graphs, and lint model,
which explains why incremental transport alone has little effect.

Workspace indexing uses a separate path. Pawn-analysis v0.1.20 reuses its
prepared syntax and symbols when it completes workspace semantics instead of
parsing every closed file twice.

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
