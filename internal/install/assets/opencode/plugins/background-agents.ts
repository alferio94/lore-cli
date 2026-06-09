/**
 * background-agents
 *
 * Lore-managed OpenCode plugin that adds async, persistent delegation tools.
 * The plugin is copied by `lore install --target opencode` into
 * ~/.config/opencode/plugins/, where OpenCode auto-discovers local plugins.
 */

import * as crypto from "node:crypto"
import * as fs from "node:fs/promises"
import * as os from "node:os"
import * as path from "node:path"
import { execFile } from "node:child_process"
import { type Plugin, type ToolContext, tool } from "@opencode-ai/plugin"
import type { createOpencodeClient } from "@opencode-ai/sdk"

type OpencodeClient = ReturnType<typeof createOpencodeClient>

interface Delegation {
  id: string
  sessionID: string
  parentSessionID: string
  parentAgent: string
  prompt: string
  agent: string
  status: "running" | "complete" | "error" | "cancelled" | "timeout"
  startedAt: Date
  completedAt?: Date
  error?: string
  result?: string
}

interface SessionMessageItem {
  info: { role?: string }
  parts?: Array<{ type?: string; text?: string }>
}

const MAX_RUN_TIME_MS = 15 * 60 * 1000
const WORDS_A = ["brisk", "calm", "clear", "quiet", "steady", "swift", "tidy", "warm"]
const WORDS_B = ["amber", "blue", "green", "silver", "violet", "white", "gold", "red"]
const WORDS_C = ["badger", "falcon", "lynx", "otter", "raven", "tiger", "wolf", "wren"]

function log(client: OpencodeClient, level: "debug" | "info" | "warn" | "error", message: string): void {
  client.app.log({ body: { service: "background-agents", level, message } }).catch(() => {})
}

function randomWord(words: string[]): string {
  const bytes = crypto.randomBytes(2)
  return words[bytes.readUInt16BE(0) % words.length]
}

function generateReadableId(): string {
  return `${randomWord(WORDS_A)}-${randomWord(WORDS_B)}-${randomWord(WORDS_C)}`
}

function hashPath(projectRoot: string): string {
  return crypto.createHash("sha256").update(projectRoot).digest("hex").slice(0, 16)
}

async function projectID(projectRoot: string): Promise<string> {
  return await new Promise<string>((resolve) => {
    const timer = setTimeout(() => resolve(hashPath(projectRoot)), 5000)
    execFile("git", ["rev-list", "--max-parents=0", "--all"], { cwd: projectRoot }, (error, stdout) => {
      clearTimeout(timer)
      if (error) return resolve(hashPath(projectRoot))
      const root = stdout
        .split("\n")
        .map((x) => x.trim())
        .filter(Boolean)
        .sort()[0]
      resolve(root && /^[a-f0-9]{40}$/i.test(root) ? root : hashPath(projectRoot))
    })
  })
}

function firstTextFromLastAssistant(messages: SessionMessageItem[] | undefined): string {
  const assistant = (messages ?? []).filter((m) => m.info?.role === "assistant").at(-1)
  const text = (assistant?.parts ?? [])
    .filter((p) => p.type === "text" && typeof p.text === "string")
    .map((p) => p.text)
    .join("\n")
    .trim()
  return text || "Delegation completed but produced no text output."
}

class DelegationManager {
  private delegations = new Map<string, Delegation>()
  private pendingByParent = new Map<string, Set<string>>()

  constructor(
    private readonly client: OpencodeClient,
    private readonly baseDir: string,
  ) {}

  private async getRootSessionID(sessionID: string): Promise<string> {
    let currentID = sessionID
    for (let depth = 0; depth < 10; depth++) {
      try {
        const session = await this.client.session.get({ path: { id: currentID } })
        const parentID = (session.data as { parentID?: string } | undefined)?.parentID
        if (!parentID) return currentID
        currentID = parentID
      } catch {
        return currentID
      }
    }
    return currentID
  }

  private async directoryFor(sessionID: string): Promise<string> {
    const rootID = await this.getRootSessionID(sessionID)
    const dir = path.join(this.baseDir, rootID)
    await fs.mkdir(dir, { recursive: true })
    return dir
  }

  private async outputPath(sessionID: string, id: string): Promise<string> {
    return path.join(await this.directoryFor(sessionID), `${id}.md`)
  }

