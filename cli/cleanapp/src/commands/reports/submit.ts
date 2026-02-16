import type { Command } from "commander";

import { httpRequest } from "../../http.js";
import { printHuman, printJson, requireToken } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";
import { genSourceId, parseFloatArg } from "../../util.js";
import { CLI_VERSION } from "../../version.js";

export function addSubmit(program: Command): void {
  program
    .command("submit")
    .description("Submit a single report (wraps POST /v1/reports:bulkIngest)")
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

      const body = {
        items: [
          {
            source_id: sourceId,
            title: opts.title || "",
            description: opts.desc || "",
            lat: lat ?? undefined,
            lng: lng ?? undefined,
            collected_at: (opts.collectedAt || "").trim() || new Date().toISOString(),
            agent_id: (opts.agentId || "").trim() || "cleanapp-cli",
            agent_version: (opts.agentVersion || "").trim() || CLI_VERSION,
            source_type: (opts.sourceType || "").trim() || "text",
          },
        ],
      };

      const res = await httpRequest(cfg, { method: "POST", path: "/v1/reports:bulkIngest", body });

      if (cfg.output === "human") {
        const r: any = res.data;
        const it = r.items?.[0];
        printHuman(
          [
            `submitted=${r.submitted} accepted=${r.accepted} duplicates=${r.duplicates} rejected=${r.rejected}`,
            it ? `item: status=${it.status} source_id=${it.source_id} report_seq=${it.report_seq ?? "(n/a)"} queued=${it.queued}` : "",
          ]
            .filter(Boolean)
            .join("\n"),
        );
        return;
      }

      printJson(res.data);
    });
}

