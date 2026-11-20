package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/eddiectc/acode/internal/tui"
	"github.com/eddiectc/aharness/model"
)

func TestParseModeSelection(t *testing.T) {
	t.Parallel()
	stateDir := t.TempDir()
	base := []string{"-state-dir", stateDir, "-model", "deepseek-v4.1-flash", "-workspace", t.TempDir()}
	cfg := cfgTestParseOK(t, base, nil)
	if cfg.Mode != ModeTUI || !cfg.Stream {
		t.Fatalf("default mode = %v stream=%v", cfg.Mode, cfg.Stream)
	}
	if cfg.TaskSet {
		t.Fatal("TUI new session should allow no task")
	}
	cfg = cfgTestParseOK(t, append(append([]string{}, base...), "-batch", "-task", "work"), nil)
	if cfg.Mode != ModeBatch {
		t.Fatalf("batch mode = %v", cfg.Mode)
	}
	cfg = cfgTestParseOK(t, append(append([]string{}, base...), "-ui-json", "-ui-size", "80x24"), nil)
	if cfg.Mode != ModeUIJSON || cfg.UISizeWidth != 80 || cfg.UISizeHeight != 24 {
		t.Fatalf("ui-json config = %+v", cfg)
	}
}

func TestParseRemovedInteractiveFlag(t *testing.T) {
	t.Parallel()
	args := append(cfgTestBaseArgs(t), "-interactive")
	if err := cfgTestParseErr(t, args, nil); err == nil {
		t.Fatal("-interactive accepted")
	}
}

func TestParseModeExclusionsAndUISize(t *testing.T) {
	t.Parallel()
	base := cfgTestBaseArgs(t)
	if err := cfgTestParseErr(t, append(append([]string{}, base...), "-batch", "-ui-json"), nil); err == nil {
		t.Fatal("-batch and -ui-json accepted together")
	}
	if err := cfgTestParseErr(t, append(append([]string{}, base...), "-ui-size", "80x24"), nil); err == nil {
		t.Fatal("-ui-size accepted without -ui-json")
	}
	for _, size := range []string{"19x10", "100x4", "501x30", "100x201", "bad", "100", "100x"} {
		if err := cfgTestParseErr(t, append(append([]string{}, base...), "-ui-json", "-ui-size", size), nil); err == nil {
			t.Errorf("-ui-size %q accepted", size)
		}
	}
	cfg := cfgTestParseOK(t, append(append([]string{}, base...), "-ui-json"), nil)
	if cfg.UISizeWidth != defaultUISizeWidth || cfg.UISizeHeight != defaultUISizeHeight {
		t.Fatalf("default ui size = %dx%d", cfg.UISizeWidth, cfg.UISizeHeight)
	}
}

func TestModeTaskAndStreamRules(t *testing.T) {
	t.Parallel()
	// Batch requires a task for a new session; TUI does not.
	batch := config{Mode: ModeBatch}
	if err := validateModeRules(&batch); err == nil || !strings.Contains(err.Error(), "task") {
		t.Fatalf("batch rule error = %v", err)
	}
	tui := config{Mode: ModeTUI}
	if err := validateModeRules(&tui); err != nil || !tui.Stream {
		t.Fatalf("TUI rule = %v stream=%v", err, tui.Stream)
	}
	fork := config{Mode: ModeBatch, ForkID: "0190a0c4-0000-7000-8000-0000000000aa"}
	if err := validateModeRules(&fork); err == nil {
		t.Fatal("batch fork without a task accepted")
	}
	fork.Mode = ModeTUI
	if err := validateModeRules(&fork); err != nil {
		t.Fatalf("TUI fork without a task: %v", err)
	}
	noStream := config{Mode: ModeTUI, StreamSet: true}
	if err := validateModeRules(&noStream); err == nil {
		t.Fatal("-stream=false accepted in TUI mode")
	}
	pdf := config{Mode: ModeBatch, TaskSet: true}
	if err := validateModeRules(&pdf); err != nil {
		t.Fatalf("batch with task: %v", err)
	}
}

func TestVersionSkipsModeSelection(t *testing.T) {
	t.Parallel()
	cfg, err := parseConfig([]string{"-version", "-ui-json", "-batch"}, cfgTestGetenv(nil), io.Discard)
	if err != nil || !cfg.ShowVersion {
		t.Fatalf("version config = %+v err=%v", cfg, err)
	}
}

