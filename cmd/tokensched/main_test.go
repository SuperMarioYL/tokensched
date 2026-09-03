package main

import (
	"encoding/json"
	"os"
	"strings"
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

// TestVersionLockstep is the single-source-of-truth version guard: the version
// var, the VERSION file, web/site.json content_version, and the CHANGELOG head
// entry must all agree. This test fails on any tag where a version surface was
// bumped without bumping the others — e.g. on the shipped v0.7.0 tag the
// version var was v0.4.0-dev (3 minors behind), the VERSION file was 0.7.0, and
// site.json was v0.6.0 (one minor behind). The v prefix is normalized away so
// "v0.8.0" and "0.8.0" compare equal.
func TestVersionLockstep(t *testing.T) {
	want := strings.TrimPrefix(version, "v")

	// VERSION file at the repo root (../../VERSION relative to cmd/tokensched/).
	raw, err := os.ReadFile("../../VERSION")
	if err != nil {
		t.Fatalf("read VERSION file: %v", err)
	}
	fileVer := strings.TrimSpace(string(raw))
	if fileVer != want {
		t.Errorf("VERSION file = %q, want %q (version var = %q)", fileVer, want, version)
	}

	// web/site.json content_version.
	siteBytes, err := os.ReadFile("../../web/site.json")
	if err != nil {
		t.Fatalf("read web/site.json: %v", err)
	}
	var sj struct {
		ContentVersion string `json:"content_version"`
	}
	if err := json.Unmarshal(siteBytes, &sj); err != nil {
		t.Fatalf("parse web/site.json: %v", err)
	}
	siteVer := strings.TrimPrefix(sj.ContentVersion, "v")
	if siteVer != want {
		t.Errorf("site.json content_version = %q, want %q", sj.ContentVersion, want)
	}

	// CHANGELOG.md head entry (## [X.Y.Z] - ...).
	changelogBytes, err := os.ReadFile("../../CHANGELOG.md")
	if err != nil {
		t.Fatalf("read CHANGELOG.md: %v", err)
	}
	changelogVer := changelogHeadVersion(changelogBytes)
	if changelogVer != want {
		t.Errorf("CHANGELOG head version = %q, want %q", changelogVer, want)
	}
}

// changelogHeadVersion extracts the version from the first "## [X.Y.Z]" line.
func changelogHeadVersion(data []byte) string {
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "## [") {
			rest := strings.TrimPrefix(line, "## [")
			if idx := strings.Index(rest, "]"); idx >= 0 {
				return rest[:idx]
			}
		}
	}
	return ""
}
