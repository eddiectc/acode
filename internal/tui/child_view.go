package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/tools"
)

// Child view display caps. They bound one focused child's transcript rendering
// only; the child session log is unchanged.
const (
	childToolBodyRows  = 12
	childToolBodyBytes = 2 << 10
	childToolArgsBytes = 2 << 10
	childPendingSteers = 8
	childErrBytes      = 512
)

// pendingSteer is one composer line sent to a focused child that the child has
// not acknowledged yet. The host assigns the ID the SDK echoes back.
type pendingSteer struct {
	id   uint64
	text string
}

// childFocus is one delegated child's read-only transcript, opened by Enter on
// an /agent-hub row. While the child is running the composer steers it and its
// display events update the view live; every response boundary replaces the
// live list with the canonical history snapshot. A finished child leaves the
// view read-only until Esc returns to the parent.
type childFocus struct {
	session  string
	agent    string
	status   string
	readOnly bool
	items    []agent.HistoryItem
	// base is the number of items installed by the last snapshot. A live user
	// message already present there is not appended a second time.
	base int
	// run is the current child run number, turn is the turn the live stream is
	// on, and baseTurn is the highest turn the last snapshot covers. A delta at
	// or below baseTurn is already in the snapshot and is ignored.
	run      int
	turn     int
	baseTurn int
	// assistant is the index of the in-flight response's assistant item, or -1
	// when the current turn has no assistant text yet. pendingTools maps a tool
	// call ID to its item so a completion pairs with its start.
	assistant    int
	pendingTools map[string]int
	// dirty reports that a live event changed the list and one coalesced tick
	// should rebuild the viewport.
	dirty bool
	// pending holds composer lines sent to the child that have not been
	// acknowledged yet. They render as queued and are dropped by ID when the
	// child records them with source=subagent-steer.
	pending []pendingSteer
	err     string
	view    transcriptView
	follow  bool
	width   int
}

// setItems installs a fresh child history snapshot. It is the canonical
// replacement for the live list: it derives the next run number and the turn
// boundary the snapshot covers, and resets the live item bookkeeping so queued
// events already represented here cannot duplicate an item.
func (f *childFocus) setItems(items []agent.HistoryItem) {
	f.items = items
	f.base = len(items)
	f.assistant = -1
	f.pendingTools = make(map[string]int)
	f.baseTurn = 0
	f.dirty = false
	for i, item := range items {
		f.baseTurn = max(f.baseTurn, item.Turn)
		if item.Kind == agent.HistoryTool && item.Pending && item.CallID != "" {
			f.pendingTools[item.CallID] = i
		}
	}
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Kind != agent.HistoryRun {
			continue
		}
		f.run = items[i].Run
		if items[i].Status != "" {
			f.status = string(items[i].Status)
			f.readOnly = true
			// A terminal run cannot consume a queued line; leaving it visible
			// would claim steering that will never happen.
			f.pending = nil
		}
		break
	}
}

// addPending records a steering line already accepted by the host.
func (f *childFocus) addPending(id uint64, text string) {
	f.pending = append(f.pending, pendingSteer{id: id, text: text})
	if len(f.pending) > childPendingSteers {
		f.pending = f.pending[len(f.pending)-childPendingSteers:]
	}
}

// ackPending drops the line the child acknowledged. Matching by ID keeps two
// identical submissions distinct.
func (f *childFocus) ackPending(id uint64) {
	if len(f.pending) == 0 {
		return
	}
	kept := f.pending[:0]
	for _, entry := range f.pending {
		if id != 0 && entry.id == id {
			continue
		}
		kept = append(kept, entry)
	}
	f.pending = kept
}

// handleChildDisplay projects one delegated child's live event into the focused
// view. Events for any other child are drained and dropped, and the parent
// transcript is never touched.
func (m *App) handleChildDisplay(obs agent.ChildObservation, ev agent.DisplayEvent) tea.Cmd {
	f := m.childFocus
	if f == nil || f.session != obs.Session || ev == nil {
		return m.nextDisplayCmd()
	}
	if f.applyLive(ev) {
		f.dirty = true
		if !m.coalesceScheduled {
			m.coalesceScheduled = true
			return tea.Batch(m.nextDisplayCmd(), tea.Tick(coalesceDelay, func(time.Time) tea.Msg {
				return coalesceMsg{}
			}))
		}
	}
	return m.nextDisplayCmd()
}

