package install

import (
	"fmt"
	"strings"

	"github.com/alferio94/lore-cli/internal/agentpack"
)

func renderManagedSkillMarkdown(skill agentpack.ManagedSkill) string {
	var builder strings.Builder
	builder.WriteString("---\n")
	builder.WriteString(fmt.Sprintf("name: %s\n", skill.Name))
	builder.WriteString("description: >\n")
	for _, line := range strings.Split(strings.TrimSpace(skill.Description), "\n") {
		builder.WriteString("  ")
		builder.WriteString(strings.TrimSpace(line))
		builder.WriteByte('\n')
	}
	builder.WriteString("---\n")
	builder.WriteString(skill.Body)
	if !strings.HasSuffix(skill.Body, "\n") {
		builder.WriteByte('\n')
	}
	return builder.String()
}
