package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestWriteBuildInfo(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	writeBuildInfo(&buf, buildInfo{
		Version:     "v1.2.3",
		GoVersion:   "go1.27.3",
		VCSRevision: "abc123",
		VCSModified: true,
		HasVCS:      true,
	})
	out := buf.String()
	for _, want := range []string{"acode v1.2.3", "go: go1.27.3", "vcs revision: abc123", "vcs modified: true"} {
		if !strings.Contains(out, want) {
			t.Errorf("writeBuildInfo output = %q, want substring %q", out, want)
		}
	}
}

func TestStampedVersion(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name          string
		linkVersion   string
		moduleVersion string
		want          string
	}{
		{name: "build stamp wins", linkVersion: "v1.2.3", moduleVersion: "v9.9.9", want: "v1.2.3"},
		{name: "build stamp without module version", linkVersion: "v1.2.3", want: "v1.2.3"},
		{name: "module version", moduleVersion: "v0.4.1", want: "v0.4.1"},
		{name: "unversioned build", want: "devel"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := stampedVersion(tt.linkVersion, tt.moduleVersion); got != tt.want {
				t.Errorf("stampedVersion(%q, %q) = %q, want %q",
					tt.linkVersion, tt.moduleVersion, got, tt.want)
			}
		})
	}
}

func TestWriteBuildInfoUnknownVCS(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	writeBuildInfo(&buf, buildInfo{
		Version:   "devel",
		GoVersion: "go1.27.3",
	})
	out := buf.String()
	for _, want := range []string{"vcs revision: unknown", "vcs modified: unknown"} {
		if !strings.Contains(out, want) {
			t.Errorf("writeBuildInfo output = %q, want substring %q", out, want)
		}
	}
}
