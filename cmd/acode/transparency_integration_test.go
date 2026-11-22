//go:build unix

package main

import (
	"bytes"
	"encoding/json/jsontext"
	jsonv2 "encoding/json/v2"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/session"
)

// uiJSONOutput is the subset of the -ui-json protocol these tests read.
type uiJSONOutput struct {
	frames []uiJSONFrame
	events []string
}

type uiJSONFrame struct {
	overlay string
	text    string
}

func collectUIJSON(t *testing.T, out string) uiJSONOutput {
	t.Helper()
	var collected uiJSONOutput
	for line := range strings.SplitSeq(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		var head struct {
			Type    string   `json:"type"`
			Overlay string   `json:"overlay"`
			Lines   []string `json:"lines"`
			Event   string   `json:"event"`
		}
		if err := jsonv2.Unmarshal([]byte(line), &head); err != nil {
			t.Fatalf("decode protocol line: %v\n%s", err, line)
		}
		switch head.Type {
		case "frame":
			collected.frames = append(collected.frames, uiJSONFrame{overlay: head.Overlay, text: strings.Join(head.Lines, "\n")})
		case "event":
			collected.events = append(collected.events, head.Event)
		}
	}
	return collected
}

// lastOverlay returns the most recent frame with the given overlay value.
func (o uiJSONOutput) lastOverlay(overlay string) (uiJSONFrame, bool) {
	for i := len(o.frames) - 1; i >= 0; i-- {
		if o.frames[i].overlay == overlay {
			return o.frames[i], true
		}
	}
	return uiJSONFrame{}, false
}

// TestBinaryUIJSONPromptShowsRecordedPromptBeforeAnyRun drives /prompt with no
// run in the session: the new session records session_start eagerly, so the
// overlay is available immediately. It also proves the view emits no protocol
// event and closes with esc.
func TestBinaryUIJSONPromptShowsRecordedPromptBeforeAnyRun(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	f := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	workspace := t.TempDir()
	stateDir := t.TempDir()
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"/prompt"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"key","key":"esc"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, f.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	out := collectUIJSON(t, stdout)
	if len(out.events) != 0 {
		t.Fatalf("display commands emitted protocol events: %v", out.events)
	}
	prompt, ok := out.lastOverlay("prompt")
	if !ok {
		t.Fatalf("no prompt frame in:\n%s", stdout)
	}
	for _, want := range []string{
		"system prompt · " + promptVersionForTest(t, stateDir),
		"source:   recorded in session_start",
		"You are acode",
	} {
		if !strings.Contains(prompt.text, want) {
			t.Fatalf("prompt frame lacks %q:\n%s", want, prompt.text)
		}
	}
	if last := out.frames[len(out.frames)-1]; last.overlay != "none" {
		t.Fatalf("last frame overlay = %q, want none", last.overlay)
	}
}

// promptVersionForTest reads the recorded prompt version from the one session
// log these tests create.
func promptVersionForTest(t *testing.T, stateDir string) string {
	t.Helper()
	files := sessionFiles(t, stateDir)
	if len(files) != 1 {
		t.Fatalf("session files = %v", files)
	}
	start, _ := recordedSession(t, files[0])
	return start.PromptVersion
}

// TestBinaryUIJSONRequestMatchesRecordedEvent proves the overlay shows the
// projected request the session log recorded, byte-counted from the same raw
// payload: the request section header equals the indented recorded event.
func TestBinaryUIJSONRequestMatchesRecordedEvent(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	f := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	workspace := t.TempDir()
	stateDir := t.TempDir()
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"say hi"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"text","text":"/request"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, f.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash", "-log-requests")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	out := collectUIJSON(t, stdout)
	if strings.Contains(strings.Join(out.events, ","), "request") {
		t.Fatalf("overlay emitted a protocol event: %v", out.events)
	}
	request, ok := out.lastOverlay("request")
	if !ok {
		t.Fatalf("no request frame in:\n%s", stdout)
	}

	files := sessionFiles(t, stateDir)
	if len(files) != 1 {
		t.Fatalf("session files = %v", files)
	}
	events := readSessionEvents(t, files[0])
	var recorded session.Event
	for _, ev := range events {
		if ev.Type == session.TypeModelRequest {
			recorded = ev
		}
	}
	if recorded.Seq == 0 {
		t.Fatal("the log has no model_request event with -log-requests")
	}
	payload := jsontext.Value(bytes.Clone(recorded.Data))
	if err := payload.Indent(jsontext.WithIndent("  ")); err != nil {
		t.Fatalf("indent recorded payload: %v", err)
	}
	for _, want := range []string{
		fmt.Sprintf("request  seq %d · %d bytes", recorded.Seq, len(payload)),
		`"model": "deepseek-v4.1-flash"`,
		"last model attempt · run 1 · turn 1 · projected",
	} {
		if !strings.Contains(request.text, want) {
			t.Fatalf("request frame lacks %q:\n%s", want, request.text)
		}
	}
}

// TestBinaryUIJSONRequestWithoutLogRequests keeps the response-only path
// honest: the response is shown, the request section says it was not recorded,
// and the hint names the switch and the restart.
func TestBinaryUIJSONRequestWithoutLogRequests(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	f := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		writeJSON(w, 200, chatFinalResponse("done"))
	})
	workspace := t.TempDir()
	stateDir := t.TempDir()
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"say hi"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"text","text":"/request"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, f.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "100x30", "-model", "deepseek-v4.1-flash")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	out := collectUIJSON(t, stdout)
	request, ok := out.lastOverlay("request")
	if !ok {
		t.Fatalf("no request frame in:\n%s", stdout)
	}
	for _, want := range []string{
		"request  not recorded for this turn",
		"-log-requests",
		"next startup",
		"response seq ",
		`"text": "done"`,
	} {
		if !strings.Contains(request.text, want) {
			t.Fatalf("response-only frame lacks %q:\n%s", want, request.text)
		}
	}
}
