import type { Command } from "commander";

import { httpRequest } from "../../http.js";
import { printHuman, printJson, requireToken } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";
import { genSourceId, parseFloatArg } from "../../util.js";
import { CLI_VERSION } from "../../version.js";
import { toWireSubmission } from "../../wire.js";

export function addSubmit(program: Command): void {
  program
    .command("submit")
    .description("Submit a single machine-originated report via CleanApp Wire")
    .option("--title <title>", "Report title")
    .option("--desc <description>", "Report description")
    .option("--lat <lat>", "Latitude", undefined)
    .option("--lng <lng>", "Longitude", undefined)
    .option("--source-id <sourceId>", "Idempotency key (auto-generated if omitted)")
    .option("--agent-id <id>", "Agent identifier", "cleanapp-cli")
    .option("--agent-version <version>", "Agent version", CLI_VERSION)
    .option("--collected-at <rfc3339>", "RFC3339 collected timestamp (defaults to now)")
    .option("--source-type <type>", "text|vision|sensor|web", "text")
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);
      requireToken(cfg);

      const sourceId = (opts.sourceId || "").trim() || genSourceId("cleanapp");

      let lat: number | undefined;
      let lng: number | undefined;
      if (opts.lat !== undefined || opts.lng !== undefined) {
        if (opts.lat === undefined || opts.lng === undefined) {
          throw new Error("submit: --lat and --lng must be provided together");
        }
        lat = parseFloatArg(opts.lat, "lat");
        lng = parseFloatArg(opts.lng, "lng");
      }

      const body = toWireSubmission({
        source_id: sourceId,
        title: opts.title || "",
        description: opts.desc || "",
        lat: lat ?? undefined,
        lng: lng ?? undefined,
        collected_at: (opts.collectedAt || "").trim() || new Date().toISOString(),
        agent_id: (opts.agentId || "").trim() || "cleanapp-cli",
        agent_version: (opts.agentVersion || "").trim() || CLI_VERSION,
        source_type: (opts.sourceType || "").trim() || "text",
      });

      const res = await httpRequest(cfg, { method: "POST", path: "/api/v1/agent-reports:submit", body });

      if (cfg.output === "human") {
        const r: any = res.data;
        printHuman(
          [
            `receipt=${r.receipt_id} status=${r.status} lane=${r.lane}`,
            `source_id=${r.source_id} report_id=${r.report_id ?? "(n/a)"} replay=${Boolean(r.idempotency_replay)}`,
          ]
            .filter(Boolean)
            .join("\n"),
        );
        return;
      }

      printJson(res.data);
    });
}
