package tui

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/tools"
)

// liveChildItems is the snapshot a focused child starts from: a run header and
// the delegated task, with no assistant text yet.
func liveChildItems() []agent.HistoryItem {
	return []agent.HistoryItem{
		{Kind: agent.HistoryRun, Run: 1, Model: "deepseek-v4.1-flash"},
		{Kind: agent.HistoryUser, Run: 1, Source: "delegate", Text: "Find the projection."},
	}
}

func childObs(session string) agent.ChildObservation {
	return agent.ChildObservation{
		CallID: "call-1", Index: 0, Agent: "explore", Session: session,
		Intent: "find the projection", Model: "deepseek-v4.1-flash",
		Tools: []string{"read_file", "search"},
	}
}

// focusedChild starts a parent run with one delegated child and opens its view.
func focusedChild(t *testing.T, history []agent.HistoryItem) (*App, *manualHost, agent.ChildObservation) {
	t.Helper()
	host := newManualHost()
	host.childHistory = map[string][]agent.HistoryItem{"child-1": history}
	app := newTestApp(t, host)
	startTestRun(t, app, "delegate the work")
	deliver(app, agent.RunStarted{Turn: 0, Model: "deepseek-v4.1-flash", Dialect: "openai-chat"})
	deliver(app, startedEvent(0, "child-1", "explore", "find the projection"))
	submitComposer(t, app, "/agent-hub")
	press(t, app, "enter")
	if app.childFocus == nil || app.childFocus.session != "child-1" {
		t.Fatalf("child view did not open: %+v", app.childFocus)
	}
	return app, host, childObs("child-1")
}

// deliverChild sends one delegated child event the way the host does: through
// the callback the app installed, then through the run's display channel.
func deliverChild(t *testing.T, app *App, host *manualHost, obs agent.ChildObservation, ev agent.DisplayEvent) {
	t.Helper()
	callback := host.childCallback()
	if callback == nil {
		t.Fatal("the app installed no child callback")
	}
	if err := callback(ChildEvent{Child: obs, Event: ev}); err != nil {
		t.Fatalf("child callback: %v", err)
	}
	drainDisplay(t, app)
}

// drainDisplay feeds the next channel item to the app.
func drainDisplay(t *testing.T, app *App) {
	t.Helper()
	select {
	case msg := <-app.run.events:
		updateApp(t, app, displayEventMsg{run: app.run.number, msg: msg})
	default:
		t.Fatal("the display channel had no message")
	}
}

func childView(app *App) string {
	if app.childFocus == nil {
		return ""
	}
	return ansi.Strip(app.childFocus.view.View())
}

func countChildTools(app *App) int {
	count := 0
	for _, item := range app.childFocus.items {
		if item.Kind == agent.HistoryTool {
			count++
		}
	}
	return count
}

func TestChildFocusRendersLiveTextBeforeProgress(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	parentItems := len(app.items)

	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "The projection "})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "lives in context.go."})
	if view := childView(app); strings.Contains(view, "lives in context.go.") {
		t.Fatalf("the view rebuilt before its coalesce tick:\n%s", view)
	}

	updateApp(t, app, coalesceMsg{})
	view := childView(app)
	if !strings.Contains(view, "The projection lives in context.go.") {
		t.Fatalf("live child text missing from the view:\n%s", view)
	}
	if len(app.items) != parentItems {
		t.Fatalf("the parent transcript gained %d items from a child event", len(app.items)-parentItems)
	}
}

func TestChildFocusSnapshotReplacesTheLiveItem(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "live answer"})
	updateApp(t, app, coalesceMsg{})

	// The canonical snapshot arrives at the response boundary with the same text.
	host.mu.Lock()
	host.childHistory["child-1"] = []agent.HistoryItem{
		{Kind: agent.HistoryRun, Run: 1, Model: "deepseek-v4.1-flash", Turns: 1},
		{Kind: agent.HistoryUser, Run: 1, Source: "delegate", Text: "Find the projection."},
		{Kind: agent.HistoryAssistant, Run: 1, Turn: 1, Text: "live answer"},
	}
	host.mu.Unlock()
	deliver(app, agent.SubagentProgress{
		Turn: 1, CallID: "call-1", Index: 0, Agent: "explore", Session: "child-1", ChildTurns: 1,
	})

	view := childView(app)
	if got := strings.Count(view, "live answer"); got != 1 {
		t.Fatalf("the answer rendered %d times after the snapshot:\n%s", got, view)
	}
	if got := len(app.childFocus.items); got != 3 {
		t.Fatalf("items = %d, want the canonical 3", got)
	}
	if app.childFocus.baseTurn != 1 {
		t.Fatalf("baseTurn = %d, want 1", app.childFocus.baseTurn)
	}
	// A queued delta for the turn the snapshot covers is ignored.
	deliverChild(t, app, host, obs, agent.TextDelta{Text: " again"})
	updateApp(t, app, coalesceMsg{})
	if got := strings.Count(childView(app), "live answer"); got != 1 {
		t.Fatalf("a queued delta duplicated the snapshotted turn:\n%s", childView(app))
	}
}