func TestSelectModeMatrix(t *testing.T) {
	orig := terminalCheck
	t.Cleanup(func() { terminalCheck = orig })
	t.Setenv("TERM", "xterm-256color")
	base := cfgTestBaseArgs(t)

	terminalCheck = func(any) bool { return true }
	cfg := cfgTestParseOK(t, base, nil)
	if err := selectMode(&cfg, strings.NewReader(""), io.Discard); err != nil || cfg.Mode != ModeTUI {
		t.Fatalf("TTY mode = %v err=%v", cfg.Mode, err)
	}

	terminalCheck = func(any) bool { return false }
	cfg = cfgTestParseOK(t, base, nil)
	if err := selectMode(&cfg, strings.NewReader(""), io.Discard); err != nil || cfg.Mode != ModeBatch {
		t.Fatalf("non-TTY mode = %v err=%v", cfg.Mode, err)
	}

	terminalCheck = func(any) bool { return true }
	cfg = cfgTestParseOK(t, base, nil)
	t.Setenv("TERM", "dumb")
	if err := selectMode(&cfg, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("TERM=dumb accepted for TUI")
	}
	t.Setenv("TERM", "")
	if err := selectMode(&cfg, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("empty TERM accepted for TUI")
	}

	// -ui-json ignores the TTY and TERM checks.
	cfg = cfgTestParseOK(t, append(append([]string{}, base...), "-ui-json"), nil)
	if err := selectMode(&cfg, strings.NewReader(""), io.Discard); err != nil || cfg.Mode != ModeUIJSON {
		t.Fatalf("ui-json mode = %v err=%v", cfg.Mode, err)
	}

	// Non-TTY downgrade applies the batch task matrix.
	terminalCheck = func(any) bool { return false }
	missingTask := []string{"-model", "deepseek-v4.1-flash", "-workspace", t.TempDir(), "-state-dir", t.TempDir()}
	cfg = cfgTestParseOK(t, missingTask, nil)
	if err := selectMode(&cfg, strings.NewReader(""), io.Discard); err == nil {
		t.Fatal("non-TTY new session without a task accepted")
	}
}

func TestParseTUIDefersCatalogResolution(t *testing.T) {
	t.Parallel()
	// The TUI host resolves the catalog after parseConfig; an unknown model is
	// not rejected until the host loads the catalog.
	cfg := cfgTestParseOK(t, []string{
		"-ui-json",
		"-model", "model-not-in-catalog",
		"-state-dir", t.TempDir(),
		"-workspace", t.TempDir(),
	}, nil)
	if cfg.Mode != ModeUIJSON || !cfg.Stream {
		t.Fatalf("mode/stream = %v/%t", cfg.Mode, cfg.Stream)
	}
	if cfg.Dialect != "" || cfg.BaseURL != "" || cfg.MaxContextTokens != 0 || cfg.MaxOutputTokens != 0 {
		t.Fatalf("TUI parse resolved catalog values early: %+v", cfg)
	}
	if !cfg.modelSet {
		t.Fatal("modelSet = false, want true")
	}
}

func TestParseCatalogTerminalActionsIgnoreTTYRules(t *testing.T) {
	for _, action := range []string{"-list-models", "-refresh-models"} {
		t.Run(action, func(t *testing.T) {
			orig := terminalCheck
			t.Cleanup(func() { terminalCheck = orig })
			terminalCheck = func(any) bool { return false }
			t.Setenv("TERM", "")
			cfg := cfgTestParseOK(t, []string{
				action,
				"-ui-json",
				"-task", "ignored",
				"-state-dir", t.TempDir(),
			}, nil)
			if !cfg.ListModels && !cfg.RefreshModels {
				t.Fatalf("terminal action not recorded: %+v", cfg)
			}
			if cfg.TaskSet || cfg.Task != "" {
				t.Fatalf("terminal action retained the task: %+v", cfg)
			}
			if cfg.Mode != 0 || cfg.UIJSON {
				t.Fatalf("terminal action retained mode flags: %+v", cfg)
			}
		})
	}
}

