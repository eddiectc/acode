package tui

import (
	"fmt"
	"image/color"
	"strings"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// ThemeName selects one built-in UI palette. The value is the persisted
// ui_theme spelling.
type ThemeName string

const (
	// ThemeDefault is the built-in palette. It leaves the terminal background
	// alone.
	ThemeDefault ThemeName = "acode"
	// ThemeSolarizedDark is the Solarized dark palette on base03.
	ThemeSolarizedDark ThemeName = "solarized-dark"
	// ThemeSolarizedLight is the Solarized light palette on base3.
	ThemeSolarizedLight ThemeName = "solarized-light"
)

// ParseTheme validates and normalizes one ui_theme value. The returned name is
// the canonical spelling; an empty value is not a theme.
func ParseTheme(raw string) (ThemeName, error) {
	switch name := ThemeName(strings.ToLower(strings.TrimSpace(raw))); name {
	case ThemeDefault, ThemeSolarizedDark, ThemeSolarizedLight:
		return name, nil
	default:
		return "", fmt.Errorf("unknown theme %q: want %s, %s, or %s",
			raw, ThemeDefault, ThemeSolarizedDark, ThemeSolarizedLight)
	}
}

// ThemeSettingOptions lists the selectable themes in settings order.
func ThemeSettingOptions() []SettingOption {
	return []SettingOption{
		{Value: string(ThemeDefault), Label: "acode"},
		{Value: string(ThemeSolarizedDark), Label: "solarized dark"},
		{Value: string(ThemeSolarizedLight), Label: "solarized light"},
	}
}

// themeDisplay is the operator-facing name of one theme.
func themeDisplay(name ThemeName) string {
	for _, option := range ThemeSettingOptions() {
		if option.Value == string(name) {
			return option.Label
		}
	}
	return string(ThemeDefault)
}

// themeColors is one palette before it becomes styles. background is empty for
// a palette that leaves the terminal background alone.
type themeColors struct {
	light                    bool
	accent, user, text       string
	muted, tool, notice, err string
	background               string
	cursorLine, selection    string
}

// uiTheme is the resolved style set for one palette. plain is NO_COLOR: color
// and background are stripped, leaving only weight and style attributes.
type uiTheme struct {
	plain bool
	light bool

	// Background-painting themes use these colors for the full frame and for
	// widgets that render their own text (the composer and pickers).
	background     color.Color
	accentColor    color.Color
	textColor      color.Color
	mutedColor     color.Color
	cursorColor    color.Color
	selectionColor color.Color

	title    lipgloss.Style
	label    lipgloss.Style
	user     lipgloss.Style
	assist   lipgloss.Style
	think    lipgloss.Style
	tool     lipgloss.Style
	dim      lipgloss.Style
	notice   lipgloss.Style
	errorSty lipgloss.Style
	status   lipgloss.Style
	header   lipgloss.Style
	box      lipgloss.Style
}

// uiThemeFor resolves one palette. An unknown or empty name falls back to the
// built-in default; configuration validates the name before it reaches here.
func uiThemeFor(noColor bool, name ThemeName) uiTheme {
	if noColor {
		return uiTheme{
			plain:  true,
			light:  name == ThemeSolarizedLight,
			title:  lipgloss.NewStyle().Bold(true),
			label:  lipgloss.NewStyle().Bold(true),
			think:  lipgloss.NewStyle().Italic(true),
			status: lipgloss.NewStyle(),
			header: lipgloss.NewStyle(),
			box:    lipgloss.NewStyle(),
		}
	}
	switch name {
	case ThemeSolarizedDark:
		return themeFromColors(themeColors{
			accent: "#268bd2", user: "#859900", text: "#839496",
			muted: "#586e75", tool: "#b58900", notice: "#b58900", err: "#dc322f",
			background: "#002b36", cursorLine: "#073642", selection: "#073642",
		})
	case ThemeSolarizedLight:
		return themeFromColors(themeColors{
			light:  true,
			accent: "#268bd2", user: "#859900", text: "#586e75",
			muted: "#657b83", tool: "#b58900", notice: "#b58900", err: "#dc322f",
			background: "#fdf6e3", cursorLine: "#eee8d5", selection: "#eee8d5",
		})
	default:
		return themeFromColors(themeColors{
			accent: "#89b4fa", user: "#a6e3a1", text: "#cdd6f4",
			muted: "#6c7086", tool: "#f9e2af", notice: "#f9e2af", err: "#f38ba8",
		})
	}
}

// themeFromColors builds the full style set for one palette.
func themeFromColors(c themeColors) uiTheme {
	accent := lipgloss.Color(c.accent)
	text := lipgloss.Color(c.text)
	muted := lipgloss.Color(c.muted)
	t := uiTheme{
		light:       c.light,
		accentColor: accent,
		textColor:   text,
		mutedColor:  muted,
		title:       lipgloss.NewStyle().Bold(true).Foreground(accent),
		label:       lipgloss.NewStyle().Bold(true).Foreground(accent),
		user:        lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color(c.user)),
		assist:      lipgloss.NewStyle().Foreground(text),
		think:       lipgloss.NewStyle().Foreground(muted).Italic(true),
		tool:        lipgloss.NewStyle().Foreground(lipgloss.Color(c.tool)),
		dim:         lipgloss.NewStyle().Foreground(muted),
		notice:      lipgloss.NewStyle().Foreground(lipgloss.Color(c.notice)),
		errorSty:    lipgloss.NewStyle().Foreground(lipgloss.Color(c.err)),
		status:      lipgloss.NewStyle().Foreground(muted),
		header:      lipgloss.NewStyle().Foreground(text),
		box:         lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(accent).Padding(0, 1),
	}
	if c.background != "" {
		t.background = lipgloss.Color(c.background)
	}
	if c.cursorLine != "" {
		t.cursorColor = lipgloss.Color(c.cursorLine)
	}
	if c.selection != "" {
		t.selectionColor = lipgloss.Color(c.selection)
	}
	return t
}

