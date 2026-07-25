// Package schedule turns a budget allocation into a schedule that fits a fixed
// token budget. The allocator (package budget) decides admission order by
// value-per-token and resolves any predicted overrun as it walks candidates —
// down-tiering or preempting tasks that do not fit the remaining budget — so
// the final plan is guaranteed within budget. The scheduler wraps that pass
// and reports the overrun the naive all-top-tier plan would have produced.
package schedule

import (
	"sort"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
)

// Plan is the scheduler's output: a budget-conserving set of decisions plus
// the overrun it had to absorb.
type Plan struct {
	Decisions []budget.Decision
	Budget    int // the input budget
	// Overrun is the deficit (tokens) of the naive all-top-tier plan against
	// Budget; positive means the naive plan would have blown the budget.
	Overrun int
	Totals  budget.Totals
}

// Scheduler produces a within-budget Plan from a task tree.
type Scheduler struct {
	alloc budget.Allocator
	opts  Options
}

// Options tunes the scheduler.
type Options struct {
	// Hook is forwarded to the allocator and consulted once per task during
	// allocation (it may force a preempt or down-tier regardless of fit).
	Hook budget.PreemptionHook
	// PreferDownTier is forwarded to the allocator. nil => default
	// (down-tier before preempt, the historical behaviour); an explicit false
	// switches the allocator to preempt-before-down-tier. Wired from the policy
	// file's prefer_downtier knob by the CLI (v0.4.0).
	PreferDownTier *bool
}

// New builds a Scheduler backed by a GreedyAllocator.
func New(opts *Options) *Scheduler {
	o := Options{}
	if opts != nil {
		o = *opts
	}
	return &Scheduler{
		alloc: budget.NewGreedyAllocator(&budget.Options{Hook: o.Hook, PreferDownTier: o.PreferDownTier}),
		opts:  o,
	}
}

// Schedule allocates the budget across the tree and reports the residual
// overrun. The greedy allocator (Allocate) already down-tiers or preempts
// each task against the remaining budget as it walks candidates, so the
// returned decisions provably sum to <= budgetTokens; no second relax pass is
// needed.
//
// v0.5.0 removed a "relax the lowest-marginal-value running task" loop that
// sat after Allocate: it was provably unreachable, because Allocate.place()
// only ever commits Budget <= remaining-at-placement-time (preempted tasks
// commit 0), so the loop's condition (total(decisions) > budgetTokens) could
// never be true. Overrun resolution lives entirely in the greedy Allocate()
// pass; the removed lowest-marginal-value relax logic and its density ranking
// are no longer invoked by Schedule.
func (s *Scheduler) Schedule(root *tasktree.Task, budgetTokens int) Plan {
	decisions := s.alloc.Allocate(root, budgetTokens)
	overrun := budget.PredictOverrun(root, budgetTokens)

	return Plan{
		Decisions: decisions,
		Budget:    budgetTokens,
		Overrun:   overrun,
		Totals:    budget.Summarize(decisions),
	}
}

// Sorted returns decisions ordered for display: kept first, then down-tiered,
// then preempted, each group by descending value.
func Sorted(ds []budget.Decision) []budget.Decision {
	out := make([]budget.Decision, len(ds))
	copy(out, ds)
	rank := func(a budget.Action) int {
		switch a {
		case budget.Keep:
			return 0
		case budget.DownTier:
			return 1
		default:
			return 2
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if rank(out[i].Action) != rank(out[j].Action) {
			return rank(out[i].Action) < rank(out[j].Action)
		}
		if out[i].Value != out[j].Value {
			return out[i].Value > out[j].Value
		}
		return out[i].TaskID < out[j].TaskID
	})
	return out
}
