// background-agents.ts — Lore-managed OpenCode background-agents plugin.
//
// This is a minimal stub that follows the standard Lore plugin
// contract (an `export default function` factory) so the asset
// round-trips through `installAssets.ReadFile` exactly like the Pi
// bundled extensions. It is fresh, minimal, and contains no
// third-party authored copy.
//
// Scope: the plugin intentionally does nothing in this slice other
// than declare its identity. The bounded surface for the OpenCode
// re-add is config-only; runtime background-agent behavior, profile
// management, and bootstrap/package-manager integration are
// explicitly out of scope. The plugin bundle references the
// community `opencode-subagent-statusline` plugin and the
// Lore-managed `tui.json` settings file.

export const LORE_BACKGROUND_AGENTS_PLUGIN = {
	id: "lore-background-agents",
	owner: "lore-cli",
	version: 1,
	description:
		"Lore-managed OpenCode background-agents stub. The plugin is " +
		"installed alongside the core pack but does not activate any " +
		"runtime behavior in this slice; the re-add is config-only.",
	scope: "config-only",
	enables: [] as const,
};

export default function createBackgroundAgentsPlugin(): {
	id: string;
	owner: string;
	version: number;
} {
	return {
		id: LORE_BACKGROUND_AGENTS_PLUGIN.id,
		owner: LORE_BACKGROUND_AGENTS_PLUGIN.owner,
		version: LORE_BACKGROUND_AGENTS_PLUGIN.version,
	};
}
