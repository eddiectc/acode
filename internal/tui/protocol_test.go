package tui

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"reflect"
	"strings"
	"testing"

	"github.com/charmbracelet/x/vt"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/model"
)

// TestUsageLineCoversEveryUsageField fails when model.Usage gains a field the
// protocol line does not carry. Every integer field gets a distinct value and
// must appear in the marshaled line; every boolean field must change it. A new
// non-integer field fails until this test and usageLine are updated together.
func TestUsageLineCoversEveryUsageField(t *testing.T) {
	usage := model.Usage{}
	value := reflect.ValueOf(&usage).Elem()
	typ := value.Type()
	unique := make(map[int64]string)
	for i := range typ.NumField() {
		field := typ.Field(i)
		switch field.Type.Kind() {
		case reflect.Int, reflect.Int32, reflect.Int64:
			// Large enough that no protocol version or count can collide.
			number := int64(1_000_003 + i*1_001)
			value.Field(i).SetInt(number)
			unique[number] = field.Name
		case reflect.Bool:
			// Checked one at a time below.
		default:
			t.Fatalf("model.Usage.%s has kind %s; extend usageLine and this test", field.Name, field.Type.Kind())
		}
	}
	data, err := jsonv2.Marshal(usageLine(usage))
	if err != nil {
		t.Fatalf("marshal usage line: %v", err)
	}
	var decoded map[string]any
	if err := jsonv2.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("decode usage line: %v", err)
	}
	present := make(map[int64]bool)
	for _, item := range decoded {
		if number, ok := item.(float64); ok {
			present[int64(number)] = true
		}
	}
	for number, name := range unique {
		if !present[number] {
			t.Fatalf("model.Usage.%s is not carried by UsageLine (value %d missing from %s)", name, number, data)
		}
	}

	base, err := jsonv2.Marshal(usageLine(model.Usage{}))
	if err != nil {
		t.Fatalf("marshal empty usage line: %v", err)
	}
	for i := range typ.NumField() {
		field := typ.Field(i)
		if field.Type.Kind() != reflect.Bool {
			continue
		}
		one := model.Usage{}
		reflect.ValueOf(&one).Elem().Field(i).SetBool(true)
		changed, err := jsonv2.Marshal(usageLine(one))
		if err != nil {
			t.Fatalf("marshal usage line with %s: %v", field.Name, err)
		}
		if bytes.Equal(base, changed) {
			t.Fatalf("model.Usage.%s is not carried by UsageLine: %s", field.Name, changed)
		}
	}
}

func TestDecodeCommandValidation(t *testing.T) {
	valid := []string{
		`{"v":1,"type":"text","text":"hi"}`,
		`{"v":1,"type":"key","key":"f1"}`,
		`{"v":1,"type":"key","key":"f2"}`,
		`{"v":1,"type":"key","key":"f4"}`,
		`{"v":1,"type":"key","key":"shift-enter"}`,
		`{"v":1,"type":"key","key":"alt+enter"}`,
		`{"v":1,"type":"key","key":"ctrl-c"}`,
		`{"v":1,"type":"resize","width":100,"height":30}`,
		`{"v":1,"type":"wait","until":"run_started"}`,
		`{"v":1,"type":"frame"}`,
		`{"v":1,"type":"snapshot"}`,
		`{"v":1,"type":"quit"}`,
		`{"v":1,"type":"frame","future_field":true}`,
	}
	for _, line := range valid {
		if _, err := decodeCommand([]byte(line)); err != nil {
			t.Errorf("decodeCommand(%s): %v", line, err)
		}
	}
	invalid := []string{
		`{"type":"frame"}`,
		`{"v":2,"type":"frame"}`,
		`{"v":1,"type":"nope"}`,
		`{"v":1,"type":"key","key":"hyper+x"}`,
		`{"v":1,"type":"resize","width":10,"height":30}`,
		`{"v":1,"type":"resize","width":600,"height":30}`,
		`{"v":1,"type":"wait","until":"nope"}`,
		`{"v":1,"type":"wait","until":"idle","timeout_ms":400000}`,
		`{"v":1,"type":"snapshot","format":"png"}`,
		`not json`,
	}
	for _, line := range invalid {
		if _, err := decodeCommand([]byte(line)); err == nil {
			t.Errorf("decodeCommand(%s) accepted", line)
		}
	}
	text := strings.Repeat("x", maxProtocolText+1)
	line, err := jsonv2.Marshal(map[string]any{"v": 1, "type": "text", "text": text})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decodeCommand(line); err == nil {
		t.Fatal("over-cap text accepted")
	}
	cmd, err := decodeCommand([]byte(`{"v":1,"type":"wait","until":"idle"}`))
	if err != nil || cmd.TimeoutMS != 30_000 {
		t.Fatalf("default timeout = %d err=%v", cmd.TimeoutMS, err)
	}
}

