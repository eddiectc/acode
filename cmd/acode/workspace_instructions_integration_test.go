//go:build unix

package main

import (
	jsonv2 "encoding/json/v2"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/session"
)

const workspaceRules = "# Workspace rules\n\nWrite artifacts under out/, not the repo root.\n"

func writeWorkspaceRules(t *testing.T, workspace, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(workspace, workspaceInstructionsFile), []byte(text), 0o600); err != nil {
		t.Fatalf("write %s: %v", workspaceInstructionsFile, err)
	}
}

// sessionStartFields returns the raw session_start object so a test can assert
// a field was omitted, not merely empty.
func sessionStartFields(t *testing.T, path string) map[string]any {
	t.Helper()
	for _, ev := range readSessionEvents(t, path) {
		if ev.Type != session.TypeSessionStart {
			continue
		}
		var fields map[string]any
		if err := jsonv2.Unmarshal(ev.Data, &fields); err != nil {
			t.Fatalf("decode session_start fields: %v", err)
		}
		return fields
	}
	t.Fatalf("%s has no session_start", path)
	return nil
}

// createLegacySessionWithoutRecordedPrompt writes the shape of a log from
// before session_start carried system_prompt: the contract fields are present,
// the prompt is not.
func createLegacySessionWithoutRecordedPrompt(t *testing.T, stateDir, workspace string) string {
	t.Helper()
	cfg := testSessionConfig(t, stateDir, workspace, "deepseek-v4.1-flash")
	cfg.PromptVersion = agent.PromptVersionV2
	id := session.NewID()
	store, err := session.Create(stateDir, id)
	if err != nil {
		t.Fatalf("create legacy session: %v", err)
	}
	ev, err := session.NewEvent(session.TypeSessionStart, 0, "", map[string]any{
		"provider":         cfg.Provider,
		"dialect":          string(cfg.Dialect),
		"model":            cfg.Model,
		"base_url":         cfg.BaseURL,
		"limits":           cfg.Limits,
		"max_turns":        cfg.MaxTurns,
		"max_total_tokens": cfg.MaxTotalTokens,
		"prompt_version":   cfg.PromptVersion,
		"toolset_version":  cfg.ToolsetVersion,
		"workspace":        workspace,
		"state_dir":        stateDir,
		"harness_version":  cfg.HarnessVersion,
	})
	if err != nil {
		_ = store.Close()
		t.Fatalf("build legacy session_start: %v", err)
	}
	if err := store.Append(ev); err != nil {
		_ = store.Close()
		t.Fatalf("append legacy session_start: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy session: %v", err)
	}
	return id
}

func TestBinaryBatchNewSeedsWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := filepath.Join(t.TempDir(), "state")
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if run.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	start, _ := recordedSession(t, sessionPathFor(stateDir, run.id))
	if start.AppendedPrompt != workspaceRules {
		t.Fatalf("appended_prompt = %q, want the workspace rules", start.AppendedPrompt)
	}
	if got := strings.Count(start.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("rules appear %d times in the prompt, want once:\n%s", got, start.SystemPrompt)
	}
	if !strings.HasSuffix(start.SystemPrompt, "\n\n"+workspaceRules) {
		t.Fatalf("prompt does not append the rules after a blank line:\n%s", start.SystemPrompt)
	}
	if !strings.Contains(start.SystemPrompt, "You are acode, a coding harness working in the workspace") {
		t.Fatalf("prompt lacks the acode identity:\n%s", start.SystemPrompt)
	}
	if strings.Contains(start.SystemPrompt, "Available agents:") {
		t.Fatalf("batch prompt carries a catalog but withholds delegate:\n%s", start.SystemPrompt)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != start.SystemPrompt {
		t.Fatalf("request system prompt differs from the recording:\nrequest:\n%s\nrecorded:\n%s", got, start.SystemPrompt)
	}
}

func TestBinaryBatchNewWithoutInstructionsOmitsAppendedPrompt(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if run.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	path := sessionPathFor(stateDir, run.id)
	if _, ok := sessionStartFields(t, path)["appended_prompt"]; ok {
		t.Fatalf("session_start records appended_prompt without a workspace file")
	}
}

func TestBinaryUIJSONNewSeedsWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		sseFinal(t, w, "done")
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"hello"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	files := sessionFiles(t, stateDir)
	if len(files) != 1 {
		t.Fatalf("session files = %v, want one new session", files)
	}
	start, _ := recordedSession(t, files[0])
	if start.AppendedPrompt != workspaceRules {
		t.Fatalf("appended_prompt = %q, want the workspace rules", start.AppendedPrompt)
	}
	if got := strings.Count(start.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("rules appear %d times in the prompt, want once:\n%s", got, start.SystemPrompt)
	}
	if !strings.HasSuffix(start.SystemPrompt, "\n\n"+workspaceRules) {
		t.Fatalf("prompt does not append the rules after a blank line:\n%s", start.SystemPrompt)
	}
	if !strings.Contains(start.SystemPrompt, "Available agents:") {
		t.Fatalf("TUI parent prompt lacks the agent catalog:\n%s", start.SystemPrompt)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != start.SystemPrompt {
		t.Fatalf("request system prompt differs from the recording:\nrequest:\n%s\nrecorded:\n%s", got, start.SystemPrompt)
	}
}

func TestBinaryResumeKeepsRecordedWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := filepath.Join(t.TempDir(), "state")
	seedProvider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	seed := runHarness(t, bin, workspace, stateDir, seedProvider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if seed.exit != 0 {
		t.Fatalf("seed exit = %d\nstderr:\n%s", seed.exit, seed.stderr)
	}
	const changed = "# Workspace rules\n\nThis changed after the session started.\n"
	writeWorkspaceRules(t, workspace, changed)

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("resumed"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-resume", seed.id, "-task", "continue")
	if run.exit != 0 {
		t.Fatalf("resume exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	start, _ := recordedSession(t, sessionPathFor(stateDir, seed.id))
	if start.AppendedPrompt != workspaceRules {
		t.Fatalf("resume rewrote appended_prompt = %q, want the recorded text", start.AppendedPrompt)
	}
	if got := strings.Count(start.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("recorded rules appear %d times, want once:\n%s", got, start.SystemPrompt)
	}
	if strings.Contains(start.SystemPrompt, changed) {
		t.Fatalf("resume re-read the changed workspace file:\n%s", start.SystemPrompt)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != start.SystemPrompt {
		t.Fatalf("resumed request system prompt differs from the recording")
	}
}

func TestBinaryForkInheritsRecordedWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := filepath.Join(t.TempDir(), "state")
	seedProvider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	seed := runHarness(t, bin, workspace, stateDir, seedProvider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if seed.exit != 0 {
		t.Fatalf("seed exit = %d\nstderr:\n%s", seed.exit, seed.stderr)
	}
	const changed = "# Workspace rules\n\nThis changed after the session started.\n"
	writeWorkspaceRules(t, workspace, changed)

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("forked"))
	})
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, nil,
		"-batch", "-fork", seed.id, "-task", "continue")
	if exit != 0 {
		t.Fatalf("fork exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	parentStart, _ := recordedSession(t, sessionPathFor(stateDir, seed.id))
	var forkPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) != seed.id+".jsonl" {
			forkPath = path
		}
	}
	if forkPath == "" {
		t.Fatal("fork log missing")
	}
	forkStart, _ := recordedSession(t, forkPath)
	if forkStart.SystemPrompt != parentStart.SystemPrompt {
		t.Fatalf("fork rewrote the inherited prompt:\nfork:\n%s\nparent:\n%s", forkStart.SystemPrompt, parentStart.SystemPrompt)
	}
	if forkStart.AppendedPrompt != workspaceRules {
		t.Fatalf("fork appended_prompt = %q, want the inherited text", forkStart.AppendedPrompt)
	}
	if got := strings.Count(forkStart.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("inherited rules appear %d times, want once:\n%s", got, forkStart.SystemPrompt)
	}
	if strings.Contains(forkStart.SystemPrompt, changed) {
		t.Fatalf("fork re-read the changed workspace file:\n%s", forkStart.SystemPrompt)
	}
}

