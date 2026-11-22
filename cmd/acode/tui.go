package main

import (
	"context"
	"encoding/json/jsontext"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/charmbracelet/colorprofile"

	"github.com/eddiectc/acode/internal/tui"
	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

// hostRun is one run started by the TUI host.
type hostRun struct {
	ctx      context.Context
	cancel   context.CancelFunc
	control  *runController
	spawner  *agent.Spawner
	done     chan agent.Result
	finished chan struct{}
}

func (r *hostRun) Interrupt() {
	if r.control != nil {
		r.control.Interrupt()
	}
}

func (r *hostRun) Terminate() {
	if r.cancel != nil {
		r.cancel()
	}
}

func (r *hostRun) Done() <-chan agent.Result { return r.done }

// tuiHost implements tui.Host over an engine host, which owns the durable
// session, workspace, work directory, and resolved contract. tuiHost owns only
// app policy (definitions, operator overrides, catalog, settings) and display
// state. Per-run model choices live in next; they never rewrite the session.
type tuiHost struct {
	host    *engine.Host
	cfg     config
	next    runSelection
	source  model.CatalogSource
	catalog model.Catalog
	models  []tui.ModelChoice
	// agents is the effective catalog loaded for this process, with the
	// startup overrides applied. It is what a new run's spawner uses.
	agents *agent.AgentCatalog
	// baseAgents is the discovered catalog before overrides. The definitions
	// browser applies the current overrides to a copy for display, so a saved
	// setting is visible immediately without changing what this process runs.
	baseAgents     *agent.AgentCatalog
	agentOverrides map[string]agent.AgentOverride

	mu             sync.Mutex
	info           tui.Info
	active         *hostRun
	lastSpawner    *agent.Spawner
	closed         bool
	lastStatus     agent.StopReason
	lastReason     string
	pendingWarning string
}

func (h *tuiHost) Info() tui.Info {
	h.mu.Lock()
	defer h.mu.Unlock()
	info := h.info
	info.ThinkingEffort = h.next.effort
	return info
}

func (h *tuiHost) Models() []tui.ModelChoice {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]tui.ModelChoice(nil), h.models...)
}

func (h *tuiHost) History() ([]agent.HistoryItem, error) {
	return h.host.History()
}

// modelChoice converts a normalized catalog model into picker metadata. The
// effort slice is cloned so a picker edit can never mutate the loaded catalog.
func modelChoice(m model.CatalogModel) tui.ModelChoice {
	if m.Name == "" {
		m.Name = m.ID
	}
	m.ReasoningEfforts = append([]string(nil), m.ReasoningEfforts...)
	return tui.ModelChoice{CatalogModel: m, Switchable: true}
}

// recordedChoiceLocked describes the session's current model from recorded
// values. It is the offline picker fallback and is never switchable then.
func (h *tuiHost) recordedChoiceLocked(switchable bool) tui.ModelChoice {
	name := h.next.model
	if name == "" {
		name = h.info.Model
	}
	return tui.ModelChoice{
		CatalogModel: model.CatalogModel{
			ID:            name,
			Name:          name,
			Dialect:       h.next.dialect,
			ContextWindow: h.next.limits.MaxContextTokens,
			InputLimit:    h.next.limits.MaxInputTokens,
		},
		Switchable: switchable,
	}
}

// catalogPrice returns the model's price snapshot with this catalog's
// provenance. An unknown or unpriced model yields the zero snapshot.
func catalogPrice(cat model.Catalog, id string) model.Pricing {
	pricing, _ := cat.Price(id)
	return pricing
}

// setCatalogLocked replaces the picker entries. A current recorded model that
// is absent from the catalog stays visible as the current entry.
func (h *tuiHost) setCatalogLocked(cat model.Catalog) {
	choices := make([]tui.ModelChoice, 0, len(cat.Models)+1)
	for _, m := range cat.Usable() {
		choices = append(choices, modelChoice(m))
	}
	if _, ok := cat.Models[h.next.model]; !ok {
		choices = append(choices, h.recordedChoiceLocked(true))
	}
	sort.Slice(choices, func(i, j int) bool { return choices[i].ID < choices[j].ID })
	h.catalog = cat
	h.models = choices
}

// RefreshModels performs the explicit picker refresh (Ctrl-R). A failure
// leaves the loaded entries untouched.
func (h *tuiHost) RefreshModels(ctx context.Context) ([]tui.ModelChoice, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errors.New("session host is closed")
	}
	cfg := h.cfg
	source := h.source
	h.mu.Unlock()

	if source == nil {
		var err error
		source, err = newCatalogSource(cfg)
		if err != nil {
			return nil, err
		}
	}
	cat, err := source.Refresh(ctx, cfg.Provider)
	if err != nil {
		return nil, err
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.source = source
	h.setCatalogLocked(cat)
	// A cache-write failure is a catalog warning, not a failed refresh.
	h.pendingWarning = strings.Join(cat.Warnings, "; ")
	return append([]tui.ModelChoice(nil), h.models...), nil
}