func TestEncodeSemanticEvents(t *testing.T) {
	cases := []struct {
		ev   any
		want []string
	}{
		{EventRunStarted{Run: 2, Model: "m", Dialect: "openai-chat", ThinkingEffort: "high"}, []string{`"event":"run_started"`, `"run":2`, `"model":"m"`, `"thinking_effort":"high"`}},
		{EventRunStarted{Run: 2, Model: "m", Dialect: "openai-chat"}, []string{`"event":"run_started"`, `"run":2`, `"model":"m"`}},
		{EventRunFinished{Status: agent.StopCompleted, Reason: "ok", RunTurns: 2, RunUsage: model.Usage{InputTokens: 1, OutputTokens: 2}, Turns: 3, Usage: model.Usage{InputTokens: 4, OutputTokens: 5}}, []string{`"status":"completed"`, `"run_turns":2`, `"input_tokens":4`}},
		{EventToolStarted{CallID: "c", Name: "read_file", Arguments: `{"path":"a"}`}, []string{`"event":"tool_started"`, `"arguments":{"path":"a"}`}},
		{EventToolStarted{CallID: "c", Name: "read_file", Arguments: "not json"}, []string{`"arguments":"not json"`}},
		{EventToolFinished{CallID: "c", Name: "read_file", OK: true, Code: "ok"}, []string{`"ok":true`, `"exit_code":0`, `"truncated":false`}},
		{EventModelChanged{Model: "m2", Dialect: "openai-responses", ThinkingEffort: "max"}, []string{`"event":"model_changed"`, `"thinking_effort":"max"`}},
		{EventModelChanged{Model: "m2", Dialect: "openai-responses"}, []string{`"event":"model_changed"`}},
		{EventNotice{Kind: "thinking_effort", Text: "thinking effort for the next run: high"}, []string{`"kind":"thinking_effort"`, `"text":"thinking effort for the next run: high"`}},
		{EventNotice{Kind: "retry", Text: "again"}, []string{`"kind":"retry"`, `"text":"again"`}},
		{EventError{Code: "internal_error", Message: "bad"}, []string{`"code":"internal_error"`}},
	}
	for _, tc := range cases {
		data, err := encodeEvent(tc.ev)
		if err != nil {
			t.Fatalf("encodeEvent(%T): %v", tc.ev, err)
		}
		if bytes.ContainsRune(data, 0x1b) {
			t.Fatalf("event contains ESC: %s", data)
		}
		for _, want := range tc.want {
			if !strings.Contains(string(data), want) {
				t.Errorf("event %T missing %s: %s", tc.ev, want, data)
			}
		}
	}
}

func TestEncodeEffortFieldOmittedForProviderDefault(t *testing.T) {
	for _, ev := range []any{
		EventRunStarted{Run: 1, Model: "m", Dialect: "openai-chat"},
		EventModelChanged{Model: "m", Dialect: "openai-chat"},
	} {
		data, err := encodeEvent(ev)
		if err != nil {
			t.Fatalf("encodeEvent(%T): %v", ev, err)
		}
		if bytes.Contains(data, []byte("thinking_effort")) {
			t.Fatalf("event %T exposed an empty thinking effort: %s", ev, data)
		}
	}
}

