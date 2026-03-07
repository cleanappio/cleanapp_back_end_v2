import { describe, expect, it } from "vitest";

import { isWireSubmission, toWireSubmission } from "../src/wire.js";

describe("wire wrapping", () => {
  it("wraps legacy items into cleanapp-wire.v1 envelopes", () => {
    const wrapped = toWireSubmission({
      source_id: "source-1",
      title: "Broken login",
      description: "Users stuck on callback",
      source_type: "web",
    });

    expect(wrapped.schema_version).toBe("cleanapp-wire.v1");
    expect((wrapped as any).source_id).toBe("source-1");
    expect((wrapped as any).agent.agent_id).toBe("cleanapp-cli");
    expect((wrapped as any).report.domain).toBe("digital");
    expect((wrapped as any).report.digital_context).toBeTruthy();
  });

  it("passes through existing Wire envelopes unchanged", () => {
    const input = {
      schema_version: "cleanapp-wire.v1",
      source_id: "source-2",
      submitted_at: new Date().toISOString(),
      agent: { agent_id: "a", agent_type: "cli", auth_method: "api_key" },
      report: { domain: "digital", problem_type: "general_issue", title: "x", confidence: 0.8, digital_context: {} },
    };

    expect(isWireSubmission(input)).toBe(true);
    expect(toWireSubmission(input as any)).toBe(input);
  });
});