func TestChildFocusStreamsTheFirstTurnWhenFocusOpensMidResponse(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	// The child's ModelStarted was drained before focus opened, so only deltas
	// are queued. The snapshot holds no assistant text, so nothing can duplicate.
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "first turn "})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "streams anyway"})
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); !strings.Contains(view, "first turn streams anyway") {
		t.Fatalf("the first turn's live text is missing:\n%s", view)
	}
}

func TestChildFocusIgnoresAnUnknownTurnAfterASnapshottedTurn(t *testing.T) {
	app, host, obs := focusedChild(t, []agent.HistoryItem{
		{Kind: agent.HistoryRun, Run: 1, Model: "deepseek-v4.1-flash"},
		{Kind: agent.HistoryUser, Run: 1, Source: "delegate", Text: "Find the projection."},
		{Kind: agent.HistoryAssistant, Run: 1, Turn: 1, Text: "first answer"},
	})
	// No ModelStarted was delivered, so the turn is unknown and the snapshot
	// already holds one: the delta cannot be attributed and is ignored.
	deliverChild(t, app, host, obs, agent.TextDelta{Text: " stale tail"})
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); strings.Contains(view, "stale tail") {
		t.Fatalf("an unattributable delta was appended:\n%s", view)
	}

	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 2, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "second answer"})
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); !strings.Contains(view, "second answer") {
		t.Fatalf("the next turn's delta was dropped:\n%s", view)
	}
}

func TestChildFocusIgnoresQueuedDeltasForTheSnapshottedTurn(t *testing.T) {
	app, host, obs := focusedChild(t, []agent.HistoryItem{
		{Kind: agent.HistoryRun, Run: 1, Model: "deepseek-v4.1-flash"},
		{Kind: agent.HistoryUser, Run: 1, Source: "delegate", Text: "Find the projection."},
		{Kind: agent.HistoryAssistant, Run: 1, Turn: 1, Text: "first answer"},
	})

	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: " stale tail"})
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); strings.Contains(view, "stale tail") {
		t.Fatalf("a delta for the snapshotted turn was appended:\n%s", view)
	}

	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 2, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "second answer"})
	updateApp(t, app, coalesceMsg{})
	view := childView(app)
	if !strings.Contains(view, "second answer") {
		t.Fatalf("the next turn's delta was dropped:\n%s", view)
	}
	if strings.Contains(view, "stale tail") {
		t.Fatalf("the stale delta appeared after the next turn:\n%s", view)
	}
}

func TestChildFocusPairsLiveToolStartAndCompletion(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.ToolStarted{Turn: 1, Call: model.ToolCall{
		ID: "call-9", Name: "read_file", Arguments: json.RawMessage(`{"path":"context.go"}`),
	}})
	if got := countChildTools(app); got != 1 {
		t.Fatalf("tool items = %d, want 1 after the start", got)
	}
	deliverChild(t, app, host, obs, agent.ToolCompleted{
		Turn: 1, CallID: "call-9", Name: "read_file", OK: true, Code: tools.CodeOK, Output: "contents",
	})
	if got := countChildTools(app); got != 1 {
		t.Fatalf("tool items = %d, want the start updated in place", got)
	}
	item := app.childFocus.items[len(app.childFocus.items)-1]
	if item.Pending || !item.OK || item.Output != "contents" {
		t.Fatalf("paired tool item = %+v", item)
	}
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); !strings.Contains(view, "tool read_file") || strings.Contains(view, "pending") {
		t.Fatalf("tool item did not render as completed:\n%s", view)
	}

	// A completion whose start was missed still renders as one completed item.
	deliverChild(t, app, host, obs, agent.ToolCompleted{
		Turn: 1, CallID: "call-miss", Name: "search", OK: false, Code: tools.CodeNotFound, Error: "boom",
	})
	if got := countChildTools(app); got != 2 {
		t.Fatalf("tool items = %d, want an appended completion", got)
	}
}

