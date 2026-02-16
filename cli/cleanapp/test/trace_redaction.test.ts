import { describe, expect, it, vi } from "vitest";

import { httpRequest } from "../src/http.js";

describe("--trace redaction", () => {
  it("never prints the bearer token in trace logs", async () => {
    const spy = vi.spyOn(console, "error").mockImplementation(() => {});
    const token = "cleanapp_fk_live_SUPER_SECRET_TOKEN_SHOULD_NOT_LEAK";

    await httpRequest(
      {
        env: "prod",
        apiUrl: "https://example.invalid",
        output: "json",
        token,
        trace: true,
        dryRun: true,
      } as any,
      { method: "GET", path: "/v1/fetchers/me", idempotent: true },
    );

    const combined = spy.mock.calls.map((c) => c.join(" ")).join("\n");
    expect(combined.includes(token)).toBe(false);
    spy.mockRestore();
  });
});

