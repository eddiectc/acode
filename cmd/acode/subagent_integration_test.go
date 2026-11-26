//go:build unix

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

// chatDelegateResponse builds a Chat Completions response that calls delegate.
func chatDelegateResponse(id string, tasks string) map[string]any {
	return map[string]any{
		"id": "chatcmpl-delegate", "object": "chat.completion",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id": id, "type": "function",
					"function": map[string]any{"name": "delegate", "arguments": tasks},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 21, "completion_tokens": 9},
	}
}

// requestToolNames returns the tool names a Chat request offered.
func requestToolNames(t *testing.T, body []byte) []string {
	t.Helper()
	var request struct {
		Tools []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(body, &request); err != nil {
		t.Fatalf("decode request tools: %v", err)
	}
	names := make([]string, 0, len(request.Tools))
	for _, tool := range request.Tools {
		names = append(names, tool.Function.Name)
	}
	return names
}

func TestSubagentEndToEnd(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()

	var (
		mu          sync.Mutex
		parentTools []string
		childTools  []string
	)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			mu.Lock()
			parentTools = requestToolNames(t, body)
			mu.Unlock()
			writeJSON(w, 200, chatDelegateResponse("call_d1",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"Read hello.txt and report its contents."}]}`))
		case 2:
			mu.Lock()
			childTools = requestToolNames(t, body)
			mu.Unlock()
			writeJSON(w, 200, chatFinalResponse("hello.txt contains a greeting"))
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
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-ui-json", "-model", "deepseek-v4.1-flash",
		"-provider-base-url", provider.url,
		"-max-turns", "5")
	cmd.Dir = workspace
	env := testEnvBase()
	env = append(env,
		"ACODE_STATE_DIR="+stateDir,
		"ACODE_PROVIDER_TOKEN=test-token",
		testCatalogEnv(),
	)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	if err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run harness: %v\nstderr:\n%s", err, stderr.String())
		}
	}
	if !strings.Contains(stdout.String(), "tool delegate") {
		t.Fatalf("frames do not show the delegate block:\n%s", stdout.String())
	}

	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 2 {
		t.Fatalf("session logs = %v (%v), want a parent and one child", files, err)
	}
	var parentPath, childPath string
	for _, path := range files {
		events := readSessionEvents(t, path)
		start := events[0]
		var data struct {
			ParentCallID string `json:"parent_call_id"`
		}
		_ = json.Unmarshal(start.Data, &data)
		if data.ParentCallID != "" {
			childPath = path
		} else {
			parentPath = path
		}
	}
	if parentPath == "" || childPath == "" {
		t.Fatalf("parent = %q, child = %q", parentPath, childPath)
	}
	parentID := strings.TrimSuffix(filepath.Base(parentPath), ".jsonl")
	childID := strings.TrimSuffix(filepath.Base(childPath), ".jsonl")

	// The child records the delegation contract and runs as a normal session.
	childEvents := readSessionEvents(t, childPath)
	var childStart struct {
		PromptVersion  string   `json:"prompt_version"`
		ToolsetVersion string   `json:"toolset_version"`
		Agent          string   `json:"agent"`
		Tools          []string `json:"tools"`
		SystemPrompt   string   `json:"system_prompt"`
		ParentSession  string   `json:"parent_session"`
		ParentCallID   string   `json:"parent_call_id"`
	}
	if err := json.Unmarshal(childEvents[0].Data, &childStart); err != nil {
		t.Fatalf("decode child session_start: %v", err)
	}
	if childStart.ParentSession != parentID || childStart.ParentCallID != "call_d1" {
		t.Fatalf("child parent ref = %+v, want parent %s call call_d1", childStart, parentID)
	}
	if childStart.Agent != "explore" || childStart.PromptVersion != agent.PromptVersion || childStart.ToolsetVersion != tools.Version {
		t.Fatalf("child contract = %+v", childStart)
	}
	if strings.Join(childStart.Tools, ",") != "read_file,search" {
		t.Fatalf("child grant = %v", childStart.Tools)
	}
	if !strings.Contains(childStart.SystemPrompt, `You are the acode "explore" agent`) {
		t.Fatalf("child prompt = %q, want the acode identity line", childStart.SystemPrompt)
	}
	final := 0
	for _, ev := range childEvents {
		if ev.Type == "final" {
			final++
		}
	}
	if final != 1 {
		t.Fatalf("child final events = %d, want 1", final)
	}

	// The child's plan excludes delegate, and the parent's includes it.
	mu.Lock()
	gotParent, gotChild := parentTools, childTools
	mu.Unlock()
	if !contains(gotParent, "delegate") {
		t.Fatalf("parent tools = %v, want delegate", gotParent)
	}
	if contains(gotChild, "delegate") {
		t.Fatalf("child tools = %v, want no delegate", gotChild)
	}
	if strings.Join(gotChild, ",") != "read_file,search" {
		t.Fatalf("child request tools = %v, want the explore grant", gotChild)
	}

	// The parent records one result and folds the child's accounting.
	parentEvents := readSessionEvents(t, parentPath)
	var parentStart struct {
		SystemPrompt string `json:"system_prompt"`
	}
	if err := json.Unmarshal(parentEvents[0].Data, &parentStart); err != nil {
		t.Fatalf("decode parent session_start: %v", err)
	}
	if !strings.Contains(parentStart.SystemPrompt, "You are acode, a coding harness working in the workspace") {
		t.Fatalf("parent prompt = %q, want the acode identity line", parentStart.SystemPrompt)
	}
	if !strings.Contains(parentStart.SystemPrompt, "- reviewer: Independent review of a change its author has read and believes correct.") {
		t.Fatalf("parent prompt = %q, want the shipped reviewer description in the catalog", parentStart.SystemPrompt)
	}
	if !strings.Contains(parentStart.SystemPrompt, "Run the relevant tests before reporting the change done") {
		t.Fatalf("parent prompt = %q, want the prompt-v3 verify/report rule", parentStart.SystemPrompt)
	}
	var (
		foundResult   bool
		details       tools.DelegateDetails
		subagentUse   int
		subagentTurns int
	)
	for _, ev := range parentEvents {
		switch ev.Type {
		case "tool_result":
			var data struct {
				Name    string          `json:"name"`
				OK      bool            `json:"ok"`
				Code    string          `json:"code"`
				Details json.RawMessage `json:"details"`
			}
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("decode tool_result: %v", err)
			}
			if data.Name != "delegate" {
				continue
			}
			foundResult = true
			if !data.OK || data.Code != "ok" {
				t.Fatalf("delegate result = ok %v code %s", data.OK, data.Code)
			}
			parsed, ok := tools.ParseDelegateDetails(data.Details)
			if !ok {
				t.Fatalf("delegate details did not decode: %s", data.Details)
			}
			details = parsed
		case "run_end":
			var data struct {
				SubagentTurns int `json:"subagent_turns"`
				SubagentUsage struct {
					InputTokens  int `json:"input_tokens"`
					OutputTokens int `json:"output_tokens"`
				} `json:"subagent_usage"`
			}
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("decode run_end: %v", err)
			}
			subagentTurns = data.SubagentTurns
			subagentUse = data.SubagentUsage.InputTokens + data.SubagentUsage.OutputTokens
		}
	}
	if !foundResult {
		t.Fatal("the parent log has no delegate tool_result")
	}
	if len(details.Tasks) != 1 || details.Tasks[0].Session != childID || details.Tasks[0].Status != "completed" {
		t.Fatalf("delegate details = %+v, want one completed task for %s", details.Tasks, childID)
	}
	if details.SubagentTurns != 1 || subagentTurns != 1 {
		t.Fatalf("subagent turns = details %d run_end %d, want 1", details.SubagentTurns, subagentTurns)
	}
	if subagentUse == 0 || details.SubagentUsage.Total() != subagentUse {
		t.Fatalf("subagent usage = details %d run_end %d, want the same non-zero total", details.SubagentUsage.Total(), subagentUse)
	}
	// The parent log contains no child events.
	for _, ev := range parentEvents {
		if ev.Session != parentID {
			t.Fatalf("parent log contains an event from session %s", ev.Session)
		}
	}
}

func TestSubagentReadOnlyFanOutEndToEnd(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()

	var (
		mu       sync.Mutex
		children int
		once     sync.Once
	)
	release := make(chan struct{})
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_d1",
				`{"intent":"fan out the reads","tasks":[`+
					`{"agent":"explore","task":"one"},`+
					`{"agent":"explore","task":"two"},`+
					`{"agent":"explore","task":"three"}]}`))
		case 2, 3, 4:
			// Hold every child until all three are in flight. A sequential
			// implementation would deadlock here; the timeout turns that into a
			// fast failure instead of a hung test.
			mu.Lock()
			children++
			if children == 3 {
				once.Do(func() { close(release) })
			}
			mu.Unlock()
			select {
			case <-release:
				writeJSON(w, 200, chatFinalResponse("child report"))
			case <-time.After(10 * time.Second):
				writeJSON(w, 500, map[string]any{"error": "children did not overlap"})
			}
		case 5:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})

	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"fan out the reads"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":45000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin,
		"-ui-json", "-model", "deepseek-v4.1-flash",
		"-provider-base-url", provider.url,
		"-max-turns", "5")
	cmd.Dir = workspace
	env := testEnvBase()
	env = append(env,
		"ACODE_STATE_DIR="+stateDir,
		"ACODE_PROVIDER_TOKEN=test-token",
		testCatalogEnv(),
	)
	cmd.Env = env
	cmd.Stdin = strings.NewReader(script)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Fatalf("run harness: %v\nstderr:\n%s", err, stderr.String())
		}
	}

	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 4 {
		t.Fatalf("session logs = %v (%v), want a parent and three children", files, err)
	}
	var parentPath string
	childSessions := make(map[string]bool, 3)
	for _, path := range files {
		events := readSessionEvents(t, path)
		var start struct {
			ParentCallID string `json:"parent_call_id"`
		}
		if err := json.Unmarshal(events[0].Data, &start); err != nil {
			t.Fatalf("decode session_start: %v", err)
		}
		if start.ParentCallID == "" {
			parentPath = path
			continue
		}
		childSessions[strings.TrimSuffix(filepath.Base(path), ".jsonl")] = true
		final := 0
		for _, ev := range events {
			if ev.Type == "final" {
				final++
			}
		}
		if final != 1 {
			t.Fatalf("child %s final events = %d, want 1", path, final)
		}
	}
	if parentPath == "" || len(childSessions) != 3 {
		t.Fatalf("parent = %q, children = %v", parentPath, childSessions)
	}

	var details tools.DelegateDetails
	found := false
	for _, ev := range readSessionEvents(t, parentPath) {
		if ev.Type != "tool_result" {
			continue
		}
		var data struct {
			Name    string          `json:"name"`
			Details json.RawMessage `json:"details"`
		}
		if err := json.Unmarshal(ev.Data, &data); err != nil {
			t.Fatalf("decode tool_result: %v", err)
		}
		if data.Name != "delegate" {
			continue
		}
		parsed, ok := tools.ParseDelegateDetails(data.Details)
		if !ok {
			t.Fatalf("delegate details did not decode: %s", data.Details)
		}
		details = parsed
		found = true
	}
	if !found {
		t.Fatal("the parent log has no delegate tool_result")
	}
	if details.Execution != tools.ExecutionConcurrent {
		t.Fatalf("execution = %q (%q), want concurrent", details.Execution, details.ExecutionReason)
	}
	if len(details.Tasks) != 3 || details.SubagentTurns != 3 {
		t.Fatalf("details = %+v, want three completed tasks", details)
	}
	for _, task := range details.Tasks {
		if task.Status != tools.SubagentCompleted {
			t.Fatalf("task = %+v, want completed", task)
		}
		if !childSessions[task.Session] {
			t.Fatalf("task session %q is not a recorded child", task.Session)
		}
	}
}

func contains(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

// subagentOverrideScript is the same one-submission script the end-to-end
// subagent tests use.
func subagentOverrideScript() string {
	return strings.Join([]string{
		`{"v":1,"type":"text","text":"delegate the read"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":45000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
}

func TestSubagentOverrideChangesChildModelAndCap(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(`{
		"agent_overrides": {"explore": {"model": "kimi-k2.7-code", "max_turns": 2}}
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	var (
		mu         sync.Mutex
		childModel string
	)
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_override",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"Read and report."}]}`))
		case 2:
			var request struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(body, &request)
			mu.Lock()
			childModel = request.Model
			mu.Unlock()
			writeJSON(w, 200, chatFinalResponse("child report"))
		case 3:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url,
		strings.NewReader(subagentOverrideScript()),
		"-ui-json", "-model", "deepseek-v4.1-flash", "-max-turns", "5")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}

	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 2 {
		t.Fatalf("session logs = %v (%v), want a parent and one child", files, err)
	}
	type start struct {
		Model          string   `json:"model"`
		ThinkingEffort string   `json:"thinking_effort"`
		MaxTurns       int      `json:"max_turns"`
		Agent          string   `json:"agent"`
		Tools          []string `json:"tools"`
		ParentCallID   string   `json:"parent_call_id"`
	}
	var parentStart, childStart start
	for _, path := range files {
		events := readSessionEvents(t, path)
		var got start
		if err := json.Unmarshal(events[0].Data, &got); err != nil {
			t.Fatalf("decode session_start: %v", err)
		}
		if got.ParentCallID == "" {
			parentStart = got
		} else {
			childStart = got
		}
	}
	if parentStart.Model != "deepseek-v4.1-flash" {
		t.Fatalf("parent model = %q, want the run model", parentStart.Model)
	}
	if childStart.Agent != "explore" || childStart.Model != "kimi-k2.7-code" {
		t.Fatalf("child start = %+v, want explore on the override model", childStart)
	}
	if childStart.ThinkingEffort != "" {
		t.Fatalf("child effort = %q, want the model default after a model override", childStart.ThinkingEffort)
	}
	if childStart.MaxTurns != 2 {
		t.Fatalf("child max_turns = %d, want the override 2", childStart.MaxTurns)
	}
	if strings.Join(childStart.Tools, ",") != "read_file,search" {
		t.Fatalf("child grant = %v, want the definition grant unchanged", childStart.Tools)
	}
	mu.Lock()
	gotChildModel := childModel
	mu.Unlock()
	if gotChildModel != "kimi-k2.7-code" {
		t.Fatalf("child request model = %q, want the override", gotChildModel)
	}
}