// SelectModel resolves a picker selection and applies its dialect, base URL,
// context window, and input ceiling to the next run only. The session contract
// is untouched; a failed selection leaves the previous run selection usable.
func (h *tuiHost) SelectModel(id string) (tui.ModelChoice, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return tui.ModelChoice{}, errors.New("session host is closed")
	}
	h.pendingWarning = ""
	if id == h.next.model {
		// Keep the recorded fallback's exact limits, but prefer loaded catalog
		// metadata so a re-selection still carries the advertised efforts.
		if m, ok := h.catalog.Models[id]; ok && m.Validate() == nil {
			h.next.pricing = catalogPrice(h.catalog, id)
			return modelChoice(m), nil
		}
		return h.recordedChoiceLocked(true), nil
	}
	m, ok := h.catalog.Models[id]
	if !ok {
		return tui.ModelChoice{}, fmt.Errorf("model %q is not in the loaded catalog", id)
	}
	if err := m.Validate(); err != nil {
		return tui.ModelChoice{}, fmt.Errorf("model %q: %w", id, err)
	}
	cfg := h.cfg
	if err := applyCatalogModel(&cfg, m, func(text string) { h.pendingWarning = text }); err != nil {
		return tui.ModelChoice{}, err
	}
	cfg.Pricing = catalogPrice(h.catalog, id)
	if err := validateEffective(&cfg); err != nil {
		return tui.ModelChoice{}, err
	}
	h.next = runSelection{
		model:   cfg.Model,
		dialect: cfg.Dialect,
		baseURL: cfg.BaseURL,
		limits:  cfg.Limits,
		pricing: cfg.Pricing,
		effort:  h.next.effort,
	}
	h.info.ContextLimit = cfg.Limits.MaxContextTokens
	return modelChoice(m), nil
}

// TakeModelWarning returns and clears the warning from the last successful
// picker selection. It implements tui.ModelWarningProvider.
func (h *tuiHost) TakeModelWarning() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	warning := h.pendingWarning
	h.pendingWarning = ""
	return warning
}

func (h *tuiHost) Workspace() *tools.Workspace { return h.host.WorkspaceHandle() }

// workspaceRoot is the engine workspace root used by operator-state path
// rules. It is empty before a host exists (unit tests of config-file policy
// construct a host without one).
func (h *tuiHost) workspaceRoot() string {
	if h.host == nil {
		return ""
	}
	return h.host.Workspace()
}

// Agents returns the definitions the browser should show: the declared set
// with the current operator overrides applied for display. The process keeps
// the catalog it loaded for running children, so a saved override takes effect
// on the next process. It never re-reads the definition directory.
func (h *tuiHost) Agents() []tui.AgentDefView {
	h.mu.Lock()
	defer h.mu.Unlock()
	return agentViews(h.overriddenAgentsLocked())
}

// overriddenAgentsLocked applies the current overrides to a copy of the
// declared catalog for the browser. The startup catalog already validated
// them and a save validates its candidate before writing, so a failure here is
// unreachable; falling back to the declared set keeps the browser usable.
func (h *tuiHost) overriddenAgentsLocked() *agent.AgentCatalog {
	if len(h.agentOverrides) == 0 {
		return h.baseAgents
	}
	catalog, err := agent.ApplyAgentOverrides(h.baseAgents, h.agentOverrides)
	if err != nil {
		return h.baseAgents
	}
	return catalog
}

// SetAgentOverride replaces one agent's operator override and persists it to
// the active config file. An empty override removes the entry. The definition
// file still owns the tool grant and the prompt body; this surface only routes
// the model, effort, turn cap, and enabled state. A nil base catalog means no
// definitions were loaded, so every name is unknown.
func (h *tuiHost) SetAgentOverride(name string, override agent.AgentOverride) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	path := h.cfg.ConfigPath
	if path == "" {
		return errors.New("the config file is disabled (-config \"\"); agent overrides cannot be saved")
	}
	if _, ok := h.baseAgents.LookupDefinition(name); !ok {
		return fmt.Errorf("agent_overrides[%q]: no agent definition with that name", name)
	}
	if withinWorkspace(h.workspaceRoot(), path) {
		return errors.New("the config file is inside the workspace; agent overrides are operator state and are never written there")
	}
	// Re-read so an external edit to another key, or to another agent's
	// override, is preserved by the rewrite.
	file, err := loadFileConfig(path, false)
	if err != nil {
		return err
	}
	candidate := file.agentOverrides()
	if override.Empty() {
		if _, ok := candidate[name]; !ok {
			return nil
		}
		delete(candidate, name)
	} else {
		if candidate == nil {
			candidate = make(map[string]agent.AgentOverride, 1)
		}
		candidate[name] = override
	}
	if _, err := agent.ApplyAgentOverrides(h.baseAgents, candidate); err != nil {
		return err
	}
	if len(candidate) == 0 {
		file.AgentOverrides = nil
	} else {
		file.AgentOverrides = make(map[string]fileAgentOverride, len(candidate))
		for key, value := range candidate {
			file.AgentOverrides[key] = fromAgent(value)
		}
	}
	if err := writeFileConfig(path, file); err != nil {
		return err
	}
	h.agentOverrides = candidate
	return nil
}

