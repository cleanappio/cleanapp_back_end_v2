import type { Command } from "commander";

import { CLIError, EXIT } from "../errors.js";
import { httpRequest } from "../http.js";
import { printHuman, printJson, requireToken } from "../output.js";
import { resolveRuntimeConfig } from "../runtime.js";

export function addMetrics(program: Command): void {
  program
    .command("metrics")
    .description("Fetcher metrics (calls GET /v1/fetchers/me/metrics when available)")
    .option("--since <dur>", "Since window (e.g. 24h|7d)", "24h")
    .option("--group-by <unit>", "Group by (hour|day)", "hour")
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);
      requireToken(cfg);

      const since = String(opts.since || "24h").trim();
      const groupBy = String(opts.groupBy || "hour").trim();

      try {
        const res = await httpRequest(cfg, {
          method: "GET",
          path: "/v1/fetchers/me/metrics",
          query: { since, group_by: groupBy },
          idempotent: true,
        });

        if (cfg.output === "human") {
          printHuman(JSON.stringify(res.data, null, 2));
          return;
        }
        printJson(res.data);
      } catch (err: any) {
        const status = (err as any)?.status;
        if (status === 404) {
          // Graceful fallback to /v1/fetchers/me (caps/usage) so users still get something useful.
          const me = await httpRequest(cfg, { method: "GET", path: "/v1/fetchers/me", idempotent: true });
          const msg = "metrics endpoint not available on this API yet; returning /v1/fetchers/me instead";

          if (cfg.output === "human") {
            const m: any = me.data;
            printHuman(
              [
                msg,
                "",
                `Fetcher: ${m.fetcher_id} (${m.name || "unknown"})`,
                `Status: ${m.status} | Tier: ${m.tier} | Reputation: ${m.reputation_score}`,
                `Caps: ${m.caps?.per_minute_cap_items}/min, ${m.caps?.daily_cap_items}/day`,
                `Usage: ${m.usage?.minute_used}/min, ${m.usage?.daily_used}/day, remaining=${m.usage?.daily_remaining}`,
              ].join("\n"),
            );
            return;
          }

          printJson({ ok: true, warning: msg, me: me.data });
          return;
        }
        throw err;
      }
    });
}
