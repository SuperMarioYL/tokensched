package budget

import (
	"testing"

	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

// leaf builds a leaf task with explicit per-tier estimates.
func leaf(id string, value float64, tiers []tier.Tier, est map[tier.Tier]int) *tasktree.Task {
	return &tasktree.Task{ID: id, Value: value, Tiers: tiers, EstTokens: est}
}

func tree(children ...*tasktree.Task) *tasktree.Task {
	return &tasktree.Task{
		ID:        "(root)",
		Tiers:     []tier.Tier{tier.Opus, tier.Sonnet, tier.Haiku},
		EstTokens: map[tier.Tier]int{tier.Opus: 0, tier.Sonnet: 0, tier.Haiku: 0},
		Children:  children,
	}
}

func allTiers() []tier.Tier { return []tier.Tier{tier.Opus, tier.Sonnet, tier.Haiku} }

func decisionFor(ds []Decision, id string) Decision {
	for _, d := range ds {
		if d.TaskID == id {
			return d
		}
	}
	return Decision{}
}

// TestBudgetConservation: the sum of allocated budget never exceeds the input
// budget, across a range of budgets including 0.
func TestBudgetConservation(t *testing.T) {
	root := tree(
		leaf("a", 100, allTiers(), map[tier.Tier]int{tier.Opus: 80_000, tier.Sonnet: 32_000, tier.Haiku: 9_600}),
		leaf("b", 60, allTiers(), map[tier.Tier]int{tier.Opus: 70_000, tier.Sonnet: 28_000, tier.Haiku: 8_400}),
		leaf("c", 20, allTiers(), map[tier.Tier]int{tier.Opus: 90_000, tier.Sonnet: 36_000, tier.Haiku: 10_800}),
	)
	a := NewGreedyAllocator(nil)
	for _, b := range []int{0, 5_000, 50_000, 120_000, 240_000, 1_000_000} {
		ds := a.Allocate(root, b)
		tot := Summarize(ds)
		if tot.Budget > b {
			t.Fatalf("budget=%d: allocated %d tokens, exceeds budget", b, tot.Budget)
		}
		if len(ds) != 3 {
			t.Fatalf("budget=%d: expected 3 decisions, got %d", b, len(ds))
		}
	}
}

// TestHighValueNotCutFirst: under a tight budget, the highest value-per-token
// task must be kept on its top tier while lower-value tasks absorb the cuts.
func TestHighValueNotCutFirst(t *testing.T) {
	// All three cost the same on Opus; value differs => v/tok ranks by value.
	root := tree(
		leaf("high", 100, allTiers(), map[tier.Tier]int{tier.Opus: 80_000, tier.Sonnet: 32_000, tier.Haiku: 9_600}),
		leaf("mid", 50, allTiers(), map[tier.Tier]int{tier.Opus: 80_000, tier.Sonnet: 32_000, tier.Haiku: 9_600}),
		leaf("low", 5, allTiers(), map[tier.Tier]int{tier.Opus: 80_000, tier.Sonnet: 32_000, tier.Haiku: 9_600}),
	)
	// Budget fits exactly one Opus task plus scraps.
	ds := NewGreedyAllocator(nil).Allocate(root, 90_000)

	high := decisionFor(ds, "high")
	if high.Action != Keep || high.Tier != tier.Opus {
		t.Fatalf("highest-value task should be kept on opus, got action=%s tier=%s", high.Action, high.Tier)
	}
	low := decisionFor(ds, "low")
	if low.Action == Keep && low.Tier == tier.Opus {
		t.Fatalf("lowest-value task should NOT be kept on opus under a tight budget; got %s/%s", low.Action, low.Tier)
	}
	// And the high task must realise at least as much value as the low one.
	if high.Value < low.Value {
		t.Fatalf("high task value %.1f < low task value %.1f", high.Value, low.Value)
	}
}

// TestDownTierPreferredOverPreempt: when a task can fit on a cheaper tier, the
// allocator down-tiers it rather than preempting it.
func TestDownTierPreferredOverPreempt(t *testing.T) {
	root := tree(
		// 'big' takes the whole Opus budget.
		leaf("big", 100, allTiers(), map[tier.Tier]int{tier.Opus: 100_000, tier.Sonnet: 40_000, tier.Haiku: 12_000}),
		// 'small' won't fit on Opus in what's left, but fits on Haiku.
		leaf("small", 30, allTiers(), map[tier.Tier]int{tier.Opus: 80_000, tier.Sonnet: 32_000, tier.Haiku: 9_000}),
	)
	ds := NewGreedyAllocator(nil).Allocate(root, 110_000)

	small := decisionFor(ds, "small")
	if small.Action != DownTier {
		t.Fatalf("small task should be down-tiered (fits on a cheaper tier), got %s", small.Action)
	}
	if small.Action == Preempt {
		t.Fatalf("small task was preempted but a cheaper tier fit the budget")
	}
	if small.Budget == 0 {
		t.Fatalf("down-tiered task should still get a budget, got 0")
	}
}

// TestPreemptWhenNothingFits: a task whose cheapest tier still doesn't fit the
// remaining budget is preempted.
func TestPreemptWhenNothingFits(t *testing.T) {
	root := tree(
		leaf("hog", 100, allTiers(), map[tier.Tier]int{tier.Opus: 50_000, tier.Sonnet: 20_000, tier.Haiku: 6_000}),
		// Even Haiku (5_000) exceeds the 1_000 left after 'hog'.
		leaf("doomed", 10, allTiers(), map[tier.Tier]int{tier.Opus: 40_000, tier.Sonnet: 16_000, tier.Haiku: 5_000}),
	)
	ds := NewGreedyAllocator(nil).Allocate(root, 51_000)
	doomed := decisionFor(ds, "doomed")
	if doomed.Action != Preempt {
		t.Fatalf("doomed task should be preempted (no tier fits in remaining budget), got %s", doomed.Action)
	}
	if doomed.Budget != 0 {
		t.Fatalf("preempted task must have 0 budget, got %d", doomed.Budget)
	}
}

// TestPredictOverrun: the naive all-top-tier demand minus budget.
func TestPredictOverrun(t *testing.T) {
	root := tree(
		leaf("a", 1, allTiers(), map[tier.Tier]int{tier.Opus: 100_000, tier.Sonnet: 40_000, tier.Haiku: 12_000}),
		leaf("b", 1, allTiers(), map[tier.Tier]int{tier.Opus: 100_000, tier.Sonnet: 40_000, tier.Haiku: 12_000}),
	)
	if got := PredictOverrun(root, 150_000); got != 50_000 {
		t.Fatalf("PredictOverrun = %d, want 50000", got)
	}
	if got := PredictOverrun(root, 300_000); got != -100_000 {
		t.Fatalf("PredictOverrun (under budget) = %d, want -100000", got)
	}
}

// TestZeroCostFreeValueRankedFirst: a zero-token task with positive value is
// free realised value (unbounded value-per-token) and must be admitted ahead of
// every finite-cost task — never ranked last and never the first thing cut.
func TestZeroCostFreeValueRankedFirst(t *testing.T) {
	root := tree(
		leaf("free", 90, allTiers(), map[tier.Tier]int{tier.Opus: 0, tier.Sonnet: 0, tier.Haiku: 0}),
		leaf("paid", 100, allTiers(), map[tier.Tier]int{tier.Opus: 10_000, tier.Sonnet: 4_000, tier.Haiku: 1_200}),
	)
	ds := NewGreedyAllocator(nil).Allocate(root, 1_000_000)
	// Decisions come back in descending value-per-token order; the free task has
	// infinite v/tok, so it must be first.
	if ds[0].TaskID != "free" {
		t.Fatalf("zero-cost free-value task should rank first (infinite v/tok), got %q first", ds[0].TaskID)
	}
	if d := decisionFor(ds, "free"); d.Action != Keep {
		t.Fatalf("zero-cost free-value task should be kept, got %s", d.Action)
	}
}

// TestZeroCostFreeValueNotStarved: under a budget so tight only one paid task
// fits, the free-value task is still admitted (it costs nothing) rather than
// being starved by its mis-ranked value-per-token.
func TestZeroCostFreeValueNotStarved(t *testing.T) {
	root := tree(
		leaf("paid_low", 5, allTiers(), map[tier.Tier]int{tier.Opus: 80_000, tier.Sonnet: 32_000, tier.Haiku: 9_600}),
		leaf("free", 70, allTiers(), map[tier.Tier]int{tier.Opus: 0, tier.Sonnet: 0, tier.Haiku: 0}),
	)
	ds := NewGreedyAllocator(nil).Allocate(root, 80_000)
	if d := decisionFor(ds, "free"); d.Action == Preempt {
		t.Fatalf("zero-cost free-value task must never be preempted, got %s", d.Action)
	}
}

// TestPreemptionHook: a hook that forces preempt overrides admission.
func TestPreemptionHook(t *testing.T) {
	root := tree(
		leaf("keepme", 100, allTiers(), map[tier.Tier]int{tier.Opus: 10_000, tier.Sonnet: 4_000, tier.Haiku: 1_200}),
		leaf("dropme", 90, allTiers(), map[tier.Tier]int{tier.Opus: 10_000, tier.Sonnet: 4_000, tier.Haiku: 1_200}),
	)
	hook := PreemptionHook(func(tk *tasktree.Task, _ int) Action {
		if tk.ID == "dropme" {
			return Preempt
		}
		return Keep
	})
	a := NewGreedyAllocator(&Options{Hook: hook})
	ds := a.Allocate(root, 1_000_000) // plenty of budget; only the hook cuts

	if d := decisionFor(ds, "dropme"); d.Action != Preempt {
		t.Fatalf("hook should have preempted dropme, got %s", d.Action)
	}
	if d := decisionFor(ds, "keepme"); d.Action != Keep {
		t.Fatalf("keepme should be kept, got %s", d.Action)
	}
}