func TestChildFocusLiveSteerAckDropsTheQueuedLine(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	typeText(t, app, "focus on the parser")
	press(t, app, "enter")
	if len(host.steered) != 1 {
		t.Fatalf("steering = %+v", host.steered)
	}
	deliverChild(t, app, host, obs, agent.MessageRecorded{
		Turn: 1, ID: host.steered[0].id, Source: agent.SourceSubagentSteer, Text: "focus on the parser",
	})
	updateApp(t, app, coalesceMsg{})
	if len(app.childFocus.pending) != 0 {
		t.Fatalf("pending steers = %+v, want the acknowledgement to drop it", app.childFocus.pending)
	}
	view := childView(app)
	if strings.Contains(view, "queued") {
		t.Fatalf("the acknowledged line is still queued:\n%s", view)
	}
	if !strings.Contains(view, "delegation steer") || !strings.Contains(view, "focus on the parser") {
		t.Fatalf("the recorded steer lost its label:\n%s", view)
	}
}

func TestChildFocusLiveFinishSetsReadOnly(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "done"})
	deliverChild(t, app, host, obs, agent.RunFinished{
		RunID: "child-run",
		Result: agent.Result{
			Status: agent.StopCompleted, RunTurns: 1, CumulativeTurns: 1,
			RunUsage: model.Usage{InputTokens: 10, OutputTokens: 2},
		},
		Reason: "model completed the task", Duration: time.Second,
	})
	updateApp(t, app, coalesceMsg{})

	f := app.childFocus
	if !f.readOnly || f.status != string(agent.StopCompleted) {
		t.Fatalf("finished child focus = readOnly %v status %q", f.readOnly, f.status)
	}
	if view := childView(app); !strings.Contains(view, "completed") {
		t.Fatalf("the run header lacks the terminal status:\n%s", view)
	}
	typeText(t, app, "one more thing")
	press(t, app, "enter")
	if len(host.steered) != 0 {
		t.Fatalf("a finished child accepted steering: %+v", host.steered)
	}
	if !strings.Contains(app.notice, "finished") {
		t.Fatalf("notice = %q", app.notice)
	}
}

func TestChildFocusIgnoresOtherChildren(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	deliver(app, startedEvent(1, "child-2", "reviewer", "review the change"))
	other := childObs("child-2")
	other.Index = 1

	deliverChild(t, app, host, other, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, other, agent.TextDelta{Text: "reviewer text"})
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); strings.Contains(view, "reviewer text") {
		t.Fatalf("another child's text reached the focused view:\n%s", view)
	}

	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "explore text"})
	updateApp(t, app, coalesceMsg{})
	if view := childView(app); !strings.Contains(view, "explore text") {
		t.Fatalf("the focused child's text is missing:\n%s", view)
	}
}

func TestChildFocusConcurrentChildrenStreamIndependently(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	deliver(app, startedEvent(1, "child-2", "reviewer", "review the change"))
	callback := host.childCallback()
	streams := []struct {
		obs  agent.ChildObservation
		text string
	}{
		{obs, "explore text"},
		{childObs("child-2"), "reviewer text"},
	}
	var wg sync.WaitGroup
	for _, stream := range streams {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = callback(ChildEvent{Child: stream.obs, Event: agent.ModelStarted{Turn: 1, Attempt: 1}})
			_ = callback(ChildEvent{Child: stream.obs, Event: agent.TextDelta{Text: stream.text}})
		}()
	}
	wg.Wait()
	for range len(streams) * 2 {
		drainDisplay(t, app)
	}
	updateApp(t, app, coalesceMsg{})
	view := childView(app)
	if !strings.Contains(view, "explore text") {
		t.Fatalf("the focused child's text is missing:\n%s", view)
	}
	if strings.Contains(view, "reviewer text") {
		t.Fatalf("a sibling's text reached the focused view:\n%s", view)
	}
}

