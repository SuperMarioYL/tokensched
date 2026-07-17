// Package report renders allocation decisions and the hard-truncation vs
// scheduled-yield comparison to a terminal using lipgloss.
package report

import (
	"fmt"
	"strings"

	"github.com/SuperMarioYL/tokensched/internal/budget"
	"github.com/SuperMarioYL/tokensched/internal/sim"
	"github.com/SuperMarioYL/tokensched/internal/tasktree"
	"github.com/SuperMarioYL/tokensched/internal/tier"
	"github.com/charmbracelet/lipgloss"
)

var (
	title    = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#F2B705"))
	subtle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#8A8A8A"))
	keepClr  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3CB371"))
	downClr  = lipgloss.NewStyle().Foreground(lipgloss.Color("#F2B705"))
	preClr   = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5484D"))
	header   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#7AA2F7"))
	good     = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#3CB371"))
	bad      = lipgloss.NewStyle().Foreground(lipgloss.Color("#E5484D"))
	boxStyle = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
)

func actionStyle(a budget.Action) lipgloss.Style {
	switch a {
	case budget.Keep:
		return keepClr
	case budget.DownTier:
		return downClr
	default:
		return preClr
	}
}

// Tree renders a task tree with each leaf's initial (top-tier) budget. Used by
// `tokensched plan`.
func Tree(root *tasktree.Task, budgetTokens int) string {
	var b strings.Builder
	b.WriteString(title.Render(fmt.Sprintf("Task tree — %s", root.ID)) + "\n")
	if budgetTokens > 0 {
		b.WriteString(subtle.Render(fmt.Sprintf("budget: %s tokens", humanInt(budgetTokens))) + "\n")
	}
	b.WriteString("\n")

	var walk func(t *tasktree.Task, prefix string, last bool, isRoot bool)
	walk = func(t *tasktree.Task, prefix string, last bool, isRoot bool) {
		connector := ""
		childPrefix := prefix
		if !isRoot {
			if last {
				connector = "└─ "
				childPrefix = prefix + "   "
			} else {
				connector = "├─ "
				childPrefix = prefix + "│  "
			}
		}

		line := prefix + connector + lipgloss.NewStyle().Bold(true).Render(t.ID)
		if len(t.Children) == 0 {
			top := t.HighestTier()
			est := t.EstAt(top)
			line += subtle.Render(fmt.Sprintf("  value=%g  tiers=[%s]  init=%s@%s tok",
				t.Value, tierList(t.Tiers), top, humanInt(est)))
		}
		b.WriteString(line + "\n")

		for i, c := range t.Children {
			walk(c, childPrefix, i == len(t.Children)-1, false)
		}
	}
	walk(root, "", true, true)

	// Per-node initial budget summary.
	leaves := tasktree.Leaves(root)
	want := 0
	for _, l := range leaves {
		want += l.EstAt(l.HighestTier())
	}
	b.WriteString("\n")
	b.WriteString(subtle.Render(fmt.Sprintf("%d leaf tasks · naive all-top-tier demand = %s tokens",
		len(leaves), humanInt(want))))
	if budgetTokens > 0 {
		over := want - budgetTokens
		if over > 0 {
			b.WriteString("  " + bad.Render(fmt.Sprintf("(overruns budget by %s)", humanInt(over))))
		} else {
			b.WriteString("  " + good.Render(fmt.Sprintf("(fits budget, headroom %s)", humanInt(budgetTokens-want))))
		}
	}
	b.WriteString("\n")
	return b.String()
}

// Decisions renders the per-task allocation decisions as a table.
func Decisions(ds []budget.Decision) string {
	var b strings.Builder
	b.WriteString(header.Render("Allocation decisions") + "\n")

	rows := [][]string{{"TASK", "ACTION", "TIER", "BUDGET", "VALUE", "REASON"}}
	for _, d := range ds {
		tierStr := d.Tier.String()
		if d.Action == budget.Preempt {
			tierStr = "—"
		}
		rows = append(rows, []string{
			d.TaskID,
			d.Action.String(),
			tierStr,
			humanInt(d.Budget),
			fmt.Sprintf("%.1f", d.Value),
			truncate(d.Reason, 52),
		})
	}
	b.WriteString(renderTable(rows, func(rowIdx int) lipgloss.Style {
		if rowIdx == 0 {
			return header
		}
		return actionStyle(ds[rowIdx-1].Action)
	}))
	return b.String()
}

