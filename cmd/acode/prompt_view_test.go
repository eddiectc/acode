package main

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/eddiectc/acode/internal/tui"
	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

// testSessionPath is the SDK's session log layout under a state directory.
func testSessionPath(stateDir, id string) string {
	return filepath.Join(stateDir, "sessions", id+".jsonl")
}

// writeSessionLog writes one valid v2 JSONL log from the supplied events,
// filling the envelope fields the reader validates.
func writeSessionLog(t *testing.T, events ...session.Event) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "session.jsonl")
	var buf bytes.Buffer
	for i := range events {
		ev := events[i]
		ev.V = session.Version
		ev.Seq = uint64(i + 1)
		ev.Session = "0190a0c4-0000-7000-8000-0000000000aa"
		line, err := jsonv2.Marshal(ev)
		if err != nil {
			t.Fatalf("marshal event %d: %v", i, err)
		}
		buf.Write(line)
		buf.WriteByte('\n')
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write session log: %v", err)
	}
	return path
}

func newSessionEvent(t *testing.T, typ session.EventType, turn int, payload any) session.Event {
	t.Helper()
	ev, err := session.NewEvent(typ, turn, "", payload)
	if err != nil {
		t.Fatalf("build %s event: %v", typ, err)
	}
	return ev
}

// legacyStart is a session_start written before the resolved prompt was
// recorded.
type legacyStart struct {
	PromptVersion  string `json:"prompt_version"`
	ToolsetVersion string `json:"toolset_version"`
	Workspace      string `json:"workspace"`
	Provider       string `json:"provider"`
	Model          string `json:"model"`
}

func TestPromptViewFromRecordedSession(t *testing.T) {
	stateDir, workspace := t.TempDir(), t.TempDir()
	id := createTestSession(t, stateDir, workspace, "deepseek-v4.1-flash", "", "")
	path := testSessionPath(stateDir, id)

	view, err := promptView(path, nil)
	if err != nil {
		t.Fatalf("promptView: %v", err)
	}
	events, err := session.ReadSnapshot(path)
	if err != nil {
		t.Fatalf("ReadSnapshot: %v", err)
	}
	state, err := agent.Inspect(events)
	if err != nil {
		t.Fatalf("Inspect: %v", err)
	}
	if view.Text != state.SystemPrompt || view.Version != state.PromptVersion {
		t.Fatalf("view text length = %d, version = %q; want %d/%q",
			len(view.Text), view.Version, len(state.SystemPrompt), state.PromptVersion)
	}
	if view.Bytes != len(state.SystemPrompt) {
		t.Fatalf("bytes = %d, want %d", view.Bytes, len(state.SystemPrompt))
	}
	if view.Reconstructed || view.Capped || view.Tag != state.PromptTag {
		t.Fatalf("flags = reconstructed %v capped %v tag %q", view.Reconstructed, view.Capped, view.Tag)
	}
	if !strings.Contains(view.Text, "You are acode") {
		t.Fatalf("recorded prompt lacks the acode identity:\n%s", view.Text)
	}
}

func TestPromptViewReconstructsLegacyLog(t *testing.T) {
	workspace := t.TempDir()
	path := writeSessionLog(t, newSessionEvent(t, session.TypeSessionStart, 0, legacyStart{
		PromptVersion:  agent.PromptVersionV2,
		ToolsetVersion: tools.Version,
		Workspace:      workspace,
		Provider:       providerName,
		Model:          "deepseek-v4.1-flash",
	}))

	view, err := promptView(path, nil)
	if err != nil {
		t.Fatalf("promptView: %v", err)
	}
	if !view.Reconstructed {
		t.Fatal("legacy prompt is not labelled reconstructed")
	}
	want, err := agent.RenderSystemPrompt(agent.PromptVersionV2, workspace, acodeIdentity, nil)
	if err != nil {
		t.Fatalf("RenderSystemPrompt: %v", err)
	}
	if view.Text != want || view.Bytes != len(want) {
		t.Fatalf("rebuilt prompt differs from the public renderer (%d vs %d bytes)", len(view.Text), len(want))
	}
	if view.Version != agent.PromptVersionV2 {
		t.Fatalf("version = %q, want %q", view.Version, agent.PromptVersionV2)
	}
}

