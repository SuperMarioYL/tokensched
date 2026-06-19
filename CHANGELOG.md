# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.2.0] - 2026-06-19

### Added

- **`--json` output for `plan` and `run`.** Both commands now accept `--json` to
  emit a stable, machine-readable document instead of the lipgloss terminal
  report — `plan --json` carries every leaf (value, tiers, top tier, initial
  tokens) plus the naive demand and overrun; `run --json` carries both
  strategies' per-task decisions and the headline value/tasks saved. This makes
  the scheduler directly consumable from an agent harness (no ANSI, no table
  parsing); a preempted task serialises with an empty `tier`.

### Fixed

- **Zero-cost tasks are no longer mis-ranked.** A sub-task whose top-tier
  estimate is `0` is free realised value (unbounded value-per-token), but the
  greedy allocator was assigning it a value-per-token of `0` — ranking it *last*
  instead of *first*. Such tasks now sort ahead of every finite-cost task, so
  free value is admitted first and is never the first thing cut.

## [0.1.1] - 2026-06-19

### Added

- **Release artifacts.** Tagged builds now ship cross-platform binaries
  (linux/macOS/windows × amd64/arm64) + `checksums.txt` via GoReleaser, attached
  to the GitHub Release — `go install` is no longer the only way to get it.
- **Animated demo.** `docs/demo.tape` is rendered to `assets/demo.gif` in CI (vhs).

### Changed

- **README.** Added a dark/light architecture diagram (`<picture>` + bespoke SVG)
  and aligned the layout to the house visual style.

## [0.1.0] - 2026-06-18

### Added

- **m1 — task-tree model & `plan`.** Parse a YAML task tree (sub-tasks with a
  declared `value`, per-tier `est_tokens`, and eligible `tiers`) into an
  in-memory `*tasktree.Task` tree, with validation (value ≥ 0, non-empty tiers,
  unique ids). `tokensched plan <tree.yaml>` prints the tree with each leaf's
  initial top-tier budget and the naive all-top-tier demand vs the budget.
- **m2 — allocate, preempt & `run`.** A greedy `budget.Allocator` distributes a
  fixed token budget by descending value-per-token and predicts overrun; the
  `schedule` package resolves overrun by down-tiering the lowest-marginal-value
  task (Opus → Sonnet → Haiku) and preempting only when a task is not
  down-tierable. `tokensched run <tree.yaml> --budget <N>` replays naive
  hard-truncation against the scheduled run and prints the
  "hard-truncation vs scheduled-yield" comparison.
- **m3 — report & importable allocator API.** A lipgloss terminal report renders
  the per-task decisions (keep / down-tier / preempt + reason + allocated tokens)
  and the before/after comparison table. The `budget` package exposes a stable,
  importable API — the `Allocator` interface, the `PreemptionHook`, and the
  `tier` cost/capability coefficients — so the allocator can be vendored into an
  agent harness.

[0.2.0]: https://github.com/SuperMarioYL/tokensched/releases/tag/v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/tokensched/releases/tag/v0.1.0
