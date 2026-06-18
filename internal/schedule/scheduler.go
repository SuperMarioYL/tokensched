// Package schedule turns a budget allocation into a schedule that fits a fixed
// token budget. The allocator (package budget) decides admission order by
// value-per-token; the scheduler then resolves any predicted overrun by
// down-tiering or preempting tasks, lowest-marginal-value first, and
// guarantees the final plan is within budget.
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
	// Hook is forwarded to the allocator and consulted again during overrun
	// resolution.
	Hook budget.PreemptionHook
}

// New builds a Scheduler backed by a GreedyAllocator.
func New(opts *Options) *Scheduler {
	o := Options{}
	if opts != nil {
		o = *opts
	}
	return &Scheduler{
		alloc: budget.NewGreedyAllocator(&budget.Options{Hook: o.Hook}),
		opts:  o,
	}
}

// Schedule allocates the budget across the tree and resolves any residual
// overrun. The greedy allocator already down-tiers/preempts as it walks
// candidates; this pass guarantees the invariants hold and re-resolves any
// leftover overrun by repeatedly relaxing the lowest-marginal-value task
// (down-tier first, preempt only when not down-tierable).
func (s *Scheduler) Schedule(root *tasktree.Task, budgetTokens int) Plan {
	decisions := s.alloc.Allocate(root, budgetTokens)
	overrun := budget.PredictOverrun(root, budgetTokens)

	// Index decisions by task for in-place relaxation.
	byID := make(map[string]*budget.Decision, len(decisions))
	for i := range decisions {
		byID[decisions[i].TaskID] = &decisions[i]
	}
	taskByID := make(map[string]*tasktree.Task)
	for _, t := range tasktree.Leaves(root) {
		taskByID[t.ID] = t
	}

	// Relax until within budget. Each step picks the lowest-marginal-value
	// still-running task and relaxes it (down-tier, else preempt).
	for total(decisions) > budgetTokens {
		victim := lowestMarginalRunning(decisions, taskByID)
		if victim == nil {
			break // nothing left to relax
		}
		relax(byID[victim.TaskID], taskByID[victim.TaskID], s.opts.Hook, budgetTokens-totalExcluding(decisions, victim.TaskID))
	}

	return Plan{
		Decisions: decisions,
		Budget:    budgetTokens,
		Overrun:   overrun,
		Totals:    budget.Summarize(decisions),
	}
}

func total(ds []budget.Decision) int {
	sum := 0
	for _, d := range ds {
		sum += d.Budget
	}
	return sum
}

func totalExcluding(ds []budget.Decision, id string) int {
	sum := 0
	for _, d := range ds {
		if d.TaskID == id {
			continue
		}
		sum += d.Budget
	}
	return sum
}

// lowestMarginalRunning returns the still-running (non-preempted) decision with
// the lowest marginal value (realised value at its current tier). Ties broken
// by id for determinism.
func lowestMarginalRunning(ds []budget.Decision, _ map[string]*tasktree.Task) *budget.Decision {
	var best *budget.Decision
	for i := range ds {
		d := &ds[i]
		if d.Action == budget.Preempt {
			continue
		}
		if best == nil || d.Value < best.Value || (d.Value == best.Value && d.TaskID < best.TaskID) {
			best = d
		}
	}
	return best
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
