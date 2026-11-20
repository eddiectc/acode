//go:build unix

package main

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

// requestSystemPrompt returns the system message a Chat request carries. The
// projection replays the recorded prompt as the first message.
func requestSystemPrompt(t *testing.T, body []byte) string {
	t.Helper()
	var request struct {
		Messages []struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode provider request: %v\n%s", err, body)
	}
	for _, message := range request.Messages {
		if message.Role == "system" {
			return message.Content
		}
	}
	t.Fatalf("request carries no system message: %s", body)
	return ""
}

// legacyRecordedPrompt stands in for a pre-bump session's recorded prompt. It
// is deliberately not what the current renderer would produce, so a re-render
// is visible in the request and the new log.
const legacyRecordedPrompt = "You are aharness, a coding harness working in the workspace \"/legacy\".\n\n- Legacy v2 rules."

// seedRecordedPromptSession writes a prompt-v2 session whose recorded prompt
// is legacyRecordedPrompt, as a pre-bump log would be.
func seedRecordedPromptSession(t *testing.T, stateDir, workspace string) string {
	t.Helper()
	cfg := testSessionConfig(t, stateDir, workspace, "deepseek-v4.1-flash")
	rcfg := testRunnerConfig(cfg)
	rcfg.PromptVersion = agent.PromptVersionV2
	rcfg.SystemPrompt = legacyRecordedPrompt
	rcfg.Agent = "explore"
	rcfg.Tools = []string{tools.NameReadFile}
	id := session.NewID()
	store, err := session.Create(stateDir, id)
	if err != nil {
		t.Fatalf("create legacy session: %v", err)
	}
	if err := agent.WriteSessionStart(store, rcfg, nil); err != nil {
		_ = store.Close()
		t.Fatalf("write legacy session start: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close legacy session: %v", err)
	}
	return id
}

// TestBinaryBatchPromptShape pins what a batch parent records: the acode
// identity, the prompt-v3 body, and no agent catalog. Batch withholds delegate
// and loads no definitions, so a catalog would advertise roles the parent
// cannot call.
func TestBinaryBatchPromptShape(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	stateDir := filepath.Join(t.TempDir(), "state")
	run := runHarness(t, bin, t.TempDir(), stateDir, provider.url,
		"-batch", "-task", "finish", "-model", "deepseek-v4.1-flash")
	if run.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	start, _ := recordedSession(t, sessionPathFor(stateDir, run.id))
	if start.PromptVersion != agent.PromptVersion || start.ToolsetVersion != tools.Version {
		t.Fatalf("contract versions = %q/%q, want %q/%q",
			start.PromptVersion, start.ToolsetVersion, agent.PromptVersion, tools.Version)
	}
	for _, want := range []string{
		"You are acode, a coding harness working in the workspace",
		"Run the relevant tests before reporting the change done",
	} {
		if !strings.Contains(start.SystemPrompt, want) {
			t.Fatalf("batch prompt lacks %q:\n%s", want, start.SystemPrompt)
		}
	}
	if strings.Contains(start.SystemPrompt, "Available agents:") {
		t.Fatalf("batch prompt carries a catalog but withholds delegate:\n%s", start.SystemPrompt)
	}
	if start.Agent != "" {
		t.Fatalf("batch parent agent = %q, want a root session", start.Agent)
	}
	if start.Tools != nil {
		t.Fatalf("batch session grant = %v, want the full recorded toolset (unset)", start.Tools)
	}
	if slices.Contains(start.RootTools, tools.NameDelegate) || !slices.Contains(start.RootTools, tools.NameReadFile) {
		t.Fatalf("batch root grant = %v, want current tools without delegate", start.RootTools)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != start.SystemPrompt {
		t.Fatalf("request system prompt differs from the recording:\nrequest:\n%s\nrecorded:\n%s", got, start.SystemPrompt)
	}
}

// TestBinaryResumeReplaysRecordedPrompt covers the plan's DEC4 claim: a
// pre-bump session replays its recorded prompt byte-identically, in the log
// and in the model request, instead of re-rendering the current identity.
func TestBinaryResumeReplaysRecordedPrompt(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	id := seedRecordedPromptSession(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("resumed"))
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-resume", id, "-task", "continue")
	if run.exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", run.exit, run.stdout, run.stderr)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != legacyRecordedPrompt {
		t.Fatalf("resumed request system prompt = %q, want the recorded %q", got, legacyRecordedPrompt)
	}
	start, _ := recordedSession(t, sessionPathFor(stateDir, id))
	if start.PromptVersion != agent.PromptVersionV2 || start.SystemPrompt != legacyRecordedPrompt {
		t.Fatalf("resume rewrote the recorded contract: version=%q prompt=%q",
			start.PromptVersion, start.SystemPrompt)
	}
	if start.Agent != "explore" || !slices.Equal(start.Tools, []string{tools.NameReadFile}) {
		t.Fatalf("resume agent/tools = %q/%v, want the recorded explore/read_file", start.Agent, start.Tools)
	}
}