// rebuildChildFocusIfDirty renders a live update that a coalesce tick
// collected. It re-renders the focused transcript only; the parent transcript,
// scrolling, and the follow state are untouched.
func (m *App) rebuildChildFocusIfDirty() {
	f := m.childFocus
	if f == nil || !f.dirty {
		return
	}
	f.dirty = false
	m.rebuildChildFocus()
}

// applyLive maps one display event from the focused child onto the same
// agent.HistoryItem list the snapshot uses, so the live and canonical views
// share one renderer. It reports whether the list changed and needs a rebuild.
// Thinking deltas are ignored: the child view has never rendered them.
func (f *childFocus) applyLive(ev agent.DisplayEvent) bool {
	switch ev := ev.(type) {
	case agent.RunStarted:
		return f.applyRunStarted(ev)
	case agent.ModelStarted:
		if ev.Turn != f.turn {
			f.turn = ev.Turn
			f.assistant = -1
		}
		return false
	case agent.TextDelta:
		return f.applyTextDelta(ev.Text)
	case agent.ResponseCompleted:
		return f.applyResponseCompleted(ev)
	case agent.ToolStarted:
		return f.applyToolStarted(ev)
	case agent.ToolCompleted:
		return f.applyToolCompleted(ev)
	case agent.MessageRecorded:
		return f.applyMessageRecorded(ev)
	case agent.Notice:
		text := CapText(Sanitize(ev.Text), maxTranscriptItemText)
		if text == "" || f.snapshotHasNotice(ev.Kind, text) {
			return false
		}
		f.items = append(f.items, agent.HistoryItem{
			Kind: agent.HistoryNotice, Run: f.run, Turn: ev.Turn,
			NoticeKind: ev.Kind, Text: text,
		})
		return true
	case agent.RunFinished:
		return f.applyRunFinished(ev)
	}
	return false
}

// applyRunStarted appends a run header unless the snapshot already shows this
// run; a queued start for a run the snapshot covers must not duplicate it or
// reset the live turn.
func (f *childFocus) applyRunStarted(ev agent.RunStarted) bool {
	if last, ok := f.lastRun(); ok && last.Status == "" {
		return false
	}
	f.run++
	f.turn = ev.Turn
	f.assistant = -1
	f.items = append(f.items, agent.HistoryItem{
		Kind: agent.HistoryRun, Run: f.run, Turn: ev.Turn,
		Model: ev.Model, Dialect: ev.Dialect,
	})
	return true
}

func (f *childFocus) applyTextDelta(text string) bool {
	if text == "" {
		return false
	}
	switch {
	case f.turn > 0 && f.turn <= f.baseTurn:
		return false
	case f.turn == 0:
		// Focus opened after the child's ModelStarted was drained. With no
		// completed turn in the snapshot (baseTurn 0) any delta belongs to the
		// first turn and cannot duplicate it; with a completed turn, the turn
		// is unknown, so the delta is ignored until the next ModelStarted.
		if f.baseTurn > 0 {
			return false
		}
		f.turn = 1
	}
	text = Sanitize(text)
	if item := f.liveAssistant(); item != nil {
		item.Text = CapText(item.Text+text, maxTranscriptItemText)
		return true
	}
	f.items = append(f.items, agent.HistoryItem{
		Kind: agent.HistoryAssistant, Run: f.run, Turn: f.turn,
		Text: CapText(text, maxTranscriptItemText),
	})
	f.assistant = len(f.items) - 1
	return true
}

func (f *childFocus) applyResponseCompleted(ev agent.ResponseCompleted) bool {
	changed := false
	f.turn = ev.Turn
	text := CapText(Sanitize(ev.Text), maxTranscriptItemText)
	switch item := f.liveAssistant(); {
	case item != nil:
		if item.Text != text {
			item.Text = text
			changed = true
		}
	case text != "" && ev.Turn > f.baseTurn:
		// The item was missed because no delta arrived before the response.
		f.items = append(f.items, agent.HistoryItem{
			Kind: agent.HistoryAssistant, Run: f.run, Turn: ev.Turn,
			Text: text, Usage: ev.Usage,
		})
		changed = true
	}
	f.assistant = -1
	if run, ok := f.lastRun(); ok && (run.Turns != ev.CumulativeTurns || run.Usage != ev.CumulativeUsage) {
		run.Turns = ev.CumulativeTurns
		run.Usage = ev.CumulativeUsage
		changed = true
	}
	return changed
}

