package main

import (
	"embed"
	"fmt"
	"io/fs"
	"path/filepath"
	"slices"

	"github.com/eddiectc/aharness/agent"
)

//go:embed agents/*.md
var agentDefinitionFS embed.FS

// acodeIdentity is the product name in the parent prompt's identity line and
// in every delegated child's. The SDK defaults it to "aharness"; acode is the
// product, so its sessions record the acode identity. Like the user agent, it
// is not recorded, so every construction supplies it.
const acodeIdentity = "acode"

// defaultAgentDefinitions parses the five role definitions acode ships: the
// roster the /agents browser lists, the routing names the operator's
// agent_overrides key, and the descriptions the parent reads when deciding to
// delegate. The SDK ships no definitions of its own, so these are the base the
// operator's <state-dir>/agents/*.md files override by name. A parse failure
// is a build defect rather than an operator condition, so it is an error the
// caller reports at startup instead of a skipped entry.
func defaultAgentDefinitions() ([]agent.AgentDefinition, error) {
	entries, err := fs.ReadDir(agentDefinitionFS, "agents")
	if err != nil {
		return nil, fmt.Errorf("read embedded agent definitions: %w", err)
	}
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		names = append(names, entry.Name())
	}
	slices.Sort(names)
	defs := make([]agent.AgentDefinition, 0, len(names))
	for _, name := range names {
		data, err := agentDefinitionFS.ReadFile(filepath.Join("agents", name))
		if err != nil {
			return nil, fmt.Errorf("read embedded agent definition %s: %w", name, err)
		}
		def, err := agent.ParseAgentDefinition(data)
		if err != nil {
			return nil, fmt.Errorf("parse embedded agent definition %s: %w", name, err)
		}
		defs = append(defs, def)
	}
	return defs, nil
}
