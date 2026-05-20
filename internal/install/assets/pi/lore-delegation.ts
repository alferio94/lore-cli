import type { ExtensionAPI, ExtensionCommandContext, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { type SelectItem, SelectList, Text, matchesKey, truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";
import { Type } from "typebox";
import { spawn, type ChildProcessWithoutNullStreams } from "node:child_process";
import { createHash } from "node:crypto";
import { promises as fs } from "node:fs";
import os from "node:os";
import path from "node:path";

const PI_BIN = process.env.PI_BIN ?? "pi";
const AGENT_DIR = path.join(os.homedir(), ".pi", "agent");
const LORE_MEMORY_EXTENSION = path.join(AGENT_DIR, "extensions", "lore-memory.ts");
const LORE_DELEGATION_EXTENSION = path.join(AGENT_DIR, "extensions", "lore-delegation.ts");
const BASE_DIR = path.join(os.homedir(), ".local", "share", "pi", "delegations");
const SDD_MODELS_FILE = path.join(AGENT_DIR, "sdd-models.json");
const SETTINGS_FILE = path.join(AGENT_DIR, "settings.json");

const SDD_PHASES = [
  "sdd-init",
  "sdd-explore",
  "sdd-propose",
  "sdd-spec",
  "sdd-design",
  "sdd-tasks",
  "sdd-apply",
  "sdd-verify",
  "sdd-archive",
] as const;

const THINKING_LEVELS = ["off", "minimal", "low", "medium", "high", "xhigh"] as const;

type ThinkingLevel = (typeof THINKING_LEVELS)[number];

interface ModelChoice {
  model?: string;
  thinking?: ThinkingLevel;
}

interface SddModelsConfig {
  default?: ModelChoice;
  agents?: Record<string, ModelChoice>;
  nonSddDefault?: ModelChoice;
  workers?: Record<string, ModelChoice>;
}

interface PiSettingsConfig {
  defaultProvider?: string;
  defaultModel?: string;
  defaultThinkingLevel?: ThinkingLevel;
  [key: string]: unknown;
}

type LoreModelsContext = ExtensionContext & Partial<Pick<ExtensionCommandContext, "waitForIdle">>;

type LoreWorkerSpecialization = "general" | "research" | "review" | "docs";

type DelegationKind = "sdd" | "lore-worker";

interface DelegationRoute {
  requestedAgent: string;
  canonicalAgent: string;
  kind: DelegationKind;
  specialization?: LoreWorkerSpecialization;
  normalizationNote?: string;
}

interface LoreWorkerEnvelope {
  status: "completed" | "running" | "needs_user_input" | "failed";
  kind: "lore-worker";
  specialization: LoreWorkerSpecialization;
  summary: string;
  artifacts: string[];
  memory_saved: string[];
  next: string | null;
  risks: string[];
}

interface SddDelegationEnvelope {
  status: "completed" | "running" | "needs_user_input" | "failed";
  phase: "init" | "explore" | "proposal" | "spec" | "design" | "tasks" | "apply" | "verify" | "archive";
  summary: string;
  artifacts: string[];
  next: string | null;
  question: string | null;
  options: string[];
  risks: string[];
  skill_resolution: "injected" | "fallback-registry" | "fallback-path" | "none";
}

type DelegationEnvelope = LoreWorkerEnvelope | SddDelegationEnvelope;

type DelegationStatus = "running" | "completed" | "error" | "timeout" | "cancelled";

interface Delegation {
  id: string;
  agent: string;
  requestedAgent: string;
  kind: DelegationKind;
  specialization?: LoreWorkerSpecialization;
  normalizationNote?: string;
  prompt: string;
  status: DelegationStatus;
  sessionId: string;
  cwd: string;
  startedAt: string;
  completedAt?: string;
  exitCode?: number | null;
  signal?: string | null;
  outputFile: string;
  traceFile: string;
  modelRef: string;
  thinking?: ThinkingLevel;
  liveStdout?: string;
  liveStderr?: string;
  liveTrace?: string;
  error?: string;
  envelope?: DelegationEnvelope;
  child?: ChildProcessWithoutNullStreams;
}

function textResult(text: string, details: Record<string, unknown> = {}) {
  return { content: [{ type: "text" as const, text }], details };
}

function sessionId(ctx: ExtensionContext): string {
  return ctx.sessionManager.getSessionId?.() ?? "ephemeral";
}

function projectId(cwd: string): string {
  return createHash("sha256").update(cwd).digest("hex").slice(0, 16);
}

function randomId(): string {
  const left = Math.random().toString(36).slice(2, 7);
  const right = Math.random().toString(36).slice(2, 7);
  return `dg-${left}-${right}`;
}

function compact(text: string, max = 500): string {
  const clean = text.trim().replace(/\s+/g, " ");
  return clean.length > max ? `${clean.slice(0, max)}…` : clean;
}

function compactJson(value: unknown, max = 1000): string {
  try { return compact(JSON.stringify(value), max); } catch { return compact(String(value), max); }
}

async function appendChildTrace(type: string, details: Record<string, unknown> = {}) {
  const traceFile = process.env.PI_DELEGATION_TRACE;
  if (!traceFile) return;
  const record = { ts: new Date().toISOString(), type, ...details };
  await fs.appendFile(traceFile, `${JSON.stringify(record)}\n`, "utf8").catch(() => {});
}

function formatTraceLine(raw: string): string | undefined {
  try {
    const record = JSON.parse(raw) as Record<string, unknown>;
    const ts = typeof record.ts === "string" ? record.ts.slice(11, 19) : "--:--:--";
    const type = typeof record.type === "string" ? record.type : "event";
    const tool = typeof record.toolName === "string" ? ` ${record.toolName}` : "";
    const status = typeof record.status === "string" ? ` ${record.status}` : "";
    const skills = Array.isArray(record.skills) && record.skills.length > 0 ? ` — skills: ${record.skills.join(", ")}` : "";
    const summary = typeof record.summary === "string" ? ` — ${record.summary}` : "";
    return `${ts} ${type}${tool}${status}${skills}${summary}`;
  } catch {
    return raw.trim() || undefined;
  }
}

function summarizeLoadedSkills(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  const names = value.map((item) => {
    if (typeof item === "string") return item;
    if (!item || typeof item !== "object") return undefined;
    const data = item as Record<string, unknown>;
    const raw = data.name ?? data.id ?? data.title ?? data.command ?? data.path ?? data.file ?? data.location;
    if (typeof raw !== "string" || !raw.trim()) return undefined;
    const trimmed = raw.trim();
    const match = trimmed.match(/([^/\\]+)\/SKILL\.md$/i);
    return match?.[1] ?? trimmed.replace(/^\/skill:/, "");
  }).filter((item): item is string => Boolean(item));
  return Array.from(new Set(names));
}

function extractUsageSummary(rawTrace: string) {
  let input = 0;
  let output = 0;
  let total = 0;
  let model = "";
  for (const line of rawTrace.split(/\r?\n/)) {
    if (!line.trim()) continue;
    try {
      const record = JSON.parse(line) as Record<string, unknown>;
      if (record.type !== "token_usage") continue;
      input += typeof record.input === "number" ? record.input : 0;
      output += typeof record.output === "number" ? record.output : 0;
      total += typeof record.totalTokens === "number" ? record.totalTokens : 0;
      if (typeof record.model === "string" && record.model) model = record.model;
    } catch {}
  }
  return { input, output, totalTokens: total || input + output, model };
}

function tailLines(text: string, count: number): string[] {
  const lines = text.split(/\r?\n/).filter((line) => line.trim().length > 0);
  return lines.slice(Math.max(0, lines.length - count));
}

function toStringArray(value: unknown): string[] {
  if (!Array.isArray(value)) return [];
  return value.filter((item): item is string => typeof item === "string").map((item) => item.trim()).filter(Boolean);
}

function isEnvelopeStatus(value: unknown): value is LoreWorkerEnvelope["status"] {
  return value === "completed" || value === "running" || value === "needs_user_input" || value === "failed";
}

function normalizeLoreWorkerEnvelope(value: unknown, route: DelegationRoute): LoreWorkerEnvelope | undefined {
  if (!value || typeof value !== "object") return undefined;
  const data = value as Record<string, unknown>;
  if (!isEnvelopeStatus(data.status)) return undefined;
  if (data.kind !== "lore-worker") return undefined;
  if (typeof data.summary !== "string") return undefined;
  const specialization = data.specialization;
  if (specialization !== "general" && specialization !== "research" && specialization !== "review" && specialization !== "docs") {
    return undefined;
  }

  return {
    status: data.status,
    kind: "lore-worker",
    specialization,
    summary: data.summary.trim(),
    artifacts: toStringArray(data.artifacts),
    memory_saved: toStringArray(data.memory_saved),
    next: typeof data.next === "string" ? data.next.trim() || null : null,
    risks: toStringArray(data.risks),
  };
}

function normalizeSddDelegationEnvelope(value: unknown): SddDelegationEnvelope | undefined {
  if (!value || typeof value !== "object") return undefined;
  const data = value as Record<string, unknown>;
  if (!isEnvelopeStatus(data.status)) return undefined;
  if (typeof data.summary !== "string") return undefined;
  if (typeof data.phase !== "string") return undefined;
  const validPhases = ["init", "explore", "proposal", "spec", "design", "tasks", "apply", "verify", "archive"] as const;
  if (!validPhases.includes(data.phase as (typeof validPhases)[number])) return undefined;
  const skillResolution = data.skill_resolution;
  const validSkillResolutions = ["injected", "fallback-registry", "fallback-path", "none"] as const;
  if (typeof skillResolution !== "string" || !validSkillResolutions.includes(skillResolution as (typeof validSkillResolutions)[number])) {
    return undefined;
  }

  return {
    status: data.status,
    phase: data.phase as SddDelegationEnvelope["phase"],
    summary: data.summary.trim(),
    artifacts: toStringArray(data.artifacts),
    next: typeof data.next === "string" ? data.next.trim() || null : null,
    question: typeof data.question === "string" ? data.question.trim() || null : null,
    options: toStringArray(data.options),
    risks: toStringArray(data.risks),
    skill_resolution: skillResolution as SddDelegationEnvelope["skill_resolution"],
  };
}

function extractEnvelopeJson(text: string): string | undefined {
  const trimmed = text.trim();
  if (!trimmed) return undefined;
  if (trimmed.startsWith("{") && trimmed.endsWith("}")) return trimmed;

  const fenced = trimmed.match(/```json\s*([\s\S]*?)```/i) ?? trimmed.match(/```\s*([\s\S]*?)```/i);
  if (fenced?.[1]) return fenced[1].trim();

  const firstBrace = trimmed.indexOf("{");
  const lastBrace = trimmed.lastIndexOf("}");
  if (firstBrace >= 0 && lastBrace > firstBrace) return trimmed.slice(firstBrace, lastBrace + 1);
  return undefined;
}

function parseDelegationEnvelope(text: string, route: DelegationRoute): DelegationEnvelope | undefined {
  const json = extractEnvelopeJson(text);
  if (!json) return undefined;
  try {
    const parsed = JSON.parse(json);
    return route.kind === "sdd"
      ? normalizeSddDelegationEnvelope(parsed)
      : normalizeLoreWorkerEnvelope(parsed, route);
  } catch {
    return undefined;
  }
}

function envelopePreview(envelope: DelegationEnvelope, max = 120): string {
  const label = "phase" in envelope ? `${envelope.phase}/${envelope.status}` : `${envelope.specialization}/${envelope.status}`;
  return compact(`${label}: ${envelope.summary}`, max);
}

function envelopeMarkdown(envelope: DelegationEnvelope): string {
  return [
    "## Envelope",
    "",
    "```json",
    JSON.stringify(envelope, null, 2),
    "```",
    "",
    "## Summary",
    "",
    `- status: ${envelope.status}`,
    `- summary: ${envelope.summary}`,
    `- artifacts: ${envelope.artifacts.length > 0 ? envelope.artifacts.join("; ") : "(none)"}`,
    "phase" in envelope
      ? `- next: ${envelope.next || "(none)"}`
      : `- memory_saved: ${envelope.memory_saved.length > 0 ? envelope.memory_saved.join("; ") : "(none)"}`,
    `- risks: ${envelope.risks.length > 0 ? envelope.risks.join("; ") : "(none)"}`,
    "phase" in envelope
      ? `- question: ${envelope.question || "(none)"}`
      : `- next: ${envelope.next || "(none)"}`,
    "phase" in envelope
      ? `- skill_resolution: ${envelope.skill_resolution}`
      : `- specialization: ${envelope.specialization}`,
    "",
  ].join("\n");
}

async function readJsonFile<T>(file: string, fallback: T): Promise<T> {
  try {
    return JSON.parse(await fs.readFile(file, "utf8")) as T;
  } catch {
    return fallback;
  }
}

async function writeJsonFile(file: string, data: unknown) {
  await fs.mkdir(path.dirname(file), { recursive: true });
  await fs.writeFile(file, `${JSON.stringify(data, null, 2)}\n`, "utf8");
}

function normalizeThinking(value: unknown): ThinkingLevel | undefined {
  return THINKING_LEVELS.includes(value as ThinkingLevel) ? (value as ThinkingLevel) : undefined;
}

function formatDelegationAgent(agent: string, specialization?: LoreWorkerSpecialization): string {
  return specialization ? `${agent}/${specialization}` : agent;
}

function normalizeDelegationRoute(inputAgent?: string): DelegationRoute {
  const requested = inputAgent?.trim() || "lore-worker";
  const normalized = requested.toLowerCase();

  if (normalized.startsWith("sdd-")) {
    if (!SDD_PHASES.includes(normalized as (typeof SDD_PHASES)[number])) {
      throw new Error(`Unknown SDD agent: ${requested}`);
    }
    return {
      requestedAgent: requested,
      canonicalAgent: normalized,
      kind: "sdd",
    };
  }

  const aliases: Record<string, LoreWorkerSpecialization> = {
    "lore-worker": "general",
    general: "general",
    explore: "research",
    research: "research",
    researcher: "research",
    review: "review",
    reviewer: "review",
    scribe: "docs",
    docs: "docs",
  };

  const specialization = aliases[normalized];
  if (specialization) {
    return {
      requestedAgent: requested,
      canonicalAgent: "lore-worker",
      kind: "lore-worker",
      specialization,
      normalizationNote: normalized === "lore-worker" ? undefined : `Alias '${requested}' normalized to lore-worker/${specialization}`,
    };
  }

  return {
    requestedAgent: requested,
    canonicalAgent: "lore-worker",
    kind: "lore-worker",
    specialization: "general",
    normalizationNote: `Unknown non-SDD agent '${requested}' normalized to lore-worker/general`,
  };
}

async function loadSddModelsConfig(): Promise<SddModelsConfig> {
  const config = await readJsonFile<SddModelsConfig>(SDD_MODELS_FILE, {});
  return {
    default: config.default ?? {},
    agents: config.agents ?? {},
    nonSddDefault: config.nonSddDefault ?? {},
    workers: config.workers ?? {},
  };
}

async function saveSddModelsConfig(config: SddModelsConfig) {
  await writeJsonFile(SDD_MODELS_FILE, {
    default: config.default ?? {},
    agents: config.agents ?? {},
    nonSddDefault: config.nonSddDefault ?? {},
    workers: config.workers ?? {},
  });
}

async function loadPiSettings(): Promise<PiSettingsConfig> {
  return readJsonFile<PiSettingsConfig>(SETTINGS_FILE, {});
}

function hasModelRef(value: unknown): value is string {
  return typeof value === "string" && value.trim().length > 0;
}

function settingsDefaultModelRef(settings: PiSettingsConfig): string | undefined {
  return hasModelRef(settings.defaultProvider) && hasModelRef(settings.defaultModel)
    ? `${settings.defaultProvider}/${settings.defaultModel}`
    : undefined;
}

function activeMainModelRef(ctx: ExtensionContext, settings: PiSettingsConfig): string | undefined {
  if (ctx.model) return `${ctx.model.provider}/${ctx.model.id}`;
  return settingsDefaultModelRef(settings);
}

interface AvailableModelDescriptor {
  provider: string;
  id: string;
  name?: string;
  reasoning?: boolean;
}

async function getAvailableModelsSafe(ctx: { modelRegistry?: { getAvailable?: (() => unknown) | undefined } | undefined }): Promise<AvailableModelDescriptor[]> {
  const registry = ctx.modelRegistry;
  if (!registry?.getAvailable) return [];
  try {
    const available = await Promise.resolve(registry.getAvailable.call(registry));
    return Array.isArray(available) ? available as AvailableModelDescriptor[] : [];
  } catch {
    return [];
  }
}

function findModelSafe(ctx: { modelRegistry?: { find?: ((provider: string, id: string) => unknown) | undefined } | undefined }, provider: string, id: string): any {
  const registry = ctx.modelRegistry;
  if (!registry?.find) return undefined;
  try {
    return registry.find.call(registry, provider, id);
  } catch {
    return undefined;
  }
}

function hasConfiguredDelegationModels(ctx: ExtensionContext, settings: PiSettingsConfig, config: SddModelsConfig): boolean {
  if (activeMainModelRef(ctx, settings)) return true;
  if (hasModelRef(config.default?.model) || hasModelRef(config.nonSddDefault?.model)) return true;
  if (Object.values(config.agents ?? {}).some((choice) => hasModelRef(choice?.model))) return true;
  return Object.values(config.workers ?? {}).some((choice) => hasModelRef(choice?.model));
}

function splitModelRef(ref: string): { provider: string; id: string } | undefined {
  const slash = ref.indexOf("/");
  if (slash <= 0 || slash === ref.length - 1) return undefined;
  return { provider: ref.slice(0, slash), id: ref.slice(slash + 1) };
}

async function resolveDelegationModel(route: DelegationRoute): Promise<ModelChoice | undefined> {
  const config = await loadSddModelsConfig();
  const choice = route.kind === "sdd"
    ? (config.agents?.[route.canonicalAgent] ?? config.default)
    : (config.workers?.[formatDelegationAgent(route.canonicalAgent, route.specialization)] ?? config.nonSddDefault);
  if (!choice?.model) return undefined;
  return { model: choice.model, thinking: normalizeThinking(choice.thinking) };
}

function agentInstructions(route: DelegationRoute): string {
  if (route.kind === "sdd") {
    return `You are an SDD phase executor (${route.canonicalAgent}). Load the relevant skill from ~/.pi/agent/skills/${route.canonicalAgent}/SKILL.md plus ~/.pi/agent/skills/_shared/sdd-phase-common.md. Persist durable artifacts to Lore when lore_* tools are available. Return a compact operational summary.`;
  }

  switch (route.specialization ?? "general") {
    case "research":
      return "You are Lore worker (research specialization). Investigate thoroughly, read relevant files, avoid code mutation unless explicitly requested, and return concise findings with file references.";
    case "docs":
      return "You are Lore worker (docs specialization). Produce clear written artifacts, summaries, changelog notes, or commit/PR support. Avoid code mutation unless explicitly requested.";
    case "review":
      return "You are Lore worker (review specialization). Look for correctness, security, maintainability, and test gaps. Return prioritized findings with evidence.";
    default:
      return "You are Lore worker (general specialization). Complete the delegated task directly. Use Lore memory tools for durable discoveries when relevant. Do not launch further delegations. Return concise results with paths and evidence.";
  }
}

function buildPrompt(input: { route: DelegationRoute; prompt: string; cwd: string; parentSession: string; id: string }) {
  const specializationLine = input.route.kind === "lore-worker"
    ? `Specialization: ${input.route.specialization ?? "general"}`
    : "";
  const normalizationLine = input.route.normalizationNote ? `Normalization: ${input.route.normalizationNote}` : "";
  const envelopeContract = input.route.kind === "sdd"
    ? `- Return exactly this object shape:\n  {\n    "status": "completed" | "running" | "needs_user_input" | "failed",\n    "phase": "init" | "explore" | "proposal" | "spec" | "design" | "tasks" | "apply" | "verify" | "archive",\n    "summary": string,\n    "artifacts": string[],\n    "next": string | null,\n    "question": string | null,\n    "options": string[],\n    "risks": string[],\n    "skill_resolution": "injected" | "fallback-registry" | "fallback-path" | "none"\n  }\n- Keep the SDD envelope compact. Do not inline full artifact bodies.`
    : `- If you make important discoveries, decisions, or fixes, you MUST call lore_save before returning.\n- If Lore persistence fails, mention that explicitly in \"risks\" and leave \"memory_saved\" empty.\n- Return exactly this object shape:\n  {\n    "status": "completed" | "running" | "needs_user_input" | "failed",\n    "kind": "lore-worker",\n    "specialization": "general" | "research" | "review" | "docs",\n    "summary": string,\n    "artifacts": string[],\n    "memory_saved": string[],\n    "next": string | null,\n    "risks": string[]\n  }`;
  return `You are a background delegated Pi agent.\n\nDelegation ID: ${input.id}\nParent session: ${input.parentSession}\nWorking directory: ${input.cwd}\nRequested agent: ${input.route.requestedAgent}\nCanonical agent: ${input.route.canonicalAgent}\n${specializationLine}\n${normalizationLine}\n\n${agentInstructions(input.route)}\n\nRules:\n- Work independently and do not ask the user questions unless blocked.\n- Do not call delegate/delegation tools.\n- Use Lore memory tools for durable discoveries or SDD artifacts when relevant.\n- Persisted technical artifacts must be in English.\n- Your final answer must be valid JSON only. No markdown, no code fences, no prose before or after the JSON.\n${envelopeContract}\n- Keep \"summary\" concise and high signal.\n- Use \"artifacts\" for files, Lore artifacts, or other durable outputs.\n- Use an empty array when there are no artifacts or risks.\n\nTask:\n${input.prompt}`;
}

class DelegationManager {
  private delegations = new Map<string, Delegation>();
  private lastCtx: ExtensionContext | undefined;
  onComplete?: (delegation: Delegation, preview: string) => void;

  setContext(ctx: ExtensionContext) {
    this.lastCtx = ctx;
    this.updateStatus(ctx);
  }

  private counts() {
    let running = 0;
    let completed = 0;
    let error = 0;
    for (const d of this.delegations.values()) {
      if (d.status === "running") running++;
      else if (d.status === "completed") completed++;
      else error++;
    }
    return { running, completed, error };
  }

  updateStatus(ctx = this.lastCtx) {
    if (!ctx?.hasUI) return;
    const c = this.counts();
    ctx.ui.setStatus("subagents", `subagents ${c.running}⏳ ${c.completed}✓ ${c.error}✗`);
  }

  private async dirFor(ctx: ExtensionContext) {
    const dir = path.join(BASE_DIR, projectId(ctx.cwd), sessionId(ctx));
    await fs.mkdir(dir, { recursive: true });
    return dir;
  }

  private async writeOutput(d: Delegation, output: string, stderr: string) {
    const envelopeSection = d.envelope
      ? `${envelopeMarkdown(d.envelope)}\n`
      : "## Envelope\n\nStructured envelope missing or invalid. See raw output below.\n\n";
    const body = `# Delegation ${d.id}\n\n` +
      `**Requested Agent:** ${d.requestedAgent}\n` +
      `**Agent:** ${formatDelegationAgent(d.agent, d.specialization)}\n` +
      `**Model:** ${d.modelRef}${d.thinking ? ` · thinking=${d.thinking}` : ""}\n` +
      `**Kind:** ${d.kind}\n` +
      (d.normalizationNote ? `**Normalization:** ${d.normalizationNote}\n` : "") +
      `**Status:** ${d.status}\n` +
      `**Started:** ${d.startedAt}\n` +
      `**Completed:** ${d.completedAt ?? "N/A"}\n` +
      `**Exit Code:** ${d.exitCode ?? "N/A"}\n\n` +
      `## Prompt\n\n${d.prompt}\n\n` +
      envelopeSection +
      `## Raw Output\n\n${output.trim() || "(no stdout)"}\n\n` +
      (stderr.trim() ? `## Stderr\n\n\`\`\`text\n${stderr.trim()}\n\`\`\`\n` : "");
    await fs.writeFile(d.outputFile, body, "utf8");
  }

  async delegate(ctx: ExtensionContext, input: { prompt: string; agent?: string }) {
    this.setContext(ctx);
    const dir = await this.dirFor(ctx);
    let id = randomId();
    while (this.delegations.has(id)) id = randomId();

    const route = normalizeDelegationRoute(input.agent);
    const modelChoice = await resolveDelegationModel(route);
    const settings = await loadPiSettings();
    const fallbackModel = modelChoice?.model ?? activeMainModelRef(ctx, settings);
    const fallbackThinking = modelChoice?.thinking ?? pi.getThinkingLevel() ?? normalizeThinking(settings.defaultThinkingLevel);
    if (!fallbackModel) {
      const availableModels = await getAvailableModelsSafe(ctx);
      if (availableModels.length === 0) {
        throw new Error("No available Pi models detected for delegation. Configure/login to a provider and API key, then use /lore-models once models appear.");
      }
      const routingTarget = route.kind === "sdd" ? `${route.canonicalAgent} or default-sdd` : `${formatDelegationAgent(route.canonicalAgent, route.specialization)} or default-non-sdd`;
      throw new Error(`No model configured for delegation. Use /lore-models to set ${routingTarget}, or choose a main default model first.`);
    }
    const outputFile = path.join(dir, `${id}.md`);
    const traceFile = path.join(dir, `${id}.jsonl`);
    await fs.writeFile(traceFile, "", "utf8").catch(() => {});
    const d: Delegation = {
      id,
      agent: route.canonicalAgent,
      requestedAgent: route.requestedAgent,
      kind: route.kind,
      specialization: route.specialization,
      normalizationNote: route.normalizationNote,
      prompt: input.prompt,
      status: "running",
      sessionId: sessionId(ctx),
      cwd: ctx.cwd,
      startedAt: new Date().toISOString(),
      outputFile,
      traceFile,
      modelRef: fallbackModel,
      thinking: fallbackThinking,
    };
    this.delegations.set(id, d);
    this.updateStatus(ctx);

    const childPrompt = buildPrompt({
      id,
      route,
      prompt: d.prompt,
      cwd: d.cwd,
      parentSession: d.sessionId,
    });

    const args = [
      "--print",
      "--no-session",
      "--no-extensions",
      "--extension",
      LORE_MEMORY_EXTENSION,
      "--extension",
      LORE_DELEGATION_EXTENSION,
    ];

    args.push("--model", fallbackModel);
    if (fallbackThinking) args.push("--thinking", fallbackThinking);
    args.push(childPrompt);

    const child = spawn(PI_BIN, args, {
      cwd: d.cwd,
      env: { ...process.env, PI_DELEGATION_CHILD: "1", PI_DELEGATION_TRACE: traceFile },
      stdio: ["ignore", "pipe", "pipe"],
    });
    d.child = child;

    let stdout = "";
    let stderr = "";
    const refreshTrace = async () => {
      const raw = await fs.readFile(traceFile, "utf8").catch(() => "");
      d.liveTrace = tailLines(raw, 200).map((line) => formatTraceLine(line)).filter((line): line is string => Boolean(line)).join("\n");
    };
    child.stdout.on("data", (chunk) => {
      stdout += chunk.toString();
      d.liveStdout = stdout;
    });
    child.stderr.on("data", (chunk) => {
      stderr += chunk.toString();
      d.liveStderr = stderr;
    });
    child.on("error", async (err) => {
      d.status = "error";
      d.error = err.message;
      d.envelope = parseDelegationEnvelope(stdout, route);
      d.completedAt = new Date().toISOString();
      await refreshTrace();
      await this.writeOutput(d, stdout, `${stderr}\n${err.stack ?? err.message}`);
      this.updateStatus();
    });
    child.on("close", async (code, signal) => {
      if (d.status !== "running") return;
      d.exitCode = code;
      d.signal = signal;
      d.envelope = parseDelegationEnvelope(stdout, route);
      if (code !== 0) {
        d.status = "error";
      } else if (!d.envelope) {
        d.status = "error";
        d.error = `Invalid ${route.kind} delegation envelope`;
      } else if (d.envelope.status === "failed") {
        d.status = "error";
      } else {
        d.status = d.envelope.status === "running" ? "running" : "completed";
      }
      d.completedAt = new Date().toISOString();
      await refreshTrace();
      await this.writeOutput(d, stdout, stderr);
      this.updateStatus();
      const suffix = d.envelope
        ? envelopePreview(d.envelope, 120)
        : compact(stderr || stdout, 120);
      this.onComplete?.(d, suffix);
      if (this.lastCtx?.hasUI) {
        this.lastCtx.ui.notify(`Delegation ${d.id} ${d.status}: ${suffix}`, d.status === "completed" ? "info" : "error");
      }
    });

    return d;
  }

  async list(ctx: ExtensionContext) {
    this.setContext(ctx);
    const items: Array<Pick<Delegation, "id" | "agent" | "status" | "startedAt" | "completedAt" | "outputFile" | "traceFile" | "modelRef"> & { summary?: string; specialization?: LoreWorkerSpecialization; thinking?: ThinkingLevel }> = [];
    for (const d of this.delegations.values()) {
      items.push({
        id: d.id,
        agent: d.agent,
        specialization: d.specialization,
        status: d.status,
        startedAt: d.startedAt,
        completedAt: d.completedAt,
        outputFile: d.outputFile,
        traceFile: d.traceFile,
        modelRef: d.modelRef,
        thinking: d.thinking,
        summary: d.error,
      });
    }
    try {
      const dir = await this.dirFor(ctx);
      const files = await fs.readdir(dir);
      for (const file of files.filter((f) => f.endsWith(".md"))) {
        const id = file.replace(/\.md$/, "");
        if (items.some((i) => i.id === id)) continue;
        const full = path.join(dir, file);
        const content = await fs.readFile(full, "utf8").catch(() => "");
        const status = (content.match(/^\*\*Status:\*\*\s*(.+)$/m)?.[1] as DelegationStatus | undefined) ?? "completed";
        const agent = content.match(/^\*\*Agent:\*\*\s*(.+)$/m)?.[1] ?? "unknown";
        const modelRef = content.match(/^\*\*Model:\*\*\s*(.+)$/m)?.[1] ?? "unknown";
        items.push({ id, agent, status, startedAt: "", outputFile: full, traceFile: path.join(dir, `${id}.jsonl`), modelRef, summary: compact(content, 160) });
      }
    } catch {}
    return items;
  }

  async read(ctx: ExtensionContext, id: string) {
    this.setContext(ctx);
    const d = this.delegations.get(id);
    if (d) {
      try { return await fs.readFile(d.outputFile, "utf8"); } catch {
        return `Delegation ${id} is ${d.status}. Output file not available yet: ${d.outputFile}`;
      }
    }
    const dir = await this.dirFor(ctx);
    const file = path.join(dir, `${id}.md`);
    return fs.readFile(file, "utf8");
  }

  async liveSnapshot(ctx: ExtensionContext, id: string) {
    this.setContext(ctx);
    const d = this.delegations.get(id);
    const dir = await this.dirFor(ctx);
    const traceFile = d?.traceFile ?? path.join(dir, `${id}.jsonl`);
    const rawTrace = await fs.readFile(traceFile, "utf8").catch(() => "");
    const trace = tailLines(rawTrace, 80).map((line) => formatTraceLine(line)).filter((line): line is string => Boolean(line)).join("\n");
    const usage = extractUsageSummary(rawTrace);
    if (d) {
      d.liveTrace = trace;
      return {
        id: d.id,
        agent: formatDelegationAgent(d.agent, d.specialization),
        status: d.status,
        startedAt: d.startedAt,
        completedAt: d.completedAt,
        trace,
        stdout: d.liveStdout ?? "",
        stderr: d.liveStderr ?? "",
        outputFile: d.outputFile,
        modelRef: usage.model || d.modelRef,
        usage,
      };
    }

    const outputFile = path.join(dir, `${id}.md`);
    const content = await fs.readFile(outputFile, "utf8").catch(() => "");
    const status = content.match(/^\*\*Status:\*\*\s*(.+)$/m)?.[1] ?? "completed";
    const agent = content.match(/^\*\*Agent:\*\*\s*(.+)$/m)?.[1] ?? "unknown";
    const modelRef = usage.model || content.match(/^\*\*Model:\*\*\s*(.+)$/m)?.[1] || "unknown";
    return { id, agent, status, startedAt: "", completedAt: "", trace, stdout: content, stderr: "", outputFile, modelRef, usage };
  }
}

const manager = new DelegationManager();

type DelegationListItem = Awaited<ReturnType<DelegationManager["list"]>>[number];
type LiveSnapshot = Awaited<ReturnType<DelegationManager["liveSnapshot"]>>;

async function showLiveDelegationOverlay(ctx: ExtensionContext, id: string) {
  let snapshot: LiveSnapshot | undefined = await manager.liveSnapshot(ctx, id);
  let timer: NodeJS.Timeout | undefined;

  await ctx.ui.custom<void>((tui, theme, _keybindings, done) => {
    let closed = false;
    let scrollFromBottom = 0;
    let lastBodySize = 0;
    const refresh = async () => {
      if (closed) return;
      snapshot = await manager.liveSnapshot(ctx, id).catch(() => snapshot);
      tui.requestRender();
    };
    timer = setInterval(refresh, 900);

    const padLine = (line: string, width: number) => {
      const padding = Math.max(0, width - visibleWidth(line));
      return line + " ".repeat(padding);
    };
    const bordered = (line: string, innerWidth: number) =>
      theme.fg("accent", "│") + padLine(truncateToWidth(line, innerWidth), innerWidth) + theme.fg("accent", "│");

    return {
      render(width: number) {
        const innerWidth = Math.max(30, width - 2);
        const s = snapshot;
        const lines: string[] = [];
        lines.push(theme.fg("accent", `╭${"─".repeat(innerWidth)}╮`));
        lines.push(bordered(theme.bold(`Delegation ${id}`), innerWidth));
        lines.push(bordered(s ? `${s.agent} · ${s.status}` : "Loading…", innerWidth));
        if (s?.modelRef) lines.push(bordered(`model: ${s.modelRef}`, innerWidth));
        if (s?.usage && (s.usage.input > 0 || s.usage.output > 0)) {
          lines.push(bordered(`tokens: input=${s.usage.input} output=${s.usage.output} total=${s.usage.totalTokens}`, innerWidth));
        }
        if (s?.outputFile) lines.push(bordered(theme.fg("dim", s.outputFile), innerWidth));
        const body: string[] = [];
        body.push("");
        body.push(theme.fg("muted", "Live trace"));
        const trace = s?.trace?.trim() ? s.trace.split(/\r?\n/).filter(Boolean) : ["(sin eventos todavía; esperando al agente)"];
        body.push(...trace);
        if (s?.stderr?.trim()) {
          body.push("");
          body.push(theme.fg("warning", "stderr"));
          body.push(...s.stderr.split(/\r?\n/).filter(Boolean));
        }
        if (s?.stdout?.trim()) {
          body.push("");
          body.push(theme.fg("muted", s.status === "running" ? "stdout parcial" : "resultado"));
          body.push(...s.stdout.split(/\r?\n/).filter(Boolean));
        }
        lastBodySize = body.length;
        const visibleBodyLines = 28;
        const maxScroll = Math.max(0, body.length - visibleBodyLines);
        scrollFromBottom = Math.min(scrollFromBottom, maxScroll);
        const start = Math.max(0, body.length - visibleBodyLines - scrollFromBottom);
        const visibleBody = body.slice(start, start + visibleBodyLines);
        for (const line of visibleBody) lines.push(bordered(line, innerWidth));
        lines.push(bordered("", innerWidth));
        const scrollLabel = maxScroll > 0 ? ` • scroll ${maxScroll - scrollFromBottom}/${maxScroll}` : "";
        lines.push(bordered(theme.fg("dim", `↑↓/jk scroll • pgup/pgdn rápido • esc/q cerrar${scrollLabel}`), innerWidth));
        lines.push(theme.fg("accent", `╰${"─".repeat(innerWidth)}╯`));
        return lines;
      },
      handleInput(data: string) {
        const maxScroll = Math.max(0, lastBodySize - 28);
        if (matchesKey(data, "escape") || matchesKey(data, "q")) {
          closed = true;
          if (timer) clearInterval(timer);
          done();
        } else if (matchesKey(data, "up") || matchesKey(data, "k")) {
          scrollFromBottom = Math.min(maxScroll, scrollFromBottom + 1);
          tui.requestRender();
        } else if (matchesKey(data, "down") || matchesKey(data, "j")) {
          scrollFromBottom = Math.max(0, scrollFromBottom - 1);
          tui.requestRender();
        } else if (matchesKey(data, "pageup")) {
          scrollFromBottom = Math.min(maxScroll, scrollFromBottom + 10);
          tui.requestRender();
        } else if (matchesKey(data, "pagedown")) {
          scrollFromBottom = Math.max(0, scrollFromBottom - 10);
          tui.requestRender();
        }
      },
      invalidate() {},
    };
  }, {
    overlay: true,
    overlayOptions: {
      anchor: "right-center",
      width: "72%",
      minWidth: 72,
      maxHeight: "85%",
      margin: 1,
    },
  });

  if (timer) clearInterval(timer);
}

function formatSubagentTimestamp(item: DelegationListItem): string {
  const raw = item.completedAt || item.startedAt;
  if (!raw) return "time unknown";
  const when = new Date(raw);
  if (Number.isNaN(when.getTime())) return raw;
  const date = when.toLocaleDateString(undefined, { month: "short", day: "numeric" });
  const time = when.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit" });
  return `${item.completedAt ? "done" : "started"} ${date} ${time}`;
}

function formatSubagentDescription(item: DelegationListItem): string {
  const idPart = item.id.length > 18 ? item.id.slice(0, 18) : item.id;
  const modelPart = item.modelRef || "model unknown";
  const thinkingPart = item.thinking ? `thinking ${item.thinking}` : "thinking n/a";
  return `${idPart} · ${modelPart} · ${thinkingPart} · ${formatSubagentTimestamp(item)}`;
}

async function showSubagentsOverlay(ctx: ExtensionContext, items: DelegationListItem[]) {
  const selectedId = await ctx.ui.custom<string | null>((tui, theme, _keybindings, done) => {
    let selectedIndex = 0;
    const rows: SelectItem[] = items.map((item) => ({
      value: item.id,
      label: `${formatDelegationAgent(item.agent, item.specialization)} [${item.status}]`,
      description: formatSubagentDescription(item),
    }));
    const selectList = new SelectList(rows, Math.min(Math.max(rows.length, 6), 18), {
      selectedPrefix: (text) => theme.fg("accent", text),
      selectedText: (text) => theme.fg("accent", text),
      description: (text) => theme.fg("muted", text),
      scrollInfo: (text) => theme.fg("dim", text),
      noMatch: (text) => theme.fg("warning", text),
    }, {
      minPrimaryColumnWidth: 24,
      maxPrimaryColumnWidth: 42,
    });
    selectList.onSelect = (item) => done(item.value);
    selectList.onCancel = () => done(null);
    selectList.onSelectionChange = (item) => {
      const nextIndex = rows.findIndex((row) => row.value === item.value);
      selectedIndex = nextIndex >= 0 ? nextIndex : selectedIndex;
    };

    const padLine = (line: string, width: number) => {
      const padding = Math.max(0, width - visibleWidth(line));
      return line + " ".repeat(padding);
    };
    const bordered = (line: string, innerWidth: number) =>
      theme.fg("accent", "│") + padLine(truncateToWidth(line, innerWidth), innerWidth) + theme.fg("accent", "│");

    return {
      render(width: number) {
        const innerWidth = Math.max(50, width - 2);
        const lines: string[] = [];
        lines.push(theme.fg("accent", `╭${"─".repeat(innerWidth)}╮`));
        lines.push(bordered(theme.bold("Subagents"), innerWidth));
        lines.push(bordered(theme.fg("muted", `${items.length} delegations · Enter opens live overlay`), innerWidth));
        lines.push(bordered("", innerWidth));
        for (const line of selectList.render(innerWidth)) lines.push(bordered(line, innerWidth));
        lines.push(bordered("", innerWidth));
        lines.push(bordered(theme.fg("dim", `↑↓/jk navigate · Enter open · Esc cancel (${selectedIndex + 1}/${items.length})`), innerWidth));
        lines.push(theme.fg("accent", `╰${"─".repeat(innerWidth)}╯`));
        return lines;
      },
      invalidate() {
        selectList.invalidate();
      },
      handleInput(data: string) {
        let nextData = data;
        if (matchesKey(data, "j")) nextData = "\x1b[B";
        else if (matchesKey(data, "k")) nextData = "\x1b[A";
        selectList.handleInput(nextData);
        tui.requestRender();
      },
    };
  }, {
    overlay: true,
    overlayOptions: {
      anchor: "center",
      width: "96%",
      minWidth: 72,
      maxHeight: "80%",
      margin: { top: 2, left: 1, right: 1 },
    },
  });

  if (selectedId) await showLiveDelegationOverlay(ctx, selectedId);
}

const DELEGATION_PROTOCOL = `## Background Delegation Tools\n\nPi has background delegation tools available:\n- delegate(prompt, agent): start a background Pi worker and return immediately with an id\n- delegation_list(): list running/completed delegations for this session\n- delegation_read(id): read a completed delegation result\n\nUse delegation for independent research/review/SDD phase work. Do not poll aggressively; the footer shows aggregate status and a notification appears when a delegation completes.`;

export default function loreDelegation(pi: ExtensionAPI) {
  if (process.env.PI_DELEGATION_CHILD === "1") {
    pi.on("before_agent_start", async (event, _ctx) => {
      const options = event.systemPromptOptions as { skills?: unknown } | undefined;
      const skills = summarizeLoadedSkills(options?.skills);
      await appendChildTrace("skills_loaded", { skills, summary: skills.length > 0 ? `${skills.length} skill(s)` : "no skills loaded" });
    });
    pi.on("agent_start", async (_event, ctx) => {
      const model = ctx.model ? `${ctx.model.provider}/${ctx.model.id}` : undefined;
      await appendChildTrace("agent_start", model ? { summary: `model ${model}`, model } : {});
    });
    pi.on("agent_end", async (_event, _ctx) => appendChildTrace("agent_end"));
    pi.on("turn_start", async (event, _ctx) => appendChildTrace("turn_start", { summary: `turn ${event.turnIndex}` }));
    pi.on("turn_end", async (event, _ctx) => appendChildTrace("turn_end", { summary: `turn ${event.turnIndex}` }));
    pi.on("message_end", async (event, _ctx) => {
      const message = event.message as { role?: string; content?: unknown; provider?: string; model?: string; usage?: { input?: number; output?: number; totalTokens?: number } };
      if (message.role !== "assistant") return;
      const model = message.provider && message.model ? `${message.provider}/${message.model}` : message.model;
      if (message.usage) {
        await appendChildTrace("token_usage", {
          model,
          input: message.usage.input ?? 0,
          output: message.usage.output ?? 0,
          totalTokens: message.usage.totalTokens ?? ((message.usage.input ?? 0) + (message.usage.output ?? 0)),
          summary: `${message.usage.input ?? 0} in / ${message.usage.output ?? 0} out`,
        });
      }
      await appendChildTrace("assistant_message", { summary: compactJson(message.content, 600) });
    });
    pi.on("tool_execution_start", async (event, _ctx) => appendChildTrace("tool_start", { toolName: event.toolName, summary: compactJson(event.args, 700) }));
    pi.on("tool_execution_update", async (event, _ctx) => appendChildTrace("tool_update", { toolName: event.toolName, summary: compactJson(event.partialResult, 700) }));
    pi.on("tool_execution_end", async (event, _ctx) => appendChildTrace("tool_end", { toolName: event.toolName, status: event.isError ? "error" : "ok", summary: compactJson(event.result, 900) }));
    return;
  }

  manager.onComplete = (d, preview) => {
    const envelopeBlock = d.envelope
      ? ("phase" in d.envelope
          ? [
              `Envelope status: ${d.envelope.status}`,
              `Phase: ${d.envelope.phase}`,
              `Summary: ${d.envelope.summary}`,
              `Artifacts: ${d.envelope.artifacts.length > 0 ? d.envelope.artifacts.join("; ") : "(none)"}`,
              `Risks: ${d.envelope.risks.length > 0 ? d.envelope.risks.join("; ") : "(none)"}`,
              `Next step: ${d.envelope.next || "(none)"}`,
            ].join("\n")
          : [
              `Envelope status: ${d.envelope.status}`,
              `Kind: lore-worker/${d.envelope.specialization}`,
              `Summary: ${d.envelope.summary}`,
              `Artifacts: ${d.envelope.artifacts.length > 0 ? d.envelope.artifacts.join("; ") : "(none)"}`,
              `Memory saved: ${d.envelope.memory_saved.length > 0 ? d.envelope.memory_saved.join("; ") : "(none)"}`,
              `Risks: ${d.envelope.risks.length > 0 ? d.envelope.risks.join("; ") : "(none)"}`,
              `Next step: ${d.envelope.next || "(none)"}`,
            ].join("\n"))
      : `Preview: ${preview || "(no output preview)"}\nStructured envelope: missing or invalid`;

    pi.sendMessage({
      customType: "delegation-notification",
      display: true,
      content:
        `Delegation ${d.status}: ${d.id}\n` +
        `Agent: ${formatDelegationAgent(d.agent, d.specialization)}\n` +
        `${envelopeBlock}\n` +
        `Use delegation_read({\"id\":\"${d.id}\"}) to view the full result.\n\n` +
        `Assistant action: acknowledge this completion and provide a brief summary only. ` +
        `Prefer the structured envelope above over raw output. ` +
        `Do not launch follow-up work unless the user explicitly requested automatic continuation or the active workflow state says execution mode auto.`,
      details: {
        id: d.id,
        status: d.status,
        outputFile: d.outputFile,
        envelope: d.envelope,
      },
    }, { triggerTurn: true, deliverAs: "followUp" });
  };

  const noModelGuidanceShown = new Set<string>();
  const noModelGuidanceMessage = "No model configured for Pi delegations. Use /lore-models to select a default or phase model.";
  const noProviderGuidanceMessage = "No available models detected. Configure/login to a Pi provider and API key, then use /lore-models.";

  const showOverlaySelect = async (
    ctx: LoreModelsContext,
    title: string,
    items: SelectItem[],
    options?: { subtitle?: string; width?: number | string; minWidth?: number; maxHeight?: number | string },
  ) => ctx.ui.custom<string | null>((tui, theme, _kb, done) => {
    const titleText = new Text(theme.fg("accent", theme.bold(title)), 1, 0);
    const subtitleText = options?.subtitle ? new Text(theme.fg("muted", options.subtitle), 1, 0) : undefined;
    const footerText = new Text(theme.fg("dim", "↑↓/jk navegar • enter seleccionar • esc regresar"), 1, 0);

    const selectList = new SelectList(items, Math.min(Math.max(items.length, 6), 14), {
      selectedPrefix: (text) => theme.fg("accent", text),
      selectedText: (text) => theme.fg("accent", text),
      description: (text) => theme.fg("muted", text),
      scrollInfo: (text) => theme.fg("dim", text),
      noMatch: (text) => theme.fg("warning", text),
    });
    selectList.onSelect = (item) => done(item.value);
    selectList.onCancel = () => done(null);

    const padLine = (line: string, width: number) => {
      const padding = Math.max(0, width - visibleWidth(line));
      return line + " ".repeat(padding);
    };

    const withSideBorders = (line: string, innerWidth: number) =>
      theme.fg("accent", "│") + padLine(line, innerWidth) + theme.fg("accent", "│");

    return {
      render(width: number) {
        const innerWidth = Math.max(20, width - 2);
        const lines: string[] = [];
        const topBorder = theme.fg("accent", `╭${"─".repeat(innerWidth)}╮`);
        const bottomBorder = theme.fg("accent", `╰${"─".repeat(innerWidth)}╯`);

        lines.push(topBorder);
        for (const line of titleText.render(innerWidth)) lines.push(withSideBorders(line, innerWidth));
        if (subtitleText) {
          for (const line of subtitleText.render(innerWidth)) lines.push(withSideBorders(line, innerWidth));
        }
        lines.push(withSideBorders("", innerWidth));
        for (const line of selectList.render(innerWidth)) lines.push(withSideBorders(line, innerWidth));
        lines.push(withSideBorders("", innerWidth));
        for (const line of footerText.render(innerWidth)) lines.push(withSideBorders(line, innerWidth));
        lines.push(bottomBorder);
        return lines;
      },
      invalidate() {
        titleText.invalidate();
        subtitleText?.invalidate();
        footerText.invalidate();
        selectList.invalidate();
      },
      handleInput(data: string) {
        let nextData = data;
        if (matchesKey(data, "j")) nextData = "\x1b[B";
        else if (matchesKey(data, "k")) nextData = "\x1b[A";
        selectList.handleInput(nextData);
        tui.requestRender();
      },
    };
  }, {
    overlay: true,
    overlayOptions: {
      anchor: "center",
      width: options?.width ?? "70%",
      minWidth: options?.minWidth ?? 60,
      maxHeight: options?.maxHeight ?? "75%",
      margin: 1,
    },
  });

  const openLoreModelsOverlay = async (ctx: LoreModelsContext, options?: { startupGuidance?: boolean }) => {
    if (typeof ctx.waitForIdle === "function") await ctx.waitForIdle();
    if (!ctx.hasUI) return;

    const availableModels = await getAvailableModelsSafe(ctx);
    if (availableModels.length === 0) {
      ctx.ui.notify(noProviderGuidanceMessage, options?.startupGuidance ? "warning" : "error");
      return;
    }

    const modelItems = availableModels
      .map((model) => ({
        value: `${model.provider}/${model.id}`,
        label: `${model.provider}/${model.id}`,
        description: [
          model.name && model.name !== model.id ? model.name : undefined,
          model.reasoning ? "thinking supported" : "thinking off only",
        ].filter(Boolean).join(" • "),
      }))
      .sort((a, b) => a.label.localeCompare(b.label));

    const chooseModel = async (
      key: string,
      current?: string,
      inheritDescription?: string,
      inheritLabel?: string,
    ) => {
      const items: SelectItem[] = [];
      if (inheritDescription && inheritLabel) {
        items.push({
          value: "__inherit__",
          label: inheritLabel,
          description: inheritDescription,
        });
      }
      items.push(...modelItems.map((item) => ({
        ...item,
        description: item.value === current
          ? [item.description, "current"].filter(Boolean).join(" • ")
          : item.description,
      })));
      return showOverlaySelect(ctx, `Model for ${key}`, items, {
        subtitle: current ? `Current: ${current}` : "Pick a model",
        width: "78%",
        minWidth: 72,
        maxHeight: "80%",
      });
    };

    const chooseThinking = async (key: string, current?: ThinkingLevel) => {
      const items: SelectItem[] = THINKING_LEVELS.map((level) => ({
        value: level,
        label: level,
        description: level === current ? "current" : undefined,
      }));
      return showOverlaySelect(ctx, `Thinking for ${key}`, items, {
        subtitle: current ? `Current: ${current}` : "Pick a thinking level",
        width: 42,
        minWidth: 42,
        maxHeight: 16,
      }) as Promise<ThinkingLevel | null>;
    };

    let mainModelOverride: string | undefined;
    let mainThinkingOverride: ThinkingLevel | undefined;

    while (true) {
      const settings = await loadPiSettings();
      const sddConfig = await loadSddModelsConfig();
      const mainModel = mainModelOverride ?? activeMainModelRef(ctx, settings) ?? "unset";
      const mainThinking = mainThinkingOverride ?? pi.getThinkingLevel() ?? settings.defaultThinkingLevel ?? "unset";
      const defaultSdd = sddConfig.default ?? {};
      const defaultNonSdd = sddConfig.nonSddDefault ?? {};
      const loreWorkerKeys = [
        "lore-worker/general",
        "lore-worker/research",
        "lore-worker/review",
        "lore-worker/docs",
      ] as const;

      const menuItems: SelectItem[] = [
        {
          value: "main-agent",
          label: "Main agent",
          description: `${mainModel} · ${mainThinking}`,
        },
        {
          value: "default-non-sdd",
          label: "Default non-SDD routing",
          description: `${defaultNonSdd.model ?? "unset"} · ${defaultNonSdd.thinking ?? "unset"}`,
        },
        ...loreWorkerKeys.map((workerKey) => {
          const choice = sddConfig.workers?.[workerKey] ?? {};
          const inheritedModel = choice.model ?? defaultNonSdd.model ?? "unset";
          const inheritedThinking = choice.thinking ?? defaultNonSdd.thinking ?? "unset";
          const mode = choice.model || choice.thinking ? "custom" : "inherits default-non-sdd";
          return {
            value: workerKey,
            label: workerKey,
            description: `${inheritedModel} · ${inheritedThinking} • ${mode}`,
          };
        }),
        {
          value: "default-sdd",
          label: "Default SDD routing",
          description: `${defaultSdd.model ?? "unset"} · ${defaultSdd.thinking ?? "unset"}`,
        },
        ...SDD_PHASES.map((phase) => {
          const choice = sddConfig.agents?.[phase] ?? {};
          const inheritedModel = choice.model ?? defaultSdd.model ?? "unset";
          const inheritedThinking = choice.thinking ?? defaultSdd.thinking ?? "unset";
          const mode = choice.model || choice.thinking ? "custom" : "inherits default-sdd";
          return {
            value: phase,
            label: phase,
            description: `${inheritedModel} · ${inheritedThinking} • ${mode}`,
          };
        }),
        {
          value: "__close__",
          label: "Close",
          description: "Exit the model routing panel",
        },
      ];

      const selected = await showOverlaySelect(ctx, "Model Routing", menuItems, {
        subtitle: options?.startupGuidance
          ? "No model is configured yet. Pick one to enable delegations."
          : "Main agent + LoreWorker + SDD phases in one place",
        width: "74%",
        minWidth: 68,
        maxHeight: "80%",
      });
      if (!selected || selected === "__close__") return;

      const currentChoice = selected === "main-agent"
        ? { model: mainModel === "unset" ? undefined : mainModel, thinking: normalizeThinking(mainThinking) }
        : selected === "default-sdd"
          ? defaultSdd
          : selected === "default-non-sdd"
            ? defaultNonSdd
            : selected.startsWith("lore-worker/")
              ? sddConfig.workers?.[selected] ?? {}
              : sddConfig.agents?.[selected] ?? {};

      const inheritDescription = selected !== "main-agent" && selected !== "default-sdd" && selected !== "default-non-sdd"
        ? selected.startsWith("lore-worker/")
          ? `${defaultNonSdd.model ?? "unset"} · ${defaultNonSdd.thinking ?? "unset"}`
          : `${defaultSdd.model ?? "unset"} · ${defaultSdd.thinking ?? "unset"}`
        : undefined;
      const inheritLabel = selected !== "main-agent" && selected !== "default-sdd" && selected !== "default-non-sdd"
        ? selected.startsWith("lore-worker/") ? "Use default-non-sdd" : "Use default-sdd"
        : undefined;

      const model = await chooseModel(selected, currentChoice.model, inheritDescription, inheritLabel);
      if (!model) continue;

      if (model === "__inherit__") {
        const nextConfig = await loadSddModelsConfig();
        if (selected.startsWith("lore-worker/")) {
          if (nextConfig.workers?.[selected]) {
            delete nextConfig.workers[selected];
            await saveSddModelsConfig(nextConfig);
          }
          ctx.ui.notify(`${selected} now inherits default-non-sdd`, "info");
        } else {
          if (nextConfig.agents?.[selected]) {
            delete nextConfig.agents[selected];
            await saveSddModelsConfig(nextConfig);
          }
          ctx.ui.notify(`${selected} now inherits default-sdd`, "info");
        }
        continue;
      }

      const thinking = await chooseThinking(selected, currentChoice.thinking);
      if (!thinking) continue;

      if (selected === "main-agent") {
        const parsed = splitModelRef(model);
        if (!parsed) {
          ctx.ui.notify(`Invalid model reference: ${model}`, "error");
          continue;
        }
        const modelObject = findModelSafe(ctx, parsed.provider, parsed.id);
        if (!modelObject) {
          ctx.ui.notify(`Model not found: ${model}`, "error");
          continue;
        }
        const success = await pi.setModel(modelObject);
        if (!success) {
          ctx.ui.notify(`No API key available for ${model}`, "error");
          continue;
        }
        pi.setThinkingLevel(thinking);
        await writeJsonFile(SETTINGS_FILE, { ...settings, defaultProvider: parsed.provider, defaultModel: parsed.id, defaultThinkingLevel: thinking });
        mainModelOverride = model;
        mainThinkingOverride = thinking;
        ctx.ui.notify(`Main agent set to ${model} · ${thinking}`, "info");
        continue;
      }

      const nextConfig = await loadSddModelsConfig();
      if (selected === "default-sdd") {
        nextConfig.default = { model, thinking };
      } else if (selected === "default-non-sdd") {
        nextConfig.nonSddDefault = { model, thinking };
      } else if (selected.startsWith("lore-worker/")) {
        nextConfig.workers = { ...(nextConfig.workers ?? {}), [selected]: { model, thinking } };
      } else {
        nextConfig.agents = { ...(nextConfig.agents ?? {}), [selected]: { model, thinking } };
      }
      await saveSddModelsConfig(nextConfig);
      ctx.ui.notify(`${selected} set to ${model} · ${thinking}`, "info");
    }
  };

  const maybeShowNoModelGuidance = async (ctx: ExtensionContext) => {
    if (process.env.PI_DELEGATION_CHILD === "1" || !ctx.hasUI) return;
    const sid = sessionId(ctx);
    if (noModelGuidanceShown.has(sid)) return;

    const settings = await loadPiSettings();
    const sddConfig = await loadSddModelsConfig();
    if (hasConfiguredDelegationModels(ctx, settings, sddConfig)) return;

    noModelGuidanceShown.add(sid);
    try {
      await openLoreModelsOverlay(ctx, { startupGuidance: true });
    } catch {
      ctx.ui.notify(noModelGuidanceMessage, "warning");
    }
  };

  pi.on("session_start", async (_event, ctx) => {
    manager.setContext(ctx);
    await maybeShowNoModelGuidance(ctx);
  });
  pi.on("before_agent_start", async (event, ctx) => {
    manager.setContext(ctx);
    return { systemPrompt: `${event.systemPrompt}\n\n---\n\n${DELEGATION_PROTOCOL}` };
  });

  pi.registerCommand("lore-models", {
    description: "Configure model routing for the main agent, LoreWorker, and SDD phases",
    handler: async (_args, ctx) => openLoreModelsOverlay(ctx),
  });

  pi.registerShortcut("ctrl+space", {
    description: "Open background delegation viewer",
    handler: async (ctx) => {
      manager.setContext(ctx);
      if (!ctx.hasUI) return;

      const items = await manager.list(ctx);
      if (items.length === 0) {
        ctx.ui.notify("No delegations for this session.", "info");
        return;
      }

      await showSubagentsOverlay(ctx, items);
    },
  });

  pi.registerTool({
    name: "delegate",
    label: "Delegate",
    description: "Start a background Pi worker for independent work. Returns immediately with a delegation id.",
    promptSnippet: "delegate: run independent background work with a Pi worker",
    parameters: Type.Object({
      prompt: Type.String({ description: "Detailed prompt for the background worker. Prefer English for technical artifact work." }),
      agent: Type.Optional(Type.String({ description: "Worker kind. Non-SDD aliases normalize to lore-worker (e.g. reviewer, researcher, scribe, general). SDD phases use sdd-init, sdd-explore, sdd-propose, sdd-spec, sdd-design, sdd-tasks, sdd-apply, sdd-verify, sdd-archive." })),
    }),
    async execute(_id, params, _signal, _update, ctx) {
      try {
        const d = await manager.delegate(ctx, params);
        return textResult(
          `Delegation started: ${d.id}\nAgent: ${formatDelegationAgent(d.agent, d.specialization)}\nStatus: running\nUse delegation_read({"id":"${d.id}"}) when it completes.`,
          { id: d.id, status: d.status, outputFile: d.outputFile, traceFile: d.traceFile, requestedAgent: d.requestedAgent, agent: d.agent, specialization: d.specialization, normalizationNote: d.normalizationNote },
        );
      } catch (err) {
        return textResult(err instanceof Error ? err.message : String(err), { error: true });
      }
    },
  });

  pi.registerTool({
    name: "delegation_list",
    label: "Delegations",
    description: "List background delegations for the current session.",
    promptSnippet: "delegation_list: list background delegation status",
    parameters: Type.Object({}),
    async execute(_id, _params, _signal, _update, ctx) {
      const items = await manager.list(ctx);
      if (items.length === 0) return textResult("No delegations for this session.", { count: 0 });
      const lines = items.map((d) => `- ${d.id} [${d.status}] agent=${formatDelegationAgent(d.agent, d.specialization)} model=${d.modelRef}${d.thinking ? ` thinking=${d.thinking}` : ""} file=${d.outputFile}`);
      return textResult(`## Delegations\n\n${lines.join("\n")}`, { count: items.length, items });
    },
  });

  pi.registerTool({
    name: "delegation_read",
    label: "Read Delegation",
    description: "Read a background delegation result by id.",
    promptSnippet: "delegation_read: retrieve a background delegation result",
    parameters: Type.Object({ id: Type.String({ description: "Delegation id, e.g. dg-abc12-def34" }) }),
    async execute(_id, params, _signal, _update, ctx) {
      try {
        const content = await manager.read(ctx, params.id);
        return textResult(content, { id: params.id });
      } catch (err) {
        return textResult(`Delegation ${params.id} not found: ${err instanceof Error ? err.message : String(err)}`, { id: params.id });
      }
    },
  });
}
