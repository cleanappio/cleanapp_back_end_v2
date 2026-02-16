import type { Command } from "commander";

import { CLIError, EXIT } from "../../errors.js";
import { httpRequest } from "../../http.js";
import { printHuman, printJson } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";

export function addStatus(program: Command): void {
  program
    .command("status")
    .description("Check report status (currently uses /api/v3/reports/by-seq for report_seq)")
    .option("--report-id <seq>", "Report sequence ID (report_seq)")
    .option("--source-id <sourceId>", "Fetcher source_id (not yet supported by API)")
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);

      const reportId = (opts.reportId || "").trim();
      const sourceId = (opts.sourceId || "").trim();

      if (!reportId && !sourceId) throw new CLIError("status: provide --report-id (or --source-id when supported)", EXIT.USER);
      if (reportId && sourceId) throw new CLIError("status: provide only one of --report-id or --source-id", EXIT.USER);

      if (sourceId) {
        throw new CLIError(
          "status by --source-id is not supported yet (no API endpoint). Use the report_seq returned from bulkIngest.",
          EXIT.USER,
        );
      }

      const res = await httpRequest(cfg, {
        method: "GET",
        path: "/api/v3/reports/by-seq",
        query: { seq: reportId },
        idempotent: true,
      });

      if (cfg.output === "human") {
        const r: any = res.data;
        const seq = r?.report?.seq ?? r?.Report?.Seq ?? reportId;
        const title = r?.analysis?.title || r?.Analysis?.Title || r?.report?.title || r?.Report?.Title || "(no title)";
        printHuman([`Report seq=${seq}`, `Title: ${title}`].join("\n"));
        return;
      }

      printJson(res.data);
    });
}

