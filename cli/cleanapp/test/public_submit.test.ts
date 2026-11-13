import { randomUUID } from "node:crypto";
import fs from "node:fs/promises";
import os from "node:os";
import path from "node:path";
import { Command } from "commander";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const state = vi.hoisted(() => ({ token: "", trace: false, output: "json" }));
vi.mock("../src/runtime.js", () => ({ resolveRuntimeConfig: async () => ({ apiUrl: "https://fixture.invalid", token: state.token, env: "test", output: state.output, trace: state.trace, dryRun: false }) }));
vi.mock("../src/output.js", () => ({ printJson: vi.fn(), printHuman: vi.fn() }));
import { addSubmit } from "../src/commands/reports/submit.js";
import { toPublicAgentReport } from "../src/public-report.js";
import { printHuman, printJson } from "../src/output.js";

async function submit(...args: string[]) {
  const program = new Command();
  addSubmit(program);
  await program.parseAsync(["submit", ...args], { from: "user" });
}

describe("first report without an integration key", () => {
  beforeEach(() => {
    state.token = ""; state.trace = false; state.output = "json";
    vi.clearAllMocks();
    vi.spyOn(console, "error").mockImplementation(() => {});
    vi.stubGlobal("fetch", vi.fn(async () => new Response(JSON.stringify({ receipt_id: "rcpt_fixture", status: "quarantined", publication_status: "private", tracking_token: "gdt_private" }), { headers: { "content-type": "application/json" } })));
  });
  afterEach(() => { vi.unstubAllGlobals(); vi.restoreAllMocks(); });

  it("sends only the simple description adapter, with one private UUID and no Authorization", async () => {
    await submit("--desc", "I saw an unresponsive checkout button.");
    const calls = vi.mocked(fetch).mock.calls;
    expect(calls).toHaveLength(1);
    const [url, request] = calls[0];
    expect(url).toBe("https://fixture.invalid/api/v1/public/agent-reports");
    expect((request!.headers as any).Authorization).toBeUndefined();
    const body = JSON.parse(String(request!.body));
    expect(body.description).toBe("I saw an unresponsive checkout button.");
    expect(body.source_id).toMatch(/^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/);
    expect(body).not.toHaveProperty("report");
    expect(body).not.toHaveProperty("delivery");
  });

  it("retains an explicit intent unchanged for an exact retry", async () => {
    const sourceId = randomUUID();
    await submit("--desc", "Observed problem", "--source-id", sourceId);
    await submit("--desc", "Observed problem", "--source-id", sourceId);
    const bodies = vi.mocked(fetch).mock.calls.map(([,request]) => String(request!.body));
    expect(bodies[0]).toBe(bodies[1]);
  });

  it("preserves the existing authenticated Wire path", async () => {
    state.token = "synthetic-approved-wire-key";
    await submit("--title", "Observed problem", "--desc", "Details", "--source-id", "existing-integration-id");
    const [url,request] = vi.mocked(fetch).mock.calls[0];
    expect(url).toBe("https://fixture.invalid/api/v1/agent-reports:submit");
    expect((request!.headers as any).Authorization).toBe("Bearer synthetic-approved-wire-key");
    expect(JSON.parse(String(request!.body)).schema_version).toBe("cleanapp-wire.v1");
  });

  it("rejects guessable intent IDs and unsupported advanced context before any HTTP", async () => {
    await expect(submit("--desc", "Observed problem", "--source-id", "test123")).rejects.toThrow("UUIDv4");
    await expect(submit("--desc", "Observed problem", "--collected-at", "2026-11-13T12:00:00Z")).rejects.toThrow("approved Wire");
    await expect(submit("--desc", "Observed problem", "--source-type", "vision")).rejects.toThrow("approved Wire");
    expect(fetch).not.toHaveBeenCalled();
    expect(() => toPublicAgentReport({ desc: "Observed problem", lat: "47" })).toThrow("together");
  });

  it("does not blindly retry an ambiguous failure or leak the retained receipt capability in errors", async () => {
    state.trace = true;
    const secret = "gdt_secret_private_tracking_capability";
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ receipt_id: "rcpt_fixture", tracking_token: secret, processing_status: "pending_retry" }), { status: 503, headers: { "content-type": "application/json" } }));
    const errorLog = vi.spyOn(console, "error").mockImplementation(() => {});
    let error: unknown;
    try { await submit("--desc", "Observed problem", "--source-id", randomUUID()); } catch (caught) { error = caught; }
    expect(String(error)).toContain("503");
    expect(String(error)).not.toContain(secret);
    expect(errorLog.mock.calls.flat().join(" ")).not.toContain(secret);
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("returns the private tracking capability to a human caller without a second submission", async () => {
    state.output = "human";
    await submit("--desc", "Observed problem");
    expect(vi.mocked(printHuman).mock.calls[0][0]).toContain("gdt_private");
    expect(fetch).toHaveBeenCalledTimes(1);
  });

  it("atomically preserves a retained 503 receipt in a private file and refuses to overwrite before another POST", async () => {
    const directory = await fs.mkdtemp(path.join(os.tmpdir(), "cleanapp-private-receipt-"));
    const file = path.join(directory, "receipt.json");
    const secret = "gdt_retained_private";
    vi.mocked(fetch).mockResolvedValue(new Response(JSON.stringify({ receipt_id: "rcpt_retained", tracking_token: secret, publication_status: "private", processing_status: "pending_retry" }), { status: 503, headers: { "content-type": "application/json" } }));
    try {
      await expect(submit("--desc", "Observed problem", "--receipt-file", file)).rejects.toThrow("503");
      expect(JSON.parse(await fs.readFile(file, "utf8")).tracking_token).toBe(secret);
      expect((await fs.stat(file)).mode & 0o777).toBe(0o600);
      expect(JSON.stringify(vi.mocked(printJson).mock.calls)).not.toContain(secret);
      await expect(submit("--desc", "Observed problem", "--receipt-file", file)).rejects.toThrow();
      expect(fetch).toHaveBeenCalledTimes(1);
    } finally { await fs.rm(directory, { recursive: true, force: true }); }
  });
});
