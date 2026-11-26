//go:build unix

package main

import (
	"context"
	"testing"

	"github.com/eddiectc/aharness/engine"
	"github.com/eddiectc/aharness/model"
	"github.com/eddiectc/aharness/tools"
)

// TestImageCapabilityFollowsTheResolvedModel pins read_image's gate input: a
// discovered model carries its catalog capability, while a resume or fork that
// inherits the recorded model leaves it unknown so the provider decides.
func TestImageCapabilityFollowsTheResolvedModel(t *testing.T) {
	t.Parallel()
	cfg := v1ResumeConfig(t)
	// A capability carried from the previous session must not survive a switch
	// to a session whose model is only replayed.
	carried := true
	cfg.ImageInput = &carried
	if err := applySessionDefaults(context.Background(), &cfg, v1State(), nil, nil); err != nil {
		t.Fatalf("applySessionDefaults: %v", err)
	}
	if cfg.ImageInput != nil {
		t.Fatalf("inherited model ImageInput = %v, want unknown", *cfg.ImageInput)
	}

	resolved := config{}
	entry := model.CatalogModel{
		ID:            "vision-model",
		Dialect:       model.DialectChat,
		BaseURL:       "https://example.invalid/v1",
		ContextWindow: 100_000,
		ImageInput:    true,
	}
	if err := applyCatalogModel(&resolved, entry, nil); err != nil {
		t.Fatalf("applyCatalogModel: %v", err)
	}
	if resolved.ImageInput == nil || !*resolved.ImageInput {
		t.Fatalf("resolved model ImageInput = %v, want the catalog capability", resolved.ImageInput)
	}
	if got := toolOptions(resolved, "", "").ImageInput; got == nil || !*got {
		t.Fatalf("toolOptions ImageInput = %v, want the resolved capability", got)
	}

	// The opened host's tool options are authoritative, so a TUI session switch
	// cannot leave cfg disagreeing with the gate read_image actually applies.
	opened := config{}
	applyResolvedSpec(&opened, engine.Spec{ToolOptions: tools.Options{ImageInput: boolPtr(false)}})
	if opened.ImageInput == nil || *opened.ImageInput {
		t.Fatalf("resolved spec ImageInput = %v, want the host's false", opened.ImageInput)
	}
	applyResolvedSpec(&opened, engine.Spec{})
	if opened.ImageInput != nil {
		t.Fatalf("resolved spec ImageInput = %v, want unknown", *opened.ImageInput)
	}
}