func TestBinaryUIJSONForkInheritsRecordedWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	seedProvider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	seed := runHarness(t, bin, workspace, stateDir, seedProvider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if seed.exit != 0 {
		t.Fatalf("seed exit = %d\nstderr:\n%s", seed.exit, seed.stderr)
	}
	const changed = "# Workspace rules\n\nThis changed after the session started.\n"
	writeWorkspaceRules(t, workspace, changed)

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("forked"))
	})
	script := `{"v":1,"type":"quit"}` + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-fork", seed.id, "-ui-size", "100x30")
	if exit != 0 {
		t.Fatalf("fork exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	parentStart, _ := recordedSession(t, sessionPathFor(stateDir, seed.id))
	var forkPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) != seed.id+".jsonl" {
			forkPath = path
		}
	}
	if forkPath == "" {
		t.Fatal("fork log missing")
	}
	forkStart, _ := recordedSession(t, forkPath)
	if forkStart.SystemPrompt != parentStart.SystemPrompt || forkStart.AppendedPrompt != workspaceRules {
		t.Fatalf("TUI fork prompt/appended = %q/%q, want the inherited contract", forkStart.SystemPrompt, forkStart.AppendedPrompt)
	}
	if strings.Contains(forkStart.SystemPrompt, changed) {
		t.Fatalf("TUI fork re-read the changed workspace file:\n%s", forkStart.SystemPrompt)
	}
}

func TestBinaryResumeLegacyLogDoesNotSeedWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := seedRecordedPromptSession(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("resumed"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-resume", parentID, "-task", "continue")
	if run.exit != 0 {
		t.Fatalf("resume exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	start, _ := recordedSession(t, sessionPathFor(stateDir, parentID))
	if start.SystemPrompt != legacyRecordedPrompt || start.AppendedPrompt != "" {
		t.Fatalf("resume rewrote the legacy contract: prompt = %q appended = %q", start.SystemPrompt, start.AppendedPrompt)
	}
	if _, ok := sessionStartFields(t, sessionPathFor(stateDir, parentID))["appended_prompt"]; ok {
		t.Fatal("resume added appended_prompt to a legacy log")
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != legacyRecordedPrompt {
		t.Fatalf("resumed request prompt = %q, want the recorded legacy prompt", got)
	}
}

func TestBinaryForkLegacyLogDoesNotSeedWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := seedRecordedPromptSession(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("forked"))
	})
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, nil,
		"-batch", "-fork", parentID, "-task", "continue")
	if exit != 0 {
		t.Fatalf("fork exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var forkPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) != parentID+".jsonl" {
			forkPath = path
		}
	}
	if forkPath == "" {
		t.Fatal("fork log missing")
	}
	forkStart, _ := recordedSession(t, forkPath)
	if forkStart.SystemPrompt != legacyRecordedPrompt {
		t.Fatalf("fork prompt = %q, want the recorded legacy prompt", forkStart.SystemPrompt)
	}
	if strings.Contains(forkStart.SystemPrompt, workspaceRules) {
		t.Fatalf("fork seeded a legacy log with workspace rules:\n%s", forkStart.SystemPrompt)
	}
	if _, ok := sessionStartFields(t, forkPath)["appended_prompt"]; ok {
		t.Fatal("fork recorded appended_prompt for a legacy log")
	}
}

