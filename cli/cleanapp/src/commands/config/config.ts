import prompts from "prompts";

import { CLIError, EXIT } from "../../errors.js";
import { getConfigPath, loadConfig, redactConfigForPrint, saveConfig, type CleanAppEnv, type OutputMode } from "../../config.js";
import { printHuman, printJson } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";

function assertSupportedKey(k: string): asserts k is "apiUrl" | "output" | "env" | "token" {
  if (k === "apiUrl" || k === "output" || k === "env" || k === "token") return;
  throw new CLIError(`Unsupported key: ${k}. Supported: apiUrl, output, env, token`, EXIT.USER);
}

export async function configPathCmd(cmd: any): Promise<void> {
  const cfg = await resolveRuntimeConfig(cmd);
  if (cfg.output === "human") return printHuman(getConfigPath());
  printJson({ path: getConfigPath() });
}

export async function configGetCmd(cmd: any, key?: string): Promise<void> {
  const cfg = await resolveRuntimeConfig(cmd);
  const fileCfg = await loadConfig();

  if (!key) {
    const redacted = redactConfigForPrint(fileCfg);
    if (cfg.output === "human") return printHuman(JSON.stringify(redacted, null, 2));
    return printJson(redacted);
  }

  assertSupportedKey(key);

  const redacted = redactConfigForPrint(fileCfg);
  const v = (redacted as any)[key];
  if (cfg.output === "human") return printHuman(String(v ?? ""));
  printJson({ [key]: v ?? null });
}

export async function configSetCmd(cmd: any, key: string, value?: string): Promise<void> {
  const cfg = await resolveRuntimeConfig(cmd);
  assertSupportedKey(key);

  const fileCfg = await loadConfig();

  if (key === "token") {
    const opts = cmd?.optsWithGlobals?.() || cmd?.opts?.() || {};
    if (!opts.token) {
      throw new CLIError("Refusing to set token without explicit --token flag.", EXIT.USER);
    }
    if (value) {
      throw new CLIError("Do not pass the token as a CLI argument. Use --token for masked input.", EXIT.USER);
    }
    const ans = await prompts(
      {
        type: "password",
        name: "token",
        message: "CLEANAPP_API_TOKEN (masked input)",
        validate: (v: string) => (v && v.trim() ? true : "Token cannot be empty"),
      },
      { onCancel: () => true },
    );
    if (!ans || !ans.token) return;
    fileCfg.token = String(ans.token).trim();
    await saveConfig(fileCfg);
    if (cfg.output === "human") return printHuman("Token saved to local config (redacted on display).");
    return printJson({ ok: true });
  }

  if (key === "apiUrl") {
    if (!value) throw new CLIError("Missing value for apiUrl", EXIT.USER);
    fileCfg.apiUrl = String(value).trim();
  } else if (key === "output") {
    if (!value) throw new CLIError("Missing value for output", EXIT.USER);
    const v = String(value).trim();
    if (v !== "json" && v !== "human") throw new CLIError("output must be json or human", EXIT.USER);
    fileCfg.output = v as OutputMode;
  } else if (key === "env") {
    if (!value) throw new CLIError("Missing value for env", EXIT.USER);
    const v = String(value).trim();
    if (v !== "prod" && v !== "sandbox" && v !== "custom") throw new CLIError("env must be prod|sandbox|custom", EXIT.USER);
    fileCfg.env = v as CleanAppEnv;
  }

  await saveConfig(fileCfg);
  if (cfg.output === "human") return printHuman("Config updated.");
  printJson({ ok: true });
}

export async function logoutCmd(cmd: any): Promise<void> {
  const cfg = await resolveRuntimeConfig(cmd);
  const fileCfg = await loadConfig();
  if (!fileCfg.token) {
    if (cfg.output === "human") return printHuman("No local token to remove.");
    return printJson({ ok: true, removed: false });
  }
  delete fileCfg.token;
  await saveConfig(fileCfg);
  if (cfg.output === "human") return printHuman("Removed token from local config (env vars unaffected).");
  printJson({ ok: true, removed: true });
}

