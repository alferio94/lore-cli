// model-variants.ts — Lore-managed OpenCode model-variants plugin.
//
// This is a minimal stub that follows the standard Lore plugin
// contract (an `export default function` factory) so the asset
// round-trips through `installAssets.ReadFile` exactly like the Pi
// bundled extensions. It is fresh, minimal, and contains no
// third-party authored copy.
//
// Scope: the plugin intentionally does nothing in this slice other
// than declare its identity. The bounded surface for the OpenCode
// re-add is config-only; runtime model-variant resolution, profile
// management, and bootstrap/package-manager integration are
// explicitly out of scope. The plugin bundle references the
// community `opencode-subagent-statusline` plugin and the
// Lore-managed `tui.json` settings file.

export const LORE_MODEL_VARIANTS_PLUGIN = {
	id: "lore-model-variants",
	owner: "lore-cli",
	version: 1,
	description:
		"Lore-managed OpenCode model-variants stub. The plugin is " +
		"installed alongside the core pack but does not activate any " +
		"runtime behavior in this slice; the re-add is config-only.",
	scope: "config-only",
	enables: [] as const,
};

export default function createModelVariantsPlugin(): {
	id: string;
	owner: string;
	version: number;
} {
	return {
		id: LORE_MODEL_VARIANTS_PLUGIN.id,
		owner: LORE_MODEL_VARIANTS_PLUGIN.owner,
		version: LORE_MODEL_VARIANTS_PLUGIN.version,
	};
}