// ClearAgentOverrides removes every operator override for one agent.
func (h *tuiHost) ClearAgentOverrides(name string) error {
	return h.SetAgentOverride(name, agent.AgentOverride{})
}

// withinWorkspace reports whether path is the workspace or resolves into it.
// tools.Workspace.Contains answers the other direction (a workspace-relative
// path that stays inside); operator state must also be refused when it is
// lexically outside but symlinks into the workspace, so the resolved target is
// checked here.
func withinWorkspace(root, path string) bool {
	if root == "" {
		return false
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return false
	}
	target, err := filepath.Abs(path)
	if err != nil {
		return false
	}
	if pathWithin(absRoot, target) {
		return true
	}
	if resolved, err := filepath.EvalSymlinks(target); err == nil {
		return pathWithin(absRoot, resolved)
	}
	// The file may not exist yet, so resolve the directory it will be created
	// in and check the joined path.
	if resolvedDir, err := filepath.EvalSymlinks(filepath.Dir(target)); err == nil {
		return pathWithin(absRoot, filepath.Join(resolvedDir, filepath.Base(target)))
	}
	return false
}

// pathWithin reports whether target is root or one of its descendants. Both
// paths must be absolute and clean.
func pathWithin(root, target string) bool {
	return target == root || strings.HasPrefix(target, root+string(filepath.Separator))
}

// SteerSubagent routes one composer line to a running delegated child. The
// child consumes it at its next turn boundary and records it with
// source=subagent-steer.
func (h *tuiHost) SteerSubagent(sessionID string, line agent.SteeringLine) error {
	h.mu.Lock()
	run := h.active
	h.mu.Unlock()
	if run == nil || run.spawner == nil {
		return fmt.Errorf("no run is active")
	}
	return run.spawner.SteerChild(sessionID, line)
}

// SubagentHistory reads one delegated child's display transcript. The latest
// run's spawner is kept after the run ends, so a finished child stays readable
// until the operator leaves the view.
func (h *tuiHost) SubagentHistory(sessionID string) ([]agent.HistoryItem, error) {
	h.mu.Lock()
	spawner := h.lastSpawner
	h.mu.Unlock()
	if spawner == nil {
		return nil, fmt.Errorf("no delegated child has run in this session")
	}
	return spawner.ChildHistory(sessionID)
}

// agentViews converts the loaded catalog into browser rows: active
// definitions first, then the invalid files whose parse errors a user needs to
// see. The model, effort, and cap are the effective values after overrides;
// the declared values and a source label say where each came from.
func agentViews(catalog *agent.AgentCatalog) []tui.AgentDefView {
	if catalog == nil {
		return nil
	}
	views := make([]tui.AgentDefView, 0, len(catalog.Agents)+len(catalog.Invalid))
	for _, def := range catalog.Agents {
		modelSource, effortSource, maxTurnsSource := agentProvenance(def)
		status := "active"
		if def.Disabled {
			status = "disabled"
		}
		views = append(views, tui.AgentDefView{
			Name:             def.Name,
			Description:      def.Description,
			Model:            def.Model,
			Effort:           def.ThinkingEffort,
			ModelSource:      modelSource,
			EffortSource:     effortSource,
			MaxTurnsSource:   maxTurnsSource,
			DeclaredModel:    def.DeclaredModel,
			DeclaredEffort:   def.DeclaredThinkingEffort,
			DeclaredMaxTurns: def.DeclaredMaxTurns,
			Override:         def.Override,
			Enabled:          !def.Disabled,
			Source:           def.Source,
			Path:             def.Path,
			Tools:            append([]string(nil), def.Tools...),
			MaxTurns:         def.MaxTurns,
			Status:           status,
			PromptFirstLine:  firstLine(def.Prompt),
		})
	}
	for _, def := range catalog.Invalid {
		views = append(views, tui.AgentDefView{
			Name:   def.Name,
			Source: def.Source,
			Path:   def.Path,
			Status: "invalid",
			Err:    def.Err,
		})
	}
	return views
}

