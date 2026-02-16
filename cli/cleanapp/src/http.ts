import { setTimeout as sleep } from "node:timers/promises";

import { CLIError, EXIT } from "./errors.js";
import { redactHeaders } from "./redact.js";
import type { RuntimeConfig } from "./runtime.js";

export type HttpRequest = {
  method: "GET" | "POST" | "PUT" | "PATCH" | "DELETE";
  path: string;
  query?: Record<string, string | number | boolean | undefined>;
  headers?: Record<string, string>;
  body?: unknown;
  timeoutMs?: number;
  idempotent?: boolean;
};

export type HttpResponse<T = unknown> = {
  status: number;
  data: T;
  headers: Record<string, string>;
};

function toUrl(apiUrl: string, path: string, query?: HttpRequest["query"]): string {
  const base = apiUrl.replace(/\/+$/, "");
  const p = path.startsWith("/") ? path : `/${path}`;
  const u = new URL(base + p);
  for (const [k, v] of Object.entries(query || {})) {
    if (v === undefined) continue;
    u.searchParams.set(k, String(v));
  }
  return u.toString();
}

function defaultTimeoutMs(req: HttpRequest): number {
  if (req.timeoutMs && req.timeoutMs > 0) return req.timeoutMs;
  // Keep CLI snappy; allow longer for uploads.
  if (req.method === "GET") return 20_000;
  return 60_000;
}

function isRetryableStatus(status: number): boolean {
  return status === 502 || status === 503 || status === 504;
}

function classifyHttpErrorExitCode(status: number): number {
  if (status >= 400 && status <= 499) return EXIT.USER;
  return EXIT.NET;
}

async function parseBody(res: Response): Promise<unknown> {
  const ct = res.headers.get("content-type") || "";
  if (ct.includes("application/json")) {
    try {
      return await res.json();
    } catch {
      // fallthrough
    }
  }
  return await res.text();
}

export async function httpRequest<T = unknown>(cfg: RuntimeConfig, req: HttpRequest): Promise<HttpResponse<T>> {
  const url = toUrl(cfg.apiUrl, req.path, req.query);

  const headers: Record<string, string> = { ...(req.headers || {}) };
  if (cfg.token) headers["Authorization"] = `Bearer ${cfg.token}`;
  if (req.body !== undefined && headers["content-type"] == null) headers["content-type"] = "application/json";

  if (cfg.trace) {
    // trace goes to stderr so JSON output stays machine-friendly on stdout.
    // never print secrets.
    // eslint-disable-next-line no-console
    console.error("[trace] request", JSON.stringify({ method: req.method, url, headers: redactHeaders(headers) }));
  }

  if (cfg.dryRun) {
    // Special case: dry-run returns a 200-style response with request preview and does not send.
    return {
      status: 200,
      data: {
        dry_run: true,
        request: {
          method: req.method,
          url,
          headers: redactHeaders(headers),
          body: req.body ?? null,
        },
      } as any,
      headers: {},
    };
  }

  const timeoutMs = defaultTimeoutMs(req);
  const attempts = req.idempotent ? 3 : 1;

  let lastErr: unknown;
  for (let attempt = 1; attempt <= attempts; attempt++) {
    const ac = new AbortController();
    const t = setTimeout(() => ac.abort(new Error("timeout")), timeoutMs);
    try {
      const res = await fetch(url, {
        method: req.method,
        headers,
        body: req.body === undefined ? undefined : JSON.stringify(req.body),
        signal: ac.signal,
      });

      clearTimeout(t);

      const headersOut: Record<string, string> = {};
      res.headers.forEach((v, k) => {
        headersOut[k] = v;
      });

      const body = await parseBody(res);

      if (cfg.trace) {
        // eslint-disable-next-line no-console
        console.error("[trace] response", JSON.stringify({ status: res.status, url }));
      }

      if (res.status >= 400) {
        // Retry safe idempotent calls on transient 5xx-ish statuses.
        if (req.idempotent && attempt < attempts && isRetryableStatus(res.status)) {
          await sleep(250 * attempt);
          continue;
        }

        const msg = typeof body === "string" ? body : JSON.stringify(body);
        throw new CLIError(`HTTP ${res.status} ${url}: ${msg}`, classifyHttpErrorExitCode(res.status), res.status);
      }

      return { status: res.status, data: body as T, headers: headersOut };
    } catch (err) {
      clearTimeout(t);
      lastErr = err;

      const aborted = (err as any)?.name === "AbortError";
      if (cfg.trace) {
        // eslint-disable-next-line no-console
        console.error("[trace] error", JSON.stringify({ url, aborted, attempt, err: String((err as any)?.message || err) }));
      }

      if (req.idempotent && attempt < attempts) {
        await sleep(250 * attempt);
        continue;
      }

      if (err instanceof CLIError) throw err;
      throw new CLIError(`Network error calling ${url}: ${String((err as any)?.message || err)}`, EXIT.NET);
    }
  }

  throw new CLIError(`Network error calling ${url}: ${String((lastErr as any)?.message || lastErr)}`, EXIT.NET);
}
