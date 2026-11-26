//go:build unix

package main

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/tools"
)

// writeTestPNG writes one small deterministic PNG into the workspace.
func writeTestPNG(t *testing.T, path string) {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 4, 4))
	for y := range 4 {
		for x := range 4 {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode fixture PNG: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o600); err != nil {
		t.Fatalf("write fixture PNG: %v", err)
	}
}

func readImageToolCallResponse(intent, path string) map[string]any {
	return map[string]any{
		"id": "chatcmpl-image", "object": "chat.completion",
		"choices": []any{map[string]any{
			"index": 0,
			"message": map[string]any{
				"role": "assistant",
				"tool_calls": []any{map[string]any{
					"id": "call_image", "type": "function",
					"function": map[string]any{
						"name":      "read_image",
						"arguments": `{"intent":"` + intent + `","path":"` + path + `"}`,
					},
				}},
			},
			"finish_reason": "tool_calls",
		}},
		"usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 7},
	}
}

// TestBinaryReadImageSendsTheWorkspaceImageToTheModel exercises the toolset-v3
// builtin end to end: the tool is declared to the model, a small workspace PNG
// reaches the next provider request as a data URL, and the session records the
// image beside the one-line caption.
func TestBinaryReadImageSendsTheWorkspaceImageToTheModel(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeTestPNG(t, filepath.Join(workspace, "shot.png"))

	var delivered bool
	model := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		if call == 1 {
			if !slices.Contains(requestToolNames(t, body), "read_image") {
				t.Errorf("first provider request does not declare read_image:\n%s", body)
			}
			writeJSON(w, 200, readImageToolCallResponse("read the screenshot", "shot.png"))
			return
		}
		if !bytes.Contains(body, []byte(`"image_url"`)) || !bytes.Contains(body, []byte(`"data:image/png;base64,`)) {
			t.Errorf("second provider request carries no image part:\n%s", body)
		}
		delivered = true
		writeJSON(w, 200, chatFinalResponse("the image is a red square"))
	})

	run := runHarness(t, bin, workspace, filepath.Join(t.TempDir(), "state"), model.url,
		"-task", "read the screenshot",
		"-model", "deepseek-v4.1-flash",
		"-max-turns", "5",
	)
	if run.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", run.exit, run.stdout, run.stderr)
	}
	if !delivered {
		t.Fatal("the model never received the image")
	}
	if !strings.Contains(run.stdout, "the image is a red square") {
		t.Errorf("stdout = %q, want the final assistant text", run.stdout)
	}
	for _, want := range []string{
		`"type":"tool_result"`,
		`"name":"read_image"`,
		`"code":"ok"`,
		`"media_type":"image/png"`,
		`"toolset_version":"` + tools.Version + `"`,
	} {
		if !strings.Contains(run.log, want) {
			t.Errorf("session log missing %q:\n%s", want, run.log)
		}
	}
}

// sseReadImageToolCall streams one read_image call over Chat SSE, the shape the
// TUI run uses.
func sseReadImageToolCall(t *testing.T, w http.ResponseWriter, path string) {
	t.Helper()
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	writeSSE(t, w, "", `{"choices":[{"index":0,"delta":{"role":"assistant","tool_calls":[{"index":0,"id":"call_image","type":"function","function":{"name":"read_image","arguments":""}}]}}]}`)
	writeSSE(t, w, "", `{"choices":[{"index":0,"delta":{"tool_calls":[{"index":0,"function":{"arguments":"{\"intent\":\"read the screenshot\",\"path\":\"`+path+`\"}"}}]}}]}`)
	writeSSE(t, w, "", `{"choices":[{"index":0,"delta":{},"finish_reason":"tool_calls"}]}`)
	writeSSE(t, w, "", `{"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7}}`)
	writeSSE(t, w, "", `[DONE]`)
}

