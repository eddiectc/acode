package tui

import (
	"strings"
	"testing"

	"charm.land/bubbles/v2/list"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/vt"
)

func TestParseTheme(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		raw  string
		want ThemeName
	}{
		{"acode", ThemeDefault},
		{"Solarized-Dark", ThemeSolarizedDark},
		{" solarized-light ", ThemeSolarizedLight},
	} {
		got, err := ParseTheme(tc.raw)
		if err != nil || got != tc.want {
			t.Errorf("ParseTheme(%q) = %q, %v; want %q", tc.raw, got, err, tc.want)
		}
	}
	for _, raw := range []string{"", "dracula", "solarized"} {
		if _, err := ParseTheme(raw); err == nil {
			t.Errorf("ParseTheme(%q) accepted", raw)
		}
	}
}

func TestThemePalettes(t *testing.T) {
	t.Parallel()
	plain := uiThemeFor(true, ThemeSolarizedLight)
	if plain.paintsFrame() || !plain.plain {
		t.Fatalf("NO_COLOR theme paints the frame: %+v", plain)
	}
	if got := paintFrame("x", 3, plain); got != "x" {
		t.Fatalf("NO_COLOR frame = %q, want it untouched", got)
	}

	standard := uiThemeFor(false, ThemeDefault)
	if standard.paintsFrame() {
		t.Fatal("the built-in theme paints the terminal background")
	}
	if got := paintFrame("x", 3, standard); got != "x" {
		t.Fatalf("built-in frame = %q, want it untouched", got)
	}

	dark := uiThemeFor(false, ThemeSolarizedDark)
	if !dark.paintsFrame() || dark.light {
		t.Fatalf("solarized dark theme = %+v", dark)
	}
	if out := paintFrame("x", 1, dark); !strings.Contains(out, "48;2;0;43;54") ||
		!strings.Contains(out, "38;2;131;148;150") {
		t.Fatalf("solarized dark frame lacks its colors: %q", out)
	}

	light := uiThemeFor(false, ThemeSolarizedLight)
	if !light.paintsFrame() || !light.light {
		t.Fatalf("solarized light theme = %+v", light)
	}
	if out := paintFrame("x", 1, light); !strings.Contains(out, "48;2;253;246;227") ||
		!strings.Contains(out, "38;2;88;110;117") {
		t.Fatalf("solarized light frame lacks its colors: %q", out)
	}
}

func TestPaintFrameRestoresBackgroundAfterReset(t *testing.T) {
	t.Parallel()
	light := uiThemeFor(false, ThemeSolarizedLight)
	open := sgrOpen(mustStyle(t, light))
	if open == "" {
		t.Fatal("theme frame has no opening SGR")
	}
	// The assistant style ends with a reset, so the plain text after it would
	// otherwise lose the theme background.
	styled := "a" + light.assist.Render("b") + "c"
	out := paintFrame(styled, 5, light)
	if got := ansi.Strip(out); got != "abc  " {
		t.Fatalf("painted text = %q, want abc padded to 5 cells", got)
	}
	if !strings.Contains(out, "\x1b[m"+open) {
		t.Fatalf("frame SGR was not restored after an inner reset: %q", out)
	}
}

func TestPaintFrameSkipsOtherEscapeSequences(t *testing.T) {
	t.Parallel()
	light := uiThemeFor(false, ThemeSolarizedLight)
	osc := "\x1b]8;;https://example.test\x07link\x1b]8;;\x07"
	out := paintFrame(osc, 4, light)
	if !strings.Contains(out, osc) {
		t.Fatalf("paintFrame rewrote an OSC sequence: %q", out)
	}
	if got := ansi.Strip(out); got != "link" {
		t.Fatalf("painted OSC text = %q, want link", got)
	}
	for _, seq := range []string{
		// A string sequence may contain an SGR-like byte pair; the scanner must
		// copy the payload verbatim instead of applying a frame reset inside it.
		"\x1b_Gf=100,a=T;\x1b[0mAAAA\x1b\\", // Kitty APC
		"\x1bPq\x1b[0m#0;2;0;0;0\x1b\\",     // DCS
	} {
		out := paintFrame(seq+"x", 1, light)
		if !strings.Contains(out, seq) {
			t.Fatalf("paintFrame rewrote a string sequence: %q", out)
		}
	}
}

