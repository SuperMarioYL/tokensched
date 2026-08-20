package main

import (
	"testing"
)

// TestParseBudgetDecimalSuffixRound is a regression test for the
// float-truncation bug in parseBudget. Before the fix, parseBudget computed
// the budget as int(f * mult); when the nearest float64 to a decimal input
// sat just below the true decimal (e.g. 4.1 -> 4.0999999999999996), the
// product could land a hair under the integer and int() truncated toward
// zero, silently returning one token fewer than the user typed with no
// error. The decimal-suffixed inputs in the first block are the ones that
// exercised the bug: under the old int(f * mult) they returned exactly one
// less (4.1m -> 4099999, 8.2m -> 8199999, 32.3k -> 32299); math.Round now
// returns the exact value the user typed. The second block holds inputs
// whose float product already lands on the integer, so the fix is a no-op
// for them (4.1k rounds exactly to 4100 under both paths).
func TestParseBudgetDecimalSuffixRound(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		// Decimal-suffixed inputs that exercised the off-by-one truncation.
		{"4.1m", 4_100_000},
		{"8.2m", 8_200_000},
		{"32.3k", 32_300},
		// Inputs whose float product already lands on the integer; the fix
		// must not change them.
		{"4.1k", 4_100},
		{"200k", 200_000},
		{"1.5m", 1_500_000},
		{"200000", 200_000},
		{"1.5M", 1_500_000},
	}
	for _, c := range cases {
		got, err := parseBudget(c.in)
		if err != nil {
			t.Fatalf("parseBudget(%q) returned unexpected error: %v", c.in, err)
		}
		if got != c.want {
			t.Errorf("parseBudget(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

// TestParseBudgetErrors guards the error paths: empty, whitespace, and
// non-numeric or unknown-suffix inputs must fail instead of being silently
// coerced into a budget.
func TestParseBudgetErrors(t *testing.T) {
	for _, in := range []string{"", "   ", "abc", "1.5x", "--", "x"} {
		if _, err := parseBudget(in); err == nil {
			t.Errorf("parseBudget(%q) want error, got nil", in)
		}
	}
}