func TestBinaryResumeLegacyLogWithoutRecordedPromptDoesNotSeed(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := createLegacySessionWithoutRecordedPrompt(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("resumed"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-resume", parentID, "-task", "continue")
	if run.exit != 0 {
		t.Fatalf("resume exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	got := requestSystemPrompt(t, bodies[0])
	// A log written before the recorded prompt replays the recorded version;
	// the SDK's faithful reconstruction uses its default identity. The plan's
	// claim is only that no workspace seed is added.
	if !strings.Contains(got, "a coding harness working in the workspace") {
		t.Fatalf("resumed request lacks a rendered prompt:\n%s", got)
	}
	if strings.Contains(got, workspaceRules) {
		t.Fatalf("resume seeded a log with no recorded prompt:\n%s", got)
	}
	if _, ok := sessionStartFields(t, sessionPathFor(stateDir, parentID))["appended_prompt"]; ok {
		t.Fatal("resume added appended_prompt to a log that recorded no prompt")
	}
}

func TestBinaryDelegatedChildOfLegacyParentGetsNoWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := createTestSessionWithContract(t, stateDir, workspace, "deepseek-v4.1-flash", func(cfg *config) {
		cfg.PromptVersion = agent.PromptVersionV2
		cfg.SystemPrompt = legacyRecordedPrompt
	})
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_legacy",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"Survey the workspace and report."}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("survey report"))
		case 3:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"delegate the read"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":45000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-resume", parentID, "-ui-size", "100x30", "-max-turns", "5")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var childPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) == parentID+".jsonl" {
			continue
		}
		childPath = path
	}
	if childPath == "" {
		t.Fatal("delegated child log missing")
	}
	childStart, _ := recordedSession(t, childPath)
	if strings.Contains(childStart.SystemPrompt, workspaceRules) {
		t.Fatalf("child of a legacy parent was seeded:\n%s", childStart.SystemPrompt)
	}
	if _, ok := sessionStartFields(t, childPath)["appended_prompt"]; ok {
		t.Fatal("child of a legacy parent recorded appended_prompt")
	}
}

// seedUIJSONSessionWithRules starts and quits a new TUI session so the test
// has a parent log with recorded workspace instructions and no runs.
func seedUIJSONSessionWithRules(t *testing.T, bin, workspace, stateDir string) string {
	t.Helper()
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		t.Errorf("unexpected provider call %d", call)
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	})
	script := `{"v":1,"type":"quit"}` + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash")
	if exit != 0 {
		t.Fatalf("seed exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	files := sessionFiles(t, stateDir)
	if len(files) != 1 {
		t.Fatalf("seed session files = %v, want one", files)
	}
	return strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")
}

func TestBinaryBatchSkipsOverCapWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, strings.Repeat("a", maxWorkspaceInstructionsBytes+1))
	stateDir := filepath.Join(t.TempDir(), "state")
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if run.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	for _, want := range []string{"acode: warning:", "workspace instructions skipped", workspaceInstructionsFile} {
		if !strings.Contains(run.stderr, want) {
			t.Fatalf("stderr lacks %q:\n%s", want, run.stderr)
		}
	}
	start, _ := recordedSession(t, sessionPathFor(stateDir, run.id))
	if start.AppendedPrompt != "" {
		t.Fatalf("over-cap file recorded appended_prompt = %q", start.AppendedPrompt)
	}
	if _, ok := sessionStartFields(t, sessionPathFor(stateDir, run.id))["appended_prompt"]; ok {
		t.Fatal("over-cap file recorded an appended_prompt field")
	}
}

func TestBinaryUIJSONShowsWorkspaceInstructionsWarning(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, strings.Repeat("a", maxWorkspaceInstructionsBytes+1))
	stateDir := t.TempDir()
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		t.Errorf("unexpected provider call %d", call)
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	})
	script := strings.Join([]string{
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "workspace instructions skipped") {
		t.Fatalf("TUI frame lacks the skip notice:\n%s", stdout)
	}
}

func TestBinaryUIJSONNewShowsWorkspaceInstructionsWarning(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := seedUIJSONSessionWithRules(t, bin, workspace, stateDir)
	writeWorkspaceRules(t, workspace, strings.Repeat("a", maxWorkspaceInstructionsBytes+1))

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		t.Errorf("unexpected provider call %d", call)
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"/new"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-resume", parentID, "-ui-size", "100x30")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "workspace instructions skipped") {
		t.Fatalf("/new frame lacks the skip notice:\n%s", stdout)
	}
	var freshPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) != parentID+".jsonl" {
			freshPath = path
		}
	}
	if freshPath == "" {
		t.Fatal("/new did not create a fresh session log")
	}
	freshStart, _ := recordedSession(t, freshPath)
	if freshStart.AppendedPrompt != "" {
		t.Fatalf("/new after an over-cap read recorded appended_prompt = %q", freshStart.AppendedPrompt)
	}
	if _, ok := sessionStartFields(t, freshPath)["appended_prompt"]; ok {
		t.Fatal("/new after an over-cap read recorded an appended_prompt field")
	}
}

