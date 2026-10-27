// Package audit parses GitHub Actions workflows and reports concurrency problems.
package audit

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Concurrency is a flattened workflow- or job-level concurrency block.
//
// GitHub accepts either a bare string (the group) or an object with group and
// cancel-in-progress, and cancel-in-progress itself may be an expression. All
// three shapes collapse into this struct.
type Concurrency struct {
	Present      bool   `json:"present"`
	Group        string `json:"group,omitempty"`
	Cancel       bool   `json:"cancelInProgress"`
	CancelSet    bool   `json:"cancelInProgressSet"`
	CancelIsExpr bool   `json:"cancelInProgressIsExpression"`
	Level        string `json:"level,omitempty"`
}

// Job is the slice of a workflow job this tool reasons about.
type Job struct {
	ID          string      `json:"id"`
	Environment string      `json:"environment,omitempty"`
	Concurrency Concurrency `json:"concurrency"`

	// text is every name/uses/run string in the job, used for classification.
	text string
}

// Workflow is one parsed file under .github/workflows.
type Workflow struct {
	Path        string      `json:"path"`
	Name        string      `json:"name"`
	Events      []string    `json:"events"`
	Concurrency Concurrency `json:"concurrency"`
	Jobs        []Job       `json:"jobs"`
	Class       Class       `json:"class"`
}

// FileError marks a workflow that could not be read or parsed. It is fatal:
// an unparseable workflow means the audit result would be a guess.
type FileError struct {
	Path string
	Err  error
}

func (e *FileError) Error() string { return fmt.Sprintf("%s: %v", e.Path, e.Err) }
func (e *FileError) Unwrap() error { return e.Err }

// Load reads every workflow under dir/.github/workflows. A missing workflows
// directory is not an error, it just yields no workflows.
func Load(dir string) ([]Workflow, error) {
	wfDir := filepath.Join(dir, ".github", "workflows")
	entries, err := os.ReadDir(wfDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, &FileError{Path: wfDir, Err: err}
	}

	var paths []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		switch strings.ToLower(filepath.Ext(e.Name())) {
		case ".yml", ".yaml":
			paths = append(paths, filepath.Join(wfDir, e.Name()))
		}
	}
	sort.Strings(paths)

	workflows := make([]Workflow, 0, len(paths))
	for _, p := range paths {
		w, err := parseFile(p)
		if err != nil {
			return nil, err
		}
		workflows = append(workflows, w)
	}
	return workflows, nil
}

func parseFile(path string) (Workflow, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return Workflow{}, &FileError{Path: path, Err: err}
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return Workflow{}, &FileError{Path: path, Err: err}
	}

	root := document(&doc)
	if root == nil {
		return Workflow{}, &FileError{Path: path, Err: fmt.Errorf("empty workflow")}
	}
	if root.Kind != yaml.MappingNode {
		return Workflow{}, &FileError{Path: path, Err: fmt.Errorf("workflow root is not a mapping")}
	}

	w := Workflow{Path: path}
	if n := mapValue(root, "name"); n != nil {
		w.Name = n.Value
	}
	if w.Name == "" {
		w.Name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}

	// `on` is special: YAML resolvers may tag it as a boolean, so match on the
	// raw scalar text rather than on a decoded key.
	onNode := mapValue(root, "on")
	if onNode == nil {
		onNode = mapValue(root, "true")
	}
	w.Events = eventNames(onNode)

	w.Concurrency = parseConcurrency(mapValue(root, "concurrency"), "workflow")

	if jobs := mapValue(root, "jobs"); jobs != nil && jobs.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(jobs.Content); i += 2 {
			id := jobs.Content[i].Value
			body := jobs.Content[i+1]
			if body.Kind != yaml.MappingNode {
				continue
			}
			job := Job{
				ID:          id,
				Environment: environmentName(mapValue(body, "environment")),
				Concurrency: parseConcurrency(mapValue(body, "concurrency"), "job:"+id),
				text:        jobText(body),
			}
			w.Jobs = append(w.Jobs, job)
		}
	}

	w.Class = classify(&w)
	return w, nil
}

