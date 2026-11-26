package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/tools"
)

// testDefaultDefinitions returns acode's shipped definitions for tests that
// need the production catalog shape.
func testDefaultDefinitions(t *testing.T) []agent.AgentDefinition {
	t.Helper()
	defs, err := defaultAgentDefinitions()
	if err != nil {
		t.Fatalf("defaultAgentDefinitions: %v", err)
	}
	return defs
}

// TestDefaultAgentDefinitions pins the shipped roster: the routing names, the
// trigger-shaped descriptions with their byte counts against the SDK's
// 200-byte cap, the grants, and the bodies. The descriptions are the parent's
// only routing signal, and the SDK ships no definitions of its own, so any
// change here is deliberate and this pin is what makes it one.
func TestDefaultAgentDefinitions(t *testing.T) {
	t.Parallel()
	defs := testDefaultDefinitions(t)

	names := make([]string, len(defs))
	for i, def := range defs {
		names[i] = def.Name
	}
	if !slices.Equal(names, []string{"explore", "implementer", "researcher", "reviewer", "task"}) {
		t.Fatalf("roster = %v", names)
	}

	want := map[string]struct {
		description string
		tools       []string
		body        string
	}{
		"explore": {
			description: "Read-only lookup: where something lives, how a mechanism works, which files a change touches. Call it when you need the conclusion, not the reads; it reports with file and line and cannot edit.",
			tools:       []string{"read_file", "search"},
			body: `Explore the assigned area and report what you found. Lead with the
answer, cite file and line for every claim, and include only the facts the
caller needs to act. When the evidence does not settle the question, say what
is missing rather than guessing. Your tools are read-only: if a change is
needed, describe it precisely instead of making it.
`,
		},
		"implementer": {
			description: "Implementation of one self-contained change whose full spec fits in a prompt. It reads, edits, runs the tests, and reports what changed and what it could not verify. Keep cascading work here.",
			tools:       []string{"read_file", "edit_file", "search", "run_command"},
			body: `Implement the assigned change exactly as specified, keeping it minimal and
focused. Read the surrounding code before editing, and run the relevant
tests after. When the spec is ambiguous or contradicts the code, follow
the most reasonable interpretation and record the choice in your report rather
than choosing silently. Report what changed, which commands you ran and
their outcomes, and anything you could not verify.
`,
		},
		"researcher": {
			description: "Answers questions needing sources from the workspace, the web, or both. Call it for a multi-source survey you would otherwise run yourself; claims come back cited, with inference marked.",
			tools:       []string{"read_file", "search", "web_search", "web_fetch"},
			body: `Research the assigned question using the workspace and the web. Cite every
claim with a file and line or a URL, and note a source's date when recency
matters. Separate what you verified from what you inferred, and say plainly
when the evidence is insufficient. Prefer current sources; name the reason
when you rely on an older one.
`,
		},
		"reviewer": {
			description: "Independent review of a change its author has read and believes correct. Call it before finishing contract, documented-behavior, or multi-file changes. It returns defects, not confirmation.",
			tools:       []string{"read_file", "search"},
			body: `Review the assigned change as a skeptic: the author has read it and believes
it is correct, so your value is disagreement, not agreement. Lead with
concrete findings, each cited to file and line. Check first what the author
claims is fixed, verified, or covered by tests — check each claim against
the code and the tests, and say plainly what you could not settle, because
confident wrongness is the defect a self-check cannot catch. Then look for
behavior regressions, unstated behavior changes, contract drift between the
change and its documentation, and tests that now pass vacuously. Report
defects in the assigned change, not improvement ideas for the rest of the
codebase. Say plainly when you find nothing, and end with what you covered
and what you did not look at, so an empty finding is a scoped claim rather
than a guess.
`,
		},
		"task": {
			description: "Self-contained multi-step work needing reads, edits, and commands: the fallback when no narrower role fits. It can do everything except delegate.",
			tools:       []string{"read_file", "edit_file", "search", "run_command", "web_search", "web_fetch", "todo", "read_image"},
			body: `Carry out the assigned task end to end. Read before editing, keep changes
minimal, and run the relevant tests. When the spec is ambiguous, follow the
most reasonable interpretation and record the choice in your report rather
than choosing silently. Report what you changed, what you verified, and what
remains.
`,
		},
	}

	// The exact byte counts of the shipped descriptions, so a rewrite that
	// would exceed the catalog cap fails here rather than at startup.
	wantBytes := map[string]int{"explore": 193, "implementer": 191, "researcher": 186, "reviewer": 189, "task": 145}

	for _, def := range defs {
		expected, ok := want[def.Name]
		if !ok {
			t.Fatalf("unexpected agent %q", def.Name)
		}
		if def.Description != expected.description {
			t.Errorf("%s description = %q, want %q", def.Name, def.Description, expected.description)
		}
		if got := len(def.Description); got != wantBytes[def.Name] || got > 200 {
			t.Errorf("%s description is %d bytes, want %d", def.Name, got, wantBytes[def.Name])
		}
		if !slices.Equal(def.Tools, expected.tools) {
			t.Errorf("%s tools = %v, want %v", def.Name, def.Tools, expected.tools)
		}
		if def.Prompt != expected.body {
			t.Errorf("%s body = %q, want %q", def.Name, def.Prompt, expected.body)
		}
		if def.Source != agent.SourceUser || def.Path != "" {
			t.Errorf("%s source/path = %q/%q, want a file-less parsed definition", def.Name, def.Source, def.Path)
		}
	}
}

