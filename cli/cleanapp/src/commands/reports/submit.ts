import type { Command } from "commander";

import { httpRequest } from "../../http.js";
import { printHuman, printJson } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";
import { genSourceId, parseFloatArg } from "../../util.js";
import { CLI_VERSION } from "../../version.js";
import { toWireSubmission } from "../../wire.js";
import { toPublicAgentReport, writePrivateJson } from "../../public-report.js";
import { CLIError } from "../../errors.js";

export function addSubmit(program: Command): void {
  program
    .command("submit")
    .description("Submit one report privately without a key, or use approved Wire credentials")
    .option("--title <title>", "Report title")
    .option("--desc <description>", "Report description")
    .option("--lat <lat>", "Latitude", undefined)
    .option("--lng <lng>", "Longitude", undefined)
    .option("--source-id <sourceId>", "Idempotency key (auto-generated if omitted)")
    .option("--url <url>", "Observed HTTP(S) URL (public private-review intake)")
    .option("--receipt-file <file>", "Save private intent/receipt to a new mode-0600 JSON file")
    .option("--agent-id <id>", "Agent identifier", "cleanapp-cli")
    .option("--agent-version <version>", "Agent version", CLI_VERSION)
    .option("--collected-at <rfc3339>", "RFC3339 collected timestamp (defaults to now)")
    .option("--source-type <type>", "text|vision|sensor|web", "text")
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);
      if (!cfg.token) {
        const body = toPublicAgentReport(opts);
        if (!cfg.dryRun) {
          // This is intentional private CLI output, never an HTTP trace. It
          // preserves the intent when the network response is ambiguous.
          console.error(`source_id=${body.source_id} (keep private; retain for exact retries)`);
          if (opts.receiptFile) await writePrivateJson(opts.receiptFile, { source_id: body.source_id, processing_status: "unconfirmed", request: body });
        }
        const deliver = async (r: any) => {
          if (opts.receiptFile && !cfg.dryRun) {
            try { await writePrivateJson(opts.receiptFile, r, true); }
            catch { printJson(r); throw new Error("Could not save the receipt file; private receipt was returned on stdout. Retain it; do not submit a new report."); }
          }
          if (cfg.output === "human" && !cfg.dryRun) {
            printHuman(`receipt=${r.receipt_id} status=${r.status} publication=${r.publication_status}\nsource_id=${r.source_id}\n${opts.receiptFile ? `Private receipt saved: ${opts.receiptFile}` : `Private tracking token: ${r.tracking_token}`}\nKeep the receipt private. Check with cleanapp status --receipt-id ${r.receipt_id} --tracking-token-file <private-receipt.json>.`);
          } else if (opts.receiptFile && r.tracking_token) {
            const { tracking_token: _, ...summary } = r;
            printJson({ ...summary, private_receipt_file: opts.receiptFile });
          } else printJson(r);
        };
        // Generate the UUID once before sending. No blind automatic retry:
        // callers can retain --source-id and retry this same intent explicitly.
        try {
          const res = await httpRequest(cfg, { method: "POST", path: "/api/v1/public/agent-reports", body });
          await deliver(res.data);
        } catch (error) {
          const retained = error instanceof CLIError ? error.response as any : undefined;
          if (retained?.receipt_id && retained?.tracking_token) await deliver(retained);
          throw error;
        }
        return;
      }
      if (opts.receiptFile) throw new Error("--receipt-file is for public private-review intake; approved Wire receipts use JSON output");
      if (opts.url) throw new Error("--url is supported by public intake; approved Wire submissions use their existing envelope/bulk input");

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