func mustStyle(t *testing.T, theme uiTheme) lipgloss.Style {
	t.Helper()
	style, ok := theme.frameStyle()
	if !ok {
		t.Fatal("theme has no frame style")
	}
	return style
}

func TestSetThemeRepaintsTheFrame(t *testing.T) {
	app := newTestApp(t, newManualHost())
	app.items = append(app.items, &transcriptItem{kind: itemAssistant, text: "hello"})
	app.invalidateRenders()
	app.setSize(60, 12)
	if content := app.View().Content; !strings.Contains(content, "hello") {
		t.Fatalf("default frame lacks the transcript:\n%s", content)
	}
	app.setTheme(ThemeSolarizedLight)
	app.setSize(60, 12)
	content := app.View().Content
	if !strings.Contains(content, "48;2;253;246;227") {
		t.Fatalf("solarized light frame lacks its background:\n%q", content)
	}
	for i, line := range strings.Split(content, "\n") {
		if width := ansi.StringWidth(line); width != 60 {
			t.Fatalf("row %d width = %d, want 60", i, width)
		}
	}
	app.applySettingFields([]SettingField{{Key: SettingTheme, Value: "solarized-dark"}})
	if app.theme != ThemeSolarizedDark {
		t.Fatalf("theme after settings apply = %q", app.theme)
	}
	app.applySettingFields([]SettingField{{Key: SettingTheme, Value: "acode"}})
	if app.theme != ThemeDefault {
		t.Fatalf("theme after settings reset = %q", app.theme)
	}
}

func TestThemeListUsesLightPalette(t *testing.T) {
	app := newTestApp(t, newManualHost())
	app.setTheme(ThemeSolarizedLight)
	items := []list.Item{
		fileItem{name: "alpha", rel: "alpha"},
		fileItem{name: "beta", rel: "beta"},
	}
	l := list.New(items, list.NewDefaultDelegate(), 40, 20)
	app.themeList(&l)
	l.Select(1)
	view := l.View()
	// The Bubbles delegate hardcodes #dddddd for normal titles; a light theme
	// must replace it with the theme text color.
	if strings.Contains(view, "221;221;221") {
		t.Fatalf("picker kept the dark default title color:\n%q", view)
	}
	if !strings.Contains(view, "38;2;88;110;117") {
		t.Fatalf("picker has no solarized light body text:\n%q", view)
	}
	if !strings.Contains(view, "38;2;38;139;210") {
		t.Fatalf("picker has no solarized light accent:\n%q", view)
	}
}

func TestThemeComposerUsesLightPalette(t *testing.T) {
	app := newTestApp(t, newManualHost())
	app.setTheme(ThemeSolarizedLight)
	view := app.composer.View()
	if !strings.Contains(view, "38;2;38;139;210") {
		t.Fatalf("composer prompt is not accented:\n%q", view)
	}
	if !strings.Contains(view, "48;2;238;232;213") {
		t.Fatalf("composer cursor line has no light background:\n%q", view)
	}
	app.setTheme(ThemeDefault)
	if view := app.composer.View(); strings.Contains(view, "253;246;227") {
		t.Fatalf("composer kept the light background after switching back:\n%q", view)
	}
}

