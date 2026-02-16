import { defaultApiUrlForEnv, loadConfig, normalizeApiUrl, type CleanAppEnv, type OutputMode } from "./config.js";

export type RuntimeConfig = {
  env: CleanAppEnv;
  apiUrl: string;
  output: OutputMode;
  token?: string;
  trace: boolean;
  dryRun: boolean;
};

export async function resolveRuntimeConfig(cmd: any): Promise<RuntimeConfig> {
  const fileCfg = await loadConfig();

  const opts = typeof cmd?.optsWithGlobals === "function" ? cmd.optsWithGlobals() : cmd?.opts?.() || {};

  const env: CleanAppEnv = (fileCfg.env as any) || "prod";

  const apiUrl =
    (opts.apiUrl ? normalizeApiUrl(String(opts.apiUrl)) : "") ||
    (process.env.CLEANAPP_API_URL ? normalizeApiUrl(process.env.CLEANAPP_API_URL) : "") ||
    (fileCfg.apiUrl ? normalizeApiUrl(fileCfg.apiUrl) : "") ||
    defaultApiUrlForEnv(env);

  let output: OutputMode =
    (opts.output === "json" || opts.output === "human" ? opts.output : undefined) ||
    (fileCfg.output === "json" || fileCfg.output === "human" ? fileCfg.output : undefined) ||
    "json";

  if (opts.human === true) output = "human";

  const token = (process.env.CLEANAPP_API_TOKEN || "").trim() || (fileCfg.token || "").trim() || undefined;

  return {
    env,
    apiUrl,
    output,
    token,
    trace: Boolean(opts.trace),
    dryRun: Boolean(opts.dryRun),
  };
}

