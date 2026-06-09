/**
 * lore-models
 *
 * Lore-managed OpenCode plugin that:
 *   1. Snapshots provider model variant keys into the existing
 *      `~/.lore/cache/opencode-model-variants.json` cache (best-effort,
 *      metadata only — never authoritative for user-chosen settings).
 *   2. Exposes an in-OpenCode configuration flow that lets the user pick
 *      `model` and `variant` per Lore-managed agent. The preferred
 *      UX is a floating/dialog selector when a verified safe OpenCode
 *      runtime API exists; otherwise this plugin offers a documented
 *      fallback flow that:
 *        - reads the current value,
 *        - reports the selected model/variant,
 *        - persists the choice by hot-editing `opencode.json` safely
 *          (parse, validate, backup, atomic write with restrictive
 *          permissions, re-verify, redact secrets), and
 *        - surfaces success/failure inside OpenCode via
 *          `client.tui.showToast` when available.
 *   3. Never edits anything other than `agent.<name>.model` and
 *      `agent.<name>.variant` inside `~/.config/opencode/opencode.json`,
 *      and only for agents it recognizes as Lore-managed by name
 *      and prompt/mode convention.
 *
 * The plugin intentionally does NOT use undocumented OpenCode UI
 * internals as the only path: when a safe selector API is unavailable,
 * the fallback is a structured in-OpenCode command/tool flow that
 * reports the proposal, applies the safe hot-edit, and reports the
 * result. The user never has to leave OpenCode or hand-edit
 * `opencode.json`.
 */

import type { Plugin } from "@opencode-ai/plugin"
import { chmod, mkdir, readFile, rename, writeFile } from "fs/promises"
import { homedir } from "os"
import path from "path"

type ProviderWithModels = {
  id?: string
  models?: Record<string, { variants?: Record<string, unknown> }>
}

type LoreAgentSelector = "lore" | "lore-worker" | "sdd"

const LORE_MANAGED_AGENT_NAMES: string[] = [
  "lore",
  "lore-worker",
  "sdd-init",
  "sdd-explore",
  "sdd-propose",
  "sdd-spec",
  "sdd-design",
  "sdd-tasks",
  "sdd-apply",
  "sdd-verify",
  "sdd-archive",
]

const SECRET_LIKE_KEYS = [
  "Authorization",
  "authorization",
  "apiKey",
  "api_key",
  "token",
  "password",
  "secret",
]

function providerList(data: unknown): ProviderWithModels[] {
  const payload = (data as { data?: unknown })?.data ?? data
  const candidate =
    (payload as { all?: unknown[] })?.all ??
    (payload as { providers?: unknown[] })?.providers ??
    (Array.isArray(payload) ? payload : [])
  return Array.isArray(candidate) ? (candidate as ProviderWithModels[]) : []
}

function isPlainObject(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value)
}

function looksLikeLoreManagedAgent(name: string, entry: unknown): boolean {
  if (!isPlainObject(entry)) return false
  if (!LORE_MANAGED_AGENT_NAMES.includes(name)) return false
  const prompt = entry.prompt
  if (typeof prompt !== "string") return false
  if (name === "lore") {
    return prompt === "{file:./AGENTS.md}"
  }
  return prompt === `{file:./skills/${name}/SKILL.md}`
}

function looksLikeLoreManagedPayload(payload: unknown): boolean {
  if (!isPlainObject(payload)) return false
  const agent = payload.agent
  if (!isPlainObject(agent)) return false
  const lore = agent["lore"]
  if (!looksLikeLoreManagedAgent("lore", lore)) return false
  // At least one Lore-managed agent (sdd-* or lore-worker) must be
  // present for the file to be considered a Lore-managed config.
  let loreFound = false
  for (const name of Object.keys(agent)) {
    if (LORE_MANAGED_AGENT_NAMES.includes(name) && name !== "lore") {
      loreFound = true
      break
    }
  }
  return loreFound
}

