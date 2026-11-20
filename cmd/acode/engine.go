package main

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/tools"
)

// runSelection is the per-run model choice. It never rewrites the session
// contract: each run passes it as engine.RunSpec, so a picker selection applies
// to the next run only and a failed selection leaves the session unchanged.
type runSelection struct {
	model   string
	dialect model.Dialect
	baseURL string
	limits  agent.Limits
	pricing model.Pricing
	effort  string
}

// selectionFromSpec starts the next-run selection from the engine's resolved
// session contract.
func selectionFromSpec(spec engine.Spec) runSelection {
	return runSelection{
		model:   spec.Model,
		dialect: model.Dialect(spec.Dialect),
		baseURL: spec.BaseURL,
		limits:  spec.Limits,
		pricing: spec.Pricing,
		effort:  effortValue(spec.ThinkingEffort),
	}
}

// effortValue dereferences the engine's tri-state thinking effort. Nil means
// inherit, so it is the caller's recorded or default value.
func effortValue(effort *string) string {
	if effort == nil {
		return ""
	}
	return *effort
}

// effortOption returns the tri-state override the engine expects. The empty
// string is an explicit provider default, so a cleared inherited effort is
// sent as an override rather than omitted.
func (s runSelection) effortOption() *string {
	effort := s.effort
	return &effort
}

// applyResolvedSpec records the engine's resolved session contract in the app
// configuration. Per-run model choices live in runSelection instead, so a
// picker selection never rewrites these values.
func applyResolvedSpec(cfg *config, spec engine.Spec) {
	cfg.Provider = spec.Provider
	cfg.Model = spec.Model
	cfg.Dialect = model.Dialect(spec.Dialect)
	cfg.BaseURL = spec.BaseURL
	cfg.Workspace = spec.Workspace
	cfg.Limits = spec.Limits
	cfg.MaxContextTokens = spec.Limits.MaxContextTokens
	cfg.MaxOutputTokens = spec.Limits.MaxOutputTokens
	cfg.InputLimit = spec.Limits.MaxInputTokens
	cfg.Pricing = spec.Pricing
	cfg.PromptVersion = spec.PromptVersion
	cfg.ToolsetVersion = spec.ToolsetVersion
	cfg.Agent = spec.Agent
	cfg.ThinkingEffort = effortValue(spec.ThinkingEffort)
}

// batchRootGrant returns the root-only grant for a batch-created session: every
// tool of the recorded version except delegate, intersected with the recorded
// session grant when there is one. The session grant itself stays unset, so the
// recorded contract remains complete; narrowing the root is what makes a
// session first created by batch permanently non-delegating, even when a TUI
// later resumes it.
func batchRootGrant(recorded []string, toolsetVersion string) ([]string, error) {
	names, err := tools.NamesFor(toolsetVersion)
	if err != nil {
		return nil, err
	}
	root := make([]string, 0, len(names))
	for _, name := range names {
		if name == tools.NameDelegate {
			continue
		}
		if recorded != nil && !slices.Contains(recorded, name) {
			continue
		}
		root = append(root, name)
	}
	return root, nil
}

// newCompleterFor builds the adapter for one run's dialect and provider
// endpoint. The endpoint is passed rather than read from a shared config so a
// per-run model switch that changes the base URL reaches the new server for
// both the root run and its children. An unknown dialect is rejected rather
// than silently mapped onto Chat.
func newCompleterFor(dialect model.Dialect, baseURL, token string, timeout time.Duration, userAgent, sessionID string) model.Completer {
	switch dialect {
	case model.DialectResponses:
		return model.NewOpenAIResponses(baseURL, token, sessionID, userAgent, timeout)
	case model.DialectChat:
		return model.NewOpenAIChat(baseURL, token, sessionID, userAgent, timeout)
	default:
		return rejectingCompleter{err: fmt.Errorf("unsupported provider dialect %q", dialect)}
	}
}

// rejectingCompleter fails one model call with the construction error. The
// SDK exposes exactly two dialects today; this arm keeps a future one from
// silently talking Chat to a Responses-shaped endpoint.
type rejectingCompleter struct{ err error }

func (c rejectingCompleter) Complete(context.Context, model.Request) (model.Response, error) {
	return model.Response{}, c.err
}
