package audit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func load(t *testing.T, dir string) []Workflow {
	t.Helper()
	ws, err := Load(dir)
	if err != nil {
		t.Fatalf("Load(%s): %v", dir, err)
	}
	return ws
}

func analyze(t *testing.T, dir string, ignore ...string) []Finding {
	t.Helper()
	set := map[string]bool{}
	for _, r := range ignore {
		set[r] = true
	}
	return Analyze(load(t, dir), set)
}

func findingFor(findings []Finding, file string) []Finding {
	var out []Finding
	for _, f := range findings {
		if strings.HasSuffix(f.Workflow, file) {
			out = append(out, f)
		}
	}
	return out
}

func TestCleanRepoHasNoFindings(t *testing.T) {
	ws := load(t, "../../testdata/clean")
	if len(ws) != 3 {
		t.Fatalf("want 3 workflows, got %d", len(ws))
	}
	if f := Analyze(ws, nil); len(f) != 0 {
		t.Fatalf("want no findings, got %+v", f)
	}
}

func TestClassification(t *testing.T) {
	cases := map[string]Class{
		"../../testdata/clean/.github/workflows/deploy.yml":            ClassDeploy,
		"../../testdata/clean/.github/workflows/pr.yml":                ClassPRCheck,
		"../../testdata/clean/.github/workflows/nightly.yml":           ClassScheduled,
		"../../testdata/messy/.github/workflows/release.yml":           ClassDeploy,
		"../../testdata/messy/.github/workflows/sha-keyed.yml":         ClassOther,
		"../../testdata/shapes/.github/workflows/job-level-deploy.yml": ClassDeploy,
	}
	for path, want := range cases {
		w, err := parseFile(path)
		if err != nil {
			t.Fatalf("parseFile(%s): %v", path, err)
		}
		if w.Class != want {
			t.Errorf("%s: class = %q, want %q", path, w.Class, want)
		}
	}
}

func TestMissingDeployConcurrency(t *testing.T) {
	got := findingFor(analyze(t, "../../testdata/messy"), "release.yml")
	if len(got) != 1 {
		t.Fatalf("want exactly 1 finding for release.yml, got %+v", got)
	}
	f := got[0]
	if f.Rule != RuleMissingDeployConcurrency {
		t.Errorf("rule = %q, want %q", f.Rule, RuleMissingDeployConcurrency)
	}
	if f.Severity != "high" {
		t.Errorf("severity = %q, want high", f.Severity)
	}
	if !strings.Contains(f.Suggestion, "cancel-in-progress: false") {
		t.Errorf("suggestion must queue, not cancel:\n%s", f.Suggestion)
	}
}

func TestDeployThatCancelsMidRun(t *testing.T) {
	got := findingFor(analyze(t, "../../testdata/messy"), "deploy-staging.yml")
	if len(got) != 1 || got[0].Rule != RuleDeployCancelInProgress {
		t.Fatalf("want one DEPLOY_CANCEL_IN_PROGRESS, got %+v", got)
	}
	if got[0].Severity != "high" {
		t.Errorf("severity = %q, want high", got[0].Severity)
	}
}

func TestMissingPRCancel(t *testing.T) {
	got := findingFor(analyze(t, "../../testdata/messy"), "pr-matrix.yml")
	if len(got) != 1 || got[0].Rule != RuleMissingPRCancel {
		t.Fatalf("want one MISSING_PR_CANCEL, got %+v", got)
	}
	if got[0].Severity != "medium" {
		t.Errorf("severity = %q, want medium", got[0].Severity)
	}
	// 3 jobs x 5 min x 3 superseded pushes.
	if !strings.Contains(got[0].Note, "~45 runner-min/week") {
		t.Errorf("note missing the wasted-minutes estimate: %q", got[0].Note)
	}
	if !strings.Contains(got[0].Suggestion, "cancel-in-progress: true") {
		t.Errorf("suggestion must cancel:\n%s", got[0].Suggestion)
	}
}

func TestGroupTooCoarseAndTooFine(t *testing.T) {
	findings := analyze(t, "../../testdata/messy")

	coarse := findingFor(findings, "integration.yml")
	if len(coarse) != 1 || coarse[0].Rule != RuleGroupTooCoarse {
		t.Fatalf("want one GROUP_TOO_COARSE for a workflow-name-only group, got %+v", coarse)
	}
	if coarse[0].Severity != "medium" {
		t.Errorf("coarse severity = %q, want medium", coarse[0].Severity)
	}

	fine := findingFor(findings, "sha-keyed.yml")
	if len(fine) != 1 || fine[0].Rule != RuleGroupTooFine {
		t.Fatalf("want one GROUP_TOO_FINE for a sha-keyed group, got %+v", fine)
	}
	if fine[0].Severity != "low" {
		t.Errorf("fine severity = %q, want low", fine[0].Severity)
	}

	ignored := analyze(t, "../../testdata/messy", RuleGroupTooFine)
	if len(findingFor(ignored, "sha-keyed.yml")) != 0 {
		t.Errorf("--ignore GROUP_TOO_FINE must drop the finding")
	}
	if Worst(ignored) != SevHigh {
		t.Errorf("remaining worst = %v, want high", Worst(ignored))
	}
}