func TestPromptViewUnsupportedVersion(t *testing.T) {
	path := writeSessionLog(t, newSessionEvent(t, session.TypeSessionStart, 0, legacyStart{
		PromptVersion:  "prompt-v99",
		ToolsetVersion: tools.Version,
		Workspace:      t.TempDir(),
		Provider:       providerName,
		Model:          "deepseek-v4.1-flash",
	}))
	// RenderSystemPrompt supports v0/v1/v2/v3; v99 must be a wrapped error, not
	// a panic and not a fabricated body.
	if _, err := agent.RenderSystemPrompt("prompt-v99", "", acodeIdentity, nil); err == nil {
		t.Fatal("the SDK now renders prompt-v99; update this test's sentinel version")
	}
	view, err := promptView(path, nil)
	if err == nil {
		t.Fatal("promptView accepted an unsupported prompt version")
	}
	if view.Version != "prompt-v99" {
		t.Fatalf("version = %q, want the recorded version", view.Version)
	}
	if view.Text != "" {
		t.Fatalf("promptView fabricated prompt text:\n%s", view.Text)
	}
	if !strings.Contains(err.Error(), "prompt-v99") {
		t.Fatalf("error does not name the version: %v", err)
	}
}

func TestPromptViewMissingSessionStart(t *testing.T) {
	path := writeSessionLog(t, newSessionEvent(t, session.TypeRunStart, 0, struct{}{}))
	view, err := promptView(path, nil)
	if err == nil {
		t.Fatal("promptView accepted a log without session_start")
	}
	if !errors.Is(err, session.ErrCorrupt) {
		t.Fatalf("error = %v, want a wrapped session.ErrCorrupt", err)
	}
	if view.Text != "" || view.Version != "" {
		t.Fatalf("view = %+v, want the zero value", view)
	}
}

func TestPromptViewCappedPrompt(t *testing.T) {
	stateDir, workspace := t.TempDir(), t.TempDir()
	id := createTestSessionWithContract(t, stateDir, workspace, "deepseek-v4.1-flash", func(cfg *agent.Config) {
		cfg.SystemPrompt = strings.Repeat("x", 40<<10)
	})
	view, err := promptView(testSessionPath(stateDir, id), nil)
	if err != nil {
		t.Fatalf("promptView: %v", err)
	}
	if !view.Capped {
		t.Fatal("a prompt recorded over the SDK cap is not marked capped")
	}
	if len(view.Text) >= 40<<10 {
		t.Fatalf("recorded prompt is %d bytes, want the SDK cap applied", len(view.Text))
	}
	if view.Bytes != len(view.Text) {
		t.Fatalf("bytes = %d, want the recorded length %d", view.Bytes, len(view.Text))
	}
	if view.Reconstructed {
		t.Fatal("a recorded prompt is labelled reconstructed")
	}
}

