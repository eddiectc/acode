package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/eddiectc/acode/internal/tui"
	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

const (
	// maxPreviousSessions bounds the /resume picker. The list is ordered by
	// modification time, so the cap drops the oldest entries.
	maxPreviousSessions = 100
	// sessionHeadEvents is enough for session_start and the first task while
	// keeping /resume independent of the full log size.
	sessionHeadEvents = 64
	// sessionPreviewBytes bounds the first-message preview shown per row.
	sessionPreviewBytes = 160
)

// prepareRunLocked resolves the next-run model and thinking effort against the
// loaded catalog. An explicit picker/option value replaces the inherited effort;
// either way, a value the effective catalog model does not advertise is cleared
// with a notice so the provider default is used instead of sending an
// unsupported field.
func (h *tuiHost) prepareRunLocked(opts tui.RunOptions, warn func(string)) (string, model.Dialect, error) {
	modelName := opts.Model
	if modelName == "" {
		modelName = h.info.Model
	}
	if modelName == "" {
		modelName = h.cfg.Model
	}
	if modelName != h.cfg.Model {
		m, ok := h.catalog.Models[modelName]
		if !ok {
			return "", "", fmt.Errorf("model %q is not in the loaded catalog", modelName)
		}
		if err := applyCatalogModel(&h.cfg, m, warn); err != nil {
			return "", "", err
		}
		h.cfg.Pricing = catalogPrice(h.catalog, modelName)
		if m.Deprecated {
			warn("model " + m.ID + " is deprecated")
		}
		if err := validateEffective(&h.cfg); err != nil {
			return "", "", err
		}
	}
	resolveRunThinkingEffort(&h.cfg, h.catalog.Models, modelName, opts, warn)
	return modelName, h.cfg.Dialect, nil
}

// sessionSwitchReadyLocked reports whether the host can replace its store.
func (h *tuiHost) sessionSwitchReadyLocked() error {
	if h.closed {
		return errors.New("session host is closed")
	}
	if h.active != nil {
		return errors.New("a run is already active")
	}
	return nil
}

// rebuildModelsLocked refreshes picker entries for the effective model after a
// session switch. Without a loaded catalog the recorded model remains a
// visible, unswitchable fallback, exactly as at startup.
func (h *tuiHost) rebuildModelsLocked() {
	if h.catalog.Models == nil {
		h.models = []tui.ModelChoice{h.recordedChoiceLocked(false)}
		return
	}
	h.setCatalogLocked(h.catalog)
}

// sessionInfo builds the immutable TUI header for a store and its reconstructed
// state.
func sessionInfo(cfg config, store *session.Store, state agent.State) tui.Info {
	return tui.Info{
		SessionID:      store.ID(),
		TracePath:      store.Path(),
		Model:          cfg.Model,
		Dialect:        string(cfg.Dialect),
		Workspace:      cfg.Workspace,
		MaxTurns:       cfg.MaxTurns,
		MaxTotalTokens: cfg.MaxTotalTokens,
		Turns:          state.Turns,
		Usage:          state.Usage,
		ContextTokens:  state.ContextTokens,
		ContextLimit:   cfg.MaxContextTokens,
		CostMicroUSD:   state.CostMicroUSD,
		CostComplete:   state.CostComplete,
		ThinkingEffort: cfg.ThinkingEffort,
	}
}

// ListSessions returns previous sessions recorded for the current workspace,
// newest first. The active session is excluded; a selected session may still
// fail to open if another process holds its lock.
func (h *tuiHost) ListSessions() ([]tui.PreviousSession, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return nil, errors.New("session host is closed")
	}
	return listPreviousSessions(h.cfg.StateDir, h.ws.Dir(), h.sessionID)
}

// NewSession replaces the current session with a fresh log in the same state
// directory and workspace. It records the supplied next-run settings in the new
// session_start.
func (h *tuiHost) NewSession(opts tui.RunOptions) (tui.SessionSnapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.sessionSwitchReadyLocked(); err != nil {
		return tui.SessionSnapshot{}, err
	}
	var warnings []string
	warn := func(text string) { warnings = append(warnings, text) }
	if _, _, err := h.prepareRunLocked(opts, warn); err != nil {
		return tui.SessionSnapshot{}, err
	}
	cfg := h.cfg
	cfg.ResumeID = ""
	cfg.ForkID = ""
	// A new session starts a fresh contract. The prompt, versions, agent, and
	// grant on h.cfg are replay values inherited from whichever session was
	// resumed or forked last; carrying them here would make /new record a
	// session contract it never ran — a pre-bump prompt and version, or a
	// batch grant that withholds delegate from a TUI session.
	cfg.PromptVersion = agent.PromptVersion
	cfg.ToolsetVersion = tools.Version
	cfg.SystemPrompt = ""
	cfg.Agent = ""
	cfg.Tools = nil
	store, err := session.CreateNew(cfg.StateDir)
	if err != nil {
		return tui.SessionSnapshot{}, fmt.Errorf("create session log: %w", err)
	}
	if err := agent.WriteSessionStart(store, runnerConfig(cfg), nil); err != nil {
		_ = store.Close()
		return tui.SessionSnapshot{}, err
	}
	history, err := agent.History(store.Events())
	if err != nil {
		_ = store.Close()
		return tui.SessionSnapshot{}, fmt.Errorf("read new session: %w", err)
	}
	old := h.store
	h.cfg = cfg
	h.store = store
	h.sessionID = store.ID()
	h.info = sessionInfo(cfg, store, agent.State{})
	h.lastSpawner = nil
	h.lastStatus = ""
	h.lastReason = ""
	h.pendingWarning = ""
	h.rebuildModelsLocked()
	notices := warnings
	if err := old.Close(); err != nil {
		notices = append(notices, "close previous session: "+sanitizeText(err.Error()))
	}
	return tui.SessionSnapshot{
		Info:    h.info,
		History: history,
		Models:  append([]tui.ModelChoice(nil), h.models...),
		Notices: notices,
	}, nil
}

