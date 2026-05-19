import type { ExtensionAPI } from "@earendil-works/pi-coding-agent";
import { Text } from "@earendil-works/pi-tui";
import { Type } from "typebox";

const loreBinaryPath = "{{LORE_BINARY_PATH}}";
const loreConfigDir = "{{LORE_CONFIG_DIR}}";
const loreServerURL = "{{LORE_SERVER_URL}}";

type BrokerPayload = {
  ok?: boolean;
  status?: number;
  code?: string;
  message?: string;
  request_id?: string;
  data?: unknown;
};

type ToolResult = {
  content: Array<{ type: "text"; text: string }>;
  details?: BrokerPayload;
};

function withQuery(path: string, query: Record<string, unknown>): string {
  const params = new URLSearchParams();
  for (const [key, value] of Object.entries(query)) {
    if (value === undefined || value === null || value === "") {
      continue;
    }
    params.set(key, String(value));
  }
  const encoded = params.toString();
  return encoded ? `${path}?${encoded}` : path;
}

function formatContent(data: unknown): string {
  if (typeof data === "string") {
    return data;
  }
  return JSON.stringify(data ?? null, null, 2);
}

function asRecord(value: unknown): Record<string, unknown> | undefined {
  return value && typeof value === "object" && !Array.isArray(value) ? (value as Record<string, unknown>) : undefined;
}

function summarizeScalar(value: unknown): string | undefined {
  if (typeof value === "string") {
    const trimmed = value.trim();
    return trimmed.length > 0 ? trimmed : undefined;
  }
  if (typeof value === "number" || typeof value === "boolean") {
    return String(value);
  }
  return undefined;
}

function summarizeIdentifier(value: unknown, keys: string[]): string | undefined {
  const record = asRecord(value);
  if (!record) return undefined;
  for (const key of keys) {
    const summary = summarizeScalar(record[key]);
    if (summary) return summary;
  }
  return undefined;
}

function summarizeCount(value: unknown): number | undefined {
  if (Array.isArray(value)) {
    return value.length;
  }
  const record = asRecord(value);
  if (!record) return undefined;
  for (const key of ["items", "results", "memories", "observations", "projects", "skills"]) {
    if (Array.isArray(record[key])) {
      return (record[key] as unknown[]).length;
    }
  }
  return undefined;
}

function renderSummaryText(context: { lastComponent?: unknown }, text: string) {
  const component = (context.lastComponent as Text | undefined) ?? new Text("", 0, 0);
  component.setText(text);
  return component;
}

function compactToolCall(label: string, detail: string | undefined, theme: { bold(text: string): string; fg(name: string, text: string): string }) {
  const title = theme.fg("toolTitle", theme.bold(label));
  return detail ? `${title}${theme.fg("toolOutput", ` ${detail}`)}` : title;
}

function renderCompactCall(
  label: string,
  buildDetail: () => string | undefined,
  theme: { bold(text: string): string; fg(name: string, text: string): string },
  context: { lastComponent?: unknown },
) {
  let detail: string | undefined;
  try {
    detail = buildDetail();
  } catch {
    detail = undefined;
  }
  return renderSummaryText(context, compactToolCall(label, detail, theme));
}

function renderCompactResult(
  label: string,
  buildDetail: () => string | undefined,
  theme: { bold(text: string): string; fg(name: string, text: string): string },
  context: { lastComponent?: unknown; isError?: boolean },
) {
  let detail: string | undefined;
  try {
    detail = buildDetail();
  } catch {
    detail = undefined;
  }
  const status = context.isError ? "failed" : "done";
  const prefix = theme.fg(context.isError ? "warning" : "toolTitle", theme.bold(`${label} ${status}`));
  return renderSummaryText(context, detail ? `${prefix}${theme.fg("toolOutput", ` ${detail}`)}` : prefix);
}

