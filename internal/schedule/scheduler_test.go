package schedule

import (
	"testing"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

func leaf(id string, value float64, est map[tier.Tier]int) *tasktree.Task {
	return &tasktree.Task{
		ID:        id,
		Value:     value,
		Tiers:     []tier.Tier{tier.Opus, tier.Sonnet, tier.Haiku},
		EstTokens: est,
	}
}

func tree(children ...*tasktree.Task) *tasktree.Task {
	return &tasktree.Task{
		ID:        "(root)",
		Tiers:     []tier.Tier{tier.Opus, tier.Sonnet, tier.Haiku},
		EstTokens: map[tier.Tier]int{tier.Opus: 0, tier.Sonnet: 0, tier.Haiku: 0},
		Children:  children,
	}
}

func opusSonnetHaiku(o, s, h int) map[tier.Tier]int {
	return map[tier.Tier]int{tier.Opus: o, tier.Sonnet: s, tier.Haiku: h}
}

func decisionFor(p Plan, id string) budget.Decision {
	for _, d := range p.Decisions {
		if d.TaskID == id {
			return d
		}
	}
	return budget.Decision{}
}

func planTotal(p Plan) int {
	sum := 0
	for _, d := range p.Decisions {
		sum += d.Budget
	}
	return sum
}

// TestScheduleStaysWithinBudget: the final plan never exceeds the budget,
// across a deliberately over-budget tree and several budget levels.
func TestScheduleStaysWithinBudget(t *testing.T) {
	root := tree(
		leaf("a", 100, opusSonnetHaiku(80_000, 32_000, 9_600)),
		leaf("b", 70, opusSonnetHaiku(70_000, 28_000, 8_400)),
		leaf("c", 40, opusSonnetHaiku(90_000, 36_000, 10_800)),
		leaf("d", 10, opusSonnetHaiku(60_000, 24_000, 7_200)),
	)
	s := New(nil)
	for _, b := range []int{0, 10_000, 50_000, 100_000, 200_000, 500_000} {
		p := s.Schedule(root, b)
		if planTotal(p) > b {
			t.Fatalf("budget=%d: plan total %d exceeds budget", b, planTotal(p))
		}
	}
}

// TestHighValueKeptUnderOverrun: an over-budget tree keeps the highest-value
// task on Opus and pushes the lowest-value work down/out.
func TestHighValueKeptUnderOverrun(t *testing.T) {
	root := tree(
		leaf("critical", 100, opusSonnetHaiku(80_000, 32_000, 9_600)),
		leaf("nice", 50, opusSonnetHaiku(80_000, 32_000, 9_600)),
		leaf("trivial", 3, opusSonnetHaiku(80_000, 32_000, 9_600)),
	)
	// naive demand = 240k; budget 100k => must shed ~140k.
	p := New(nil).Schedule(root, 100_000)
	if p.Overrun != 140_000 {
		t.Fatalf("expected overrun 140000, got %d", p.Overrun)
	}

	critical := decisionFor(p, "critical")
	if critical.Action != budget.Keep || critical.Tier != tier.Opus {
		t.Fatalf("critical task should stay on opus, got %s/%s", critical.Action, critical.Tier)
	}
	trivial := decisionFor(p, "trivial")
	if trivial.Action == budget.Keep && trivial.Tier == tier.Opus {
		t.Fatalf("trivial task must not keep opus under heavy overrun, got %s/%s", trivial.Action, trivial.Tier)
	}
	// Critical must realise more value than trivial.
	if critical.Value <= trivial.Value {
		t.Fatalf("critical value %.1f should exceed trivial value %.1f", critical.Value, trivial.Value)
	}
}

// TestDownTierBeforePreempt: when a single task overruns but fits on a cheaper
// tier, the scheduler down-tiers it instead of preempting.
func TestDownTierBeforePreempt(t *testing.T) {
	root := tree(
		// Opus demand 200k > 150k budget, but Haiku fits comfortably.
		leaf("solo", 100, opusSonnetHaiku(200_000, 80_000, 24_000)),
	)
	p := New(nil).Schedule(root, 150_000)
	solo := decisionFor(p, "solo")
	if solo.Action != budget.DownTier {
		t.Fatalf("solo task should be down-tiered, not %s", solo.Action)
	}
	if solo.Action == budget.Preempt {
		t.Fatalf("solo task was preempted but a cheaper tier fit")
	}
	if solo.Budget == 0 || solo.Budget > 150_000 {
		t.Fatalf("down-tiered budget out of range: %d", solo.Budget)
	}
}

// TestPreemptOnlyWhenNotDownTierable: a Haiku-only task that doesn't fit gets
// preempted (it cannot be down-tiered further).
func TestPreemptOnlyWhenNotDownTierable(t *testing.T) {
	haikuOnly := &tasktree.Task{
		ID:        "haiku_only",
		Value:     5,
		Tiers:     []tier.Tier{tier.Haiku},
		EstTokens: map[tier.Tier]int{tier.Haiku: 50_000},
	}
	big := leaf("big", 100, opusSonnetHaiku(90_000, 36_000, 10_800))
	root := tree(big, haikuOnly)

	// Budget only fits 'big' on Opus (90k) with 5k left; haiku_only needs 50k.
	p := New(nil).Schedule(root, 95_000)
	ho := decisionFor(p, "haiku_only")
	if ho.Action != budget.Preempt {
		t.Fatalf("haiku-only task that won't fit should be preempted (not down-tierable), got %s", ho.Action)
	}
	if planTotal(p) > 95_000 {
		t.Fatalf("plan total %d exceeds budget", planTotal(p))
	}
}

// TestSchedulerPreemptHook: a value-threshold hook preempts low-value tasks.
func TestSchedulerPreemptHook(t *testing.T) {
	root := tree(
		leaf("keep", 100, opusSonnetHaiku(10_000, 4_000, 1_200)),
		leaf("drop", 2, opusSonnetHaiku(10_000, 4_000, 1_200)),
	)
	p := New(&Options{Hook: PreemptBelow(10)}).Schedule(root, 1_000_000)
	if d := decisionFor(p, "drop"); d.Action != budget.Preempt {
		t.Fatalf("low-value task should be preempted by hook, got %s", d.Action)
	}
	if d := decisionFor(p, "keep"); d.Action == budget.Preempt {
		t.Fatalf("high-value task should survive the hook, got %s", d.Action)
	}
}
