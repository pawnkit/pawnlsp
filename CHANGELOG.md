# Changelog

Notable changes are recorded here.

## Unreleased

### Performance

- Use pawn-analysis v0.30.23 and pawnlint v1.8.56 for incremental CFG checks.

## 0.34.41 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.22 and pawnlint v1.8.55 for workspace semantic reuse.

## 0.34.40 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.21 and pawnlint v1.8.54 for incremental name checks.

## 0.34.39 - 2026-08-03

### CI

- Use PawnKit Actions v1.8.73 for support checks.

## 0.34.38 - 2026-08-03

### CI

- Use PawnKit Actions v1.8.72 for support checks.

## 0.34.37 - 2026-08-03

### Correctness

- Keep open documents in document diagnostics instead of repeating them in workspace diagnostics.

## 0.34.36 - 2026-08-03

### Performance

- Use pawnlint v1.8.53's cached shared diagnostic mappings.

## 0.34.35 - 2026-08-03

### Performance

- Use pawnlint v1.8.52's ordered loop call index for loop-scoped rules.

## 0.34.34 - 2026-08-03

### Performance

- Use pawnlint v1.8.51's shared loop indexes for performance checks.

## 0.34.33 - 2026-08-03

### Performance

- Use pawnlint v1.8.50's cached enclosing-function lookups.

## 0.34.32 - 2026-08-03

### Performance

- Use pawnlint v1.8.49's cached flow evaluation for strict linting.

## 0.34.31 - 2026-08-03

### Performance

- Use pawnlint v1.8.48's faster expression and initialization checks.

## 0.34.30 - 2026-08-03

### Performance

- Use pawnlint v1.8.47's faster initialization checks.

## 0.34.29 - 2026-08-03

### Performance

- Use pawnlint v1.8.46's flow-evaluation shortcut for strict linting.

## 0.34.28 - 2026-08-03

### Correctness

- Use pawnlint's inactive-branch guard for constant-condition diagnostics.

## 0.34.27 - 2026-08-03

### Performance

- Use pawnlint's cached loop uncertainty index during strict editor linting.

## 0.34.26 - 2026-08-03

### Performance

- Reuse the cached pointer parse in editor linting.

## 0.34.25 - 2026-08-03

### Changed

- Use the current pawnfmt and pawnlint releases in the language server.

## 0.34.24 - 2026-08-03

### Performance

- Keep the workspace index for root edits confined to a function body.

## 0.34.23 - 2026-08-03

### Fixed

- Republish the release after a provenance attestation lookup failure.

## 0.34.22 - 2026-08-03

### Performance

- Use pawnlint v1.8.41 for target-aware cached control-flow models.

## 0.34.21 - 2026-08-03

### Performance

- Use pawnlint v1.8.39 for cached editor project models.

## 0.34.20 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.20 to avoid copying immutable workspace signatures.

## 0.34.19 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.19 for cheaper workspace indexing.

## 0.34.18 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.18, which reuses completion option hashes across a
  workspace snapshot.

## 0.34.17 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.17 to cache workspace resolver fingerprints during
  multi-file completion.

## 0.34.16 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.16 and pawn-parser v1.5.11 for cheaper local edits.

## 0.34.15 - 2026-08-03

### Performance

- Use pawnlint v1.8.37 for lower-overhead lint runs.

## 0.34.14 - 2026-08-03

### Performance

- Use pawn-analysis v0.30.15 for lower-allocation same-length edits.

## 0.34.13 - 2026-08-03

### Diagnostics

- Refresh pull diagnostics when analysis is ready, before the full lint pass.

## 0.34.12 - 2026-08-03

### Changed

- Use pawn-api v0.19.6, pawnfmt v1.4.9, and pawnlint v1.8.36.

## 0.34.11 - 2026-08-03

### Performance

- Reuse unchanged closed-file workspace results across index restarts.
- Invalidate completed results when exported workspace declarations change.

## 0.34.10 - 2026-08-03

### Changed

- Use the retained-token analysis path from pawn-analysis v0.30.13.
- Use pawnlint v1.8.35 and pawn-parser v1.5.10.

## 0.34.9 - 2026-08-02

### Changed

- Use the current analysis, API, formatter, and linter releases.

## 0.34.8 - 2026-08-02

### Performance

- Run the 100K-line incremental budget in CI with the 50K budget.

## 0.34.7 - 2026-08-02