func TestTUISelectModelReportsContextClamp(t *testing.T) {
	t.Parallel()
	host := &tuiHost{
		cfg: config{
			Provider:         providerName,
			Model:            "large",
			Dialect:          model.DialectChat,
			BaseURL:          testCatalogBaseURL,
			MaxContextTokens: 1_000_000,
			MaxTurns:         defaultMaxTurns,
			MaxTotalTokens:   defaultMaxTotalTokens,
			SessionTimeout:   defaultSessionTimeout,
			ModelTimeout:     defaultModelTimeout,
			maxContextSet:    true,
			maxContextValue:  500_000,
		},
		catalog: model.Catalog{Models: map[string]model.CatalogModel{
			"small": {
				ID:            "small",
				Dialect:       model.DialectChat,
				BaseURL:       testCatalogBaseURL,
				ContextWindow: 262_144,
				ToolCall:      true,
			},
		}},
	}
	if _, err := host.SelectModel("small"); err != nil {
		t.Fatalf("SelectModel: %v", err)
	}
	if host.next.limits.MaxContextTokens != 262_144 {
		t.Fatalf("selected MaxContextTokens = %d, want the model clamp", host.next.limits.MaxContextTokens)
	}
	if host.cfg.MaxContextTokens != 1_000_000 {
		t.Fatalf("session MaxContextTokens = %d, want the recorded value untouched", host.cfg.MaxContextTokens)
	}
	warning := host.TakeModelWarning()
	if !strings.Contains(warning, "clamped") {
		t.Fatalf("warning = %q, want the context clamp", warning)
	}
	if got := host.TakeModelWarning(); got != "" {
		t.Fatalf("warning was not consumed: %q", got)
	}
}

func TestTUIModelChoiceCopiesReasoningEfforts(t *testing.T) {
	t.Parallel()
	source := model.CatalogModel{
		ID:               "deepseek-v4.1-flash",
		Name:             "DeepSeek V4.1 Flash",
		Dialect:          model.DialectChat,
		Reasoning:        true,
		ReasoningEfforts: []string{"low", "high", "max"},
	}
	choice := modelChoice(source)
	if strings.Join(choice.ReasoningEfforts, ",") != "low,high,max" {
		t.Fatalf("ReasoningEfforts = %v", choice.ReasoningEfforts)
	}
	choice.ReasoningEfforts[0] = "changed"
	if source.ReasoningEfforts[0] != "low" {
		t.Fatal("modelChoice aliased the catalog effort slice")
	}

	recorded := (&tuiHost{cfg: config{Model: "recorded"}, info: tui.Info{Model: "recorded"}}).recordedChoiceLocked(true)
	if len(recorded.ReasoningEfforts) != 0 {
		t.Fatalf("recorded fallback exposed efforts: %v", recorded.ReasoningEfforts)
	}
}

func TestTUISelectModelSwitchesBaseURL(t *testing.T) {
	t.Parallel()
	cfg := config{
		Provider:         providerName,
		Model:            "one",
		Dialect:          model.DialectChat,
		BaseURL:          "https://opencode.ai/inference/go/openai/v1",
		MaxContextTokens: 200_000,
		MaxTurns:         defaultMaxTurns,
		MaxTotalTokens:   defaultMaxTotalTokens,
		SessionTimeout:   defaultSessionTimeout,
		ModelTimeout:     defaultModelTimeout,
	}
	if err := validateEffective(&cfg); err != nil {
		t.Fatalf("validate config: %v", err)
	}
	host := &tuiHost{
		cfg: cfg,
		catalog: model.Catalog{Models: map[string]model.CatalogModel{
			"one": {
				ID: "one", Dialect: model.DialectChat, BaseURL: "https://opencode.ai/inference/go/openai/v1",
				ContextWindow: 200_000, ToolCall: true,
			},
			"two": {
				ID: "two", Dialect: model.DialectChat, BaseURL: "https://opencode.ai/inference/go/openai/v2",
				ContextWindow: 300_000, ToolCall: true,
			},
		}},
	}
	host.next = runSelection{
		model:   cfg.Model,
		dialect: cfg.Dialect,
		baseURL: cfg.BaseURL,
		limits:  cfg.Limits,
	}
	if _, err := host.SelectModel("two"); err != nil {
		t.Fatalf("SelectModel: %v", err)
	}
	if host.next.baseURL != "https://opencode.ai/inference/go/openai/v2" {
		t.Fatalf("selected base URL = %q, want the new model's endpoint", host.next.baseURL)
	}
	if host.cfg.BaseURL != "https://opencode.ai/inference/go/openai/v1" {
		t.Fatalf("session base URL = %q, want the recorded value untouched", host.cfg.BaseURL)
	}
}

func TestNewCompleterForRejectsUnknownDialect(t *testing.T) {
	t.Parallel()
	completer := newCompleterFor(model.Dialect("future"), "https://example.invalid/v1",
		"token", time.Second, "acode/test", "session")
	_, err := completer.Complete(context.Background(), model.Request{Model: "m"})
	if err == nil || !strings.Contains(err.Error(), "unsupported provider dialect") {
		t.Fatalf("unknown dialect error = %v, want a rejection", err)
	}
}

