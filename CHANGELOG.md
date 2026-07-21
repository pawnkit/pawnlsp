# Changelog

Notable changes are recorded here.

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
