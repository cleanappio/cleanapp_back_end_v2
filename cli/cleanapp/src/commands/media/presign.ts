import fs from "node:fs/promises";
import path from "node:path";

import type { Command } from "commander";
import mime from "mime-types";

import { CLIError, EXIT } from "../../errors.js";
import { httpRequest } from "../../http.js";
import { printHuman, printJson, requireToken } from "../../output.js";
import { resolveRuntimeConfig } from "../../runtime.js";

// NOTE: As of this CLI's initial release, the backend does not expose /v1/media:presign.
// We keep the CLI thin and do NOT invent a presign protocol; instead we:
// - attempt the call
// - if missing, return a clear "API gap" message
// This preserves forward compatibility when the endpoint is added server-side.

export function addPresign(program: Command): void {
  program
    .command("presign")
    .description("Request a presigned upload URL (calls POST /v1/media:presign). Optionally upload the file.")
    .requiredOption("--file <path>", "File path to upload")
    .option("--upload", "Upload the file to the returned presigned URL", false)
    .action(async function (opts: any) {
      const cfg = await resolveRuntimeConfig(this);
      requireToken(cfg);

      const filePath = String(opts.file);
      const upload = Boolean(opts.upload);

      let st: { size: number };
      try {
        const s = await fs.stat(filePath);
        st = { size: s.size };
      } catch (err: any) {
        throw new CLIError(`presign: cannot read file: ${filePath} (${String(err?.message || err)})`, EXIT.USER);
      }

      const contentType = String(mime.lookup(filePath) || "application/octet-stream");
      const filename = path.basename(filePath);

      // Best-effort, minimal metadata (the server can ignore fields it doesn't need).
      const body = { filename, content_type: contentType, size_bytes: st.size };

      try {
        const res = await httpRequest(cfg, { method: "POST", path: "/v1/media:presign", body });
        const data: any = res.data;

        // If caller requested upload, we need a URL and method.
        if (upload) {
          const method = String(data?.method || "PUT").toUpperCase();
          const url = String(data?.url || "");
          const headers = (data?.headers && typeof data.headers === "object" ? data.headers : {}) as Record<string, string>;

          if (!url) {
            throw new CLIError("presign: response missing url; cannot upload", EXIT.NET);
          }

          if (cfg.trace) {
            // Never print full presigned URL (it may contain signatures).
            // eslint-disable-next-line no-console
            console.error("[trace] upload", JSON.stringify({ method, url: new URL(url).origin + new URL(url).pathname }));
          }

          if (cfg.dryRun) {
            // httpRequest already handled dry-run for the presign call; do not attempt upload.
          } else {
            const buf = await fs.readFile(filePath);
            const upRes = await fetch(url, {
              method,
              headers: { ...headers, "content-type": contentType },
              body: buf,
            });
            if (!upRes.ok) {
              const t = await upRes.text().catch(() => "");
              throw new CLIError(`presign upload failed: HTTP ${upRes.status} ${t}`.trim(), EXIT.NET);
            }
          }
        }

        if (cfg.output === "human") {
          const url = data?.url ? "(url returned)" : "(no url)";
          printHuman(`presign ok ${url}${upload ? " (upload attempted)" : ""}`);
          return;
        }

        printJson(res.data);
      } catch (err: any) {
        // If backend doesn't have the endpoint, make it explicit instead of a cryptic HTTP error.
        const status = (err as any)?.status;
        if (status === 404) {
          const msg =
            "presign is not available on this API yet (missing POST /v1/media:presign). " +
            "For now, host media yourself and include `media: [{url, sha256, content_type}]` in bulkIngest items.";
          if (cfg.output === "human") {
            printHuman(msg);
            throw new CLIError(msg, EXIT.USER);
          }
          printJson({ ok: false, error: "api_gap", message: msg });
          throw new CLIError(msg, EXIT.USER);
        }
        throw err;
      }
    });
}