func TestChildFocusKeepsLiveTurnAcrossAQueuedRunStart(t *testing.T) {
	app, host, obs := focusedChild(t, liveChildItems())
	deliverChild(t, app, host, obs, agent.ModelStarted{Turn: 1, Attempt: 1})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: "live"})
	// The run header is already in the snapshot, so this queued start must not
	// reset the in-flight turn and split the assistant text.
	deliverChild(t, app, host, obs, agent.RunStarted{Turn: 0, Model: "deepseek-v4.1-flash", Dialect: "openai-chat"})
	deliverChild(t, app, host, obs, agent.TextDelta{Text: " continues"})
	updateApp(t, app, coalesceMsg{})

	assistants := 0
	for _, item := range app.childFocus.items {
		if item.Kind == agent.HistoryAssistant {
			assistants++
		}
	}
	if assistants != 1 {
		t.Fatalf("assistant items = %d, want the live turn kept in one item", assistants)
	}
	if view := childView(app); !strings.Contains(view, "live continues") {
		t.Fatalf("the live turn split or lost text:\n%s", view)
	}
}

func TestChildFocusIgnoresQueuedToolEventsTheSnapshotCovers(t *testing.T) {
	app, host, obs := focusedChild(t, []agent.HistoryItem{
		{Kind: agent.HistoryRun, Run: 1, Model: "deepseek-v4.1-flash"},
		{Kind: agent.HistoryUser, Run: 1, Source: "delegate", Text: "Find the projection."},
		{Kind: agent.HistoryTool, Run: 1, Turn: 1, CallID: "call-9", Name: "read_file", OK: true, Code: "ok", Output: "contents"},
	})
	deliverChild(t, app, host, obs, agent.ToolStarted{Turn: 1, Call: model.ToolCall{ID: "call-9", Name: "read_file"}})
	deliverChild(t, app, host, obs, agent.ToolCompleted{Turn: 1, CallID: "call-9", Name: "read_file", OK: true, Code: tools.CodeOK, Output: "contents"})
	if got := countChildTools(app); got != 1 {
		t.Fatalf("tool items = %d, want the snapshot's one completed call", got)
	}
	if item := app.childFocus.items[len(app.childFocus.items)-1]; item.Pending {
		t.Fatalf("paired item = %+v, want the snapshot's completed item", item)
	}
}

func TestChildFocusIgnoresQueuedNoticesTheSnapshotCovers(t *testing.T) {
	app, host, obs := focusedChild(t, []agent.HistoryItem{
		{Kind: agent.HistoryRun, Run: 1, Model: "deepseek-v4.1-flash"},
		{Kind: agent.HistoryNotice, Run: 1, Turn: 1, NoticeKind: agent.NoticeRetry, Text: "model retry 1 (timeout)"},
	})
	deliverChild(t, app, host, obs, agent.Notice{Kind: agent.NoticeRetry, Text: "model retry 1 (timeout)"})
	notices := 0
	for _, item := range app.childFocus.items {
		if item.Kind == agent.HistoryNotice {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("notice items = %d, want the snapshot's one notice", notices)
	}
}

func TestChildFocusFinishedClearsQueuedLinesWhenTheSnapshotFails(t *testing.T) {
	app, host, _ := focusedChild(t, liveChildItems())
	typeText(t, app, "focus on the parser")
	press(t, app, "enter")
	if len(app.childFocus.pending) != 1 {
		t.Fatalf("pending steers = %+v", app.childFocus.pending)
	}
	host.mu.Lock()
	host.childHistoryErr = agent.ErrSubagentNotRunning
	host.mu.Unlock()
	deliver(app, agent.SubagentFinished{
		CallID: "call-1", Index: 0, Agent: "explore", Session: "child-1",
		Status: "completed", ChildTurns: 1,
	})
	if len(app.childFocus.pending) != 0 {
		t.Fatalf("a finished child still shows queued lines: %+v", app.childFocus.pending)
	}
	if !app.childFocus.readOnly {
		t.Fatal("a finished child is not read-only")
	}
}

func TestChildSinkAfterRunStopReturnsNil(t *testing.T) {
	host := newManualHost()
	app := newTestApp(t, host)
	run, _ := startTestRun(t, app, "delegate the work")
	callback := host.childCallback()
	if callback == nil {
		t.Fatal("the app installed no child callback")
	}
	run.done <- agent.Result{Status: agent.StopCompleted, CumulativeTurns: 1}
	close(run.done)
	finishRun(t, app, agent.Result{Status: agent.StopCompleted, CumulativeTurns: 1})

	if err := callback(ChildEvent{Child: childObs("child-1"), Event: agent.TextDelta{Text: "late"}}); err != nil {
		t.Fatalf("a sink invoked after the run stopped returned %v, want nil", err)
	}
}