func TestSubagentOverrideUnknownAgentIsConfigError(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(`{
		"agent_overrides": {"nobody": {"model": "kimi-k2.7-code"}}
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 500, map[string]any{"error": "the TUI should fail before any model call"})
	})
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url,
		strings.NewReader(""),
		"-ui-json", "-model", "deepseek-v4.1-flash")
	if exit != 2 {
		t.Fatalf("exit = %d, want a config error\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "nobody") || !strings.Contains(stderr, "agent_overrides") {
		t.Fatalf("stderr = %q, want the offending agent and key", stderr)
	}
}

func TestSubagentOverrideDisablesAgent(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(stateDir, "config.json"), []byte(`{
		"agent_overrides": {"explore": {"enabled": false}}
	}`), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	var calls int
	var mu sync.Mutex
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		mu.Lock()
		calls = call
		mu.Unlock()
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_disabled",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"Read and report."}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("parent continued without a child"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url,
		strings.NewReader(subagentOverrideScript()),
		"-ui-json", "-model", "deepseek-v4.1-flash", "-max-turns", "5")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	mu.Lock()
	gotCalls := calls
	mu.Unlock()
	if gotCalls != 2 {
		t.Fatalf("model calls = %d, want the parent delegate and final only", gotCalls)
	}
	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("session logs = %v (%v), want only the parent", files, err)
	}
	events := readSessionEvents(t, files[0])
	var prompt string
	var delegateResult struct {
		OK     bool
		Code   string
		Error  string
		Output string
	}
	for _, ev := range events {
		switch ev.Type {
		case "session_start":
			var start struct {
				SystemPrompt string `json:"system_prompt"`
			}
			_ = json.Unmarshal(ev.Data, &start)
			prompt = start.SystemPrompt
		case "tool_result":
			var data struct {
				Name   string `json:"name"`
				OK     bool   `json:"ok"`
				Code   string `json:"code"`
				Error  string `json:"error"`
				Output string `json:"output"`
			}
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("decode tool_result: %v", err)
			}
			if data.Name == "delegate" {
				delegateResult.OK = data.OK
				delegateResult.Code = data.Code
				delegateResult.Error = data.Error
				delegateResult.Output = data.Output
			}
		}
	}
	if delegateResult.OK || delegateResult.Code != "not_found" {
		t.Fatalf("delegate result = %+v, want not_found", delegateResult)
	}
	message := delegateResult.Error + delegateResult.Output
	if !strings.Contains(message, `unknown agent "explore"`) {
		t.Fatalf("delegate message = %q, want the disabled name", message)
	}
	if strings.Contains(prompt, "- explore:") {
		t.Fatalf("the prompt still offers the disabled agent:\n%s", prompt)
	}
	if !strings.Contains(prompt, "- reviewer:") {
		t.Fatalf("the prompt lost the remaining agents:\n%s", prompt)
	}
}

func TestBatchCannotDelegate(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			// The model asks for a tool the batch grant withheld. It must see
			// unknown_tool instead of a child session.
			writeJSON(w, 200, chatDelegateResponse("call_d1",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"read hello.txt"}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("done without delegating"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	run := runHarness(t, bin, workspace, stateDir, provider.url,
		"-batch", "-task", "try to delegate", "-model", "deepseek-v4.1-flash", "-max-turns", "4")
	if run.exit != 0 {
		t.Fatalf("exit = %d\nstderr:\n%s", run.exit, run.stderr)
	}
	if strings.Count(run.log, `"type":"session_start"`) != 1 {
		t.Fatalf("a child session was created by a batch run:\n%s", run.log)
	}
	if !strings.Contains(run.log, `"name":"delegate"`) || !strings.Contains(run.log, `"code":"unknown_tool"`) {
		t.Fatalf("the withheld delegate call was not rejected:\n%s", run.log)
	}
	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("session logs = %v (%v), want one", files, err)
	}
	// The recorded root grant stays on the batch session; the session grant
	// remains full so a later TUI resume can still sound out the contract.
	events := readSessionEvents(t, files[0])
	var start struct {
		Tools     []string `json:"tools"`
		RootTools []string `json:"root_tools"`
	}
	if err := json.Unmarshal(events[0].Data, &start); err != nil {
		t.Fatalf("decode session_start: %v", err)
	}
	if start.Tools != nil {
		t.Fatalf("batch session grant = %v, want the full toolset (unset)", start.Tools)
	}
	if len(start.RootTools) == 0 || contains(start.RootTools, "delegate") {
		t.Fatalf("batch root grant = %v, want a recorded list without delegate", start.RootTools)
	}
}

// TestTUIResumeOfBatchSessionCannotDelegate is the grant-split contract: a
// batch-created session records a full session grant and a root grant without
// delegate, and the TUI honours the root grant through the engine-built
// registry. Without root_tools on resume, this run could dispatch a child.
func TestTUIResumeOfBatchSessionCannotDelegate(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()
	batchProvider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("batch done"))
	})
	first := runHarness(t, bin, workspace, stateDir, batchProvider.url,
		"-batch", "-task", "seed the contract", "-model", "deepseek-v4.1-flash")
	if first.exit != 0 {
		t.Fatalf("batch exit = %d\nstderr:\n%s", first.exit, first.stderr)
	}

	tuiProvider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_tui",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"read hello.txt"}]}`))
		case 2:
			writeJSON(w, 200, chatFinalResponse("done without delegating"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"try to delegate"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	_, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, tuiProvider.url, strings.NewReader(script),
		"-ui-json", "-resume", first.id, "-max-output-tokens", "1000", "-max-context-tokens", "200000")
	if exit != 0 {
		t.Fatalf("tui resume exit = %d\nstderr:\n%s", exit, stderr)
	}
	data, err := os.ReadFile(sessionPathFor(stateDir, first.id))
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	log := string(data)
	if strings.Count(log, `"type":"session_start"`) != 1 {
		t.Fatalf("a child session was created by the resumed TUI run:\n%s", log)
	}
	if !strings.Contains(log, `"name":"delegate"`) || !strings.Contains(log, `"code":"unknown_tool"`) {
		t.Fatalf("the recorded root grant did not withhold delegate:\n%s", log)
	}
}