func (f *childFocus) applyToolStarted(ev agent.ToolStarted) bool {
	if _, ok := f.pendingTools[ev.Call.ID]; ok {
		// The snapshot already shows this call as pending.
		return false
	}
	if _, ok := f.snapshotTool(ev.Call.ID); ok {
		// The snapshot already holds this call; a completed call has no pending
		// entry, so the map above is not enough.
		return false
	}
	f.items = append(f.items, agent.HistoryItem{
		Kind: agent.HistoryTool, Run: f.run, Turn: ev.Turn,
		CallID:    ev.Call.ID,
		Name:      SanitizeLine(ev.Call.Name),
		Arguments: childArguments(ev.Call.Arguments),
		Pending:   true,
	})
	f.pendingTools[ev.Call.ID] = len(f.items) - 1
	return true
}

func (f *childFocus) applyToolCompleted(ev agent.ToolCompleted) bool {
	if item, ok := f.snapshotTool(ev.CallID); ok && !item.Pending {
		// The snapshot already holds this completion.
		return false
	}
	index, ok := f.pendingTools[ev.CallID]
	delete(f.pendingTools, ev.CallID)
	var item *agent.HistoryItem
	if ok && index >= 0 && index < len(f.items) && f.items[index].Kind == agent.HistoryTool {
		item = &f.items[index]
	} else {
		// The start was missed or already replaced by a snapshot.
		f.items = append(f.items, agent.HistoryItem{
			Kind: agent.HistoryTool, Run: f.run, Turn: ev.Turn,
			CallID: ev.CallID,
			Name:   SanitizeLine(ev.Name),
		})
		item = &f.items[len(f.items)-1]
	}
	item.Pending = false
	item.OK = ev.OK
	item.Code = string(ev.Code)
	item.Output = CapText(Sanitize(ev.Output), maxTranscriptItemText)
	item.Error = CapText(Sanitize(ev.Error), maxTranscriptItemText)
	item.ExitCode = ev.ExitCode
	item.Truncated = ev.Truncated
	item.TimedOut = ev.TimedOut
	item.Duration = ev.Duration
	return true
}

// applyMessageRecorded acknowledges a recorded child user message, so a
// delegation steer keeps its label and drops out of the queued list. A hint is
// a notice in the canonical projection, so the live list renders it that way.
func (f *childFocus) applyMessageRecorded(ev agent.MessageRecorded) bool {
	pendingBefore := len(f.pending)
	f.ackPending(ev.ID)
	changed := len(f.pending) != pendingBefore
	text := CapText(Sanitize(ev.Text), maxTranscriptItemText)
	if f.snapshotHasMessage(ev, text) {
		return changed
	}
	item := agent.HistoryItem{
		Kind: agent.HistoryUser, Run: f.run, Turn: ev.Turn,
		Source: ev.Source, Text: text,
	}
	switch ev.Source {
	case "interrupt-hint":
		item.Kind = agent.HistoryNotice
		item.NoticeKind = agent.NoticeInterrupt
	case "continuation-hint", "stuck-hint":
		item.Kind = agent.HistoryNotice
		item.NoticeKind = agent.NoticeWarning
	}
	f.items = append(f.items, item)
	return true
}

// snapshotHasMessage reports whether the last snapshot already holds this user
// message or hint. The child's own task is recorded before its first model
// call, so a queued acknowledgement can arrive after focus opened on a snapshot
// that already contains it.
func (f *childFocus) snapshotHasMessage(ev agent.MessageRecorded, text string) bool {
	for _, item := range f.items[:f.base] {
		if item.Source != ev.Source || item.Text != text {
			continue
		}
		if item.Kind == agent.HistoryUser || item.Kind == agent.HistoryNotice {
			return true
		}
	}
	return false
}