// TestDefaultAgentDefinitionsTaskGrant pins the task grant to the SDK's
// current toolset minus delegate. The embedded file lists the tools by name,
// so an SDK release that adds a tool must fail here and force a deliberate
// file update instead of silently narrowing the fallback role.
func TestDefaultAgentDefinitionsTaskGrant(t *testing.T) {
	t.Parallel()
	defs := testDefaultDefinitions(t)
	var task *agent.AgentDefinition
	for i := range defs {
		if defs[i].Name == "task" {
			task = &defs[i]
		}
	}
	if task == nil {
		t.Fatal("task definition missing")
	}
	if got, want := task.Tools, tools.NamesWithout("delegate"); !slices.Equal(got, want) {
		t.Fatalf("task tools = %v, want %v (the file must name every current tool except delegate)", got, want)
	}
}

// TestLoadAgentsOperatorFilesWin pins the discovery precedence: the shipped
// five are the base, an operator file with the same name replaces exactly that
// role, and a malformed operator file is skipped with a warning rather than
// removing the shipped role.
func TestLoadAgentsOperatorFilesWin(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "agents"), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	operator := "name: reviewer\ndescription: Operator review.\ntools: read_file\n---\nOperator body.\n"
	if err := os.WriteFile(filepath.Join(dir, "agents", "reviewer.md"), []byte(operator), 0o600); err != nil {
		t.Fatalf("write operator definition: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "agents", "broken.md"), []byte("no terminator\n"), 0o600); err != nil {
		t.Fatalf("write broken definition: %v", err)
	}

	catalog := agent.LoadAgents(testDefaultDefinitions(t), dir)
	if len(catalog.Warnings) != 1 || !strings.Contains(catalog.Warnings[0], "broken.md") {
		t.Fatalf("warnings = %v, want one skip for broken.md", catalog.Warnings)
	}
	byName := map[string]agent.AgentDefinition{}
	for _, def := range catalog.Agents {
		byName[def.Name] = def
	}
	if len(byName) != 5 {
		t.Fatalf("catalog = %v, want the five names", byName)
	}
	reviewer, ok := byName["reviewer"]
	if !ok || reviewer.Source != agent.SourceUser || reviewer.Description != "Operator review." ||
		reviewer.Path == "" || reviewer.Prompt != "Operator body.\n" {
		t.Fatalf("operator reviewer = %+v, want the operator file to win", reviewer)
	}
	for _, name := range []string{"explore", "implementer", "researcher", "task"} {
		def, ok := byName[name]
		if !ok {
			t.Fatalf("%s missing from the catalog", name)
		}
		if def.Source != agent.SourceDefault || def.Path != "" {
			t.Fatalf("%s source/path = %q/%q, want the shipped default", name, def.Source, def.Path)
		}
	}
}