  async delegate(input: { parentSessionID: string; parentAgent: string; prompt: string; agent: string }): Promise<Delegation> {
    const agentsResult = await this.client.app.agents({})
    const agents = (agentsResult.data ?? []) as Array<{ name: string; mode?: string; description?: string }>
    const validAgent = agents.find((agent) => agent.name === input.agent)
    if (!validAgent) {
      const available = agents.map((agent) => `- ${agent.name}${agent.description ? `: ${agent.description}` : ""}`).join("\n")
      throw new Error(`Agent ${JSON.stringify(input.agent)} was not found.\n\nAvailable agents:\n${available || "(none)"}`)
    }

    let id = generateReadableId()
    for (let attempts = 0; this.delegations.has(id) && attempts < 10; attempts++) id = generateReadableId()
    if (this.delegations.has(id)) id = crypto.randomUUID().slice(0, 8)

    const session = await this.client.session.create({
      body: { title: `Delegation: ${id}`, parentID: input.parentSessionID },
    })
    const sessionID = (session.data as { id?: string } | undefined)?.id
    if (!sessionID) throw new Error("OpenCode did not return a delegation session id")

    const delegation: Delegation = {
      id,
      sessionID,
      parentSessionID: input.parentSessionID,
      parentAgent: input.parentAgent,
      prompt: input.prompt,
      agent: input.agent,
      status: "running",
      startedAt: new Date(),
    }
    this.delegations.set(id, delegation)
    if (!this.pendingByParent.has(input.parentSessionID)) this.pendingByParent.set(input.parentSessionID, new Set())
    this.pendingByParent.get(input.parentSessionID)?.add(id)

    setTimeout(() => this.timeout(id).catch(() => {}), MAX_RUN_TIME_MS + 5000)

    this.client.session
      .prompt({
        path: { id: sessionID },
        body: {
          agent: input.agent,
          parts: [{ type: "text", text: input.prompt }],
          tools: { delegate: false, delegation_read: false, delegation_list: false, task: false },
        },
      })
      .catch((error: Error) => this.fail(id, error.message).catch(() => {}))

    return delegation
  }

  findBySession(sessionID: string): Delegation | undefined {
    return Array.from(this.delegations.values()).find((delegation) => delegation.sessionID === sessionID)
  }

  async complete(sessionID: string): Promise<void> {
    const delegation = this.findBySession(sessionID)
    if (!delegation || delegation.status !== "running") return
    delegation.status = "complete"
    delegation.completedAt = new Date()
    try {
      const messages = await this.client.session.messages({ path: { id: delegation.sessionID } })
      delegation.result = firstTextFromLastAssistant(messages.data as SessionMessageItem[] | undefined)
    } catch (error) {
      delegation.result = `Delegation completed, but the result could not be read: ${error instanceof Error ? error.message : String(error)}`
    }
    await this.persist(delegation, delegation.result)
    await this.notify(delegation)
  }

  private async fail(id: string, message: string): Promise<void> {
    const delegation = this.delegations.get(id)
    if (!delegation || delegation.status !== "running") return
    delegation.status = "error"
    delegation.error = message
    delegation.completedAt = new Date()
    await this.persist(delegation, `Error: ${message}`)
    await this.notify(delegation)
  }

  private async timeout(id: string): Promise<void> {
    const delegation = this.delegations.get(id)
    if (!delegation || delegation.status !== "running") return
    delegation.status = "timeout"
    delegation.error = `Delegation timed out after ${MAX_RUN_TIME_MS / 1000}s`
    delegation.completedAt = new Date()
    await this.persist(delegation, delegation.error)
    await this.notify(delegation)
  }

  private async persist(delegation: Delegation, content: string): Promise<void> {
    const filePath = await this.outputPath(delegation.parentSessionID, delegation.id)
    const header = [
      `# Delegation ${delegation.id}`,
      "",
      `**ID:** ${delegation.id}`,
      `**Agent:** ${delegation.agent}`,
      `**Status:** ${delegation.status}`,
      `**Started:** ${delegation.startedAt.toISOString()}`,
      `**Completed:** ${delegation.completedAt?.toISOString() || "N/A"}`,
      "",
      "---",
      "",
    ].join("\n")
    await fs.writeFile(filePath, header + content, "utf8")
  }

  private async notify(delegation: Delegation): Promise<void> {
    const pending = this.pendingByParent.get(delegation.parentSessionID)
    pending?.delete(delegation.id)
    const allComplete = !pending || pending.size === 0
    if (allComplete) this.pendingByParent.delete(delegation.parentSessionID)

    await this.client.session.prompt({
      path: { id: delegation.parentSessionID },
      body: {
        noReply: true,
        agent: delegation.parentAgent,
        parts: [{ type: "text", text: `[TASK NOTIFICATION]\nID: ${delegation.id}\nStatus: ${delegation.status}\nUse delegation_read(id) to retrieve the full result.` }],
      },
    })

    if (allComplete) {
      await this.client.session.prompt({
        path: { id: delegation.parentSessionID },
        body: {
          noReply: false,
          agent: delegation.parentAgent,
          parts: [{ type: "text", text: "[TASK NOTIFICATION] All delegations complete." }],
        },
      })
    }
  }

