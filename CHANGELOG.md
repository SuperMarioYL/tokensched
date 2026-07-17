# Changelog

All notable changes to this project are documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [0.4.0] - 2026-07-17

### Added

- **`prefer_downtier: false` now actually preempts instead of down-tiering.**
  v0.3.0 made the `preempt_below_value` policy knob reachable from the CLI but
  left the policy file's second documented knob, `prefer_downtier`, as a
  reported-only no-op (`run --json` echoed it but the scheduler always down-tiered
  before preempting). As of v0.4.0 a `prefer_downtier: false` policy switches the
  allocator to preempt-before-down-tier: a sub-task whose top tier does not fit
  the remaining budget is dropped outright instead of being salvaged on a cheaper
  model, so a tight budget drops low-priority work rather than degrading it.
  `run --json`'s `policy.active` now honestly reflects the change (true for
  `prefer_downtier: false` even with no `preempt_below_value`).

### Fixed

- **`run --json` and the terminal report now agree on overrun vs headroom.**
  The JSON document emitted a raw (possibly negative) `overrun_tokens` when the
  tree fit the budget, while the terminal report clamped it to 0 and never
  mentioned headroom — so a harness parsing `run --json` saw
  `overrun_tokens: -50000` with no way to distinguish overrun from headroom.
  `overrun_tokens` is now clamped to `>= 0` on both surfaces and a new
  `headroom_tokens` field reports the budget surplus; the terminal report says
  "naive plan fits — headroom N tokens" instead of silently clamping.
- **Contradictory `est_tokens` for a non-eligible tier now fail fast.** A task
  declaring `tiers: [opus]` but quoting `est_tokens: {haiku: 5000}` previously
  stored the haiku estimate and then silently ignored it (Haiku is never selected
  because it is not in the allowed tiers), hiding a misconfiguration the user
  very likely intended as "Haiku is eligible". `tasktree` now rejects it with a
  clear error naming the offending tier.

## [0.3.0] - 2026-06-27

### Added

- **`run --policy <file>` — the pluggable preemption policy is now reachable.**
  A new `internal/policy` package parses the `preemption` block of a policy file
  (`preempt_below_value`, `prefer_downtier`) into a preemption hook and forwards
  it to the scheduler. Before this release the knobs were documented in
  `policy.example.yaml` and existed in code (`schedule.PreemptBelow`, the
  `PreemptionHook` type), but the CLI always passed a `nil` hook, so the
  project's pluggable-policy primitive was unreachable from the binary.
  `run --budget 100k --policy policy.example.yaml` now applies the file's
  preemption policy; a positive `preempt_below_value` preempts every sub-task
  whose declared value is below the threshold, and a `0`/absent threshold is a
  no-op identical to a run with no `--policy`.
- **Effective policy in `run --json`.** The `run --json` document now carries a
  `policy` object (`source`, `preempt_below_value`, `prefer_downtier`, `active`)
  so a calling harness can confirm exactly which preemption policy was applied.

### Fixed

- **Overrun relaxation no longer over-degrades a single branch.** When the
  scheduler had to relax tasks to fit a budget, it ranked victims by raw
  realised value — but relaxing (down-tiering) a task lowers its realised value,
  so the same just-relaxed task was re-selected and marched all the way to the
  floor before any sibling low-value task was touched. Victim selection now
  ranks by value *density* (realised value per allocated token, stable under
  down-tiering) and prefers a still-down-tierable task over an already-floored
  one, so relaxation spreads evenly across the lowest-value frontier.

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

[0.4.0]: https://github.com/SuperMarioYL/tokensched/releases/tag/v0.4.0
[0.3.0]: https://github.com/SuperMarioYL/tokensched/releases/tag/v0.3.0
[0.2.0]: https://github.com/SuperMarioYL/tokensched/releases/tag/v0.2.0
[0.1.0]: https://github.com/SuperMarioYL/tokensched/releases/tag/v0.1.0
