package main

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/eddiectc/acode/internal/tui"
	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

func testSessionConfig(t *testing.T, stateDir, workspace, modelName string) config {
	t.Helper()
	cfg := config{
		Provider:         providerName,
		Model:            modelName,
		Dialect:          model.DialectChat,
		BaseURL:          "https://example.invalid/v1",
		Workspace:        workspace,
		StateDir:         stateDir,
		PromptVersion:    agent.PromptVersion,
		ToolsetVersion:   tools.Version,
		HarnessVersion:   "test",
		MaxContextTokens: 200_000,
	}
	if err := validateEffective(&cfg); err != nil {
		t.Fatalf("validate test session config: %v", err)
	}
	return cfg
}

// testRunnerConfig maps a test config to the runner config SDK helpers record.
// Production construction now goes through engine.Open; tests that seed a log
// directly still need the same recorded contract.
func testRunnerConfig(cfg config) agent.Config {
	return agent.Config{
		Provider:       cfg.Provider,
		Dialect:        string(cfg.Dialect),
		Model:          cfg.Model,
		BaseURL:        cfg.BaseURL,
		Workspace:      cfg.Workspace,
		StateDir:       cfg.StateDir,
		Identity:       acodeIdentity,
		PromptVersion:  cfg.PromptVersion,
		ToolsetVersion: cfg.ToolsetVersion,
		HarnessVersion: cfg.HarnessVersion,
		Agent:          cfg.Agent,
		Agents:         cfg.Agents,
		Limits:         cfg.Limits,
		MaxTurns:       cfg.MaxTurns,
		MaxTotalTokens: cfg.MaxTotalTokens,
		SessionTimeout: cfg.SessionTimeout,
		ModelTimeout:   cfg.ModelTimeout,
		ThinkingEffort: cfg.ThinkingEffort,
		Pricing:        cfg.Pricing,
	}
}

// createTestSession writes a valid session log and returns its ID. The first
// user message is optional.
func createTestSession(t *testing.T, stateDir, workspace, modelName, thinkingEffort, firstMessage string) string {
	t.Helper()
	cfg := testSessionConfig(t, stateDir, workspace, modelName)
	cfg.ThinkingEffort = thinkingEffort
	id := session.NewID()
	store, err := session.Create(stateDir, id)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := agent.WriteSessionStart(store, testRunnerConfig(cfg), nil); err != nil {
		_ = store.Close()
		t.Fatalf("write session start: %v", err)
	}
	if firstMessage != "" {
		ev, err := session.NewEvent(session.TypeUserMessage, 0, "", struct {
			Text   string `json:"text"`
			Source string `json:"source"`
		}{Text: firstMessage, Source: "tui"})
		if err != nil {
			_ = store.Close()
			t.Fatalf("build user message: %v", err)
		}
		if err := store.Append(ev); err != nil {
			_ = store.Close()
			t.Fatalf("append user message: %v", err)
		}
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}
	return id
}

// createTestSessionWithContract writes a session log whose recorded contract is
// adjusted by mutate after the base config is built. Tests use it to seed a
// pre-bump session: the recorded prompt, version, agent, and grant then differ
// from the current defaults.
func createTestSessionWithContract(t *testing.T, stateDir, workspace, modelName string, mutate func(*agent.Config)) string {
	t.Helper()
	cfg := testSessionConfig(t, stateDir, workspace, modelName)
	rcfg := testRunnerConfig(cfg)
	mutate(&rcfg)
	id := session.NewID()
	store, err := session.Create(stateDir, id)
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
	if err := agent.WriteSessionStart(store, rcfg, nil); err != nil {
		_ = store.Close()
		t.Fatalf("write session start: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close session: %v", err)
	}
	return id
}

