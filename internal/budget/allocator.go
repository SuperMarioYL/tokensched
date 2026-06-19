// Package budget is the stable, importable core of TokenSched: a per-sub-task
// token admission-controller. Given a task tree and a fixed token budget, an
// Allocator decides how to spend the budget by expected value-per-token,
// predicts overrun, and emits a Decision per leaf task (keep on its top tier,
// down-tier to a cheaper model, or preempt entirely).
//
// This package performs no I/O and makes no network calls. It is intended to
// be vendored into an agent harness: build a *tasktree.Task tree, call
// Allocate, and act on the returned []Decision.
package budget

import (
	"fmt"
	"math"
	"sort"

	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

// Action is the disposition the allocator chose for a task.
type Action int

const (
	// Keep runs the task on its highest eligible tier.
	Keep Action = iota
	// DownTier runs the task on a cheaper-than-top eligible tier.
	DownTier
	// Preempt drops the task entirely (no budget allocated).
	Preempt
)

// String renders the action for reports.
func (a Action) String() string {
	switch a {
	case Keep:
		return "keep"
	case DownTier:
		return "down-tier"
	case Preempt:
		return "preempt"
	default:
		return "unknown"
	}
}

// Decision is the allocator's verdict for one task.
type Decision struct {
	TaskID string    // task identifier
	Action Action    // Keep | DownTier | Preempt
	Tier   tier.Tier // tier the task is allocated to (Unknown when preempted)
	Budget int       // tokens allocated (0 when preempted)
	Value  float64   // realised value at the allocated tier (0 when preempted)
	Reason string    // human-readable explanation of the decision
}

// PreemptionHook lets a caller veto or force a disposition for a task at the
// moment it is being considered for the remaining budget. It is invoked once
// per task during allocation with the task and the budget remaining before the
// task is placed. Returning Keep/DownTier/Preempt overrides the allocator's
// own choice; returning the same action the allocator would have chosen is a
// no-op. A nil hook is ignored.
type PreemptionHook func(t *tasktree.Task, remaining int) Action

// Allocator distributes a fixed token budget across a task tree.
type Allocator interface {
	Allocate(root *tasktree.Task, budget int) []Decision
}

// Options tunes a GreedyAllocator.
type Options struct {
	// Hook, if non-nil, is consulted per task (see PreemptionHook).
	Hook PreemptionHook
}

// GreedyAllocator allocates by descending value-per-token. Each leaf task is
// considered at its highest eligible tier first; when the budget cannot cover
// it, the allocator down-tiers the task to the cheapest eligible tier that
// fits, and preempts only when no eligible tier fits. High-value tasks are
// admitted before low-value ones, so they are never the first to be cut.
type GreedyAllocator struct {
	opts Options
}

// NewGreedyAllocator builds a GreedyAllocator. Pass nil for default options.
func NewGreedyAllocator(opts *Options) *GreedyAllocator {
	a := &GreedyAllocator{}
	if opts != nil {
		a.opts = *opts
	}
	return a
}

// candidate is a leaf task paired with its top-tier value-per-token, used to
// order admission.
type candidate struct {
	task *tasktree.Task
	vpt  float64 // value-per-token at the highest eligible tier
}

// Allocate implements Allocator. It returns one Decision per leaf task in the
// tree, in descending value-per-token order (highest-value first). Budget is
// conserved: the sum of all Decision.Budget never exceeds the input budget.
func (a *GreedyAllocator) Allocate(root *tasktree.Task, budget int) []Decision {
	leaves := tasktree.Leaves(root)
	cands := make([]candidate, 0, len(leaves))
	for _, t := range leaves {
		top := t.HighestTier()
		est := t.EstAt(top)
		var vpt float64
		if est > 0 {
			vpt = t.ValueAt(top) / float64(est)
		} else {
			// A zero- (or non-positive) cost task is free realised value: its
			// value-per-token is unbounded, so it must rank ahead of every
			// finite-cost task (free value is never the first thing cut). A
			// zero-value, zero-cost task stays at the bottom on the tie-break.
			if t.ValueAt(top) > 0 {
				vpt = math.Inf(1)
			}
		}
		cands = append(cands, candidate{task: t, vpt: vpt})
	}
	// Highest value-per-token first; ties broken by higher declared value,
	// then by id for determinism.
	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].vpt != cands[j].vpt {
			return cands[i].vpt > cands[j].vpt
		}
		if cands[i].task.Value != cands[j].task.Value {
			return cands[i].task.Value > cands[j].task.Value
		}
		return cands[i].task.ID < cands[j].task.ID
	})

	remaining := budget
	decisions := make([]Decision, 0, len(cands))
	for _, c := range cands {
		d := a.place(c.task, remaining)
		if a.opts.Hook != nil {
			if forced := a.opts.Hook(c.task, remaining); forced != d.Action {
				d = a.applyForced(c.task, forced, remaining)
			}
		}
		remaining -= d.Budget
		decisions = append(decisions, d)
	}
	return decisions
}