### Performance

- Keep the workspace index for edits to files outside the active include graph.

## 0.34.6 - 2026-08-02

### Performance

- Use pawnlint 1.8.32 and reuse its shared function-effect index across files.

## 0.34.5 - 2026-08-02

### Performance

- Use pawnlint 1.8.31 and its indexed shared function effects.

## 0.34.4 - 2026-08-02

### Compatibility

- Use pawnlint 1.8.30 with the latest shared analysis cache.

## 0.34.3 - 2026-08-02

### Performance

- Use pawn-analysis 0.30.11 and reuse compatible function-effect facts.

## 0.34.2 - 2026-08-02

### Compatibility

- Use pawn-analysis 0.30.10 and pawnlint 1.8.29.

## 0.34.1 - 2026-08-02

### Compatibility

- Use pawn-api 0.19.3 and pawnlint 1.8.28.

## 0.34.0 - 2026-08-02

### Compatibility

- Use pawnfmt 1.4.7 and pawn-project 0.34.2.

## 0.33.99 - 2026-08-02

### Compatibility

- Use pawnfmt 1.4.6 and pawnlint 1.8.27.

## 0.33.98 - 2026-08-02

### Compatibility

- Use pawn-analysis 0.30.9 and pawn-parser 1.5.8.

## 0.33.97 - 2026-08-02

### Diagnostics

- Keep unaffected analysis findings visible while an edited document is rechecked.

## 0.33.96 - 2026-08-02

### Diagnostics

- Keep unaffected lint findings visible while an edited document is rechecked.

## 0.33.95 - 2026-08-02

### Documentation

- Record that workspace latency budgets run in Linux CI.

## 0.33.94 - 2026-08-02

### Performance

- Share immutable open-document snapshots with the workspace index.

## 0.33.93 - 2026-08-02

### Performance

- Use pawnlint 1.8.26 for faster recursive-call checks.

## 0.33.92 - 2026-08-02

### Performance

- Use pawnlint 1.8.25 for faster statement-macro checks.

## 0.33.91 - 2026-08-01

### Compatibility

- Use pawn-analysis 0.30.8, pawnlint 1.8.24, and pawn-parser 1.5.7.

## 0.33.90 - 2026-08-01

### Compatibility

- Use pawn-analysis 0.30.7 and pawnlint 1.8.23 for PawnPlus tag aliases.

## 0.33.89 - 2026-08-01

### Workspace features

- Ignore workspace results from an index replaced during a refresh.

## 0.33.88 - 2026-08-01

### Formatting

- Drop formatting edits prepared for a document version that has already
  changed.

## 0.33.87 - 2026-08-01

### Diagnostics

- Avoid showing the same finding twice when analysis and lint report it.

## 0.33.86 - 2026-08-01

### Performance

- Use pawnlint 1.8.22 for lower-allocation define checks.

## 0.33.85 - 2026-08-01

### Performance

- Use the indexed callable lookup in pawn-analysis and pawnlint.

## 0.33.84 - 2026-08-01

### Performance

- Use pawnlint 1.8.20 to avoid preparing the open document twice.

## 0.33.83 - 2026-08-01

### Performance

- Use pawnlint 1.8.19 to reuse the open document's parsed syntax tree.

## 0.33.82 - 2026-08-01

### Performance

- Use pawnlint 1.8.18, which skips constant evaluation for non-constant
  operands.

## 0.33.81 - 2026-08-01

### Performance

- Pass known edits through workspace indexing so entry-file diagnostics use the
  same incremental path as open-document diagnostics.

## 0.33.80 - 2026-08-01

### Performance

- Pass editor byte edits to pawn-analysis and avoid a redundant full-file scan.

## 0.33.79 - 2026-08-01

### Performance

- Use pawn-analysis 0.30.4 and pawnlint 1.8.16 for faster local edits.

## 0.33.78 - 2026-08-01

### Performance

- Use pawnlint 1.8.15, which reuses its built-in rule registry for editor
  diagnostics.

## 0.33.77 - 2026-08-01

### Performance

- Use pawnlint 1.8.14, which reuses rule IDs and token indexes per lint pass.

## 0.33.76 - 2026-08-01

### Performance

- Use pawnlint 1.8.13, which reuses token indexes across a lint pass.

## 0.33.75 - 2026-08-01

### Performance

- Start document analysis without waiting for the workspace diagnostics graph.

