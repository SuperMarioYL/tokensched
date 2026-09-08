[简体中文](./README.md) · [Website](https://tokensched.lei6393.com) · [GitHub](https://github.com/SuperMarioYL/tokensched)

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/hero-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/hero-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/hero-dark.svg">
  <img src="./assets/presentation/hero-light.svg" width="960" alt="Hero diagram">
</picture>

# tokensched

**Allocate the token budget before the task tree runs.**

TokenSched turns a declared task tree into a budget allocation and compares it with a naive highest-tier simulation.

## Why use it

When estimated task demand exceeds a fixed budget, some work needs a lower tier or must wait. An explicit allocation shows those choices before a harness spends tokens.

- **Budget before execution** — Inspect expected demand before any model call.
- **Explain tier choices** — Each task receives an explicit allocation action.
- **Export decisions** — JSON can feed an external orchestrator.

## Architecture

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/architecture-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/architecture-dark.svg">
  <img src="./assets/presentation/architecture-light.svg" width="960" alt="Architecture diagram">
</picture>

The YAML loader produces task leaves with value, estimated tokens and allowed tiers. The greedy allocator ranks value per token and places tasks under the remaining budget, down-tiering or preempting according to policy. The simulator compares the resulting decisions with a declared-order naive run.

| Component | Responsibility |
| --- | --- |
| `Task tree` | internal/tasktree |
| `Policy / tiers` | internal/policy; internal/tier |
| `Greedy allocation` | internal/budget |
| `Simulation report` | internal/sim; internal/report |

## Install and quickstart

Build with the version declared in the repository manifest. Run the example from the repository root.

```bash
git clone https://github.com/SuperMarioYL/tokensched.git
cd tokensched
go build ./cmd/tokensched
```

Simulate the shipped six-task tree with a 200000-token budget and print the actual allocation report.

```bash
python3 examples/presentation-demo.py
```

## Recorded demo

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/process-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/process-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/process-dark.svg">
  <img src="./assets/presentation/process-light.svg" width="960" alt="Process diagram">
</picture>

The output reports simulated allocation decisions and estimated token totals under the declared budget.

```text
{
  "budget": 200000,
  "overrun_tokens": 87000,
  "naive": {
    "strategy": "hard-truncation",
    "spent_tokens": 180000,
    "value": 240,
    "completed": 3,
    "truncated": 3,
    "decisions": [
      {
        "task_id": "design-oauth-flow",
        "action": "keep",
        "tier": "opus",
        "budget_tokens": 70000,
        "value": 95,
        "reason": "ran on opus in declared order (no scheduling)"
      },
      {
        "task_id": "implement-token-exchange",
        "action": "keep",
        "tier": "opus",
        "budget_tokens": 60000,
        "value": 90,
        "reason": "ran on opus in declared order (no scheduling)"
      },
      {
        "task_id": "write-integration-tests",
        "action": "keep",
        "tier": "opus",
        "budget_tokens": 50000,
        "value": 55,
        "reason": "ran on opus in declared order (no scheduling)"
      },
      {
        "task_id": "refactor-config-loader",
        "action": "preempt",
        "tier": "",
        "budget_tokens": 0,
        "value": 0,
        "reason": "hard-truncated: budget exhausted before this task"
      },
      {
        "task_id": "update-changelog",
        "action": "preempt",
        "tier": "",
        "budget_tokens": 0,
        "value": 0,
        "reason": "hard-truncated: budget exhausted before this task"
      },
      {
        "task_id": "tidy-import-ordering",
        "action": "preempt",
        "tier": "",
        "budget_tokens": 0,
        "value": 0,
        "reason": "hard-truncated: budget exhausted before this task"
      }
    ]
  },
  "scheduled": {
    "strategy": "scheduled",
    "spent_tokens": 197500,
    "value": 258.45000000000005,
    "completed": 6,
    "truncated": 0,
    "decisions": [
      {
        "task_id": "design-oauth-flow",
        "action": "keep",
        "tier": "opus",
        "budget_tokens": 70000,
        "value": 95,
        "reason": "fits on opus at 70000 tok (v/tok=0.00136); kept"
      },
      {
        "task_id": "implement-token-exchange",
        "action": "keep",
        "tier": "opus",
        "budget_tokens": 60000,
        "value": 90,
        "reason": "fits on opus at 60000 tok (v/tok=0.00150); kept"
      },
      {
        "task_id": "write-integration-tests",
        "action": "keep",
        "tier": "opus",
        "budget_tokens": 50000,
        "value": 55,
        "reason": "fits on opus at 50000 tok (v/tok=0.00110); kept"
      },
      {
        "task_id": "refactor-config-loader",
        "action": "down-tier",
        "tier": "haiku",
        "budget_tokens": 7000,
        "value": 13.5,
        "reason": "won't fit on sonnet (22000>20000 rem); down-tiered to haiku at 7000 tok"
      },
      {
        "task_id": "update-changelog",
        "action": "down-tier",
        "tier": "haiku",
        "budget_tokens": 5000,
        "value": 3.6,
        "reason": "won't fit on opus (40000>13000 rem); down-tiered to haiku at 5000 tok"
      },
      {
        "task_id": "tidy-import-ordering",
        "action": "down-tier",
        "tier": "haiku",
        "budget_tokens": 5500,
        "value": 1.35,
        "reason": "won't fit on opus (45000>8000 rem); down-tiered to haiku at 5500 tok"
      }
    ]
  },
  "tasks_saved": 3
}
```

The complete command and output are recorded in [docs/demo-results.json](./docs/demo-results.json). Inputs and reproduction code are included in the repository.

![Existing terminal recording](./assets/demo.gif)

The existing recording is retained for context; the text example above documents the reproducible scenario.

## Usage

The CLI exposes the following operations. Commands after the example use your own paths or identifiers.

```bash
go run ./cmd/tokensched plan examples/overrun-tasktree.yaml --budget 200k
go run ./cmd/tokensched run examples/overrun-tasktree.yaml --budget 200k --json
go run ./cmd/tokensched run examples/overrun-tasktree.yaml --budget 200k --policy policy.example.yaml
```

## Configuration

run requires a positive --budget. --policy reads preemption.prefer_downtier and preemption.preempt_below_value. Tier cost and capability coefficients in the implementation scale estimates; validate those assumptions for your workload before using the plan.

## Integrations and responsibilities

<picture>
  <source media="(max-width: 600px) and (prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-mobile-dark.svg">
  <source media="(max-width: 600px)" srcset="./assets/presentation/integrations-mobile-light.svg">
  <source media="(prefers-color-scheme: dark)" srcset="./assets/presentation/integrations-dark.svg">
  <img src="./assets/presentation/integrations-light.svg" width="960" alt="Integrations diagram">
</picture>

The following routes are implemented in the source. Choose the input that matches your task and keep the resulting artifact with your project.

| Route | Implemented role |
| --- | --- |
| YAML task tree | Value, estimated tokens and tiers |
| Budget flag | Integer or k/m suffix |
| Policy YAML | Down-tier and value-floor choices |
| JSON report | Per-task allocation decisions |

## Limits and next steps

- run is a deterministic simulation, not a model execution loop. Counts and value scores derive from declared estimates and coefficients.
- The CLI does not control an account’s usage windows or prevent an external harness from spending more than planned.
- Tier value multipliers are assumptions, not measured model quality.

Live harness enforcement and estimate calibration are integration work beyond the current simulation CLI.

## License and contributions

See [LICENSE](./LICENSE). When reporting an issue, include a minimal input, the command, and the observed output.