// applyRunFinished settles the run header and leaves the view read-only. A
// queued line cannot be consumed by a finished run, so it is dropped.
func (f *childFocus) applyRunFinished(ev agent.RunFinished) bool {
	changed := false
	if run, ok := f.lastRun(); ok {
		run.Status = ev.Result.Status
		run.Reason = SanitizeLine(ev.Reason)
		run.Turns = ev.Result.RunTurns
		run.Usage = ev.Result.RunUsage
		run.Duration = ev.Duration
		changed = true
	}
	f.status = SanitizeLine(string(ev.Result.Status))
	f.readOnly = true
	if len(f.pending) > 0 {
		f.pending = nil
		changed = true
	}
	return changed
}

// snapshotHasNotice reports whether the last snapshot already holds this notice.
func (f *childFocus) snapshotHasNotice(kind agent.NoticeKind, text string) bool {
	for _, item := range f.items[:f.base] {
		if item.Kind == agent.HistoryNotice && item.NoticeKind == kind && item.Text == text {
			return true
		}
	}
	return false
}

// snapshotTool returns the snapshot item for a tool call ID. A queued live
// event for a call the snapshot already covers must not append a second item.
func (f *childFocus) snapshotTool(callID string) (*agent.HistoryItem, bool) {
	if callID == "" {
		return nil, false
	}
	for i := f.base - 1; i >= 0; i-- {
		item := &f.items[i]
		if item.Kind == agent.HistoryTool && item.CallID == callID {
			return item, true
		}
	}
	return nil, false
}

// lastRun returns the run header the live events settle.
func (f *childFocus) lastRun() (*agent.HistoryItem, bool) {
	for i := len(f.items) - 1; i >= 0; i-- {
		if f.items[i].Kind == agent.HistoryRun {
			return &f.items[i], true
		}
	}
	return nil, false
}

// liveAssistant returns the assistant item of the in-flight response, or nil
// when the current turn has no assistant text yet.
func (f *childFocus) liveAssistant() *agent.HistoryItem {
	if f.assistant < 0 || f.assistant >= len(f.items) {
		return nil
	}
	item := &f.items[f.assistant]
	if item.Kind != agent.HistoryAssistant || item.Turn != f.turn {
		return nil
	}
	return item
}

// childArguments bounds a live tool call's arguments the way the SDK bounds the
// recorded item: an over-cap call keeps a quoted, capped preview instead of a
// half-written JSON object. Only the display preview reads it.
func childArguments(raw json.RawMessage) json.RawMessage {
	if len(raw) <= childToolArgsBytes {
		return raw
	}
	quoted, err := json.Marshal(CapText(Sanitize(string(raw)), childToolArgsBytes))
	if err != nil {
		return json.RawMessage(`"<unprintable arguments>"`)
	}
	return quoted
}

// rebuild renders the child history into the focus viewport.
func (f *childFocus) rebuild(t uiTheme, width int, rich bool) {
	if width <= 0 {
		return
	}
	f.width = width
	var lines []string
	if f.err != "" {
		lines = append(lines, t.errorSty.Render("error: "+SanitizeLine(f.err)))
	}
	lines = append(lines, renderChildHistory(t, f.items, f.pending, width, rich)...)
	f.view.SetLines(wrapLines(lines, width))
	if f.follow {
		f.view.GotoBottom()
	}
}

// renderChildHistory renders one delegated child's display history. Assistant
// text is markdown source and follows the parent transcript's rich/raw mode;
// tool bodies are capped because the focused view is not the log.
func renderChildHistory(t uiTheme, items []agent.HistoryItem, pending []pendingSteer, width int, rich bool) []string {
	var lines []string
	for _, item := range items {
		switch item.Kind {
		case agent.HistoryRun:
			lines = append(lines, renderChildRun(t, item, width))
		case agent.HistoryUser:
			label := "task"
			if item.Source == agent.SourceSubagentSteer {
				label = "delegation steer"
			}
			lines = append(lines, renderTextBlock(t, label, t.user, item.Text, width)...)
		case agent.HistoryAssistant:
			lines = append(lines, renderChildAssistant(t, item.Text, width, rich)...)
		case agent.HistoryTool:
			lines = append(lines, renderChildTool(t, item, width)...)
		case agent.HistoryNotice:
			style := t.notice
			if item.NoticeKind.Failure() {
				style = t.errorSty
			}
			lines = append(lines, renderTextBlock(t, "notice", style, item.Text, width)...)
		}
	}
	for _, entry := range pending {
		lines = append(lines, renderTextBlock(t, "delegation steer", t.user, entry.text, width)...)
		lines = append(lines, t.dim.Render("  queued…"))
	}
	if len(lines) == 0 {
		lines = append(lines, t.dim.Render("the child has no transcript yet"))
	}
	return lines
}