func TestBinaryDelegatedChildOfResumedSessionCarriesWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := seedUIJSONSessionWithRules(t, bin, workspace, stateDir)
	const changed = "# Workspace rules\n\nThis changed after the session started.\n"
	writeWorkspaceRules(t, workspace, changed)

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_resume",
				`{"intent":"delegate the survey","tasks":[{"agent":"explore","task":"Survey the workspace and report."}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("survey report"))
		case 3:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"delegate the survey"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":45000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-resume", parentID, "-ui-size", "100x30", "-max-turns", "5")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var childPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) == parentID+".jsonl" {
			continue
		}
		childPath = path
	}
	if childPath == "" {
		t.Fatal("delegated child log missing")
	}
	childStart, _ := recordedSession(t, childPath)
	if got := strings.Count(childStart.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("child of a resumed parent carries the rules %d times, want once:\n%s", got, childStart.SystemPrompt)
	}
	if !strings.HasSuffix(childStart.SystemPrompt, "\n\n"+workspaceRules) {
		t.Fatalf("child rules are not appended after the role body:\n%s", childStart.SystemPrompt)
	}
	if strings.Contains(childStart.SystemPrompt, changed) {
		t.Fatalf("child of a resumed parent re-read the changed file:\n%s", childStart.SystemPrompt)
	}
	if _, ok := sessionStartFields(t, childPath)["appended_prompt"]; ok {
		t.Fatal("child of a resumed parent recorded appended_prompt")
	}
}

func TestBinaryDelegatedChildOfForkedSessionCarriesWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := seedUIJSONSessionWithRules(t, bin, workspace, stateDir)
	const changed = "# Workspace rules\n\nThis changed after the session started.\n"
	writeWorkspaceRules(t, workspace, changed)

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_fork",
				`{"intent":"delegate the survey","tasks":[{"agent":"explore","task":"Survey the workspace and report."}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("survey report"))
		case 3:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"delegate the survey"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":45000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-fork", parentID, "-ui-size", "100x30", "-max-turns", "5")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var forkPath, childPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) == parentID+".jsonl" {
			continue
		}
		if _, ok := sessionStartFields(t, path)["parent_call_id"]; ok {
			childPath = path
		} else {
			forkPath = path
		}
	}
	if forkPath == "" || childPath == "" {
		t.Fatalf("fork = %q, child = %q, want both logs", forkPath, childPath)
	}
	parentStart, _ := recordedSession(t, sessionPathFor(stateDir, parentID))
	forkStart, _ := recordedSession(t, forkPath)
	if forkStart.SystemPrompt != parentStart.SystemPrompt || forkStart.AppendedPrompt != workspaceRules {
		t.Fatalf("fork prompt/appended = %q/%q, want the inherited contract", forkStart.SystemPrompt, forkStart.AppendedPrompt)
	}
	childStart, _ := recordedSession(t, childPath)
	if got := strings.Count(childStart.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("child of a forked parent carries the rules %d times, want once:\n%s", got, childStart.SystemPrompt)
	}
	if !strings.HasSuffix(childStart.SystemPrompt, "\n\n"+workspaceRules) {
		t.Fatalf("child rules are not appended after the role body:\n%s", childStart.SystemPrompt)
	}
	if strings.Contains(childStart.SystemPrompt, changed) {
		t.Fatalf("child of a forked parent re-read the changed file:\n%s", childStart.SystemPrompt)
	}
	if _, ok := sessionStartFields(t, childPath)["appended_prompt"]; ok {
		t.Fatal("child of a forked parent recorded appended_prompt")
	}
}

func TestBinaryForkLegacyLogWithoutRecordedPromptDoesNotSeed(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	parentID := createLegacySessionWithoutRecordedPrompt(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("forked"))
	})
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, nil,
		"-batch", "-fork", parentID, "-task", "continue")
	if exit != 0 {
		t.Fatalf("fork exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var forkPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) != parentID+".jsonl" {
			forkPath = path
		}
	}
	if forkPath == "" {
		t.Fatal("fork log missing")
	}
	forkStart, _ := recordedSession(t, forkPath)
	if strings.Contains(forkStart.SystemPrompt, workspaceRules) {
		t.Fatalf("fork seeded a log with no recorded prompt:\n%s", forkStart.SystemPrompt)
	}
	if !strings.Contains(forkStart.SystemPrompt, "a coding harness working in the workspace") {
		t.Fatalf("fork did not render the recorded version:\n%s", forkStart.SystemPrompt)
	}
	if _, ok := sessionStartFields(t, forkPath)["appended_prompt"]; ok {
		t.Fatal("fork recorded appended_prompt for a log that recorded no prompt")
	}
}

