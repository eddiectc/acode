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
// loaded catalog and stores them in the per-run selection. An explicit
// picker/option value replaces the inherited effort; either way, a value the
// effective catalog model does not advertise is cleared with a notice so the
// provider default is used instead of sending an unsupported field. The
// session contract is never rewritten.
func (h *tuiHost) prepareRunLocked(opts tui.RunOptions, warn func(string)) error {
	sel := h.next
	modelName := opts.Model
	if modelName == "" {
		modelName = sel.model
	}
	if modelName == "" {
		modelName = h.cfg.Model
	}
	if modelName != sel.model {
		m, ok := h.catalog.Models[modelName]
		if !ok {
			return fmt.Errorf("model %q is not in the loaded catalog", modelName)
		}
		cfg := h.cfg
		if err := applyCatalogModel(&cfg, m, warn); err != nil {
			return err
		}
		cfg.Pricing = catalogPrice(h.catalog, modelName)
		if m.Deprecated {
			warn("model " + m.ID + " is deprecated")
		}
		if err := validateEffective(&cfg); err != nil {
			return err
		}
		sel = runSelection{
			model:   cfg.Model,
			dialect: cfg.Dialect,
			baseURL: cfg.BaseURL,
			limits:  cfg.Limits,
			pricing: cfg.Pricing,
			effort:  sel.effort,
		}
	}
	resolveRunThinkingEffort(&sel, h.catalog.Models, modelName, opts, warn)
	h.next = sel
	h.info.Model = sel.model
	h.info.Dialect = string(sel.dialect)
	h.info.ContextLimit = sel.limits.MaxContextTokens
	return nil
}

// sessionSwitchReadyLocked reports whether the host can replace its engine
// host.
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

// tuiSessionPolicy runs the app-owned part of session opening, exactly as batch
// does: inspect the recorded state, load a catalog only when an explicit model
// or effort needs it, then apply the resume/fork defaults and the task rules.
// It returns the exit code the same failure would produce at startup and the
// failure itself; the caller decides whether to print or surface it.
func tuiSessionPolicy(ctx context.Context, cfg *config, id string, warn func(string)) (model.CatalogSource, int, error) {
	snapshot, err := engine.Inspect(ctx, cfg.StateDir, id)
	if err != nil {
		return nil, engineExitCode(err), err
	}
	state := snapshot.State
	if len(state.CustomTools) > 0 {
		return nil, exitConfig, fmt.Errorf("session %s records caller-defined tools; the TUI cannot provide their implementations", id)
	}
	source, err := sessionCatalogSource(*cfg)
	if err != nil {
		return nil, exitConfig, err
	}
	if err := applySessionDefaults(ctx, cfg, state, source, warn); err != nil {
		return nil, catalogExitCode(err), err
	}
	if err := validateSessionRules(cfg, state); err != nil {
		return nil, exitConfig, err
	}
	return source, 0, nil
}

// sessionSnapshotLocked installs a freshly opened engine host as the active
// session and returns the state the TUI needs. The caller holds h.mu. The old
// host is closed only after the new one is fully readable, so a failed switch
// leaves the current session intact.
func (h *tuiHost) sessionSnapshotLocked(host *engine.Host, notes []string) (tui.SessionSnapshot, error) {
	cfg := h.cfg
	resolved := host.Spec()
	applyResolvedSpec(&cfg, resolved)
	state, err := host.State()
	if err != nil {
		_ = host.Close()
		return tui.SessionSnapshot{}, fmt.Errorf("read session %s: %w", sanitizeText(host.ID()), err)
	}
	history, err := host.History()
	if err != nil {
		_ = host.Close()
		return tui.SessionSnapshot{}, fmt.Errorf("read session %s: %w", sanitizeText(host.ID()), err)
	}
	old := h.host
	h.host = host
	h.cfg = cfg
	h.next = selectionFromSpec(resolved)
	h.info = sessionInfo(cfg, host, state)
	h.lastSpawner = nil
	h.lastStatus = ""
	h.lastReason = ""
	h.pendingWarning = ""
	h.rebuildModelsLocked()
	notices := notes
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

// sessionInfo builds the TUI header for an engine host and its reconstructed
// state.
func sessionInfo(cfg config, host *engine.Host, state agent.State) tui.Info {
	return tui.Info{
		SessionID:      host.ID(),
		TracePath:      host.Path(),
		Model:          cfg.Model,
		Dialect:        string(cfg.Dialect),
		Workspace:      host.Workspace(),
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
	return listPreviousSessions(h.host.StateDir(), h.host.Workspace(), h.host.ID())
}

// NewSession replaces the current session with a fresh log in the same state
// directory and workspace. It records the supplied next-run settings and reads
// the workspace instructions once, through the engine, so the new contract
// carries them exactly as a started-from-scratch TUI session does.
func (h *tuiHost) NewSession(opts tui.RunOptions) (tui.SessionSnapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if err := h.sessionSwitchReadyLocked(); err != nil {
		return tui.SessionSnapshot{}, err
	}
	var warnings []string
	warn := func(text string) { warnings = append(warnings, text) }
	previousNext, previousInfo := h.next, h.info
	if err := h.prepareRunLocked(opts, warn); err != nil {
		return tui.SessionSnapshot{}, err
	}
	instructions := workspaceInstructionsForNewSession(h.host.WorkspaceHandle(), warn)
	cfg := h.effectiveConfigLocked()
	cfg.ResumeID = ""
	cfg.ForkID = ""
	// A new session starts a fresh contract. The prompt, versions, agent, and
	// grant on h.cfg are replay values inherited from whichever session was
	// resumed or forked last; carrying them here would make /new record a
	// session contract it never ran — a pre-bump prompt and version, or a
	// batch grant that withholds delegate from a TUI session. The engine
	// records the current defaults and the workspace text.
	cfg.PromptVersion = agent.PromptVersion
	cfg.ToolsetVersion = tools.Version
	cfg.Agent = ""
	host, err := engine.Open(context.Background(), tuiEngineSpec(cfg, engine.ModeNew, "", instructions, h.agents))
	if err != nil {
		h.next, h.info = previousNext, previousInfo
		return tui.SessionSnapshot{}, err
	}
	return h.sessionSnapshotLocked(host, warnings)
}

// ResumeSession replaces the current session with a previous session recorded
// for the same workspace. The target is validated and fully opened before the
// current host is closed, so a failed resume leaves the active session
// untouched. The recorded model, effort, prompt, and grant are replayed
// exactly; only the next run's selection starts from them.
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
	if canonical == h.host.ID() {
		return tui.SessionSnapshot{}, errors.New("session is already active")
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
	// The switch stays in this process's workspace; the engine rejects a
	// target recorded elsewhere.
	cfg.Workspace = h.host.Workspace()
	cfg.workspaceSet = true
	var warnings []string
	warn := func(text string) { warnings = append(warnings, text) }
	if _, _, err := tuiSessionPolicy(context.Background(), &cfg, canonical, warn); err != nil {
		return tui.SessionSnapshot{}, err
	}
	host, err := engine.Open(context.Background(), tuiEngineSpec(cfg, engine.ModeResume, canonical, "", h.agents))
	if err != nil {
		return tui.SessionSnapshot{}, err
	}
	return h.sessionSnapshotLocked(host, warnings)
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
