// Command gha-concurrency-audit reports GitHub Actions workflows whose
// concurrency configuration lets deploys race or lets superseded pull-request
// runs burn runner minutes.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/moveeeax/gha-concurrency-audit/internal/audit"
)

const version = "0.1.0"

// Exit codes: 0 clean, 1 findings at or above --fail-on, 2 usage or parse error.
const (
	exitClean    = 0
	exitFindings = 1
	exitError    = 2
)

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("gha-concurrency-audit", flag.ContinueOnError)
	fs.SetOutput(stderr)
	dir := fs.String("dir", ".", "repository root to scan")
	format := fs.String("format", "table", "output format: table, json or markdown")
	failOn := fs.String("fail-on", "high", "exit 1 when a finding at this severity or above exists: high, medium, low or never")
	ignore := fs.String("ignore", "", "comma-separated rule names to skip, e.g. GROUP_TOO_FINE")
	showVersion := fs.Bool("version", false, "print version and exit")
	fs.Usage = func() {
		fmt.Fprintf(stderr, "usage: gha-concurrency-audit [flags]\n\nflags:\n")
		fs.PrintDefaults()
		fmt.Fprintf(stderr, "\nrules: %v\n", audit.AllRules)
	}

	if err := fs.Parse(args); err != nil {
		return exitError
	}
	if *showVersion {
		fmt.Fprintln(stdout, version)
		return exitClean
	}

	threshold, err := audit.ParseSeverity(*failOn)
	if err != nil {
		fmt.Fprintf(stderr, "gha-concurrency-audit: %v\n", err)
		return exitError
	}
	ignored, err := audit.ParseIgnore(*ignore)
	if err != nil {
		fmt.Fprintf(stderr, "gha-concurrency-audit: %v\n", err)
		return exitError
	}

	workflows, err := audit.Load(*dir)
	if err != nil {
		fmt.Fprintf(stderr, "gha-concurrency-audit: %v\n", err)
		return exitError
	}

	findings := audit.Analyze(workflows, ignored)
	report := audit.NewReport(*dir, workflows, findings)
	if err := report.Write(stdout, *format); err != nil {
		fmt.Fprintf(stderr, "gha-concurrency-audit: %v\n", err)
		return exitError
	}

	if threshold != audit.SevNever && audit.Worst(findings) >= threshold {
		return exitFindings
	}
	return exitClean
}