func TestListPreviousSessionsFiltersWorkspaceAndOrdersNewest(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	otherWorkspace := t.TempDir()
	older := createTestSession(t, stateDir, workspace, "model-a", "", "older task")
	newer := createTestSession(t, stateDir, workspace, "model-b", "", "  newer\n task  ")
	other := createTestSession(t, stateDir, otherWorkspace, "model-c", "", "other task")
	current := createTestSession(t, stateDir, workspace, "model-d", "", "current task")

	base := time.Date(2026, 11, 14, 10, 0, 0, 0, time.UTC)
	for id, at := range map[string]time.Time{
		older:   base,
		newer:   base.Add(time.Hour),
		other:   base.Add(2 * time.Hour),
		current: base.Add(3 * time.Hour),
	} {
		path := session.SessionPath(stateDir, id)
		if err := os.Chtimes(path, at, at); err != nil {
			t.Fatalf("set mtime %s: %v", id, err)
		}
	}
	// A corrupt log in the same workspace is skipped instead of failing the
	// whole picker.
	corrupt := session.SessionPath(stateDir, session.NewID())
	if err := os.WriteFile(corrupt, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	choices, err := listPreviousSessions(stateDir, workspace, current)
	if err != nil {
		t.Fatalf("listPreviousSessions: %v", err)
	}
	if len(choices) != 2 {
		t.Fatalf("choices = %+v, want the two same-workspace sessions", choices)
	}
	if choices[0].ID != newer || choices[1].ID != older {
		t.Fatalf("choices = %+v, want newest first", choices)
	}
	if choices[0].Summary != "newer task" || choices[0].Model != "model-b" {
		t.Fatalf("newer choice = %+v", choices[0])
	}
	if choices[0].Modified.IsZero() {
		t.Fatal("newer choice has no modification time")
	}
}

func TestListPreviousSessionsMissingDirectory(t *testing.T) {
	t.Parallel()
	choices, err := listPreviousSessions(t.TempDir(), "/workspace", "")
	if err != nil {
		t.Fatalf("listPreviousSessions: %v", err)
	}
	if len(choices) != 0 {
		t.Fatalf("choices = %+v, want none", choices)
	}
}

func TestSessionPreviewCapsAndSanitizes(t *testing.T) {
	t.Parallel()
	got := sessionPreview(strings.Repeat("x", sessionPreviewBytes+5) + "\n\x1b")
	if len(got) > sessionPreviewBytes+32 {
		t.Fatalf("preview length = %d", len(got))
	}
	if strings.ContainsRune(got, '\x1b') {
		t.Fatalf("preview contains ESC: %q", got)
	}
}

// newSessionHost builds a TUI host around one open current session, using the
// same engine open path as production.
func newSessionHost(t *testing.T, stateDir, workspace string) (*tuiHost, string) {
	t.Helper()
	current := createTestSession(t, stateDir, workspace, "model-current", "", "current task")
	cfg := testSessionConfig(t, stateDir, workspace, "model-current")
	cfg.ResumeID = current
	host, code := openTUIHost(cfg, io.Discard)
	if host == nil {
		t.Fatalf("openTUIHost exit code %d", code)
	}
	t.Cleanup(func() { _ = host.Close() })
	return host, current
}

func TestTUIHostNewSessionReplacesTheStore(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	host, current := newSessionHost(t, stateDir, workspace)

	snapshot, err := host.NewSession(tui.RunOptions{
		Model: "model-current", ThinkingEffort: "high", ThinkingEffortSet: true,
	})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if snapshot.Info.SessionID == current {
		t.Fatal("NewSession reused the active session ID")
	}
	if host.host.ID() != snapshot.Info.SessionID {
		t.Fatalf("host session = %q, snapshot = %q",
			host.host.ID(), snapshot.Info.SessionID)
	}
	if host.cfg.ThinkingEffort != "high" || snapshot.Info.ThinkingEffort != "high" {
		t.Fatalf("effort: cfg=%q info=%q", host.cfg.ThinkingEffort, snapshot.Info.ThinkingEffort)
	}
	if snapshot.Info.Turns != 0 || len(snapshot.History) != 0 {
		t.Fatalf("new session inherited state: info=%+v history=%v", snapshot.Info, snapshot.History)
	}
	events, err := session.Read(snapshot.Info.TracePath)
	if err != nil {
		t.Fatalf("read new log: %v", err)
	}
	if len(events) != 1 || events[0].Type != session.TypeSessionStart {
		t.Fatalf("new log events = %+v", events)
	}
	if !strings.Contains(string(events[0].Data), `"thinking_effort":"high"`) {
		t.Fatalf("session_start lacks the next-run effort: %s", events[0].Data)
	}
	// The old session lock was released with the old store.
	reopened, err := session.Open(stateDir, current)
	if err != nil {
		t.Fatalf("old session stayed locked: %v", err)
	}
	_ = reopened.Close()
}

// TestTUIHostNewSessionStartsAFreshContract pins the replay boundary after a
// session switch: the recorded prompt, versions, agent, and grant belong to
// the session they were resumed from. /new must start from the current
// contract, or a pre-bump prompt (including an aharness identity line) and a
// batch grant that withholds delegate would leak into a fresh TUI session.
func TestTUIHostNewSessionStartsAFreshContract(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	host, _ := newSessionHost(t, stateDir, workspace)
	host.cfg.Agents = agent.LoadAgents(testDefaultDefinitions(t), t.TempDir()).Infos()

	legacyPrompt := "You are aharness, a coding harness working in the workspace \"/legacy\".\n\n- Legacy v2 rules."
	legacy := createTestSessionWithContract(t, stateDir, workspace, "model-current", func(c *agent.Config) {
		c.PromptVersion = agent.PromptVersionV2
		c.SystemPrompt = legacyPrompt
		c.Agent = "explore"
		c.Tools = []string{"read_file"}
	})
	if _, err := host.ResumeSession(legacy); err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	spec := host.host.Spec()
	if spec.SystemPrompt != legacyPrompt || spec.PromptVersion != agent.PromptVersionV2 {
		t.Fatalf("resume did not inherit the recorded contract: prompt=%q version=%q",
			spec.SystemPrompt, spec.PromptVersion)
	}

	snapshot, err := host.NewSession(tui.RunOptions{Model: "model-current"})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	events, err := session.Read(snapshot.Info.TracePath)
	if err != nil {
		t.Fatalf("read new log: %v", err)
	}
	if len(events) != 1 || events[0].Type != session.TypeSessionStart {
		t.Fatalf("new log events = %+v, want one session_start", events)
	}
	var start struct {
		PromptVersion  string   `json:"prompt_version"`
		ToolsetVersion string   `json:"toolset_version"`
		SystemPrompt   string   `json:"system_prompt"`
		Agent          string   `json:"agent"`
		Tools          []string `json:"tools"`
		RootTools      []string `json:"root_tools"`
	}
	if err := json.Unmarshal(events[0].Data, &start); err != nil {
		t.Fatalf("decode session_start: %v", err)
	}
	if start.PromptVersion != agent.PromptVersion || start.ToolsetVersion != tools.Version {
		t.Fatalf("new contract versions = %q/%q, want %q/%q",
			start.PromptVersion, start.ToolsetVersion, agent.PromptVersion, tools.Version)
	}
	for _, want := range []string{
		"You are acode, a coding harness working in the workspace",
		"Run the relevant tests before reporting the change done",
		"- reviewer: ",
	} {
		if !strings.Contains(start.SystemPrompt, want) {
			t.Fatalf("new prompt lacks %q:\n%s", want, start.SystemPrompt)
		}
	}
	if strings.Contains(start.SystemPrompt, legacyPrompt) {
		t.Fatalf("new prompt replayed the resumed session's prompt:\n%s", start.SystemPrompt)
	}
	if start.Agent != "" || start.Tools != nil {
		t.Fatalf("new contract agent/tools = %q/%v, want a fresh root session", start.Agent, start.Tools)
	}
	if start.RootTools != nil {
		t.Fatalf("new TUI contract root_tools = %v, want no narrowing", start.RootTools)
	}
}

// TestSessionOpenFailureExitCodes pins the engine's stricter open errors to the
// CLI contract: a missing, invalid, or locked session is a configuration-class
// failure (2), in both batch and the TUI, and nothing is created.
func TestSessionOpenFailureExitCodes(t *testing.T) {
	t.Parallel()
	workspace := t.TempDir()
	stateDir := t.TempDir()
	env := envLookup(integrationEnv(stateDir, ""))
	run := func(args ...string) (string, int) {
		t.Helper()
		_, stderr, exit := runCLIEnv(t, env, nil, append([]string{"-workspace", workspace}, args...)...)
		return stderr, exit
	}
	missing := session.NewID()
	for _, args := range [][]string{
		{"-batch", "-resume", missing},
		{"-batch", "-fork", missing, "-task", "x"},
		{"-ui-json", "-resume", missing},
	} {
		stderr, exit := run(args...)
		if exit != exitConfig {
			t.Fatalf("%v exit = %d, want %d\nstderr:\n%s", args, exit, exitConfig, stderr)
		}
	}

	// A session another process holds is rejected before any state changes.
	id := createTestSession(t, stateDir, workspace, "model-current", "", "held task")
	held, err := session.Open(stateDir, id)
	if err != nil {
		t.Fatalf("hold session: %v", err)
	}
	defer func() { _ = held.Close() }()
	stderr, exit := run("-batch", "-resume", id, "-task", "x")
	if exit != exitConfig {
		t.Fatalf("locked session exit = %d, want %d\nstderr:\n%s", exit, exitConfig, stderr)
	}
}

func TestTUIHostResumeSessionInheritsRecordedSettings(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	host, current := newSessionHost(t, stateDir, workspace)
	previous := createTestSession(t, stateDir, workspace, "model-previous", "low", "earlier task")

	snapshot, err := host.ResumeSession(previous)
	if err != nil {
		t.Fatalf("ResumeSession: %v", err)
	}
	if snapshot.Info.SessionID != previous || host.host.ID() != previous {
		t.Fatalf("snapshot = %q, host = %q, want %q",
			snapshot.Info.SessionID, host.host.ID(), previous)
	}
	if host.cfg.Model != "model-previous" || host.cfg.ThinkingEffort != "low" {
		t.Fatalf("resumed config model=%q effort=%q", host.cfg.Model, host.cfg.ThinkingEffort)
	}
	if snapshot.Info.Model != "model-previous" || snapshot.Info.ThinkingEffort != "low" {
		t.Fatalf("resumed info = %+v", snapshot.Info)
	}
	if host.cfg.MaxContextTokens != 200_000 {
		t.Fatalf("max context = %d, want the recorded 200000", host.cfg.MaxContextTokens)
	}
	if len(snapshot.History) == 0 {
		t.Fatal("resume returned no history")
	}
	// The old store was closed, so its lock is available.
	reopened, err := session.Open(stateDir, current)
	if err != nil {
		t.Fatalf("previous session stayed locked: %v", err)
	}
	_ = reopened.Close()
}

func TestTUIHostResumeRejectsOtherWorkspaceAndKeepsCurrent(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	otherWorkspace := t.TempDir()
	host, current := newSessionHost(t, stateDir, workspace)
	other := createTestSession(t, stateDir, otherWorkspace, "model-other", "", "other task")

	if _, err := host.ResumeSession(other); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("ResumeSession error = %v, want workspace mismatch", err)
	}
	if host.host.ID() != current {
		t.Fatalf("failed resume switched to %q", host.host.ID())
	}
	// The current session is still open and locked.
	if _, err := session.Open(stateDir, current); !errors.Is(err, session.ErrLocked) {
		t.Fatalf("current session lock = %v, want ErrLocked", err)
	}
	// The rejected log is not left locked.
	if reopened, err := session.Open(stateDir, other); err != nil {
		t.Fatalf("rejected session stayed locked: %v", err)
	} else {
		_ = reopened.Close()
	}
}

func TestTUIHostResumeLockedSessionKeepsCurrent(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	workspace := t.TempDir()
	host, current := newSessionHost(t, stateDir, workspace)
	locked := createTestSession(t, stateDir, workspace, "model-locked", "", "locked task")
	held, err := session.Open(stateDir, locked)
	if err != nil {
		t.Fatalf("hold session: %v", err)
	}
	defer func() { _ = held.Close() }()

	if _, err := host.ResumeSession(locked); !errors.Is(err, session.ErrLocked) {
		t.Fatalf("ResumeSession error = %v, want ErrLocked", err)
	}
	if host.host.ID() != current {
		t.Fatalf("failed resume switched the host to %q", host.host.ID())
	}
}
