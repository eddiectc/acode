package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

func TestMaxSubagentsConfig(t *testing.T) {
	t.Parallel()
	cfg := cfgTestParseOK(t, cfgTestArgs(t), nil)
	if cfg.MaxSubagents != defaultMaxSubagents || cfg.maxSubagentsSet {
		t.Fatalf("default max subagents = %d (set %v), want %d", cfg.MaxSubagents, cfg.maxSubagentsSet, defaultMaxSubagents)
	}

	cfg = cfgTestParseOK(t, cfgTestArgs(t, "-max-subagents=3"), nil)
	if cfg.MaxSubagents != 3 || !cfg.maxSubagentsSet {
		t.Fatalf("max subagents = %d (set %v), want 3", cfg.MaxSubagents, cfg.maxSubagentsSet)
	}

	cfg = cfgTestParseOK(t, cfgTestArgs(t), map[string]string{"ACODE_MAX_SUBAGENTS": "0"})
	if cfg.MaxSubagents != 0 || !cfg.maxSubagentsSet {
		t.Fatalf("env max subagents = %d (set %v), want unlimited", cfg.MaxSubagents, cfg.maxSubagentsSet)
	}

	if err := cfgTestParseErr(t, cfgTestArgs(t, "-max-subagents=-1"), nil); !strings.Contains(err.Error(), "max subagents must not be negative") {
		t.Fatalf("negative flag error = %v", err)
	}
	if err := cfgTestParseErr(t, cfgTestArgs(t), map[string]string{"ACODE_MAX_SUBAGENTS": "-2"}); !strings.Contains(err.Error(), "max subagents must not be negative") {
		t.Fatalf("negative env error = %v", err)
	}
}

func TestMaxSubagentsConfigFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte(`{"max_subagents": 5}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := cfgTestParseOK(t, cfgTestArgs(t, "-config", path), nil)
	if cfg.MaxSubagents != 5 {
		t.Fatalf("max subagents = %d, want 5 from the file", cfg.MaxSubagents)
	}
	if err := os.WriteFile(path, []byte(`{"max_subagents": -1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cfgTestParseErr(t, cfgTestArgs(t, "-config", path), nil); !strings.Contains(err.Error(), "max_subagents must not be negative") {
		t.Fatalf("negative file error = %v", err)
	}
}

func TestBatchRootGrantExcludesDelegate(t *testing.T) {
	t.Parallel()
	root, err := batchRootGrant(nil, tools.Version)
	if err != nil {
		t.Fatalf("batchRootGrant: %v", err)
	}
	if slices.Contains(root, tools.NameDelegate) {
		t.Fatalf("batch root grant includes delegate: %v", root)
	}
	if len(root) != len(tools.NamesWithout(tools.NameDelegate)) {
		t.Fatalf("batch root grant = %v, want every current tool except delegate", root)
	}

	// A resumed old session keeps its frozen schema set; a current-only name
	// must not leak into its root grant.
	old, err := batchRootGrant(nil, "toolset-v0")
	if err != nil {
		t.Fatalf("batchRootGrant(toolset-v0): %v", err)
	}
	if len(old) != 4 || slices.Contains(old, "web_search") || slices.Contains(old, tools.NameDelegate) {
		t.Fatalf("toolset-v0 batch root grant = %v", old)
	}

	recorded, err := batchRootGrant([]string{tools.NameReadFile}, tools.Version)
	if err != nil {
		t.Fatalf("batchRootGrant(recorded): %v", err)
	}
	if len(recorded) != 1 || recorded[0] != tools.NameReadFile {
		t.Fatalf("recorded batch root grant = %v, want the recorded list", recorded)
	}

	// An explicit empty recorded grant stays a non-nil empty root grant.
	none, err := batchRootGrant([]string{}, tools.Version)
	if err != nil {
		t.Fatalf("batchRootGrant(empty): %v", err)
	}
	if none == nil || len(none) != 0 {
		t.Fatalf("empty recorded grant = %v, want a non-nil empty grant", none)
	}
}

// TestTUISessionSpecCarriesTheContract pins the engine inputs the TUI supplies:
// identity, user agent, definitions, and the per-run subagent cap. The per-run
// model is a RunSpec, so it is checked at the Start path instead.
func TestTUISessionSpecCarriesTheContract(t *testing.T) {
	t.Parallel()
	definitions := agent.LoadAgents(testDefaultDefinitions(t), t.TempDir())
	cfg := config{
		Provider:         providerName,
		Model:            "m",
		Dialect:          model.DialectChat,
		BaseURL:          "https://example.invalid/v1",
		Workspace:        t.TempDir(),
		StateDir:         t.TempDir(),
		PromptVersion:    agent.PromptVersion,
		ToolsetVersion:   tools.Version,
		HarnessVersion:   "test",
		MaxSubagents:     3,
		MaxContextTokens: 200_000,
		Agents:           definitions.Infos(),
	}
	if err := validateEffective(&cfg); err != nil {
		t.Fatalf("validate effective: %v", err)
	}
	spec := tuiEngineSpec(cfg, engine.ModeNew, "", "", definitions)
	if spec.Identity != acodeIdentity {
		t.Fatalf("identity = %q, want %q", spec.Identity, acodeIdentity)
	}
	if spec.UserAgent != acodeUserAgent("test") || spec.ToolOptions.UserAgent != spec.UserAgent {
		t.Fatalf("user agents = %q/%q", spec.UserAgent, spec.ToolOptions.UserAgent)
	}
	if spec.MaxSubagents != 3 {
		t.Fatalf("max subagents = %d, want 3", spec.MaxSubagents)
	}
	if len(spec.Definitions) != len(definitions.Agents) || len(spec.Definitions) == 0 {
		t.Fatalf("definitions = %d, want the loaded catalog", len(spec.Definitions))
	}
	if spec.AppendPromptSet {
		t.Fatal("an empty workspace text must not set AppendPrompt")
	}
	if spec.NewCompleter == nil {
		t.Fatal("engine spec has no completer factory")
	}
}

func TestResumePickerExcludesChildren(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	parent := createTestSession(t, stateDir, workspace, "model-a", "", "parent task")

	cfg := testSessionConfig(t, stateDir, workspace, "model-a")
	rcfg := testRunnerConfig(cfg)
	rcfg.Agent = "explore"
	rcfg.Tools = []string{"read_file"}
	child := session.NewID()
	store, err := session.Create(stateDir, child)
	if err != nil {
		t.Fatalf("create child session: %v", err)
	}
	parentRef := &agent.ParentRef{Session: parent, Seq: 1, CallID: "call-1"}
	if err := agent.WriteSessionStart(store, rcfg, parentRef); err != nil {
		_ = store.Close()
		t.Fatalf("write child session start: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close child session: %v", err)
	}

	choices, err := listPreviousSessions(stateDir, workspace, "")
	if err != nil {
		t.Fatalf("listPreviousSessions: %v", err)
	}
	if len(choices) != 1 || choices[0].ID != parent {
		t.Fatalf("choices = %+v, want only the parent session", choices)
	}

	// The child is still addressable by ID.
	state, err := agent.Inspect(readSessionEvents(t, session.SessionPath(stateDir, child)))
	if err != nil {
		t.Fatalf("Inspect(child): %v", err)
	}
	if state.ParentSession != parent || state.ParentCallID != "call-1" || state.Agent != "explore" {
		t.Fatalf("child state = %+v", state)
	}
}