// agentProvenance labels where an effective routing value came from. The
// browser shows the label in the expanded row and marks an overridden value
// with an asterisk.
func agentProvenance(def agent.AgentDefinition) (model, effort, maxTurns string) {
	model = "inherit"
	if def.Model != "" {
		model = "definition"
	}
	maxTurns = "inherit"
	if def.MaxTurns > 0 {
		maxTurns = "definition"
	}
	effort = "inherit"
	if def.ThinkingEffort != "" {
		effort = "definition"
	}
	if def.Override != nil {
		if def.Override.Model != nil {
			model = "override"
		}
		if def.Override.ThinkingEffort != nil {
			effort = "override"
		} else if def.Override.Model != nil && def.DeclaredModel != def.Model {
			// A model override without an effort resets the child to the new
			// model's default, so the declared effort no longer applies.
			effort = "model default"
		}
		if def.Override.MaxTurns != nil {
			maxTurns = "override"
		}
	}
	return model, effort, maxTurns
}

// firstLine collapses the first non-empty prompt line for the expanded row.
func firstLine(text string) string {
	for line := range strings.SplitSeq(text, "\n") {
		collapsed := strings.Join(strings.Fields(line), " ")
		if collapsed != "" {
			return tui.CapText(tui.SanitizeLine(collapsed), 200)
		}
	}
	return ""
}

// resolveAgentModel resolves an agent's model and effort through the loaded
// catalog without mutating the host configuration. A child that inherits the
// run's model gets the run selection's recorded values, so an offline resume
// stays usable and a switched base URL reaches the child.
func (h *tuiHost) resolveAgentModel(modelID, effort string) (agent.ModelResolution, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return agent.ModelResolution{}, errors.New("session host is closed")
	}
	m, ok := h.catalog.Models[modelID]
	if !ok {
		if modelID == h.next.model {
			return agent.ModelResolution{
				Dialect: string(h.next.dialect),
				BaseURL: h.next.baseURL,
				Limits:  h.next.limits,
				Pricing: h.next.pricing,
			}, nil
		}
		return agent.ModelResolution{}, fmt.Errorf("model %q is not in the loaded catalog", modelID)
	}
	if err := m.Validate(); err != nil {
		return agent.ModelResolution{}, fmt.Errorf("model %q: %w", modelID, err)
	}
	if effort != "" {
		if err := validateModelThinkingEffort(effort, m); err != nil {
			return agent.ModelResolution{}, err
		}
	}
	cfg := h.cfg
	if err := applyCatalogModel(&cfg, m, func(string) {}); err != nil {
		return agent.ModelResolution{}, err
	}
	if err := validateEffective(&cfg); err != nil {
		return agent.ModelResolution{}, err
	}
	return agent.ModelResolution{
		Dialect: string(cfg.Dialect),
		BaseURL: cfg.BaseURL,
		Limits:  cfg.Limits,
		Pricing: catalogPrice(h.catalog, modelID),
	}, nil
}

// TodoItems returns the checklist recorded by the active session log so the
// TUI widget can show it before the next todo call and after a session switch.
func (h *tuiHost) TodoItems() []tools.TodoItem {
	return h.host.Todo()
}

// effectiveConfigLocked is the app configuration with the next-run selection
// applied. It is what the details and settings views describe.
func (h *tuiHost) effectiveConfigLocked() config {
	cfg := h.cfg
	cfg.Model = h.next.model
	cfg.Dialect = h.next.dialect
	cfg.BaseURL = h.next.baseURL
	cfg.Limits = h.next.limits
	cfg.MaxContextTokens = h.next.limits.MaxContextTokens
	cfg.MaxOutputTokens = h.next.limits.MaxOutputTokens
	cfg.InputLimit = h.next.limits.MaxInputTokens
	cfg.Pricing = h.next.pricing
	cfg.ThinkingEffort = h.next.effort
	return cfg
}

// sessionReadState is what a log read needs from the host: the active session
// log path, the agent catalog used to rebuild a legacy prompt, and the effective
// log_requests setting. It is copied under the host lock so the read itself
// holds nothing.
type sessionReadState struct {
	path        string
	agents      []agent.AgentInfo
	logRequests bool
}

// readState copies the inputs for an on-demand log read. The engine host owns
// the path; a session switch replaces the host under the same lock, so a switch
// cannot interleave with the copy.
func (h *tuiHost) readState() (sessionReadState, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return sessionReadState{}, errors.New("session host is closed")
	}
	if h.host == nil {
		return sessionReadState{}, errors.New("no session is open")
	}
	return sessionReadState{
		path:        h.host.Path(),
		agents:      h.cfg.Agents,
		logRequests: h.cfg.LogRequests,
	}, nil
}

// Prompt returns the system prompt recorded for the active session. It reads
// the log through session.ReadSnapshot, which opens its own descriptor and
// takes no flock, so the store's exclusive lock is not a conflict and a run may
// keep appending while the view is built.
func (h *tuiHost) Prompt() (tui.PromptView, error) {
	state, err := h.readState()
	if err != nil {
		return tui.PromptView{}, err
	}
	return promptView(state.path, state.agents)
}

