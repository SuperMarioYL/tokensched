package policy

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
)

func mustParse(t *testing.T, y string) Policy {
	t.Helper()
	p, err := Parse([]byte(y))
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", y, err)
	}
	return p
}

// A leaf task helper for hook tests.
func leaf(id string, value float64) *tasktree.Task {
	tr := []tier.Tier{tier.Opus}
	return &tasktree.Task{
		ID:        id,
		Value:     value,
		Tiers:     tr,
		EstTokens: map[tier.Tier]int{tier.Opus: 100},
	}
}

func TestParse_PreemptBelowValue(t *testing.T) {
	p := mustParse(t, `
preemption:
  prefer_downtier: true
  preempt_below_value: 5
`)
	if p.PreemptBelowValue != 5 {
		t.Fatalf("preempt_below_value = %g, want 5", p.PreemptBelowValue)
	}
	if !p.PreferDownTier {
		t.Fatalf("prefer_downtier = false, want true")
	}
	if !p.Active() {
		t.Fatalf("Active() = false, want true for a positive threshold")
	}
}

func TestParse_ZeroThresholdIsNoOp(t *testing.T) {
	// An absent or zero threshold must be a no-op equal to today's behaviour:
	// Hook() returns nil so the scheduler runs exactly as the pre-v0.3.0 path.
	for _, y := range []string{
		`preemption: {preempt_below_value: 0}`,
		`preemption: {prefer_downtier: true}`, // threshold omitted
		``,                                    // no preemption block at all
	} {
		p := mustParse(t, y)
		if p.Active() {
			t.Fatalf("Active() = true for %q, want false (zero/absent threshold)", y)
		}
		if p.Hook() != nil {
			t.Fatalf("Hook() != nil for %q, want nil no-op", y)
		}
	}
}

func TestParse_MalformedErrors(t *testing.T) {
	// A negative threshold is rejected.
	if _, err := Parse([]byte(`preemption: {preempt_below_value: -1}`)); err == nil {
		t.Fatalf("Parse(negative threshold) = nil error, want error")
	}
	// Structurally broken YAML is rejected.
	if _, err := Parse([]byte("preemption: [this, is, not, a, map")); err == nil {
		t.Fatalf("Parse(broken yaml) = nil error, want error")
	}
}

func TestHook_PreemptsBelowThresholdKeepsAtOrAbove(t *testing.T) {
	p := mustParse(t, `preemption: {preempt_below_value: 10}`)
	h := p.Hook()
	if h == nil {
		t.Fatalf("Hook() = nil, want a hook for threshold 10")
	}
	// A task below the threshold is preempted under the policy ...
	if got := h(leaf("low", 9), 1_000_000); got != budget.Preempt {
		t.Fatalf("hook(value=9) = %v, want Preempt", got)
	}
	// ... and one at/above the threshold is kept (admitted by the allocator).
	if got := h(leaf("high", 10), 1_000_000); got != budget.Keep {
		t.Fatalf("hook(value=10) = %v, want Keep", got)
	}
}

func TestDefault_IsNoOp(t *testing.T) {
	d := Default()
	if d.Active() {
		t.Fatalf("Default().Active() = true, want false")
	}
	if d.Hook() != nil {
		t.Fatalf("Default().Hook() != nil, want nil")
	}
}

func TestParse_RealExampleFile(t *testing.T) {
	// The shipped policy.example.yaml carries preempt_below_value: 0 and
	// prefer_downtier: true — parsing it must yield a no-op policy.
	const sample = `
budget: 200000
strategy: value-per-token
preemption:
  prefer_downtier: true
  preempt_below_value: 0
tiers:
  opus: {cost_mult: 1.00, cap_mult: 1.00}
`
	p := mustParse(t, sample)
	if p.Active() {
		t.Fatalf("example-file policy Active() = true, want false (threshold 0)")
	}
	if !p.PreferDownTier {
		t.Fatalf("example-file prefer_downtier = false, want true")
	}
	if !strings.Contains(sample, "preempt_below_value") {
		t.Fatal("sanity: sample should mention preempt_below_value")
	}
}

// TestActive_TrueForPreferDownTierFalse (v0.4.0): prefer_downtier:false changes
// scheduling (preempt-before-down-tier) even with no threshold, so Active() must
// be true — the --json `active` field is now honest about the second knob.
func TestActive_TrueForPreferDownTierFalse(t *testing.T) {
	p := mustParse(t, `preemption: {prefer_downtier: false}`)
	if !p.Active() {
		t.Fatalf("prefer_downtier:false should make Active() true (it changes scheduling), got false")
	}
	// It does NOT set a preemption hook (no threshold) — the effect is via the
	// allocator's PreferDownTier flag, not the hook.
	if p.Hook() != nil {
		t.Fatalf("prefer_downtier:false should NOT set a hook (no threshold), got non-nil")
	}
}

// TestScheduleOptions_WiresPreferDownTier (v0.4.0): ScheduleOptions forwards both
// the hook and the PreferDownTier flag so the CLI can honour prefer_downtier:false.
func TestScheduleOptions_WiresPreferDownTier(t *testing.T) {
	// prefer_downtier:false => ScheduleOptions.PreferDownTier points at false.
	pFalse := mustParse(t, `preemption: {prefer_downtier: false}`)
	so := pFalse.ScheduleOptions()
	if so == nil || so.PreferDownTier == nil || *so.PreferDownTier {
		t.Fatalf("prefer_downtier:false should wire a non-nil *bool == false")
	}
	// Default (prefer_downtier true) => ScheduleOptions.PreferDownTier points at true.
	soDef := Default().ScheduleOptions()
	if soDef == nil || soDef.PreferDownTier == nil || !*soDef.PreferDownTier {
		t.Fatalf("default should wire a non-nil *bool == true")
	}
	// A positive threshold still yields a hook alongside the flag.
	pThresh := mustParse(t, `preemption: {prefer_downtier: true, preempt_below_value: 10}`)
	soT := pThresh.ScheduleOptions()
	if soT.Hook == nil {
		t.Fatalf("threshold policy should forward a non-nil hook")
	}
}