func TestExchangeViewSelectsAndFormats(t *testing.T) {
	start := newSessionEvent(t, session.TypeSessionStart, 0, legacyStart{
		PromptVersion:  agent.PromptVersion,
		ToolsetVersion: tools.Version,
		Workspace:      t.TempDir(),
		Provider:       providerName,
		Model:          "deepseek-v4.1-flash",
	})
	run := newSessionEvent(t, session.TypeRunStart, 0, struct{}{})
	request := newSessionEvent(t, session.TypeModelRequest, 1, struct {
		Model    string   `json:"model"`
		Messages []string `json:"messages"`
	}{Model: "m", Messages: []string{}})
	response := newSessionEvent(t, session.TypeModelResponse, 1, struct {
		Text string `json:"text"`
	}{Text: "hi"})
	path := writeSessionLog(t, start, run, request, response)

	view, err := exchangeView(path, false)
	if err != nil {
		t.Fatalf("exchangeView: %v", err)
	}
	if !view.HasRequest || !view.HasResponse {
		t.Fatalf("flags = request %v response %v", view.HasRequest, view.HasResponse)
	}
	if view.RequestSeq != 3 || view.ResponseSeq != 4 || view.Run != 1 || view.Turn != 1 {
		t.Fatalf("view = %+v", view)
	}
	const wantRequest = "{\n  \"model\": \"m\",\n  \"messages\": []\n}"
	if view.Request != wantRequest {
		t.Fatalf("request = %q, want %q", view.Request, wantRequest)
	}
	if view.Response != "{\n  \"text\": \"hi\"\n}" {
		t.Fatalf("response = %q", view.Response)
	}
	if view.LogRequests {
		t.Fatal("log_requests = true, want the supplied setting")
	}

	// A newest request is the in-flight or failed case: no synthetic response.
	inFlight := newSessionEvent(t, session.TypeModelRequest, 2, struct {
		Model string `json:"model"`
	}{Model: "m"})
	view, err = exchangeView(writeSessionLog(t, start, run, request, response, inFlight), true)
	if err != nil {
		t.Fatalf("exchangeView: %v", err)
	}
	if !view.HasRequest || view.HasResponse || view.ResponseSeq != 0 || view.Turn != 2 {
		t.Fatalf("in-flight view = %+v", view)
	}

	// Logging off: the response stands alone.
	view, err = exchangeView(writeSessionLog(t, start, run, response), false)
	if err != nil {
		t.Fatalf("exchangeView: %v", err)
	}
	if view.HasRequest || !view.HasResponse {
		t.Fatalf("logging-off view = %+v", view)
	}

	// A request recorded by an earlier failed run is never paired across the
	// new run_start.
	laterResponse := newSessionEvent(t, session.TypeModelResponse, 1, struct {
		Text string `json:"text"`
	}{Text: "later"})
	view, err = exchangeView(writeSessionLog(t, start, run, request, run, laterResponse), false)
	if err != nil {
		t.Fatalf("exchangeView: %v", err)
	}
	if view.HasRequest || !view.HasResponse || view.Run != 2 {
		t.Fatalf("cross-run view = %+v", view)
	}
}

func TestExchangeViewEmptyAndCorrupt(t *testing.T) {
	start := newSessionEvent(t, session.TypeSessionStart, 0, legacyStart{
		PromptVersion:  agent.PromptVersion,
		ToolsetVersion: tools.Version,
		Workspace:      t.TempDir(),
		Provider:       providerName,
		Model:          "deepseek-v4.1-flash",
	})
	view, err := exchangeView(writeSessionLog(t, start, newSessionEvent(t, session.TypeRunStart, 0, struct{}{})), false)
	if err != nil {
		t.Fatalf("exchangeView on a run with no model events: %v", err)
	}
	if view.HasRequest || view.HasResponse || view.Run != 0 || view.Turn != 0 {
		t.Fatalf("empty view = %+v", view)
	}

	// A recorded model payload must be a JSON object; anything else is a
	// corrupt-log error, never a panic.
	bad := newSessionEvent(t, session.TypeModelRequest, 1, []string{"not", "an", "object"})
	if _, err := exchangeView(writeSessionLog(t, start, bad), false); !errors.Is(err, session.ErrCorrupt) {
		t.Fatalf("error = %v, want a wrapped session.ErrCorrupt", err)
	}
}

