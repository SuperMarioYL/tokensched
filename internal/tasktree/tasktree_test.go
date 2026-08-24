package tasktree

import (
	"strings"
	"testing"

	"github.com/SuperMarioYL/tokensched/internal/tier"
)

// mustParse fails the test if the YAML does not parse cleanly.
func mustParse(t *testing.T, y string) *Task {
	t.Helper()
	root, err := Parse([]byte(y))
	if err != nil {
		t.Fatalf("Parse(%q) error: %v", y, err)
	}
	return root
}

// wantParseErr asserts the YAML fails to parse.
func wantParseErr(t *testing.T, y, wantSub string) {
	t.Helper()
	_, err := Parse([]byte(y))
	if err == nil {
		t.Fatalf("Parse(%q) = nil error, want an error", y)
	}
	if wantSub != "" && !strings.Contains(err.Error(), wantSub) {
		t.Fatalf("Parse(%q) err = %q, want it to contain %q", y, err.Error(), wantSub)
	}
}

// TestParseEstTokensNonEligibleTierRejected (v0.4.0): a task declaring
// tiers:[opus] but quoting an est_tokens entry for haiku is a contradictory
// input — the user very likely thinks Haiku is eligible. Before v0.4.0 the
// haiku estimate was stored and then silently dropped (Haiku is never selected
// because it is not in the allowed tiers). It now fails fast.
func TestParseEstTokensNonEligibleTierRejected(t *testing.T) {
	wantParseErr(t, `
root:
  id: only-opus
  value: 10
  tiers: [opus]
  est_tokens:
    opus: 70000
    haiku: 5000
`, "not in the eligible tiers")
}

// TestParseEstTokensEligibleMapAccepted: a map naming only eligible tiers
// (including a subset like tiers:[sonnet,haiku]) parses cleanly.
func TestParseEstTokensEligibleMapAccepted(t *testing.T) {
	root := mustParse(t, `
root:
  id: subset
  value: 30
  tiers: [sonnet, haiku]
  est_tokens:
    sonnet: 22000
    haiku: 7000
`)
	if got := root.EstAt(tier.Haiku); got != 7000 {
		t.Fatalf("EstAt(haiku) = %d, want 7000", got)
	}
	if got := root.EstAt(tier.Sonnet); got != 22000 {
		t.Fatalf("EstAt(sonnet) = %d, want 22000", got)
	}
}

// TestParseEstTokensScalarStillScales: the scalar shorthand keeps working for a
// single-tier task (the v0.4.0 validation only tightens the map path).
func TestParseEstTokensScalarStillScales(t *testing.T) {
	root := mustParse(t, `
root:
  id: scalar
  value: 5
  tiers: [opus, sonnet, haiku]
  est_tokens: 100000
`)
	if got := root.EstAt(tier.Opus); got != 100000 {
		t.Fatalf("EstAt(opus) = %d, want 100000", got)
	}
	// Sonnet scales by the cost-coefficient ratio 0.40/1.0 = 0.40.
	if got := root.EstAt(tier.Sonnet); got != 40000 {
		t.Fatalf("EstAt(sonnet) = %d, want 40000", got)
	}
}

// TestParseEstTokensScalarHaikuRounds is a regression test for the
// float-truncation bug in decodeEstTokens' scalar path. Before the fix,
// per-tier estimates were computed as int(float64(base) * ratio); when the
// float product of base and the tier CostMult ratio landed a hair below the
// integer, int() truncated toward zero and silently returned one token fewer
// than the ratio implied. base 33333 * Haiku ratio 0.12 = 3999.96, which
// int() floored to 3999; math.Round now returns 4000. This is the same
// float-truncation class the v0.6.0 parseBudget fix closed, mirrored onto
// per-tier est_tokens scaling. Sonnet (33333 * 0.40 = 13333.2) is unaffected
// by the off-by-one on this base, so it is asserted only for stability.
func TestParseEstTokensScalarHaikuRounds(t *testing.T) {
	root := mustParse(t, `
root:
  id: haiku-round
  value: 5
  tiers: [opus, sonnet, haiku]
  est_tokens: 33333
`)
	if got := root.EstAt(tier.Opus); got != 33333 {
		t.Fatalf("EstAt(opus) = %d, want 33333", got)
	}
	if got := root.EstAt(tier.Sonnet); got != 13333 {
		t.Fatalf("EstAt(sonnet) = %d, want 13333", got)
	}
	if got := root.EstAt(tier.Haiku); got != 4000 {
		t.Fatalf("EstAt(haiku) = %d, want 4000 (math.Round of 3999.96)", got)
	}
}

// TestParseEstTokensMapInferredHaikuRounds locks in the same math.Round fix on
// the inferEstimate path (the fill loop that derives a tier missing from the
// est_tokens map from the nearest higher tier). With opus quoted at 33333 and
// sonnet/haiku omitted, the fill infers sonnet (33333 * 0.40 = 13333) and then
// haiku from sonnet (13333 * 0.30 = 3999.9), which int() floored to 3999;
// math.Round now returns 4000. The opus->sonnet->haiku fill would otherwise
// compound the one-token loss.
func TestParseEstTokensMapInferredHaikuRounds(t *testing.T) {
	root := mustParse(t, `
root:
  id: inferred-haiku
  value: 5
  tiers: [opus, sonnet, haiku]
  est_tokens:
    opus: 33333
`)
	if got := root.EstAt(tier.Opus); got != 33333 {
		t.Fatalf("EstAt(opus) = %d, want 33333", got)
	}
	if got := root.EstAt(tier.Haiku); got != 4000 {
		t.Fatalf("EstAt(haiku) = %d, want 4000 (math.Round of 3999.9 via sonnet)", got)
	}
}
