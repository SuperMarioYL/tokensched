// Package tier models the model tiers a sub-task can run on (Opus, Sonnet,
// Haiku) together with their relative cost and capability coefficients.
//
// The allocator/scheduler never talks to a live API; it reasons purely over
// these coefficients. CostMult scales the token spend of running a task on a
// tier (Haiku is cheaper than Opus); CapMult scales how much of a task's
// declared value is actually realised when it runs on that tier (a low-tier
// model recovers less of the value). Both are relative to Opus = 1.0.
package tier

import (
	"fmt"
	"strings"
)

// Tier identifies a model capability/cost band.
type Tier int

const (
	// Unknown is the zero value and is never a valid allocation target.
	Unknown Tier = iota
	// Haiku is the cheapest, least capable tier.
	Haiku
	// Sonnet is the mid tier.
	Sonnet
	// Opus is the most capable, most expensive tier.
	Opus
)

// Ordered lists tiers from highest capability to lowest. Down-tiering walks
// this slice left-to-right (Opus -> Sonnet -> Haiku).
var Ordered = []Tier{Opus, Sonnet, Haiku}

// String renders the canonical lowercase name used in YAML and reports.
func (t Tier) String() string {
	switch t {
	case Opus:
		return "opus"
	case Sonnet:
		return "sonnet"
	case Haiku:
		return "haiku"
	default:
		return "unknown"
	}
}

// Rank returns a comparable capability rank (Opus highest). Higher is more
// capable. Unknown ranks below every real tier.
func (t Tier) Rank() int {
	switch t {
	case Opus:
		return 3
	case Sonnet:
		return 2
	case Haiku:
		return 1
	default:
		return 0
	}
}

// CostMult is the token-cost coefficient relative to Opus (Opus = 1.0).
// A task's estimated tokens are quoted at the Opus baseline; running it on a
// cheaper tier multiplies the spend down.
func (t Tier) CostMult() float64 {
	switch t {
	case Opus:
		return 1.0
	case Sonnet:
		return 0.40
	case Haiku:
		return 0.12
	default:
		return 1.0
	}
}

// CapMult is the capability coefficient relative to Opus (Opus = 1.0): the
// fraction of a task's declared value that is realised on this tier.
func (t Tier) CapMult() float64 {
	switch t {
	case Opus:
		return 1.0
	case Sonnet:
		return 0.75
	case Haiku:
		return 0.45
	default:
		return 0.0
	}
}

// Parse converts a tier name (case-insensitive) to a Tier. It accepts the
// canonical names and a couple of common aliases.
func Parse(s string) (Tier, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "opus":
		return Opus, nil
	case "sonnet":
		return Sonnet, nil
	case "haiku":
		return Haiku, nil
	default:
		return Unknown, fmt.Errorf("tier: unknown tier %q (want opus|sonnet|haiku)", s)
	}
}

// Lower returns the next tier down from t (Opus->Sonnet->Haiku), restricted to
// the allowed set. It returns (Unknown, false) when there is no lower allowed
// tier. allowed is the set of tiers a task is eligible for.
func Lower(t Tier, allowed []Tier) (Tier, bool) {
	allow := make(map[Tier]bool, len(allowed))
	for _, a := range allowed {
		allow[a] = true
	}
	// Walk Ordered (high->low) and return the first allowed tier strictly
	// below t.
	for _, cand := range Ordered {
		if cand.Rank() < t.Rank() && allow[cand] {
			return cand, true
		}
	}
	return Unknown, false
}

// Highest returns the most capable tier in allowed, or Unknown if empty.
func Highest(allowed []Tier) Tier {
	best := Unknown
	for _, a := range allowed {
		if a.Rank() > best.Rank() {
			best = a
		}
	}
	return best
}