export default function (pi: ExtensionAPI) {
  async function runBroker(method: string, path: string, body: unknown, signal?: AbortSignal) {
    const args = ["api", "request", "--json", "--method", method, "--path", path];
    if (body !== undefined) {
      args.push("--body-json", JSON.stringify(body));
    }

    return runLoreBroker(args, "Lore broker", signal);
  }

  async function runMCPBroker(tool: string, argumentsObject: Record<string, unknown>, signal?: AbortSignal) {
    const args = ["api", "mcp-call", "--json", "--tool", tool, "--args-json", JSON.stringify(argumentsObject)];
    return runLoreBroker(args, "Lore MCP broker", signal);
  }

  async function runLoreBroker(args: string[], label: string, signal?: AbortSignal): Promise<ToolResult> {
    const result = await pi.exec(loreBinaryPath, args, {
      signal,
      env: { LORE_CONFIG_DIR: loreConfigDir },
    });

    const rawOutput = (result.stdout || result.stderr || "").trim();
    if (result.code !== 0) {
      throw new Error(rawOutput || `${label} failed with exit code ${result.code}`);
    }

    let payload: BrokerPayload;
    try {
      payload = JSON.parse(rawOutput) as BrokerPayload;
    } catch (error) {
      throw new Error(`${label} returned invalid JSON: ${String(error)}`);
    }

    if (!payload.ok) {
      throw new Error(payload.message || payload.code || `${label} request failed`);
    }

    return {
      content: [{ type: "text" as const, text: formatContent(payload.data) }],
      details: payload,
    };
  }

  pi.registerTool({
    name: "lore_search",
    label: "Lore Search",
    description: "Search Lore observations",
    parameters: Type.Object({
      query: Type.Optional(Type.String({ description: "Query text is not supported by the current Lore memories API; leave empty and use filters only" })),
      project: Type.Optional(Type.String({ description: "Project key" })),
      type: Type.Optional(Type.String({ description: "Observation type filter" })),
      scope: Type.Optional(Type.String({ description: "Scope filter" })),
      limit: Type.Optional(Type.Number({ description: "Maximum results" })),
    }),
    async execute(_toolCallId, params, signal) {
      const { query, project, type, scope, limit } = params as {
        query?: string;
        project?: string;
        type?: string;
        scope?: string;
        limit?: number;
      };

      if ((query ?? "").trim().length > 0) {
        throw new Error("lore_search query text is unsupported by the current Lore memories API; use filters only");
      }

      const path = withQuery("/v1/memories", {
        project_id: project,
        type,
        scope,
        limit,
      });
      return runBroker("GET", path, undefined, signal);
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { project?: string; type?: string; scope?: string; limit?: number };
      return renderCompactCall(
        "lore_search",
        () => {
          const parts = [
            summarizeScalar(params.project) ? `project=${summarizeScalar(params.project)}` : undefined,
            summarizeScalar(params.type) ? `type=${summarizeScalar(params.type)}` : undefined,
            summarizeScalar(params.scope) ? `scope=${summarizeScalar(params.scope)}` : undefined,
            typeof params.limit === "number" ? `limit=${params.limit}` : undefined,
          ].filter(Boolean);
          return parts.length > 0 ? parts.join(" ") : "filters only";
        },
        theme,
        context,
      );
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_search",
        () => {
          const count = summarizeCount(toolResult.details?.data);
          return count === undefined ? "search completed" : `${count} observation${count === 1 ? "" : "s"}`;
        },
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_save",
    label: "Lore Save",
    description: "Create a Lore memory. Requires a Lore project_id; topic_key is stored as metadata only.",
    parameters: Type.Object({
      title: Type.String({ description: "Memory title" }),
      content: Type.String({ description: "Memory content" }),
      project: Type.String({ description: "Lore project_id (UUID), not project key" }),
      type: Type.Optional(Type.String({ description: "Memory type" })),
      scope: Type.Optional(Type.String({ description: "Scope, defaults to project" })),
      topic_key: Type.Optional(Type.String({ description: "Stable key stored as metadata.topic_key" })),
    }),
    async execute(_toolCallId, params, signal) {
      const { title, content, type, project, scope, topic_key } = params as {
        title: string;
        content: string;
        type?: string;
        project?: string;
        scope?: string;
        topic_key?: string;
      };

      const metadata = topic_key ? { topic_key } : undefined;
      return runBroker(
        "POST",
        "/v1/memories",
        {
          project_id: project,
          scope: scope ?? "project",
          type: type ?? "note",
          title,
          content,
          metadata,
        },
        signal,
      );
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { title?: string; type?: string; project?: string };
      return renderCompactCall(
        "lore_save",
        () => summarizeScalar(params.title) ?? summarizeScalar(params.type) ?? summarizeScalar(params.project) ?? "save memory",
        theme,
        context,
      );
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_save",
        () => summarizeIdentifier(toolResult.details?.data, ["id", "title", "name"]) ?? "memory saved",
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_get_observation",
    label: "Lore Get Observation",
    description: "Get one Lore observation by id",
    parameters: Type.Object({
      id: Type.String({ description: "Observation id" }),
    }),
    async execute(_toolCallId, params, signal) {
      const { id } = params as { id: string };
      return runBroker("GET", `/v1/memories/${encodeURIComponent(id)}`, undefined, signal);
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { id?: string };
      return renderCompactCall("lore_get_observation", () => summarizeScalar(params.id) ?? "load observation", theme, context);
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_get_observation",
        () => summarizeIdentifier(toolResult.details?.data, ["id", "title", "name"]) ?? "observation loaded",
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_context",
    label: "Lore Context",
    description: "Get project Lore context via MCP lore_project_context",
    parameters: Type.Object({
      project: Type.String({ description: "Lore project_id (UUID), not project key" }),
      project_id: Type.Optional(Type.String({ description: "Lore project_id (UUID); overrides project when provided" })),
      memory_limit: Type.Optional(Type.Number({ description: "Maximum memories included in context" })),
    }),
    async execute(_toolCallId, params, signal) {
      const { project, project_id, memory_limit } = params as { project: string; project_id?: string; memory_limit?: number };
      return runMCPBroker(
        "lore_project_context",
        {
          project_id: project_id ?? project,
          memory_limit,
        },
        signal,
      );
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { project?: string; project_id?: string; memory_limit?: number };
      return renderCompactCall(
        "lore_context",
        () => {
          const project = summarizeScalar(params.project_id) ?? summarizeScalar(params.project);
          const limit = typeof params.memory_limit === "number" ? ` limit=${params.memory_limit}` : "";
          return project ? `project=${project}${limit}` : `load project context${limit}`;
        },
        theme,
        context,
      );
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_context",
        () => {
          const count = summarizeCount(toolResult.details?.data);
          return count === undefined ? "project context loaded" : `${count} ${count === 1 ? "memory" : "memories"} in context`;
        },
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_project_list",
    label: "Lore Project List",
    description: "List Lore projects",
    parameters: Type.Object({}),
    async execute(_toolCallId, _params, signal) {
      return runBroker("GET", "/v1/projects", undefined, signal);
    },
    renderCall(_args, theme, context) {
      return renderCompactCall("lore_project_list", () => "list projects", theme, context);
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_project_list",
        () => {
          const count = summarizeCount(toolResult.details?.data);
          return count === undefined ? "projects loaded" : `${count} project${count === 1 ? "" : "s"}`;
        },
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_project_create",
    label: "Lore Project Create",
    description: "Create a Lore project",
    parameters: Type.Object({
      key: Type.String({ description: "Project key" }),
      display_name: Type.String({ description: "Project display name" }),
      metadata: Type.Optional(Type.Any({ description: "Project metadata" })),
    }),
    async execute(_toolCallId, params, signal) {
      return runBroker("POST", "/v1/projects", params, signal);
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { key?: string; display_name?: string };
      return renderCompactCall(
        "lore_project_create",
        () => summarizeScalar(params.key) ?? summarizeScalar(params.display_name) ?? "create project",
        theme,
        context,
      );
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_project_create",
        () => summarizeIdentifier(toolResult.details?.data, ["key", "id", "display_name", "name"]) ?? "project created",
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_project_get",
    label: "Lore Project Get",
    description: "Get a Lore project by id",
    parameters: Type.Object({
      id: Type.String({ description: "Project id" }),
    }),
    async execute(_toolCallId, params, signal) {
      const { id } = params as { id: string };
      return runBroker("GET", `/v1/projects/${encodeURIComponent(id)}`, undefined, signal);
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { id?: string };
      return renderCompactCall("lore_project_get", () => summarizeScalar(params.id) ?? "load project", theme, context);
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_project_get",
        () => summarizeIdentifier(toolResult.details?.data, ["key", "id", "display_name", "name"]) ?? "project loaded",
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_skill_save",
    label: "Lore Skill Save",
    description: "Create or update a Lore skill",
    parameters: Type.Object({
      name: Type.String({ description: "Skill name" }),
      summary: Type.String({ description: "Skill summary" }),
      content: Type.String({ description: "Skill content" }),
      kind: Type.Optional(Type.String({ description: "Skill kind" })),
      metadata: Type.Optional(Type.Any({ description: "Skill metadata" })),
    }),
    async execute(_toolCallId, params, signal) {
      return runBroker("POST", "/v1/skills", params, signal);
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { name?: string; kind?: string };
      return renderCompactCall("lore_skill_save", () => summarizeScalar(params.name) ?? summarizeScalar(params.kind) ?? "save skill", theme, context);
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_skill_save",
        () => summarizeIdentifier(toolResult.details?.data, ["name", "id"]) ?? "skill saved",
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_skill_list",
    label: "Lore Skill List",
    description: "List Lore skills",
    parameters: Type.Object({}),
    async execute(_toolCallId, _params, signal) {
      return runBroker("GET", "/v1/skills", undefined, signal);
    },
    renderCall(_args, theme, context) {
      return renderCompactCall("lore_skill_list", () => "list skills", theme, context);
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_skill_list",
        () => {
          const count = summarizeCount(toolResult.details?.data);
          return count === undefined ? "skills loaded" : `${count} skill${count === 1 ? "" : "s"}`;
        },
        theme,
        context,
      );
    },
  });

  pi.registerTool({
    name: "lore_skill_get",
    label: "Lore Skill Get",
    description: "Get a Lore skill by name",
    parameters: Type.Object({
      name: Type.String({ description: "Skill name" }),
    }),
    async execute(_toolCallId, params, signal) {
      const { name } = params as { name: string };
      return runBroker("GET", `/v1/skills/${encodeURIComponent(name)}`, undefined, signal);
    },
    renderCall(args, theme, context) {
      const params = (args ?? {}) as { name?: string };
      return renderCompactCall("lore_skill_get", () => summarizeScalar(params.name) ?? "load skill", theme, context);
    },
    renderResult(result, _options, theme, context) {
      const toolResult = result as ToolResult;
      return renderCompactResult(
        "lore_skill_get",
        () => summarizeIdentifier(toolResult.details?.data, ["name", "id"]) ?? "skill loaded",
        theme,
        context,
      );
    },
  });

  pi.on("session_start", async (_event, ctx) => {
    if (!ctx.hasUI) return;
    ctx.ui.setStatus("lore", "Lore checking");

    try {
      const result = await pi.exec(loreBinaryPath, ["status"], {
        env: { LORE_CONFIG_DIR: loreConfigDir },
      });
      ctx.ui.setStatus("lore", result.code === 0 ? "Lore healthy" : "Lore degraded");
    } catch {
      ctx.ui.setStatus("lore", "Lore offline");
    }
  });
}
