import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

export type CleanAppEnv = "prod" | "sandbox" | "custom";
export type OutputMode = "json" | "human";

export type FileConfig = {
  env?: CleanAppEnv;
  apiUrl?: string;
  output?: OutputMode;
  token?: string;
};

export function getConfigPath(): string {
  // Testability + advanced usage: allow overriding the config path.
  const p = (process.env.CLEANAPP_CONFIG_PATH || "").trim();
  if (p) return p;
  return path.join(os.homedir(), ".cleanapp", "config.json");
}

export function normalizeApiUrl(s: string): string {
  const trimmed = (s || "").trim();
  if (!trimmed) return trimmed;
  return trimmed.replace(/\/+$/, "");
}

export function defaultApiUrlForEnv(env: CleanAppEnv): string {
  switch (env) {
    case "prod":
      // v1 ingest surface currently lives on report-listener.
      return "https://live.cleanapp.io";
    case "sandbox":
      // Placeholder until a dedicated sandbox domain exists.
      return "https://live.cleanapp.io";
    case "custom":
      return "https://live.cleanapp.io";
    default:
      return "https://live.cleanapp.io";
  }
}

export async function loadConfig(): Promise<FileConfig> {
  const p = getConfigPath();
  try {
    const raw = await fs.readFile(p, "utf8");
    const parsed = JSON.parse(raw) as FileConfig;
    if (!parsed || typeof parsed !== "object") return {};
    const out: FileConfig = {};
    if (parsed.env === "prod" || parsed.env === "sandbox" || parsed.env === "custom") out.env = parsed.env;
    if (typeof parsed.apiUrl === "string") out.apiUrl = normalizeApiUrl(parsed.apiUrl);
    if (parsed.output === "json" || parsed.output === "human") out.output = parsed.output;
    if (typeof parsed.token === "string" && parsed.token.trim()) out.token = parsed.token.trim();
    return out;
  } catch (err: any) {
    if (err && err.code === "ENOENT") return {};
    throw err;
  }
}

export async function saveConfig(cfg: FileConfig): Promise<void> {
  const p = getConfigPath();
  await fs.mkdir(path.dirname(p), { recursive: true });

  const out: FileConfig = {
    env: cfg.env,
    apiUrl: cfg.apiUrl ? normalizeApiUrl(cfg.apiUrl) : undefined,
    output: cfg.output,
    token: cfg.token,
  };

  await fs.writeFile(p, JSON.stringify(out, null, 2) + "\n", "utf8");

  // If token is present, try to restrict perms on unix-like systems.
  if (out.token && process.platform !== "win32") {
    try {
      await fs.chmod(p, 0o600);
    } catch {
      // Best-effort.
    }
  }
}

export function redactConfigForPrint(cfg: FileConfig): Record<string, unknown> {
  return {
    env: cfg.env,
    apiUrl: cfg.apiUrl,
    output: cfg.output,
    token: cfg.token ? "***redacted***" : undefined,
  };
}