// TestBinaryReadImageFollowsAMidSessionModelSwitch is the per-run gate: the
// session opens on a vision model, the picker selects a text-only one, and that
// run's read_image reports unavailable instead of delivering an image. A stale
// construction-time gate would deliver it, and a nil per-run capability means
// unknown — which would also deliver it — so this is the direction the wiring
// is the only thing that can fail closed.
func TestBinaryReadImageFollowsAMidSessionModelSwitch(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	workspace := t.TempDir()
	writeTestPNG(t, filepath.Join(workspace, "shot.png"))
	stateDir := t.TempDir()

	model := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		if call == 1 {
			sseReadImageToolCall(t, w, "shot.png")
			return
		}
		if bytes.Contains(body, []byte(`"data:image/png;base64,`)) {
			t.Errorf("the text-only model's request carries an image:\n%s", body)
		}
		sseFinal(t, w, "no image for me")
	})

	script := strings.Join([]string{
		`{"v":1,"type":"key","key":"ctrl-p"}`,
		`{"v":1,"type":"text","text":"kimi-k2.7-code"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"text","text":"read the screenshot"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":20000}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	stdout, stderr, exit := runHarnessRaw(t, bin, workspace, stateDir, model.url, strings.NewReader(script),
		"-ui-json", "-ui-size", "120x40", "-model", "deepseek-v4.1-flash")
	if exit != 0 {
		t.Fatalf("exit = %d\nstdout:\n%s\nstderr:\n%s", exit, stdout, stderr)
	}
	files := sessionFiles(t, stateDir)
	if len(files) != 1 {
		t.Fatalf("session files = %v", files)
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		t.Fatalf("read session log: %v", err)
	}
	log := string(data)
	for _, want := range []string{`"code":"unavailable"`, "does not accept image input"} {
		if !strings.Contains(log, want) {
			t.Errorf("session log missing %q:\n%s", want, log)
		}
	}
	if strings.Contains(log, `"images":[`) {
		t.Errorf("the switch kept the opening model's vision gate:\n%s", log)
	}
}

// TestBinaryReadImageRefusesTextOnlyModelBeforeReading pins the capability
// gate: the fake catalog marks kimi-k2.7-code text-only, so the tool reports
// unavailable without touching the file and no image reaches the provider. The
// path deliberately names no file, so a read would be not_found instead.
func TestBinaryReadImageRefusesTextOnlyModelBeforeReading(t *testing.T) {
	t.Parallel()
	bin := buildHarness(t)
	model := newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
		if call == 1 {
			if !slices.Contains(requestToolNames(t, body), "read_image") {
				t.Errorf("first provider request does not declare read_image:\n%s", body)
			}
			writeJSON(w, 200, readImageToolCallResponse("read the screenshot", "missing.png"))
			return
		}
		if bytes.Contains(body, []byte(`"image_url"`)) {
			t.Errorf("second provider request carries an image for a text-only model:\n%s", body)
		}
		writeJSON(w, 200, chatFinalResponse("no image for me"))
	})

	run := runHarness(t, bin, t.TempDir(), filepath.Join(t.TempDir(), "state"), model.url,
		"-task", "read the screenshot",
		"-model", "kimi-k2.7-code",
		"-max-turns", "5",
	)
	if run.exit != 0 {
		t.Fatalf("exit = %d, want 0\nstdout:\n%s\nstderr:\n%s", run.exit, run.stdout, run.stderr)
	}
	if got := len(providerBodies(model)); got != 2 {
		t.Fatalf("provider calls = %d, want the tool round and the answer", got)
	}
	if !strings.Contains(run.stdout, "no image for me") {
		t.Errorf("stdout = %q, want the final assistant text", run.stdout)
	}
	for _, want := range []string{
		`"name":"read_image"`,
		`"code":"unavailable"`,
		"does not accept image input",
	} {
		if !strings.Contains(run.log, want) {
			t.Errorf("session log missing %q:\n%s", want, run.log)
		}
	}
	for _, unwanted := range []string{`"code":"not_found"`, `"images":[`} {
		if strings.Contains(run.log, unwanted) {
			t.Errorf("session log contains %q:\n%s", unwanted, run.log)
		}
	}
}