// promptView reads the recorded prompt from one session log. A log written
// before the prompt was recorded is rebuilt from its prompt version and labelled
// reconstructed: the rebuild uses the current identity and agent catalog, so the
// text can differ from what originally ran.
func promptView(path string, agents []agent.AgentInfo) (tui.PromptView, error) {
	events, err := session.ReadSnapshot(path)
	if err != nil {
		return tui.PromptView{}, fmt.Errorf("read session for prompt: %w", err)
	}
	state, err := agent.Inspect(events)
	if err != nil {
		return tui.PromptView{}, fmt.Errorf("inspect session for prompt: %w", err)
	}
	view := tui.PromptView{
		Text:    state.SystemPrompt,
		Version: state.PromptVersion,
		Tag:     state.PromptTag,
		Bytes:   len(state.SystemPrompt),
		Capped:  state.PromptCapped,
	}
	if view.Text != "" {
		return view, nil
	}
	rendered, err := agent.RenderSystemPrompt(state.PromptVersion, state.Workspace, acodeIdentity, agents)
	if err != nil {
		// Keep the version so the overlay can name what could not be rendered.
		return view, fmt.Errorf("reconstruct prompt version %q: %w", state.PromptVersion, err)
	}
	view.Text = rendered
	view.Bytes = len(rendered)
	view.Reconstructed = true
	return view, nil
}

// Exchange returns the latest projected model attempt in the active session.
// Selection, pairing, and the run boundary belong to the SDK reader; the host
// only formats the recorded payloads.
func (h *tuiHost) Exchange() (tui.ExchangeView, error) {
	state, err := h.readState()
	if err != nil {
		return tui.ExchangeView{}, err
	}
	return exchangeView(state.path, state.logRequests)
}

// exchangeView reads the newest model attempt from one session log and
// pretty-prints the recorded payloads.
func exchangeView(path string, logRequests bool) (tui.ExchangeView, error) {
	events, err := session.ReadSnapshot(path)
	if err != nil {
		return tui.ExchangeView{}, fmt.Errorf("read session for request: %w", err)
	}
	exchange, err := agent.LatestModelExchange(events)
	if err != nil {
		return tui.ExchangeView{}, fmt.Errorf("latest model exchange: %w", err)
	}
	view := tui.ExchangeView{
		RequestSeq:  exchange.RequestSeq,
		ResponseSeq: exchange.ResponseSeq,
		Run:         exchange.Run,
		Turn:        exchange.Turn,
		HasRequest:  exchange.HasRequest,
		HasResponse: exchange.HasResponse,
		LogRequests: logRequests,
	}
	if exchange.HasRequest {
		if view.Request, err = indentPayload(exchange.Request); err != nil {
			return tui.ExchangeView{}, fmt.Errorf("request seq %d: %w", exchange.RequestSeq, err)
		}
	}
	if exchange.HasResponse {
		if view.Response, err = indentPayload(exchange.Response); err != nil {
			return tui.ExchangeView{}, fmt.Errorf("response seq %d: %w", exchange.ResponseSeq, err)
		}
	}
	return view, nil
}

// indentPayload pretty-prints one recorded payload in place with the project's
// JSON writer, so field order and number spelling survive. It never re-encodes
// a local struct, which would drop fields the app does not know.
func indentPayload(raw []byte) (string, error) {
	value := jsontext.Value(raw)
	if err := value.Indent(jsontext.WithIndent("  ")); err != nil {
		return "", fmt.Errorf("format recorded payload: %w", err)
	}
	return string(value), nil
}

// Details returns the configuration and terminal details for the details
// overlay.
func (h *tuiHost) Details() tui.Details {
	h.mu.Lock()
	defer h.mu.Unlock()
	cfg := h.effectiveConfigLocked()
	return tui.Details{
		TracePath:        h.host.Path(),
		StateDir:         h.host.StateDir(),
		BaseURL:          cfg.BaseURL,
		Provider:         cfg.Provider,
		PromptVersion:    cfg.PromptVersion,
		ToolsetVersion:   cfg.ToolsetVersion,
		HarnessVersion:   cfg.HarnessVersion,
		MaxContextTokens: cfg.MaxContextTokens,
		MaxOutputTokens:  cfg.MaxOutputTokens,
		MaxOutputUnset:   !cfg.Limits.MaxOutputSet(),
		SessionTimeout:   cfg.SessionTimeout,
		ModelTimeout:     cfg.ModelTimeout,
		HTTPTimeout:      cfg.HTTPTimeout,
		ThinkingEffort:   cfg.ThinkingEffort,
	}
}

// resolveRunThinkingEffort applies the next run's effort selection and the
// model-switch rule: an explicit option (including the empty provider default)
// replaces the inherited value, and a value the effective catalog model does
// not advertise is cleared with a warning. A model absent from the catalog
// cannot be validated offline, so its recorded value is kept.
func resolveRunThinkingEffort(sel *runSelection, catalog map[string]model.CatalogModel, modelName string, opts tui.RunOptions, warn func(string)) {
	if opts.ThinkingEffortSet {
		sel.effort = opts.ThinkingEffort
	}
	if sel.effort == "" {
		return
	}
	if m, ok := catalog[modelName]; ok {
		sel.effort = clearedThinkingEffort(sel.effort, m, warn)
	}
}