// TestSubagentFocusedChildStreamsLiveDeltas proves the focused child view shows
// delegated text while the child is still streaming. The child's first turn is a
// buffered tool call, so focus opens on a snapshotted turn; its second turn
// streams and the provider holds the stream open until a frame shows the first
// chunk. The child log then records the observed-streaming contract.
func TestSubagentFocusedChildStreamsLiveDeltas(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "hello.txt"), []byte("hello\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	stateDir := t.TempDir()

	var startOnce, streamOnce sync.Once
	childStarted := make(chan struct{})
	releaseChild := make(chan struct{})
	streaming := make(chan struct{})
	releaseStream := make(chan struct{})
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatDelegateResponse("call_live",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"Read hello.txt and report its contents."}]}`))
		case 2:
			// Hold the child's first turn until the test has focused its view.
			startOnce.Do(func() { close(childStarted) })
			<-releaseChild
			writeJSON(w, 200, chatToolCallResponse())
		case 3:
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			writeSSE(t, w, "", `{"choices":[{"index":0,"delta":{"content":"child observed "}}]}`)
			streamOnce.Do(func() { close(streaming) })
			<-releaseStream
			writeSSE(t, w, "", `{"choices":[{"index":0,"delta":{"content":"live tokens."}}]}`)
			writeSSE(t, w, "", `{"choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`)
			writeSSE(t, w, "", `{"choices":[],"usage":{"prompt_tokens":12,"completion_tokens":4}}`)
			writeSSE(t, w, "", `[DONE]`)
		case 4:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})

	h := startInteractiveHarness(t, bin, workspace, stateDir, provider.url, nil,
		"-ui-json", "-ui-size", "120x30", "-model", "deepseek-v4.1-flash", "-max-turns", "6")
	h.send(`{"v":1,"type":"text","text":"delegate the read"}`)
	h.send(`{"v":1,"type":"key","key":"enter"}`)
	h.framesUntil("tool delegate")
	<-childStarted
	h.send(`{"v":1,"type":"text","text":"/agent-hub"}`)
	h.send(`{"v":1,"type":"key","key":"enter"}`)
	h.framesUntil("intent")
	h.send(`{"v":1,"type":"key","key":"enter"}`)
	h.framesUntil("child explore")
	close(releaseChild)
	<-streaming
	// The provider is blocked mid-stream, so the frame below is proof the view
	// shows child text before SubagentFinished exists.
	h.framesUntil("child observed")
	close(releaseStream)
	h.send(`{"v":1,"type":"key","key":"esc"}`)
	h.framesUntil("parent done")
	h.quit()

	// The frames requested before releaseStream prove the view showed child text
	// while the child was mid-stream. Here, check that no such text appeared
	// before the view was focused, and that every focused live frame reports no
	// overlay.
	liveFrames := 0
	sawFocused := false
	var leaked string
	for line := range strings.SplitSeq(strings.TrimSpace(h.output()), "\n") {
		var frame struct {
			Type    string   `json:"type"`
			Overlay string   `json:"overlay"`
			Lines   []string `json:"lines"`
		}
		if err := json.Unmarshal([]byte(line), &frame); err != nil || frame.Type != "frame" {
			continue
		}
		body := strings.Join(frame.Lines, "\n")
		focused := strings.Contains(body, "child explore")
		if strings.Contains(body, "child observed") {
			if !sawFocused && leaked == "" {
				leaked = body
			}
			if focused {
				liveFrames++
				if frame.Overlay != "none" {
					t.Errorf("live child frame overlay = %q, want none", frame.Overlay)
				}
			}
		}
		if focused {
			sawFocused = true
		}
	}
	if leaked != "" {
		t.Fatalf("child text appeared before the focused view:\n%s", leaked)
	}
	if liveFrames == 0 {
		t.Fatalf("no frame showed the streamed child text:\n%s", h.output())
	}

	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 2 {
		t.Fatalf("session logs = %v (%v), want a parent and one child", files, err)
	}
	foundChild := false
	for _, path := range files {
		events := readSessionEvents(t, path)
		var start struct {
			ParentCallID string `json:"parent_call_id"`
		}
		if err := json.Unmarshal(events[0].Data, &start); err != nil {
			t.Fatalf("decode session_start: %v", err)
		}
		if start.ParentCallID == "" {
			continue
		}
		foundChild = true
		if got := countEventType(events, "delta"); got != 0 {
			t.Fatalf("child log contains %d display deltas", got)
		}
		streamed := false
		for _, ev := range events {
			var data struct {
				Stream   bool   `json:"stream"`
				Text     string `json:"text"`
				Streamed bool   `json:"streamed"`
			}
			switch ev.Type {
			case session.TypeRunStart:
				if err := json.Unmarshal(ev.Data, &data); err != nil {
					t.Fatalf("decode run_start: %v", err)
				}
				if !data.Stream {
					t.Fatalf("observed child run_start.stream = false: %s", ev.Data)
				}
			case session.TypeModelResponse:
				if err := json.Unmarshal(ev.Data, &data); err != nil {
					t.Fatalf("decode model_response: %v", err)
				}
				if strings.Contains(data.Text, "child observed") {
					if !data.Streamed {
						t.Fatalf("streamed child response recorded streamed = false: %s", ev.Data)
					}
					streamed = true
				}
			}
		}
		if !streamed {
			t.Fatal("child log has no streamed model_response")
		}
	}
	if !foundChild {
		t.Fatalf("no child session log in %v", files)
	}
}

