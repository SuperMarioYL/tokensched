package schedule

import (
	"os"
	"strings"
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

// ---------------------------------------------------------------------------
// v0.5.0 regression note: removed the unreachable Schedule() relax loop.
// ---------------------------------------------------------------------------
//
// v0.5.0 removed Schedule()'s post-Allocate "relax the lowest-marginal-value
// running task" loop:
//
//	for total(decisions) > budgetTokens {
//	    victim := lowestMarginalRunning(decisions, taskByID)
//	    if victim == nil { break }
//	    relax(byID[victim.TaskID], taskByID[victim.TaskID], s.opts.Hook, ...)
//	}
//
// It was provably UNREACHABLE: GreedyAllocator.Allocate conserves budget by
// construction — place()/applyForced() only ever commit Decision.Budget <= the
// remaining budget at placement time (and preempted tasks commit 0) — so the
// loop's condition (total(decisions) > budgetTokens) could never be true. The
// documented "scheduler relaxes the lowest-marginal-value task first"
// behaviour was a production no-op; overrun is resolved entirely inside the
// greedy Allocate() pass.
//
// The two direct-call tests below (TestRelaxSpreadsAcrossFrontier and
// TestRelaxPrefersDownTierableVictim) previously exercised lowestMarginalRunning
// and relax() in isolation, bypassing Schedule(). Those symbols were deleted
// alongside the loop. The tests are retained here as a REGRESSION NOTE: the
// density-vs-raw-value ranking and the "prefer a down-tierable victim" logic
// were correct in isolation and still work if the relax pass is ever re-wired.
// They are intentionally commented out (the symbols they called no longer
// exist); see the behavior-equivalence test below for the invariant the dead
// loop was nominally guarding, now covered end-to-end through Schedule().
//
// (Original bodies preserved verbatim in comments for future reference.)
//
// func TestRelaxSpreadsAcrossFrontier(t *testing.T) {
//	tasks := map[string]*tasktree.Task{
//		"x": leaf("x", 31, opusSonnetHaiku(30_000, 12_000, 3_600)),
//		"y": leaf("y", 30, opusSonnetHaiku(30_000, 12_000, 3_600)),
//		"z": leaf("z", 29, opusSonnetHaiku(30_000, 12_000, 3_600)),
//	}
//	dec := func(id string) budget.Decision {
//		tt := tasks[id]
//		return budget.Decision{
//			TaskID: id, Action: budget.Keep, Tier: tier.Opus,
//			Budget: tt.EstAt(tier.Opus), Value: tt.ValueAt(tier.Opus),
//		}
//	}
//	decisions := []budget.Decision{dec("x"), dec("y"), dec("z")}
//	v1 := lowestMarginalRunning(decisions, tasks)
//	if v1 == nil { t.Fatal("step 1: no victim selected") }
//	for i := range decisions {
//		if decisions[i].TaskID == v1.TaskID {
//			relax(&decisions[i], tasks[v1.TaskID], nil, 1_000_000)
//		}
//	}
//	v2 := lowestMarginalRunning(decisions, tasks)
//	if v2 == nil { t.Fatal("step 2: no victim selected") }
//	if v1.TaskID == v2.TaskID {
//		t.Fatalf("relaxation re-selected the same task %q ...", v1.TaskID)
//	}
// }
//
// func TestRelaxPrefersDownTierableVictim(t *testing.T) {
//	floored := &tasktree.Task{
//		ID: "floored", Value: 8, Tiers: []tier.Tier{tier.Haiku},
//		EstTokens: map[tier.Tier]int{tier.Haiku: 5_000},
//	}
//	roomy := leaf("roomy", 9, opusSonnetHaiku(30_000, 12_000, 3_600))
//	tasks := map[string]*tasktree.Task{"floored": floored, "roomy": roomy}
//	decisions := []budget.Decision{
//		{TaskID: "floored", Action: budget.Keep, Tier: tier.Haiku, Budget: 5_000, Value: floored.ValueAt(tier.Haiku)},
//		{TaskID: "roomy", Action: budget.Keep, Tier: tier.Opus, Budget: 30_000, Value: roomy.ValueAt(tier.Opus)},
//	}
//	v := lowestMarginalRunning(decisions, tasks)
//	if v == nil || v.TaskID != "roomy" {
//		got := "<nil>"; if v != nil { got = v.TaskID }
//		t.Fatalf("expected down-tierable 'roomy' to be relaxed first, got %s", got)
//	}
// }

// exampleTree mirrors examples/overrun-tasktree.yaml: a realistic Claude Code
// session whose naive all-top-tier demand (~287k tokens) overruns a 200k
// window. refactor-config-loader is restricted to [sonnet, haiku]; all other
// leaves run [opus, sonnet, haiku].
func exampleTree() *tasktree.Task {
	return tree(
		leaf("design-oauth-flow", 95, opusSonnetHaiku(70_000, 28_000, 9_000)),
		leaf("implement-token-exchange", 90, opusSonnetHaiku(60_000, 24_000, 8_000)),
		leaf("write-integration-tests", 55, opusSonnetHaiku(50_000, 20_000, 6_500)),
		&tasktree.Task{
			ID:        "refactor-config-loader",
			Value:     30,
			Tiers:     []tier.Tier{tier.Sonnet, tier.Haiku},
			EstTokens: map[tier.Tier]int{tier.Sonnet: 22_000, tier.Haiku: 7_000},
		},
		leaf("update-changelog", 8, opusSonnetHaiku(40_000, 16_000, 5_000)),
		leaf("tidy-import-ordering", 3, opusSonnetHaiku(45_000, 18_000, 5_500)),
	)
}

// TestScheduleBudgetInvariantHoldsWithoutRelaxLoop is the v0.5.0
// behavior-equivalence proof for removing the dead relax loop. It runs Schedule()
// across a battery of budgets — the same budgets the bug-hunter instrumented to
// prove the loop never fired (under-run, the overrun knee, and deep overruns) —
// and asserts the invariant the dead loop was supposed to enforce but never
// needed, since greedy Allocate.place() already enforces it:
//
//	total(decisions) <= budgetTokens   for every budget.
//
// Removing the loop must not break this invariant; this test locks that in.
func TestScheduleBudgetInvariantHoldsWithoutRelaxLoop(t *testing.T) {
	root := exampleTree()
	s := New(nil)
	for _, b := range []int{50_000, 100_000, 150_000, 200_000, 300_000, 360_000, 1_000_000} {
		p := s.Schedule(root, b)
		if got := planTotal(p); got > b {
			t.Fatalf("budget=%d: plan total %d exceeds budget (invariant broken after removing the relax loop)", b, got)
		}
		for _, d := range p.Decisions {
			if d.Budget < 0 {
				t.Fatalf("budget=%d: decision %q has negative budget %d", b, d.TaskID, d.Budget)
			}
		}
	}
}

// TestRelaxLoopRemovedFromScheduler proves the dead relax loop is physically
// gone from scheduler.go (the grep -c == 0 assertion). If a future change
// reintroduces `for total(decisions) > budgetTokens` or the lowestMarginalRunning
// caller, this test fails and forces the author to reconcile with the fact
// that Allocate() provably conserves budget.
func TestRelaxLoopRemovedFromScheduler(t *testing.T) {
	src, err := os.ReadFile("scheduler.go")
	if err != nil {
		t.Fatalf("read scheduler.go: %v", err)
	}
	body := string(src)
	if strings.Contains(body, "for total(decisions) > budgetTokens") {
		t.Fatalf("scheduler.go still contains the unreachable relax loop " +
			"`for total(decisions) > budgetTokens`; v0.5.0 removed it because " +
			"Allocate.place() provably conserves budget (the condition is never true)")
	}
	if strings.Contains(body, "lowestMarginalRunning") {
		t.Fatalf("scheduler.go still references lowestMarginalRunning, which v0.5.0 removed from Schedule()")
	}
	if strings.Contains(body, "func total(") || strings.Contains(body, "func totalExcluding(") {
		t.Fatalf("scheduler.go still defines total()/totalExcluding(), which v0.5.0 removed with the relax loop")
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