// Start launches one run and returns its handle. The app supplies the display
// callback and steering channel; the host owns the goroutine and cleanup. The
// engine builds the registry, spawner, grant, todo seed, and work directories
// from the session contract plus this run's selection.
func (h *tuiHost) Start(sub tui.Submission, opts tui.RunOptions, steer <-chan agent.SteeringLine, display func(agent.DisplayEvent) error) (tui.Run, error) {
	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return nil, errors.New("session host is closed")
	}
	if h.active != nil {
		h.mu.Unlock()
		return nil, errors.New("a run is already active")
	}
	var warnings []string
	warn := func(text string) { warnings = append(warnings, text) }
	if err := h.prepareRunLocked(opts, warn); err != nil {
		h.mu.Unlock()
		return nil, err
	}
	sel := h.next

	runCtx, cancel := context.WithCancel(context.Background())
	control := newRunController(cancel)
	forward := display
	if forward != nil {
		forward = func(ev agent.DisplayEvent) error {
			select {
			case <-runCtx.Done():
				return nil
			default:
			}
			return display(ev)
		}
	}
	runner, spawner, err := h.host.BuildRunnerWithSpawner(engine.RunSpec{
		Model:          sel.model,
		Dialect:        string(sel.dialect),
		BaseURL:        sel.baseURL,
		Limits:         sel.limits,
		Pricing:        sel.pricing,
		ThinkingEffort: sel.effortOption(),
		Stream:         true,
		Interactive:    false,
		NewCompleter: func(dialect model.Dialect, baseURL, sessionID string) model.Completer {
			return newCompleterFor(dialect, baseURL, h.cfg.Token, h.cfg.HTTPTimeout, acodeUserAgent(h.cfg.HarnessVersion), sessionID)
		},
		ResolveModel: h.resolveAgentModel,
	}, forward, steer, control)
	if err != nil {
		cancel()
		h.mu.Unlock()
		return nil, err
	}
	run := &hostRun{
		ctx:      runCtx,
		cancel:   cancel,
		control:  control,
		spawner:  spawner,
		done:     make(chan agent.Result, 1),
		finished: make(chan struct{}),
	}
	h.active = run
	h.lastSpawner = spawner
	h.mu.Unlock()

	if display != nil {
		for _, text := range warnings {
			_ = display(agent.Notice{Kind: agent.NoticeWarning, Text: text})
		}
	}
	go h.run(run, runner, agent.RunOptions{
		Task:     sub.Text,
		TaskSet:  true,
		TaskFrom: sub.Source,
	}, sel)
	return run, nil
}

func (h *tuiHost) run(run *hostRun, runner *agent.Runner, opts agent.RunOptions, sel runSelection) {
	defer close(run.finished)
	var result agent.Result
	func() {
		defer func() {
			if recovered := recover(); recovered != nil {
				result = agent.Result{
					Status: agent.StopInternalError,
					Err:    fmt.Errorf("run goroutine panicked: %v", sanitizeText(fmt.Sprint(recovered))),
				}
			}
		}()
		var err error
		result, err = runner.Run(run.ctx, opts)
		if err != nil {
			result.Err = errors.Join(result.Err, err)
		}
	}()
	// The engine recreates the work tree on the next BuildRunner; a leftover
	// tree is not a run failure, exactly as before.
	_ = h.host.CleanupWork()
	if err := h.host.Store().Sync(); err != nil {
		result.Status = agent.StopInternalError
		result.Err = errors.Join(result.Err, err)
	}

	h.mu.Lock()
	if h.active == run {
		h.active = nil
	}
	h.info.Turns = result.CumulativeTurns
	h.info.Usage = result.CumulativeUsage
	h.info.ContextTokens = result.ContextTokens
	h.info.CostMicroUSD = result.CostMicroUSD
	h.info.CostComplete = result.CostComplete
	h.info.Model = sel.model
	h.info.Dialect = string(sel.dialect)
	h.lastStatus = result.Status
	h.lastReason = ""
	if result.Err != nil {
		h.lastReason = sanitizeText(result.Err.Error())
	}
	h.mu.Unlock()

	run.done <- result
	close(run.done)
}

// Close terminates any active run, waits for its durable cleanup, then closes
// the engine host, which syncs the log, releases the workspace, and removes the
// work tree.
func (h *tuiHost) Close() error {
	h.mu.Lock()
	h.closed = true
	active := h.active
	h.mu.Unlock()
	if active != nil {
		active.Terminate()
		<-active.finished
	}
	if h.host == nil {
		return nil
	}
	return h.host.Close()
}

func (h *tuiHost) summary() (tui.Info, agent.StopReason, string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.info, h.lastStatus, h.lastReason
}

