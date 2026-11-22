package tui

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// promptHost is a host with the optional /prompt and /request capabilities.
type promptHost struct {
	*manualHost
	prompt      PromptView
	promptErr   error
	exchange    ExchangeView
	exchangeErr error
}

func (h *promptHost) Prompt() (PromptView, error)     { return h.prompt, h.promptErr }
func (h *promptHost) Exchange() (ExchangeView, error) { return h.exchange, h.exchangeErr }

// promptOnlyHost implements /prompt and not /request, so the capability
// boundary is exercised instead of a nil view.
type promptOnlyHost struct {
	*manualHost
	prompt PromptView
}

func (h *promptOnlyHost) Prompt() (PromptView, error) { return h.prompt, nil }

func overlayText(app *App) string { return strings.Join(app.overlay.help, "\n") }

func TestSlashPromptShowsRecordedPrompt(t *testing.T) {
	host := &promptHost{manualHost: newManualHost(), prompt: PromptView{
		Text:    "You are acode, a coding harness.\nSecond line.",
		Version: "prompt-v3",
		Bytes:   46,
	}}
	app := newTestApp(t, host)

	if cmd := submitComposer(t, app, "/prompt"); cmd != nil {
		t.Fatalf("/prompt returned %v, want no command", cmd)
	}
	if app.overlay.kind != OverlayPrompt {
		t.Fatalf("overlay = %q, want prompt (notice %q)", app.overlay.kind, app.notice)
	}
	if app.overlay.title != "system prompt · prompt-v3" {
		t.Fatalf("title = %q", app.overlay.title)
	}
	text := overlayText(app)
	for _, want := range []string{
		"prompt:   prompt-v3",
		"bytes:    46",
		"source:   recorded in session_start",
		"You are acode, a coding harness.",
		"Second line.",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt overlay is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "reconstructed") || strings.Contains(text, "capped:") {
		t.Fatalf("recorded prompt is labelled as rebuilt or capped:\n%s", text)
	}
	if _, _, ok := host.last(); ok {
		t.Fatal("/prompt started a run")
	}

	press(t, app, "esc")
	if app.overlay.kind != OverlayNone {
		t.Fatalf("overlay = %q after esc, want none", app.overlay.kind)
	}
}

func TestSlashPromptMarksReconstructedCappedPrompt(t *testing.T) {
	host := &promptHost{manualHost: newManualHost(), prompt: PromptView{
		Text:          "rebuilt body",
		Version:       "prompt-v2",
		Tag:           "tier-1",
		Bytes:         12,
		Reconstructed: true,
		Capped:        true,
	}}
	app := newTestApp(t, host)

	submitComposer(t, app, "/prompt")
	if app.overlay.kind != OverlayPrompt {
		t.Fatalf("overlay = %q, want prompt", app.overlay.kind)
	}
	text := overlayText(app)
	for _, want := range []string{
		"prompt:   prompt-v2",
		"tag:      tier-1",
		"reconstructed from the prompt version; the log predates the recorded prompt",
		"capped:   the recorded prompt was truncated by the SDK prompt cap",
		"rebuilt body",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt overlay is missing %q:\n%s", want, text)
		}
	}
}