func document(n *yaml.Node) *yaml.Node {
	if n == nil {
		return nil
	}
	if n.Kind == yaml.DocumentNode {
		if len(n.Content) == 0 {
			return nil
		}
		return n.Content[0]
	}
	return n
}

// mapValue returns the value node for key in a mapping, comparing the key's
// raw text so that `on`, `yes` and friends are found whatever tag the resolver
// gave them.
func mapValue(n *yaml.Node, key string) *yaml.Node {
	n = document(n)
	if n == nil || n.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(n.Content); i += 2 {
		if strings.EqualFold(n.Content[i].Value, key) {
			return n.Content[i+1]
		}
	}
	return nil
}

func eventNames(n *yaml.Node) []string {
	n = document(n)
	if n == nil {
		return nil
	}
	switch n.Kind {
	case yaml.ScalarNode:
		if n.Value == "" {
			return nil
		}
		return []string{n.Value}
	case yaml.SequenceNode:
		var out []string
		for _, c := range n.Content {
			if c.Kind == yaml.ScalarNode && c.Value != "" {
				out = append(out, c.Value)
			}
		}
		return out
	case yaml.MappingNode:
		var out []string
		for i := 0; i+1 < len(n.Content); i += 2 {
			out = append(out, n.Content[i].Value)
		}
		return out
	}
	return nil
}

func parseConcurrency(n *yaml.Node, level string) Concurrency {
	n = document(n)
	if n == nil {
		return Concurrency{}
	}
	c := Concurrency{Present: true, Level: level}
	if n.Kind == yaml.ScalarNode {
		c.Group = n.Value
		return c
	}
	if g := mapValue(n, "group"); g != nil {
		c.Group = g.Value
	}
	if ci := mapValue(n, "cancel-in-progress"); ci != nil {
		c.CancelSet = true
		v := strings.TrimSpace(ci.Value)
		if strings.Contains(v, "${{") {
			// An expression cancels on some refs; treat it as a deliberate choice.
			c.CancelIsExpr = true
			c.Cancel = true
		} else {
			c.Cancel = strings.EqualFold(v, "true")
		}
	}
	return c
}

func environmentName(n *yaml.Node) string {
	n = document(n)
	if n == nil {
		return ""
	}
	switch n.Kind {
	case yaml.ScalarNode:
		return n.Value
	case yaml.MappingNode:
		if name := mapValue(n, "name"); name != nil {
			return name.Value
		}
	case yaml.SequenceNode:
		if len(n.Content) > 0 {
			return environmentName(n.Content[0])
		}
	}
	return ""
}

func jobText(job *yaml.Node) string {
	var sb strings.Builder
	if u := mapValue(job, "uses"); u != nil {
		sb.WriteString(u.Value)
		sb.WriteByte('\n')
	}
	steps := mapValue(job, "steps")
	if steps == nil || steps.Kind != yaml.SequenceNode {
		return sb.String()
	}
	for _, s := range steps.Content {
		for _, key := range []string{"name", "uses", "run"} {
			if v := mapValue(s, key); v != nil {
				sb.WriteString(v.Value)
				sb.WriteByte('\n')
			}
		}
	}
	return sb.String()
}

// Effective returns the concurrency block that actually guards the workflow:
// the workflow-level block, or failing that the first job-level one.
func (w *Workflow) Effective() Concurrency {
	if w.Concurrency.Present {
		return w.Concurrency
	}
	for _, j := range w.Jobs {
		if j.Concurrency.Present {
			return j.Concurrency
		}
	}
	return Concurrency{}
}

// HasEvent reports whether the workflow is triggered by any of events.
func (w *Workflow) HasEvent(events ...string) bool {
	for _, want := range events {
		for _, got := range w.Events {
			if strings.EqualFold(got, want) {
				return true
			}
		}
	}
	return false
}
