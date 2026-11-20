package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"

	"golang.org/x/term"

	"github.com/eddiectc/acode/internal/tui"
	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/session"
	"github.com/eddiectc/aharness/tools"
)

const (
	exitConfig = 2
	exitSetup  = 1
)

// run executes the CLI and returns the process exit code. main alone calls
// os.Exit.
func run() int {
	return runWithIO(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)
}

// runArgs is the testable CLI entry point.
func runArgs(args []string, stdout, stderr io.Writer) int {
	return runWithIO(args, os.Stdin, stdout, stderr)
}

func runWithIO(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	return runWithIOEnv(args, os.Getenv, stdin, stdout, stderr)
}

// runWithIOEnv is runWithIO with the environment source injected. Tests use it
// to exercise CLI behavior in-process against an explicit environment instead
// of mutating os.Environ.
func runWithIOEnv(args []string, getenv func(string) string, stdin io.Reader, stdout, stderr io.Writer) int {
	if stdout == nil {
		stdout = io.Discard
	}
	if stderr == nil {
		stderr = io.Discard
	}
	cfg, err := parseConfig(args, getenv, stderr)
	if err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
		return exitConfig
	}
	if cfg.ShowVersion {
		printVersion(stdout)
		return 0
	}
	cfg.HarnessVersion = buildVersion()
	if cfg.RefreshModels || cfg.ListModels {
		return runCatalogAction(cfg, stdout, stderr)
	}
	if err := selectMode(&cfg, stdin, stdout); err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", tui.SanitizeLine(err.Error()))
		return exitConfig
	}
	return execute(cfg, stdin, stdout, stderr)
}

// selectMode applies the TTY rules: explicit -ui-json and -batch win, and an
// unspecified mode is TUI only when both stdin and stdout are terminals.
func selectMode(cfg *config, stdin io.Reader, stdout io.Writer) error {
	switch {
	case cfg.UIJSON:
		cfg.Mode = ModeUIJSON
	case cfg.Batch:
		cfg.Mode = ModeBatch
	case !isTerminal(stdin) || !isTerminal(stdout):
		cfg.Mode = ModeBatch
	default:
		cfg.Mode = ModeTUI
		if term := os.Getenv("TERM"); term == "" || term == "dumb" {
			return errors.New("interactive mode requires a terminal TERM value other than \"dumb\"; use -batch for one-shot output")
		}
	}
	if cfg.Mode == ModeTUI {
		if _, err := tui.ImageProtocolFromEnv(os.Getenv, false); err != nil {
			return err
		}
	}
	return validateModeRules(cfg)
}

// terminalCheck is the TTY predicate; tests replace it to exercise the mode
// matrix without a PTY.
var terminalCheck = isTerminalValue

// isTerminal reports whether v is backed by a terminal file descriptor.
func isTerminal(v any) bool { return terminalCheck(v) }

func isTerminalValue(v any) bool {
	type fileDescriptor interface{ Fd() uintptr }
	f, ok := v.(fileDescriptor)
	return ok && term.IsTerminal(int(f.Fd()))
}

// execute wires the workspace, session state, provider adapter, tool registry,
// and runner, then performs terminal cleanup. Configuration has already been
// validated for a new session; resume and fork inherit before running.
func execute(cfg config, stdin io.Reader, stdout, stderr io.Writer) int {
	if cfg.Mode != ModeBatch {
		return executeTUI(cfg, stdin, stdout, stderr)
	}
	logger := slog.New(slog.NewTextHandler(stderr, &slog.HandlerOptions{Level: slog.LevelInfo}))
	switch {
	case cfg.ResumeID != "":
		return executeResume(cfg, stdin, stdout, stderr, logger)
	case cfg.ForkID != "":
		return executeFork(cfg, stdin, stdout, stderr, logger)
	default:
		return executeNew(cfg, stdin, stdout, stderr, logger)
	}
}

func executeNew(cfg config, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) int {
	source, err := newCatalogSource(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return exitConfig
	}
	if err := resolveNewModel(context.Background(), &cfg, source, stderr); err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return catalogExitCode(err)
	}
	// A new session is the only contract that discovers workspace
	// instructions. Read once through an SDK handle, then let the engine fold,
	// record, and propagate the text. Resume and fork replay what they recorded
	// instead.
	instructions, err := newSessionWorkspaceText(cfg, func(warning string) {
		warnf(stderr, "%s", warning)
	})
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return exitConfig
	}
	spec, err := batchEngineSpec(cfg, engine.ModeNew, "", instructions, nil, logger)
	if err != nil {
		return engineFailure(err, stderr)
	}
	host, err := engine.Open(context.Background(), spec)
	if err != nil {
		return engineFailure(err, stderr)
	}
	cfg.Workspace = host.Workspace()
	return executeEngineRun(cfg, host, stdin, stdout, stderr, logger, "session started")
}