func TestCaptureFrameStylesWideCellsCursorAndSVG(t *testing.T) {
	emu := vt.NewSafeEmulator(20, 4)
	_, _ = emu.Write([]byte("\x1b[31mred\x1b[0m \x1b[1;4mbold\x1b[0m 界"))
	frame := captureFrame(emu, 1, StateStreaming, OverlayNone, []imageRef{{
		path: "chart.png", alt: "chart", line: 1, col: 0, width: 4, height: 2, protocol: "glyph",
	}}, "truecolor", false, "glyph")
	if frame.Seq != 1 || frame.Width != 20 || frame.Height != 4 {
		t.Fatalf("frame header = %+v", frame)
	}
	if len(frame.Lines) != 4 {
		t.Fatalf("lines = %d", len(frame.Lines))
	}
	if !strings.Contains(frame.Lines[0], "red bold 界") {
		t.Fatalf("line = %q", frame.Lines[0])
	}
	foundRed, foundBold := false, false
	for _, span := range frame.Styles {
		if span.Text == "red" && span.Fg == "ansi:1" {
			foundRed = true
		}
		if span.Text == "bold" {
			foundBold = true
			hasBold, hasUnderline := false, false
			for _, attr := range span.Attrs {
				hasBold = hasBold || attr == "bold"
				hasUnderline = hasUnderline || attr == "underline"
			}
			if !hasBold || !hasUnderline {
				t.Fatalf("bold span attrs = %v", span.Attrs)
			}
		}
	}
	if !foundRed || !foundBold {
		t.Fatalf("styles missing: %+v", frame.Styles)
	}
	if len(frame.Images) != 1 || frame.Images[0].Row != 1 || frame.Images[0].Width != 4 {
		t.Fatalf("images = %+v", frame.Images)
	}
	if frame.Cursor.Shape != "block" {
		t.Fatalf("cursor = %+v", frame.Cursor)
	}
	svg1 := snapshotSVG(emu, frame)
	svg2 := snapshotSVG(emu, frame)
	if svg1 != svg2 {
		t.Fatal("SVG snapshot is not deterministic")
	}
	if !strings.HasPrefix(svg1, "<svg") || !strings.Contains(svg1, "red") || !strings.Contains(svg1, "bold") || !strings.Contains(svg1, "界") {
		t.Fatalf("SVG content = %q", svg1)
	}
	if strings.ContainsRune(svg1, 0x1b) {
		t.Fatal("SVG contains ESC")
	}
}

func TestFrameAndSnapshotCaps(t *testing.T) {
	frame := Frame{V: 1, Type: "frame", Height: 3, Lines: []string{strings.Repeat("x", 2<<20)}}
	if _, ok := encodeFrame(frame); ok {
		t.Fatal("over-cap frame encoded")
	}
	snapshot := SnapshotLine{V: 1, Type: "snapshot", Format: "svg", SVG: strings.Repeat("x", maxSnapshotPayload+1)}
	if _, ok := encodeSnapshot(snapshot); ok {
		t.Fatal("over-cap snapshot encoded")
	}
}

func TestErrorLineSanitized(t *testing.T) {
	line := errorLine("bad", "a\x1b]52;c;x\x07b")
	if strings.ContainsRune(line.Message, 0x1b) || strings.ContainsRune(line.Message, 0x07) {
		t.Fatalf("error message contains controls: %q", line.Message)
	}
}

func FuzzDecodeCommand(f *testing.F) {
	f.Add([]byte(`{"v":1,"type":"frame"}`))
	f.Add([]byte(`{"v":1,"type":"text","text":"hi"}`))
	f.Add([]byte(`{"v":9,"type":"quit"}`))
	f.Add([]byte(`{"v":1,"type":"key","key":"ctrl-c"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		cmd, err := decodeCommand(data)
		if err != nil {
			return
		}
		if cmd.V != 1 {
			t.Fatalf("accepted version %d", cmd.V)
		}
		if len(cmd.Text) > maxProtocolText {
			t.Fatalf("accepted text of %d bytes", len(cmd.Text))
		}
	})
}
