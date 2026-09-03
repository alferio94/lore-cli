package install

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentconfig"
	"github.com/alferio94/lore-cli/internal/agentpack"
)

const (
	codexStrongProfileRelativePath = "sdd-strong.config.toml"
	codexMidProfileRelativePath    = "sdd-mid.config.toml"
	codexCheapProfileRelativePath  = "sdd-cheap.config.toml"
)

type codexSDDLane struct {
	Name            string
	ProfilePath     string
	Model           string
	ReasoningEffort string
	Use             string
}

var codexLoreMCPApprovedTools = []string{
	"lore_me",
	"lore_project_activity",
	"lore_project_context",
	"lore_project_get",
	"lore_project_list",
	"lore_memory_get",
	"lore_memory_save",
	"lore_memory_search",
}

func renderCodexSDDSection(cfg agentconfig.Config) string {
	lanes := codexSDDLanes(cfg)
	lines := []string{
		"## Codex-native SDD orchestration",
		"",
		"- Default to Codex native multi-agent/subagent execution for non-trivial repository work when `spawn_agent`, `wait_agent`, and `close_agent` are available.",
		"- For SDD phases, spawn the matching specialized worker (`sdd-explore`, `sdd-propose`, `sdd-spec`, `sdd-design`, `sdd-tasks`, `sdd-apply`, `sdd-verify`, `sdd-archive`) and wait for its compact handoff before continuing.",
		"- Route workers by SDD lane; pass the model and reasoning effort explicitly when the Codex tool surface supports those fields.",
		"- If Codex native agent tools are unavailable, continue inline/sequentially and preserve the same SDD artifact persistence and compact envelope contracts.",
		"- Do not treat projected profiles as a global default override; they are explicit session presets and routing lanes.",
		"",
		"### SDD lanes",
		"",
	}
	for _, lane := range lanes {
		lines = append(lines, fmt.Sprintf("- `%s` (`%s`): model `%s`, reasoning effort `%s` — %s.", lane.Name, lane.ProfilePath, lane.Model, lane.ReasoningEffort, lane.Use))
	}
	return strings.Join(lines, "\n")
}

func renderCodexSDDProfiles(cfg agentconfig.Config) []RenderedFile {
	lanes := codexSDDLanes(cfg)
	rendered := make([]RenderedFile, 0, len(lanes))
	for _, lane := range lanes {
		content := strings.Join([]string{
			fmt.Sprintf("model = %q", lane.Model),
			fmt.Sprintf("model_reasoning_effort = %q", lane.ReasoningEffort),
			"",
		}, "\n")
		rendered = append(rendered, RenderedFile{
			Component:    ComponentCorePack,
			RelativePath: filepath.ToSlash(lane.ProfilePath),
			MergeMode:    MergeModeReplace,
			Content:      []byte(content),
		})
	}
	return rendered
}

func renderCodexLoreMCPApprovalFragment() string {
	tools := append([]string(nil), codexLoreMCPApprovedTools...)
	sort.Strings(tools)
	sections := make([]string, 0, len(tools))
	for _, tool := range tools {
		sections = append(sections, strings.Join([]string{
			fmt.Sprintf("[mcp_servers.lore.tools.%s]", tool),
			`approval_mode = "approve"`,
		}, "\n"))
	}
	return strings.Join(sections, "\n\n")
}

func codexSDDLanes(cfg agentconfig.Config) []codexSDDLane {
	model := codexSDDProfileModel(cfg)
	return []codexSDDLane{
		{
			Name:            "strong",
			ProfilePath:     codexStrongProfileRelativePath,
			Model:           model,
			ReasoningEffort: "high",
			Use:             "architecture, exploration, design, apply, and risky verification",
		},
		{
			Name:            "mid",
			ProfilePath:     codexMidProfileRelativePath,
			Model:           model,
			ReasoningEffort: "medium",
			Use:             "specification, task breakdown, and normal implementation handoffs",
		},
		{
			Name:            "cheap",
			ProfilePath:     codexCheapProfileRelativePath,
			Model:           model,
			ReasoningEffort: "low",
			Use:             "mechanical documentation, summaries, and low-risk cleanup",
		},
	}
}

func codexSDDProfileModel(cfg agentconfig.Config) string {
	if agent, ok := cfg.SDDAgents["sdd-apply"]; ok && strings.TrimSpace(agent.Model) != "" {
		return strings.TrimSpace(agent.Model)
	}
	if len(cfg.SDDAgents) > 0 {
		names := make([]string, 0, len(cfg.SDDAgents))
		for name := range cfg.SDDAgents {
			names = append(names, name)
		}
		sort.Strings(names)
		for _, name := range names {
			if model := strings.TrimSpace(cfg.SDDAgents[name].Model); model != "" {
				return model
			}
		}
	}
	return agentpack.DefaultSDDModel
}