func executeResume(cfg config, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) int {
	snapshot, err := engine.Inspect(context.Background(), cfg.StateDir, cfg.ResumeID)
	if err != nil {
		return engineFailure(err, stderr)
	}
	state := snapshot.State
	if len(state.CustomTools) > 0 {
		_, _ = fmt.Fprintf(stderr, "acode: session %s records caller-defined tools; the CLI cannot provide their implementations\n", sanitizeText(cfg.ResumeID))
		return exitConfig
	}
	source, err := sessionCatalogSource(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return exitConfig
	}
	if err := applySessionDefaults(context.Background(), &cfg, state, source, func(w string) {
		warnf(stderr, "%s", w)
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return catalogExitCode(err)
	}
	if err := validateSessionRules(&cfg, state); err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return exitConfig
	}
	spec, err := batchEngineSpec(cfg, engine.ModeResume, cfg.ResumeID, "", nil, logger)
	if err != nil {
		return engineFailure(err, stderr)
	}
	host, err := engine.Open(context.Background(), spec)
	if err != nil {
		return engineFailure(err, stderr)
	}
	cfg.Workspace = host.Workspace()
	return executeEngineRun(cfg, host, stdin, stdout, stderr, logger, "session resumed")
}

func executeFork(cfg config, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger) int {
	snapshot, err := engine.Inspect(context.Background(), cfg.StateDir, cfg.ForkID)
	if err != nil {
		return engineFailure(err, stderr)
	}
	state := snapshot.State
	if len(state.CustomTools) > 0 {
		_, _ = fmt.Fprintf(stderr, "acode: session %s records caller-defined tools; the CLI cannot provide their implementations\n", sanitizeText(cfg.ForkID))
		return exitConfig
	}
	catSource, err := sessionCatalogSource(cfg)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return exitConfig
	}
	if err := applySessionDefaults(context.Background(), &cfg, state, catSource, func(w string) {
		warnf(stderr, "%s", w)
	}); err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return catalogExitCode(err)
	}
	if err := validateSessionRules(&cfg, state); err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		return exitConfig
	}
	spec, err := batchEngineSpec(cfg, engine.ModeFork, cfg.ForkID, "", state.Tools, logger)
	if err != nil {
		return engineFailure(err, stderr)
	}
	host, err := engine.Open(context.Background(), spec)
	if err != nil {
		return engineFailure(err, stderr)
	}
	cfg.Workspace = host.Workspace()
	return executeEngineRun(cfg, host, stdin, stdout, stderr, logger, "session forked")
}

// batchEngineSpec maps the resolved CLI configuration onto the shared engine
// contract. instructions is the new-session workspace text; the engine
// composes and records it, and only ModeNew may carry it. A new or forked batch
// session records the toolset's full grant and narrows only the root, so the
// recorded contract stays complete while the session can never delegate.
// recorded is the parent's recorded grant when forking, nil otherwise.
func batchEngineSpec(cfg config, mode engine.Mode, sessionID, instructions string, recorded []string, logger *slog.Logger) (engine.Spec, error) {
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
		RunTimeout:     cfg.SessionTimeout,
		ModelTimeout:   cfg.ModelTimeout,
		RequireUsage:   cfg.RequireUsage,
		LogRequests:    cfg.LogRequests,
		Version:        cfg.HarnessVersion,
		Logger:         logger,
		NewCompleter: func(dialect model.Dialect, baseURL, sessionID string) model.Completer {
			return newCompleterFor(dialect, baseURL, cfg.Token, cfg.HTTPTimeout, acodeUserAgent(cfg.HarnessVersion), sessionID)
		},
	}
	switch mode {
	case engine.ModeNew, engine.ModeFork:
		root, err := batchRootGrant(recorded, toolsetVersionOf(cfg))
		if err != nil {
			return engine.Spec{}, fmt.Errorf("tool grant: %w", err)
		}
		spec.RootGrant = root
		if mode == engine.ModeNew {
			spec.CustomToolsSet = true
			spec.AppendPrompt = instructions
			spec.AppendPromptSet = instructions != ""
		}
	}
	return spec, nil
}

