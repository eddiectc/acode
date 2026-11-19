package main

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/tools"
)

const (
	// workspaceInstructionsFile is the one workspace file acode seeds into a
	// new session's prompt. It is root-only by design: no nested hierarchy, no
	// alias, and no import expansion.
	workspaceInstructionsFile = "AGENTS.md"
	// maxWorkspaceInstructionsBytes bounds the seeded file. The SDK read
	// returns ErrTooLarge and no bytes above it, so acode skips the file rather
	// than presenting a partial one as the workspace rules. The cap also keeps
	// the composed prompt under the SDK's 32 KiB prompt cap.
	maxWorkspaceInstructionsBytes = 16 * 1024
)

// readWorkspaceInstructions reads the workspace root AGENTS.md through the SDK
// workspace handle, which applies the tool path policy: a relative root-only
// path, no symlink components, a regular file, and the size limit. An absent
// file returns empty text and no error. Invalid UTF-8 is replaced before the
// text can reach the prompt or the log; valid bytes are otherwise unchanged.
func readWorkspaceInstructions(ws *tools.Workspace) (string, error) {
	if ws == nil {
		return "", errors.New("workspace is not open")
	}
	data, err := ws.ReadDisplayFile(workspaceInstructionsFile, maxWorkspaceInstructionsBytes)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return "", nil
		}
		return "", fmt.Errorf("read %s: %w", workspaceInstructionsFile, err)
	}
	return sanitizeText(string(data)), nil
}

// seedWorkspaceInstructions composes the workspace instructions into cfg for a
// new session. It renders the base prompt when the config carries no explicit
// one, joins the file with the same "\n\n" the engine uses, and records the
// file as AppendedPrompt. The caller uses it only at a new-contract site, never
// on resume or fork, so a running session can never pick up a changed file.
// A skipped file is returned as one warning and leaves cfg unchanged.
func seedWorkspaceInstructions(cfg *config, ws *tools.Workspace) string {
	text, err := readWorkspaceInstructions(ws)
	if err != nil {
		return "workspace instructions skipped: " + sanitizeText(err.Error())
	}
	if text == "" {
		return ""
	}
	base := cfg.SystemPrompt
	if base == "" {
		rendered, err := agent.RenderSystemPrompt(cfg.PromptVersion, cfg.Workspace, acodeIdentity, cfg.Agents)
		if err != nil {
			return "workspace instructions skipped: render system prompt: " + sanitizeText(err.Error())
		}
		base = rendered
	}
	cfg.SystemPrompt = agent.CapSystemPrompt(base + "\n\n" + text)
	cfg.AppendedPrompt = text
	return ""
}