// tuiEngineSpec builds the engine construction input for the TUI. The identity,
// tool options, and completer factory are supplied here; the per-run model
// choice is a RunSpec. ModeNew carries the workspace instructions through
// AppendPrompt so the engine folds, caps, records, and propagates them; resume
// and fork replay what the session recorded.
func tuiEngineSpec(cfg config, mode engine.Mode, sessionID, instructions string, definitions *agent.AgentCatalog) engine.Spec {
	effort := cfg.ThinkingEffort
	spec := engine.Spec{
		Workspace:      cfg.Workspace,
		StateDir:       cfg.StateDir,
		Mode:           mode,
		SessionID:      sessionID,
		Identity:       acodeIdentity,
		Provider:       cfg.Provider,
		Dialect:        string(cfg.Dialect),
		BaseURL:        cfg.BaseURL,
		Model:          cfg.Model,
		Limits:         cfg.Limits,
		Pricing:        cfg.Pricing,
		ThinkingEffort: &effort,
		Agents:         cfg.Agents,
		Agent:          cfg.Agent,
		UserAgent:      acodeUserAgent(cfg.HarnessVersion),
		ToolOptions:    toolOptions(cfg, "", ""),
		PromptVersion:  cfg.PromptVersion,
		ToolsetVersion: cfg.ToolsetVersion,
		MaxTurns:       cfg.MaxTurns,
		MaxTotalTokens: cfg.MaxTotalTokens,
		MaxSubagents:   cfg.MaxSubagents,
		RunTimeout:     cfg.SessionTimeout,
		ModelTimeout:   cfg.ModelTimeout,
		RequireUsage:   cfg.RequireUsage,
		LogRequests:    cfg.LogRequests,
		Version:        cfg.HarnessVersion,
		NewCompleter: func(dialect model.Dialect, baseURL, sessionID string) model.Completer {
			return newCompleterFor(dialect, baseURL, cfg.Token, cfg.HTTPTimeout, acodeUserAgent(cfg.HarnessVersion), sessionID)
		},
	}
	if definitions != nil {
		spec.Definitions = definitions.Agents
	}
	if mode == engine.ModeNew {
		spec.AppendPrompt = instructions
		spec.AppendPromptSet = instructions != ""
	}
	return spec
}

// openTUIHost opens the session shell used by the TUI and -ui-json. It
// validates resume/fork state before the terminal is touched and performs the
// app-owned policy before the engine validates and records the contract.
func openTUIHost(cfg config, stderr io.Writer) (*tuiHost, int) {
	var startupWarn []string
	warn := func(text string) { startupWarn = append(startupWarn, text) }

	// Definitions are loaded once per process: acode's five shipped roles as
	// the base, then the operator's <state-dir>/agents/*.md files, which
	// replace a shipped role by name. The recorded prompt and grant keep a
	// resumed session's replay exact, so this catalog only affects new
	// children and the /agents browser. Operator overrides are applied to a
	// copy; the declared values stay available for the browser's provenance
	// view.
	defaults, defErr := defaultAgentDefinitions()
	if defErr != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(defErr.Error()))
		return nil, exitConfig
	}
	baseDefinitions := agent.LoadAgents(defaults, cfg.StateDir)
	definitions, overrideErr := agent.ApplyAgentOverrides(baseDefinitions, cfg.AgentOverrides)
	if overrideErr != nil {
		prefix := "acode: agent overrides: "
		if cfg.ConfigPath != "" {
			prefix = fmt.Sprintf("acode: config file %s: ", cfg.ConfigPath)
		}
		_, _ = fmt.Fprintf(stderr, "%s%s\n", prefix, tui.SanitizeLine(overrideErr.Error()))
		return nil, exitConfig
	}
	for _, warning := range definitions.Warnings {
		warn(warning)
	}
	cfg.Agents = definitions.Infos()

	ctx := context.Background()
	var (
		catalogSource model.CatalogSource
		mode          engine.Mode
		sessionID     string
		instructions  string
	)
	switch {
	case cfg.ResumeID != "":
		source, code, err := tuiSessionPolicy(ctx, &cfg, cfg.ResumeID, warn)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
			return nil, code
		}
		catalogSource, mode, sessionID = source, engine.ModeResume, cfg.ResumeID
	case cfg.ForkID != "":
		source, code, err := tuiSessionPolicy(ctx, &cfg, cfg.ForkID, warn)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
			return nil, code
		}
		catalogSource, mode, sessionID = source, engine.ModeFork, cfg.ForkID
	default:
		source, err := newCatalogSource(cfg)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
			return nil, exitConfig
		}
		if err := resolveNewModel(ctx, &cfg, source, stderr); err != nil {
			_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
			return nil, catalogExitCode(err)
		}
		catalogSource = source
		instructions, err = newSessionWorkspaceText(cfg, warn)
		if err != nil {
			_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
			return nil, exitConfig
		}
		mode = engine.ModeNew
	}

	host, err := engine.Open(ctx, tuiEngineSpec(cfg, mode, sessionID, instructions, definitions))
	if err != nil {
		return nil, engineFailure(err, stderr)
	}
	resolved := host.Spec()
	applyResolvedSpec(&cfg, resolved)
	state, err := host.State()
	if err != nil {
		_ = host.Close()
		_, _ = fmt.Fprintf(stderr, "acode: read session %s: %v\n", tui.SanitizeLine(host.ID()), tui.SanitizeLine(err.Error()))
		return nil, exitSetup
	}
	info := sessionInfo(cfg, host, state)
	if catalogSource == nil {
		if src, err := newCatalogSource(cfg); err == nil {
			catalogSource = src
		}
	}
	var loaded *model.Catalog
	if catalogSource != nil {
		if cat, err := catalogSource.Load(ctx, cfg.Provider); err == nil {
			loaded = &cat
		}
	}
	th := &tuiHost{
		host:           host,
		cfg:            cfg,
		next:           selectionFromSpec(resolved),
		source:         catalogSource,
		agents:         definitions,
		baseAgents:     baseDefinitions,
		agentOverrides: cfg.AgentOverrides,
		info:           info,
		pendingWarning: strings.Join(startupWarn, "; "),
	}
	if loaded != nil {
		warnCatalog(stderr, *loaded)
		th.setCatalogLocked(*loaded)
	} else {
		th.models = []tui.ModelChoice{th.recordedChoiceLocked(false)}
	}
	return th, 0
}

