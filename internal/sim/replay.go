// Package sim replays a task tree under two execution strategies and compares
// them. There are no live API calls; "execution" is a deterministic walk over
// the declared estimates.
//
//   - Naive hard-truncation: run every task on its highest tier in declared
//     order until the budget is exhausted, then hard-cut everything after.
//     This is what happens today when an agent blows its 5-hour window mid-run.
//   - TokenSched scheduled: the budget scheduler pre-allocates by
//     value-per-token, down-tiers low-value tasks, and preempts only when not
//     down-tierable, so high-value work survives within the same budget.
package sim

import (
	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/schedule"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

// Outcome is the result of one execution strategy.
type Outcome struct {
	Strategy  string            // "hard-truncation" | "scheduled"
	Decisions []budget.Decision // per-task disposition
	Spent     int               // tokens spent (never exceeds budget)
	Value     float64           // total realised value
	Completed int               // tasks that ran (Keep or DownTier)
	Truncated int               // tasks that were cut / preempted
}

// Comparison bundles both outcomes for reporting.
type Comparison struct {
	Budget  int
	Overrun int // naive all-top-tier deficit vs budget (positive => overrun)
	// Headroom is the naive budget headroom: budget minus the naive all-top-tier
	// demand, clamped to >= 0. It is the positive counterpart of Overrun — when
	// the tree fits, Overrun is 0 (clamped for display) and Headroom reports how
	// many tokens remain. Surfaced so run --json and the terminal report can
	// agree on overrun vs headroom (v0.4.0).
	Headroom   int
	NaiveCount int // number of leaf tasks
	Naive      Outcome
	Scheduled  Outcome
	ValueSaved float64 // Scheduled.Value - Naive.Value
	TasksSaved int     // Scheduled.Completed - Naive.Completed
}

// Replay runs both strategies over root against budget and returns the
// comparison. hook is forwarded to the scheduler (pass nil for none). It is a
// back-compat wrapper around ReplayWithOpts for callers that only need a
// preemption hook; use ReplayWithOpts to also pass a PreferDownTier policy
// (v0.4.0).
func Replay(root *tasktree.Task, budgetTokens int, hook budget.PreemptionHook) Comparison {
	return ReplayWithOpts(root, budgetTokens, &schedule.Options{Hook: hook})
}

// ReplayWithOpts runs both strategies over root against budget, forwarding the
// full scheduler options (hook + PreferDownTier policy) to the scheduler. It is
// the entry point that honours the prefer_downtier:false policy (v0.4.0): a nil
// opts is treated as the default (no hook, down-tier preferred).
func ReplayWithOpts(root *tasktree.Task, budgetTokens int, opts *schedule.Options) Comparison {
	leaves := tasktree.Leaves(root)

	naive := replayNaive(leaves, budgetTokens)

	sched := schedule.New(opts)
	plan := sched.Schedule(root, budgetTokens)
	scheduled := outcomeFromPlan(plan)

	// Overrun = naive demand - budget (may be negative when the tree fits).
	// Headroom is its positive counterpart, clamped to >= 0.
	headroom := 0
	if plan.Overrun < 0 {
		headroom = -plan.Overrun
	}

	return Comparison{
		Budget:     budgetTokens,
		Overrun:    plan.Overrun,
		Headroom:   headroom,
		NaiveCount: len(leaves),
		Naive:      naive,
		Scheduled:  scheduled,
		ValueSaved: scheduled.Value - naive.Value,
		TasksSaved: scheduled.Completed - naive.Completed,
	}
}

// replayNaive models today's behaviour: every task runs on its highest tier in
// declared (depth-first) order; once the budget is spent, every remaining task
// is hard-truncated. No down-tiering, no value-aware reordering.
func replayNaive(leaves []*tasktree.Task, budgetTokens int) Outcome {
	out := Outcome{Strategy: "hard-truncation"}
	remaining := budgetTokens
	cut := false
	for _, t := range leaves {
		top := t.HighestTier()
		est := t.EstAt(top)
		if !cut && est <= remaining {
			remaining -= est
			out.Spent += est
			out.Value += t.ValueAt(top)
			out.Completed++
			out.Decisions = append(out.Decisions, budget.Decision{
				TaskID: t.ID,
				Action: budget.Keep,
				Tier:   top,
				Budget: est,
				Value:  t.ValueAt(top),
				Reason: "ran on opus in declared order (no scheduling)",
			})
			continue
		}
		// First failure hard-cuts the rest of the run.
		cut = true
		out.Truncated++
		out.Decisions = append(out.Decisions, budget.Decision{
			TaskID: t.ID,
			Action: budget.Preempt,
			Tier:   tier.Unknown,
			Budget: 0,
			Value:  0,
			Reason: "hard-truncated: budget exhausted before this task",
		})
	}
	return out
}

func outcomeFromPlan(p schedule.Plan) Outcome {
	out := Outcome{Strategy: "scheduled"}
	out.Decisions = schedule.Sorted(p.Decisions)
	for _, d := range p.Decisions {
		out.Spent += d.Budget
		out.Value += d.Value
		if d.Action == budget.Preempt {
			out.Truncated++
		} else {
			out.Completed++
		}
	}
	return out
}