// effectiveTheme resolves the app's theme, treating the zero value and an
// unknown name as the built-in default.
func (m *App) effectiveTheme() ThemeName {
	switch m.theme {
	case ThemeSolarizedDark, ThemeSolarizedLight:
		return m.theme
	default:
		return ThemeDefault
	}
}

// uiTheme resolves the app's active palette.
func (m *App) uiTheme() uiTheme {
	return uiThemeFor(m.noColor, m.effectiveTheme())
}

// paintsFrame reports whether the palette owns the terminal foreground and
// background. Only then does View paint every cell.
func (t uiTheme) paintsFrame() bool {
	return !t.plain && t.background != nil && t.textColor != nil
}

// frameStyle is the full-frame foreground/background pair.
func (t uiTheme) frameStyle() (lipgloss.Style, bool) {
	if !t.paintsFrame() {
		return lipgloss.Style{}, false
	}
	return lipgloss.NewStyle().Foreground(t.textColor).Background(t.background), true
}

// paintFrame paints every cell of a rendered frame with the theme foreground
// and background. A nested SGR reset would otherwise drop the frame colors for
// the rest of a row, so the frame SGR is emitted again after each reset, and
// each row is padded to the full width. A theme without a background leaves the
// frame untouched.
func paintFrame(content string, width int, t uiTheme) string {
	style, ok := t.frameStyle()
	if !ok || width <= 0 {
		return content
	}
	open := sgrOpen(style)
	if open == "" {
		return content
	}
	lines := strings.Split(content, "\n")
	for i, line := range lines {
		line = reapplySGR(line, open)
		if used := ansi.StringWidth(line); used < width {
			line += strings.Repeat(" ", width-used)
		}
		lines[i] = open + line + "\x1b[m"
	}
	return strings.Join(lines, "\n")
}

// sgrOpen returns the opening SGR sequence of a style without its trailing
// reset. Lipgloss is the authority for how a color is encoded for the active
// profile.
func sgrOpen(style lipgloss.Style) string {
	rendered := style.Render("")
	if rendered == "\x1b[m" {
		return ""
	}
	if !strings.HasSuffix(rendered, "\x1b[m") {
		return ""
	}
	return strings.TrimSuffix(rendered, "\x1b[m")
}

// reapplySGR inserts open after every SGR reset in line. It leaves other escape
// sequences untouched, including the OSC/APC/DCS strings the image protocols
// emit.
func reapplySGR(line, open string) string {
	if !strings.Contains(line, "\x1b") {
		return line
	}
	var b strings.Builder
	b.Grow(len(line) + len(open)*4)
	for i := 0; i < len(line); {
		if line[i] != 0x1b {
			b.WriteByte(line[i])
			i++
			continue
		}
		if i+1 >= len(line) {
			b.WriteString(line[i:])
			break
		}
		switch line[i+1] {
		case '[':
			j := i + 2
			for j < len(line) && (line[j] < 0x40 || line[j] > 0x7e) {
				j++
			}
			if j >= len(line) {
				b.WriteString(line[i:])
				i = len(line)
				continue
			}
			seq := line[i : j+1]
			b.WriteString(seq)
			if line[j] == 'm' {
				if suffix, reset := sgrResetSuffix(string(seq[2 : len(seq)-1])); reset {
					b.WriteString(open)
					if suffix != "" {
						b.WriteString("\x1b[")
						b.WriteString(suffix)
						b.WriteByte('m')
					}
				}
			}
			i = j + 1
		case ']', 'P', '_', '^', 'X':
			j := i + 2
			for j < len(line) {
				if line[j] == 0x07 {
					j++
					break
				}
				if line[j] == 0x1b && j+1 < len(line) && line[j+1] == '\\' {
					j += 2
					break
				}
				j++
			}
			b.WriteString(line[i:j])
			i = j
		default:
			b.WriteString(line[i : i+2])
			i += 2
		}
	}
	return b.String()
}

// sgrResetSuffix reports whether params contains a full SGR reset. A reset
// combined with later parameters returns those parameters so they can be
// re-emitted after the frame SGR. Extended color arguments are skipped: a zero
// channel in 38/48/58;2;r;g;b is a color, not a reset.
func sgrResetSuffix(params string) (string, bool) {
	if params == "" {
		return "", true
	}
	parts := strings.Split(params, ";")
	for i := 0; i < len(parts); i++ {
		switch parts[i] {
		case "38", "48", "58":
			switch {
			case i+1 >= len(parts):
				// A truncated color has no further parameters to read.
			case parts[i+1] == "5":
				i += 2
			case parts[i+1] == "2":
				i += 4
			default:
				i++
			}
			continue
		}
		if isZeroSGRParam(parts[i]) {
			return strings.Join(parts[i+1:], ";"), true
		}
	}
	return "", false
}

// isZeroSGRParam reports whether one SGR parameter is the reset code. An empty
// parameter is also zero, and leading zeros denote the same code.
func isZeroSGRParam(param string) bool {
	return strings.Trim(param, "0") == ""
}
