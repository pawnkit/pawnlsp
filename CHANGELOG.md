# Changelog

Notable changes are recorded here.

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
