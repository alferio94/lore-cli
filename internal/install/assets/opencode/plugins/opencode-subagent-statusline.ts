// opencode-subagent-statusline.ts — community statusline plugin referenced
// by the Lore-managed `~/.config/opencode/tui.json`.
//
// This file is a community plugin reference stub. It only
// declares the shape the Lore-managed `tui.json` expects, so a
// downstream install can resolve the plugin via the community
// package source. The bounded surface is config-only: the actual
// JS bundle is fetched at runtime, not bundled into the Lore
// installer.

export const LORE_OPENCODE_SUBAGENT_STATUSLINE_PLUGIN = {
	id: "opencode-subagent-statusline",
	owner: "community",
	version: 1,
	description:
		"Community statusline plugin referenced by the Lore-managed " +
		"`~/.config/opencode/tui.json`. The plugin bundle is resolved " +
		"at runtime from the documented community package source; this " +
		"file only declares the shape the tui.json expects.",
	scope: "config-only",
	source: "community://opencode-subagent-statusline",
	enables: [] as const,
};

export default function createOpenCodeSubagentStatuslinePlugin(): {
	id: string;
	owner: string;
	version: number;
} {
	return {
		id: LORE_OPENCODE_SUBAGENT_STATUSLINE_PLUGIN.id,
		owner: LORE_OPENCODE_SUBAGENT_STATUSLINE_PLUGIN.owner,
		version: LORE_OPENCODE_SUBAGENT_STATUSLINE_PLUGIN.version,
	};
}
