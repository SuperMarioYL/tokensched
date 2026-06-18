package schedule

import (
	"fmt"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

// relax resolves overrun for a single task by lowering its commitment one step:
//
//   - if the task can run on a cheaper eligible tier, down-tier it (Opus ->
//     Sonnet -> Haiku);
//   - only when it is already on its cheapest eligible tier (not down-tierable)
//     does it get preempted.
//
// This encodes the m2 invariant "down-tier preferred over preempt". The hook,
// if present, may force a preempt regardless. fitBudget is the budget available
// to this task (total budget minus everything else already committed); relax
// only down-tiers to a tier that fits within it, otherwise preempts.
func relax(d *budget.Decision, t *tasktree.Task, hook budget.PreemptionHook, fitBudget int) {
	if d == nil || t == nil {
		return
	}
	if d.Action == budget.Preempt {
		return // already at the floor
	}

	// A hook may force an immediate preempt.
	if hook != nil && hook(t, fitBudget) == budget.Preempt {
		preempt(d, "preempted by preemption hook during overrun resolution")
		return
	}

	// Try to down-tier one step from the current tier.
	if next, ok := tier.Lower(d.Tier, t.Tiers); ok {
		est := t.EstAt(next)
		if est <= fitBudget {
			d.Action = budget.DownTier
			d.Tier = next
			d.Budget = est
			d.Value = t.ValueAt(next)
			d.Reason = fmt.Sprintf("overrun: down-tiered to %s at %d tok (lowest marginal value first)", next, est)
			return
		}
		// Even the next tier doesn't fit the residual budget: keep stepping
		// down to the cheapest eligible tier.
		cur := next
		for {
			lower, ok := tier.Lower(cur, t.Tiers)
			if !ok {
				break
			}
			cur = lower
			if t.EstAt(cur) <= fitBudget {
				d.Action = budget.DownTier
				d.Tier = cur
				d.Budget = t.EstAt(cur)
				d.Value = t.ValueAt(cur)
				d.Reason = fmt.Sprintf("overrun: down-tiered to %s at %d tok", cur, t.EstAt(cur))
				return
			}
		}
		// No cheaper tier fits => preempt.
		preempt(d, fmt.Sprintf("overrun: not fittable on any cheaper tier within %d tok; preempted", fitBudget))
		return
	}

	// Not down-tierable (already cheapest eligible tier) => preempt.
	preempt(d, fmt.Sprintf("overrun: %s is the cheapest eligible tier and won't fit; preempted (not down-tierable)", d.Tier))
}

func preempt(d *budget.Decision, reason string) {
	d.Action = budget.Preempt
	d.Tier = tier.Unknown
	d.Budget = 0
	d.Value = 0
	d.Reason = reason
}

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
