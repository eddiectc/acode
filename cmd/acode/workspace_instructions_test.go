package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eddiectc/aharness/agent"
	"github.com/eddiectc/aharness/tools"
)

// openTestWorkspace opens dir as the SDK workspace handle the reader requires.
func openTestWorkspace(t *testing.T, dir string) *tools.Workspace {
	t.Helper()
	ws, err := tools.OpenWorkspace(dir)
	if err != nil {
		t.Fatalf("open workspace: %v", err)
	}
	t.Cleanup(func() { _ = ws.Close() })
	return ws
}

func writeWorkspaceFile(t *testing.T, dir, name string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), data, mode); err != nil {
		t.Fatalf("write %s: %v", name, err)
	}
}

func TestReadWorkspaceInstructions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		setup     func(t *testing.T, dir string)
		want      string
		wantError bool
	}{
		{
			name:  "absent",
			setup: func(*testing.T, string) {},
		},
		{
			name: "empty",
			setup: func(t *testing.T, dir string) {
				writeWorkspaceFile(t, dir, workspaceInstructionsFile, nil, 0o600)
			},
		},
		{
			name: "normal",
			setup: func(t *testing.T, dir string) {
				writeWorkspaceFile(t, dir, workspaceInstructionsFile, []byte("Use tabs.\n"), 0o600)
			},
			want: "Use tabs.\n",
		},
		{
			name: "at cap",
			setup: func(t *testing.T, dir string) {
				writeWorkspaceFile(t, dir, workspaceInstructionsFile,
					[]byte(strings.Repeat("a", maxWorkspaceInstructionsBytes)), 0o600)
			},
			want: strings.Repeat("a", maxWorkspaceInstructionsBytes),
		},
		{
			name: "over cap",
			setup: func(t *testing.T, dir string) {
				writeWorkspaceFile(t, dir, workspaceInstructionsFile,
					[]byte(strings.Repeat("a", maxWorkspaceInstructionsBytes+1)), 0o600)
			},
			wantError: true,
		},
		{
			name: "invalid utf8",
			setup: func(t *testing.T, dir string) {
				writeWorkspaceFile(t, dir, workspaceInstructionsFile, []byte("ok\xff\n"), 0o600)
			},
			want: "ok\uFFFD\n",
		},
		{
			name: "symlink",
			setup: func(t *testing.T, dir string) {
				writeWorkspaceFile(t, dir, "target.md", []byte("linked\n"), 0o600)
				if err := os.Symlink("target.md", filepath.Join(dir, workspaceInstructionsFile)); err != nil {
					t.Skipf("symlink unsupported: %v", err)
				}
			},
			wantError: true,
		},
		{
			name: "dangling symlink",
			setup: func(t *testing.T, dir string) {
				if err := os.Symlink("missing.md", filepath.Join(dir, workspaceInstructionsFile)); err != nil {
					t.Skipf("symlink unsupported: %v", err)
				}
			},
			wantError: true,
		},
		{
			name: "directory",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, workspaceInstructionsFile), 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
			},
			wantError: true,
		},
		{
			name: "unreadable",
			setup: func(t *testing.T, dir string) {
				if os.Geteuid() == 0 {
					t.Skip("running as root: mode bits do not deny the read")
				}
				writeWorkspaceFile(t, dir, workspaceInstructionsFile, []byte("secret\n"), 0)
			},
			wantError: true,
		},
		{
			name: "nested only",
			setup: func(t *testing.T, dir string) {
				if err := os.Mkdir(filepath.Join(dir, "sub"), 0o700); err != nil {
					t.Fatalf("mkdir: %v", err)
				}
				writeWorkspaceFile(t, filepath.Join(dir, "sub"), workspaceInstructionsFile, []byte("nested\n"), 0o600)
			},
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			tc.setup(t, dir)
			got, err := readWorkspaceInstructions(openTestWorkspace(t, dir))
			if tc.wantError {
				if err == nil {
					t.Fatalf("read error = nil, got text %q", got)
				}
				if got != "" {
					t.Fatalf("read text = %q, want none on error", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("read: %v", err)
			}
			if got != tc.want {
				t.Fatalf("text = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestReadWorkspaceInstructionsOverCapIsTooLarge(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	writeWorkspaceFile(t, dir, workspaceInstructionsFile,
		[]byte(strings.Repeat("a", maxWorkspaceInstructionsBytes+1)), 0o600)
	_, err := readWorkspaceInstructions(openTestWorkspace(t, dir))
	if !errors.Is(err, tools.ErrTooLarge) {
		t.Fatalf("over-cap error = %v, want ErrTooLarge", err)
	}
}

func TestSeedWorkspaceInstructionsComposes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const text = "# Workspace rules\n\nWrite artifacts under out/.\n"
	writeWorkspaceFile(t, dir, workspaceInstructionsFile, []byte(text), 0o600)
	ws := openTestWorkspace(t, dir)
	cfg := config{
		Workspace:     dir,
		PromptVersion: agent.PromptVersion,
		Agents:        []agent.AgentInfo{{Name: "explore", Description: "Read-only lookup."}},
	}
	if warning := seedWorkspaceInstructions(&cfg, ws); warning != "" {
		t.Fatalf("seed warning = %q", warning)
	}
	if cfg.AppendedPrompt != text {
		t.Fatalf("appended prompt = %q, want the file text", cfg.AppendedPrompt)
	}
	if !strings.HasPrefix(cfg.SystemPrompt, "You are acode, a coding harness working in the workspace") {
		t.Fatalf("composed prompt lacks the acode identity:\n%s", cfg.SystemPrompt)
	}
	if !strings.Contains(cfg.SystemPrompt, "- explore: Read-only lookup.") {
		t.Fatalf("composed prompt lacks the catalog:\n%s", cfg.SystemPrompt)
	}
	if !strings.HasSuffix(cfg.SystemPrompt, "\n\n"+text) {
		t.Fatalf("composed prompt does not end with the file after a blank line:\n%s", cfg.SystemPrompt)
	}
	if got := strings.Count(cfg.SystemPrompt, text); got != 1 {
		t.Fatalf("file appears %d times, want once", got)
	}
}

func TestSeedWorkspaceInstructionsKeepsAnExplicitPrompt(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const text = "workspace rules\n"
	writeWorkspaceFile(t, dir, workspaceInstructionsFile, []byte(text), 0o600)
	cfg := config{SystemPrompt: "explicit base"}
	if warning := seedWorkspaceInstructions(&cfg, openTestWorkspace(t, dir)); warning != "" {
		t.Fatalf("seed warning = %q", warning)
	}
	if cfg.SystemPrompt != "explicit base\n\n"+text || cfg.AppendedPrompt != text {
		t.Fatalf("prompt = %q appended = %q", cfg.SystemPrompt, cfg.AppendedPrompt)
	}
}

func TestSeedWorkspaceInstructionsChangesNothingWithoutFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	cfg := config{SystemPrompt: "unchanged", AppendedPrompt: "kept"}
	if warning := seedWorkspaceInstructions(&cfg, openTestWorkspace(t, dir)); warning != "" {
		t.Fatalf("seed warning = %q", warning)
	}
	if cfg.SystemPrompt != "unchanged" || cfg.AppendedPrompt != "kept" {
		t.Fatalf("cfg changed without a file: prompt = %q appended = %q", cfg.SystemPrompt, cfg.AppendedPrompt)
	}
}

func TestSeedWorkspaceInstructionsWarnsAndLeavesPromptOnSkip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, workspaceInstructionsFile), 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	cfg := config{SystemPrompt: "unchanged", AppendedPrompt: "kept"}
	warning := seedWorkspaceInstructions(&cfg, openTestWorkspace(t, dir))
	if !strings.Contains(warning, "workspace instructions skipped") ||
		!strings.Contains(warning, workspaceInstructionsFile) {
		t.Fatalf("warning = %q", warning)
	}
	if cfg.SystemPrompt != "unchanged" || cfg.AppendedPrompt != "kept" {
		t.Fatalf("cfg changed on skip: prompt = %q appended = %q", cfg.SystemPrompt, cfg.AppendedPrompt)
	}
}
