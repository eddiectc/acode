package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

var (
	harnessOnce sync.Once
	harnessPath string
	harnessDir  string
	harnessErr  error
)

// buildHarness returns the real binary built from the current source tree.
// Integration tests run the binary, not runArgs, so exit codes, signals, and
// file modes are real.
//
// The binary is built once per test process and shared by every caller. Go
// caches compiled packages but not a linked executable written to a fresh -o
// path, so each caller that passed its own t.TempDir() paid for a full 26 MB
// relink. Sharing one build keeps the suite fast; tests only execute the
// binary and never modify it.
func buildHarness(t *testing.T) string {
	t.Helper()
	harnessOnce.Do(func() {
		_, file, _, ok := runtime.Caller(0)
		if !ok {
			harnessErr = errors.New("runtime.Caller failed")
			return
		}
		root := filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
		dir, err := os.MkdirTemp("", "acode-test-bin")
		if err != nil {
			harnessErr = fmt.Errorf("temp dir for harness: %w", err)
			return
		}
		bin := filepath.Join(dir, "acode")
		cmd := exec.CommandContext(context.Background(), "go", "build", "-trimpath", "-o", bin, "./cmd/acode")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			_ = os.RemoveAll(dir)
			harnessErr = fmt.Errorf("build harness: %w\n%s", err, out)
			return
		}
		harnessPath, harnessDir = bin, dir
	})
	if harnessErr != nil {
		t.Fatal(harnessErr)
	}
	return harnessPath
}

// cleanupHarness removes the shared test binary. TestMain calls it.
func cleanupHarness() {
	if harnessDir != "" {
		_ = os.RemoveAll(harnessDir)
	}
}

// testHomeDir is the default HOME for tests that parse a config in-process.
// It contains an unreadable ~/.acode/config.json, so a parse that consults the
// operator's real home fails loudly instead of silently ingesting the
// operator's settings. The directory is created lazily and once: Go's caches
// resolve under $HOME, and creating it before the module cache is warm would
// redirect the first `go build` and force a module re-download.
//
// Tests never redirect the process environment itself (see testEnvBase): Go
// reads GOCACHE and GOMODCACHE from the real env, so a process-wide HOME
// would move the caches off their warm locations and turn the suite's
// `go build` into network work this repo forbids.
var (
	testHomeOnce sync.Once
	testHomeDir  string
)

func testHome() string {
	testHomeOnce.Do(func() {
		dir, err := os.MkdirTemp("", "acode-test-home")
		if err != nil {
			panic(fmt.Sprintf("test home: %v", err))
		}
		configDir := filepath.Join(dir, ".acode")
		if err := os.MkdirAll(configDir, 0o700); err != nil {
			panic(fmt.Sprintf("test home config dir: %v", err))
		}
		if err := os.WriteFile(filepath.Join(configDir, "config.json"), []byte(`{"model": "operator",`), 0o600); err != nil {
			panic(fmt.Sprintf("write test home config: %v", err))
		}
		// Mode 000 makes the file unreadable by this user, so any read of the
		// default implicit config fails even if a future parser tolerates
		// truncated JSON.
		if err := os.Chmod(filepath.Join(configDir, "config.json"), 0); err != nil {
			panic(fmt.Sprintf("chmod test home config: %v", err))
		}
		testHomeDir = dir
	})
	return testHomeDir
}

// cleanupTestHome removes the poisoned home. TestMain calls it after the
// tests, once no config parse can run.
func cleanupTestHome() {
	if testHomeDir != "" {
		_ = os.RemoveAll(testHomeDir)
	}
}

// testEnvBase is the single canonical base environment for every harness
// child process. It copies the operator environment minus every ACODE_
// variable, then replaces HOME with the poisoned test home, so a child cannot
// read the operator's real state directory or config, whatever flags it is
// handed and whatever ACODE_* the operator exported. Every child env is
// built from this base; no site may pass os.Environ() to a child directly.
func testEnvBase() []string {
	env := make([]string, 0, len(os.Environ())+2)
	for _, entry := range os.Environ() {
		if strings.HasPrefix(entry, "ACODE_") {
			continue
		}
		env = append(env, entry)
	}
	return append(env, "HOME="+testHome())
}

// runCLIEnv runs the CLI in-process against an explicit environment. It mirrors
// runWithIO, so stdout, stderr, and the exit code match a real process without
// paying process startup. Use it for setup runs whose process boundary is not
// under test; signals, TTY handling, file modes, and the run under test still
// execute the real binary.
func runCLIEnv(t *testing.T, getenv func(string) string, stdin io.Reader, args ...string) (stdout, stderr string, exit int) {
	t.Helper()
	var out, errBuf bytes.Buffer
	exit = runWithIOEnv(args, getenv, stdin, &out, &errBuf)
	return out.String(), errBuf.String(), exit
}

// envLookup turns environment entries into the getenv function parseConfig
// expects. A missing key reads as unset, matching a child process.
func envLookup(entries []string) func(string) string {
	values := make(map[string]string, len(entries))
	for _, entry := range entries {
		if key, value, ok := strings.Cut(entry, "="); ok {
			values[key] = value
		}
	}
	return func(key string) string { return values[key] }
}
