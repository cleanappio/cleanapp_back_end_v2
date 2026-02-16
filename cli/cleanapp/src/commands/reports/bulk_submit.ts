import fs from "node:fs/promises";

import type { Command } from "commander";
import { parse as parseCsv } from "csv-parse/sync";

import { httpRequest } from "../../http.js";
import { printHuman, printJson, requireToken } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";
import { fileExt } from "../../util.js";

type IngestItem = Record<string, any>;

async function loadItems(filePath: string): Promise<IngestItem[]> {
  const raw = await fs.readFile(filePath, "utf8");
  const ext = fileExt(filePath);

  if (ext === ".ndjson" || ext === ".jsonl") {
    const items: IngestItem[] = [];
    for (const line of raw.split(/\r?\n/)) {
      const t = line.trim();
      if (!t) continue;
      items.push(JSON.parse(t));
    }
    return items;
  }

  if (ext === ".json") {
    const parsed = JSON.parse(raw);
    if (Array.isArray(parsed)) return parsed;
    if (parsed && typeof parsed === "object" && Array.isArray((parsed as any).items)) return (parsed as any).items;
    throw new Error("bulk-submit: JSON must be an array or an object with {items:[...] }");
  }

  if (ext === ".csv") {
    const rows = parseCsv(raw, { columns: true, skip_empty_lines: true, trim: true });
    return rows.map((r: any) => {
      const item: any = {
        source_id: r.source_id || r.sourceId,
        title: r.title || "",
        description: r.description || r.desc || "",
        collected_at: r.collected_at || r.collectedAt || "",
        agent_id: r.agent_id || r.agentId || "",
        agent_version: r.agent_version || r.agentVersion || "",
        source_type: r.source_type || r.sourceType || "",
      };
      if (r.lat !== undefined && r.lat !== "") item.lat = Number(r.lat);
      if (r.lng !== undefined && r.lng !== "") item.lng = Number(r.lng);
      if (r.media_url) {
        item.media = [
          {
            url: r.media_url,
            sha256: r.media_sha256 || "",
            content_type: r.media_content_type || "",
          },
        ];
      }
      return item;
    });
  }

  throw new Error("bulk-submit: unsupported file type (use .ndjson, .jsonl, .json, or .csv)");
}

function chunk<T>(xs: T[], n: number): T[][] {
  const out: T[][] = [];
  for (let i = 0; i < xs.length; i += n) out.push(xs.slice(i, i + n));
  return out;
}

export function addBulkSubmit(program: Command): void {
  program
    .command("bulk-submit")
    .description("Bulk submit reports from a file (ndjson|json|csv)")
    .requiredOption("--file <path>", "Input file path (.ndjson|.jsonl|.json|.csv)")
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);
      requireToken(cfg);

      const items = await loadItems(String(opts.file));
      if (!Array.isArray(items) || items.length === 0) {
        throw new Error("bulk-submit: no items found");
      }

      // API maxItems is 100.
      const batches = chunk(items, 100);

      const responses: any[] = [];
      let submitted = 0;
      let accepted = 0;
      let duplicates = 0;
      let rejected = 0;

      for (const b of batches) {
        const body = { items: b };
        const res = await httpRequest(cfg, { method: "POST", path: "/v1/reports:bulkIngest", body });
        responses.push(res.data);
        const r: any = res.data;
        submitted += Number(r.submitted || 0);
        accepted += Number(r.accepted || 0);
        duplicates += Number(r.duplicates || 0);
        rejected += Number(r.rejected || 0);
      }

      if (cfg.output === "human") {
        printHuman(
          [
            `batches=${batches.length} items=${items.length}`,
            `submitted=${submitted} accepted=${accepted} duplicates=${duplicates} rejected=${rejected}`,
          ].join("\n"),
        );
        return;
      }

      printJson({
        batches: batches.length,
        items: items.length,
        totals: { submitted, accepted, duplicates, rejected },
        responses,
      });
    });
}