// place is the core greedy step for one task against the remaining budget. It
// tries the highest eligible tier first (Keep), then walks down to cheaper
// eligible tiers (DownTier), and Preempts when nothing fits.
func (a *GreedyAllocator) place(t *tasktree.Task, remaining int) Decision {
	top := t.HighestTier()
	topEst := t.EstAt(top)

	if topEst <= remaining {
		return Decision{
			TaskID: t.ID,
			Action: Keep,
			Tier:   top,
			Budget: topEst,
			Value:  t.ValueAt(top),
			Reason: fmt.Sprintf("fits on %s at %d tok (v/tok=%s); kept", top, topEst, vptStr(t, top)),
		}
	}

	// Walk down eligible tiers (high->low) looking for the first that fits.
	cur := top
	for {
		next, ok := tier.Lower(cur, t.Tiers)
		if !ok {
			break
		}
		cur = next
		est := t.EstAt(cur)
		if est <= remaining {
			return Decision{
				TaskID: t.ID,
				Action: DownTier,
				Tier:   cur,
				Budget: est,
				Value:  t.ValueAt(cur),
				Reason: fmt.Sprintf("won't fit on %s (%d>%d rem); down-tiered to %s at %d tok", top, topEst, remaining, cur, est),
			}
		}
	}

	return Decision{
		TaskID: t.ID,
		Action: Preempt,
		Tier:   tier.Unknown,
		Budget: 0,
		Value:  0,
		Reason: fmt.Sprintf("no eligible tier fits in %d rem (cheapest=%s@%d); preempted", remaining, cur, t.EstAt(cur)),
	}
}

// applyForced realises a hook-forced action, clamping to what the budget can
// actually cover (a forced Keep that doesn't fit degrades to DownTier/Preempt).
func (a *GreedyAllocator) applyForced(t *tasktree.Task, action Action, remaining int) Decision {
	switch action {
	case Preempt:
		return Decision{
			TaskID: t.ID,
			Action: Preempt,
			Tier:   tier.Unknown,
			Budget: 0,
			Value:  0,
			Reason: "preempted by preemption hook",
		}
	case DownTier:
		// Force the cheapest eligible tier that fits, else preempt.
		cheapest := cheapestEligible(t)
		est := t.EstAt(cheapest)
		if est <= remaining {
			return Decision{
				TaskID: t.ID,
				Action: DownTier,
				Tier:   cheapest,
				Budget: est,
				Value:  t.ValueAt(cheapest),
				Reason: fmt.Sprintf("down-tiered to %s by preemption hook", cheapest),
			}
		}
		return Decision{
			TaskID: t.ID,
			Action: Preempt,
			Reason: fmt.Sprintf("hook forced down-tier but %s (%d) exceeds %d rem; preempted", cheapest, est, remaining),
		}
	case Keep:
		top := t.HighestTier()
		est := t.EstAt(top)
		if est <= remaining {
			return Decision{
				TaskID: t.ID,
				Action: Keep,
				Tier:   top,
				Budget: est,
				Value:  t.ValueAt(top),
				Reason: fmt.Sprintf("kept on %s by preemption hook", top),
			}
		}
		// Forced keep cannot exceed budget; fall back to normal placement.
		return a.place(t, remaining)
	default:
		return a.place(t, remaining)
	}
}

func eligible(t *tasktree.Task, tr tier.Tier) bool {
	for _, e := range t.Tiers {
		if e == tr {
			return true
		}
	}
	return false
}

func cheapestEligible(t *tasktree.Task) tier.Tier {
	best := t.HighestTier()
	for _, tr := range t.Tiers {
		if tr.Rank() < best.Rank() {
			best = tr
		}
	}
	return best
}

func vptStr(t *tasktree.Task, tr tier.Tier) string {
	est := t.EstAt(tr)
	if est <= 0 {
		return "inf"
	}
	return fmt.Sprintf("%.5f", t.ValueAt(tr)/float64(est))
}

// Totals summarises a decision set.
type Totals struct {
	Budget    int     // tokens allocated
	Value     float64 // realised value
	Kept      int     // count of Keep decisions
	DownTier  int     // count of DownTier decisions
	Preempted int     // count of Preempt decisions
}

// Summarize aggregates a slice of decisions.
func Summarize(ds []Decision) Totals {
	var t Totals
	for _, d := range ds {
		t.Budget += d.Budget
		t.Value += d.Value
		switch d.Action {
		case Keep:
			t.Kept++
		case DownTier:
			t.DownTier++
		case Preempt:
			t.Preempted++
		}
	}
	return t
}

// PredictOverrun reports the deficit (tokens) between running every leaf on its
// highest tier and the available budget. A positive value means the naive
// all-Opus plan overruns the budget by that many tokens.
func PredictOverrun(root *tasktree.Task, budget int) int {
	want := 0
	for _, t := range tasktree.Leaves(root) {
		want += t.EstAt(t.HighestTier())
	}
	return want - budget
}