## 0.33.74 - 2026-08-01

### Performance

- Use pawnlint 1.8.12, which skips control-flow evaluation for proven
  non-zero integer division operands.

## 0.33.73 - 2026-08-01

### Performance

- Reuse trivia-only analysis through pawn-analysis 0.30.3.

### Changed

- Use pawnfmt 1.4.4 and pawnlint 1.8.11.

## 0.33.72 - 2026-08-01

### Fixed

- Classify inactive preprocessor branches separately from source comments.

## 0.33.71 - 2026-07-30

### Fixed

- Limit shared function-fact collection to project entry graphs.

## 0.33.70 - 2026-07-30

### Performance

- Share function effects between analysis and editor linting.
- Reduced SAFW warm-lint time and allocation by avoiding duplicate effect analysis.

## 0.33.69 - 2026-07-30

### Performance

- Reuse parsing and symbol resolution for edits that wrap an expression in parentheses.
- Added a 50,000-line benchmark for token-moving edits.

## 0.33.68 - 2026-07-30

### Performance

- Reduced strict editor linting work for functions with parameters.

## 0.33.67 - 2026-07-29

### Performance

- Updated pawn-analysis to reuse local semantic checks.
- Added 100,000-line analysis benchmarks.
- Reduced the idle delay before local analysis.

## 0.33.66 - 2026-07-29

### Performance

- Publish local analysis sooner after an edit.
- Track P50 and P95 latency for 50,000-line editor changes.

## 0.33.65 - 2026-07-29

### Fixed

- Resolve relative pawnlint config paths from the workspace.

### Performance

- Share compact syntax indexes between include contexts.
- Tightened the incremental diagnostics regression budget.

## 0.33.64 - 2026-07-29

### Performance

- Updated pawnlint to reuse strict loop mutation checks.

## 0.33.63 - 2026-07-29

### Performance

- Updated pawnlint to reduce strict-profile call graph and semantic evaluation work.

## 0.33.62 - 2026-07-29

### Performance

- Updated pawnlint to reduce strict-profile token scans and allocations.

## 0.33.61 - 2026-07-29

### Fixed

- Accept open.mp API tag aliases such as `VEHICLE_TIRE_STATUS`.

## 0.33.60 - 2026-07-29

### Fixed

- Updated pawnlint to reduce strict-mode false positives and startup work.

## 0.33.59 - 2026-07-29

### Performance

- Avoid copying shared tokens for rebased trivia edits.

## 0.33.58 - 2026-07-29

### Performance

- Reduce memory used by incremental trivia diagnostics.

## 0.33.57 - 2026-07-29

### Performance

- Reuse rebased parser syntax during incremental linting.

## 0.33.56 - 2026-07-29

### Performance

- Reuse compatible preprocessing and rebase syntax after whitespace edits.

## 0.33.55 - 2026-07-29

### Performance

- Reuse unchanged local symbols during compatible editor edits.

## 0.33.54 - 2026-07-29

### Performance

- Reuse unchanged syntax during compatible editor edits.

## 0.33.53 - 2026-07-29

### Added

- Check incremental diagnostic latency, allocated bytes, and allocations in CI.

## 0.33.52 - 2026-07-29

### Performance

- Updated pawnlint to reuse cached function parameter facts.

## 0.33.51 - 2026-07-29

### Performance

- Updated pawn-analysis to retain macro invocation ranges during preprocessing.
- Reduced SAFW incremental analysis from about 200-227 ms to 80-126 ms on the
  reference machine.

## 0.33.50 - 2026-07-29

### Added

- Show cancellable progress when diagnostics take longer than 500 ms.
- Handle LSP request cancellation for document and workspace diagnostics.

## 0.33.49 - 2026-07-29

### Performance

- Do not queue formatting behind workspace or document diagnostics.

## 0.33.48 - 2026-07-29

### Fixed

- Do not duplicate a leading file header when formatting on save.

## 0.33.47 - 2026-07-29

### Fixed

- Keep grouped `#define` alignment when formatting on save.

## 0.33.46 - 2026-07-29

### Added

- Label variadic arguments as `arg1`, `arg2`, and so on in inlay hints.

## 0.33.45 - 2026-07-29

### Fixed

- Recover hover and definition targets for declarations in large dependency graphs.

## 0.33.44 - 2026-07-29

### Added

- Show colour previews and a picker for `{RRGGBB}` colours inside strings.