// TestNewCompleterForUsesThePassedBaseURL pins P2.8: the factory builds the
// adapter for the endpoint it is given, not a shared config field, so a
// per-run model change reaches the new server.
func TestNewCompleterForUsesThePassedBaseURL(t *testing.T) {
	t.Parallel()
	server := func(answer string) *fakeProvider {
		return newFakeProvider(t, func(w http.ResponseWriter, r *http.Request, call int, body []byte) {
			if r.URL.Path != "/chat/completions" {
				t.Errorf("request path = %q, want /chat/completions", r.URL.Path)
			}
			writeJSON(w, 200, chatFinalResponse(answer))
		})
	}
	one := server("one")
	two := server("two")
	req := model.Request{
		Model:    "m",
		Messages: []model.Message{{Role: model.RoleUser, Content: "hi"}},
	}
	for _, tc := range []struct {
		provider *fakeProvider
		want     string
	}{
		{provider: one, want: "one"},
		{provider: two, want: "two"},
	} {
		completer := newCompleterFor(model.DialectChat, tc.provider.url, "token",
			5*time.Second, acodeUserAgent("test"), "session")
		response, err := completer.Complete(context.Background(), req)
		if err != nil {
			t.Fatalf("Complete(%s): %v", tc.provider.url, err)
		}
		if response.Text != tc.want {
			t.Fatalf("response from %s = %q, want %q", tc.provider.url, response.Text, tc.want)
		}
	}
	for name, provider := range map[string]*fakeProvider{"one": one, "two": two} {
		provider.mu.Lock()
		calls := provider.calls
		provider.mu.Unlock()
		if calls != 1 {
			t.Fatalf("%s server calls = %d, want 1", name, calls)
		}
	}
}

func TestTUIHostInfoTracksEffectiveEffort(t *testing.T) {
	t.Parallel()
	host := &tuiHost{
		next: runSelection{effort: "high"},
		info: tui.Info{Model: "deepseek-v4.1-flash"},
	}
	if got := host.Info().ThinkingEffort; got != "high" {
		t.Fatalf("Info().ThinkingEffort = %q, want high", got)
	}
	host.next.effort = ""
	if got := host.Info().ThinkingEffort; got != "" {
		t.Fatalf("Info().ThinkingEffort = %q, want the provider default", got)
	}
}

func TestTUIResolveRunThinkingEffort(t *testing.T) {
	t.Parallel()
	catalog := map[string]model.CatalogModel{
		"supported": {ID: "supported", Reasoning: true, ReasoningEfforts: []string{"low", "high"}},
		"no-values": {ID: "no-values", Reasoning: true},
		"no-reason": {ID: "no-reason"},
	}
	cases := []struct {
		name      string
		inherited string
		opts      tui.RunOptions
		model     string
		want      string
		wantWarn  string
	}{
		{
			name:      "inherit supported",
			inherited: "high", model: "supported", want: "high",
		},
		{
			name:      "explicit supported",
			inherited: "low", opts: tui.RunOptions{ThinkingEffort: "high", ThinkingEffortSet: true},
			model: "supported", want: "high",
		},
		{
			name:      "explicit unsupported clears",
			inherited: "low", opts: tui.RunOptions{ThinkingEffort: "max", ThinkingEffortSet: true},
			model: "supported", want: "", wantWarn: "cleared",
		},
		{
			name:      "inherit unsupported clears",
			inherited: "max", model: "supported", want: "", wantWarn: "cleared",
		},
		{
			name:      "explicit provider default clears",
			inherited: "high", opts: tui.RunOptions{ThinkingEffortSet: true},
			model: "supported", want: "",
		},
		{
			name:      "no advertised values clears",
			inherited: "high", model: "no-values", want: "", wantWarn: "advertises no effort levels",
		},
		{
			name:      "no reasoning clears",
			inherited: "high", model: "no-reason", want: "", wantWarn: "does not support reasoning",
		},
		{
			name:      "unknown model keeps recorded value",
			inherited: "high", model: "offline-model", want: "high",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sel := runSelection{effort: tc.inherited}
			var warnings []string
			resolveRunThinkingEffort(&sel, catalog, tc.model, tc.opts, func(text string) {
				warnings = append(warnings, text)
			})
			if sel.effort != tc.want {
				t.Fatalf("selection effort = %q, want %q", sel.effort, tc.want)
			}
			joined := strings.Join(warnings, "\n")
			if tc.wantWarn != "" {
				if !strings.Contains(joined, tc.wantWarn) || !strings.Contains(joined, "provider default") {
					t.Fatalf("warnings = %q, want %q and the provider default", joined, tc.wantWarn)
				}
			} else if joined != "" {
				t.Fatalf("unexpected warnings: %q", joined)
			}
		})
	}
}
