package main

import (
	"errors"
	"fmt"
	"io/fs"

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

// newSessionWorkspaceText reads the workspace root AGENTS.md for a new session.
// The workspace is opened only for the read: the engine opens the handle it
// keeps. A missing file yields empty text; an unreadable one is one warning and
// yields empty text so the session still starts; a workspace that cannot be
// opened is a hard error.
func newSessionWorkspaceText(cfg config, warn func(string)) (string, error) {
	ws, err := tools.OpenWorkspace(cfg.Workspace)
	if err != nil {
		return "", fmt.Errorf("open workspace %s: %w", cfg.Workspace, err)
	}
	text := workspaceInstructionsForNewSession(ws, warn)
	closeErr := ws.Close()
	if closeErr != nil && warn != nil {
		warn("workspace instructions: close workspace: " + sanitizeText(closeErr.Error()))
	}
	return text, nil
}

// workspaceInstructionsForNewSession reads the root AGENTS.md through an open
// workspace handle. A read failure becomes one warning and empty text, so a
// session always starts; the SDK already bounds the file and applies the tool
// path policy.
func workspaceInstructionsForNewSession(ws *tools.Workspace, warn func(string)) string {
	text, err := readWorkspaceInstructions(ws)
	if err != nil {
		if warn != nil {
			warn("workspace instructions skipped: " + sanitizeText(err.Error()))
		}
		return ""
	}
	return text
}