### Changed

- Updated pawnfmt to align consecutive `#define` values by default.

## 0.33.43 - 2026-07-29

### Fixed

- Format source-control ranges that span several declarations.

## 0.33.42 - 2026-07-29

### Fixed

- Do not wait for semantic analysis before range or on-type formatting.

## 0.33.41 - 2026-07-29

### Performance

- Avoid full-output parsing when formatting editor ranges.

## 0.33.40 - 2026-07-29

### Performance

- Use pawnfmt's syntax-unit range formatter for editor ranges.

## 0.33.39 - 2026-07-28

### Fixed

- Map references from expanded includes back to their source files.

## 0.33.38 - 2026-07-28

### Added

- Show editor colour previews for hexadecimal Pawn colours.
- Use forwarded function signatures for macro calls such as
  `PlayerDialog_Show`.

### Fixed

- Show parameter hints when argument and parameter names match.
- Limit quick fixes to the selected diagnostic.
- Remove duplicate unreachable-code diagnostics.

## 0.33.37 - 2026-07-28

### Fixed

- Do not report guarded YSI include chains as cycles.
- Do not apply native argument counts to macro-replaced calls.

## 0.33.36 - 2026-07-28

### Fixed

- Keep nested dependency files in the active project context.
- Remove workspace indexes after their last document closes.

## 0.33.35 - 2026-07-28

### Performance

- Updated pawnlint to share callable-variant lookups across project files.
- Reduced median warm SAFW editor linting from about 173 ms to 166 ms,
  allocations from about 79 MB to 72 MB, and allocation count from about
  320,000 to 308,000 on the reference machine.

## 0.33.34 - 2026-07-28

### Performance

- Updated pawnlint to use typed reference sorting.
- Reduced median warm SAFW editor linting from about 177 ms to 173 ms and
  allocation count from about 323,000 to 320,000 on the reference machine.

## 0.33.33 - 2026-07-28

### Performance

- Updated pawnlint to reduce reference sorting time.
- Reduced median warm SAFW editor linting from about 187 ms to 177 ms on the
  reference machine.

## 0.33.32 - 2026-07-28

### Performance

- Reuse bounded include resolutions between warm lint runs.
- Invalidate those resolutions with watched files and project settings.
- Reduced warm SAFW editor linting from about 191 ms to about 173 ms,
  allocations from about 82 MB to 79 MB, and allocation count from about
  352,000 to 323,000 on the reference machine.

## 0.33.31 - 2026-07-28

### Performance

- Cache call ordering keys while building lint call graphs.
- Resolve enclosing functions through per-file ranges.
- Reduced warm SAFW editor linting from about 205 ms to about 191 ms on the
  reference machine.

## 0.33.30 - 2026-07-28

### Performance

- Reuse unresolved symbol lookups during lint reference indexing.
- Skip sorting reference lists that are already ordered.
- Reduced warm SAFW editor linting from about 218 ms to about 205 ms and
  allocation count from about 399,000 to 352,000 on the reference machine.

## 0.33.29 - 2026-07-28

### Performance

- Reuse bounded filesystem probes during warm include resolution.
- Invalidate those probes when watched files or project settings change.
- Reduced warm SAFW editor linting from about 230 ms to about 205 ms and
  allocations from about 87 MB to 81 MB on the reference machine.

## 0.33.28 - 2026-07-28

### Performance

- Reuse adjacent define environments during lint project construction.
- Reduced warm SAFW editor linting from about 250 ms to about 230 ms on the
  reference machine.

## 0.33.27 - 2026-07-28

### Performance

- Bound cached lint analysis generations during long editor sessions.

## 0.33.26 - 2026-07-28

### Performance

- Updated pawnlint to reuse immutable define contexts between diagnostics.
- Reduced warm SAFW editor linting from about 271 ms to about 255 ms and
  allocations from about 92 MB to 87 MB on the reference machine.

## 0.33.25 - 2026-07-28

### Performance

- Updated pawnlint to reduce reference-index allocation and map growth.
- Reduced warm SAFW editor linting from about 289 ms to about 271 ms and
  allocations from about 96 MB to 92 MB on the reference machine.

## 0.33.24 - 2026-07-28

### Performance

- Updated pawn-project to avoid rebuilding canonical paths.
- Updated pawnlint to reduce function and reference indexing allocation.
- Reduced warm SAFW editor linting from about 306 ms to about 289 ms and
  allocations from about 125 MB to 96 MB on the reference machine.

