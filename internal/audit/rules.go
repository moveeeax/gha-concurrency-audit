package audit

import (
	"fmt"
	"strings"
)

// Severity orders findings. --fail-on compares against it.
type Severity int

const (
	SevNever Severity = iota
	SevLow
	SevMedium
	SevHigh
)

func (s Severity) String() string {
	switch s {
	case SevHigh:
		return "high"
	case SevMedium:
		return "medium"
	case SevLow:
		return "low"
	}
	return "never"
}

// ParseSeverity accepts the --fail-on vocabulary.
func ParseSeverity(s string) (Severity, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "high":
		return SevHigh, nil
	case "medium", "med":
		return SevMedium, nil
	case "low":
		return SevLow, nil
	case "never", "none", "off":
		return SevNever, nil
	}
	return SevNever, fmt.Errorf("unknown severity %q (want high, medium, low or never)", s)
}

// Rule identifiers, also the --ignore vocabulary.
const (
	RuleMissingDeployConcurrency = "MISSING_DEPLOY_CONCURRENCY"
	RuleDeployCancelInProgress   = "DEPLOY_CANCEL_IN_PROGRESS"
	RuleMissingPRCancel          = "MISSING_PR_CANCEL"
	RuleGroupTooCoarse           = "GROUP_TOO_COARSE"
	RuleGroupTooFine             = "GROUP_TOO_FINE"
)

// AllRules is every rule this version knows, in reporting order.
var AllRules = []string{
	RuleMissingDeployConcurrency,
	RuleDeployCancelInProgress,
	RuleMissingPRCancel,
	RuleGroupTooCoarse,
	RuleGroupTooFine,
}

// Finding is one problem in one workflow, with the block to paste instead.
type Finding struct {
	Workflow   string `json:"workflow"`
	Name       string `json:"name"`
	Class      Class  `json:"class"`
	Rule       string `json:"rule"`
	Severity   string `json:"severity"`
	Note       string `json:"note"`
	Suggestion string `json:"suggestion"`

	severity Severity
}

// Sev exposes the parsed severity for exit-code comparison.
func (f Finding) Sev() Severity { return f.severity }

// scopingTokens are the interpolations that make a group narrow enough that
// unrelated refs do not queue behind each other.
var scopingTokens = []string{
	"github.ref",
	"github.ref_name",
	"github.head_ref",
	"github.base_ref",
	"github.event.number",
	"github.event.pull_request",
	"github.event.issue.number",
	"inputs.",
	"matrix.",
	"environment",
}

// uniqueTokens make a group collide with nothing, which makes the block
// decorative.
var uniqueTokens = []string{
	"github.sha",
	"github.run_id",
	"github.run_number",
	"github.run_attempt",
	"github.event.pull_request.head.sha",
}

func containsAny(s string, tokens []string) bool {
	low := strings.ToLower(s)
	for _, t := range tokens {
		if strings.Contains(low, t) {
			return true
		}
	}
	return false
}

// Analyze runs every rule that is not ignored over the parsed workflows.
func Analyze(workflows []Workflow, ignore map[string]bool) []Finding {
	var out []Finding
	add := func(w Workflow, rule string, sev Severity, note, suggestion string) {
		if ignore[rule] {
			return
		}
		out = append(out, Finding{
			Workflow:   w.Path,
			Name:       w.Name,
			Class:      w.Class,
			Rule:       rule,
			Severity:   sev.String(),
			Note:       note,
			Suggestion: suggestion,
			severity:   sev,
		})
	}

	for _, w := range workflows {
		eff := w.Effective()
		suggestion := Suggest(w)

		switch w.Class {
		case ClassDeploy:
			if !eff.Present {
				add(w, RuleMissingDeployConcurrency, SevHigh,
					"deploy workflow has no concurrency block, so two pushes can apply at once and the slower run wins",
					suggestion)
			} else if eff.Cancel {
				what := "cancel-in-progress is true"
				if eff.CancelIsExpr {
					what = "cancel-in-progress is an expression that can evaluate true"
				}
				add(w, RuleDeployCancelInProgress, SevHigh,
					what+"; cancelling mid-apply leaves half-applied state, queue instead",
					suggestion)
			}
		case ClassPRCheck:
			if !eff.Present || !eff.Cancel {
				add(w, RuleMissingPRCancel, SevMedium,
					fmt.Sprintf("superseded pushes keep running: %s", wastedNote(w)),
					suggestion)
			}
		}

		if !eff.Present || eff.Group == "" {
			continue
		}
		switch {
		case containsAny(eff.Group, uniqueTokens):
			add(w, RuleGroupTooFine, SevLow,
				fmt.Sprintf("group %q is unique per run, so it can never collide and the block does nothing", eff.Group),
				suggestion)
		case !containsAny(eff.Group, scopingTokens) && coarseApplies(w):
			add(w, RuleGroupTooCoarse, SevMedium,
				fmt.Sprintf("group %q has no ref, head_ref or environment interpolation, so unrelated refs serialise behind one queue", eff.Group),
				suggestion)
		}
	}
	return out
}

// coarseApplies keeps GROUP_TOO_COARSE off deploys that legitimately want one
// global queue. A deploy that targets several environments is the exception:
// there a single queue makes staging wait for production.
func coarseApplies(w Workflow) bool {
	if w.Class != ClassDeploy {
		return true
	}
	return len(w.Environments()) > 1
}

// Assumed cost of one superseded pull-request run. Rough on purpose: the point
// is the order of magnitude, and the stretch goal is to measure it from the
// Actions API instead.
const (
	assumedMinutesPerJob     = 5
	assumedSupersededPerWeek = 3
)

func wastedNote(w Workflow) string {
	jobs := len(w.Jobs)
	if jobs == 0 {
		jobs = 1
	}
	est := jobs * assumedMinutesPerJob * assumedSupersededPerWeek
	return fmt.Sprintf("~%d runner-min/week (%d jobs x %d min x %d superseded pushes/week, estimated)",
		est, jobs, assumedMinutesPerJob, assumedSupersededPerWeek)
}

// Suggest renders the concurrency block this workflow's class should carry.
func Suggest(w Workflow) string {
	group, cancel := "${{ github.workflow }}-${{ github.ref }}", "false"
	switch w.Class {
	case ClassDeploy:
		if envs := w.Environments(); len(envs) > 0 {
			group = "deploy-" + envs[0]
			if len(envs) > 1 {
				group = "deploy-${{ github.workflow }}-${{ matrix.environment }}"
			}
		}
		cancel = "false"
	case ClassPRCheck:
		group = "${{ github.workflow }}-${{ github.head_ref || github.ref }}"
		cancel = "true"
	}
	return fmt.Sprintf("concurrency:\n  group: %s\n  cancel-in-progress: %s", group, cancel)
}

// ParseIgnore turns a comma-separated --ignore value into a lookup set and
// rejects names that are not rules, so a typo does not silently ignore nothing.
func ParseIgnore(s string) (map[string]bool, error) {
	out := map[string]bool{}
	for _, part := range strings.Split(s, ",") {
		name := strings.ToUpper(strings.TrimSpace(part))
		if name == "" {
			continue
		}
		known := false
		for _, r := range AllRules {
			if r == name {
				known = true
				break
			}
		}
		if !known {
			return nil, fmt.Errorf("unknown rule %q (known: %s)", name, strings.Join(AllRules, ", "))
		}
		out[name] = true
	}
	return out, nil
}

// Worst returns the highest severity among findings.
func Worst(findings []Finding) Severity {
	worst := SevNever
	for _, f := range findings {
		if f.severity > worst {
			worst = f.severity
		}
	}
	return worst
}
