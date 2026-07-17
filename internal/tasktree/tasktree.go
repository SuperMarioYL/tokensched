// Package tasktree parses a YAML task tree into an in-memory *Task tree and
// validates it. A task tree describes the sub-tasks a coding agent intends to
// run: each node carries a declared value, a per-tier estimated token cost,
// and the set of model tiers it is eligible to run on.
package tasktree

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/SuperMarioYL/tokensched/internal/tier"
	"gopkg.in/yaml.v3"
)

// Task is the core node of a task tree. EstTokens holds the estimated token
// cost per eligible tier (quoted at that tier). Tiers lists the eligible
// tiers; a task always has at least one. Children are nested sub-tasks.
type Task struct {
	ID        string
	Value     float64
	EstTokens map[tier.Tier]int
	Tiers     []tier.Tier
	Children  []*Task
}

// yamlTask is the on-disk representation. est_tokens maps a tier name to a
// token estimate. A shorthand `est_tokens: <int>` (a single number) means the
// estimate at the highest eligible tier, scaled down for the others by the
// tier cost coefficients.
type yamlTask struct {
	ID        string      `yaml:"id"`
	Value     float64     `yaml:"value"`
	EstTokens yaml.Node   `yaml:"est_tokens"`
	Tiers     []string    `yaml:"tiers"`
	Children  []*yamlTask `yaml:"children"`
}

// yamlTree is the document root. A tree may declare a single root task under
// `root:` or a forest of top-level tasks under `tasks:`. When a forest is
// given it is wrapped in a synthetic root named "(root)".
type yamlTree struct {
	Root  *yamlTask   `yaml:"root"`
	Tasks []*yamlTask `yaml:"tasks"`
}

// LoadFile reads and parses a task tree from a YAML file.
func LoadFile(path string) (*Task, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("tasktree: read %s: %w", path, err)
	}
	t, err := Parse(b)
	if err != nil {
		return nil, fmt.Errorf("tasktree: %s: %w", path, err)
	}
	return t, nil
}

// Parse decodes a task tree from raw YAML bytes and validates it.
func Parse(data []byte) (*Task, error) {
	var doc yamlTree
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("parse yaml: %w", err)
	}

	var root *Task
	switch {
	case doc.Root != nil:
		t, err := convert(doc.Root)
		if err != nil {
			return nil, err
		}
		root = t
	case len(doc.Tasks) > 0:
		children := make([]*Task, 0, len(doc.Tasks))
		for _, yt := range doc.Tasks {
			c, err := convert(yt)
			if err != nil {
				return nil, err
			}
			children = append(children, c)
		}
		// Synthetic root: zero declared value, runs on the union of child
		// tiers so it never constrains allocation.
		root = &Task{
			ID:        "(root)",
			Value:     0,
			EstTokens: map[tier.Tier]int{tier.Opus: 0, tier.Sonnet: 0, tier.Haiku: 0},
			Tiers:     []tier.Tier{tier.Opus, tier.Sonnet, tier.Haiku},
			Children:  children,
		}
	default:
		return nil, fmt.Errorf("empty tree: expected a `root:` task or a `tasks:` list")
	}

	if err := Validate(root); err != nil {
		return nil, err
	}
	return root, nil
}

func convert(yt *yamlTask) (*Task, error) {
	if yt == nil {
		return nil, fmt.Errorf("nil task node")
	}
	if strings.TrimSpace(yt.ID) == "" {
		return nil, fmt.Errorf("task missing id")
	}

	tiers := make([]tier.Tier, 0, len(yt.Tiers))
	for _, name := range yt.Tiers {
		tr, err := tier.Parse(name)
		if err != nil {
			return nil, fmt.Errorf("task %q: %w", yt.ID, err)
		}
		tiers = append(tiers, tr)
	}
	// Sort tiers high->low for deterministic ordering.
	sort.Slice(tiers, func(i, j int) bool { return tiers[i].Rank() > tiers[j].Rank() })

	est, err := decodeEstTokens(yt, tiers)
	if err != nil {
		return nil, err
	}

	children := make([]*Task, 0, len(yt.Children))
	for _, c := range yt.Children {
		cc, err := convert(c)
		if err != nil {
			return nil, err
		}
		children = append(children, cc)
	}

	return &Task{
		ID:        yt.ID,
		Value:     yt.Value,
		EstTokens: est,
		Tiers:     tiers,
		Children:  children,
	}, nil
}

