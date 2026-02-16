import type { Command } from "commander";

import { httpRequest } from "../../http.js";
import { printHuman, printJson, requireToken } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";

export function addAuthWhoami(auth: Command): void {
  auth
    .command("whoami")
    .description("Show the current fetcher identity for the configured API token")
    .action(async function () {
      const cfg = await resolveRuntimeConfig(this);
      requireToken(cfg);

      const res = await httpRequest(cfg, { method: "GET", path: "/v1/fetchers/me", idempotent: true });

      if (cfg.output === "human") {
        const me: any = res.data;
        printHuman(
          [
            `Fetcher: ${me.fetcher_id} (${me.name || "unknown"})`,
            `Owner: ${me.owner_type} | Status: ${me.status} | Tier: ${me.tier} | Reputation: ${me.reputation_score}`,
            `Caps: ${me.caps?.per_minute_cap_items}/min, ${me.caps?.daily_cap_items}/day`,
            `Usage: ${me.usage?.minute_used}/min, ${me.usage?.daily_used}/day, remaining=${me.usage?.daily_remaining}`,
          ].join("\n"),
        );
        return;
      }

      printJson(res.data);
    });
}

