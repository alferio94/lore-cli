import path from "node:path";
import type { ExtensionAPI, ExtensionContext } from "@earendil-works/pi-coding-agent";
import { truncateToWidth, visibleWidth } from "@earendil-works/pi-tui";

function formatTokens(value: number): string {
  if (value >= 1_000_000) return `${(value / 1_000_000).toFixed(1)}M`;
  if (value >= 1_000) return `${Math.round(value / 1_000)}K`;
  return `${value}`;
}

function padBetween(left: string, right: string, width: number): string {
  const gap = Math.max(1, width - visibleWidth(left) - visibleWidth(right));
  return truncateToWidth(left + " ".repeat(gap) + right, width);
}

function contextColor(percent: number | null | undefined): "success" | "warning" | "error" | "muted" {
  if (percent == null) return "muted";
  if (percent < 60) return "success";
  if (percent < 80) return "warning";
  return "error";
}

function thinkingColor(level: string | undefined): string {
  switch (level) {
    case "minimal": return "thinkingMinimal";
    case "low": return "thinkingLow";
    case "medium": return "thinkingMedium";
    case "high": return "thinkingHigh";
    case "xhigh": return "thinkingXhigh";
    default: return "thinkingOff";
  }
}

function renderLoreStatus(theme: any, value: string | undefined): string | undefined {
  if (!value) return undefined;
  const normalized = value.toLowerCase();
  if (normalized.includes("offline")) {
    return `${theme.fg("dim", "Lore")} ${theme.fg("error", "offline")}`;
  }
  if (normalized.includes("degraded")) {
    return `${theme.fg("dim", "Lore")} ${theme.fg("warning", "degraded")}`;
  }
  return `${theme.fg("dim", "Lore")} ${theme.fg("success", "healthy")}`;
}

function renderSubagentStatus(theme: any, value: string | undefined): string | undefined {
  if (!value) return undefined;
  const match = value.match(/subagents\s+(\d+)⏳\s+(\d+)✓\s+(\d+)✗/i);
  if (!match) return theme.fg("muted", value);

  const running = Number.parseInt(match[1] ?? "0", 10);
  const completed = Number.parseInt(match[2] ?? "0", 10);
  const error = Number.parseInt(match[3] ?? "0", 10);
  if (running === 0 && completed === 0 && error === 0) return undefined;

  const parts = [theme.fg("dim", "sub")];
  if (running > 0) parts.push(theme.fg("warning", `${running}⏳`));
  if (completed > 0) parts.push(theme.fg("success", `${completed}✓`));
  if (error > 0) parts.push(theme.fg("error", `${error}✗`));
  return parts.join(` ${theme.fg("dim", "·")} `);
}

export default function loreFooter(pi: ExtensionAPI) {
  let currentModel = "no-model";

  const installFooter = (ctx: ExtensionContext) => {
    if (!ctx.hasUI) return;

    ctx.ui.setFooter((tui, theme, footerData) => {
      const unsub = footerData.onBranchChange(() => tui.requestRender());

      return {
        dispose: unsub,
        invalidate() {},
        render(width: number): string[] {
          const project = path.basename(ctx.cwd) || ctx.cwd;
          const branch = footerData.getGitBranch();
          const usage = ctx.getContextUsage();
          const model = currentModel;
          const thinking = pi.getThinkingLevel();

          const leftBits = [theme.fg("accent", project)];
          if (branch) {
            leftBits.push(`${theme.fg("dim", "branch")} ${theme.fg("muted", branch)}`);
          }
          if (usage) {
            const pct = usage.percent == null ? "?" : `${usage.percent.toFixed(1)}%`;
            const ctxText = `${pct}/${formatTokens(usage.contextWindow)}`;
            leftBits.push(`${theme.fg("dim", "ctx")} ${theme.fg(contextColor(usage.percent), ctxText)}`);
          }

          const right = `${theme.fg("muted", model)} ${theme.fg("dim", "·")} ${theme.fg(thinkingColor(thinking), thinking)}`;
          const line1 = padBetween(leftBits.join(` ${theme.fg("dim", "·")} `), right, width);

          const statuses = footerData.getExtensionStatuses();
          const statusBits: string[] = [];

          const loreStatus = renderLoreStatus(theme, statuses.get("lore"));
          if (loreStatus) statusBits.push(loreStatus);

          const subagentStatus = renderSubagentStatus(theme, statuses.get("subagents"));
          if (subagentStatus) statusBits.push(subagentStatus);

          for (const [key, value] of statuses.entries()) {
            if (key === "lore" || key === "subagents" || !value) continue;
            statusBits.push(theme.fg("muted", value));
          }

          if (statusBits.length === 0) {
            statusBits.push(`${theme.fg("dim", "Lore")} ${theme.fg("success", "healthy")}`);
          }

          const line2 = truncateToWidth(statusBits.join(` ${theme.fg("dim", "·")} `), width);
          return [line1, line2];
        },
      };
    });
  };

  pi.on("session_start", async (_event, ctx) => {
    currentModel = ctx.model?.id ?? "no-model";
    installFooter(ctx);
  });

  pi.on("model_select", async (event, ctx) => {
    currentModel = event.model.id;
    installFooter(ctx);
  });

  pi.on("thinking_level_select", async (_event, ctx) => {
    installFooter(ctx);
  });
}