func TestBinaryUIJSONNewAfterResumeReadsWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	seedProvider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	seed := runHarness(t, bin, workspace, stateDir, seedProvider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if seed.exit != 0 {
		t.Fatalf("seed exit = %d\nstderr:\n%s", seed.exit, seed.stderr)
	}
	const changed = "# Workspace rules\n\nFresh contract for the new session.\n"
	writeWorkspaceRules(t, workspace, changed)

	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		t.Errorf("unexpected provider call %d", call)
		http.Error(w, "unexpected call", http.StatusInternalServerError)
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"/new"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-resume", seed.id, "-ui-size", "100x30")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var freshPath string
	for _, path := range sessionFiles(t, stateDir) {
		if filepath.Base(path) != seed.id+".jsonl" {
			freshPath = path
		}
	}
	if freshPath == "" {
		t.Fatal("/new did not create a fresh session log")
	}
	freshStart, _ := recordedSession(t, freshPath)
	if freshStart.AppendedPrompt != changed {
		t.Fatalf("/new appended_prompt = %q, want the changed file", freshStart.AppendedPrompt)
	}
	if got := strings.Count(freshStart.SystemPrompt, changed); got != 1 {
		t.Fatalf("changed rules appear %d times, want once:\n%s", got, freshStart.SystemPrompt)
	}
	if strings.Contains(freshStart.SystemPrompt, workspaceRules) {
		t.Fatalf("/new carried the resumed session's old rules:\n%s", freshStart.SystemPrompt)
	}
	parentStart, _ := recordedSession(t, sessionPathFor(stateDir, seed.id))
	if parentStart.AppendedPrompt != workspaceRules {
		t.Fatalf("parent appended_prompt = %q, want the recorded text unchanged", parentStart.AppendedPrompt)
	}
}

func TestBinaryDelegatedChildCarriesWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeWorkspaceRules(t, workspace, workspaceRules)
	stateDir := t.TempDir()
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_rules",
				`{"intent":"delegate the survey","tasks":[{"agent":"explore","task":"Survey the workspace and report."}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("survey report"))
		case 3:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"delegate the survey"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":45000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash", "-max-turns", "5")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	var parentPath, childPath string
	for _, path := range sessionFiles(t, stateDir) {
		fields := sessionStartFields(t, path)
		if _, ok := fields["parent_call_id"]; ok {
			childPath = path
		} else {
			parentPath = path
		}
	}
	if parentPath == "" || childPath == "" {
		t.Fatalf("parent = %q, child = %q, want a delegated session pair", parentPath, childPath)
	}
	parentStart, _ := recordedSession(t, parentPath)
	childStart, _ := recordedSession(t, childPath)
	if parentStart.AppendedPrompt != workspaceRules {
		t.Fatalf("parent appended_prompt = %q, want the workspace rules", parentStart.AppendedPrompt)
	}
	if childStart.AppendedPrompt != "" {
		t.Fatalf("child appended_prompt = %q, want none", childStart.AppendedPrompt)
	}
	if _, ok := sessionStartFields(t, childPath)["appended_prompt"]; ok {
		t.Fatalf("child session_start records appended_prompt, want provenance only on the parent")
	}
	if !strings.HasPrefix(childStart.SystemPrompt, `You are the acode "explore" agent`) {
		t.Fatalf("child prompt lacks its role identity:\n%s", childStart.SystemPrompt)
	}
	if got := strings.Count(childStart.SystemPrompt, workspaceRules); got != 1 {
		t.Fatalf("child prompt carries the rules %d times, want once:\n%s", got, childStart.SystemPrompt)
	}
	if !strings.HasSuffix(childStart.SystemPrompt, "\n\n"+workspaceRules) {
		t.Fatalf("child rules are not appended after the role body:\n%s", childStart.SystemPrompt)
	}
}
