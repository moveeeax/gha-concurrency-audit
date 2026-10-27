package audit

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Report is what the JSON format emits and what the human formats render.
type Report struct {
	Dir       string         `json:"dir"`
	Workflows []WorkflowInfo `json:"workflows"`
	Findings  []Finding      `json:"findings"`
	Summary   map[string]int `json:"summary"`
}

// WorkflowInfo is the per-file inventory, useful on its own: it shows what the
// tool thought each workflow was for.
type WorkflowInfo struct {
	Path        string   `json:"path"`
	Name        string   `json:"name"`
	Class       Class    `json:"class"`
	Events      []string `json:"events"`
	Concurrency string   `json:"concurrency"`
}

// NewReport pairs the workflow inventory with the findings.
func NewReport(dir string, workflows []Workflow, findings []Finding) Report {
	r := Report{
		Dir:      dir,
		Findings: findings,
		Summary:  map[string]int{"high": 0, "medium": 0, "low": 0},
	}
	for _, w := range workflows {
		eff := w.Effective()
		desc := "none"
		if eff.Present {
			desc = eff.Group
			if desc == "" {
				desc = "(no group)"
			}
			if eff.Cancel {
				desc += " [cancels]"
			} else {
				desc += " [queues]"
			}
		}
		r.Workflows = append(r.Workflows, WorkflowInfo{
			Path:        w.Path,
			Name:        w.Name,
			Class:       w.Class,
			Events:      w.Events,
			Concurrency: desc,
		})
	}
	for _, f := range findings {
		r.Summary[f.Severity]++
	}
	if r.Findings == nil {
		r.Findings = []Finding{}
	}
	if r.Workflows == nil {
		r.Workflows = []WorkflowInfo{}
	}
	return r
}

// Write renders the report in one of table, json or markdown.
func (r Report) Write(w io.Writer, format string) error {
	switch strings.ToLower(format) {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(r)
	case "markdown", "md":
		return r.writeMarkdown(w)
	case "table", "":
		return r.writeTable(w)
	}
	return fmt.Errorf("unknown format %q (want table, json or markdown)", format)
}

func (r Report) writeTable(out io.Writer) error {
	if len(r.Workflows) == 0 {
		fmt.Fprintf(out, "no workflows found under %s/.github/workflows\n", r.Dir)
		return nil
	}

	if len(r.Findings) == 0 {
		fmt.Fprintf(out, "%d workflow(s) scanned, no concurrency findings.\n", len(r.Workflows))
		return nil
	}

	tw := tabwriter.NewWriter(out, 0, 2, 2, ' ', 0)
	fmt.Fprintln(tw, "SEVERITY\tRULE\tCLASS\tWORKFLOW\tNOTE")
	for _, f := range r.Findings {
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", f.Severity, f.Rule, f.Class, f.Workflow, f.Note)
	}
	if err := tw.Flush(); err != nil {
		return err
	}

	fmt.Fprintf(out, "\n%d workflow(s), %d finding(s): %d high, %d medium, %d low\n",
		len(r.Workflows), len(r.Findings), r.Summary["high"], r.Summary["medium"], r.Summary["low"])

	fmt.Fprintln(out, "\nSuggested blocks")
	for _, f := range dedupeByWorkflow(r.Findings) {
		fmt.Fprintf(out, "\n%s (%s)\n", f.Workflow, f.Class)
		for _, line := range strings.Split(f.Suggestion, "\n") {
			fmt.Fprintf(out, "  %s\n", line)
		}
	}
	return nil
}

func (r Report) writeMarkdown(out io.Writer) error {
	fmt.Fprintln(out, "## gha-concurrency-audit")
	fmt.Fprintln(out)

	if len(r.Workflows) == 0 {
		fmt.Fprintf(out, "No workflows found under `%s/.github/workflows`.\n", r.Dir)
		return nil
	}
	if len(r.Findings) == 0 {
		fmt.Fprintf(out, "%d workflow(s) scanned, no concurrency findings.\n", len(r.Workflows))
		return nil
	}

	fmt.Fprintf(out, "%d workflow(s), **%d finding(s)**: %d high, %d medium, %d low\n\n",
		len(r.Workflows), len(r.Findings), r.Summary["high"], r.Summary["medium"], r.Summary["low"])

	fmt.Fprintln(out, "| Severity | Rule | Class | Workflow | Note |")
	fmt.Fprintln(out, "| --- | --- | --- | --- | --- |")
	for _, f := range r.Findings {
		fmt.Fprintf(out, "| %s | `%s` | %s | `%s` | %s |\n",
			f.Severity, f.Rule, f.Class, f.Workflow, escapePipes(f.Note))
	}

	fmt.Fprintln(out, "\n### Suggested blocks")
	for _, f := range dedupeByWorkflow(r.Findings) {
		fmt.Fprintf(out, "\n`%s` (%s)\n\n```yaml\n%s\n```\n", f.Workflow, f.Class, f.Suggestion)
	}
	return nil
}

func escapePipes(s string) string { return strings.ReplaceAll(s, "|", `\|`) }

// dedupeByWorkflow keeps one suggestion per workflow: the suggested block is a
// property of the workflow's class, so repeating it per finding is noise.
func dedupeByWorkflow(findings []Finding) []Finding {
	seen := map[string]bool{}
	var out []Finding
	for _, f := range findings {
		if seen[f.Workflow] {
			continue
		}
		seen[f.Workflow] = true
		out = append(out, f)
	}
	return out
}