func TestSlashPromptUnavailableAndReadErrors(t *testing.T) {
	app := newTestApp(t, newManualHost())
	submitComposer(t, app, "/prompt")
	if app.overlay.kind != OverlayNone || !strings.Contains(app.notice, "unavailable") {
		t.Fatalf("overlay = %q notice = %q, want an unavailable notice", app.overlay.kind, app.notice)
	}

	readErr := &promptHost{manualHost: newManualHost(), promptErr: fmt.Errorf("read session for prompt: %w", errors.New("boom"))}
	app = newTestApp(t, readErr)
	submitComposer(t, app, "/prompt")
	if app.overlay.kind != OverlayNone || !strings.Contains(app.notice, "system prompt:") {
		t.Fatalf("overlay = %q notice = %q, want a status notice", app.overlay.kind, app.notice)
	}

	renderErr := &promptHost{manualHost: newManualHost(),
		prompt:    PromptView{Version: "prompt-v9"},
		promptErr: fmt.Errorf("reconstruct prompt version %q: unsupported", "prompt-v9"),
	}
	app = newTestApp(t, renderErr)
	submitComposer(t, app, "/prompt")
	if app.overlay.kind != OverlayPrompt {
		t.Fatalf("overlay = %q, want prompt for an unrenderable version", app.overlay.kind)
	}
	text := overlayText(app)
	for _, want := range []string{"prompt:   prompt-v9", "prompt text unavailable:", "unsupported"} {
		if !strings.Contains(text, want) {
			t.Fatalf("prompt overlay is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "source:") || strings.Contains(text, "bytes:") {
		t.Fatalf("unrenderable prompt claims a body it does not have:\n%s", text)
	}
}

func TestPromptOverlaySurvivesAStackedHelpOverlay(t *testing.T) {
	host := &promptHost{manualHost: newManualHost(), prompt: PromptView{
		Text: "recorded body", Version: "prompt-v3", Bytes: 13,
	}}
	app := newTestApp(t, host)
	submitComposer(t, app, "/prompt")
	promptText := overlayText(app)

	press(t, app, "f1")
	if app.overlay.kind != OverlayHelp {
		t.Fatalf("overlay = %q, want help", app.overlay.kind)
	}
	press(t, app, "esc")
	if app.overlay.kind != OverlayPrompt {
		t.Fatalf("overlay = %q after esc, want prompt", app.overlay.kind)
	}
	if app.overlay.title != "system prompt · prompt-v3" || overlayText(app) != promptText {
		t.Fatalf("stacked overlay lost the prompt view: title %q", app.overlay.title)
	}
}

func TestSlashRequestShowsProjectedPair(t *testing.T) {
	request := "{\n  \"model\": \"deepseek-v4.1-flash\",\n  \"messages\": []\n}"
	response := "{\n  \"text\": \"hello\",\n  \"usage\": {\"input_tokens\": 9}\n}"
	host := &promptHost{manualHost: newManualHost(), exchange: ExchangeView{
		Request: request, Response: response,
		RequestSeq: 42, ResponseSeq: 43,
		Run: 2, Turn: 3,
		HasRequest: true, HasResponse: true,
		LogRequests: true,
	}}
	app := newTestApp(t, host)

	submitComposer(t, app, "/request")
	if app.overlay.kind != OverlayRequest {
		t.Fatalf("overlay = %q, want request (notice %q)", app.overlay.kind, app.notice)
	}
	if app.overlay.title != "last model attempt · run 2 · turn 3 · projected" {
		t.Fatalf("title = %q", app.overlay.title)
	}
	text := overlayText(app)
	for _, want := range []string{
		fmt.Sprintf("request  seq 42 · %d bytes", len(request)),
		fmt.Sprintf("response seq 43 · %d bytes", len(response)),
		`"model": "deepseek-v4.1-flash"`,
		`"text": "hello"`,
		"projected payloads; HTTP headers and exact wire bytes are not recorded",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("request overlay is missing %q:\n%s", want, text)
		}
	}
	if _, _, ok := host.last(); ok {
		t.Fatal("/request started a run")
	}

	press(t, app, "esc")
	if app.overlay.kind != OverlayNone {
		t.Fatalf("overlay = %q after esc, want none", app.overlay.kind)
	}
}

func TestSlashRequestHonestAboutMissingSides(t *testing.T) {
	response := "{\n  \"text\": \"no request logged\"\n}"
	host := &promptHost{manualHost: newManualHost(), exchange: ExchangeView{
		Response: response, ResponseSeq: 7, Run: 1, Turn: 2,
		HasResponse: true,
	}}
	app := newTestApp(t, host)

	submitComposer(t, app, "/request")
	if app.overlay.kind != OverlayRequest {
		t.Fatalf("overlay = %q, want request", app.overlay.kind)
	}
	text := overlayText(app)
	for _, want := range []string{
		"request  not recorded for this turn",
		"-log-requests",
		"restart",
		"response seq 7",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("response-only overlay is missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(text, "request  seq") {
		t.Fatalf("response-only overlay claims a request section:\n%s", text)
	}
	press(t, app, "esc")

	// With logging on, the hint says a later turn records one instead of
	// telling the operator to enable the setting again.
	host.exchange.LogRequests = true
	submitComposer(t, app, "/request")
	if text := overlayText(app); !strings.Contains(text, "log_requests is on; a later turn records one.") {
		t.Fatalf("logging-on hint is wrong:\n%s", text)
	}
	press(t, app, "esc")

	// An in-flight or failed request has no response yet; that is not an error.
	host.exchange = ExchangeView{
		Request: "{\n  \"messages\": []\n}", RequestSeq: 11, Run: 3, Turn: 4,
		HasRequest: true,
	}
	submitComposer(t, app, "/request")
	if text := overlayText(app); !strings.Contains(text, "response not recorded (in flight or failed)") {
		t.Fatalf("request-only overlay is wrong:\n%s", text)
	}
	press(t, app, "esc")

	// A session with no model events opens no overlay.
	host.exchange = ExchangeView{}
	submitComposer(t, app, "/request")
	if app.overlay.kind != OverlayNone || !strings.Contains(app.notice, "no model exchange") {
		t.Fatalf("overlay = %q notice = %q, want a no-exchange notice", app.overlay.kind, app.notice)
	}
}

func TestSlashRequestUnavailableAndReadErrors(t *testing.T) {
	app := newTestApp(t, newManualHost())
	submitComposer(t, app, "/request")
	if app.overlay.kind != OverlayNone || !strings.Contains(app.notice, "unavailable") {
		t.Fatalf("overlay = %q notice = %q, want an unavailable notice", app.overlay.kind, app.notice)
	}

	failing := &promptHost{manualHost: newManualHost(), exchangeErr: fmt.Errorf("read session for request: %w", errors.New("boom"))}
	app = newTestApp(t, failing)
	submitComposer(t, app, "/request")
	if app.overlay.kind != OverlayNone || !strings.Contains(app.notice, "model request:") {
		t.Fatalf("overlay = %q notice = %q, want a status notice", app.overlay.kind, app.notice)
	}
}

func TestPromptOnlyHostReportsRequestUnavailable(t *testing.T) {
	host := &promptOnlyHost{manualHost: newManualHost(), prompt: PromptView{Text: "body", Version: "prompt-v3"}}
	app := newTestApp(t, host)

	submitComposer(t, app, "/request")
	if app.overlay.kind != OverlayNone || !strings.Contains(app.notice, "unavailable") {
		t.Fatalf("overlay = %q notice = %q", app.overlay.kind, app.notice)
	}
	press(t, app, "esc")
	submitComposer(t, app, "/prompt")
	if app.overlay.kind != OverlayPrompt {
		t.Fatalf("overlay = %q, want prompt", app.overlay.kind)
	}
}

func TestPromptOverlayWrapsInsteadOfTruncating(t *testing.T) {
	long := strings.Repeat("wrap-me-", 40) + "ta il"
	host := &promptHost{manualHost: newManualHost(), prompt: PromptView{
		Text: long, Version: "prompt-v3", Bytes: len(long),
	}}
	app := newTestApp(t, host)
	submitComposer(t, app, "/prompt")

	innerW, _ := boxInner(app.width, app.height)
	view := app.overlay.view(app)
	for i, line := range strings.Split(view, "\n") {
		if width := ansi.StringWidth(line); width > innerW+4 {
			t.Fatalf("overlay row %d is %d cells, wider than the box: %q", i, width, line)
		}
	}
	// The tail of the long line survives wrapping; truncation would drop it.
	if !strings.Contains(view, "ta il") {
		t.Fatalf("wrapped overlay dropped the end of the prompt:\n%s", view)
	}
	// The title row stays visible above a body that fills the scroll area.
	if !strings.Contains(view, "system prompt · prompt-v3") {
		t.Fatalf("overlay dropped its title:\n%s", view)
	}
}

func TestRequestOverlaySanitizesRecordedPayload(t *testing.T) {
	payload := "{\n  \"text\": \"\x1b[31mred\x1b[0m \u202eevil\"\n}"
	host := &promptHost{manualHost: newManualHost(), exchange: ExchangeView{
		Response: payload, ResponseSeq: 5, HasResponse: true,
	}}
	app := newTestApp(t, host)
	submitComposer(t, app, "/request")

	text := overlayText(app)
	if strings.ContainsRune(text, '\x1b') || strings.ContainsRune(text, '\u202e') {
		t.Fatalf("escape or bidi control reached the overlay:\n%q", text)
	}
	if !strings.Contains(text, "\u241b") {
		t.Fatalf("ESC did not render as a visible glyph:\n%s", text)
	}
}

func TestRequestOverlayCapsPayloadHeadAndTail(t *testing.T) {
	body := strings.Repeat("x", 100<<10)
	request := `{"head":"HEAD-MARKER","data":"` + body + `","tail":"TAIL-MARKER"}`
	host := &promptHost{manualHost: newManualHost(), exchange: ExchangeView{
		Request: request, RequestSeq: 9, HasRequest: true,
	}}
	app := newTestApp(t, host)
	submitComposer(t, app, "/request")

	text := overlayText(app)
	for _, want := range []string{"request  seq 9 · ", "HEAD-MARKER", "TAIL-MARKER", "bytes]"} {
		if !strings.Contains(text, want) {
			t.Fatalf("capped overlay is missing %q (len %d)", want, len(text))
		}
	}
	if want := "[omitted " + itoa(len(request)-exchangePayloadBytes) + " bytes]"; !strings.Contains(text, want) {
		t.Fatalf("capped overlay is missing the exact omitted count %q", want)
	}
	if len(text) > exchangePayloadBytes*3/2 {
		t.Fatalf("capped overlay is %d bytes, want roughly the 64 KiB cap", len(text))
	}

	press(t, app, "pgdown")
	scrolled := app.overlay.scroll
	if scrolled == 0 {
		t.Fatal("pgdown did not scroll the request overlay")
	}
	updateApp(t, app, wheelMsg(true))
	if want := scrolled - mouseWheelLines; app.overlay.scroll != want {
		t.Fatalf("wheel up left scroll at %d, want %d", app.overlay.scroll, want)
	}

	// An offset past the end clamps to the last full page instead of showing
	// blank rows.
	innerW, innerH := boxInner(app.width, app.height)
	wrapped := wrapOverlayLines(app.overlay.help, innerW)
	app.overlay.scroll = len(wrapped) * 2
	view := renderScrollWrapped(app.overlay.help, innerW, innerH-1, app.overlay.scroll)
	rows := strings.Split(view, "\n")
	if len(rows) != innerH-1 {
		t.Fatalf("scrolled view has %d rows, want %d", len(rows), innerH-1)
	}
	if last := wrapped[len(wrapped)-1]; !strings.HasSuffix(view, last) {
		t.Fatalf("scrolled view does not end at the last wrapped row %q", last)
	}
}

func TestSlashHelpListsTransparencyCommands(t *testing.T) {
	app := newTestApp(t, newManualHost())
	submitComposer(t, app, "/help")
	text := overlayText(app)
	for _, want := range []string{"/prompt", "/request"} {
		if !strings.Contains(text, want) {
			t.Fatalf("help is missing %q:\n%s", want, text)
		}
	}
}

func TestHeadlessPromptAndRequestFrames(t *testing.T) {
	host := &promptHost{
		manualHost: newManualHost(),
		prompt:     PromptView{Text: "headless prompt body", Version: "prompt-v3", Bytes: 19},
		exchange: ExchangeView{
			Request: "{\n  \"messages\": []\n}", Response: "{\n  \"text\": \"hi\"\n}",
			RequestSeq: 1, ResponseSeq: 2, Run: 1, Turn: 1,
			HasRequest: true, HasResponse: true,
		},
	}
	script := strings.Join([]string{
		`{"v":1,"type":"text","text":"/prompt"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"key","key":"esc"}`,
		`{"v":1,"type":"text","text":"/request"}`,
		`{"v":1,"type":"key","key":"enter"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"key","key":"esc"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"quit"}`,
	}, "\n") + "\n"
	var out bytes.Buffer
	exit, err := RunHeadless(HeadlessOptions{
		Host: host, Model: "deepseek-v4.1-flash",
		Width: 100, Height: 30,
		Stdin: strings.NewReader(script), Stdout: &out,
		ColorProfile: "truecolor", ImageProtocol: "glyph",
	})
	if err != nil {
		t.Fatalf("RunHeadless: %v", err)
	}
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	var frames []map[string]any
	var events []string
	for _, line := range parseOutput(t, out.String()) {
		switch line["type"] {
		case "frame":
			frames = append(frames, line)
		case "event":
			events = append(events, fmt.Sprint(line["event"]))
		}
	}
	if len(events) != 0 {
		t.Fatalf("display commands emitted protocol events: %v", events)
	}
	var promptFrame, requestFrame map[string]any
	for _, frame := range frames {
		text := strings.Join(anyStrings(frame["lines"].([]any)), "\n")
		switch frame["overlay"] {
		case "prompt":
			promptFrame = frame
			if !strings.Contains(text, "headless prompt body") {
				t.Fatalf("prompt frame lacks the prompt:\n%s", text)
			}
		case "request":
			requestFrame = frame
			if !strings.Contains(text, "request  seq 1") || !strings.Contains(text, "response seq 2") {
				t.Fatalf("request frame lacks the sections:\n%s", text)
			}
		}
	}
	if promptFrame == nil || requestFrame == nil {
		t.Fatalf("frames lack the prompt/request overlays: %d frames", len(frames))
	}
	if got := frames[len(frames)-1]["overlay"]; got != "none" {
		t.Fatalf("last frame overlay = %v, want none", got)
	}
}
