import prompts from "prompts";

import { CLIError, EXIT } from "../errors.js";
import { defaultApiUrlForEnv, loadConfig, saveConfig, type CleanAppEnv, type OutputMode } from "../config.js";
import { httpRequest } from "../http.js";
import { printHuman } from "../output.js";

export async function runInit(cmd: any): Promise<void> {
  const existing = await loadConfig();

  const envChoices: Array<{ title: string; value: CleanAppEnv }> = [
    { title: "prod", value: "prod" },
    { title: "sandbox", value: "sandbox" },
    { title: "custom", value: "custom" },
  ];

  const outputChoices: Array<{ title: string; value: OutputMode }> = [
    { title: "json (agent-friendly)", value: "json" },
    { title: "human (friendly)", value: "human" },
  ];

  const tokenChoices = [
    { title: "Use env var only (recommended for agents)", value: "env" },
    { title: "Store token locally (for humans)", value: "store" },
  ] as const;

  const answers = await prompts(
    [
      {
        type: "select",
        name: "env",
        message: "Environment",
        choices: envChoices,
        initial: Math.max(
          0,
          envChoices.findIndex((c) => c.value === (existing.env || "prod")),
        ),
      },
      {
        type: "text",
        name: "apiUrl",
        message: "API base URL",
        initial: existing.apiUrl || defaultApiUrlForEnv((existing.env as CleanAppEnv) || "prod"),
      },
      {
        type: "select",
        name: "output",
        message: "Default output mode",
        choices: outputChoices,
        initial: Math.max(
          0,
          outputChoices.findIndex((c) => c.value === (existing.output || "json")),
        ),
      },
      {
        type: "select",
        name: "tokenMode",
        message: "Token handling",
        choices: tokenChoices as any,
        initial: 0,
      },
      {
        type: (prev: any) => (prev === "store" ? "password" : null),
        name: "token",
        message: "CLEANAPP_API_TOKEN (stored locally, masked input)",
        validate: (v: string) => (v && v.trim() ? true : "Token cannot be empty"),
      },
    ],
    { onCancel: () => true },
  );

  if (!answers || !answers.env || !answers.apiUrl || !answers.output) return;

  const fileCfg = {
    env: answers.env as CleanAppEnv,
    apiUrl: String(answers.apiUrl).trim(),
    output: answers.output as OutputMode,
    token: answers.tokenMode === "store" ? String(answers.token || "").trim() : undefined,
  };

  await saveConfig(fileCfg);

  printHuman(
    [
      "Saved CleanApp CLI config.",
      "",
      "Security note: never paste tokens into chat logs. Prefer env vars for headless agents.",
      "",
      "Next steps:",
      "- Try: cleanapp auth whoami",
      "- Or set env vars for agents:",
      "  export CLEANAPP_API_URL=\"https://live.cleanapp.io\"",
      "  export CLEANAPP_API_TOKEN=\"...\"",
    ].join("\n"),
  );

  // Quick connectivity check (best-effort).
  const token = (process.env.CLEANAPP_API_TOKEN || "").trim() || fileCfg.token || "";
  if (!token) {
    printHuman("\nConnectivity check: set CLEANAPP_API_TOKEN to test auth.");
    return;
  }

  try {
    const cfg = {
      env: fileCfg.env,
      apiUrl: fileCfg.apiUrl,
      output: fileCfg.output,
      token,
      trace: Boolean(cmd?.optsWithGlobals?.()?.trace),
      dryRun: false,
    };
    const res = await httpRequest(cfg as any, { method: "GET", path: "/v1/fetchers/me", idempotent: true });
    const me: any = res.data;
    printHuman(`\nConnectivity check: OK (fetcher_id=${me.fetcher_id}, status=${me.status}, tier=${me.tier})`);
  } catch (err: any) {
    throw new CLIError(`Connectivity check failed: ${String(err?.message || err)}`, EXIT.NET);
  }
}