func TestMessyRepoFindingCount(t *testing.T) {
	if got := analyze(t, "../../testdata/messy"); len(got) != 5 {
		for _, f := range got {
			t.Logf("%s %s %s", f.Severity, f.Rule, f.Workflow)
		}
		t.Fatalf("want 5 findings, got %d", len(got))
	}
}

func TestConcurrencyShapes(t *testing.T) {
	findings := analyze(t, "../../testdata/shapes")

	// A bare string concurrency value is the group, and this one is global.
	if got := findingFor(findings, "string-concurrency.yml"); len(got) != 1 || got[0].Rule != RuleGroupTooCoarse {
		t.Errorf("string-form concurrency: want GROUP_TOO_COARSE, got %+v", got)
	}
	// Job-level concurrency still protects the deploy.
	if got := findingFor(findings, "job-level-deploy.yml"); len(got) != 0 {
		t.Errorf("job-level concurrency must satisfy a deploy, got %+v", got)
	}
	// cancel-in-progress as an expression is a deliberate choice, not a gap.
	if got := findingFor(findings, "expr-cancel.yml"); len(got) != 0 {
		t.Errorf("expression cancel-in-progress must count as cancelling, got %+v", got)
	}
	// One queue across staging and production makes them wait for each other.
	if got := findingFor(findings, "multi-env-deploy.yml"); len(got) != 1 || got[0].Rule != RuleGroupTooCoarse {
		t.Errorf("multi-environment deploy: want GROUP_TOO_COARSE, got %+v", got)
	}
}

func TestOnKeyIsFoundDespiteBooleanResolution(t *testing.T) {
	w, err := parseFile("../../testdata/clean/.github/workflows/pr.yml")
	if err != nil {
		t.Fatal(err)
	}
	if len(w.Events) != 1 || w.Events[0] != "pull_request" {
		t.Fatalf("events = %v, want [pull_request]", w.Events)
	}
}

func TestMalformedWorkflowIsFatal(t *testing.T) {
	_, err := Load("../../testdata/malformed")
	if err == nil {
		t.Fatal("want an error for unparseable YAML")
	}
	if !strings.Contains(err.Error(), "broken.yml") {
		t.Errorf("error must name the file, got %q", err)
	}
}

func TestMissingWorkflowsDirIsNotAnError(t *testing.T) {
	ws, err := Load("../../testdata/no-workflows")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ws) != 0 {
		t.Fatalf("want no workflows, got %d", len(ws))
	}
}

func TestParseSeverity(t *testing.T) {
	for in, want := range map[string]Severity{"high": SevHigh, "MEDIUM": SevMedium, "low": SevLow, "never": SevNever} {
		got, err := ParseSeverity(in)
		if err != nil || got != want {
			t.Errorf("ParseSeverity(%q) = %v, %v", in, got, err)
		}
	}
	if _, err := ParseSeverity("critical"); err == nil {
		t.Error("want an error for an unknown severity")
	}
}

func TestParseIgnoreRejectsTypos(t *testing.T) {
	set, err := ParseIgnore("group_too_fine, MISSING_PR_CANCEL")
	if err != nil {
		t.Fatal(err)
	}
	if !set[RuleGroupTooFine] || !set[RuleMissingPRCancel] {
		t.Errorf("parsed set = %v", set)
	}
	if _, err := ParseIgnore("GROUP_TOO_FINEE"); err == nil {
		t.Error("a typo must be an error, not a silently ignored rule")
	}
}

func TestReportFormats(t *testing.T) {
	ws := load(t, "../../testdata/messy")
	report := NewReport("../../testdata/messy", ws, Analyze(ws, nil))

	var jsonBuf bytes.Buffer
	if err := report.Write(&jsonBuf, "json"); err != nil {
		t.Fatal(err)
	}
	var round Report
	if err := json.Unmarshal(jsonBuf.Bytes(), &round); err != nil {
		t.Fatalf("json output is not valid JSON: %v", err)
	}
	if round.Summary["high"] != 2 || round.Summary["medium"] != 2 || round.Summary["low"] != 1 {
		t.Errorf("summary = %v", round.Summary)
	}

	var tableBuf bytes.Buffer
	if err := report.Write(&tableBuf, "table"); err != nil {
		t.Fatal(err)
	}
	table := tableBuf.String()
	for _, want := range []string{"MISSING_DEPLOY_CONCURRENCY", "Suggested blocks", "cancel-in-progress"} {
		if !strings.Contains(table, want) {
			t.Errorf("table output missing %q:\n%s", want, table)
		}
	}

	var mdBuf bytes.Buffer
	if err := report.Write(&mdBuf, "markdown"); err != nil {
		t.Fatal(err)
	}
	md := mdBuf.String()
	if !strings.Contains(md, "| Severity | Rule | Class | Workflow | Note |") {
		t.Errorf("markdown output has no table:\n%s", md)
	}
	if !strings.Contains(md, "```yaml") {
		t.Errorf("markdown output has no fenced suggestion:\n%s", md)
	}

	if err := report.Write(&mdBuf, "xml"); err == nil {
		t.Error("want an error for an unknown format")
	}
}

func TestCleanReportSaysSo(t *testing.T) {
	ws := load(t, "../../testdata/clean")
	var buf bytes.Buffer
	if err := NewReport("../../testdata/clean", ws, Analyze(ws, nil)).Write(&buf, "table"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "no concurrency findings") {
		t.Errorf("unexpected clean output: %q", buf.String())
	}
}