// executeTUI runs the interactive or headless TUI and returns the exit code.
func executeTUI(cfg config, stdin io.Reader, stdout, stderr io.Writer) int {
	host, code := openTUIHost(cfg, stderr)
	if host == nil {
		return code
	}
	seed := (*tui.Submission)(nil)
	if cfg.TaskSet {
		seed = &tui.Submission{Text: cfg.Task, Source: cfg.TaskSource}
	}
	noColor := os.Getenv("NO_COLOR") != ""
	headless := cfg.Mode == ModeUIJSON
	imageProtocol, err := tui.ImageProtocolFromEnv(os.Getenv, headless)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
		_ = host.Close()
		return exitConfig
	}
	colorProfile := "truecolor"
	if !headless {
		colorProfile = detectedProfile(stdout)
	}
	terminal := tui.TerminalInfo{
		Name:    os.Getenv("TERM"),
		Version: strings.TrimSpace(os.Getenv("TERM_PROGRAM") + " " + os.Getenv("TERM_PROGRAM_VERSION")),
	}

	var exit int
	if headless {
		exit, err = tui.RunHeadless(tui.HeadlessOptions{
			Host:          host,
			Models:        host.Models(),
			Model:         host.Info().Model,
			Dialect:       cfg.Dialect,
			Seed:          seed,
			Width:         cfg.UISizeWidth,
			Height:        cfg.UISizeHeight,
			Stdin:         stdin,
			Stdout:        stdout,
			ColorProfile:  colorProfile,
			NoColor:       noColor,
			Theme:         tui.ThemeName(cfg.UITheme),
			ImageProtocol: imageProtocol,
			Terminal:      terminal,
		})
	} else {
		exit, err = tui.RunTerminal(tui.TerminalOptions{
			Host:          host,
			Models:        host.Models(),
			Model:         host.Info().Model,
			Dialect:       cfg.Dialect,
			Seed:          seed,
			Stdin:         stdin,
			Stdout:        stdout,
			ColorProfile:  colorProfile,
			NoColor:       noColor,
			Theme:         tui.ThemeName(cfg.UITheme),
			ImageProtocol: imageProtocol,
			Terminal:      terminal,
		})
	}
	closeErr := host.Close()
	info, lastStatus, lastReason := host.summary()
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
		if exit == 0 {
			exit = exitSetup
		}
	}
	if closeErr != nil {
		_, _ = fmt.Fprintf(stderr, "acode: close session: %v\n", tui.SanitizeLine(closeErr.Error()))
		exit = exitSetup
	}
	_, _ = fmt.Fprintf(stderr, "acode: session %s trace %s\n", tui.SanitizeLine(info.SessionID), tui.SanitizeLine(info.TracePath))
	if lastStatus != "" {
		line := "acode: last run " + string(lastStatus)
		if lastReason != "" {
			line += ": " + lastReason
		}
		_, _ = fmt.Fprintln(stderr, tui.SanitizeLine(line))
	}
	return exit
}

func detectedProfile(stdout io.Writer) string {
	switch colorprofile.Detect(stdout, os.Environ()) {
	case colorprofile.TrueColor:
		return "truecolor"
	case colorprofile.ANSI256:
		return "256"
	case colorprofile.ANSI:
		return "16"
	case colorprofile.ASCII:
		return "8"
	default:
		return "none"
	}
}
