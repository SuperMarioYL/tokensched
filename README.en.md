<div align="center">

English | [简体中文](./README.md)

<img src="https://readme-typing-svg.demolab.com?font=JetBrains+Mono&weight=700&size=30&duration=3200&pause=900&color=F2B705&center=true&vCenter=true&width=720&height=70&lines=TokenSched;A+CPU+scheduler+for+your+token+budget;hard+truncation+-%3E+schedulable+soft+yield" alt="TokenSched" />

<p>
  <strong>A CPU-style scheduler for Claude Code's token budget</strong><br/>
  Pre-allocates by expected value-per-token · predicts overrun · down-tiers or preempts before the window blows
</p>

<p>
  <a href="https://golang.org"><img src="https://img.shields.io/badge/Go-1.24-00ADD8?logo=go&logoColor=white" alt="Go 1.24" /></a>
  <a href="./LICENSE"><img src="https://img.shields.io/badge/license-MIT-blue.svg" alt="License MIT" /></a>
  <a href="https://github.com/SuperMarioYL/tokensched/actions/workflows/ci.yml"><img src="https://img.shields.io/github/actions/workflow/status/SuperMarioYL/tokensched/ci.yml?branch=main&label=build" alt="Build" /></a>
  <a href="https://github.com/SuperMarioYL/tokensched/stargazers"><img src="https://img.shields.io/github/stars/SuperMarioYL/tokensched?style=flat&logo=github" alt="GitHub stars" /></a>
</p>

</div>

---

## <img src="https://api.iconify.design/tabler/rocket.svg?color=%23f2b705" width="20" height="20" align="center" /> What is it

