package report

import (
	"encoding/json"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/sim"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

// The JSON renderers below emit a stable, machine-readable view of the same
// information the lipgloss reports show, so a harness can consume TokenSched's
// decisions programmatically (no ANSI, no table parsing). The shapes are part
// of the CLI contract: additive changes only.

// jsonLeaf is one leaf task in a `plan --json` document.
type jsonLeaf struct {
	ID      string   `json:"id"`
	Value   float64  `json:"value"`
	Tiers   []string `json:"tiers"`
	TopTier string   `json:"top_tier"`
	InitTok int      `json:"init_tokens"` // estimate at the top tier
}

// jsonPlan is the `plan --json` document.
type jsonPlan struct {
	Root        string     `json:"root"`
	Budget      int        `json:"budget,omitempty"`
	LeafCount   int        `json:"leaf_count"`
	NaiveDemand int        `json:"naive_demand_tokens"`
	Overrun     int        `json:"overrun_tokens"` // naive demand - budget (0 when no budget given)
	Leaves      []jsonLeaf `json:"leaves"`
}

// jsonDecision is one allocation decision.
type jsonDecision struct {
	TaskID string  `json:"task_id"`
	Action string  `json:"action"` // keep | down-tier | preempt
	Tier   string  `json:"tier"`   // "" when preempted
	Budget int     `json:"budget_tokens"`
	Value  float64 `json:"value"`
	Reason string  `json:"reason"`
}

// jsonOutcome is one execution strategy's result.
type jsonOutcome struct {
	Strategy  string         `json:"strategy"`
	Spent     int            `json:"spent_tokens"`
	Value     float64        `json:"value"`
	Completed int            `json:"completed"`
	Truncated int            `json:"truncated"`
	Decisions []jsonDecision `json:"decisions"`
}

// jsonRun is the `run --json` document.
type jsonRun struct {
	Budget     int         `json:"budget"`
	LeafCount  int         `json:"leaf_count"`
	Overrun    int         `json:"overrun_tokens"`
	Naive      jsonOutcome `json:"naive"`
	Scheduled  jsonOutcome `json:"scheduled"`
	ValueSaved float64     `json:"value_saved"`
	TasksSaved int         `json:"tasks_saved"`
}

func decisionJSON(d budget.Decision) jsonDecision {
	tierStr := ""
	if d.Action != budget.Preempt {
		tierStr = d.Tier.String()
	}
	return jsonDecision{
		TaskID: d.TaskID,
		Action: d.Action.String(),
		Tier:   tierStr,
		Budget: d.Budget,
		Value:  d.Value,
		Reason: d.Reason,
	}
}

func outcomeJSON(o sim.Outcome) jsonOutcome {
	ds := make([]jsonDecision, 0, len(o.Decisions))
	for _, d := range o.Decisions {
		ds = append(ds, decisionJSON(d))
	}
	return jsonOutcome{
		Strategy:  o.Strategy,
		Spent:     o.Spent,
		Value:     o.Value,
		Completed: o.Completed,
		Truncated: o.Truncated,
		Decisions: ds,
	}
}

// TreeJSON renders a task tree as a JSON `plan` document (indented, trailing
// newline). budgetTokens of 0 means "no budget given": overrun is reported as 0.
func TreeJSON(root *tasktree.Task, budgetTokens int) (string, error) {
	leaves := tasktree.Leaves(root)
	out := jsonPlan{
		Root:      root.ID,
		Budget:    budgetTokens,
		LeafCount: len(leaves),
		Leaves:    make([]jsonLeaf, 0, len(leaves)),
	}
	for _, l := range leaves {
		top := l.HighestTier()
		est := l.EstAt(top)
		out.NaiveDemand += est
		out.Leaves = append(out.Leaves, jsonLeaf{
			ID:      l.ID,
			Value:   l.Value,
			Tiers:   tierStrings(l.Tiers),
			TopTier: top.String(),
			InitTok: est,
		})
	}
	if budgetTokens > 0 {
		out.Overrun = out.NaiveDemand - budgetTokens
	}
	return marshal(out)
}

// FullJSON renders a run comparison as a JSON `run` document (indented,
// trailing newline).
func FullJSON(c sim.Comparison) (string, error) {
	out := jsonRun{
		Budget:     c.Budget,
		LeafCount:  c.NaiveCount,
		Overrun:    c.Overrun,
		Naive:      outcomeJSON(c.Naive),
		Scheduled:  outcomeJSON(c.Scheduled),
		ValueSaved: c.ValueSaved,
		TasksSaved: c.TasksSaved,
	}
	return marshal(out)
}

func marshal(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b) + "\n", nil
}

func tierStrings(ts []tier.Tier) []string {
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.String()
	}
	return out
}
