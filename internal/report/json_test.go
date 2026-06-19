package report

import (
	"encoding/json"
	"testing"

	"github.com/SuperMarioYL/tokensched/internal/sim"
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

func osh(o, s, h int) map[tier.Tier]int {
	return map[tier.Tier]int{tier.Opus: o, tier.Sonnet: s, tier.Haiku: h}
}

// TestTreeJSONIsValidAndComplete: plan --json emits valid JSON with the leaves,
// naive demand, and overrun computed against the budget.
func TestTreeJSONIsValidAndComplete(t *testing.T) {
	root := tree(
		leaf("a", 100, osh(80_000, 32_000, 9_600)),
		leaf("b", 50, osh(70_000, 28_000, 8_400)),
	)
	s, err := TreeJSON(root, 100_000)
	if err != nil {
		t.Fatalf("TreeJSON: %v", err)
	}
	var doc struct {
		Root        string `json:"root"`
		Budget      int    `json:"budget"`
		LeafCount   int    `json:"leaf_count"`
		NaiveDemand int    `json:"naive_demand_tokens"`
		Overrun     int    `json:"overrun_tokens"`
		Leaves      []struct {
			ID      string `json:"id"`
			TopTier string `json:"top_tier"`
			InitTok int    `json:"init_tokens"`
		} `json:"leaves"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("emitted invalid JSON: %v\n%s", err, s)
	}
	if doc.LeafCount != 2 || len(doc.Leaves) != 2 {
		t.Fatalf("expected 2 leaves, got count=%d len=%d", doc.LeafCount, len(doc.Leaves))
	}
	if doc.NaiveDemand != 150_000 {
		t.Fatalf("naive demand = %d, want 150000", doc.NaiveDemand)
	}
	if doc.Overrun != 50_000 {
		t.Fatalf("overrun = %d, want 50000", doc.Overrun)
	}
	if doc.Leaves[0].TopTier != "opus" || doc.Leaves[0].InitTok != 80_000 {
		t.Fatalf("leaf[0] mismatch: %+v", doc.Leaves[0])
	}
}

// TestTreeJSONNoBudgetOverrunZero: with no budget, overrun is reported as 0.
func TestTreeJSONNoBudgetOverrunZero(t *testing.T) {
	root := tree(leaf("a", 1, osh(10_000, 4_000, 1_200)))
	s, err := TreeJSON(root, 0)
	if err != nil {
		t.Fatalf("TreeJSON: %v", err)
	}
	var doc struct {
		Overrun int `json:"overrun_tokens"`
		Budget  int `json:"budget"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("invalid JSON: %v", err)
	}
	if doc.Overrun != 0 {
		t.Fatalf("overrun without a budget should be 0, got %d", doc.Overrun)
	}
}

// TestFullJSONRoundTrips: run --json emits valid JSON carrying both strategies'
// decisions and the headline savings.
func TestFullJSONRoundTrips(t *testing.T) {
	root := tree(
		leaf("critical", 100, osh(80_000, 32_000, 9_600)),
		leaf("nice", 50, osh(80_000, 32_000, 9_600)),
		leaf("trivial", 3, osh(80_000, 32_000, 9_600)),
	)
	cmp := sim.Replay(root, 100_000, nil)
	s, err := FullJSON(cmp)
	if err != nil {
		t.Fatalf("FullJSON: %v", err)
	}
	var doc struct {
		Budget    int `json:"budget"`
		Overrun   int `json:"overrun_tokens"`
		Scheduled struct {
			Decisions []struct {
				TaskID string `json:"task_id"`
				Action string `json:"action"`
				Tier   string `json:"tier"`
			} `json:"decisions"`
		} `json:"scheduled"`
		TasksSaved int `json:"tasks_saved"`
	}
	if err := json.Unmarshal([]byte(s), &doc); err != nil {
		t.Fatalf("emitted invalid JSON: %v\n%s", err, s)
	}
	if doc.Budget != 100_000 {
		t.Fatalf("budget = %d, want 100000", doc.Budget)
	}
	if len(doc.Scheduled.Decisions) != 3 {
		t.Fatalf("expected 3 scheduled decisions, got %d", len(doc.Scheduled.Decisions))
	}
	// A preempted task must serialise with an empty tier string.
	for _, d := range doc.Scheduled.Decisions {
		if d.Action == "preempt" && d.Tier != "" {
			t.Fatalf("preempted task %q should have empty tier, got %q", d.TaskID, d.Tier)
		}
	}
}