## 0.33.23 - 2026-07-28

### Performance

- Updated pawnlint to use fixed-size analysis cache keys.
- Reduced warm SAFW editor linting from about 337 ms to about 306 ms and
  allocations from about 176 MB to 125 MB on the reference machine.

## 0.33.22 - 2026-07-28

### Fixed

- Pass shared include buffers to pawnlint so unsaved include changes are used.

### Performance

- Updated pawnlint to cache include metadata during each project build.
- Reduced warm SAFW editor linting from about 353 ms to about 337 ms on the
  reference machine.

## 0.33.21 - 2026-07-28

### Fixed

- Updated pawnlint so shared diagnostics use consistent Windows paths.

## 0.33.20 - 2026-07-28

### Performance

- Updated pawnlint to sort combined call-graph edges once.
- Reduced warm SAFW editor linting from about 382 ms to about 365 ms on the
  reference machine.

## 0.33.19 - 2026-07-28

### Performance

- Updated pawnlint to build call graphs from existing reference results.
- Added a repeatable warm-lint benchmark for real projects.
- Reduced warm SAFW editor linting from about 388 ms to about 382 ms on the
  reference machine.

## 0.33.18 - 2026-07-28

### Performance

- Updated pawnlint to reuse content hashes across its project caches.
- Reduced warm SAFW editor linting by about 11% on the reference machine.

## 0.33.17 - 2026-07-28

### Performance

- Index macro invocation spans once when filtering diagnostics.
- Reduced cold SAFW workspace diagnostics from about 4.0 seconds to 2.4
  seconds on the reference machine.

## 0.33.16 - 2026-07-28

### Performance

- Prepare pawnlint include parses in parallel from shared analysis tokens.
- Reduced cold SAFW workspace diagnostics by about 8% on the reference
  machine.

## 0.33.15 - 2026-07-28

### Performance

- Updated pawn-analysis for lower preprocessor allocation.
- Reduced the SAFW cold workspace diagnostic response from about 7.2 seconds
  to 3.9 seconds on the reference machine.

## 0.33.14 - 2026-07-28

### Performance

- Updated pawn-analysis to avoid copying expanded parser tokens.
- Reduced the SAFW cold workspace diagnostic response from about 10.7 seconds
  to 7.2 seconds on the reference machine.

## 0.33.13 - 2026-07-28

### Performance

- Reused pawn-analysis include diagnostics through pawnlint v1.6.0.

## 0.33.12 - 2026-07-28

### Performance

- Reused pawn-analysis results for semantic lint checks through pawnlint
  v1.5.0.

## 0.33.11 - 2026-07-28

### Performance

- Keep the last completed project graph while newer workspace refreshes are
  pending.

## 0.33.10 - 2026-07-28

### Performance

- Reused pawnlint's define-aware project state across editor passes.
- Reduced warm SAFW linting from about 605 ms to 436-483 ms on the reference
  machine.

## 0.33.9 - 2026-07-28

### Performance

- Cancel stale project linting when a newer document version arrives.
- Keep the SAFW incremental analysis check and editor lint timings in the
  real-project test.

## 0.33.8 - 2026-07-28

### Performance

- Reuse the dependency graph for safe local insertions and deletions.

## 0.33.7 - 2026-07-28

### Performance

- Reuse the previous workspace graph for safe function-body edits.
- Prefer current root symbols when a compatible dependency graph is in use.
- SAFW local analysis fell from about 8.5 seconds to 0.25-0.40 seconds on the
  reference machine.

## 0.33.6 - 2026-07-28

### Fixed

- Exclude dependency directories at any project depth.
- Move unrelated program indexing out of the diagnostic path.
- Build workspace and open-document analysis concurrently.

## 0.33.5 - 2026-07-28

### Fixed

- Use standard comment highlighting for inactive code.
- Do not diagnose include files without an entry graph.

## 0.33.4 - 2026-07-28

### Fixed

- Use the entry include graph when highlighting inactive code.

## 0.33.3 - 2026-07-28

### Fixed

- Use the entry include graph for open and closed file diagnostics.
- Apply unsaved include changes to the workspace graph.
- Avoid analysing every included file as a standalone program.

## 0.33.2 - 2026-07-28

### Fixed

- Matched open documents to normalized project paths on Windows.

