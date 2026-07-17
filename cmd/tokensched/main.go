// Command tokensched is a CPU-style scheduler for an agent's token budget. It
// parses a YAML task tree, allocates a fixed token budget across the sub-tasks
// by expected value-per-token, and replays a naive hard-truncation run against
// the scheduled run so you can see what survives.
//
// No network, no daemon: everything is plan-from-tasktree + replay.
package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/SuperMarioYL/tokensched/internal/policy"
	"github.com/SuperMarioYL/tokensched/internal/report"
	"github.com/SuperMarioYL/tokensched/internal/sim"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/spf13/cobra"
)

// version is overridable at build time with
// -ldflags "-X main.version=vX.Y.Z".
var version = "v0.4.0-dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "tokensched",
		Short:         "A CPU-style scheduler for an agent's token budget",
		Long:          "TokenSched pre-allocates a fixed token budget across a task tree by value-per-token,\npredicts overrun, and down-tiers (Opus->Sonnet->Haiku) or preempts low-value sub-tasks\nbefore the budget window blows — turning hard truncation into a schedulable soft yield.",
		SilenceUsage:  true,
		SilenceErrors: true,
	}
	root.AddCommand(newPlanCmd(), newRunCmd(), newVersionCmd())
	return root
}

func newPlanCmd() *cobra.Command {
	var budgetStr string
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "plan <tree.yaml>",
		Short: "Parse a task tree and print it with each node's initial budget",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := tasktree.LoadFile(args[0])
			if err != nil {
				return err
			}
			budgetTokens := 0
			if strings.TrimSpace(budgetStr) != "" {
				budgetTokens, err = parseBudget(budgetStr)
				if err != nil {
					return err
				}
			}
			if asJSON {
				out, err := report.TreeJSON(root, budgetTokens)
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), out)
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), report.Tree(root, budgetTokens))
			return nil
		},
	}
	cmd.Flags().StringVar(&budgetStr, "budget", "", "optional token budget to compare against (e.g. 200k)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON instead of the terminal report")
	return cmd
}

func newRunCmd() *cobra.Command {
	var budgetStr string
	var asJSON bool
	var policyPath string
	cmd := &cobra.Command{
		Use:   "run <tree.yaml> --budget <N>",
		Short: "Replay naive hard-truncation vs scheduled execution under a budget",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			root, err := tasktree.LoadFile(args[0])
			if err != nil {
				return err
			}
			if strings.TrimSpace(budgetStr) == "" {
				return fmt.Errorf("--budget is required (e.g. --budget 200k)")
			}
			budgetTokens, err := parseBudget(budgetStr)
			if err != nil {
				return err
			}
			if budgetTokens <= 0 {
				return fmt.Errorf("--budget must be positive")
			}
			// Load the pluggable preemption policy. With no --policy this is the
			// no-op default (nil hook) == the pre-v0.3.0 behaviour.
			pol := policy.Default()
			if strings.TrimSpace(policyPath) != "" {
				pol, err = policy.LoadFile(policyPath)
				if err != nil {
					return err
				}
			}
			// Replay with the full policy options so BOTH knobs take effect: the
			// preemption hook (preempt_below_value) and the PreferDownTier flag
			// (prefer_downtier:false => preempt-before-down-tier, v0.4.0).
			cmp := sim.ReplayWithOpts(root, budgetTokens, pol.ScheduleOptions())
			if asJSON {
				out, err := report.FullJSONWithPolicy(cmp, report.EffectivePolicy{
					Source:            policyOrDefault(policyPath),
					PreemptBelowValue: pol.PreemptBelowValue,
					PreferDownTier:    pol.PreferDownTier,
					Active:            pol.Active(),
				})
				if err != nil {
					return err
				}
				fmt.Fprint(cmd.OutOrStdout(), out)
				return nil
			}
			fmt.Fprint(cmd.OutOrStdout(), report.Full(cmp))
			return nil
		},
	}
	cmd.Flags().StringVar(&budgetStr, "budget", "", "token budget for the run (e.g. 200k, 1.5m, 200000)")
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit machine-readable JSON instead of the terminal report")
	cmd.Flags().StringVar(&policyPath, "policy", "", "path to a scheduling policy file (preemption block; e.g. policy.example.yaml)")
	return cmd
}

// policyOrDefault reports the policy source for the --json document: the file
// path when --policy was given, else "default".
func policyOrDefault(path string) string {
	if strings.TrimSpace(path) == "" {
		return "default"
	}
	return path
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tokensched version",
		Args:  cobra.NoArgs,
		Run: func(cmd *cobra.Command, _ []string) {
			fmt.Fprintf(cmd.OutOrStdout(), "tokensched %s\n", version)
		},
	}
}

// parseBudget accepts a plain integer or a suffixed value: k/K = thousand,
// m/M = million. Decimal suffixed values (e.g. 1.5m) are allowed.
func parseBudget(s string) (int, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return 0, fmt.Errorf("empty budget")
	}
	mult := 1.0
	switch {
	case strings.HasSuffix(s, "k"):
		mult = 1_000
		s = strings.TrimSuffix(s, "k")
	case strings.HasSuffix(s, "m"):
		mult = 1_000_000
		s = strings.TrimSuffix(s, "m")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid budget %q: want an integer or k/m-suffixed number (e.g. 200k)", s)
	}
	return int(f * mult), nil
}
