package main

import (
	"bytes"
	"strings"
	"testing"
)

func exitCode(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

func TestCleanRepoExitsZero(t *testing.T) {
	code, out, _ := exitCode(t, "--dir", "../../testdata/clean", "--fail-on", "low")
	if code != exitClean {
		t.Fatalf("exit = %d, want 0\n%s", code, out)
	}
}

func TestFailOnThreshold(t *testing.T) {
	cases := []struct {
		dir    string
		failOn string
		want   int
	}{
		{"../../testdata/messy", "high", exitFindings},
		{"../../testdata/messy", "never", exitClean},
		{"../../testdata/shapes", "high", exitClean}, // only medium findings there
		{"../../testdata/shapes", "medium", exitFindings},
	}
	for _, c := range cases {
		code, out, _ := exitCode(t, "--dir", c.dir, "--fail-on", c.failOn, "--format", "json")
		if code != c.want {
			t.Errorf("%s --fail-on %s: exit = %d, want %d\n%s", c.dir, c.failOn, code, c.want, out)
		}
	}
}

func TestIgnoreAffectsExitCode(t *testing.T) {
	all := strings.Join([]string{
		"MISSING_DEPLOY_CONCURRENCY", "DEPLOY_CANCEL_IN_PROGRESS",
		"MISSING_PR_CANCEL", "GROUP_TOO_COARSE", "GROUP_TOO_FINE",
	}, ",")
	code, out, _ := exitCode(t, "--dir", "../../testdata/messy", "--fail-on", "low", "--ignore", all)
	if code != exitClean {
		t.Fatalf("ignoring every rule must exit 0, got %d\n%s", code, out)
	}
	if !strings.Contains(out, "no concurrency findings") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestMalformedWorkflowExitsTwo(t *testing.T) {
	code, _, errOut := exitCode(t, "--dir", "../../testdata/malformed")
	if code != exitError {
		t.Fatalf("exit = %d, want 2", code)
	}
	if !strings.Contains(errOut, "broken.yml") {
		t.Errorf("stderr must name the file, got %q", errOut)
	}
}

func TestBadFlagValuesExitTwo(t *testing.T) {
	if code, _, _ := exitCode(t, "--dir", "../../testdata/clean", "--fail-on", "critical"); code != exitError {
		t.Errorf("bad --fail-on: exit = %d, want 2", code)
	}
	if code, _, _ := exitCode(t, "--dir", "../../testdata/clean", "--ignore", "NOPE"); code != exitError {
		t.Errorf("bad --ignore: exit = %d, want 2", code)
	}
	if code, _, _ := exitCode(t, "--dir", "../../testdata/clean", "--format", "xml"); code != exitError {
		t.Errorf("bad --format: exit = %d, want 2", code)
	}
}

func TestNoWorkflowsIsClean(t *testing.T) {
	code, out, _ := exitCode(t, "--dir", "../../testdata/no-workflows", "--fail-on", "low")
	if code != exitClean {
		t.Fatalf("exit = %d, want 0", code)
	}
	if !strings.Contains(out, "no workflows found") {
		t.Errorf("unexpected output: %q", out)
	}
}

func TestVersionFlag(t *testing.T) {
	code, out, _ := exitCode(t, "--version")
	if code != exitClean || strings.TrimSpace(out) != version {
		t.Errorf("--version: exit %d, out %q", code, out)
	}
}

func TestAuditsItsOwnRepository(t *testing.T) {
	code, out, errOut := exitCode(t, "--dir", "../..", "--fail-on", "low")
	if code != exitClean {
		t.Fatalf("this repository must pass its own audit: exit %d\n%s%s", code, out, errOut)
	}
}
