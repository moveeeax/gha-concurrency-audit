package audit

import "regexp"

// Class is what a workflow is for. It decides what "correct concurrency" means:
// a deploy wants to queue, a pull-request check wants to cancel, a nightly
// cron usually wants neither.
type Class string

const (
	ClassDeploy    Class = "deploy"
	ClassPRCheck   Class = "pr-check"
	ClassScheduled Class = "scheduled"
	ClassOther     Class = "other"
)

// deployPattern matches step text that writes to something outside the runner.
// Bare verbs (deploy, rollout, release, publish) are matched as words; `apply`
// and `up` are too common on their own, so they need their tool in front.
var deployPattern = regexp.MustCompile(`(?i)(\b(deploy|deployment|rollout|release|publish)\b|` +
	`\b(terraform|tofu|terragrunt)\s+apply\b|` +
	`\bkubectl\s+(apply|rollout|set\s+image)\b|` +
	`\bhelm\s+(upgrade|install)\b|` +
	`\bkustomize\s+build\b.*\bkubectl\b|` +
	`\bpulumi\s+up\b|` +
	`\bserverless\s+deploy\b|` +
	`\baws\s+(deploy|s3\s+sync|cloudformation\s+deploy)\b|` +
	`\b(npm|cargo|twine|gem)\s+publish\b|` +
	`\bdocker\s+push\b)`)

func classify(w *Workflow) Class {
	for _, j := range w.Jobs {
		if j.Environment != "" {
			return ClassDeploy
		}
	}
	for _, j := range w.Jobs {
		if deployPattern.MatchString(j.text) {
			return ClassDeploy
		}
	}
	if w.HasEvent("pull_request", "pull_request_target") {
		return ClassPRCheck
	}
	if len(w.Events) == 1 && w.HasEvent("schedule") {
		return ClassScheduled
	}
	return ClassOther
}

// Environments lists the distinct environment names referenced by the jobs,
// in job order.
func (w *Workflow) Environments() []string {
	var out []string
	seen := map[string]bool{}
	for _, j := range w.Jobs {
		if j.Environment == "" || seen[j.Environment] {
			continue
		}
		seen[j.Environment] = true
		out = append(out, j.Environment)
	}
	return out
}