**TokenSched** is a **token-budget scheduler** for [Claude Code](https://docs.anthropic.com/en/docs/claude-code) power users.

Hand it a task tree that is about to hit the wall — the sub-tasks an Agent plans to run, each with a declared *value*, an estimated *token cost*, and the model *tiers* it may run on — and it schedules your token budget the way an operating system schedules CPU time slices:

- **Pre-allocate by value-per-token** — high-value sub-tasks get budget first and are never the first to be cut;
- **Predict overrun** — before anything runs, it computes how many tokens the naive "all-Opus" plan would blow past the window;
- **Down-tier and preempt automatically** — when over budget, it relaxes the lowest-marginal-value sub-task from Opus to Sonnet to Haiku (down-tier), and only preempts a task once it is already on its cheapest eligible tier and still won't fit.

In one line: **it turns hard truncation into a schedulable soft yield.** When your Agent hits Claude Code's 5-hour window today, the most important sub-task is often exactly the one that gets hard-truncated; TokenSched makes low-value work yield first, keeping high-value work on Opus and the whole tree inside budget.

> TokenSched is an **allocator**, not a compressor. It does not touch your payload; it does admission control at the task-tree layer: deciding *which sub-task* deserves tokens, and on *which tier*.

## <img src="https://api.iconify.design/tabler/topology-star-3.svg?color=%23f2b705" width="20" height="20" align="center" /> Architecture

<p align="center">
  <picture>
    <source media="(prefers-color-scheme: dark)" srcset="./assets/atlas-dark.svg">
    <source media="(prefers-color-scheme: light)" srcset="./assets/atlas-light.svg">
    <img src="./assets/atlas-light.svg" width="880" alt="A task tree enters the allocator, which sorts by value-per-token, predicts overspend, down-tiers and preempts, then assigns each subtask to a model tier — all inside one budget window">
  </picture>
</p>

A task tree (each subtask carrying a value, a token estimate, and allowed tiers) enters the **allocator**, which does admission control the way an OS schedules CPU: ① sort by value-per-token → ② predict how far a naive all-Opus plan overspends → ③ down-tier the lowest-marginal-value subtasks (Opus→Sonnet→Haiku) → ④ preempt only when something already on the cheapest tier still won't fit. Every subtask lands on a **model tier**, and the whole tree runs inside one 5-hour **budget window**.

## <img src="https://api.iconify.design/tabler/terminal-2.svg?color=%23f2b705" width="20" height="20" align="center" /> Quickstart

Install (a single binary, no network, no daemon):

```bash
go install github.com/SuperMarioYL/tokensched/cmd/tokensched@latest
```

**1. Inspect the task tree and each node's initial budget (`plan`):**

```bash
tokensched plan examples/overrun-tasktree.yaml --budget 200k
```

```text
Task tree — ship-auth-feature
budget: 200k tokens

ship-auth-feature
├─ design-oauth-flow  value=95  tiers=[opus,sonnet,haiku]  init=opus@70k tok
├─ implement-token-exchange  value=90  tiers=[opus,sonnet,haiku]  init=opus@60k tok
├─ write-integration-tests  value=55  tiers=[opus,sonnet,haiku]  init=opus@50k tok
├─ refactor-config-loader  value=30  tiers=[sonnet,haiku]  init=sonnet@22k tok
├─ update-changelog  value=8  tiers=[opus,sonnet,haiku]  init=opus@40k tok
└─ tidy-import-ordering  value=3  tiers=[opus,sonnet,haiku]  init=opus@45k tok

6 leaf tasks · naive all-top-tier demand = 287k tokens  (overruns budget by 87k)
```

**2. Replay "naive hard-truncation vs scheduled-yield" (`run`):**

```bash
tokensched run examples/overrun-tasktree.yaml --budget 200k
```

`--budget` accepts a plain integer or a `k`/`m` suffix (e.g. `200k`, `1.5m`, `200000`). Copy `examples/overrun-tasktree.yaml` and fill in your own sub-tasks' `value` / `est_tokens` / `tiers` following the comments.

**3. Machine-readable output for a harness (`--json`):**

Both `plan` and `run` accept `--json` to emit a stable, structured document (no ANSI, no table parsing) you can feed straight into your agent orchestrator:

```bash
tokensched run examples/overrun-tasktree.yaml --budget 200k --json
```

```json
{
  "budget": 200000,
  "overrun_tokens": 87000,
  "scheduled": {
    "spent_tokens": 197500,
    "value": 258.5,
    "decisions": [
      { "task_id": "design-oauth-flow", "action": "keep", "tier": "opus", "budget_tokens": 70000, "value": 95 }
    ]
  },
  "tasks_saved": 3,
  "value_saved": 18.5
}
```

A preempted sub-task serialises with an empty `tier`. `plan --json` instead emits each leaf's `value` / `tiers` / `top_tier` / `init_tokens`, plus the naive demand and overrun.

## <img src="https://api.iconify.design/tabler/photo.svg?color=%23f2b705" width="20" height="20" align="center" /> Demo

The same task tree, doomed to overrun a 200k budget, executed two ways: naive execution hard-truncates 3 sub-tasks (including the most important one) when the window runs out; TokenSched down-tiers the low-value nodes to Haiku, keeps every high-value node on Opus, and finishes all 6 sub-tasks without blowing the budget.

<p align="center">
  <img src="./assets/demo.gif" alt="tokensched plan + schedule terminal demo" width="820" />
</p>

<sub>↑ Terminal recording (rendered in CI from <a href="./docs/demo.tape">docs/demo.tape</a> via <a href="https://github.com/charmbracelet/vhs">vhs</a> on tag push). Static before/after comparison below:</sub>

<div align="center">
  <img src="./docs/assets/demo.svg" alt="tokensched run replays an over-budget task tree: low-value nodes down-tiered to Haiku, high-value nodes kept on Opus, window not blown." width="780" />
</div>

| Metric | Naive hard-truncation | TokenSched scheduled |
| --- | --- | --- |
| tokens spent | 180k | 197.5k |
| tasks completed | **3 / 6** | **6 / 6** |
| cut / preempted | 3 | 0 |
| realised value | 240.0 | **258.5** |

The three high-value nodes `design-oauth-flow`, `implement-token-exchange`, `write-integration-tests` stay on Opus; the three low-value nodes `refactor-config-loader`, `update-changelog`, `tidy-import-ordering` are down-tiered to Haiku, landing the whole tree at 197.5k / 200k budget.

## <img src="https://api.iconify.design/tabler/bulb.svg?color=%23f2b705" width="20" height="20" align="center" /> Why

**The pain**: [Claude Code](https://docs.anthropic.com/en/docs/claude-code) power users routinely run a long chain of sub-tasks inside the 5-hour usage window and get hard-truncated when they hit the wall — and the truncated work is often exactly the important task that happened to be queued last. Today people cope by manually switching some steps to Haiku to "burn the meter slower", relying on in-the-moment judgement, with no way to preempt dynamically mid-run.

**TokenSched systematizes that move into a primitive**: a *per-sub-task token admission-controller*. It has a clear, reusable interface (task-tree value estimation + a preemption hook) — a standalone scheduling primitive, not a flag buried inside someone's harness.

It is aimed at **Agent infrastructure**: the scheduler allocates a token budget across an Agent's sub-task tree — exactly the layer that agent infra is missing. You can `import` the core allocator as a Go package into your own Agent orchestration framework and put an explainable budget cap on any Agent:

```go
import (
    "github.com/SuperMarioYL/tokensched/internal/budget"
    "github.com/SuperMarioYL/tokensched/internal/schedule"
    "github.com/SuperMarioYL/tokensched/internal/tasktree"
)

// 1. Build (or parse from YAML) a task tree
root, _ := tasktree.LoadFile("tree.yaml")

// 2. Use the greedy allocator directly for decisions (Keep / DownTier / Preempt)
alloc := budget.NewGreedyAllocator(nil)
decisions := alloc.Allocate(root, 200_000)

// 3. Or use the scheduler with a custom preemption hook
sched := schedule.New(&schedule.Options{
    Hook: schedule.PreemptBelow(10), // preempt any sub-task with value < 10
})
plan := sched.Schedule(root, 200_000)
_ = decisions
_ = plan
```

The `budget.Allocator` interface, the `budget.PreemptionHook`, and the `tier` cost/capability coefficients are stable, reusable APIs (see `policy.example.yaml` for the tunable policy knobs).

> v0.1 makes no live API calls: allocation and the comparison are a deterministic replay/simulation over the declared estimates. Intercepting real traffic is a later version's job (see the roadmap).

## <img src="https://api.iconify.design/tabler/map-2.svg?color=%23f2b705" width="20" height="20" align="center" /> Roadmap

| Version | Status | Scope |
| --- | --- | --- |
| **v0.1.0** | ✅ Released | `plan` parses a task tree and allocates initial budgets; `run` replays naive hard-truncation vs scheduled-yield; greedy value-per-token allocation + down-tier/preempt; importable allocator / preemption-hook API; lipgloss terminal report. |
| **v0.2.0** | ✅ Released | `--json` machine-readable output for `plan` / `run`, so the scheduler drops straight into an agent harness; fixed a value-per-token ordering bug that ranked zero-cost (free-value) sub-tasks last instead of first. |
| v0.3 | Planned | Real token schema + live metering; a resident daemon that watches the real 5-hour window; a learned model to auto-estimate sub-task value; a multi-agent shared budget pool. |

**Explicitly out of scope for v0.1**: an inline gateway intercepting real Claude Code / Anthropic API traffic · learned auto-valuation · web UI / dashboard · multi-user shared budget pool · auth / accounts / cloud hosting / paid tiers · token compression / payload shrinking (that's a compressor's job — TokenSched is an allocator).

## <img src="https://api.iconify.design/tabler/license.svg?color=%23f2b705" width="20" height="20" align="center" /> License

Released under the **MIT** license — fully open source, no paid features. See [LICENSE](./LICENSE).

---

<div align="center">
  <a href="./LICENSE">MIT</a> © 2026 SuperMarioYL
</div>