// executeEngineRun performs one batch run over an engine host.
func executeEngineRun(cfg config, host *engine.Host, stdin io.Reader, stdout, stderr io.Writer, logger *slog.Logger, action string) int {
	logger.Info(action,
		"session", host.ID(),
		"trace", host.Path(),
		"model", cfg.Model,
		"dialect", cfg.Dialect,
		"workspace", host.Workspace(),
		"auth", authState(cfg.Token),
		"max_turns", cfg.MaxTurns,
		"max_context_tokens", cfg.MaxContextTokens,
		"max_total_tokens", cfg.MaxTotalTokens,
		"session_timeout_ms", cfg.SessionTimeout.Milliseconds(),
		"model_timeout_ms", cfg.ModelTimeout.Milliseconds(),
		"http_timeout_ms", cfg.HTTPTimeout.Milliseconds(),
		"search_configured", cfg.SearXNGURL != "",
		"search_timeout_ms", tools.SearchTimeout.Milliseconds(),
		"web_timeout_ms", cfg.WebTimeout.Milliseconds(),
		"web_fetch_provider", cfg.WebFetchProvider,
		"web_fetch_host", webFetchHost(cfg.WebFetchURL),
		"config", configState(cfg.ConfigPath),
	)
	if cfg.Token == "" && !baseURLIsLoopback(cfg.BaseURL) {
		logger.Warn("provider token is unset; hosted OpenCode Go requires ACODE_PROVIDER_TOKEN")
	}
	runCtx, stopSignals := setupSignals()
	defer stopSignals()
	var sink *textSink
	if cfg.Stream {
		sink = &textSink{w: stdout}
	}
	var display func(agent.DisplayEvent) error
	if sink != nil {
		display = textDisplay(sink)
	}
	runner, err := host.BuildRunner(engine.RunSpec{Stream: cfg.Stream}, display, nil, nil)
	if err != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
		_ = host.Close()
		return engineExitCode(err)
	}
	result, runErr := runner.Run(runCtx, agent.RunOptions{
		Task:     cfg.Task,
		TaskSet:  cfg.TaskSet,
		TaskFrom: cfg.TaskSource,
	})
	var sinkErr error
	if sink != nil {
		sinkErr = sink.finish()
	}
	closeErr := host.Close()

	status := result.Status
	if runErr != nil {
		_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(runErr.Error()))
		if cfg.Token == "" && authFailureHint(runErr) {
			_, _ = fmt.Fprintln(stderr, "acode: hint: set ACODE_PROVIDER_TOKEN to an OpenCode Console API key")
		}
		if status == agent.StopCompleted {
			status = agent.StopInternalError
		}
	}
	if sinkErr != nil {
		_, _ = fmt.Fprintf(stderr, "acode: write streamed output: %v\n", sanitizeText(sinkErr.Error()))
		status = agent.StopInternalError
	}
	if closeErr != nil {
		_, _ = fmt.Fprintf(stderr, "acode: close session log: %v\n", sanitizeText(closeErr.Error()))
		status = agent.StopInternalError
	}
	if !cfg.Stream && result.FinalText != "" {
		_, _ = fmt.Fprint(stdout, result.FinalText)
		if !strings.HasSuffix(result.FinalText, "\n") {
			_, _ = fmt.Fprintln(stdout)
		}
	}
	return status.ExitCode()
}

// engineFailure maps an engine construction error to the CLI exit code.
func engineFailure(err error, stderr io.Writer) int {
	_, _ = fmt.Fprintf(stderr, "acode: %v\n", sanitizeText(err.Error()))
	return engineExitCode(err)
}

func engineExitCode(err error) int {
	var configErr *engine.ConfigError
	switch {
	case errors.As(err, &configErr):
		return exitConfig
	case errors.Is(err, engine.ErrSessionNotFound),
		errors.Is(err, engine.ErrSessionLocked),
		errors.Is(err, engine.ErrInvalidSessionID),
		errors.Is(err, session.ErrUnsupportedVersion),
		errors.Is(err, session.ErrMode):
		return exitConfig
	default:
		return exitSetup
	}
}

// toolOptions returns the registry options shared by batch and TUI runs.
func toolOptions(cfg config, tmpDir, workDir string) tools.Options {
	return tools.Options{
		TmpDir:           tmpDir,
		WorkDir:          workDir,
		SearXNGURL:       cfg.SearXNGURL,
		UserAgent:        acodeUserAgent(cfg.HarnessVersion),
		WebTimeout:       cfg.WebTimeout,
		WebFetchProvider: cfg.WebFetchProvider,
		WebFetchURL:      cfg.WebFetchURL,
		WebFetchKey:      cfg.WebFetchKey,
	}
}

// toolsetVersionOf resolves the recorded toolset version, defaulting to the
// current one.
func toolsetVersionOf(cfg config) string {
	if cfg.ToolsetVersion != "" {
		return cfg.ToolsetVersion
	}
	return tools.Version
}

// acodeUserAgent identifies this coding agent to provider and catalog
// endpoints, independently of the aharness SDK version recorded in the log.
func acodeUserAgent(version string) string {
	if version == "" || version == "(devel)" {
		version = "devel"
	}
	return "acode/" + version
}

// authState reports whether a provider token is configured without exposing
// its value in logs.
func authState(token string) string {
	if token == "" {
		return "unset"
	}
	return "set"
}

// configState names the resolved config file so an operator can see which file
// supplied a value. An empty path means the file was disabled with -config="".
func configState(path string) string {
	if path == "" {
		return "none"
	}
	return path
}

// authFailureHint reports whether err is a provider authentication failure,
// which is actionable as a missing or invalid ACODE_PROVIDER_TOKEN.
func authFailureHint(err error) bool {
	return errors.Is(err, model.ErrAuth)
}