// TestBinaryUIJSONForkInheritsRecordedPrompt covers the TUI fork path, which
// writes its own session_start. It must re-supply the parent's recorded
// prompt: a fresh render there would silently change a pre-bump session's
// contract (and drop the parent's identity line).
func TestBinaryUIJSONForkInheritsRecordedPrompt(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()
	parentID := seedRecordedPromptSession(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		sseFinal(t, w, "forked")
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"continue"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(script),
		"-ui-json", "-fork", parentID, "-ui-size", "100x30", "-model", "deepseek-v4.1-flash",
		"-max-output-tokens", "1000", "-max-context-tokens", "200000")
	if exit != 0 {
		t.Fatalf("fork exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	files := sessionFiles(t, stateDir)
	if len(files) != 2 {
		t.Fatalf("session files = %v, want the parent and one fork", files)
	}
	var childPath string
	for _, path := range files {
		if filepath.Base(path) != parentID+".jsonl" {
			childPath = path
		}
	}
	if childPath == "" {
		t.Fatalf("no fork log in %v", files)
	}
	start, _ := recordedSession(t, childPath)
	if start.PromptVersion != agent.PromptVersionV2 || start.SystemPrompt != legacyRecordedPrompt {
		t.Fatalf("TUI fork contract = %q/%q, want the parent's recorded contract",
			start.PromptVersion, start.SystemPrompt)
	}
	if start.Agent != "explore" || !slices.Equal(start.Tools, []string{tools.NameReadFile}) {
		t.Fatalf("TUI fork agent/tools = %q/%v, want the recorded explore/read_file", start.Agent, start.Tools)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != legacyRecordedPrompt {
		t.Fatalf("TUI fork request system prompt = %q, want the recorded %q", got, legacyRecordedPrompt)
	}
}

// TestBinaryForkInheritsRecordedPrompt pins the SDK's fork rule: a fork copies
// the parent's recorded prompt, identity line included; Identity reaches the
// fork's new children, not its inherited prompt. A fork of a pre-bump session
// therefore keeps that session's contract until a new session is started.
func TestBinaryForkInheritsRecordedPrompt(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := filepath.Join(t.TempDir(), "state")
	parentID := seedRecordedPromptSession(t, stateDir, workspace)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("forked"))
	})
	stdout, stderr, exit := runHarnessRawEnv(t, bin, workspace, stateDir, provider.url, nil, nil,
		"-batch", "-fork", parentID, "-task", "continue")
	if exit != 0 {
		t.Fatalf("fork exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	files := sessionFiles(t, stateDir)
	if len(files) != 2 {
		t.Fatalf("session files = %v, want the parent and one fork", files)
	}
	var childPath string
	for _, path := range files {
		if filepath.Base(path) != parentID+".jsonl" {
			childPath = path
		}
	}
	if childPath == "" {
		t.Fatalf("no fork log in %v", files)
	}
	start, _ := recordedSession(t, childPath)
	if start.PromptVersion != agent.PromptVersionV2 || start.SystemPrompt != legacyRecordedPrompt {
		t.Fatalf("fork contract = %q/%q, want the parent's recorded contract",
			start.PromptVersion, start.SystemPrompt)
	}
	if start.Agent != "explore" {
		t.Fatalf("fork agent = %q, want the recorded explore", start.Agent)
	}
	bodies := providerBodies(provider)
	if len(bodies) != 1 {
		t.Fatalf("provider calls = %d, want 1", len(bodies))
	}
	if got := requestSystemPrompt(t, bodies[0]); got != legacyRecordedPrompt {
		t.Fatalf("fork request system prompt = %q, want the recorded %q", got, legacyRecordedPrompt)
	}
}