func TestSetThemeRebuildsFoldedBodies(t *testing.T) {
	app := newTestApp(t, newManualHost())
	app.items = append(app.items, &transcriptItem{
		kind: itemTool, name: "read", ok: true, output: "file contents",
	})
	app.invalidateRenders()
	first := strings.Join(app.transcriptLines(), "\n")
	if !strings.Contains(first, "38;2;108;112;134") {
		t.Fatalf("built-in tool body has no built-in dim color:\n%q", first)
	}
	app.setTheme(ThemeSolarizedLight)
	second := strings.Join(app.transcriptLines(), "\n")
	if strings.Contains(second, "38;2;108;112;134") {
		t.Fatalf("folded tool body kept the built-in palette:\n%q", second)
	}
	if !strings.Contains(second, "38;2;101;123;131") {
		t.Fatalf("folded tool body has no solarized light dim color:\n%q", second)
	}
}

func TestSGRResetSkipsExtendedColorChannels(t *testing.T) {
	t.Parallel()
	for _, params := range []string{
		"38;5;0",
		"38;2;0;43;54",
		"48;2;133;153;0",
		"1;38;2;133;153;0",
		"58;2;0;0;0",
	} {
		if suffix, reset := sgrResetSuffix(params); reset {
			t.Errorf("sgrResetSuffix(%q) = %q, reset; want a color", params, suffix)
		}
	}
	for _, tc := range []struct {
		params string
		suffix string
	}{
		{"", ""},
		{"0", ""},
		{"00", ""},
		{"1;0", ""},
		{"0;38;5;0", "38;5;0"},
	} {
		suffix, reset := sgrResetSuffix(tc.params)
		if !reset || suffix != tc.suffix {
			t.Errorf("sgrResetSuffix(%q) = %q, %t; want %q, true", tc.params, suffix, reset, tc.suffix)
		}
	}
}

func TestPaintFrameKeepsZeroChannelColors(t *testing.T) {
	t.Parallel()
	light := uiThemeFor(false, ThemeSolarizedLight)
	out := paintFrame(light.user.Render("hi"), 2, light)
	marker := "133;153;0m"
	index := strings.Index(out, marker)
	if index < 0 {
		t.Fatalf("solarized user color is missing: %q", out)
	}
	if rest := out[index+len(marker):]; !strings.HasPrefix(rest, "hi") {
		t.Fatalf("frame SGR was injected into a zero-channel color: %q", out)
	}
}

func TestSolarizedLightPaintsEveryCell(t *testing.T) {
	app := newTestApp(t, newManualHost())
	app.items = []*transcriptItem{
		{kind: itemUser, text: "hello"},
		{kind: itemAssistant, text: "# Title\n\nbody text"},
		{kind: itemTool, name: "read", ok: true, output: "file contents"},
		{kind: itemThinking, text: "reasoning"},
	}
	app.invalidateRenders()
	app.setTheme(ThemeSolarizedLight)
	app.setSize(80, 24)

	emu := vt.NewSafeEmulator(80, 24)
	// Newline mode lets a bare LF return the carriage, matching the renderer's
	// newline handling.
	if _, err := emu.Write([]byte("\x1b[20h\x1b[H" + app.View().Content)); err != nil {
		t.Fatalf("render frame: %v", err)
	}
	backgrounds := make(map[string]int)
	foregrounds := make(map[string]bool)
	for y := range 24 {
		for x := range 80 {
			cell := emu.CellAt(x, y)
			if cell == nil || cell.Width == 0 {
				continue
			}
			backgrounds[colorName(cell.Style.Bg)]++
			foregrounds[colorName(cell.Style.Fg)] = true
		}
	}
	// Explicit widget backgrounds (the Glamour heading and the composer cursor
	// line) are expected; an unpainted default cell is not.
	if backgrounds["default"] != 0 {
		t.Fatalf("solarized light left %d cells with the terminal background: %v",
			backgrounds["default"], backgrounds)
	}
	if backgrounds["#fdf6e3"] < 80*24*3/4 {
		t.Fatalf("solarized light backgrounds = %v, want base3 on most cells", backgrounds)
	}
	for _, want := range []string{"#859900", "#b58900"} {
		if !foregrounds[want] {
			t.Errorf("frame has no cell with foreground %s", want)
		}
	}
}
