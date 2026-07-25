package schedule

import (
	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
)

// Preemption hooks
//
// Schedule() resolves overrun entirely within the greedy Allocate() pass: it
// down-tiers or preempts each task against the remaining budget as it walks
// candidates (see internal/budget). v0.5.0 removed the standalone relax() /
// preempt() overrun-resolution helpers and the lowest-marginal-value relax
// loop that called them — that loop was provably unreachable (Allocate.place
// only ever commits Budget <= remaining, so total(decisions) could never
// exceed budgetTokens). The preemption-hook constructors below remain part of
// the public API: they let a harness pluggably force a preempt/down-tier
// during Allocate via Options.Hook.

// AlwaysKeep is a PreemptionHook that never preempts (lets the allocator do its
// thing). Useful as an explicit default.
func AlwaysKeep(_ *tasktree.Task, _ int) budget.Action { return budget.Keep }

// PreemptBelow returns a PreemptionHook that preempts any task whose declared
// value is strictly below threshold. This is the canonical example of a
// pluggable preemption policy a harness might supply.
func PreemptBelow(threshold float64) budget.PreemptionHook {
	return func(t *tasktree.Task, _ int) budget.Action {
		if t.Value < threshold {
			return budget.Preempt
		}
		return budget.Keep
	}
}