// ResumeSession replaces the current session with a previous session recorded
// for the same workspace. The current store is closed only after the target has
// been validated, so a failed resume leaves the active session untouched.
func (h *tuiHost) ResumeSession(id string) (tui.SessionSnapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.sessionSwitchReadyLocked(); err != nil {
		return tui.SessionSnapshot{}, err
	}
	canonical, err := session.CanonicalID(id)
	if err != nil {
		return tui.SessionSnapshot{}, fmt.Errorf("resume session: %w", err)
	}
	if canonical == h.sessionID {
		return tui.SessionSnapshot{}, errors.New("session is already active")
	}
	store, err := session.Open(h.cfg.StateDir, canonical)
	if err != nil {
		return tui.SessionSnapshot{}, err
	}
	events := store.Events()
	state, err := agent.Inspect(events)
	if err != nil {
		_ = store.Close()
		return tui.SessionSnapshot{}, fmt.Errorf("read session %s: %w", sanitizeText(canonical), err)
	}
	if state.Workspace != h.ws.Dir() {
		_ = store.Close()
		return tui.SessionSnapshot{}, fmt.Errorf("session workspace %s does not match %s",
			sanitizeText(state.Workspace), sanitizeText(h.ws.Dir()))
	}
	cfg := h.cfg
	cfg.ResumeID = canonical
	cfg.ForkID = ""
	// A session switch inherits the target's recorded model, base URL, and
	// thinking effort. Runtime budgets and the explicit provider override stay
	// on the current invocation, matching -resume.
	cfg.modelSet = false
	cfg.thinkingEffortSet = false
	if !cfg.baseURLSet {
		cfg.BaseURL = ""
	}
	var warnings []string
	warn := func(text string) { warnings = append(warnings, text) }
	if err := applySessionDefaults(context.Background(), &cfg, state, h.source, warn); err != nil {
		_ = store.Close()
		return tui.SessionSnapshot{}, err
	}
	history, err := agent.History(events)
	if err != nil {
		_ = store.Close()
		return tui.SessionSnapshot{}, fmt.Errorf("read session %s: %w", sanitizeText(canonical), err)
	}
	old := h.store
	h.cfg = cfg
	h.store = store
	h.sessionID = store.ID()
	h.info = sessionInfo(cfg, store, state)
	h.lastSpawner = nil
	h.lastStatus = ""
	h.lastReason = ""
	h.pendingWarning = ""
	h.rebuildModelsLocked()
	notices := warnings
	if err := old.Close(); err != nil {
		notices = append(notices, "close previous session: "+sanitizeText(err.Error()))
	}
	return tui.SessionSnapshot{
		Info:    h.info,
		History: history,
		Models:  append([]tui.ModelChoice(nil), h.models...),
		Notices: notices,
	}, nil
}

// listPreviousSessions lists the sessions recorded for workspace. The SDK owns
// the storage layout, the head read, the newest-first order, and the cap; the
// active session and delegated children are filtered here.
func listPreviousSessions(stateDir, workspace, current string) ([]tui.PreviousSession, error) {
	summaries, err := engine.ListSummaries(context.Background(), stateDir, engine.ListOptions{
		Workspace: workspace,
		Limit:     maxPreviousSessions + 1,
		MaxEvents: sessionHeadEvents,
	})
	if err != nil {
		return nil, err
	}
	choices := make([]tui.PreviousSession, 0, min(len(summaries), maxPreviousSessions))
	for _, summary := range summaries {
		if len(choices) >= maxPreviousSessions {
			break
		}
		if summary.ID == current {
			continue
		}
		choices = append(choices, tui.PreviousSession{
			ID:       summary.ID,
			Modified: summary.UpdatedAt,
			Model:    tui.SanitizeLine(summary.Model),
			Summary:  sessionPreview(summary.FirstMessage),
		})
	}
	return choices, nil
}

// sessionPreview makes the first user message a single sanitized picker line.
func sessionPreview(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	if text == "" {
		return ""
	}
	return tui.SanitizeLine(tui.CapText(tui.SanitizeLine(text), sessionPreviewBytes))
}