// TestExchangeViewReadsWhileStoreAppends exercises the documented same-process
// path under -race: a real Store holds the exclusive lock and keeps appending
// while Exchange reads through session.ReadSnapshot. Every wait is bounded by a
// timeout, so a lock regression fails fast instead of hanging or looping on a
// growing log.
func TestExchangeViewReadsWhileStoreAppends(t *testing.T) {
	stateDir := t.TempDir()
	id := session.NewID()
	store, err := session.Create(stateDir, id)
	if err != nil {
		t.Fatalf("create store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	cfg := testSessionConfig(t, stateDir, t.TempDir(), "deepseek-v4.1-flash")
	if err := agent.WriteSessionStart(store, testRunnerConfig(cfg), nil); err != nil {
		t.Fatalf("write session start: %v", err)
	}
	path := store.Path()

	const (
		pairs = 100
		reads = 10
	)
	// firstRead makes the overlap explicit: the writer starts a second attempt
	// only after Exchange has returned a consistent prefix once.
	firstRead := make(chan struct{})
	writerDone := make(chan error, 1)
	go func() {
		for turn := 1; turn <= pairs; turn++ {
			request, err := session.NewEvent(session.TypeModelRequest, turn, "", struct {
				Turn int `json:"turn"`
			}{Turn: turn})
			if err != nil {
				writerDone <- err
				return
			}
			if err := store.Append(request); err != nil {
				writerDone <- err
				return
			}
			response, err := session.NewEvent(session.TypeModelResponse, turn, "", struct {
				Text string `json:"text"`
			}{Text: "ok"})
			if err != nil {
				writerDone <- err
				return
			}
			if err := store.Append(response); err != nil {
				writerDone <- err
				return
			}
			if turn == 1 {
				<-firstRead
			}
		}
		writerDone <- nil
	}()

	readDone := make(chan error, 1)
	go func() {
		for i := range reads {
			view, err := exchangeView(path, true)
			if err != nil {
				readDone <- err
				return
			}
			// Every snapshot is a durable prefix: a paired request is
			// recorded before its response.
			if view.HasRequest && view.HasResponse && view.RequestSeq >= view.ResponseSeq {
				readDone <- fmt.Errorf("paired seqs out of order: request %d response %d", view.RequestSeq, view.ResponseSeq)
				return
			}
			if i == 0 {
				close(firstRead)
			}
		}
		readDone <- nil
	}()

	select {
	case err := <-readDone:
		if err != nil {
			t.Fatalf("exchangeView while appending: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("exchangeView did not finish while the store appended; the read blocked on the store lock")
	}
	select {
	case err := <-writerDone:
		if err != nil {
			t.Fatalf("store writer: %v", err)
		}
	case <-time.After(20 * time.Second):
		t.Fatal("the store writer did not finish")
	}
}

// TestTUIHostPromptReplaysResumedAndForkedRecording proves the view reads the
// active session's own session_start: a resume replays the recorded legacy
// prompt verbatim, and a fork records its own prompt under the new contract.
func TestTUIHostPromptReplaysResumedAndForkedRecording(t *testing.T) {
	stateDir, workspace := t.TempDir(), t.TempDir()
	const legacyPrompt = "You are aharness, a legacy v2 prompt.\n\n- Legacy rules."
	parentID := createTestSessionWithContract(t, stateDir, workspace, "deepseek-v4.1-flash", func(cfg *agent.Config) {
		cfg.PromptVersion = agent.PromptVersionV2
		cfg.SystemPrompt = legacyPrompt
	})

	resumeCfg := testSessionConfig(t, stateDir, workspace, "deepseek-v4.1-flash")
	resumeCfg.ResumeID = parentID
	resumeHost, err := engine.Open(context.Background(), tuiEngineSpec(resumeCfg, engine.ModeResume, parentID, "", nil))
	if err != nil {
		t.Fatalf("engine.Open resume: %v", err)
	}
	resumed := &tuiHost{host: resumeHost, cfg: resumeCfg, next: selectionFromSpec(resumeHost.Spec())}
	view, err := resumed.Prompt()
	if err != nil {
		t.Fatalf("Prompt on a resumed session: %v", err)
	}
	if view.Text != legacyPrompt || view.Reconstructed {
		t.Fatalf("resume did not replay the recording (reconstructed %v):\n%s", view.Reconstructed, view.Text)
	}
	if view.Version != agent.PromptVersionV2 {
		t.Fatalf("version = %q, want the recorded %q", view.Version, agent.PromptVersionV2)
	}
	if err := resumed.Close(); err != nil {
		t.Fatalf("close resumed host: %v", err)
	}

	// The fork writes its own session_start and copies the parent history.
	// acode never overrides the prompt on fork, so the records match; /prompt
	// must read the fork's log rather than the parent's.
	forkCfg := testSessionConfig(t, stateDir, workspace, "deepseek-v4.1-flash")
	forkCfg.ForkID = parentID
	forkHost, err := engine.Open(context.Background(), tuiEngineSpec(forkCfg, engine.ModeFork, parentID, "", nil))
	if err != nil {
		t.Fatalf("engine.Open fork: %v", err)
	}
	forked := &tuiHost{host: forkHost, cfg: forkCfg, next: selectionFromSpec(forkHost.Spec())}
	t.Cleanup(func() { _ = forked.Close() })
	view, err = forked.Prompt()
	if err != nil {
		t.Fatalf("Prompt on a forked session: %v", err)
	}
	if view.Text != legacyPrompt || view.Reconstructed || view.Version != agent.PromptVersionV2 {
		t.Fatalf("fork prompt = %q/%q (reconstructed %v), want the recorded legacy prompt",
			view.Version, view.Text, view.Reconstructed)
	}
	if forked.host.Path() == testSessionPath(stateDir, parentID) {
		t.Fatal("fork opened the parent log")
	}
}

// TestTUIHostPromptFollowsSessionSwitch proves the prompt read follows the
// active engine host and that a new session records its own prompt eagerly,
// before the first run.
func TestTUIHostPromptFollowsSessionSwitch(t *testing.T) {
	stateDir, workspace := t.TempDir(), t.TempDir()
	cfg := testSessionConfig(t, stateDir, workspace, "deepseek-v4.1-flash")
	host, err := engine.Open(context.Background(), tuiEngineSpec(cfg, engine.ModeNew, "", "", nil))
	if err != nil {
		t.Fatalf("engine.Open: %v", err)
	}
	resolved := host.Spec()
	th := &tuiHost{
		host: host,
		cfg:  cfg,
		next: selectionFromSpec(resolved),
		info: sessionInfo(cfg, host, agent.State{}),
	}
	t.Cleanup(func() { _ = th.Close() })

	view, err := th.Prompt()
	if err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if !strings.Contains(view.Text, "You are acode") {
		t.Fatalf("new session prompt lacks the identity:\n%s", view.Text)
	}
	first := host.ID()

	// The new session reads the workspace instructions, so its recorded prompt
	// differs from the first one: a stale log path cannot pass this test.
	const instructions = "Use tabs for indentation.\n"
	if err := os.WriteFile(filepath.Join(workspace, "AGENTS.md"), []byte(instructions), 0o600); err != nil {
		t.Fatalf("write workspace instructions: %v", err)
	}
	snapshot, err := th.NewSession(tui.RunOptions{})
	if err != nil {
		t.Fatalf("NewSession: %v", err)
	}
	if snapshot.Info.SessionID == first {
		t.Fatal("NewSession kept the previous session")
	}
	switched, err := th.Prompt()
	if err != nil {
		t.Fatalf("Prompt after switch: %v", err)
	}
	if switched.Text == view.Text {
		t.Fatal("the prompt did not follow the new session log")
	}
	if !strings.Contains(switched.Text, "Use tabs for indentation.") {
		t.Fatalf("switched prompt lacks the new session's instructions:\n%s", switched.Text)
	}
	if switched.Version != view.Version {
		t.Fatalf("switched prompt version = %q, want %q", switched.Version, view.Version)
	}
	exchange, err := th.Exchange()
	if err != nil {
		t.Fatalf("Exchange: %v", err)
	}
	if exchange.HasRequest || exchange.HasResponse {
		t.Fatalf("a fresh session already has a model exchange: %+v", exchange)
	}
}