func renderChildRun(t uiTheme, item agent.HistoryItem, width int) string {
	status := string(item.Status)
	if status == "" {
		status = "in progress"
	}
	parts := []string{fmt.Sprintf("── run %d", item.Run), status}
	if item.Turns > 0 {
		parts = append(parts, fmt.Sprintf("%d turns", item.Turns))
	}
	if item.Usage.Total() > 0 {
		parts = append(parts, humanTokens(item.Usage.Total())+" tokens")
	}
	if item.Duration > 0 {
		parts = append(parts, formatElapsed(item.Duration))
	}
	if item.Model != "" {
		parts = append(parts, SanitizeLine(item.Model))
	}
	if item.Reason != "" && item.Status != agent.StopCompleted {
		parts = append(parts, SanitizeLine(item.Reason))
	}
	return t.dim.Render(ansi.Truncate(strings.Join(parts, " · ")+" ──", width, ""))
}

func renderChildAssistant(t uiTheme, text string, width int, rich bool) []string {
	if strings.TrimSpace(text) == "" {
		return nil
	}
	label := t.label.Render("assistant ")
	indent := strings.Repeat(" ", max(0, ansi.StringWidth(label)))
	bodyWidth := max(10, width-ansi.StringWidth(label))
	rendered, _, err := renderMarkdown(text, bodyWidth, rich, t.plain, t.light)
	if err != nil {
		rendered = Sanitize(text) + "\n[markdown fallback: " + SanitizeLine(err.Error()) + "]"
	}
	body := strings.Split(strings.TrimRight(rendered, "\n"), "\n")
	if len(body) == 0 {
		body = []string{""}
	}
	out := make([]string, 0, len(body))
	for i, line := range body {
		if i == 0 {
			out = append(out, label+t.assist.Render(line))
		} else {
			out = append(out, indent+line)
		}
	}
	return out
}

func renderChildTool(t uiTheme, item agent.HistoryItem, width int) []string {
	status := "ok"
	switch {
	case item.Pending:
		status = "pending"
	case !item.OK:
		status = item.Code
		if status == "" {
			status = "failed"
		}
	}
	parts := []string{"tool " + SanitizeLine(item.Name), SanitizeLine(status)}
	if intent := SanitizeLine(tools.Intent(item.Arguments)); intent != "" {
		parts = append(parts, intent)
	}
	lines := []string{t.tool.Render(ansi.Truncate(strings.Join(parts, " · "), width, ""))}
	body := item.Output
	if body == "" {
		body = item.Error
	}
	body = strings.TrimRight(Sanitize(body), "\n")
	if body == "" {
		return lines
	}
	bodyLines := strings.Split(body, "\n")
	folded := false
	if len(bodyLines) > childToolBodyRows {
		bodyLines = bodyLines[:childToolBodyRows]
		folded = true
	}
	size := 0
	for i, line := range bodyLines {
		size += len(line)
		if size > childToolBodyBytes {
			bodyLines = bodyLines[:i]
			folded = true
			break
		}
	}
	for _, line := range bodyLines {
		lines = append(lines, indentRows("  ", t.dim, line, width)...)
	}
	if folded {
		lines = append(lines, "  "+t.dim.Render("[output folded; the full result is in the child session log]"))
	}
	return lines
}