// TestBatchResumeOfTUISessionCannotDelegate pins the batch side of the
// observation trade. Batch loads no agent definitions, so it has no spawner and
// cannot start a child even when it resumes a TUI-created session whose grant
// still offers delegate. No batch child log can therefore exist, and the batch
// path never registers an observer.
func TestBatchResumeOfTUISessionCannotDelegate(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	stateDir := t.TempDir()

	// One provider serves the seeding TUI run and the batch run, so the recorded
	// base URL stays valid across the resume.
	provider := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		switch call {
		case 1:
			writeJSON(w, 200, chatFinalResponse("seeded"))
		case 2:
			writeJSON(w, 200, chatDelegateResponse("call_batch",
				`{"intent":"delegate the read","tasks":[{"agent":"explore","task":"Read hello.txt and report its contents."}]}`))
		case 3:
			writeJSON(w, 200, chatFinalResponse("parent done"))
		default:
			writeJSON(w, 500, map[string]any{"error": "unexpected model call"})
		}
	})
	seed := runHarnessRawScript(t, bin, workspace, stateDir, provider.url,
		`{"v":1,"type":"text","text":"seed the session"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"quit"}`)
	if seed.exit != 0 {
		t.Fatalf("seed exit = %d\nstdout:\n%s\nstderr:\n%s", seed.exit, seed.stdout, seed.stderr)
	}
	files, err := filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("seed session logs = %v (%v)", files, err)
	}
	sessionID := strings.TrimSuffix(filepath.Base(files[0]), ".jsonl")

	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, provider.url, strings.NewReader(""),
		"-batch", "-resume", sessionID, "-task", "delegate the read",
		"-model", "deepseek-v4.1-flash", "-max-turns", "6")
	if exit != 0 {
		t.Fatalf("batch exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	files, err = filepath.Glob(filepath.Join(stateDir, "sessions", "*.jsonl"))
	if err != nil || len(files) != 1 {
		t.Fatalf("session logs = %v (%v), want only the resumed parent", files, err)
	}
	events := readSessionEvents(t, files[0])
	foundDelegate, streamedRun := false, false
	for _, ev := range events {
		switch ev.Type {
		case session.TypeRunStart:
			// The log also holds the seeding TUI run, which streamed; the last
			// run_start is the batch run under test.
			var data struct {
				Stream bool `json:"stream"`
			}
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("decode run_start: %v", err)
			}
			streamedRun = data.Stream
		case session.TypeToolResult:
			var data struct {
				Name  string `json:"name"`
				Code  string `json:"code"`
				Error string `json:"error"`
			}
			if err := json.Unmarshal(ev.Data, &data); err != nil {
				t.Fatalf("decode tool_result: %v", err)
			}
			if data.Name != "delegate" {
				continue
			}
			foundDelegate = true
			if data.Code != string(tools.CodeUnavailable) || !strings.Contains(data.Error, "no agent definitions") {
				t.Fatalf("batch delegate result = %+v, want unavailable", data)
			}
		}
	}
	if streamedRun {
		t.Fatal("batch run_start.stream = true")
	}
	if !foundDelegate {
		t.Fatal("the batch run never attempted delegate")
	}
}

// runHarnessRawScript runs the binary with a generated -ui-json script.
func runHarnessRawScript(t *testing.T, bin, workspace, stateDir, baseURL string, commands ...string) harnessRun {
	t.Helper()
	script := strings.Join(commands, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, baseURL, strings.NewReader(script),
		"-ui-json", "-ui-size", "120x30", "-model", "deepseek-v4.1-flash", "-max-turns", "6")
	return harnessRun{stdout: stdout, stderr: stderr, exit: exit}
}
