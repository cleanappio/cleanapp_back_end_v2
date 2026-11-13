import type { Command } from "commander";

import { CLIError, EXIT } from "../../errors.js";
import { httpRequest } from "../../http.js";
import { printHuman, printJson } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";
import { readFileUtf8 } from "../../util.js";

export function addStatus(program: Command): void {
  program
    .command("status")
    .description("Check report status by Wire source_id / receipt_id or legacy report_seq")
    .option("--report-id <seq>", "Report sequence ID (report_seq)")
    .option("--source-id <sourceId>", "Wire source_id")
    .option("--receipt-id <receiptId>", "Wire receipt_id")
    .option("--tracking-token-file <file>", "Private public-intake receipt token file (never a command-line token)")
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);

      const reportId = (opts.reportId || "").trim();
      const sourceId = (opts.sourceId || "").trim();
      const receiptId = (opts.receiptId || "").trim();
      const provided = [reportId, sourceId, receiptId].filter(Boolean);
      if (opts.trackingTokenFile) {
        if (!receiptId || sourceId || reportId) throw new CLIError("status: --tracking-token-file requires only --receipt-id", EXIT.USER);
        const raw = (await readFileUtf8(opts.trackingTokenFile)).trim();
        let token = raw;
        if (raw.startsWith("{")) token = String(JSON.parse(raw).tracking_token || "");
        if (!token.startsWith("gdt_") || token.length > 4096) throw new CLIError("status: invalid private tracking token file", EXIT.USER);
        const res = await httpRequest({ ...cfg, token }, { method: "GET", path: `/api/v1/public/agent-reports/receipts/${encodeURIComponent(receiptId)}` });
        if (cfg.output === "human") printHuman(`receipt=${receiptId} status=${(res.data as any).status} publication=${(res.data as any).publication_status}`);
        else printJson(res.data);
        return;
      }

      if (provided.length === 0) {
        throw new CLIError("status: provide one of --source-id, --receipt-id, or --report-id", EXIT.USER);
      }
      if (provided.length > 1) {
        throw new CLIError("status: provide only one of --source-id, --receipt-id, or --report-id", EXIT.USER);
      }

      if (sourceId) {
        const res = await httpRequest(cfg, {
          method: "GET",
          path: `/api/v1/agent-reports/status/${encodeURIComponent(sourceId)}`,
          idempotent: true,
        });

        if (cfg.output === "human") {
          const r: any = res.data;
          printHuman(
            [
              `source_id=${r.source_id}`,
              `status=${r.status} lane=${r.lane} report_id=${r.report_id ?? "(n/a)"}`,
              `receipt=${r.receipt_id} replay=${Boolean(r.idempotency_replay)} updated_at=${r.updated_at}`,
            ].join("\n"),
          );
          return;
        }
        printJson(res.data);
        return;
      }

      if (receiptId) {
        const res = await httpRequest(cfg, {
          method: "GET",
          path: `/api/v1/agent-reports/receipts/${encodeURIComponent(receiptId)}`,
          idempotent: true,
        });

        if (cfg.output === "human") {
          const r: any = res.data;
          printHuman(
            [
              `receipt=${r.receipt_id}`,
              `status=${r.status} lane=${r.lane} source_id=${r.source_id}`,
              `report_id=${r.report_id ?? "(n/a)"} replay=${Boolean(r.idempotency_replay)} received_at=${r.received_at}`,
            ].join("\n"),
          );
          return;
        }
        printJson(res.data);
        return;
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