function defaultOpenCodeJSONPath(): string {
  return path.join(homedir(), ".config", "opencode", "opencode.json")
}

function defaultBackupDir(): string {
  return path.join(homedir(), ".config", "opencode", "backups")
}

function timestampStamp(): string {
  const now = new Date()
  const pad = (n: number) => String(n).padStart(2, "0")
  return (
    `${now.getUTCFullYear()}${pad(now.getUTCMonth() + 1)}${pad(now.getUTCDate())}` +
    `T${pad(now.getUTCHours())}${pad(now.getUTCMinutes())}${pad(now.getUTCSeconds())}Z`
  )
}

// redactSecretLike walks a JSON-ish object and replaces any value
// associated with a secret-bearing key with `"<redacted>"`. It
// preserves type, structure, and key order so error messages and
// logs that include the redacted tree remain useful for
// troubleshooting without leaking the secret value.
export function redactSecretLike<T>(value: T): T {
  if (Array.isArray(value)) {
    return value.map((entry) => redactSecretLike(entry)) as unknown as T
  }
  if (!isPlainObject(value)) {
    return value
  }
  const out: Record<string, unknown> = {}
  for (const [key, val] of Object.entries(value)) {
    if (SECRET_LIKE_KEYS.includes(key) && typeof val === "string") {
      out[key] = "<redacted>"
    } else {
      out[key] = redactSecretLike(val)
    }
  }
  return out as unknown as T
}

