import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";

import { describe, expect, it, beforeEach, afterEach } from "vitest";

import { loadConfig, redactConfigForPrint, saveConfig } from "../src/config.js";
import { resolveRuntimeConfig } from "../src/runtime.js";

function mockCmd(opts: Record<string, any>) {
  return {
    optsWithGlobals: () => opts,
  };
}

describe("config + runtime precedence", () => {
  let tmpDir = "";
  let cfgPath = "";

  beforeEach(async () => {
    tmpDir = await fs.mkdtemp(path.join(os.tmpdir(), "cleanapp-cli-test-"));
    cfgPath = path.join(tmpDir, "config.json");
    process.env.CLEANAPP_CONFIG_PATH = cfgPath;

    delete process.env.CLEANAPP_API_URL;
    delete process.env.CLEANAPP_API_TOKEN;
  });

  afterEach(async () => {
    delete process.env.CLEANAPP_CONFIG_PATH;
    delete process.env.CLEANAPP_API_URL;
    delete process.env.CLEANAPP_API_TOKEN;
    await fs.rm(tmpDir, { recursive: true, force: true });
  });

  it("redacts token for printing", () => {
    const r = redactConfigForPrint({ token: "secret", apiUrl: "x", env: "prod", output: "json" });
    expect(r.token).toBe("***redacted***");
  });

  it("flags > env vars > config file > defaults", async () => {
    await saveConfig({ env: "prod", apiUrl: "https://from-file.example", output: "human", token: "filetoken" });

    process.env.CLEANAPP_API_URL = "https://from-env.example";
    process.env.CLEANAPP_API_TOKEN = "envtoken";

    const cfg = await resolveRuntimeConfig(
      mockCmd({
        apiUrl: "https://from-flag.example",
        output: "json",
        trace: true,
        dryRun: true,
      }),
    );

    expect(cfg.apiUrl).toBe("https://from-flag.example");
    expect(cfg.output).toBe("json");
    expect(cfg.trace).toBe(true);
    expect(cfg.dryRun).toBe(true);
    expect(cfg.token).toBe("envtoken");

    // And ensure file is actually readable via our override path.
    const fileCfg = await loadConfig();
    expect(fileCfg.apiUrl).toBe("https://from-file.example");
  });
});