## 0.33.1 - 2026-07-28

### Fixed

- Keep workspace diagnostics consistent when files are opened.
- Respect include order and guards in closed-file diagnostics.
- Mark inactive code with a dedicated semantic token.

## 0.33.0 - 2026-07-28

### Added

- Dim source excluded by conditional compilation.

## 0.32.13 - 2026-07-26

### Fixed

- Return an empty array for clean workspace diagnostic reports.

## 0.32.12 - 2026-07-26

### Performance

- Reuse preprocessing after fixed-position comment edits.

## 0.32.11 - 2026-07-26

### Performance

- Reuse expanded analysis after equivalent comment edits.

## 0.32.10 - 2026-07-26

### Performance

- Reduced clean SAFW analysis from about 27 seconds to 9 seconds.

## 0.32.9 - 2026-07-26

### Fixed

- Allow bounded analysis of large macro-heavy projects such as SAFW.

## 0.32.8 - 2026-07-26

### Performance

- Stop tokenizing obsolete document revisions and included files.

## 0.32.7 - 2026-07-26

### Performance

- Stop state, constant, and control-flow analysis for obsolete revisions.

## 0.32.6 - 2026-07-26

### Performance

- Stop name resolution and tag checking for obsolete revisions.

## 0.32.5 - 2026-07-26

### Performance

- Stop symbol construction for obsolete document revisions.

## 0.32.4 - 2026-07-26

### Performance

- Skip remaining analysis stages when an obsolete revision is cancelled.

## 0.32.3 - 2026-07-26

### Performance

- Stop preprocessing obsolete document revisions after cancellation.

## 0.32.2 - 2026-07-26

### Performance

- Stop parsing obsolete document revisions after cancellation.

## 0.32.1 - 2026-07-26

### Fixed

- Avoid stale semantic diagnostics and CFG spans after malformed or
  signature-adjacent edits.

## 0.32.0 - 2026-07-26

### Performance

- Keep range edits in persistent buffers until analysis or an editor request
  needs contiguous text.

## 0.31.1 - 2026-07-26

### Performance

- Reuse the line index buffer after editor range changes.

## 0.31.0 - 2026-07-26

### Added

- Report declaration indexing and reuse in analysis traces and benchmarks.

### Changed

- Updated parser, analysis, and lint dependencies for declaration-level
  revision tracking.

## 0.30.1 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.6.2 for concurrent large-file parsing.
- Added a benchmark that reports each analysis stage separately.

## 0.30.0 - 2026-07-26

### Added

- Added opt-in analysis-stage tracing through `RunWithOptions` and
  `PAWNKIT_ANALYSIS_TRACE`.

## 0.29.4 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.5.2 and pawnlint v1.3.3.
- Reused tag checks for unchanged larger functions without caching small
  functions.

## 0.29.3 - 2026-07-26

### Changed

- Updated line indexes incrementally after range-based document changes.

## 0.29.2 - 2026-07-26

### Changed

- Updated to pawnlint v1.3.0 for faster full diagnostics.

## 0.29.1 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.4.3 and pawnlint v1.2.3.
- Kept analysis-ready benchmarks isolated from background lint work.

## 0.29.0 - 2026-07-26

### Changed

- Made analysis available to editor features before full lint completes.
- Refreshed pull diagnostics when full lint becomes ready.

## 0.28.4 - 2026-07-26

### Changed

- Updated to pawnlint v1.2.1 for compact resource-wrapper analysis.

## 0.28.3 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.4.1 to avoid full-document cache hashing.

## 0.28.2 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.4.0.
- Passed immutable editor buffers into analysis snapshots without another
  full-file copy.

## 0.28.1 - 2026-07-26

### Changed

- Reused the open document's line index when converting diagnostics.
- Added a large-file diagnostic conversion benchmark.

## 0.28.0 - 2026-07-26

### Added

- Returned unchanged diagnostic reports when the client already has the
  current document result.

## 0.27.5 - 2026-07-26

### Fixed

- Released diagnostic worker contexts after each run.
- Restored compatibility with the CI-pinned linter.

## 0.27.4 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.3.1 and pawnlint v1.2.0.
- Reduced tag-check allocations by reusing shared symbol indexes.

## 0.27.3 - 2026-07-26

### Changed

- Updated to pawn-analysis v0.3.0 for selective function CFG reuse.
- Primed the performance benchmark before measuring document changes.