// Comparison renders the headline "hard-truncation vs scheduled-yield" table.
func Comparison(c sim.Comparison) string {
	var b strings.Builder
	b.WriteString(title.Render("Hard-truncation vs scheduled-yield") + "\n")
	// Surface overrun vs headroom honestly on both surfaces (v0.4.0): when the
	// naive plan fits, say so and report the headroom instead of clamping a
	// negative overrun to 0 and saying nothing.
	var overLine string
	switch {
	case c.Overrun > 0:
		overLine = fmt.Sprintf("naive plan overruns by %s tokens", humanInt(c.Overrun))
	case c.Headroom > 0:
		overLine = subtle.Render(fmt.Sprintf("naive plan fits — headroom %s tokens", humanInt(c.Headroom)))
	default:
		overLine = "naive plan fits the budget exactly"
	}
	b.WriteString(subtle.Render(fmt.Sprintf("budget %s tokens · %d leaf tasks · %s",
		humanInt(c.Budget), c.NaiveCount, overLine)) + "\n\n")

	rows := [][]string{
		{"METRIC", "NAIVE HARD-TRUNCATION", "TOKENSCHED SCHEDULED"},
		{"tokens spent", humanInt(c.Naive.Spent), humanInt(c.Scheduled.Spent)},
		{"tasks completed", fmt.Sprintf("%d / %d", c.Naive.Completed, c.NaiveCount), fmt.Sprintf("%d / %d", c.Scheduled.Completed, c.NaiveCount)},
		{"tasks cut/preempted", fmt.Sprintf("%d", c.Naive.Truncated), fmt.Sprintf("%d", c.Scheduled.Truncated)},
		{"realised value", fmt.Sprintf("%.1f", c.Naive.Value), fmt.Sprintf("%.1f", c.Scheduled.Value)},
	}
	b.WriteString(renderTable(rows, func(rowIdx int) lipgloss.Style {
		if rowIdx == 0 {
			return header
		}
		return lipgloss.NewStyle()
	}))

	b.WriteString("\n")
	summary := fmt.Sprintf("scheduled-yield saved %d more task(s) and +%.1f realised value within the same %s-token budget",
		c.TasksSaved, c.ValueSaved, humanInt(c.Budget))
	if c.TasksSaved > 0 || c.ValueSaved > 0 {
		b.WriteString(good.Render("✓ " + summary))
	} else {
		b.WriteString(subtle.Render(summary))
	}
	b.WriteString("\n")
	return b.String()
}

// Full renders the complete `run` report: comparison table, then the scheduled
// per-task decisions.
func Full(c sim.Comparison) string {
	var b strings.Builder
	b.WriteString(Comparison(c))
	b.WriteString("\n")
	b.WriteString(Decisions(c.Scheduled.Decisions))
	return b.String()
}

// renderTable lays out a fixed-width table with per-row styling.
func renderTable(rows [][]string, styleFor func(rowIdx int) lipgloss.Style) string {
	if len(rows) == 0 {
		return ""
	}
	cols := len(rows[0])
	widths := make([]int, cols)
	for _, r := range rows {
		for i := 0; i < cols && i < len(r); i++ {
			if w := lipgloss.Width(r[i]); w > widths[i] {
				widths[i] = w
			}
		}
	}

	var inner strings.Builder
	for ri, r := range rows {
		st := styleFor(ri)
		var cells []string
		for i := 0; i < cols; i++ {
			cell := ""
			if i < len(r) {
				cell = r[i]
			}
			pad := widths[i] - lipgloss.Width(cell)
			if pad < 0 {
				pad = 0
			}
			cells = append(cells, st.Render(cell)+strings.Repeat(" ", pad))
		}
		inner.WriteString(strings.Join(cells, "  "))
		if ri != len(rows)-1 {
			inner.WriteString("\n")
		}
	}
	return boxStyle.Render(inner.String()) + "\n"
}

func tierList(ts []tier.Tier) string {
	parts := make([]string, len(ts))
	for i, t := range ts {
		parts[i] = t.String()
	}
	return strings.Join(parts, ",")
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	if n <= 1 {
		return s[:n]
	}
	return s[:n-1] + "…"
}

// humanInt renders large token counts compactly (e.g. 200000 -> 200k).
func humanInt(n int) string {
	if n >= 1000 && n%1000 == 0 {
		return fmt.Sprintf("%dk", n/1000)
	}
	if n >= 10000 {
		return fmt.Sprintf("%.1fk", float64(n)/1000)
	}
	return fmt.Sprintf("%d", n)
}