  async read(sessionID: string, id: string): Promise<string> {
    const filePath = await this.outputPath(sessionID, id)
    try {
      return await fs.readFile(filePath, "utf8")
    } catch {
      const running = this.delegations.get(id)
      if (running?.status === "running") return `Delegation ${id} is still running. Wait for the task notification.`
      throw new Error(`Delegation ${JSON.stringify(id)} was not found.`)
    }
  }

  async list(sessionID: string): Promise<string> {
    const dir = await this.directoryFor(sessionID)
    const files = await fs.readdir(dir).catch(() => [])
    const persisted = files.filter((file) => file.endsWith(".md")).map((file) => file.slice(0, -3))
    const memory = Array.from(this.delegations.values()).map((delegation) => `${delegation.id} [${delegation.status}]`)
    const lines = Array.from(new Set([...memory, ...persisted])).sort()
    return lines.length === 0 ? "No delegations found for this session." : `## Delegations\n\n${lines.map((line) => `- ${line}`).join("\n")}`
  }
}

function delegationRules(): string {
  return `<task-notification>
<delegation-system>

You have async background delegation tools:
- delegate(prompt, agent): launch work in a background OpenCode session and return an id immediately.
- delegation_read(id): retrieve the persisted result for a completed delegation.
- delegation_list(): list known delegations for the current session.

Do not poll. Continue useful work after calling delegate; a task notification will arrive when all active delegations complete.
Full outputs are stored on disk and survive compaction.

</delegation-system>
</task-notification>`
}

export const BackgroundAgents: Plugin = async ({ client, directory }) => {
  const typedClient = client as OpencodeClient
  const id = await projectID(directory)
  const baseDir = path.join(os.homedir(), ".local", "share", "opencode", "delegations", id)
  await fs.mkdir(baseDir, { recursive: true })
  const manager = new DelegationManager(typedClient, baseDir)
  log(typedClient, "info", `initialized delegation store at ${baseDir}`)

  return {
    tool: {
      delegate: tool({
        description: "Delegate a task to an OpenCode agent in the background. Returns immediately with an id; use delegation_read(id) after notification.",
        args: {
          prompt: tool.schema.string().describe("Detailed English prompt for the background agent."),
          agent: tool.schema.string().describe("OpenCode agent name to run in the background."),
        },
        async execute(args: { prompt: string; agent: string }, ctx: ToolContext): Promise<string> {
          if (!ctx.sessionID || !ctx.agent) return "delegate requires sessionID and agent context."
          try {
            const delegation = await manager.delegate({
              parentSessionID: ctx.sessionID,
              parentAgent: ctx.agent,
              prompt: args.prompt,
              agent: args.agent,
            })
            return `Delegation started: ${delegation.id}\nAgent: ${args.agent}\nYou will be notified when it completes. Do not poll.`
          } catch (error) {
            return `Delegation failed: ${error instanceof Error ? error.message : String(error)}`
          }
        },
      }),
      delegation_read: tool({
        description: "Read the persisted output of a completed background delegation by id.",
        args: { id: tool.schema.string().describe("Delegation id returned by delegate().") },
        async execute(args: { id: string }, ctx: ToolContext): Promise<string> {
          if (!ctx.sessionID) return "delegation_read requires sessionID context."
          return await manager.read(ctx.sessionID, args.id)
        },
      }),
      delegation_list: tool({
        description: "List background delegations for the current session.",
        args: {},
        async execute(_args: Record<string, never>, ctx: ToolContext): Promise<string> {
          if (!ctx.sessionID) return "delegation_list requires sessionID context."
          return await manager.list(ctx.sessionID)
        },
      }),
    },
    "experimental.chat.system.transform": async (_input: unknown, output: { system: string[] }) => {
      output.system = [[...output.system, delegationRules()].join("\n\n---\n\n")]
    },
    "experimental.session.compacting": async (_input: unknown, output: { context: string[] }) => {
      output.context.push("Background delegation results can be recovered with delegation_list() and delegation_read(id).")
    },
    event: async ({ event }: { event: { type?: string; properties?: Record<string, any> } }): Promise<void> => {
      if (event.type === "session.idle") {
        const sessionID = event.properties?.sessionID
        if (typeof sessionID === "string") await manager.complete(sessionID)
      }
    },
  }
}

export default BackgroundAgents