// focusAgentHubRow opens the selected roster row as the focused child view.
// Enter on a running child makes the composer its steering input; a finished
// child opens read-only. Esc later returns to the parent transcript without
// interrupting anything.
func (m *App) focusAgentHubRow() tea.Cmd {
	if !m.run.active || len(m.children) == 0 {
		m.notice = "no child is running"
		return nil
	}
	index := min(max(0, m.overlay.scroll), len(m.children)-1)
	row := m.children[index]
	if row.session == "" {
		m.notice = "the selected child has no session yet"
		return nil
	}
	m.childFocus = &childFocus{
		session:  row.session,
		agent:    row.agent,
		status:   row.status,
		readOnly: row.finished,
		follow:   true,
	}
	// Enter replaces the roster with the child view; one Esc then returns to the
	// parent transcript, matching the documented focus behavior.
	m.overlay = overlayState{kind: OverlayNone}
	m.layout()
	m.refreshChildFocus(row.session)
	if row.finished {
		m.notice = "child " + SanitizeLine(row.agent) + " finished; read-only transcript; Esc returns to the parent"
	} else {
		m.notice = "focused child " + SanitizeLine(row.agent) + "; the composer steers it; Esc returns to the parent"
	}
	return nil
}

// refreshChildFocus reloads one child's display history. It is the canonical
// replacement for the live list the child's own events build: it is called when
// the view opens, at child progress or finish boundaries, and when the parent
// run ends. The child log is read without its lock, so the refresh also works
// while the child still writes.
func (m *App) refreshChildFocus(session string) {
	f := m.childFocus
	if f == nil || f.session != session {
		return
	}
	provider, ok := m.host.(SubagentFocus)
	if !ok {
		f.err = "child transcripts are unavailable"
		m.rebuildChildFocus()
		return
	}
	items, err := provider.SubagentHistory(session)
	if err != nil {
		f.err = CapText(SanitizeLine(err.Error()), childErrBytes)
		m.rebuildChildFocus()
		return
	}
	f.err = ""
	f.setItems(items)
	m.rebuildChildFocus()
}

// rebuildChildFocus re-renders the focused transcript at the current width.
func (m *App) rebuildChildFocus() {
	f := m.childFocus
	if f == nil {
		return
	}
	f.rebuild(m.uiTheme(), m.width, m.rich)
}

// clearChildFocus returns the main transcript to the parent.
func (m *App) clearChildFocus() {
	if m.childFocus == nil {
		return
	}
	m.childFocus = nil
	m.layout()
}

// submitChildSteering sends a composer line to the focused child. The parent
// transcript is deliberately untouched: the line belongs to the child's log
// and its own transcript view.
func (m *App) submitChildSteering(text string) tea.Cmd {
	f := m.childFocus
	if f == nil {
		return nil
	}
	if f.readOnly {
		m.notice = "child " + SanitizeLine(f.agent) + " finished; Esc returns to the parent"
		m.restoreComposer(text)
		return nil
	}
	provider, ok := m.host.(SubagentFocus)
	if !ok {
		m.notice = "steering a child is unavailable"
		m.restoreComposer(text)
		return nil
	}
	m.steerNextID++
	line := agent.SteeringLine{ID: m.steerNextID, Text: text}
	if err := provider.SteerSubagent(f.session, line); err != nil {
		m.notice = "child steering: " + CapText(SanitizeLine(err.Error()), childErrBytes)
		m.restoreComposer(text)
		return nil
	}
	f.addPending(line.ID, text)
	m.rebuildChildFocus()
	m.notice = "steering queued for " + SanitizeLine(f.agent)
	return nil
}

// restoreComposer puts a rejected submission back when the composer is empty,
// so a read-only or unavailable child does not silently swallow the draft.
func (m *App) restoreComposer(text string) {
	if m.composer.Value() == "" {
		m.composer.SetValue(text)
		m.composer.CursorEnd()
	}
	m.dismissSuggestions()
	m.layout()
}

// scrollChildFocus scrolls the focused transcript. Scrolling up detaches the
// live tail; returning to the bottom reattaches it.
func (m *App) scrollChildFocus(up bool, lines int) {
	f := m.childFocus
	if f == nil {
		return
	}
	offset := f.view.YOffset()
	if up {
		f.view.ScrollUp(lines)
		if f.view.YOffset() != offset {
			f.follow = false
		}
		return
	}
	f.view.ScrollDown(lines)
	f.follow = f.view.AtBottom()
}