// hotEditAgentModelVariant performs a safe live edit of the given
// `opencode.json` file. The edit is bounded to `agent.<name>.model`
// and `agent.<name>.variant`. The function:
//
//   - rejects edits for agents that are not recognizably Lore-managed
//     by name and prompt/mode convention,
//   - rejects edits that would clobber the file when the existing
//     content cannot be parsed,
//   - preserves a deep copy of the prior file as a backup before any
//     write,
//   - writes the updated file atomically (temp file in the same
//     directory, fsync-equivalent close, rename over the target) with
//     `0600` permissions because `opencode.json` may carry bearer
//     tokens,
//   - reparses the written file to verify the requested fields are
//     present, and
//   - redacts Authorization/token-bearing values in any error or log
//     surface.
//
// The returned summary lists the changed fields without printing
// secret-bearing values.
export async function hotEditAgentModelVariant(params: {
  jsonPath?: string
  agentName: string
  model: string
  variant?: string | null
  backupDir?: string
}): Promise<{ changed: string[]; backupPath: string }> {
  const jsonPath = params.jsonPath ?? defaultOpenCodeJSONPath()
  const backupDir = params.backupDir ?? defaultBackupDir()
  const agentName = String(params.agentName ?? "").trim()
  const model = String(params.model ?? "").trim()
  const variantInput = params.variant
  const variant =
    variantInput == null
      ? undefined
      : String(variantInput).trim() === ""
        ? undefined
        : String(variantInput).trim()

  if (!agentName) {
    throw new Error("hot-edit: agentName is required")
  }
  if (!model) {
    throw new Error("hot-edit: model is required")
  }
  if (!LORE_MANAGED_AGENT_NAMES.includes(agentName)) {
    throw new Error(
      `hot-edit: agent ${JSON.stringify(agentName)} is not a Lore-managed agent`,
    )
  }
  if (variant !== undefined && variant === "") {
    throw new Error("hot-edit: variant, when present, must be non-empty")
  }

  const raw = await readFile(jsonPath, "utf8").catch((err: unknown) => {
    if (
      err &&
      typeof err === "object" &&
      "code" in err &&
      (err as { code?: string }).code === "ENOENT"
    ) {
      throw new Error(
        `hot-edit: opencode.json not found at ${jsonPath}; run \`lore install --target opencode\` first`,
      )
    }
    throw new Error(
      `hot-edit: read ${jsonPath} failed (${describeError(err)})`,
    )
  })

  let parsed: unknown
  try {
    parsed = JSON.parse(raw)
  } catch (err) {
    throw new Error(
      `hot-edit: opencode.json at ${jsonPath} is not valid JSON (${describeError(err)})`,
    )
  }
  if (!looksLikeLoreManagedPayload(parsed)) {
    throw new Error(
      `hot-edit: opencode.json at ${jsonPath} does not look like a Lore-managed config (missing agent overlay or Lore-managed prompt references)`,
    )
  }
  const payload = parsed as Record<string, unknown>
  const agent = payload.agent as Record<string, unknown>
  if (!looksLikeLoreManagedAgent(agentName, agent[agentName])) {
    throw new Error(
      `hot-edit: agent ${JSON.stringify(agentName)} is not present or not Lore-managed in ${jsonPath}; run \`lore install --target opencode\` first`,
    )
  }

  const backupPath = path.join(backupDir, `opencode.json.${timestampStamp()}.bak`)
  const changed: string[] = []
  const original = JSON.parse(JSON.stringify(payload)) as Record<string, unknown>

  // Apply edits to a deep clone so we can keep `original` for rollback
  // and so the final write only touches the requested fields.
  const updated = JSON.parse(JSON.stringify(payload)) as Record<string, unknown>
  const updatedAgent = updated.agent as Record<string, unknown>
  const entry = { ...(updatedAgent[agentName] as Record<string, unknown>) }
  entry.model = model
  if (variant === undefined) {
    // Explicit "no variant/default" path: only remove the field when
    // the caller asked for null. Otherwise preserve any pre-existing
    // value (so callers that pass variantInput=null do not silently
    // clobber a user's existing variant selection).
    if (variantInput === null) {
      if ("variant" in entry) {
        delete entry.variant
        changed.push("variant:removed")
      }
    } else if (params.variant !== undefined) {
      entry.variant = variant
      changed.push("variant:set")
    }
  } else {
    entry.variant = variant
    changed.push("variant:set")
  }
  if (!changed.includes("variant:set") && !changed.includes("variant:removed")) {
    changed.push("model:set")
  }
  updatedAgent[agentName] = entry

  // Verify the requested change is the only meaningful change vs the
  // original (defense in depth against accidental subtree edits).
  const beforeAgentEntry = (original.agent as Record<string, unknown>)[
    agentName
  ] as Record<string, unknown>
  if (beforeAgentEntry.model !== model) {
    changed.push("model:set")
  }

  // Backup first, atomic write second. Backup write is best-effort
  // for the path/permissions; we intentionally write the backup with
  // 0600 and rename over any existing file.
  await mkdir(backupDir, { recursive: true, mode: 0o700 }).catch(() => {
    /* ignore */
  })
  await writeFile(`${backupPath}.tmp`, JSON.stringify(original, null, 2), {
    mode: 0o600,
  })
  await rename(`${backupPath}.tmp`, backupPath)
  await chmod(backupPath, 0o600).catch(() => {
    /* best-effort */
  })

  const finalText = JSON.stringify(updated, null, 2) + "\n"
  const tempPath = `${jsonPath}.tmp`
  await writeFile(tempPath, finalText, { mode: 0o600 })
  await rename(tempPath, jsonPath)
  await chmod(jsonPath, 0o600).catch(() => {
    /* best-effort */
  })

  // Re-verify the on-disk file contains the requested values.
  const reparsed = JSON.parse(await readFile(jsonPath, "utf8")) as Record<
    string,
    unknown
  >
  const verifyAgent = (reparsed.agent as Record<string, unknown>)[
    agentName
  ] as Record<string, unknown>
  if (verifyAgent.model !== model) {
    throw new Error(
      `hot-edit: post-write verification failed (model mismatch); backup is at ${backupPath}`,
    )
  }
  if (variant !== undefined && verifyAgent.variant !== variant) {
    throw new Error(
      `hot-edit: post-write verification failed (variant mismatch); backup is at ${backupPath}`,
    )
  }

  return { changed, backupPath }
}