## 0.27.2 - 2026-07-26

### Changed

- Kept at most one active and one pending diagnostic run per document.

## 0.27.1 - 2026-07-26

### Changed

- Reused each open document's line index across editor requests.
- Updated to pawn-analysis v0.1.20 to avoid parsing workspace files twice.

## 0.27.0 - 2026-07-26

### Changed

- Accepted incremental LSP text changes.
- Kept the workspace index across ordinary document edits.
- Added a 50,000-line edit-to-diagnostics benchmark and baseline.
- Updated to pawn-analysis v0.1.19.

## 0.26.11 - 2026-07-25

### Changed

- Added the repository support record with CI validation.

## 0.26.10 - 2026-07-25

### Changed

- Updated to pawnlint v1.1.10.

## 0.26.9 - 2026-07-25

### Changed

- Updated to pawnlint v1.1.9.

## 0.26.8 - 2026-07-25

### Fixed

- Stopped analyzing each document twice per edit; the lint pass now reuses
  the analysis already computed for hover, completion, and diagnostics
  instead of running it again for its own shared diagnostics.
- Updated to pawnlint v1.1.8.

## 0.26.7 - 2026-07-24

### Fixed

- Stopped rebuilding a full line index (and copying the source) for every
  diagnostic's position; the document's index is now built once and reused.
- Updated to pawn-analysis v0.1.17, which stops tokenizing the entry file
  twice per edit.

## 0.26.6 - 2026-07-24

### Changed

- Updated to pawnlint v1.1.7, which also reuses each include's CST walk and
  semantic model across edits instead of rebuilding them every time.

## 0.26.5 - 2026-07-24

### Fixed

- Reused a lint parse cache across edits of the same document instead of
  building a fresh one on every keystroke.

## 0.26.4 - 2026-07-24

### Fixed

- Debounced document analysis after `didChange` so rapid typing coalesces
  into one analysis instead of restarting on every keystroke.

## 0.26.3 - 2026-07-23

### Changed

- Updated the API dataset to include confidence and review status.

## 0.26.2 - 2026-07-23

### Added

- Added `pawnlsp --version` for installers and support reports.

## 0.26.1 - 2026-07-23

### Changed

- Added lifecycle coverage for editor-managed includes.

## 0.26.0 - 2026-07-23

### Added

- Added the versioned editor-managed tool protocol from RFC 0008.

### Changed

- Updated project loading and lint diagnostics to their current releases.

## 0.25.4 - 2026-07-23

### Changed

- Routed editor-managed include paths through `pawn-project`.

## 0.25.3 - 2026-07-23

### Fixed

- Updated analysis and linting so tag names are not shown as undefined.

## 0.25.2 - 2026-07-23

### Changed

- Updated to the current formatter and linter releases.

## 0.25.1 - 2026-07-23

### Fixed

- Updated the embedded API dataset to pawn-api v0.18.0.

## 0.25.0 - 2026-07-23

### Added

- Completed SA-MP and open.mp API data for player, vehicle, text-draw, network, database, and NPC features.

## 0.24.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for 3D text labels.

## 0.23.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for HTTP requests.

## 0.22.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for gang zones.

## 0.21.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for pickups.

## 0.20.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for player classes.

## 0.19.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for object queries, attachments, and custom models.

## 0.18.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for per-player objects.

## 0.17.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for object materials and editing.

## 0.16.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for core global objects.

## 0.15.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for menus.

## 0.14.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for dialogs.

## 0.13.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for checkpoints.

## 0.12.0 - 2026-07-23

### Added

- Added completion, hover, and signature data for the SA-MP/open.mp actor API.

## 0.11.2 - 2026-07-23

### Added

- Added related source locations to analysis and lint diagnostics.

## 0.11.1 - 2026-07-23

### Fixed

- Prefer workspace declarations over API entries with the same name.
- Bound broad completion results and let clients request narrower results.

## 0.11.0 - 2026-07-23

### Added

- Added project-aware completion for include paths and preprocessor directives.
- Added source comments to macro hover.

### Changed

- Resolve completion documentation and full declarations only when selected.

## 0.10.1 - 2026-07-23

### Added

- Added clickable documentation links to lint and analysis diagnostics.

## 0.10.0 - 2026-07-23

### Added

- Added code actions to explain or locally suppress pawnlint diagnostics.

## 0.9.10 - 2026-07-23

### Fixed