// decodeEstTokens accepts either a scalar (single token estimate at the
// highest eligible tier, scaled to the others) or a mapping of tier->tokens.
func decodeEstTokens(yt *yamlTask, tiers []tier.Tier) (map[tier.Tier]int, error) {
	out := map[tier.Tier]int{}

	// Leaf-less / unset est_tokens (zero node) => all zero.
	if yt.EstTokens.Kind == 0 {
		for _, tr := range tiers {
			out[tr] = 0
		}
		return out, nil
	}

	switch yt.EstTokens.Kind {
	case yaml.ScalarNode:
		var base int
		if err := yt.EstTokens.Decode(&base); err != nil {
			return nil, fmt.Errorf("task %q: est_tokens scalar: %w", yt.ID, err)
		}
		top := tier.Highest(tiers)
		if top == tier.Unknown {
			return nil, fmt.Errorf("task %q: est_tokens scalar needs a tiers list", yt.ID)
		}
		// base is quoted at the top tier; scale by the cost coefficient ratio
		// for the cheaper tiers so a single number still produces a sensible
		// per-tier estimate.
		for _, tr := range tiers {
			ratio := tr.CostMult() / top.CostMult()
			out[tr] = int(float64(base) * ratio)
		}
	case yaml.MappingNode:
		raw := map[string]int{}
		if err := yt.EstTokens.Decode(&raw); err != nil {
			return nil, fmt.Errorf("task %q: est_tokens map: %w", yt.ID, err)
		}
		// An est_tokens map may name a tier the task is NOT eligible for
		// (e.g. tiers:[opus] + est_tokens:{haiku:5000}). Storing it would
		// silently hide a contradictory input: the tier is never selected
		// (tier.Lower only walks the allowed set), so the estimate is kept
		// but unused and the user most likely believes the tier is
		// eligible because they quoted a cost for it. Reject it up front.
		allowed := make(map[tier.Tier]bool, len(tiers))
		for _, tr := range tiers {
			allowed[tr] = true
		}
		var names []string
		for _, tr := range tiers {
			names = append(names, tr.String())
		}
		for name, v := range raw {
			tr, err := tier.Parse(name)
			if err != nil {
				return nil, fmt.Errorf("task %q: est_tokens key: %w", yt.ID, err)
			}
			if !allowed[tr] {
				return nil, fmt.Errorf("task %q: est_tokens tier %q is not in the eligible tiers [%s]", yt.ID, tr, strings.Join(names, ", "))
			}
			out[tr] = v
		}
		// Fill any eligible tier missing from the map by scaling from the
		// nearest provided higher tier.
		for _, tr := range tiers {
			if _, ok := out[tr]; ok {
				continue
			}
			out[tr] = inferEstimate(tr, out, tiers)
		}
	default:
		return nil, fmt.Errorf("task %q: est_tokens must be a number or a tier map", yt.ID)
	}
	return out, nil
}

// inferEstimate derives a token estimate for a tier missing from the explicit
// est_tokens map by scaling the highest available higher-tier estimate by the
// cost-coefficient ratio.
func inferEstimate(want tier.Tier, have map[tier.Tier]int, tiers []tier.Tier) int {
	// Prefer the closest higher tier that has an estimate.
	best := tier.Unknown
	for tr := range have {
		if tr.Rank() > want.Rank() && (best == tier.Unknown || tr.Rank() < best.Rank()) {
			best = tr
		}
	}
	if best == tier.Unknown {
		// No higher tier; use the closest lower one.
		for tr := range have {
			if best == tier.Unknown || tr.Rank() > best.Rank() {
				best = tr
			}
		}
	}
	if best == tier.Unknown {
		return 0
	}
	ratio := want.CostMult() / best.CostMult()
	return int(float64(have[best]) * ratio)
}

// Validate enforces structural invariants over the whole tree:
//   - every node value must be >= 0
//   - every node must have a non-empty tiers list
//   - every eligible tier must have a non-negative token estimate
//   - ids must be unique across the tree
func Validate(root *Task) error {
	if root == nil {
		return fmt.Errorf("nil root")
	}
	seen := map[string]bool{}
	var walk func(t *Task) error
	walk = func(t *Task) error {
		if t.Value < 0 {
			return fmt.Errorf("task %q: value must be >= 0 (got %g)", t.ID, t.Value)
		}
		if len(t.Tiers) == 0 {
			return fmt.Errorf("task %q: tiers must be non-empty", t.ID)
		}
		if seen[t.ID] {
			return fmt.Errorf("duplicate task id %q", t.ID)
		}
		seen[t.ID] = true
		for _, tr := range t.Tiers {
			est, ok := t.EstTokens[tr]
			if !ok {
				return fmt.Errorf("task %q: missing est_tokens for tier %s", t.ID, tr)
			}
			if est < 0 {
				return fmt.Errorf("task %q: est_tokens[%s] must be >= 0 (got %d)", t.ID, tr, est)
			}
		}
		for _, c := range t.Children {
			if err := walk(c); err != nil {
				return err
			}
		}
		return nil
	}
	return walk(root)
}

// Leaves returns the leaf tasks (no children) in depth-first order. Allocation
// operates over leaves: interior nodes are organisational. The synthetic root
// is never a leaf when it has children.
func Leaves(root *Task) []*Task {
	var out []*Task
	var walk func(t *Task)
	walk = func(t *Task) {
		if len(t.Children) == 0 {
			out = append(out, t)
			return
		}
		for _, c := range t.Children {
			walk(c)
		}
	}
	walk(root)
	return out
}

// HighestTier returns the most capable eligible tier for the task.
func (t *Task) HighestTier() tier.Tier {
	return tier.Highest(t.Tiers)
}

// EstAt returns the estimated token cost of running the task on tr. If the
// task is not eligible for tr it falls back to its highest tier estimate.
func (t *Task) EstAt(tr tier.Tier) int {
	if v, ok := t.EstTokens[tr]; ok {
		return v
	}
	return t.EstTokens[t.HighestTier()]
}

// ValueAt returns the realised value of running the task on tr: declared value
// scaled by the tier capability coefficient.
func (t *Task) ValueAt(tr tier.Tier) float64 {
	return t.Value * tr.CapMult()
}
