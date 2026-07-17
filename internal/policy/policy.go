// Package policy parses a TokenSched scheduling policy file (the `preemption`
// block of policy.example.yaml) into a budget.PreemptionHook the scheduler can
// honour. Before v0.3.0 the documented preemption knobs (preempt_below_value,
// prefer_downtier) lived only in policy.example.yaml and in code as
// schedule.PreemptBelow / the PreemptionHook type — but the CLI never loaded
// them (run always passed a nil hook). This package makes the pluggable
// preemption policy reachable from the binary.
//
// The package performs file I/O only in LoadFile; Parse operates on bytes and
// Policy.Hook() is pure, so the importable budget/schedule packages stay
// I/O-free.
package policy

import (
	"fmt"
	"os"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/schedule"
	"gopkg.in/yaml.v3"
)

// Policy is the effective scheduling policy parsed from a policy file. Only the
// preemption knobs that change scheduler behaviour are modelled here; the tier
// coefficients in the file are reference documentation for the importable
// packages and are not overridden at runtime in v0.3.0.
type Policy struct {
	// PreemptBelowValue: preempt any sub-task whose declared value is strictly
	// below this threshold regardless of budget headroom. 0 disables the floor
	// (the no-policy default — every task is admitted by value-per-token).
	PreemptBelowValue float64
	// PreferDownTier: down-tier (Opus->Sonnet->Haiku) before preempting. The
	// scheduler's default is to down-tier first; as of v0.4.0 a false value is
	// HONOURED — the allocator switches to preempt-before-down-tier (a task
	// that does not fit on its top tier is dropped outright instead of being
	// salvaged on a cheaper model). Before v0.4.0 this field was reported-only.
	PreferDownTier bool
}

// yamlPolicy is the on-disk shape. Only the `preemption` block is consumed; the
// rest of policy.example.yaml (budget, strategy, tiers) is ignored here.
type yamlPolicy struct {
	Preemption struct {
		PreferDownTier    *bool    `yaml:"prefer_downtier"`
		PreemptBelowValue *float64 `yaml:"preempt_below_value"`
	} `yaml:"preemption"`
}

// Default returns the policy equivalent to today's no-policy behaviour: no
// value floor, down-tier preferred.
func Default() Policy {
	return Policy{PreemptBelowValue: 0, PreferDownTier: true}
}

// LoadFile reads and parses a policy file from disk.
func LoadFile(path string) (Policy, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Policy{}, fmt.Errorf("policy: read %s: %w", path, err)
	}
	p, err := Parse(b)
	if err != nil {
		return Policy{}, fmt.Errorf("policy: %s: %w", path, err)
	}
	return p, nil
}

// Parse decodes a Policy from raw YAML bytes. A missing `preemption` block is
// not an error — it yields the Default policy. A malformed document, or a
// negative preempt_below_value, is an error.
func Parse(data []byte) (Policy, error) {
	var doc yamlPolicy
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return Policy{}, fmt.Errorf("parse yaml: %w", err)
	}

	p := Default()
	if doc.Preemption.PreferDownTier != nil {
		p.PreferDownTier = *doc.Preemption.PreferDownTier
	}
	if doc.Preemption.PreemptBelowValue != nil {
		v := *doc.Preemption.PreemptBelowValue
		if v < 0 {
			return Policy{}, fmt.Errorf("preemption.preempt_below_value must be >= 0 (got %g)", v)
		}
		p.PreemptBelowValue = v
	}
	return p, nil
}

// Hook builds the budget.PreemptionHook this policy implies. A zero
// PreemptBelowValue means no floor, so Hook returns nil (the scheduler treats a
// nil hook as "no policy", identical to the pre-v0.3.0 run). A positive
// threshold returns schedule.PreemptBelow(threshold), preempting every sub-task
// whose declared value is strictly below it.
func (p Policy) Hook() budget.PreemptionHook {
	if p.PreemptBelowValue > 0 {
		return schedule.PreemptBelow(p.PreemptBelowValue)
	}
	return nil
}

// Active reports whether the policy changes scheduling vs the no-policy default
// (i.e. whether it sets a preemption threshold OR switches off down-tiering).
// A positive preempt_below_value or an explicit prefer_downtier:false both count
// as active; the no-policy default (threshold 0, prefer_downtier true) is inactive.
func (p Policy) Active() bool {
	return p.PreemptBelowValue > 0 || !p.PreferDownTier
}

// ScheduleOptions projects this policy into the scheduler's options, so the CLI
// can forward both knobs (the preemption hook and the PreferDownTier flag) in
// one call. The returned PreferDownTier pointer is always non-nil —
// Default()/Parse() always set a concrete bool — so the allocator's default-true
// path is only taken when the policy explicitly left prefer_downtier true
// (v0.4.0).
func (p Policy) ScheduleOptions() *schedule.Options {
	pd := p.PreferDownTier
	return &schedule.Options{Hook: p.Hook(), PreferDownTier: &pd}
}