- Updated API, project, analysis, formatting, and linting dependencies.

## 0.9.9 - 2026-07-22

### Fixed

- Accepted compiler constants and current YSI iterator syntax in diagnostics.
- Stopped requiring return values from `void:` functions.
- Formatted include paths that use backslashes.

## 0.9.8 - 2026-07-21

### Fixed

- Bounded macro expansion for large projects.
- Kept `pawno` toolchain files out of indexing and workspace diagnostics.
- Avoided indexing open files twice.

## 0.9.7 - 2026-07-21

### Fixed

- Used the active include graph for workspace diagnostics.

## 0.9.6 - 2026-07-21

### Fixed

- Kept dependencies and inactive source trees out of workspace diagnostics.

## 0.9.5 - 2026-07-21

### Fixed

- Kept PawnPlus tag-macro diagnostics consistent when files are opened or closed.

## 0.9.4 - 2026-07-21

### Fixed

- Resolved quoted includes from the gamemode entry directory.
- Accepted macro parameter labels that do not start at `%0`.

## 0.9.3 - 2026-07-21

### Fixed

- Updated analysis and linting for concise returns, macro-defined tags, and nested quoted includes.

## 0.9.2 - 2026-07-21

### Fixed

- Accepted PawnPlus generic tags, declaration macros, and conditional `else if` splices.
- Respected active `#endinput` guards when reporting duplicate declarations.

## 0.9.1 - 2026-07-21

### Fixed

- Removed duplicate diagnostics in clients that support pull diagnostics.
- Limited enum-member hover to the selected member.

## 0.9.0 - 2026-07-21

### Added

- Added source hover for object-like and function-like macros.

## 0.8.2 - 2026-07-21

### Fixed

- Preserved variadic tag sets in local function hover and signature help.

## 0.8.1 - 2026-07-21

### Fixed

- Kept hover, references, highlights, rename, and call hierarchy lookups within the requested source file.
- Covered multiline test macros in pull-diagnostic regression tests.

## 0.8.0 - 2026-07-21

### Added

- Added incoming and outgoing call hierarchy for project functions.

### Changed

- Ranked local and project completions ahead of API entries.
- Removed local variables from completion outside their declaring function.

## 0.7.0 - 2026-07-21

### Added

- Added pull diagnostics for open documents and indexed workspace files.
- Added source comments to local hover and completion details.
- Added include, constraint, callback, and return details to API documentation.

## 0.6.0 - 2026-07-21

### Added

- Added range formatting and format-on-type through pawnfmt.
- Added parameter-name inlay hints for local and API calls.

## 0.5.0 - 2026-07-21

### Added

- Added document highlights for declarations and references.
- Added syntax-aware folding and selection ranges.

## 0.4.1 - 2026-07-21

### Fixed

- Released workspace indexing cancellation resources after each indexing run.

## 0.4.0 - 2026-07-21

### Added

- Added bounded background indexing for project Pawn sources.
- Added workspace symbol search and cross-file navigation.
- Added safe local and workspace symbol renaming.

## 0.3.0 - 2026-07-21

### Added

- Added completion for project symbols, macros, and profile-specific API entries.
- Added semantic highlighting for project and API symbols.
- Added signature help for project functions, macros, and API calls.

## 0.2.1 - 2026-07-21

### Fixed

- Opened resolved include files from definitions and showed the resolved path on hover.

## 0.2.0 - 2026-07-21

### Added

- Added exact local declarations to hover details.
- Added API signatures, return notes, deprecation details, and documentation links to hover details.

### Changed

- Reloaded managed include paths through LSP configuration updates.

## 0.1.4 - 2026-07-21

### Fixed

- Stopped test macros from appearing as duplicate function declarations.

## 0.1.3 - 2026-07-21

### Fixed

- Removed false missing-include diagnostics for project and editor-managed include paths.

## 0.1.2 - 2026-07-20

### Fixed

- Reloaded include paths when project files change.
- Accepted tool-managed include paths from editor clients.

## 0.1.1 - 2026-07-20

### Added

- Release archives for Linux, macOS, and Windows.

## 0.1.0 - 2026-07-19

### Added

- Diagnostics and quick fixes from pawnlint and pawn-analysis.
- Definitions, references, hover, and document symbols.
- Whole-document formatting through pawnfmt.
- Project includes and target-aware API metadata.
- Versioned document analysis with stale-result cancellation.