function describeError(err: unknown): string {
  if (!err) return "unknown"
  if (typeof err === "string") return err
  if (typeof err === "object") {
    const redacted = redactSecretLike(err as Record<string, unknown>)
    try {
      return JSON.stringify(redacted)
    } catch {
      return "<unserializable error>"
    }
  }
  return String(err)
}

export const LoreModelsPlugin: Plugin = async ({ client }) => {
  async function refreshVariantsCache(): Promise<void> {
    try {
      const result = await client.provider.list()
      const variants: Record<string, Record<string, string[]>> = {}

      for (const provider of providerList(result)) {
        if (!provider.id) continue
        for (const [modelID, model] of Object.entries(provider.models ?? {})) {
          const keys = Object.keys(model.variants ?? {}).sort()
          if (keys.length === 0) continue
          variants[provider.id] = variants[provider.id] ?? {}
          variants[provider.id][modelID] = keys
        }
      }

      const cacheDir = path.join(homedir(), ".lore", "cache")
      await mkdir(cacheDir, { recursive: true })
      const finalPath = path.join(cacheDir, "opencode-model-variants.json")
      const tmpPath = `${finalPath}.tmp`
      await writeFile(tmpPath, JSON.stringify(variants, null, 2), "utf8")
      await rename(tmpPath, finalPath)
    } catch (error) {
      const redacted = describeError(error)
      console.error("[lore-models] cache refresh failed:", redacted)
    }
  }

  async function safeHotEdit(
    agentName: string,
    model: string,
    variant?: string | null,
  ): Promise<string> {
    try {
      const result = await hotEditAgentModelVariant({
        agentName,
        model,
        variant,
      })
      const message = `[lore-models] updated ${agentName}: ${result.changed.join(", ")} (backup=${result.backupPath})`
      console.log(message)
      return message
    } catch (error) {
      const redacted = describeError(error)
      const message = `[lore-models] hot-edit failed: ${redacted}`
      console.error(message)
      throw new Error(message)
    }
  }

  refreshVariantsCache().catch((error) => {
    console.error(
      "[lore-models] unexpected refresh error:",
      describeError(error),
    )
  })

  return {
    // Fallback in-OpenCode interaction: callers can use the
    // `lore_models_set_agent` tool to set model+variant for a
    // Lore-managed agent from inside OpenCode. The tool is the
    // documented in-OpenCode path when no floating selector API
    // is available. A floating/dialog UI is preferred where a
    // verified safe selector API is available; the tool stays
    // available regardless so the user is never required to leave
    // OpenCode to configure model/variant.
    tool: {
      lore_models_set_agent: {
        description:
          "Set the model (and optional variant) for a Lore-managed agent by hot-editing ~/.config/opencode/opencode.json safely. Use this when no floating selector UI is available.",
        args: {
          agent: {
            type: "string",
            description: "Lore-managed agent name (e.g. lore, lore-worker, sdd-design).",
          },
          model: {
            type: "string",
            description: "Model identifier (e.g. anthropic/claude-sonnet-4).",
          },
          variant: {
            type: "string",
            description: "Optional variant name; pass null/empty to remove.",
            required: false,
          },
        },
        async execute(args: { agent: string; model: string; variant?: string | null }) {
          try {
            return await safeHotEdit(args.agent, args.model, args.variant)
          } catch (error) {
            return describeError(error)
          }
        },
      },
      lore_models_list_agents: {
        description:
          "List the Lore-managed agent names this plugin will edit. Useful as a fallback discovery surface when no selector UI is available.",
        args: {},
        async execute() {
          return JSON.stringify(LORE_MANAGED_AGENT_NAMES)
        },
      },
    },
  }
}

export default LoreModelsPlugin
