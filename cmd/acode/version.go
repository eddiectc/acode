package main

import (
	"fmt"
	"io"
	"runtime"
	"runtime/debug"
)

// version is stamped into the binary by a release build, for example
// -ldflags "-X main.version=v1.2.3". The linker writes it once; nothing
// assigns it at runtime, and an empty value falls back to the module version.
// A container image build excludes .git, so it has no VCS metadata to derive
// a version from and must stamp one.
var version string

// buildInfo is the subset of runtime/debug build metadata the CLI reports.
type buildInfo struct {
	Version     string
	GoVersion   string
	VCSRevision string
	VCSModified bool
	HasVCS      bool
}

// stampedVersion resolves the reported version. An explicit build stamp wins
// over the module version, and a build with neither reports devel.
func stampedVersion(linkVersion, moduleVersion string) string {
	if linkVersion != "" {
		return linkVersion
	}
	if moduleVersion != "" {
		return moduleVersion
	}
	return "devel"
}

func readBuildInfo() buildInfo {
	info := buildInfo{GoVersion: runtime.Version()}
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		info.Version = stampedVersion(version, "")
		return info
	}
	if bi.GoVersion != "" {
		info.GoVersion = bi.GoVersion
	}
	info.Version = stampedVersion(version, bi.Main.Version)
	for _, setting := range bi.Settings {
		switch setting.Key {
		case "vcs.revision":
			info.VCSRevision = setting.Value
			info.HasVCS = true
		case "vcs.modified":
			info.VCSModified = setting.Value == "true"
			info.HasVCS = true
		}
	}
	return info
}

func buildVersion() string {
	return readBuildInfo().Version
}

func printVersion(w io.Writer) {
	writeBuildInfo(w, readBuildInfo())
}

func writeBuildInfo(w io.Writer, info buildInfo) {
	revision := info.VCSRevision
	if revision == "" {
		revision = "unknown"
	}
	modified := "unknown"
	if info.HasVCS {
		modified = fmt.Sprintf("%t", info.VCSModified)
	}
	_, _ = fmt.Fprintf(w, "acode %s\n", info.Version)
	_, _ = fmt.Fprintf(w, "go: %s\n", info.GoVersion)
	_, _ = fmt.Fprintf(w, "vcs revision: %s\n", revision)
	_, _ = fmt.Fprintf(w, "vcs modified: %s\n", modified)
}
