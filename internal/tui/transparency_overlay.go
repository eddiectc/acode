package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
)

const (
	// exchangePayloadBytes is the display cap per recorded request or response:
	// half from the head and half from the tail, applied to recorded bytes
	// before sanitization. It is an app display cap; the SDK records at most
	// 1 MiB per request event.
	exchangePayloadBytes = 64 << 10
	// noticeBytes caps a status-line message built from a read or format error.
	noticeBytes = 512
)

// openPromptOverlay shows the system prompt recorded for the active session.
// The body is the recorded text, or a rebuild from the prompt version for a log
// that predates the recording, labelled because the rebuild uses the current
// identity and agent catalog.
func (m *App) openPromptOverlay() tea.Cmd {
	provider, ok := m.host.(PromptProvider)
	if !ok {
		m.notice = "the system prompt view is unavailable"
		return nil
	}
	view, err := provider.Prompt()
	if err != nil && view.Version == "" {
		m.notice = "system prompt: " + CapText(SanitizeLine(err.Error()), noticeBytes)
		return nil
	}
	if err == nil && view.Text == "" {
		m.notice = "the session records no system prompt"
		return nil
	}
	lines := promptOverlayLines(view)
	if err != nil {
		lines = append(lines, "", Sanitize("prompt text unavailable: "+err.Error()))
	}
	m.openTextOverlay(OverlayPrompt, promptOverlayTitle(view), lines)
	return nil
}

// openRequestOverlay shows the latest projected model attempt: the newest
// recorded request or response and its pair when one exists.
func (m *App) openRequestOverlay() tea.Cmd {
	provider, ok := m.host.(ExchangeProvider)
	if !ok {
		m.notice = "the model request view is unavailable"
		return nil
	}
	view, err := provider.Exchange()
	if err != nil {
		m.notice = "model request: " + CapText(SanitizeLine(err.Error()), noticeBytes)
		return nil
	}
	if !view.HasRequest && !view.HasResponse {
		m.notice = "no model exchange is recorded in this session yet"
		return nil
	}
	m.openTextOverlay(OverlayRequest, requestOverlayTitle(view), requestOverlayLines(view))
	return nil
}

// openTextOverlay installs a scrollable text overlay that wraps long lines
// instead of truncating them, and keeps a row-based scroll offset.
func (m *App) openTextOverlay(kind OverlayKind, title string, lines []string) {
	m.overlay.push(kind)
	m.overlay.title = title
	m.overlay.help = lines
	m.overlay.resize(m, m.width, m.height)
}

func promptOverlayTitle(view PromptView) string {
	version := SanitizeLine(view.Version)
	if version == "" {
		return "system prompt"
	}
	return "system prompt · " + version
}

// promptOverlayLines builds the header rows and the prompt body. The prompt is
// shown whole: the SDK applies its prompt cap, and Capped reports when it did.
// A view with no text (an unrenderable version) carries only the version; the
// caller appends the wrapped error, so no row claims a body that is absent.
func promptOverlayLines(view PromptView) []string {
	version := SanitizeLine(view.Version)
	if version == "" {
		version = "(not recorded)"
	}
	rows := []string{"prompt:   " + version}
	if view.Text == "" {
		return rows
	}
	rows = append(rows, fmt.Sprintf("bytes:    %d", view.Bytes))
	if tag := SanitizeLine(view.Tag); tag != "" {
		rows = append(rows, "tag:      "+tag)
	}
	if view.Reconstructed {
		rows = append(rows, "source:   reconstructed from the prompt version; the log predates the recorded prompt")
	} else {
		rows = append(rows, "source:   recorded in session_start")
	}
	if view.Capped {
		rows = append(rows, "capped:   the recorded prompt was truncated by the SDK prompt cap")
	}
	rows = append(rows, "")
	for _, line := range strings.Split(view.Text, "\n") {
		rows = append(rows, Sanitize(line))
	}
	return rows
}

func requestOverlayTitle(view ExchangeView) string {
	run := "?"
	if view.Run > 0 {
		run = itoa(view.Run)
	}
	return "last model attempt · run " + run + " · turn " + itoa(view.Turn) + " · projected"
}

// requestOverlayLines builds both sections in attempt order. The bytes in each
// header are the recorded payload length before the display cap and
// sanitization, matching the cap's unit.
func requestOverlayLines(view ExchangeView) []string {
	var rows []string
	if view.HasRequest {
		rows = append(rows, requestSectionHeader("request", view.RequestSeq, len(view.Request)))
		rows = append(rows, payloadRows(view.Request)...)
	} else {
		rows = append(rows, "request  not recorded for this turn")
		if view.LogRequests {
			rows = append(rows, "log_requests is on; a later turn records one.")
		} else {
			rows = append(rows,
				"log_requests was off for this turn. Enable it with -log-requests or",
				"in /settings, then restart: saved settings apply at the next startup.",
			)
		}
	}
	rows = append(rows, "")
	if view.HasResponse {
		rows = append(rows, requestSectionHeader("response", view.ResponseSeq, len(view.Response)))
		rows = append(rows, payloadRows(view.Response)...)
	} else {
		rows = append(rows, "response not recorded (in flight or failed)")
	}
	rows = append(rows, "",
		"projected payloads; HTTP headers and exact wire bytes are not recorded")
	return rows
}

func requestSectionHeader(name string, seq uint64, bytes int) string {
	return fmt.Sprintf("%-8s seq %d · %d bytes", name, seq, bytes)
}

// payloadRows applies the head-and-tail display cap, then sanitizes each line
// for the terminal. The recorded payload itself is never rewritten.
func payloadRows(payload string) []string {
	lines := strings.Split(CapTextMiddle(payload, exchangePayloadBytes), "\n")
	for i := range lines {
		lines[i] = Sanitize(lines[i])
	}
	return lines
}
